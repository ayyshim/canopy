package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
)

// offline is a runner hub where no computer is connected.
type offline struct{}

func (offline) Online(uuid.UUID) bool                                      { return false }
func (offline) Send(uuid.UUID, string, any) error                          { return nil }
func (offline) Request(context.Context, uuid.UUID, string, any, any) error { return nil }

func (e *env) addRunner(org, owner, name, kind string) string {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	fp := id.String()
	_, err := e.store.CreateRunner(context.Background(), db.CreateRunnerParams{ID: id, OrgID: uuid.MustParse(org), OwnerUserID: uuid.MustParse(owner),
		Name: name, Kind: kind, CertFingerprint: &fp})
	if err != nil {
		e.t.Fatal(err)
	}
	return id.String()
}

func userID(me map[string]any) string { return str(me["user"].(map[string]any)["id"]) }

// joinOrg invites someone into the owner's organization and returns their client.
func (e *env) joinOrg(owner *client, org, name string) (*client, string) {
	e.t.Helper()
	email := uniqueEmail(strings.ToLower(name))
	r := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": email, "role": "member"})
	token := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	c := e.client()
	me := c.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": name, "password": "a-long-password-1"})
	if me.Status != http.StatusOK {
		e.t.Fatalf("accept invite: %d %v", me.Status, me.Body)
	}
	return c, userID(e.finishSignIn(c, email, me.Body))
}

// DECISIONS "Where work runs": each person picks their own computer; only an admin shares a
// computer, and only one signed in with an API key; sign-ins are set per computer by its owner.
func TestComputersAndSharing(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Computers Co")
	org := currentOrg(me)
	laptop := e.addRunner(org, userID(me), "Riley’s laptop", "computer")
	server := e.addRunner(org, userID(me), "Team server", "server")
	bob, _ := e.joinOrg(owner, org, "Bob")

	if r := owner.do("PUT", "/api/v1/orgs/"+org+"/my-computer", map[string]any{"runner_id": laptop}); r.Status != http.StatusOK {
		t.Fatalf("pick own computer: %d %v", r.Status, r.Body)
	}
	if r := bob.do("PUT", "/api/v1/orgs/"+org+"/my-computer", map[string]any{"runner_id": laptop}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("Bob picked Riley’s laptop: %d", r.Status)
	}
	if r := owner.do("PATCH", "/api/v1/runners/"+server, map[string]any{"shared": true}); r.Status != http.StatusUnprocessableEntity || r.field("shared") == "" {
		t.Errorf("shared without an API key: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PUT", "/api/v1/runners/"+server+"/connections/claude", map[string]any{"enabled": true, "mode": "api_key", "cap_tokens_per_day": 1000000, "api_key": "sk-ant-secret-4321"}); r.Status != http.StatusOK {
		t.Fatalf("server sign-in: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PATCH", "/api/v1/runners/"+server, map[string]any{"shared": true}); r.Status != http.StatusOK {
		t.Fatalf("share: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PUT", "/api/v1/runners/"+server+"/connections/claude", map[string]any{"enabled": true, "mode": "subscription"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a shared computer took a subscription: %d", r.Status)
	}
	if r := bob.do("PUT", "/api/v1/orgs/"+org+"/my-computer", map[string]any{"runner_id": server}); r.Status != http.StatusOK {
		t.Errorf("Bob picks the shared computer: %d %v", r.Status, r.Body)
	}
	if r := bob.do("PUT", "/api/v1/runners/"+laptop+"/connections/claude", map[string]any{"enabled": true, "mode": "subscription"}); r.Status != http.StatusNotFound {
		t.Errorf("Bob changed Riley’s sign-in: %d", r.Status)
	}
	list := owner.do("GET", "/api/v1/orgs/"+org+"/runners", nil)
	if str(list.Body["my_computer"]) != laptop {
		t.Errorf("my computer: %v", list.Body["my_computer"])
	}
	var keys int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM cli_connections WHERE key_fingerprint LIKE '%secret%'`).Scan(&keys)
	if keys != 0 {
		t.Fatal("an API key reached the database")
	}

	// Someone from another organization sees nothing.
	stranger, _ := e.signup("Other Co")
	if r := stranger.do("PATCH", "/api/v1/runners/"+laptop, map[string]any{"name": "Mine now"}); r.Status != http.StatusNotFound {
		t.Errorf("stranger renamed a computer: %d", r.Status)
	}
}

// DECISIONS "Workspace knowledge": notes live in the hub; anyone in the workspace reads them,
// their author or an admin changes them.
func TestWorkspaceNotes(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Notes Co")
	org := currentOrg(me)
	ws, _ := e.createWorkspace(owner, org, setupPayload())
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff"})

	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/notes", map[string]any{"title": "Use ISO dates", "body": "Exports use ISO 8601.", "kind": "decision"})
	if r.Status != http.StatusCreated {
		t.Fatalf("create note: %d %v", r.Status, r.Body)
	}
	note := str(r.Body["note"].(map[string]any)["id"])
	if r := owner.do("POST", "/api/v1/workspaces/"+ws+"/notes", map[string]any{"title": " "}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("note without a title: %d", r.Status)
	}
	if notes := bob.do("GET", "/api/v1/workspaces/"+ws+"/notes", nil).Body["notes"].([]any); len(notes) != 1 {
		t.Errorf("Bob reads notes: %v", notes)
	}
	if r := bob.do("PATCH", "/api/v1/notes/"+note, map[string]any{"body": "No."}); r.Status != http.StatusForbidden {
		t.Errorf("Bob changed someone else’s note: %d", r.Status)
	}
	stranger, _ := e.signup("Strangers")
	if r := stranger.do("GET", "/api/v1/workspaces/"+ws+"/notes", nil); r.Status != http.StatusNotFound {
		t.Errorf("stranger listed notes: %d", r.Status)
	}
	if r := owner.do("DELETE", "/api/v1/notes/"+note, nil); r.Status != http.StatusNoContent {
		t.Errorf("delete note: %d", r.Status)
	}
}

// Goals carry where they run and their base; only their person (or an admin) ships or moves
// them, onto a computer that person may use.
func TestGoalComputerAndLifecycle(t *testing.T) {
	e := newEnv(t)
	e.srv.Orch = orch.New(e.srv.Core, offline{}, nil)
	owner, me := e.signup("Goals Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	laptop := e.addRunner(org, userID(me), "Riley’s laptop", "computer")
	bob, bobID := e.joinOrg(owner, org, "Bob")
	bobPC := e.addRunner(org, bobID, "Bob’s laptop", "computer")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff", "manager_ids": []string{riley}})

	if r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "runner_id": bobPC}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("goal on someone else’s computer: %d", r.Status)
	}
	if r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "base": "bad name"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("bad base branch: %d", r.Status)
	}
	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "runner_id": laptop, "base": "feat/export-ui", "snapshot": true})
	if r.Status != http.StatusCreated {
		t.Fatalf("give goal: %d %v", r.Status, r.Body)
	}
	g := r.Body["goal"].(map[string]any)
	goal := str(g["id"])
	if str(g["branch"]) != "canopy/csv-export" || str(g["base"]) != "feat/export-ui" || g["snapshot"] != true || str(g["runner_id"]) != laptop {
		t.Errorf("goal: %v", g)
	}
	// A second goal with the same title gets its own branch.
	r2 := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export"})
	if b := str(r2.Body["goal"].(map[string]any)["branch"]); b != "canopy/csv-export-2" {
		t.Errorf("second branch: %q", b)
	}

	if r := owner.do("POST", "/api/v1/goals/"+goal+"/done", nil); r.Status != http.StatusConflict {
		t.Errorf("done while planning: %d", r.Status)
	}
	if r := owner.do("POST", "/api/v1/goals/"+goal+"/ship", map[string]any{"action": "pr"}); r.Status != http.StatusConflict {
		t.Errorf("ship while planning: %d", r.Status)
	}
	if r := bob.do("POST", "/api/v1/goals/"+goal+"/move", map[string]any{"runner_id": bobPC}); r.Status != http.StatusForbidden {
		t.Errorf("Bob moved Riley’s goal: %d", r.Status)
	}
	if r := owner.do("POST", "/api/v1/goals/"+goal+"/move", map[string]any{"runner_id": bobPC}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("moved onto Bob’s computer: %d %v", r.Status, r.Body)
	}
	stranger, _ := e.signup("Strangers")
	if r := stranger.do("POST", "/api/v1/goals/"+goal+"/ship", map[string]any{"action": "pr"}); r.Status != http.StatusNotFound {
		t.Errorf("stranger shipped: %d", r.Status)
	}
}

// The header shows Claude Code's own limit reports for the caller's computers, never Canopy's limits.
func TestClaudeUsagePerComputer(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Claude Usage Co")
	org := currentOrg(me)
	ctx := context.Background()
	orgID, ownerID := uuid.MustParse(org), uuid.MustParse(userID(me))
	laptop := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	desktop := uuid.MustParse(e.addRunner(org, userID(me), "Desktop", "computer"))
	bob, bobID := e.joinOrg(owner, org, "Bob")
	e.addRunner(org, bobID, "Bob’s laptop", "computer")

	for _, rn := range []uuid.UUID{laptop, desktop} {
		if _, err := e.store.UpsertCliConnection(ctx, db.UpsertCliConnectionParams{ID: uuid.Must(uuid.NewV7()), OrgID: orgID, RunnerID: rn, Cli: "claude", Mode: "subscription", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.EnsureAccount(ctx, db.EnsureAccountParams{ID: uuid.Must(uuid.NewV7()), OrgID: orgID, RunnerID: rn, Cli: "claude", Mode: "subscription"}); err != nil {
			t.Fatal(err)
		}
	}
	_ = ownerID
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	util, _ := json.Marshal(map[string]any{
		"five_hour": 0.37, "reported_at": time.Now().UTC(),
		"windows": map[string]any{
			"seven_day": map[string]any{"utilization": 0.47, "status": "allowed"},
			"five_hour": map[string]any{"utilization": 0.37, "resets_at": reset, "status": "allowed_warning"},
		}})
	if _, err := e.store.UpdateAccountState(ctx, db.UpdateAccountStateParams{RunnerID: laptop, Cli: "claude", Utilization: util}); err != nil {
		t.Fatal(err)
	}

	r := owner.do("GET", "/api/v1/orgs/"+org+"/usage/claude", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("usage: %d %v", r.Status, r.Body)
	}
	byName := map[string]map[string]any{}
	for _, c := range r.Body["computers"].([]any) {
		m := c.(map[string]any)
		byName[str(m["name"])] = m
	}
	if len(byName) != 2 {
		t.Fatalf("only the caller's computers: %v", byName)
	}
	if byName["Desktop"]["reported"] != false || len(byName["Desktop"]["windows"].([]any)) != 0 {
		t.Errorf("no report yet must not read as 0%%: %v", byName["Desktop"])
	}
	l := byName["Laptop"]
	ws, _ := l["windows"].([]any)
	if l["reported"] != true || l["sign_in"] != "subscription" || l["reported_at"] == nil || len(ws) != 2 {
		t.Fatalf("laptop: %v", l)
	}
	if w := ws[0].(map[string]any); w["key"] != "five_hour" || w["used_percent"].(float64) < 36.9 || w["used_percent"].(float64) > 37.1 ||
		w["status"] != "allowed_warning" || str(w["resets_at"]) != reset.Format(time.RFC3339) {
		t.Errorf("five_hour: %v", w)
	}
	if w := ws[1].(map[string]any); w["key"] != "seven_day" || w["resets_at"] != nil {
		t.Errorf("seven_day: %v", w)
	}
	if r := bob.do("GET", "/api/v1/orgs/"+org+"/usage/claude", nil); len(r.Body["computers"].([]any)) != 1 {
		t.Errorf("Bob sees only his own computer: %v", r.Body)
	}
}
