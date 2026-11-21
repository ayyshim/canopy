package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Pause stops a job from starting new runs. A run that is going keeps going.
func (e *Engine) Pause(ctx context.Context, jobID uuid.UUID, reason string) (db.Job, error) {
	var j db.Job
	err := e.Core.Tx(ctx, func(t *core.Tx) error {
		cur, err := t.Q.LockJob(ctx, jobID)
		if err != nil {
			return err
		}
		if cur.Status == "ended" {
			return ErrEnded
		}
		j, err = t.Q.UpdateJob(ctx, db.UpdateJobParams{ID: jobID, Status: ptr("paused"), PauseReason: &reason})
		return err
	})
	return j, err
}

// Resume restarts a paused job from now; nothing missed while paused is replayed.
func (e *Engine) Resume(ctx context.Context, jobID uuid.UUID) (db.Job, error) {
	var j db.Job
	err := e.Core.Tx(ctx, func(t *core.Tx) error {
		cur, err := t.Q.LockJob(ctx, jobID)
		if err != nil {
			return err
		}
		switch cur.Status {
		case "ended":
			return ErrEnded
		case "active":
			return ErrNotPaused
		}
		j, err = t.Q.UpdateJob(ctx, db.UpdateJobParams{ID: jobID, Status: ptr("active"), PauseReason: ptr(""),
			SetNext: true, NextRunAt: nextAfter(cur, e.now()), ConsecutiveFailures: ptr(int32(0))})
		return err
	})
	return j, err
}

// RunNow starts a run outside the schedule. It does not move next_run_at.
func (e *Engine) RunNow(ctx context.Context, jobID uuid.UUID) (db.JobRun, error) {
	return e.startExtra(ctx, jobID, "manual")
}

// TestRun is a run that never writes memory and never counts as a failure.
func (e *Engine) TestRun(ctx context.Context, jobID uuid.UUID) (db.JobRun, error) {
	return e.startExtra(ctx, jobID, "test")
}

func (e *Engine) startExtra(ctx context.Context, jobID uuid.UUID, trigger string) (db.JobRun, error) {
	var run db.JobRun
	err := e.Core.Tx(ctx, func(t *core.Tx) error {
		j, err := t.Q.LockJob(ctx, jobID)
		if err != nil {
			return err
		}
		if j.Status == "ended" {
			return ErrEnded
		}
		if _, err := t.Q.RunningJobRun(ctx, jobID); err == nil {
			return ErrRunning
		} else if !store.IsNotFound(err) {
			return err
		}
		ws, err := t.Q.GetWorkspace(ctx, j.WorkspaceID)
		if err != nil {
			return err
		}
		now := e.now()
		run, err = e.startRun(ctx, t, j, ws, now, trigger, now)
		if err != nil {
			return err
		}
		if trigger == "manual" {
			_, err = t.Q.UpdateJob(ctx, db.UpdateJobParams{ID: jobID, LastRunAt: &now})
		}
		return err
	})
	return run, err
}

// sweep stops running runs that passed their timeout or token cap.
func (e *Engine) sweep(ctx context.Context, now time.Time) error {
	runs, err := e.Core.Store.RunningJobRuns(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range runs {
		if err := e.check(ctx, r, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) check(ctx context.Context, r db.JobRun, now time.Time) error {
	j, err := e.Core.Store.GetJob(ctx, r.JobID)
	if err != nil {
		return err
	}
	if r.GoalID == nil {
		return nil
	}
	tokens, err := e.Core.Store.GoalTokens(ctx, *r.GoalID)
	if err != nil {
		return err
	}
	switch {
	case j.TokenCapPerRun != nil && tokens >= *j.TokenCapPerRun:
		return e.stop(ctx, r, j, "capped", "", "Stopped at the token limit for one run.", tokens, now)
	case r.StartedAt != nil && now.Sub(*r.StartedAt) > time.Duration(j.RunTimeoutSecs)*time.Second:
		started, err := e.Core.Store.GoalStartedTasks(ctx, r.GoalID)
		if err != nil {
			return err
		}
		if started == 0 {
			return e.stop(ctx, r, j, "skipped", "computer off", "The computer never started the work.", tokens, now)
		}
		return e.stop(ctx, r, j, "failed", "", "The run took longer than its time limit.", tokens, now)
	}
	return nil
}

// stop ends the goal, then closes the run. Both steps can be repeated safely after a crash.
func (e *Engine) stop(ctx context.Context, r db.JobRun, j db.Job, status, skip, why string, tokens int64, now time.Time) error {
	if e.Host != nil {
		if err := e.Host.StopGoal(ctx, *r.GoalID, why); err != nil {
			return err
		}
	}
	return e.Core.Tx(ctx, func(t *core.Tx) error {
		if _, err := t.Q.LockJob(ctx, j.ID); err != nil {
			return err
		}
		cur, err := t.Q.GetJobRun(ctx, r.ID)
		if err != nil || cur.Status != "running" {
			return err
		}
		p := db.UpdateJobRunParams{ID: r.ID, Status: &status, SkipReason: &skip, FinishedAt: &now, Tokens: &tokens}
		if status != "skipped" {
			p.Error = &why
		}
		if _, err := t.Q.UpdateJobRun(ctx, p); err != nil {
			return err
		}
		if status != "failed" || r.Trigger == "test" {
			return nil
		}
		fails := j.ConsecutiveFailures + 1
		up := db.UpdateJobParams{ID: j.ID, ConsecutiveFailures: &fails}
		if fails >= failuresToPause {
			up.Status, up.PauseReason = ptr("paused"), ptr("failures")
		}
		_, err = t.Q.UpdateJob(ctx, up)
		return err
	})
}
