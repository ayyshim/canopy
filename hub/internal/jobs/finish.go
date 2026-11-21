package jobs

import (
	"context"
	"time"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

// FailuresToPause is how many failed runs in a row pause a job.
const FailuresToPause = 3

// Outcome says how a finished run ended.
type Outcome struct {
	Run    db.JobRun
	Job    db.Job
	Paused bool // the job paused itself after repeated failures
}

// RunFinished closes the run behind a finished job goal. A run that sent a report
// succeeded; one that did not failed. Memory is applied only on success.
func RunFinished(ctx context.Context, t *core.Tx, g db.Goal, now time.Time) (Outcome, error) {
	var out Outcome
	if g.JobRunID == nil {
		return out, nil
	}
	run, err := t.Q.GetJobRun(ctx, *g.JobRunID)
	if err != nil {
		return out, err
	}
	job, err := t.Q.GetJob(ctx, run.JobID)
	if err != nil {
		return out, err
	}
	out.Run, out.Job = run, job
	if run.Status != "running" {
		return out, nil
	}
	rows, err := t.Q.UsageWorkForGoal(ctx, g.ID)
	if err != nil {
		return out, err
	}
	var tokens int64
	for _, r := range rows {
		tokens += r.TokensIn + r.TokensOut
	}
	ok := run.Result != ""
	status, msg := "succeeded", ""
	if !ok {
		status, msg = "failed", "The agent finished without a report."
	}
	p := db.UpdateJobRunParams{ID: run.ID, Status: &status, Error: &msg, FinishedAt: &now, Tokens: &tokens, SetPending: true}
	if ok && run.PendingMemory != nil && job.Kind == "continuous" && run.Trigger != "test" {
		if err := applyMemory(ctx, t, job, *run.PendingMemory); err != nil {
			return out, err
		}
		p.SetMemoryAfter, p.MemoryAfter = true, run.PendingMemory
	}
	if out.Run, err = t.Q.UpdateJobRun(ctx, p); err != nil {
		return out, err
	}
	// Test runs do not count either way.
	if run.Trigger == "test" {
		return out, nil
	}
	jp := db.UpdateJobParams{ID: job.ID, LastRunAt: &now}
	fails := int32(0)
	if !ok {
		fails = job.ConsecutiveFailures + 1
	}
	jp.ConsecutiveFailures = &fails
	if fails >= FailuresToPause && job.Status == "active" {
		jp.Status, jp.PauseReason = ptr("paused"), ptr("failures")
		out.Paused = true
	}
	out.Job, err = t.Q.UpdateJob(ctx, jp)
	return out, err
}

func ptr[T any](v T) *T { return &v }
