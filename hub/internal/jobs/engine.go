// Package jobs runs scheduled jobs: it finds due jobs, creates their runs and goals,
// and stops runs that time out or pass their token cap. See docs/design/job-scheduler.md.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/jobs/schedule"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Host is what the engine needs from the orchestrator.
type Host interface {
	Kick(ctx context.Context, wsID uuid.UUID)
	StopGoal(ctx context.Context, goalID uuid.UUID, why string) error
}

var (
	ErrRunning   = errors.New("a run of this job is still going")
	ErrEnded     = errors.New("this job has ended")
	ErrNoWorker  = errors.New("this job has no one to do the work")
	ErrNotPaused = errors.New("this job is not paused")
)

const (
	// failuresToPause is how many failed runs in a row pause a job.
	failuresToPause = 3
	dueBatch        = 50
)

type Engine struct {
	Core     *core.Service
	Host     Host
	Interval time.Duration // 15 seconds when zero
	Now      func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	if e.Core != nil && e.Core.Now != nil {
		return e.Core.Now()
	}
	return time.Now()
}

// Run ticks until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	every := e.Interval
	if every == 0 {
		every = 15 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("job tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick handles every due job, then checks the runs that are going.
func (e *Engine) Tick(ctx context.Context) error {
	now := e.now()
	ids, err := e.Core.Store.DueJobIDs(ctx, db.DueJobIDsParams{Now: &now, MaxRows: dueBatch})
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := e.startDue(ctx, id, now); err != nil {
			slog.Error("due job", "job", id, "err", err)
			errs = append(errs, err)
		}
	}
	if err := e.sweep(ctx, now); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// nextAfter is the first cron time strictly after t, or nil when the job never runs again.
func nextAfter(j db.Job, t time.Time) *time.Time {
	c, err := schedule.ParseCron(j.Cron)
	if err != nil {
		return nil
	}
	loc, err := schedule.LoadZone(j.Timezone)
	if err != nil {
		return nil
	}
	n, ok := c.Next(t, loc)
	if !ok {
		return nil
	}
	return &n
}

func (e *Engine) startDue(ctx context.Context, id uuid.UUID, now time.Time) error {
	return e.Core.Tx(ctx, func(t *core.Tx) error {
		j, err := t.Q.LockDueJob(ctx, db.LockDueJobParams{ID: id, Now: &now})
		if store.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
		return e.handleDue(ctx, t, j, now)
	})
}

// handleDue applies the checks in order and always moves next_run_at past now.
func (e *Engine) handleDue(ctx context.Context, t *core.Tx, j db.Job, now time.Time) error {
	due := *j.NextRunAt
	next := nextAfter(j, now)
	upd := db.UpdateJobParams{ID: j.ID, SetNext: true, NextRunAt: next}
	if next == nil {
		upd.Status = ptr("ended")
	}
	save := func() error { _, err := updateJob(ctx, t, upd); return err }

	// 1. ended
	if j.EndsAt != nil && !now.Before(*j.EndsAt) {
		upd.Status, upd.NextRunAt = ptr("ended"), nil
		return save()
	}
	if j.MaxRuns != nil {
		n, err := t.Q.CountJobRuns(ctx, j.ID)
		if err != nil {
			return err
		}
		if n >= *j.MaxRuns {
			upd.Status, upd.NextRunAt = ptr("ended"), nil
			return save()
		}
	}
	// 2. too late: run_once catches up within the window, skip only within a minute
	if now.Sub(due) > lateLimit(j) {
		if err := e.skip(ctx, t, j, due, "schedule", "too late", now); err != nil {
			return err
		}
		return save()
	}
	// 3. overlap
	if _, err := t.Q.RunningJobRun(ctx, j.ID); err == nil {
		if err := e.skip(ctx, t, j, due, "schedule", "previous run still going", now); err != nil {
			return err
		}
		return save()
	} else if !store.IsNotFound(err) {
		return err
	}
	// 4. owner left
	ws, err := t.Q.GetWorkspace(ctx, j.WorkspaceID)
	if err != nil {
		return err
	}
	org, err := t.Q.GetOrganization(ctx, ws.OrgID)
	if err != nil {
		return err
	}
	if j.OwnerUserID == nil {
		return e.pause(ctx, t, j, "owner_left")
	}
	if _, err := t.Q.GetMembership(ctx, db.GetMembershipParams{OrgID: org.ID, UserID: *j.OwnerUserID}); store.IsNotFound(err) {
		return e.pause(ctx, t, j, "owner_left")
	} else if err != nil {
		return err
	}
	// 5. owner over limit
	u, err := core.UserUsage(ctx, t.Q, org, *j.OwnerUserID, now)
	if err != nil {
		return err
	}
	if u.Reason() != "" {
		if err := e.skip(ctx, t, j, due, "schedule", "owner over limit", now); err != nil {
			return err
		}
		return save()
	}
	// 6. start
	if _, err := e.startRun(ctx, t, j, ws, due, "schedule", now); err != nil {
		if errors.Is(err, ErrNoWorker) {
			if err := e.skip(ctx, t, j, due, "schedule", "no one assigned", now); err != nil {
				return err
			}
			return save()
		}
		return err
	}
	upd.LastRunAt = &now
	return save()
}

// lateLimit is how late a due run may still start.
func lateLimit(j db.Job) time.Duration {
	w := time.Duration(j.CatchupWindowSecs) * time.Second
	if j.MissedPolicy == "skip" && w > time.Minute {
		return time.Minute
	}
	return w
}

func (e *Engine) pause(ctx context.Context, t *core.Tx, j db.Job, reason string) error {
	_, err := updateJob(ctx, t, db.UpdateJobParams{ID: j.ID, Status: ptr("paused"), PauseReason: &reason})
	return err
}

// skip records a run that did not start.
func (e *Engine) skip(ctx context.Context, t *core.Tx, j db.Job, scheduled time.Time, trigger, reason string, now time.Time) error {
	n, err := t.Q.NextJobRunNumber(ctx, j.ID)
	if err != nil {
		return err
	}
	_, err = createRun(ctx, t, j, db.CreateJobRunParams{ID: uuid.Must(uuid.NewV7()), JobID: j.ID, Number: n, ScheduledFor: scheduled,
		Trigger: trigger, Status: "skipped", SkipReason: reason, FinishedAt: &now})
	return err
}

// startRun creates the run and its goal; the goal's one task carries the run text.
func (e *Engine) startRun(ctx context.Context, t *core.Tx, j db.Job, ws db.Workspace, scheduled time.Time, trigger string, now time.Time) (db.JobRun, error) {
	worker := j.AssigneeMemberID
	if worker == nil {
		worker = j.ManagerID
	}
	if worker == nil || j.OwnerUserID == nil {
		return db.JobRun{}, ErrNoWorker
	}
	manager := *worker
	if j.ManagerID != nil {
		manager = *j.ManagerID
	}
	member, err := t.Q.GetMember(ctx, *worker)
	if err != nil {
		return db.JobRun{}, err
	}
	n, err := t.Q.NextJobRunNumber(ctx, j.ID)
	if err != nil {
		return db.JobRun{}, err
	}
	run, err := createRun(ctx, t, j, db.CreateJobRunParams{ID: uuid.Must(uuid.NewV7()), JobID: j.ID, Number: n, ScheduledFor: scheduled,
		Trigger: trigger, Status: "running", StartedAt: &now})
	if err != nil {
		return run, err
	}
	loc, err := schedule.LoadZone(j.Timezone)
	if err != nil {
		loc = time.UTC
	}
	var memory string
	var earlier []db.JobRun
	if j.Kind == "continuous" {
		if j.MemoryNoteID != nil {
			note, err := t.Q.GetNote(ctx, *j.MemoryNoteID)
			if err != nil && !store.IsNotFound(err) {
				return run, err
			}
			memory = note.Body
		}
		if earlier, err = t.Q.LastJobRuns(ctx, db.LastJobRunsParams{JobID: j.ID, Limit: 3 * earlierRuns}); err != nil {
			return run, err
		}
	}
	g, err := core.CreateGoal(ctx, t, ws, core.NewGoal{ManagerID: manager, Title: j.Title + " · run " + itoa(n), By: *j.OwnerUserID,
		RunnerID: j.RunnerID, JobRunID: &run.ID,
		Direct: &core.DirectWork{MemberID: *worker, Skill: member.Skill, Title: j.Title, Description: RunText(j, run, loc, memory, earlier), RequiredDirs: j.RequiredDirs}})
	if err != nil {
		return run, err
	}
	run, err = updateRun(ctx, t, j, db.UpdateJobRunParams{ID: run.ID, GoalID: &g.ID})
	if err != nil {
		return run, err
	}
	t.AfterCommit(func() {
		if e.Host != nil {
			e.Host.Kick(context.WithoutCancel(ctx), ws.ID)
		}
	})
	return run, nil
}

func ptr[T any](v T) *T { return &v }

func itoa(n int32) string { return strconv.Itoa(int(n)) }
