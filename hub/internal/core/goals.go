package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"canopy/hub/internal/domain"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store/db"
)

// NewGoal is a goal a person gives to a manager.
type NewGoal struct {
	ManagerID   uuid.UUID
	Title       string
	DoneWhen    string
	Description string
	By          uuid.UUID
	RunnerID    *uuid.UUID  // where it runs; empty = the person's own computer, else a shared one
	Base        string      // branch to start from in its repositories; "" = their default branch
	Snapshot    bool        // include the person's uncommitted changes
	Draft       bool        // saved for later; nobody works on it yet
	JobRunID    *uuid.UUID  // set when a job run creates the goal
	Direct      *DirectWork // start active with this one work task and no plan task
}

// DirectWork is the single task of a goal that skips planning.
type DirectWork struct {
	MemberID     uuid.UUID
	Skill        *string
	Title        string
	Description  string
	RequiredDirs []byte // JSON, same shape as tasks.required_dirs; nil = none
}

// CreateGoal records a goal given to a manager (or a draft), with the next goal color,
// its own branch name and a "given" or "drafted" event.
func CreateGoal(ctx context.Context, t *Tx, ws db.Workspace, ng NewGoal) (db.Goal, error) {
	goals, err := t.Q.ListGoals(ctx, ws.ID)
	if err != nil {
		return db.Goal{}, err
	}
	branch := GoalBranch(ng.Title, goals)
	status := "planning"
	if ng.Draft {
		status = "draft"
	} else if ng.Direct != nil {
		status = "active"
	}
	g, err := t.Q.CreateGoal(ctx, db.CreateGoalParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: ws.ID, ManagerID: &ng.ManagerID, Title: ng.Title,
		DoneWhen: ng.DoneWhen, Color: domain.GoalColors[len(goals)%len(domain.GoalColors)], CreatedBy: &ng.By,
		RunnerID: ng.RunnerID, Description: ng.Description, Base: ng.Base, Branch: branch, Snapshot: ng.Snapshot, Status: status, JobRunID: ng.JobRunID})
	if err != nil {
		return g, err
	}
	if ng.Direct != nil {
		if err := createDirectTask(ctx, t, ws, g, ng); err != nil {
			return g, err
		}
	}
	if ng.Draft {
		err = GoalEvent(ctx, t, ws, g, "drafted", "Saved as a draft", nil)
	} else {
		err = GoalEvent(ctx, t, ws, g, "given", "Goal given", nil)
	}
	if err != nil {
		return g, err
	}
	return g, t.Emit(ctx, WSMember(ws.OrgID, ws.ID, ng.ManagerID, "goal.updated"), ToGoal(g))
}

// createDirectTask adds the one work task of a direct goal.
func createDirectTask(ctx context.Context, t *Tx, ws db.Workspace, g db.Goal, ng NewGoal) error {
	d := ng.Direct
	dirs := d.RequiredDirs
	if dirs == nil {
		dirs = []byte("[]")
	}
	task, err := t.Q.CreateTask(ctx, db.CreateTaskParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: ws.ID, GoalID: &g.ID, MemberID: &d.MemberID,
		Kind: "work", Title: d.Title, Description: d.Description, Skill: d.Skill, Status: "waiting", DependsOn: []uuid.UUID{},
		Steps: []byte("[]"), RequiredDirs: dirs, AttributedUserID: &ng.By, CreatedBy: &ng.By})
	if err != nil {
		return err
	}
	return t.Emit(ctx, WSMember(ws.OrgID, ws.ID, d.MemberID, "task.created"), ToTask(task))
}

// UpdateDraft saves changes to a draft goal and renames its branch to match the title.
func UpdateDraft(ctx context.Context, t *Tx, ws db.Workspace, id uuid.UUID, ng NewGoal) (db.Goal, error) {
	goals, err := t.Q.ListGoals(ctx, ws.ID)
	if err != nil {
		return db.Goal{}, err
	}
	others := slices.DeleteFunc(goals, func(g db.Goal) bool { return g.ID == id })
	g, err := t.Q.UpdateDraftGoal(ctx, db.UpdateDraftGoalParams{ID: id, ManagerID: &ng.ManagerID, Title: ng.Title, DoneWhen: ng.DoneWhen,
		Description: ng.Description, RunnerID: ng.RunnerID, Base: ng.Base, Branch: GoalBranch(ng.Title, others), Snapshot: ng.Snapshot})
	if err != nil {
		return g, err
	}
	return g, t.Emit(ctx, WSMember(ws.OrgID, ws.ID, ng.ManagerID, "goal.updated"), ToGoal(g))
}

// GiveDraft hands a draft goal to its manager so planning starts.
func GiveDraft(ctx context.Context, t *Tx, ws db.Workspace, id uuid.UUID) (db.Goal, error) {
	g, err := t.Q.GiveDraftGoal(ctx, id)
	if err != nil {
		return g, err
	}
	if err := GoalEvent(ctx, t, ws, g, "given", "Goal given", nil); err != nil {
		return g, err
	}
	return g, t.Emit(ctx, goalEvent(ws, g, "goal.updated"), ToGoal(g))
}

// DeleteDraft removes a draft goal from the board.
func DeleteDraft(ctx context.Context, t *Tx, ws db.Workspace, g db.Goal) (bool, error) {
	n, err := t.Q.DeleteDraftGoal(ctx, g.ID)
	if err != nil || n == 0 {
		return false, err
	}
	return true, t.Emit(ctx, goalEvent(ws, g, "goal.deleted"), map[string]any{"id": g.ID})
}

func goalEvent(ws db.Workspace, g db.Goal, typ string) realtime.Event {
	if g.ManagerID != nil {
		return WSMember(ws.OrgID, ws.ID, *g.ManagerID, typ)
	}
	return WS(ws.OrgID, ws.ID, typ)
}

// GoalBranch is canopy/<title-slug>, made unique within the workspace.
func GoalBranch(title string, existing []db.Goal) string {
	base := "canopy/" + domain.Slug(title)
	if base == "canopy/" {
		base = "canopy/goal"
	}
	taken := map[string]bool{}
	for _, g := range existing {
		taken[g.Branch] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// GoalEvent appends to a goal's timeline and publishes it.
func GoalEvent(ctx context.Context, t *Tx, ws db.Workspace, g db.Goal, typ, summary string, refs map[string]any) error {
	if refs == nil {
		refs = map[string]any{}
	}
	b, _ := json.Marshal(refs)
	e, err := t.Q.InsertGoalEvent(ctx, db.InsertGoalEventParams{ID: uuid.Must(uuid.NewV7()), GoalID: g.ID, Type: typ, Summary: summary, Refs: b})
	if err != nil {
		return err
	}
	ev := WS(ws.OrgID, ws.ID, "goal.event")
	ev.MemberID = g.ManagerID
	return t.Emit(ctx, ev, ToGoalEvent(e))
}
