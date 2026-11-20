package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/notify"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
)

// SPEC permissions table: only the owner deletes the organization or transfers ownership.
func TestOwnerActions(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Owner Co")
	org := currentOrg(me)
	bob, bobID := e.joinOrg(owner, org, "Bob")
	outsider, _ := e.signup("Elsewhere")

	if r := bob.do("POST", "/api/v1/orgs/"+org+"/transfer", map[string]any{"user_id": bobID}); r.Status != http.StatusForbidden {
		t.Errorf("a member transferred ownership: %d", r.Status)
	}
	if r := outsider.do("POST", "/api/v1/orgs/"+org+"/transfer", map[string]any{"user_id": bobID}); r.Status != http.StatusNotFound {
		t.Errorf("an outsider saw the organization: %d", r.Status)
	}
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/transfer", map[string]any{"user_id": uuid.NewString()}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("transfer to a stranger: %d", r.Status)
	}
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/transfer", map[string]any{"user_id": bobID}); r.Status != http.StatusNoContent {
		t.Fatalf("transfer: %d %v", r.Status, r.Body)
	}
	if r := owner.do("GET", "/api/v1/orgs/"+org, nil); str(r.Body["role"]) != "admin" {
		t.Errorf("old owner's role: %v", r.Body["role"])
	}
	if r := bob.do("GET", "/api/v1/orgs/"+org, nil); str(r.Body["role"]) != "owner" {
		t.Errorf("new owner's role: %v", r.Body["role"])
	}

	if r := owner.do("DELETE", "/api/v1/orgs/"+org, map[string]any{"confirm": "Owner Co"}); r.Status != http.StatusForbidden {
		t.Errorf("an admin deleted the organization: %d", r.Status)
	}
	if r := bob.do("DELETE", "/api/v1/orgs/"+org, map[string]any{"confirm": "owner co"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("delete with the wrong name: %d", r.Status)
	}
	if r := bob.do("DELETE", "/api/v1/orgs/"+org, map[string]any{"confirm": "Owner Co"}); r.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %v", r.Status, r.Body)
	}
	if r := bob.do("GET", "/api/v1/orgs/"+org, nil); r.Status != http.StatusNotFound {
		t.Errorf("deleted organization still there: %d", r.Status)
	}
	if r := owner.do("GET", "/api/v1/me", nil); r.Status != http.StatusOK || len(r.Body["organizations"].([]any)) != 0 {
		t.Errorf("old owner still lists it: %v", r.Body["organizations"])
	}
}

func TestNotificationChoicesAndLogoutEverywhere(t *testing.T) {
	e := newEnv(t)
	c, me := e.signup("Prefs Co")
	prefs := me["user"].(map[string]any)["notifications"].(map[string]any)
	if q := prefs["questions"].(map[string]any); q["email"] != true || q["desktop"] != true {
		t.Errorf("question defaults: %v", q)
	}
	if d := prefs["daily"].(map[string]any); d["email"] != false {
		t.Errorf("daily default: %v", d)
	}
	r := c.do("PATCH", "/api/v1/me", map[string]any{"notifications": map[string]any{"daily": map[string]any{"desktop": false, "email": true}}})
	if r.Status != http.StatusOK {
		t.Fatalf("save choices: %d %v", r.Status, r.Body)
	}
	prefs = r.Body["user"].(map[string]any)["notifications"].(map[string]any)
	if prefs["daily"].(map[string]any)["email"] != true || prefs["errors"].(map[string]any)["email"] != true {
		t.Errorf("after saving one row: %v", prefs)
	}
	if r := c.do("PATCH", "/api/v1/me", map[string]any{"notifications": map[string]any{"weekly": map[string]any{"email": true}}}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("unknown row: %d", r.Status)
	}

	// The same person signed in twice; logging out everywhere ends both.
	other := e.client()
	if r := other.do("POST", "/api/v1/auth/login", map[string]any{"email": userEmail(me), "password": "correct horse"}); r.Status != http.StatusOK {
		t.Fatalf("second sign-in: %d", r.Status)
	}
	if r := c.do("POST", "/api/v1/auth/logout", map[string]any{"everywhere": true}); r.Status != http.StatusNoContent {
		t.Fatalf("logout everywhere: %d %v", r.Status, r.Body)
	}
	if r := other.do("GET", "/api/v1/me", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("other session survived: %d", r.Status)
	}
}

// Questions still open after a short delay are emailed to the person they're for, once; the
// daily summary goes to people who asked for it.
func TestNotificationEmails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, me := e.signup("Mail Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	uid := uuid.MustParse(userID(me))
	rileyID := uuid.MustParse(riley)
	ask := func(question string) {
		_, err := e.store.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: uuid.MustParse(ws), Type: "question",
			MemberID: &rileyID, Payload: []byte(`{"question":"` + question + `","options":["Yes","No"]}`), ForUserID: &uid})
		if err != nil {
			t.Fatal(err)
		}
	}
	n := notify.New(e.store, e.mail, "http://canopy.test")
	later := func() time.Time { return time.Now().Add(5 * time.Minute) }
	sentTo := func(subject string) int {
		k := 0
		for _, m := range e.mail.Sent {
			if m.To == userEmail(me) && strings.Contains(m.Subject, subject) {
				k++
			}
		}
		return k
	}

	ask("Ship on Friday?")
	n.Pass(ctx) // too soon: people on the board answer right away
	if sentTo("has a question") != 0 {
		t.Fatal("emailed before the delay")
	}
	n.Now = later
	n.Pass(ctx)
	n.Pass(ctx)
	if sentTo("Riley has a question in Payments") != 1 {
		t.Fatalf("question emails: %v", e.mail.Sent)
	}

	owner.do("PATCH", "/api/v1/me", map[string]any{"notifications": map[string]any{"questions": map[string]any{"desktop": true, "email": false}}})
	ask("Use Postgres?")
	n.Pass(ctx)
	if sentTo("has a question") != 1 {
		t.Error("emailed with question emails turned off")
	}

	// Daily summary: something moved, so it goes out once.
	e.srv.Orch = orch.New(e.srv.Core, offline{}, nil)
	owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "Faster checkout"})
	owner.do("PATCH", "/api/v1/me", map[string]any{"notifications": map[string]any{"daily": map[string]any{"desktop": false, "email": true}}})
	n.DailyHour = 0
	n.Pass(ctx)
	n.Pass(ctx)
	if sentTo("Your Canopy summary") != 1 {
		t.Fatalf("daily summaries: %d", sentTo("Your Canopy summary"))
	}
	for _, m := range e.mail.Sent {
		if strings.Contains(m.Subject, "summary") && (!strings.Contains(m.Text, "Faster checkout") || !strings.Contains(m.Text, "Use Postgres?")) {
			t.Errorf("summary text: %s", m.Text)
		}
	}
}

func TestRestartSessions(t *testing.T) {
	e := newEnv(t)
	e.srv.Orch = orch.New(e.srv.Core, offline{}, nil)
	owner, me := e.signup("Restart Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := uuid.MustParse(str(members(d)["Riley"]["id"]))
	ctx := context.Background()
	s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: uuid.MustParse(ws), MemberID: riley, Status: "asleep"})
	if err != nil {
		t.Fatal(err)
	}
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff", "manager_ids": []string{riley.String()}})
	if r := bob.do("POST", "/api/v1/workspaces/"+ws+"/restart-sessions", nil); r.Status != http.StatusForbidden {
		t.Errorf("staff restarted sessions: %d", r.Status)
	}
	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/restart-sessions", nil)
	if r.Status != http.StatusOK || r.Body["restarted"] != float64(1) {
		t.Fatalf("restart: %d %v", r.Status, r.Body)
	}
	if got, _ := e.store.GetAgentSession(ctx, s.ID); got.Status != "ended" {
		t.Errorf("session status: %s", got.Status)
	}
}
