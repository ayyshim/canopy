package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

const goodHook = "https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz_ABCDEFGH-0123456789"

func (e *env) outbox(t *testing.T, ws uuid.UUID) []db.NotificationOutbox {
	t.Helper()
	rows, err := e.store.ListNotifications(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func mediaOf(t *testing.T, row db.NotificationOutbox) notify.Media {
	t.Helper()
	var m notify.Media
	if err := json.Unmarshal(row.Media, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Every kind of signal that needs a person queues a titled note for the right people.
func TestEnqueueSignals(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Outbox Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	dev := uuid.MustParse(str(members(d)["Dev"]["id"]))
	ctx := context.Background()
	w, _ := e.store.GetWorkspace(ctx, wsID)
	outID := uuid.New()

	cases := []struct {
		typ, kind string
		payload   map[string]any
		member    *uuid.UUID
		body      string
	}{
		{"question", "questions", map[string]any{"question": "Which table?"}, &dev, "Which table?"},
		{"gate", "questions", map[string]any{"summary": "rm -rf build"}, &dev, "rm -rf build"},
		{"dir_access", "questions", map[string]any{"path": "/src", "mode": "read"}, &dev, "/src"},
		{"review", "questions", map[string]any{"output_id": outID.String(), "title": "Mockup"}, &dev, "Mockup"},
		{"owner_change", "questions", map[string]any{"path": "/api", "description": "Edit it", "owner_user_id": uid}, &dev, "/api"},
		{"sync_conflict", "questions", map[string]any{"path": "a.txt", "owner_user_id": uid}, nil, "a.txt"},
		{"usage_limit", "questions", map[string]any{}, nil, "usage limit"},
		{"cap", "questions", map[string]any{"level": 90}, nil, "90%"},
		{"error", "errors", map[string]any{"message": "boom"}, &dev, "boom"},
		{"goal_ready", "goal_ready", map[string]any{"title": "CSV export"}, &dev, "CSV export"},
	}
	for _, c := range cases {
		err := e.srv.Core.Tx(ctx, func(tx *core.Tx) error {
			raw, _ := json.Marshal(c.payload)
			sig, err := tx.Q.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.New(), WorkspaceID: wsID, Type: c.typ, MemberID: c.member, Payload: raw})
			if err != nil {
				return err
			}
			return notify.EnqueueSignal(ctx, tx, w, sig)
		})
		if err != nil {
			t.Fatalf("%s: %v", c.typ, err)
		}
	}
	rows := e.outbox(t, wsID)
	if len(rows) != len(cases) {
		t.Fatalf("rows: %d, want %d", len(rows), len(cases))
	}
	for i, c := range cases {
		r := rows[i]
		want := "Payments - Canopy"
		if c.member != nil {
			want = "Payments - Dev"
		}
		if r.Kind != c.kind || r.Title != want || !strings.Contains(r.Body, c.body) {
			t.Errorf("%s: kind %q title %q body %q", c.typ, r.Kind, r.Title, r.Body)
		}
		if r.SignalID == nil || !strings.Contains(r.Link, "/w/"+ws+"?") || !strings.Contains(r.Link, "signal="+r.SignalID.String()) {
			t.Errorf("%s: link %q", c.typ, r.Link)
		}
		if len(r.Recipients) != 1 || r.Recipients[0] != uid {
			t.Errorf("%s: recipients %v", c.typ, r.Recipients)
		}
	}
	if m := mediaOf(t, rows[3]); len(m.OutputIDs) != 1 || m.OutputIDs[0] != outID {
		t.Errorf("review media: %+v", m)
	}
}

func TestTitleFormat(t *testing.T) {
	if got := notify.Title("Payments", "Riley"); got != "Payments - Riley" {
		t.Error(got)
	}
	if got := notify.Title("Payments", " "); got != "Payments - Canopy" {
		t.Error(got)
	}
	links := notify.ArtifactLinks("Done: https://claude.ai/artifact/abc_123 and https://claude.ai/code/artifact/0f-9. Not https://example.com/artifact/x, again https://claude.ai/artifact/abc_123")
	if len(links) != 2 || links[0].URL != "https://claude.ai/artifact/abc_123" || links[1].URL != "https://claude.ai/code/artifact/0f-9" {
		t.Errorf("links: %+v", links)
	}
}

// Preferences: progress is off, push follows desktop until set.
func TestPrefsPush(t *testing.T) {
	p := notify.Parse([]byte(`{"questions":{"desktop":false,"email":true},"errors":{"desktop":true,"email":false,"push":false}}`))
	if p["questions"].Push || !p["questions"].Email {
		t.Errorf("questions: %+v", p["questions"])
	}
	if p["errors"].Push {
		t.Errorf("errors push was set off: %+v", p["errors"])
	}
	if d := notify.Parse(nil); d["progress"].Push || d["progress"].Desktop || !d["goal_ready"].Push {
		t.Errorf("defaults: %+v", d)
	}
	if got := notify.Parse([]byte(`{"progress":{"desktop":true,"email":false}}`))["progress"]; got.Push {
		t.Errorf("progress push should stay off: %+v", got)
	}
}

// A finished task, a submitted plan and a staff draft each queue a note.
func TestEnqueueFromTools(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Tools Outbox Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley, dev := uuid.MustParse(str(members(d)["Riley"]["id"])), uuid.MustParse(str(members(d)["Dev"]["id"]))
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	ctx := context.Background()
	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	rn, _ := e.store.GetRunner(ctx, runnerID)
	rec := &recorder{}
	o := orch.New(e.srv.Core, rec, nil)
	e.srv.Orch = o

	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley.String(), "title": "CSV export", "draft": true})
	goalID := uuid.MustParse(str(g.Body["goal"].(map[string]any)["id"]))
	if _, err := e.store.UpdateGoal(ctx, db.UpdateGoalParams{ID: goalID, Status: ptr("active")}); err != nil {
		t.Fatal(err)
	}
	r := owner.do("POST", "/api/v1/goals/"+goalID.String()+"/tasks", map[string]any{"title": "Add column", "member_id": dev.String()})
	taskID := uuid.MustParse(str(r.Body["task"].(map[string]any)["id"]))
	session := func(member uuid.UUID, task *uuid.UUID) uuid.UUID {
		s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: member,
			Status: "waiting", RunnerID: &runnerID, PersonID: &uid, TaskID: task})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}

	summary := "Added it. Preview: https://claude.ai/artifact/xyz789"
	if res := call(t, o, rn, rec, session(dev, &taskID), protocol.ToolCompleteTask, map[string]any{"summary": summary}); res.IsError {
		t.Fatalf("complete_task: %+v", res)
	}
	rows := e.outbox(t, wsID)
	if len(rows) != 1 || rows[0].Kind != "progress" || rows[0].Title != "Payments - Dev" || !strings.Contains(rows[0].Body, "Added it.") {
		t.Fatalf("task done rows: %+v", rows)
	}
	if rows[0].TaskID == nil || *rows[0].TaskID != taskID || rows[0].GoalID == nil || *rows[0].GoalID != goalID {
		t.Errorf("ids: %+v", rows[0])
	}
	if m := mediaOf(t, rows[0]); len(m.Links) != 1 || m.Links[0].URL != "https://claude.ai/artifact/xyz789" {
		t.Errorf("media: %+v", m)
	}
	if len(rows[0].Recipients) != 1 || rows[0].Recipients[0] != uid {
		t.Errorf("recipients: %v", rows[0].Recipients)
	}

	// A manager's staff draft goes to the workspace admins.
	args := map[string]any{"name": "Tess", "role": "reviewer", "instructions": "Review pull requests."}
	if res := call(t, o, rn, rec, session(riley, nil), protocol.ToolProposeStaff, args); res.IsError {
		t.Fatalf("propose_staff: %+v", res)
	}
	rows = e.outbox(t, wsID)
	if len(rows) != 2 || rows[1].Kind != "questions" || rows[1].Title != "Payments - Riley" || !strings.Contains(rows[1].Body, "Tess") {
		t.Fatalf("draft rows: %+v", rows)
	}
}

func TestDispatcherRetries(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Dispatch Co")
	ws, _ := e.createWorkspace(owner, currentOrg(me), setupPayload())
	wsID := uuid.MustParse(ws)
	ctx := context.Background()
	// The test database is shared; drop other tests' rows so one batch reaches ours.
	if _, err := e.store.Pool.Exec(ctx, `DELETE FROM notification_outbox WHERE workspace_id <> $1`, wsID); err != nil {
		t.Fatal(err)
	}
	w, _ := e.store.GetWorkspace(ctx, wsID)
	for i := 0; i < 2; i++ {
		if err := e.srv.Core.Tx(ctx, func(tx *core.Tx) error {
			return notify.Enqueue(ctx, tx, w, notify.Note{Kind: "progress", Body: "hello"})
		}); err != nil {
			t.Fatal(err)
		}
	}
	rows := e.outbox(t, wsID)
	bad := rows[0].ID
	now := time.Now()
	d := notify.NewDispatcher(e.store)
	d.Now = func() time.Time { return now }
	d.Backoff = time.Minute
	d.Discord = chanFunc(func(r db.NotificationOutbox) error {
		if r.ID == bad {
			return errors.New("discord is down")
		}
		return nil
	})
	d.Pass(ctx)
	got := map[uuid.UUID]db.NotificationOutbox{}
	for _, r := range e.outbox(t, wsID) {
		got[r.ID] = r
	}
	if r := got[rows[1].ID]; r.DiscordSentAt == nil || r.PushSentAt == nil {
		t.Errorf("good row: %+v", r)
	}
	if r := got[bad]; r.DiscordSentAt != nil || r.DiscordAttempts != 1 || r.DiscordLastError != "discord is down" || r.PushSentAt == nil {
		t.Errorf("bad row: %+v", r)
	}
	// Not due yet: a second pass leaves it alone; later passes retry until the fifth failure.
	d.Pass(ctx)
	if r := e.outbox(t, wsID)[0]; r.DiscordAttempts != 1 {
		t.Errorf("retried too soon: %d", r.DiscordAttempts)
	}
	for i := 0; i < 8; i++ {
		now = now.Add(time.Hour * 24)
		d.Pass(ctx)
	}
	if r := e.outbox(t, wsID)[0]; r.DiscordAttempts != 5 || r.DiscordSentAt != nil {
		t.Errorf("after retries: %+v", r)
	}
	old := time.Now().Add(time.Hour)
	if n, err := e.store.DeleteOldNotifications(ctx, old); err != nil || n < 2 {
		t.Errorf("delete old: %d %v", n, err)
	}
}

type chanFunc func(db.NotificationOutbox) error

func (f chanFunc) Send(_ context.Context, r db.NotificationOutbox) error { return f(r) }

func TestWebhookURLCheck(t *testing.T) {
	ok := []string{goodHook, strings.Replace(goodHook, "discord.com", "ptb.discord.com", 1),
		strings.Replace(goodHook, "discord.com", "canary.discord.com", 1), strings.Replace(goodHook, "discord.com", "discordapp.com", 1)}
	for _, u := range ok {
		if err := notify.ValidateWebhook(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	bad := []string{"", "http://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz", "https://evil.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz",
		"https://discord.com.evil.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz", "https://discord.com@evil.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz",
		"https://discord.com:8443/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz", "https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz?x=1",
		"https://discord.com/api/webhooks/abc/abcdefghijklmnopqrstuvwxyz", "https://localhost/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz",
		"https://evildiscord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz", "https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz/../x"}
	for _, u := range bad {
		if notify.ValidateWebhook(u) == nil {
			t.Errorf("accepted %q", u)
		}
	}
}

func TestDiscordSettingsAPI(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Discord Co")
	org := currentOrg(me)
	ws, _ := e.createWorkspace(owner, org, setupPayload())
	path := "/api/v1/workspaces/" + ws + "/discord"
	outsider, _ := e.signup("Other Co")

	// A staff member who can see the workspace gets 404, like a stranger.
	r := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": uniqueEmail("staff"), "role": "member",
		"workspace_grants": []any{map[string]any{"workspace_id": ws, "role": "staff", "manager_ids": []string{}}}})
	token := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	staff := e.client()
	if acc := staff.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "Sam", "password": "staff password"}); acc.Status != 200 {
		t.Fatalf("accept: %d", acc.Status)
	}
	for name, c := range map[string]*client{"staff": staff, "outsider": outsider} {
		for _, m := range []string{"GET", "PUT", "DELETE"} {
			if got := c.do(m, path, map[string]any{"webhook": goodHook}).Status; got != http.StatusNotFound {
				t.Errorf("%s %s: %d", name, m, got)
			}
		}
		if got := c.do("POST", path+"/test", nil).Status; got != http.StatusNotFound {
			t.Errorf("%s test: %d", name, got)
		}
	}

	if got := owner.do("GET", path, nil); got.Status != 200 || got.Body["discord"].(map[string]any)["configured"] != false {
		t.Fatalf("empty: %d %v", got.Status, got.Body)
	}
	if got := owner.do("POST", path+"/test", nil).Status; got != http.StatusBadRequest {
		t.Errorf("test with no webhook: %d", got)
	}
	for _, u := range []string{"https://evil.example/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz", "http://discord.com/x", ""} {
		if got := owner.do("PUT", path, map[string]any{"webhook": u}).Status; got != http.StatusUnprocessableEntity && got != http.StatusBadRequest {
			t.Errorf("bad url %q: %d", u, got)
		}
	}
	if got := owner.do("PUT", path, map[string]any{"webhook": goodHook, "role_id": "not-a-number"}).Status; got == 200 {
		t.Error("bad role accepted")
	}
	if got := owner.do("PUT", path, map[string]any{"webhook": goodHook, "kinds": map[string]bool{"nonsense": true}}).Status; got == 200 {
		t.Error("bad kind accepted")
	}

	put := owner.do("PUT", path, map[string]any{"webhook": goodHook, "kinds": map[string]bool{"progress": false}, "role_id": "987654321", "forum": true})
	if put.Status != 200 {
		t.Fatalf("put: %d %v", put.Status, put.Body)
	}
	for _, body := range []resp{put, owner.do("GET", path, nil)} {
		raw, _ := json.Marshal(body.Body)
		if strings.Contains(string(raw), "abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("token leaked: %s", raw)
		}
		v := body.Body["discord"].(map[string]any)
		kinds := v["kinds"].(map[string]any)
		if v["configured"] != true || !strings.HasPrefix(str(v["webhook"]), "https://discord.com/api/webhooks/123456789012345678/") || !strings.HasSuffix(str(v["webhook"]), "••••") ||
			kinds["progress"] != false || kinds["questions"] != true || v["role_id"] != "987654321" || v["forum"] != true {
			t.Errorf("view: %v", v)
		}
	}
	// The workspace payload never carries the webhook either.
	if raw, _ := json.Marshal(owner.do("GET", "/api/v1/workspaces/"+ws, nil).Body); strings.Contains(string(raw), "abcdefghijklmnopqrstuvwxyz") {
		t.Error("workspace view leaks the webhook")
	}
	// Saving other settings without a webhook keeps the saved one.
	if got := owner.do("PUT", path, map[string]any{"forum": false}); got.Status != 200 || got.Body["discord"].(map[string]any)["configured"] != true {
		t.Errorf("keep webhook: %d %v", got.Status, got.Body)
	}

	if got := owner.do("POST", path+"/test", nil).Status; got != http.StatusAccepted {
		t.Fatalf("test: %d", got)
	}
	rows := e.outbox(t, uuid.MustParse(ws))
	if len(rows) != 1 || rows[0].Title != "Payments - Canopy" || !mediaOf(t, rows[0]).Test {
		t.Errorf("test row: %+v", rows)
	}

	if got := owner.do("DELETE", path, nil); got.Status != 200 || got.Body["discord"].(map[string]any)["configured"] != false {
		t.Errorf("delete: %d %v", got.Status, got.Body)
	}
}
