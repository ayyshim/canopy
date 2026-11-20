// Package protocol defines the messages exchanged between the hub and runners over
// /ws/runners (SPEC "API and realtime events" → Runner protocol). Both sides import it.
package protocol

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Version is bumped on incompatible changes; the hub rejects runners with another version.
const Version = 2

// Envelope wraps every message in both directions. ID is set on requests that expect a
// reply; the reply carries ReplyTo and either Data or Error.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	ReplyTo string          `json:"reply_to,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Hub → runner.
const (
	TStartSession = "start_session"
	TSendInput    = "send_input"
	TSleep        = "sleep"
	TStop         = "stop"
	TApplyGrants  = "apply_grants"
	TToolResult   = "tool_result" // answers a pending tool call (resolves a signal)
	TSetAPIKey    = "set_api_key"
	TBrowse       = "browse"
	TRevoked      = "revoked"
	TMirrorWatch  = "mirror_watch" // directories this machine owns and must sync up
	TMirrorApply  = "mirror_apply" // writes made elsewhere to apply to the owner's disk
	TMirrorFetch  = "mirror_fetch" // materialize a mirrored directory on this runner
	TRepoInfo     = "repo_info"    // is this folder a git repository, and its state
	TGitShip      = "git_ship"     // open a pull request, merge, or keep a goal's branch
	TGoalCleanup  = "goal_cleanup" // remove a goal's worktrees from this computer
)

// Runner → hub.
const (
	THello        = "hello"
	TSessionEvent = "session_event"
	TLogLine      = "log_line"
	TUsage        = "usage"
	TRateLimit    = "rate_limit"
	TToolCall     = "tool_call"
	TFileWritten  = "file_written"
	THeartbeat    = "heartbeat"
	TReply        = "reply"
	TMirrorChange = "mirror_change" // files changed in a synced directory (manifest delta)
	TMirrorResult = "mirror_result" // outcome of applying queued writes on the owner's disk
)

type CLIStatus struct {
	CLI       string `json:"cli"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path,omitempty"`
	LoggedIn  bool   `json:"logged_in"`
	HasAPIKey bool   `json:"has_api_key"`
}

type LiveSession struct {
	SessionID    uuid.UUID `json:"session_id"`
	CLISessionID string    `json:"cli_session_id"`
	Status       string    `json:"status"`
}

type Hello struct {
	Version       int           `json:"version"`
	ClientVersion string        `json:"client_version"`
	Hostname      string        `json:"hostname"`
	OS            string        `json:"os"`
	Arch          string        `json:"arch"`
	CLIs          []CLIStatus   `json:"clis"`
	Sandbox       string        `json:"sandbox"` // bwrap, docker or none
	Sessions      []LiveSession `json:"sessions"`
}

// Mount is one path visible inside a member's sandbox. Path is where the agent sees it (the
// directory's original path); Source is where it lives on the runner (differs for mirrors).
// Repository mounts set Repo instead: the client puts a worktree at <work folder>/<Repo.Name>.
type Mount struct {
	Path        string     `json:"path"`
	Source      string     `json:"source,omitempty"`
	Mode        string     `json:"mode"` // ro or rw
	DirectoryID *uuid.UUID `json:"directory_id,omitempty"`
	Mirror      bool       `json:"mirror,omitempty"`
	Repo        *RepoMount `json:"repo,omitempty"`
}

// RepoMount is a goal's branch of a git repository, checked out for one task.
type RepoMount struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	Name        string    `json:"name"`            // folder name inside the work folder
	Remote      string    `json:"remote"`          // clone from here when ClonePath is empty
	ClonePath   string    `json:"clone_path"`      // an existing clone on this computer
	Base        string    `json:"base"`            // branch the goal starts from ("" = default branch)
	GoalBranch  string    `json:"goal_branch"`     // canopy/<goal>
	TaskBranch  string    `json:"task_branch"`     // canopy/<goal>--<task>; "" for read-only
	Snapshot    bool      `json:"snapshot"`        // start from the clone's uncommitted changes
	BundleKey   string    `json:"bundle_key"`      // hub backup of the goal branch, to restore from
	BundleTip   string    `json:"bundle_tip"`      // the commit that backup ends at
	Setup       string    `json:"setup,omitempty"` // command to run once in a new worktree
	CopyFiles   []string  `json:"copy_files,omitempty"`
}

// RepoResult is a goal branch after a task's work merged into it.
type RepoResult struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	Base        string    `json:"base"`
	BaseSHA     string    `json:"base_sha"`
	TipSHA      string    `json:"tip_sha"`
	Commits     int       `json:"commits"`    // commits on the goal branch since its base
	BundleKey   string    `json:"bundle_key"` // the uploaded backup
	DiffKey     string    `json:"diff_key"`   // this task's changes as a patch (blob key)
	DiffSize    int64     `json:"diff_size"`
	Files       int       `json:"files"`
}

type RepoInfo struct {
	Path string `json:"path"`
}

type RepoInfoResult struct {
	IsRepo        bool   `json:"is_repo"`
	Remote        string `json:"remote"`
	DefaultBranch string `json:"default_branch"`
	CurrentBranch string `json:"current_branch"`
	Dirty         int    `json:"dirty"` // files with uncommitted changes
}

// GitShip asks the goal's computer to finish a goal's branches.
type GitShip struct {
	GoalID uuid.UUID   `json:"goal_id"`
	Action string      `json:"action"` // pr, merge or keep
	Title  string      `json:"title"`
	Body   string      `json:"body"`
	Repos  []RepoMount `json:"repos"`
}

type ShipResult struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	PRURL       string    `json:"pr_url,omitempty"`
	Pushed      bool      `json:"pushed,omitempty"`
	Merged      bool      `json:"merged,omitempty"`
	Conflicts   []string  `json:"conflicts,omitempty"` // files that conflict with the base
	Dirty       bool      `json:"dirty,omitempty"`     // merge refused: uncommitted changes on the base
	Error       string    `json:"error,omitempty"`
}

type GitShipResult struct {
	Repos []ShipResult `json:"repos"`
}

type GoalCleanup struct {
	GoalID uuid.UUID `json:"goal_id"`
}

// Turn is one input to an agent; usage it causes is attributed to AttributedUserID.
type Turn struct {
	TurnID           string     `json:"turn_id"`
	Kind             string     `json:"kind"` // task, chat, nudge, system
	Text             string     `json:"text"`
	TaskID           *uuid.UUID `json:"task_id,omitempty"`
	AttributedUserID *uuid.UUID `json:"attributed_user_id,omitempty"`
	CommentTaskID    *uuid.UUID `json:"comment_task_id,omitempty"` // comment turns: the task whose thread gets the reply
}

type StartSession struct {
	SessionID          uuid.UUID `json:"session_id"`
	WorkspaceID        uuid.UUID `json:"workspace_id"`
	MemberID           uuid.UUID `json:"member_id"`
	MemberName         string    `json:"member_name"`
	CLI                string    `json:"cli"`
	Model              string    `json:"model"`
	Resume             string    `json:"resume,omitempty"` // CLI session to continue
	AppendSystemPrompt string    `json:"append_system_prompt"`
	Work               string    `json:"work"` // this session's folder name; the client keeps it under its data folder
	Mounts             []Mount   `json:"mounts"`
	AllowedHosts       []string  `json:"allowed_hosts,omitempty"` // extra network access granted by a gate
	UseAPIKey          bool      `json:"use_api_key,omitempty"`   // sign in with the stored API key instead of the subscription
	Turn               *Turn     `json:"turn,omitempty"`
}

// SendInput gives a running session its next turn. Repository mounts in Mounts are switched
// to the turn's task branch first; they live inside the work folder, so no restart is needed.
type SendInput struct {
	SessionID uuid.UUID `json:"session_id"`
	Turn      Turn      `json:"turn"`
	Mounts    []Mount   `json:"mounts,omitempty"`
}

type SessionRef struct {
	SessionID uuid.UUID `json:"session_id"`
}

// ApplyGrants restarts a session (resuming its conversation) with new mounts, then sends Turn.
type ApplyGrants struct {
	SessionID uuid.UUID `json:"session_id"`
	Mounts    []Mount   `json:"mounts"`
	Work      string    `json:"work"`
	Turn      *Turn     `json:"turn,omitempty"`
}

// ToolResult answers a ToolCall. For the permission tool, Allow decides.
type ToolResult struct {
	SessionID uuid.UUID `json:"session_id"`
	CallID    string    `json:"call_id"`
	Text      string    `json:"text"`
	IsError   bool      `json:"is_error,omitempty"`
	Allow     *bool     `json:"allow,omitempty"`
	Restart   bool      `json:"restart,omitempty"` // restart the session (resume) after answering, e.g. new mounts
}

type SetAPIKey struct {
	CLI string `json:"cli"`
	Key string `json:"key"`
}

type Browse struct {
	Path string `json:"path"`
}

type BrowseEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	IsRepo bool   `json:"is_repo,omitempty"`
}

type BrowseResult struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Home    string        `json:"home"`
	Entries []BrowseEntry `json:"entries"`
}

// Session statuses the runner reports.
const (
	SessionRunning = "running" // working on a turn
	SessionWaiting = "waiting" // turn finished, idle
	SessionAsleep  = "asleep"  // process stopped, conversation kept
	SessionCrashed = "crashed"
	SessionEnded   = "ended"
	SessionError   = "error"
)

type SessionEvent struct {
	SessionID     uuid.UUID  `json:"session_id"`
	Status        string     `json:"status"`
	CLISessionID  string     `json:"cli_session_id,omitempty"`
	TurnID        string     `json:"turn_id,omitempty"`
	TurnKind      string     `json:"turn_kind,omitempty"`
	TaskID        *uuid.UUID `json:"task_id,omitempty"`
	CommentTaskID *uuid.UUID `json:"comment_task_id,omitempty"`
	ResultText    string     `json:"result_text,omitempty"` // the agent's final text for the turn
	IsError       bool       `json:"is_error,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type LogLine struct {
	SessionID uuid.UUID `json:"session_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	Kind      string    `json:"kind"` // agent, tool, error, system
	Text      string    `json:"text"`
}

type Usage struct {
	SessionID        uuid.UUID  `json:"session_id"`
	TurnID           string     `json:"turn_id,omitempty"`
	AttributedUserID *uuid.UUID `json:"attributed_user_id,omitempty"`
	CLI              string     `json:"cli"`
	Model            string     `json:"model"`
	TokensIn         int64      `json:"tokens_in"`
	TokensOut        int64      `json:"tokens_out"`
}

// RateWindow is one limit window of the CLI's sign-in. Utilization is 0..1.
type RateWindow struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
	Status      string     `json:"status,omitempty"`
}

// RateLimit is the CLI's own report of its sign-in's limits. Limited means work stopped.
type RateLimit struct {
	SessionID   uuid.UUID             `json:"session_id"`
	CLI         string                `json:"cli"`
	Limited     bool                  `json:"limited"`
	ResetsAt    *time.Time            `json:"resets_at,omitempty"`
	Utilization map[string]float64    `json:"utilization,omitempty"` // e.g. five_hour: 0.37
	Windows     map[string]RateWindow `json:"windows,omitempty"`     // each window with its reset time and status
	Message     string                `json:"message,omitempty"`
}

// ToolCall is an agent calling a Canopy tool; the hub answers with a ToolResult.
type ToolCall struct {
	SessionID uuid.UUID       `json:"session_id"`
	TurnID    string          `json:"turn_id,omitempty"`
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
}

type FileWritten struct {
	SessionID uuid.UUID  `json:"session_id"`
	TaskID    *uuid.UUID `json:"task_id,omitempty"`
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	SHA256    string     `json:"sha256"`
	Mime      string     `json:"mime"`
	BlobKey   string     `json:"blob_key"` // set after the runner uploaded it
}

// Canopy tool names (exposed to agents through the `canopy` MCP server).
const (
	ToolPermission     = "permission"
	ToolAskQuestion    = "ask_question"
	ToolRequestDir     = "request_directory"
	ToolReportProgress = "report_progress"
	ToolSubmitOutput   = "submit_output"
	ToolCompleteTask   = "complete_task"
	ToolSubmitPlan     = "submit_plan"
	ToolAnswerQuestion = "answer_question"
	ToolOwnerChange    = "request_owner_change"
	ToolReadNotes      = "read_notes"
	ToolWriteNote      = "write_note"
	ToolAssignSkill    = "assign_skill"
)

// ---- Directory mirror (DECISIONS: Directories) ----

type FileEntry struct {
	Path    string `json:"path"` // slash-separated, relative to the directory
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	Deleted bool   `json:"deleted,omitempty"`
}

type MirrorDir struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	Path        string    `json:"path"`
}

// MirrorWatch tells an owner machine which directories to sync up.
type MirrorWatch struct {
	Directories []MirrorDir `json:"directories"`
}

// MirrorChange reports changed files; Full means Files is the whole tree.
type MirrorChange struct {
	DirectoryID uuid.UUID   `json:"directory_id"`
	Full        bool        `json:"full"`
	Files       []FileEntry `json:"files"`
	Missing     bool        `json:"missing,omitempty"` // the directory isn't on this machine
}

type PendingWrite struct {
	ID      uuid.UUID `json:"id"`
	Path    string    `json:"path"`
	BaseSHA string    `json:"base_sha"` // "" = file must not exist yet
	NewSHA  string    `json:"new_sha"`  // "" = delete
	Mode    uint32    `json:"mode"`
}

// MirrorApply asks the owner machine to apply queued writes.
type MirrorApply struct {
	DirectoryID uuid.UUID      `json:"directory_id"`
	Writes      []PendingWrite `json:"writes"`
}

type WriteOutcome struct {
	ID         uuid.UUID `json:"id"`
	Status     string    `json:"status"` // applied or conflict
	CurrentSHA string    `json:"current_sha,omitempty"`
}

type MirrorResult struct {
	DirectoryID uuid.UUID      `json:"directory_id"`
	Outcomes    []WriteOutcome `json:"outcomes"`
}

// MirrorFetch asks a runner to materialize a mirrored directory (Full) or apply changes to its
// copy. The runner replies with MirrorFetched.
type MirrorFetch struct {
	DirectoryID uuid.UUID   `json:"directory_id"`
	Path        string      `json:"path"`
	Full        bool        `json:"full"`
	Files       []FileEntry `json:"files"`
}

type MirrorFetched struct {
	LocalPath string `json:"local_path"`
}

type BlobsMissing struct {
	SHA256 []string `json:"sha256"`
}
