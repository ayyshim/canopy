package api_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

type handoverEnv struct {
	t       *testing.T
	e       *env
	owner   *client
	o       *orch.Orch
	rec     *recorder
	rn      db.Runner
	blobs   blob.Dir
	ws      string
	wsID    uuid.UUID
	goal    uuid.UUID
	riley   uuid.UUID
	dev     uuid.UUID
	session uuid.UUID
}

func newHandoverEnv(t *testing.T, auto bool) *handoverEnv {
	e := newEnv(t)
	owner, me := e.signup("Handover Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	h := &handoverEnv{t: t, e: e, owner: owner, ws: ws, wsID: uuid.MustParse(ws)}
	h.riley, h.dev = uuid.MustParse(str(members(d)["Riley"]["id"])), uuid.MustParse(str(members(d)["Dev"]["id"]))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	var err error
	if h.rn, err = e.store.GetRunner(ctx, runnerID); err != nil {
		t.Fatal(err)
	}
	if auto {
		if _, err := e.store.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: h.wsID, GoalCompletion: ptr("auto")}); err != nil {
			t.Fatal(err)
		}
	}
	h.blobs = blob.Dir{Root: t.TempDir()}
	h.rec = &recorder{}
	h.o = orch.New(e.srv.Core, h.rec, h.blobs)
	h.o.Tick = time.Minute // passes run on kicks only
	e.srv.Orch = h.o
	e.srv.Resolver = h.o
	go h.o.Run(ctx)

	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": h.riley.String(), "title": "CSV export", "draft": true})
	h.goal = uuid.MustParse(str(g.Body["goal"].(map[string]any)["id"]))
	if _, err := e.store.UpdateGoal(ctx, db.UpdateGoalParams{ID: h.goal, Status: ptr("active")}); err != nil {
		t.Fatal(err)
	}
	h.addDoneWork("Add the column")
	return h
}

// addDoneWork puts one finished work task on the goal and wakes the scheduler.
func (h *handoverEnv) addDoneWork(title string) {
	ctx := context.Background()
	task, err := h.e.store.CreateTask(ctx, db.CreateTaskParams{ID: uuid.New(), WorkspaceID: h.wsID, GoalID: &h.goal, MemberID: &h.dev, Kind: "work",
		Title: title, Status: "todo", DependsOn: []uuid.UUID{}, Steps: []byte("[]"), RequiredDirs: []byte("[]")})
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.e.store.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: ptr("done"), Summary: ptr("Done")}); err != nil {
		h.t.Fatal(err)
	}
	h.o.Kick(ctx, h.wsID)
}

func (h *handoverEnv) goalNow() db.Goal {
	g, err := h.e.store.GetGoal(context.Background(), h.goal)
	if err != nil {
		h.t.Fatal(err)
	}
	return g
}

func (h *handoverEnv) waitGoal(what string, ok func(db.Goal) bool) db.Goal {
	h.t.Helper()
	for i := 0; i < 200; i++ {
		if g := h.goalNow(); ok(g) {
			return g
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatalf("goal never %s: %+v", what, h.goalNow())
	return db.Goal{}
}

func (h *handoverEnv) handoverTasks() (open *db.Task, all int) {
	tasks, _ := h.e.store.ListGoalTasks(context.Background(), &h.goal)
	for i, x := range tasks {
		if x.Kind == "handover" {
			all++
			if x.Status != "done" {
				open = &tasks[i]
			}
		}
	}
	return open, all
}

func (h *handoverEnv) waitHandover() db.Task {
	h.t.Helper()
	for i := 0; i < 200; i++ {
		if open, _ := h.handoverTasks(); open != nil {
			return *open
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatal("no handover task")
	return db.Task{}
}

// submit writes the brief and submits it for review as the manager; it returns the review signal.
func (h *handoverEnv) submit(task db.Task, text string) db.Signal {
	h.t.Helper()
	ctx := context.Background()
	if h.session == uuid.Nil {
		uid := uuid.MustParse(str(h.owner.do("GET", "/api/v1/me", nil).Body["user"].(map[string]any)["id"]))
		s, err := h.e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: h.wsID, MemberID: h.riley,
			Status: "running", RunnerID: &h.rn.ID, PersonID: &uid, TaskID: &task.ID})
		if err != nil {
			h.t.Fatal(err)
		}
		h.session = s.ID
	}
	key := "briefs/" + uuid.NewString()
	if err := h.blobs.Put(ctx, key, bytes.NewReader([]byte(text)), int64(len(text)), "text/markdown"); err != nil {
		h.t.Fatal(err)
	}
	go call(h.t, h.o, h.rn, h.rec, h.session, protocol.ToolSubmitOutput, map[string]any{"title": "Handover brief", "kind": "document", "needs_review": true,
		"name": "handover.md", "blob_key": key, "size": len(text), "mime": "text/markdown"})
	for i := 0; i < 200; i++ {
		sigs, _ := h.e.store.ListOpenSignals(ctx, h.wsID)
		for _, s := range sigs {
			if s.Type == "review" && s.TaskID != nil && *s.TaskID == task.ID {
				return s
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatal("no review signal")
	return db.Signal{}
}

func (h *handoverEnv) resolve(sig db.Signal, body map[string]any) {
	h.t.Helper()
	if r := h.owner.do("POST", "/api/v1/signals/"+sig.ID.String()+"/resolve", body); r.Status != http.StatusOK {
		h.t.Fatalf("resolve: %d %v", r.Status, r.Body)
	}
}

func TestHandoverBriefBeforeReady(t *testing.T) {
	h := newHandoverEnv(t, false)
	task := h.waitHandover()
	if task.Title != "Handover: CSV export" || task.MemberID == nil || *task.MemberID != h.riley {
		t.Fatalf("handover task: %+v", task)
	}
	// Still active, and a second pass doesn't add a second brief task.
	h.o.Kick(context.Background(), h.wsID)
	time.Sleep(300 * time.Millisecond)
	if g := h.goalNow(); g.Status != "active" || g.HandoverApprovedAt != nil {
		t.Fatalf("goal before approval: %+v", g)
	}
	if _, n := h.handoverTasks(); n != 1 {
		t.Fatalf("handover tasks: %d", n)
	}

	// Done and ship are refused without an approved brief.
	g := h.goalNow()
	if _, err := h.e.store.UpdateGoal(context.Background(), db.UpdateGoalParams{ID: g.ID, Status: ptr("ready")}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []resp{h.owner.do("POST", "/api/v1/goals/"+g.ID.String()+"/done", nil), h.owner.do("POST", "/api/v1/goals/"+g.ID.String()+"/ship", map[string]any{"action": "keep"})} {
		if r.Status != http.StatusConflict || !strings.Contains(toJSON(r.Body), "The handover brief isn’t approved yet.") {
			t.Errorf("without approval: %d %v", r.Status, r.Body)
		}
	}
	h.o.Kick(context.Background(), h.wsID)
	h.waitGoal("went back to active", func(g db.Goal) bool { return g.Status == "active" })

	// The manager can't finish the task before approval.
	sig := h.submit(task, "# Brief\n\nFirst draft.")
	if sig.ForUserID == nil || *sig.ForUserID != *g.CreatedBy {
		t.Errorf("review is for %v, want the goal's person", sig.ForUserID)
	}
	if res := call(t, h.o, h.rn, h.rec, h.session, protocol.ToolCompleteTask, map[string]any{"summary": "done"}); !res.IsError {
		t.Errorf("complete_task before approval: %+v", res)
	}

	// Changes go back to the manager, who resubmits; then the person approves.
	h.resolve(sig, map[string]any{"action": "changes", "note": "Say how to check it."})
	sig = h.submit(task, "# Brief\n\nSecond draft, with how to check.")
	if g := h.goalNow(); g.Status != "active" || g.HandoverApprovedAt != nil {
		t.Fatalf("goal after changes: %+v", g)
	}
	h.resolve(sig, map[string]any{"action": "approve"})
	g = h.waitGoal("ready", func(g db.Goal) bool { return g.Status == "ready" })
	if g.HandoverApprovedBy == nil || g.HandoverApprovedAt == nil || !strings.Contains(g.Summary, "Second draft") {
		t.Fatalf("approved goal: %+v", g)
	}
	if open, _ := h.handoverTasks(); open != nil {
		t.Errorf("handover task still open: %+v", open)
	}

	// Now the goal can be marked done.
	if r := h.owner.do("POST", "/api/v1/goals/"+g.ID.String()+"/done", nil); r.Status != http.StatusNoContent {
		t.Fatalf("done: %d %v", r.Status, r.Body)
	}

	// A new task reopens it and the old approval no longer counts.
	if r := h.owner.do("POST", "/api/v1/goals/"+g.ID.String()+"/tasks", map[string]any{"title": "One more thing", "member_id": h.dev.String()}); r.Status != http.StatusCreated {
		t.Fatalf("new task: %d %v", r.Status, r.Body)
	}
	if g := h.goalNow(); g.Status != "active" || g.HandoverApprovedAt != nil {
		t.Fatalf("reopened goal: %+v", g)
	}
	tasks, _ := h.e.store.ListGoalTasks(context.Background(), &h.goal)
	for _, x := range tasks {
		if x.Title != "One more thing" {
			continue
		}
		// The scheduler may be starting the task right now; set it done until it sticks.
		for i := 0; i < 20; i++ {
			if _, err := h.e.store.UpdateTask(context.Background(), db.UpdateTaskParams{ID: x.ID, Status: ptr("done")}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			if cur, _ := h.e.store.GetTask(context.Background(), x.ID); cur.Status == "done" {
				break
			}
		}
	}
	h.o.Kick(context.Background(), h.wsID)
	for i := 0; i < 200; i++ {
		if _, n := h.handoverTasks(); n == 2 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	tasks, _ = h.e.store.ListGoalTasks(context.Background(), &h.goal)
	for _, x := range tasks {
		t.Logf("task %s %s %s", x.Kind, x.Title, x.Status)
	}
	t.Fatalf("a reopened goal needs a fresh brief: %+v", h.goalNow())
}

func TestHandoverAutoMode(t *testing.T) {
	h := newHandoverEnv(t, true)
	task := h.waitHandover()
	time.Sleep(200 * time.Millisecond)
	if g := h.goalNow(); g.Status != "active" {
		t.Fatalf("auto mode closed the goal before approval: %s", g.Status)
	}
	h.resolve(h.submit(task, "# Brief\n\nAll good."), map[string]any{"action": "approve"})
	g := h.waitGoal("done", func(g db.Goal) bool { return g.Status == "done" })
	if g.HandoverApprovedAt == nil || !strings.Contains(g.Summary, "All good") {
		t.Fatalf("auto goal: %+v", g)
	}
}
