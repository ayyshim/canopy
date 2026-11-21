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

// A manager on a triage task hands work to staff; complete_task waits until they have.
func TestAssignTasks(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Assign Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley, dev := uuid.MustParse(str(members(d)["Riley"]["id"])), uuid.MustParse(str(members(d)["Dev"]["id"]))
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	ctx := context.Background()
	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	rn, err := e.store.GetRunner(ctx, runnerID)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	o := orch.New(e.srv.Core, rec, nil)
	e.srv.Orch = o

	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley.String(), "title": "CSV export", "draft": true})
	goalID := uuid.MustParse(str(g.Body["goal"].(map[string]any)["id"]))
	if _, err := e.store.UpdateGoal(ctx, db.UpdateGoalParams{ID: goalID, Status: ptr("active")}); err != nil {
		t.Fatal(err)
	}
	newTriage := func(title string) uuid.UUID {
		r := owner.do("POST", "/api/v1/goals/"+goalID.String()+"/tasks", map[string]any{"title": title, "member_id": riley.String()})
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
	triage := newTriage("Add a date column")
	mgrSess := session(riley, &triage)
	tasks := []map[string]any{{"key": "col", "title": "Add date column", "skill": "developer", "steps": []string{"Change the export"}}}

	// Staff can't assign, and the guard blocks completing before anything is assigned.
	if res := call(t, o, rn, rec, session(dev, nil), protocol.ToolAssignTasks, map[string]any{"tasks": tasks}); !res.IsError {
		t.Errorf("staff assign_tasks: %+v", res)
	}
	if res := call(t, o, rn, rec, mgrSess, protocol.ToolCompleteTask, map[string]any{"summary": "done"}); !res.IsError || !strings.Contains(res.Text, "assign_tasks") {
		t.Errorf("complete before assign: %+v", res)
	}
	// Bad tasks are refused.
	if res := call(t, o, rn, rec, mgrSess, protocol.ToolAssignTasks, map[string]any{"tasks": []map[string]any{{"key": "x", "title": "T", "skill": "wizard"}}}); !res.IsError {
		t.Errorf("bad skill accepted: %+v", res)
	}

	res := call(t, o, rn, rec, mgrSess, protocol.ToolAssignTasks, map[string]any{"tasks": tasks})
	if res.IsError {
		t.Fatalf("assign_tasks: %+v", res)
	}
	gt, _ := e.store.ListGoalTasks(ctx, &goalID)
	var made *db.Task
	for i, x := range gt {
		if x.ParentTaskID != nil && *x.ParentTaskID == triage {
			made = &gt[i]
		}
	}
	if made == nil || made.Kind != "work" || made.MemberID == nil || *made.MemberID != dev || made.Title != "Add date column" {
		t.Fatalf("new task: %+v", made)
	}
	note, err := e.store.GoalNote(ctx, db.GoalNoteParams{GoalID: &goalID, Kind: "plan"})
	if err != nil || !strings.Contains(note.Body, "Add date column") {
		t.Errorf("plan note: %v %+v", err, note)
	}
	events, _ := e.store.ListGoalEvents(ctx, goalID)
	found := false
	for _, ev := range events {
		found = found || ev.Type == "tasks_assigned"
	}
	if !found {
		t.Error("no goal event")
	}
	if res := call(t, o, rn, rec, mgrSess, protocol.ToolCompleteTask, map[string]any{"summary": "Handed to Dev"}); res.IsError {
		t.Errorf("complete after assign: %+v", res)
	}

	// With no work needed, a reason lets the task close.
	t2 := newTriage("Never mind")
	s2 := session(riley, &t2)
	if res := call(t, o, rn, rec, s2, protocol.ToolCompleteTask, map[string]any{"summary": "Nothing to do"}); !res.IsError {
		t.Errorf("complete without reason: %+v", res)
	}
	if res := call(t, o, rn, rec, s2, protocol.ToolCompleteTask, map[string]any{"summary": "Nothing to do", "no_work_reason": "Already shipped"}); res.IsError {
		t.Errorf("complete with reason: %+v", res)
	}
	// Plain work tasks are not guarded, and assign_tasks needs a triage task.
	if res := call(t, o, rn, rec, session(riley, nil), protocol.ToolAssignTasks, map[string]any{"tasks": tasks}); !res.IsError {
		t.Errorf("assign_tasks without triage task: %+v", res)
	}
}

func ptr[T any](v T) *T { return &v }
