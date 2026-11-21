package core

import (
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// JSON shapes shared by the REST API, board socket events and the runner protocol.

type WorkspaceView struct {
	ID             uuid.UUID `json:"id"`
	OrgID          uuid.UUID `json:"org_id"`
	Name           string    `json:"name"`
	About          string    `json:"about"`
	GoalCompletion string    `json:"goal_completion"` // manual or auto
	LogoURL        *string   `json:"logo_url"`
	CreatedAt      time.Time `json:"created_at"`
}

func ToWorkspace(w db.Workspace) WorkspaceView {
	return WorkspaceView{ID: w.ID, OrgID: w.OrgID, Name: w.Name, About: w.About, GoalCompletion: w.GoalCompletion, LogoURL: LogoURL("workspaces", w.ID, w.LogoKey), CreatedAt: w.CreatedAt}
}

type MemberView struct {
	ID             uuid.UUID  `json:"id"`
	WorkspaceID    uuid.UUID  `json:"workspace_id"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Skill          *string    `json:"skill"`
	ReportsTo      *uuid.UUID `json:"reports_to"`
	Independent    bool       `json:"independent"`
	Isolated       bool       `json:"isolated"`
	CreatedBy      *uuid.UUID `json:"created_by"`
	CLI            string     `json:"cli"`
	Model          string     `json:"model"`
	Instructions   string     `json:"instructions"`
	AvatarColor    string     `json:"avatar_color"`
	AllDirectories bool       `json:"all_directories"`
	Sleeping       bool       `json:"sleeping"`
	Position       int32      `json:"position"`
	Status         string     `json:"status"` // live status, see MemberStatus
	StatusDetail   string     `json:"status_detail,omitempty"`
}

func ToMember(m db.Member) MemberView {
	return MemberView{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name, Kind: m.Kind, Skill: m.Skill, ReportsTo: m.ReportsTo,
		Independent: m.Kind == "staff" && m.ReportsTo == nil, Isolated: m.Isolated, CreatedBy: m.CreatedBy, CLI: m.Cli, Model: m.Model, Instructions: m.Instructions,
		AvatarColor: m.AvatarColor, AllDirectories: m.AllDirectories, Sleeping: m.Sleeping, Position: m.Position, Status: "not_started"}
}

// Hidden is what a staff viewer receives for an agent outside their view: enough to draw a
// greyed placeholder, nothing more.
type HiddenMemberView struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	ReportsTo *uuid.UUID `json:"reports_to"`
	Hidden    bool       `json:"hidden"`
	Position  int32      `json:"position"`
}

func ToHiddenMember(m db.Member) HiddenMemberView {
	return HiddenMemberView{ID: m.ID, Kind: m.Kind, ReportsTo: m.ReportsTo, Hidden: true, Position: m.Position}
}

type DirectoryView struct {
	ID            uuid.UUID  `json:"id"`
	WorkspaceID   uuid.UUID  `json:"workspace_id"`
	Path          string     `json:"path"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	OwnerUserID   uuid.UUID  `json:"owner_user_id"`
	MachineID     *uuid.UUID `json:"machine_id"`
	IsRepo        bool       `json:"is_repo"`
	Remote        string     `json:"remote"`
	DefaultBranch string     `json:"default_branch"`
	SetupCommand  string     `json:"setup_command"`
	CopyFiles     []string   `json:"copy_files"`
}

func ToDirectory(d db.Directory) DirectoryView {
	v := DirectoryView{ID: d.ID, WorkspaceID: d.WorkspaceID, Path: d.Path, Name: d.Name, Kind: d.Kind, OwnerUserID: d.OwnerUserID, MachineID: d.MachineID,
		IsRepo: d.RepoRemote != "", Remote: d.RepoRemote, DefaultBranch: d.DefaultBranch, SetupCommand: d.SetupCommand, CopyFiles: d.CopyFiles}
	if v.CopyFiles == nil {
		v.CopyFiles = []string{}
	}
	return v
}

type CloneView struct {
	DirectoryID   uuid.UUID `json:"directory_id"`
	RunnerID      uuid.UUID `json:"runner_id"`
	UserID        uuid.UUID `json:"user_id"`
	Path          string    `json:"path"`
	CurrentBranch string    `json:"current_branch"`
	Dirty         int32     `json:"dirty"`
}

func ToClone(c db.DirectoryClone) CloneView {
	return CloneView{DirectoryID: c.DirectoryID, RunnerID: c.RunnerID, UserID: c.UserID, Path: c.Path, CurrentBranch: c.CurrentBranch, Dirty: c.Dirty}
}

type NoteView struct {
	ID             uuid.UUID  `json:"id"`
	Number         int32      `json:"number"`
	GoalID         *uuid.UUID `json:"goal_id"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	AuthorUserID   *uuid.UUID `json:"author_user_id"`
	AuthorMemberID *uuid.UUID `json:"author_member_id"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func ToNote(n db.Note) NoteView {
	return NoteView{ID: n.ID, Number: n.Number, GoalID: n.GoalID, Kind: n.Kind, Title: n.Title, Body: n.Body, AuthorUserID: n.AuthorUserID,
		AuthorMemberID: n.AuthorMemberID, UpdatedAt: n.UpdatedAt}
}

type GrantView struct {
	MemberID    uuid.UUID  `json:"member_id"`
	DirectoryID uuid.UUID  `json:"directory_id"`
	Mode        string     `json:"mode"`
	Source      string     `json:"source"`
	Reason      string     `json:"reason"`
	GrantedBy   *uuid.UUID `json:"granted_by"`
}

func ToGrant(g db.DirectoryGrant) GrantView {
	return GrantView{MemberID: g.MemberID, DirectoryID: g.DirectoryID, Mode: g.Mode, Source: g.Source, Reason: g.Reason, GrantedBy: g.GrantedBy}
}

type RunnerView struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	Hostname    string     `json:"hostname"`
	OS          string     `json:"os"`
	Version     string     `json:"version"`
	CLIs        []byte     `json:"-"`
	Detected    any        `json:"clis"`
	Sandbox     string     `json:"sandbox"`
	Online      bool       `json:"online"`
	Shared      bool       `json:"shared"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

type CLIStatus struct {
	CLI       string `json:"cli"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	LoggedIn  bool   `json:"logged_in"`
}

// SkillView is one skill in a workspace's library.
type SkillView struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	SourceKind string    `json:"source_kind"` // md or github
	SourceURL  string    `json:"source_url"`
	Content    string    `json:"content"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func ToSkill(s db.WorkspaceSkill) SkillView {
	return SkillView{ID: s.ID, Name: s.Name, SourceKind: s.SourceKind, SourceURL: s.SourceUrl, Content: s.Content, UpdatedAt: s.UpdatedAt}
}

func ToSkills(list []db.WorkspaceSkill) []SkillView {
	out := make([]SkillView, 0, len(list))
	for _, s := range list {
		out = append(out, ToSkill(s))
	}
	return out
}

// LogoURL is where a logo is served, or nil when none is set. The key's file name changes on every
// upload, so it doubles as a cache-busting version.
func LogoURL(kind string, id uuid.UUID, key *string) *string {
	if key == nil {
		return nil
	}
	u := fmt.Sprintf("/api/v1/%s/%s/logo?v=%s", kind, id, path.Base(*key))
	return &u
}

// DraftSkill is a skill a manager wrote or found for a draft.
type DraftSkill struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	SourceURL string `json:"source_url"`
}

// StaffProposalView is a manager's draft of a new staff member, or of new instructions for one.
type StaffProposalView struct {
	ID           uuid.UUID    `json:"id"`
	Kind         string       `json:"kind"` // new or update
	ManagerID    uuid.UUID    `json:"manager_id"`
	MemberID     *uuid.UUID   `json:"member_id"` // the staff member an update changes
	Name         string       `json:"name"`
	Role         *string      `json:"role"`
	Instructions string       `json:"instructions"`
	Skills       []DraftSkill `json:"skills"`
	Status       string       `json:"status"` // pending, approved or rejected
	Reason       string       `json:"reason"`
	Note         string       `json:"note"` // why the manager drafted it
	ResultMember *uuid.UUID   `json:"result_member_id"`
	DecidedAt    *time.Time   `json:"decided_at"`
	CreatedAt    time.Time    `json:"created_at"`
}

func ToStaffProposal(p db.StaffProposal) StaffProposalView {
	sk := []DraftSkill{}
	_ = json.Unmarshal(p.Skills, &sk)
	return StaffProposalView{ID: p.ID, Kind: p.Kind, ManagerID: p.ManagerID, MemberID: p.MemberID, Name: p.Name, Role: p.Role,
		Instructions: p.Instructions, Skills: sk, Status: p.Status, Reason: p.Reason, Note: p.Note, ResultMember: p.ResultMemberID,
		DecidedAt: p.DecidedAt, CreatedAt: p.CreatedAt}
}
