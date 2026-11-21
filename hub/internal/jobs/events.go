package jobs

import (
	"context"

	"canopy/hub/internal/core"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store/db"
)

// jobEvent is scoped to the job's manager, so people who can't see that team don't get it.
func jobEvent(ws db.Workspace, j db.Job, typ string) realtime.Event {
	if j.ManagerID != nil {
		return core.WSMember(ws.OrgID, ws.ID, *j.ManagerID, typ)
	}
	e := core.WS(ws.OrgID, ws.ID, typ)
	e.AdminOnly = true
	return e
}

// updateJob saves a job and publishes job.updated.
func updateJob(ctx context.Context, t *core.Tx, p db.UpdateJobParams) (db.Job, error) {
	j, err := t.Q.UpdateJob(ctx, p)
	if err != nil {
		return j, err
	}
	return j, emitJob(ctx, t, j)
}

func emitJob(ctx context.Context, t *core.Tx, j db.Job) error {
	ws, err := t.Q.GetWorkspace(ctx, j.WorkspaceID)
	if err != nil {
		return err
	}
	return t.Emit(ctx, jobEvent(ws, j, "job.updated"), core.ToJob(j))
}

// createRun saves a new run and publishes job.run.updated.
func createRun(ctx context.Context, t *core.Tx, j db.Job, p db.CreateJobRunParams) (db.JobRun, error) {
	r, err := t.Q.CreateJobRun(ctx, p)
	if err != nil {
		return r, err
	}
	return r, emitRun(ctx, t, j, r)
}

// updateRun saves a run and publishes job.run.updated.
func updateRun(ctx context.Context, t *core.Tx, j db.Job, p db.UpdateJobRunParams) (db.JobRun, error) {
	r, err := t.Q.UpdateJobRun(ctx, p)
	if err != nil {
		return r, err
	}
	return r, emitRun(ctx, t, j, r)
}

func emitRun(ctx context.Context, t *core.Tx, j db.Job, r db.JobRun) error {
	ws, err := t.Q.GetWorkspace(ctx, j.WorkspaceID)
	if err != nil {
		return err
	}
	return t.Emit(ctx, jobEvent(ws, j, "job.run.updated"), core.ToJobRun(r))
}
