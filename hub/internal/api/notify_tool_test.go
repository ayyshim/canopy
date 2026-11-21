package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

func TestNotifyTool(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Notify Tool Co")
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

	newGoal := func(title string) uuid.UUID {
		g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley.String(), "title": title, "draft": true})
		id := uuid.MustParse(str(g.Body["goal"].(map[string]any)["id"]))
		if _, err := e.store.UpdateGoal(ctx, db.UpdateGoalParams{ID: id, Status: ptr("active")}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newTask := func(goal uuid.UUID, member uuid.UUID, title string) uuid.UUID {
		r := owner.do("POST", "/api/v1/goals/"+goal.String()+"/tasks", map[string]any{"title": title, "member_id": member.String()})
		return uuid.MustParse(str(r.Body["task"].(map[string]any)["id"]))
	}
	session := func(member uuid.UUID, task *uuid.UUID) uuid.UUID {
		s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: member,
			Status: "waiting", RunnerID: &runnerID, PersonID: &uid, TaskID: task})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	output := func(task uuid.UUID, member uuid.UUID, name string) uuid.UUID {
		x, err := e.store.UpsertOutput(ctx, db.UpsertOutputParams{ID: uuid.New(), WorkspaceID: wsID, TaskID: &task, MemberID: &member, Kind: "image", Name: name, Title: name})
		if err != nil {
			t.Fatal(err)
		}
		return x.ID
	}
	notifyAs := func(sess uuid.UUID, args map[string]any) protocol.ToolResult {
		return call(t, o, rn, rec, sess, protocol.ToolNotify, args)
	}

	goal, other := newGoal("CSV export"), newGoal("Other goal")
	taskA, taskB := newTask(goal, dev, "Add column"), newTask(goal, riley, "Review it")
	taskOther := newTask(other, dev, "Elsewhere")
	shot, plan := output(taskA, dev, "shot.png"), output(taskB, riley, "plan.md")
	output(taskOther, dev, "secret.png")
	sess := session(dev, &taskA)

	// A milestone goes to the goal's person under the member's name, with its file and link.
	res := notifyAs(sess, map[string]any{"message": "First working build.", "level": "milestone", "files": []string{"shot.png", "plan.md"},
		"links": []string{"https://claude.ai/artifact/abc123"}})
	if res.IsError {
		t.Fatalf("notify: %+v", res)
	}
	rows := e.outbox(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[0]
	if r.Kind != "progress" || r.Title != "Payments - Dev" || r.Body != "First working build." || r.GoalID == nil || *r.GoalID != goal || r.TaskID == nil || *r.TaskID != taskA {
		t.Errorf("row: %+v", r)
	}
	if len(r.Recipients) != 1 || r.Recipients[0] != uid {
		t.Errorf("recipients: %v", r.Recipients)
	}
	m := mediaOf(t, r)
	if !m.Agent || m.Level != "milestone" || len(m.OutputIDs) != 2 || m.OutputIDs[0] != shot || m.OutputIDs[1] != plan || len(m.Links) != 1 || m.Links[0].URL != "https://claude.ai/artifact/abc123" {
		t.Errorf("media: %+v", m)
	}

	// Blocked: it counts as an error notice. Managers can send too.
	if res := notifyAs(sess, map[string]any{"message": "Need an API key from IT.", "level": "needs_attention"}); res.IsError {
		t.Fatalf("needs_attention: %+v", res)
	}
	if got := e.outbox(t, wsID); got[1].Kind != "errors" {
		t.Errorf("needs_attention kind: %s", got[1].Kind)
	}
	if res := notifyAs(session(riley, &taskB), map[string]any{"message": "Review done."}); res.IsError {
		t.Errorf("manager notify: %+v", res)
	}

	// Bad input is refused and queues nothing.
	before := len(e.outbox(t, wsID))
	for name, args := range map[string]map[string]any{
		"empty":       {"message": "  "},
		"level":       {"message": "x", "level": "urgent"},
		"http link":   {"message": "x", "links": []string{"http://example.com"}},
		"script link": {"message": "x", "links": []string{"javascript:alert(1)"}},
		"other goal":  {"message": "x", "files": []string{"secret.png"}},
		"unknown":     {"message": "x", "files": []string{"nope.png"}},
		"many links":  {"message": "x", "links": []string{"https://a.io", "https://b.io", "https://c.io", "https://d.io", "https://e.io", "https://f.io"}},
		"long":        {"message": strings.Repeat("a", 1001)},
	} {
		if res := notifyAs(sess, args); !res.IsError {
			t.Errorf("%s: accepted", name)
		} else if name == "other goal" && !strings.Contains(res.Text, "secret.png") {
			t.Errorf("other goal text: %s", res.Text)
		}
	}
	if n := len(e.outbox(t, wsID)); n != before {
		t.Fatalf("refused calls queued %d rows", n-before)
	}
	if res := notifyAs(sess, map[string]any{"message": "Link", "links": []string{"https://example.com/page"}}); res.IsError {
		t.Errorf("https link: %+v", res)
	}

	// 5 per task per hour: taskA has sent 3 so far (milestone, blocked, https link).
	for _, msg := range []string{"Fourth", "Fifth"} {
		if res := notifyAs(sess, map[string]any{"message": msg}); res.IsError {
			t.Fatalf("%s: %+v", msg, res)
		}
	}
	if res := notifyAs(sess, map[string]any{"message": "Sixth"}); !res.IsError || !strings.Contains(res.Text, "5 notifications for this task") {
		t.Errorf("sixth: %+v", res)
	}
	// Older messages stop counting after an hour.
	if _, err := e.store.Pool.Exec(ctx, `UPDATE notification_outbox SET created_at = now() - interval '2 hours' WHERE task_id = $1`, taskA); err != nil {
		t.Fatal(err)
	}
	if res := notifyAs(sess, map[string]any{"message": "After an hour"}); res.IsError {
		t.Errorf("after the hour: %+v", res)
	}

	// 20 per member per day across tasks: fill the day with other tasks, then the next is refused.
	var n int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE member_id = $1 AND media @> '{"agent": true}' AND created_at > now() - interval '1 day'`, dev).Scan(&n)
	for i := 0; n < 20; i++ {
		tk := newTask(other, dev, "Filler")
		for j := 0; j < 5 && n < 20; j++ {
			if res := notifyAs(session(dev, &tk), map[string]any{"message": "More"}); res.IsError {
				t.Fatalf("filler %d: %+v", n, res)
			}
			n++
		}
	}
	if res := notifyAs(session(dev, &taskOther), map[string]any{"message": "Too many"}); !res.IsError || !strings.Contains(res.Text, "20 notifications") {
		t.Errorf("daily limit: %+v", res)
	}
	// Another member is unaffected.
	if res := notifyAs(session(riley, &taskB), map[string]any{"message": "Still fine"}); res.IsError {
		t.Errorf("other member: %+v", res)
	}

	// Both kinds of agent are told when to use it, and when not to.
	for _, id := range []uuid.UUID{dev, riley} {
		p, err := o.MemberPrompt(ctx, id)
		if err != nil || !strings.Contains(p, "notify(message, level, files, links)") || !strings.Contains(p, "Don't use it for routine steps") {
			t.Errorf("prompt: %v\n%s", err, p)
		}
	}
}
