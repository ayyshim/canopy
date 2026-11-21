package core

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/jobs/schedule"
	"canopy/hub/internal/store/db"
)

type JobView struct {
	ID                  uuid.UUID   `json:"id"`
	WorkspaceID         uuid.UUID   `json:"workspace_id"`
	Title               string      `json:"title"`
	Kind                string      `json:"kind"`
	Status              string      `json:"status"`
	PauseReason         string      `json:"pause_reason"`
	Brief               string      `json:"brief"`
	OwnerUserID         *uuid.UUID  `json:"owner_user_id"`
	ManagerID           *uuid.UUID  `json:"manager_id"`
	AssigneeMemberID    *uuid.UUID  `json:"assignee_member_id"`
	RunnerID            *uuid.UUID  `json:"runner_id"`
	Cron                string      `json:"cron"`
	Timezone            string      `json:"timezone"`
	Schedule            string      `json:"schedule"` // the cron in words, "" if it can't be described
	NextRunAt           *time.Time  `json:"next_run_at"`
	LastRunAt           *time.Time  `json:"last_run_at"`
	MissedPolicy        string      `json:"missed_policy"`
	CatchupWindowSecs   int32       `json:"catchup_window_secs"`
	RunTimeoutSecs      int32       `json:"run_timeout_secs"`
	TokenCapPerRun      *int64      `json:"token_cap_per_run"`
	TokenCapPerMonth    *int64      `json:"token_cap_per_month"`
	Notify              string      `json:"notify"`
	EndsAt              *time.Time  `json:"ends_at"`
	MaxRuns             *int32      `json:"max_runs"`
	RequiredDirs        []DirNeed   `json:"required_dirs"`
	MemoryNoteID        *uuid.UUID  `json:"memory_note_id"`
	ConsecutiveFailures int32       `json:"consecutive_failures"`
	CreatedBy           *uuid.UUID  `json:"created_by"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	LastRun             *JobRunView `json:"last_run,omitempty"`
}

func ToJob(j db.Job) JobView {
	dirs := []DirNeed{}
	_ = json.Unmarshal(j.RequiredDirs, &dirs)
	if dirs == nil {
		dirs = []DirNeed{}
	}
	sentence, _ := schedule.Describe(j.Cron, j.Timezone)
	return JobView{ID: j.ID, WorkspaceID: j.WorkspaceID, Title: j.Title, Kind: j.Kind, Status: j.Status, PauseReason: j.PauseReason,
		Brief: j.Brief, OwnerUserID: j.OwnerUserID, ManagerID: j.ManagerID, AssigneeMemberID: j.AssigneeMemberID, RunnerID: j.RunnerID,
		Cron: j.Cron, Timezone: j.Timezone, Schedule: sentence, NextRunAt: j.NextRunAt, LastRunAt: j.LastRunAt,
		MissedPolicy: j.MissedPolicy, CatchupWindowSecs: j.CatchupWindowSecs, RunTimeoutSecs: j.RunTimeoutSecs,
		TokenCapPerRun: j.TokenCapPerRun, TokenCapPerMonth: j.TokenCapPerMonth, Notify: j.Notify, EndsAt: j.EndsAt, MaxRuns: j.MaxRuns,
		RequiredDirs: dirs, MemoryNoteID: j.MemoryNoteID, ConsecutiveFailures: j.ConsecutiveFailures,
		CreatedBy: j.CreatedBy, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}
}

type JobRunView struct {
	ID           uuid.UUID  `json:"id"`
	JobID        uuid.UUID  `json:"job_id"`
	Number       int32      `json:"number"`
	ScheduledFor time.Time  `json:"scheduled_for"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	SkipReason   string     `json:"skip_reason"`
	Attempt      int32      `json:"attempt"`
	GoalID       *uuid.UUID `json:"goal_id"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Tokens       int64      `json:"tokens"`
	Result       string     `json:"result"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	Error        string     `json:"error"`
}

func ToJobRun(r db.JobRun) JobRunView {
	return JobRunView{ID: r.ID, JobID: r.JobID, Number: r.Number, ScheduledFor: r.ScheduledFor, Trigger: r.Trigger, Status: r.Status,
		SkipReason: r.SkipReason, Attempt: r.Attempt, GoalID: r.GoalID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		Tokens: r.Tokens, Result: r.Result, Title: r.Title, Summary: r.Summary, Error: r.Error}
}
