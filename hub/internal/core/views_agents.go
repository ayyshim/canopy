package core

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

type GoalView struct {
	ID                 uuid.UUID       `json:"id"`
	Number             int32           `json:"number"`
	ManagerID          *uuid.UUID      `json:"manager_id"`
	Title              string          `json:"title"`
	DoneWhen           string          `json:"done_when"`
	Description        string          `json:"description"`
	Color              string          `json:"color"`
	Status             string          `json:"status"`
	Summary            string          `json:"summary"`
	CreatedBy          *uuid.UUID      `json:"created_by"`
	PlannedAt          *time.Time      `json:"planned_at"`
	FinishedAt         *time.Time      `json:"finished_at"`
	ReadyAt            *time.Time      `json:"ready_at"`
	RunnerID           *uuid.UUID      `json:"runner_id"`
	Base               string          `json:"base"`
	Branch             string          `json:"branch"`
	Snapshot           bool            `json:"snapshot"`
	Shipped            json.RawMessage `json:"shipped"`
	ShowTasks          bool            `json:"show_tasks"`
	DoneBy             *uuid.UUID      `json:"done_by"`
	HandoverApprovedBy *uuid.UUID      `json:"handover_approved_by"`
	HandoverApprovedAt *time.Time      `json:"handover_approved_at"`
	CreatedAt          time.Time       `json:"created_at"`
	Usage              *UsageTotals    `json:"usage"`                // tokens and cost so far; nil when not loaded
	JobRunID           *uuid.UUID      `json:"job_run_id,omitempty"` // set when the goal is a job run
}

func ToGoal(g db.Goal) GoalView {
	v := GoalView{ID: g.ID, Number: g.Number, ManagerID: g.ManagerID, Title: g.Title, DoneWhen: g.DoneWhen, Description: g.Description, Color: g.Color, Status: g.Status,
		Summary: g.Summary, CreatedBy: g.CreatedBy, PlannedAt: g.PlannedAt, FinishedAt: g.FinishedAt, CreatedAt: g.CreatedAt,
		ReadyAt: g.ReadyAt, RunnerID: g.RunnerID, Base: g.Base, Branch: g.Branch, Snapshot: g.Snapshot, Shipped: g.Shipped,
		ShowTasks: g.ShowTasks, DoneBy: g.DoneBy,
		HandoverApprovedBy: g.HandoverApprovedBy, HandoverApprovedAt: g.HandoverApprovedAt, JobRunID: g.JobRunID}
	if len(v.Shipped) == 0 {
		v.Shipped = json.RawMessage("{}")
	}
	return v
}

// GoalRepoView is a goal's branch in one repository.
type GoalRepoView struct {
	GoalID      uuid.UUID `json:"goal_id"`
	DirectoryID uuid.UUID `json:"directory_id"`
	Base        string    `json:"base"`
	TipSHA      string    `json:"tip_sha"`
	Commits     int32     `json:"commits"`
	BackedUp    bool      `json:"backed_up"`
	PRURL       string    `json:"pr_url"`
	Status      string    `json:"status"`
}

func ToGoalRepo(r db.GoalRepo) GoalRepoView {
	return GoalRepoView{GoalID: r.GoalID, DirectoryID: r.DirectoryID, Base: r.Base, TipSHA: r.TipSha, Commits: r.Commits,
		BackedUp: r.BundleKey != "", PRURL: r.PrUrl, Status: r.Status}
}

type Step struct {
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type DirNeed struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	Mode        string    `json:"mode"`
}

type TaskView struct {
	ID               uuid.UUID    `json:"id"`
	Number           int32        `json:"number"`
	GoalID           *uuid.UUID   `json:"goal_id"`
	MemberID         *uuid.UUID   `json:"member_id"`
	Kind             string       `json:"kind"`
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	Skill            *string      `json:"skill"`
	Status           string       `json:"status"`
	Progress         int32        `json:"progress"`
	DependsOn        []uuid.UUID  `json:"depends_on"`
	Steps            []Step       `json:"steps"`
	CurrentStep      int32        `json:"current_step"`
	RequiredDirs     []DirNeed    `json:"required_dirs"`
	Borrowed         bool         `json:"borrowed"`
	AttributedUserID *uuid.UUID   `json:"attributed_user_id"`
	CreatedBy        *uuid.UUID   `json:"created_by"`
	ParentTaskID     *uuid.UUID   `json:"parent_task_id"`
	Summary          string       `json:"summary"`
	StartedAt        *time.Time   `json:"started_at"`
	FinishedAt       *time.Time   `json:"finished_at"`
	CreatedAt        time.Time    `json:"created_at"`
	Usage            *UsageTotals `json:"usage"` // tokens and cost so far; nil when not loaded
}

func ToTask(t db.Task) TaskView {
	v := TaskView{ID: t.ID, Number: t.Number, GoalID: t.GoalID, MemberID: t.MemberID, Kind: t.Kind, Title: t.Title, Description: t.Description,
		Skill: t.Skill, Status: t.Status, Progress: t.Progress, DependsOn: t.DependsOn, CurrentStep: t.CurrentStep,
		Borrowed: t.Borrowed, AttributedUserID: t.AttributedUserID, CreatedBy: t.CreatedBy, ParentTaskID: t.ParentTaskID,
		Summary: t.Summary, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt, CreatedAt: t.CreatedAt}
	if v.DependsOn == nil {
		v.DependsOn = []uuid.UUID{}
	}
	v.Steps = TaskSteps(t)
	v.RequiredDirs = TaskDirs(t)
	return v
}

func TaskSteps(t db.Task) []Step {
	var steps []Step
	_ = json.Unmarshal(t.Steps, &steps)
	if steps == nil {
		steps = []Step{}
	}
	return steps
}

func TaskDirs(t db.Task) []DirNeed {
	var dirs []DirNeed
	_ = json.Unmarshal(t.RequiredDirs, &dirs)
	if dirs == nil {
		dirs = []DirNeed{}
	}
	return dirs
}

type SessionView struct {
	ID            uuid.UUID  `json:"id"`
	MemberID      uuid.UUID  `json:"member_id"`
	GoalID        *uuid.UUID `json:"goal_id"`
	PersonID      *uuid.UUID `json:"person_id"`
	RunnerID      *uuid.UUID `json:"runner_id"`
	Number        int32      `json:"number"`
	Status        string     `json:"status"`
	TaskID        *uuid.UUID `json:"task_id"`
	TokensIn      int64      `json:"tokens_in"`
	TokensOut     int64      `json:"tokens_out"`
	RecoveredFrom *uuid.UUID `json:"recovered_from"`
	StartedAt     time.Time  `json:"started_at"`
	LastActiveAt  time.Time  `json:"last_active_at"`
	EndedAt       *time.Time `json:"ended_at"`
}

func ToSession(s db.Session) SessionView {
	return SessionView{ID: s.ID, MemberID: s.MemberID, GoalID: s.GoalID, PersonID: s.PersonID, RunnerID: s.RunnerID, Number: s.Number, Status: s.Status, TaskID: s.TaskID, TokensIn: s.TokensIn,
		TokensOut: s.TokensOut, RecoveredFrom: s.RecoveredFrom, StartedAt: s.StartedAt, LastActiveAt: s.LastActiveAt, EndedAt: s.EndedAt}
}

type SignalView struct {
	ID         uuid.UUID       `json:"id"`
	Type       string          `json:"type"`
	MemberID   *uuid.UUID      `json:"member_id"`
	TaskID     *uuid.UUID      `json:"task_id"`
	GoalID     *uuid.UUID      `json:"goal_id"`
	ForUserID  *uuid.UUID      `json:"for_user_id"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"`
	ResolvedBy *uuid.UUID      `json:"resolved_by"`
	Resolution json.RawMessage `json:"resolution"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at"`
}

func ToSignal(s db.Signal) SignalView {
	v := SignalView{ID: s.ID, Type: s.Type, MemberID: s.MemberID, TaskID: s.TaskID, GoalID: s.GoalID, ForUserID: s.ForUserID, Payload: s.Payload, Status: s.Status,
		ResolvedBy: s.ResolvedBy, Resolution: s.Resolution, ExpiresAt: s.ExpiresAt, CreatedAt: s.CreatedAt, ResolvedAt: s.ResolvedAt}
	if len(v.Payload) == 0 {
		v.Payload = json.RawMessage("{}")
	}
	if len(v.Resolution) == 0 {
		v.Resolution = json.RawMessage("null")
	}
	return v
}

// PersonalSignal: usage-limit and cap cards go only to the computer's owner (admins for a
// shared computer), named in the signal's for_user_id.
func PersonalSignal(typ string) bool { return typ == "usage_limit" || typ == "cap" }

type OutputVersionView struct {
	Number    int32      `json:"number"`
	Mime      string     `json:"mime"`
	Size      int64      `json:"size"`
	Note      string     `json:"note"`
	SessionID *uuid.UUID `json:"session_id"`
	Model     string     `json:"model"`
	CreatedAt time.Time  `json:"created_at"`
}

type OutputView struct {
	ID        uuid.UUID           `json:"id"`
	TaskID    *uuid.UUID          `json:"task_id"`
	MemberID  *uuid.UUID          `json:"member_id"`
	Kind      string              `json:"kind"`
	Name      string              `json:"name"`
	Title     string              `json:"title"`
	Status    string              `json:"status"`
	Versions  []OutputVersionView `json:"versions"`
	CreatedAt time.Time           `json:"created_at"`
}

func ToOutput(o db.Output, versions []db.OutputVersion) OutputView {
	v := OutputView{ID: o.ID, TaskID: o.TaskID, MemberID: o.MemberID, Kind: o.Kind, Name: o.Name, Title: o.Title, Status: o.Status,
		CreatedAt: o.CreatedAt, Versions: []OutputVersionView{}}
	for _, ver := range versions {
		v.Versions = append(v.Versions, OutputVersionView{Number: ver.Number, Mime: ver.Mime, Size: ver.Size, Note: ver.Note,
			SessionID: ver.SessionID, Model: ver.Model, CreatedAt: ver.CreatedAt})
	}
	return v
}

type ChatMessageView struct {
	ID           uuid.UUID       `json:"id"`
	MemberID     uuid.UUID       `json:"member_id"`
	AuthorUserID *uuid.UUID      `json:"author_user_id"`
	Kind         string          `json:"kind"`
	Text         string          `json:"text"`
	AsGoal       bool            `json:"as_goal"`
	Refs         json.RawMessage `json:"refs"`
	CreatedAt    time.Time       `json:"created_at"`
}

func ToChat(m db.ChatMessage) ChatMessageView {
	return ChatMessageView{ID: m.ID, MemberID: m.MemberID, AuthorUserID: m.AuthorUserID, Kind: m.Kind, Text: m.Text, AsGoal: m.AsGoal,
		Refs: m.Refs, CreatedAt: m.CreatedAt}
}

type LogLineView struct {
	ID        int64      `json:"id"`
	SessionID *uuid.UUID `json:"session_id"`
	MemberID  *uuid.UUID `json:"member_id"`
	TaskID    *uuid.UUID `json:"task_id"`
	Kind      string     `json:"kind"`
	Text      string     `json:"text"`
	At        time.Time  `json:"at"`
}

func ToLog(l db.LogLine) LogLineView {
	return LogLineView{ID: l.ID, SessionID: l.SessionID, MemberID: l.MemberID, TaskID: l.TaskID, Kind: l.Kind, Text: l.Text, At: l.At}
}

type GoalEventView struct {
	GoalID  uuid.UUID       `json:"goal_id"`
	Type    string          `json:"type"`
	Summary string          `json:"summary"`
	Refs    json.RawMessage `json:"refs"`
	At      time.Time       `json:"at"`
}

func ToGoalEvent(e db.GoalEvent) GoalEventView {
	return GoalEventView{GoalID: e.GoalID, Type: e.Type, Summary: e.Summary, Refs: e.Refs, At: e.At}
}

type AccountView struct {
	RunnerID        uuid.UUID       `json:"runner_id"`
	CLI             string          `json:"cli"`
	Mode            string          `json:"mode"`
	UsedTokens      int64           `json:"used_tokens"`
	CapTokensPerDay *int64          `json:"cap_tokens_per_day"`
	LimitedUntil    *time.Time      `json:"limited_until"`
	Utilization     json.RawMessage `json:"utilization"`
	Paused          bool            `json:"paused"`
	KeyFingerprint  string          `json:"key_fingerprint"`
	Enabled         bool            `json:"enabled"`
}
