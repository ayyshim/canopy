package orch

import (
	"context"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/shared/protocol"
	"canopy/hub/internal/store/db"
)

// StopGoal ends a goal's sessions and marks it stopped (a cancelled, capped or timed out job run).
// It does nothing for a goal that is already closed.
func (o *Orch) StopGoal(ctx context.Context, goalID uuid.UUID, why string) error {
	return o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		if g.Status == "done" || g.Status == "stopped" {
			return nil
		}
		ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, Status: ptr("stopped"), SetFinished: true, ShowTasks: ptr(false)})
		if err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
			return err
		}
		if err := o.closeReadyCard(ctx, t, st.ws, g.ID, "stopped"); err != nil {
			return err
		}
		if err := core.GoalEvent(ctx, t, st.ws, ng, "stopped", why, nil); err != nil {
			return err
		}
		if err := o.endGoalSessions(ctx, t, st.ws, g.ID); err != nil {
			return err
		}
		if g.RunnerID != nil {
			runner, id := *g.RunnerID, g.ID
			t.AfterCommit(func() { o.send(runner, protocol.TGoalCleanup, protocol.GoalCleanup{GoalID: id}) })
		}
		return nil
	})
}
