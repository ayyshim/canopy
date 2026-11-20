package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"canopy/hub/internal/domain"
	"canopy/hub/internal/store/db"
)

// NewGoal is a goal a person gives to a manager.
type NewGoal struct {
	ManagerID uuid.UUID
	Title     string
	DoneWhen  string
	By        uuid.UUID
	RunnerID  *uuid.UUID // where it runs; empty = the person's own computer, else a shared one
	Base      string     // branch to start from in its repositories; "" = their default branch
	Snapshot  bool       // include the person's uncommitted changes
}

// CreateGoal records a goal given to a manager, with the next goal color, its own branch
// name and a "given" event.
func CreateGoal(ctx context.Context, t *Tx, ws db.Workspace, ng NewGoal) (db.Goal, error) {
	goals, err := t.Q.ListGoals(ctx, ws.ID)
	if err != nil {
		return db.Goal{}, err
	}
	branch := GoalBranch(ng.Title, goals)
	g, err := t.Q.CreateGoal(ctx, db.CreateGoalParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: ws.ID, ManagerID: &ng.ManagerID, Title: ng.Title,
		DoneWhen: ng.DoneWhen, Color: domain.GoalColors[len(goals)%len(domain.GoalColors)], CreatedBy: &ng.By,
		RunnerID: ng.RunnerID, Base: ng.Base, Branch: branch, Snapshot: ng.Snapshot})
	if err != nil {
		return g, err
	}
	if err := GoalEvent(ctx, t, ws, g, "given", "Goal given", nil); err != nil {
		return g, err
	}
	return g, t.Emit(ctx, WSMember(ws.OrgID, ws.ID, ng.ManagerID, "goal.updated"), ToGoal(g))
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
