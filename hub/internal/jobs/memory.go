// Package jobs runs scheduled jobs. This file holds run reports and memory.
package jobs

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// MaxMemoryChars is the hard cap on a continuous job's summary.
const MaxMemoryChars = 5000

// ErrMemoryTooLong is shown to the agent as is.
const ErrMemoryTooLong = UserError("Too long. Fold the oldest details into short facts, keep names and open threads, and send it again.")

// UserError is a message meant for the agent, not a server failure.
type UserError string

func (e UserError) Error() string { return string(e) }

// Report is what the agent sends with job_run_report.
type Report struct {
	Title     string
	Summary   string
	Result    string // ok, attention or nothing_to_do
	NewMemory *string
}

// SaveReport stores the report on the run. New memory is held until the run succeeds.
func SaveReport(ctx context.Context, q *db.Queries, job db.Job, run db.JobRun, in Report) (db.JobRun, error) {
	switch in.Result {
	case "ok", "attention", "nothing_to_do":
	default:
		return run, UserError("result must be ok, attention or nothing_to_do.")
	}
	if run.Status != "running" {
		return run, UserError("This run is already finished.")
	}
	p := db.UpdateJobRunParams{ID: run.ID, Result: &in.Result, Title: &in.Title, Summary: &in.Summary}
	if in.NewMemory != nil {
		if job.Kind != "continuous" {
			return run, UserError("This is a single run job. It has no memory, so leave out new_memory.")
		}
		if utf8.RuneCountInString(*in.NewMemory) > MaxMemoryChars {
			return run, ErrMemoryTooLong
		}
		p.SetPending, p.PendingMemory = true, in.NewMemory
	}
	return q.UpdateJobRun(ctx, p)
}

// Memory returns the summary text of a continuous job.
func Memory(ctx context.Context, q *db.Queries, job db.Job) (string, error) {
	if job.MemoryNoteID == nil {
		return "", nil
	}
	n, err := q.GetNote(ctx, *job.MemoryNoteID)
	if store.IsNotFound(err) {
		return "", nil
	}
	return n.Body, err
}

// applyMemory saves the run's pending memory as a new note revision.
func applyMemory(ctx context.Context, t *core.Tx, job db.Job, text string) error {
	if job.MemoryNoteID == nil {
		return nil
	}
	if err := t.SaveNoteRevision(ctx, *job.MemoryNoteID); err != nil {
		return err
	}
	n, err := t.Q.UpdateNote(ctx, db.UpdateNoteParams{ID: *job.MemoryNoteID, Body: &text})
	if err != nil {
		return err
	}
	ws, err := t.Q.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		return err
	}
	return t.EmitNoteSaved(ctx, ws, nil, n)
}

// LogLine is one run in the run log.
func LogLine(r db.JobRun) string {
	when := r.ScheduledFor.UTC().Format("2006-01-02")
	what := r.Result
	if r.Status != "succeeded" {
		what = r.Status
		if r.SkipReason != "" {
			what += ": " + r.SkipReason
		}
	}
	return fmt.Sprintf("Run %d (%s, %s): %s", r.Number, when, what, firstNonEmpty(r.Title, r.Summary))
}

// RunLog lists runs that did work, oldest first, newest n.
func RunLog(ctx context.Context, q *db.Queries, job db.Job, n int32) ([]string, error) {
	runs, err := q.LastJobRuns(ctx, db.LastJobRunsParams{JobID: job.ID, Limit: n})
	if err != nil {
		return nil, err
	}
	var lines []string
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status != "running" {
			lines = append(lines, LogLine(runs[i]))
		}
	}
	return lines, nil
}

// MemoryText is what job_memory_read shows: brief, summary and the whole run log.
func MemoryText(ctx context.Context, q *db.Queries, job db.Job) (string, error) {
	mem, err := Memory(ctx, q, job)
	if err != nil {
		return "", err
	}
	log, err := RunLog(ctx, q, job, 1000)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Brief\n\n%s\n\n## Summary\n\n%s\n\n## Run log\n\n", job.Brief, firstNonEmpty(mem, "(empty)"))
	if len(log) == 0 {
		b.WriteString("No finished runs yet.\n")
	}
	for _, l := range log {
		b.WriteString("- " + l + "\n")
	}
	return b.String(), nil
}

// RunReadText is what job_run_read(n) shows for one earlier run.
func RunReadText(ctx context.Context, q *db.Queries, job db.Job, number int32, outputs []string) (string, error) {
	runs, err := q.LastJobRuns(ctx, db.LastJobRunsParams{JobID: job.ID, Limit: 100000})
	if err != nil {
		return "", err
	}
	for _, r := range runs {
		if r.Number != number {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "## %s\n\nStatus: %s", LogLine(r), r.Status)
		if r.Result != "" {
			b.WriteString(", result: " + r.Result)
		}
		fmt.Fprintf(&b, "\n\n%s\n", r.Summary)
		if r.MemoryAfter != nil {
			fmt.Fprintf(&b, "\nSummary saved after this run:\n\n%s\n", *r.MemoryAfter)
		}
		if len(outputs) > 0 {
			b.WriteString("\nOutputs: " + strings.Join(outputs, ", ") + "\n")
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("There is no run %d for this job.", number)
}

// GoalRun finds the job and run behind a goal, if it is a job goal.
func GoalRun(ctx context.Context, q *db.Queries, goalID uuid.UUID) (db.Job, db.JobRun, bool, error) {
	run, err := q.JobRunByGoal(ctx, goalID)
	if store.IsNotFound(err) {
		return db.Job{}, db.JobRun{}, false, nil
	}
	if err != nil {
		return db.Job{}, db.JobRun{}, false, err
	}
	job, err := q.GetJob(ctx, run.JobID)
	return job, run, err == nil, err
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
