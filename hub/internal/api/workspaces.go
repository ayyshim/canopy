package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/skills"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

func (s *Server) handleListCLIs(w http.ResponseWriter, _ *http.Request) error {
	httpx.JSON(w, http.StatusOK, map[string]any{"clis": domain.CLIs, "skills": domain.Skills})
	return nil
}

type workspaceSummary struct {
	core.WorkspaceView
	Members     int64  `json:"members"`
	Directories int64  `json:"directories"`
	People      int64  `json:"people"`
	Role        string `json:"role"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	m, err := authz.OrgMember(ctx, s.Store.Queries, orgID, uid)
	if err != nil {
		return err
	}
	var list []db.Workspace
	if authz.CanAdministerOrg(m.OrgRole) {
		list, err = s.Store.ListOrgWorkspaces(ctx, orgID)
	} else {
		list, err = s.Store.ListUserWorkspaces(ctx, db.ListUserWorkspacesParams{OrgID: orgID, UserID: uid})
	}
	if err != nil {
		return err
	}
	out := make([]workspaceSummary, 0, len(list))
	for _, ws := range list {
		c, err := s.Store.WorkspaceCounts(ctx, ws.ID)
		if err != nil {
			return err
		}
		v, err := authz.WorkspaceViewer(ctx, s.Store.Queries, ws, uid)
		if err != nil {
			return err
		}
		out = append(out, workspaceSummary{WorkspaceView: core.ToWorkspace(ws), Members: c.Members, Directories: c.Directories, People: c.People, Role: v.Role})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"workspaces": out})
	return nil
}

// ---- Workspace setup (SPEC "Workspace setup"): one POST with the reviewed payload. ----

type setupCLI struct {
	CLI             string `json:"cli"`
	Enabled         bool   `json:"enabled"`
	Mode            string `json:"mode"`
	CapTokensPerDay *int64 `json:"cap_tokens_per_day"`
	APIKey          string `json:"api_key"` // relayed to the runner, never stored here
}

type setupGrant struct {
	DirectoryKey string `json:"directory_key"`
	Mode         string `json:"mode"`
	Source       string `json:"source"`
	Reason       string `json:"reason"`
}

type setupMember struct {
	Key            string       `json:"key"`
	Name           string       `json:"name"`
	Kind           string       `json:"kind"`
	Skill          string       `json:"skill"`
	ReportsToKey   string       `json:"reports_to_key"`
	CLI            string       `json:"cli"`
	Model          string       `json:"model"`
	AllDirectories bool         `json:"all_directories"`
	Grants         []setupGrant `json:"grants"`
	Instructions   string       `json:"instructions"`
	SkillKeys      []string     `json:"skill_keys"` // library skills (setupSkill keys) this member starts with
}

// setupSkill is a library skill added during setup. GitHub skills arrive already fetched
// (through the preview endpoint) so the review page shows exactly what is stored.
type setupSkill struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	SourceKind string `json:"source_kind"`
	SourceURL  string `json:"source_url"`
	Content    string `json:"content"`
}

type setupPerson struct {
	UserID           *uuid.UUID `json:"user_id"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	ManagerKeys      []string   `json:"manager_keys"`
	IndependentStaff bool       `json:"independent_staff"`
}

type setupPayload struct {
	Workspace struct {
		Name           string `json:"name"`
		About          string `json:"about"`
		GoalCompletion string `json:"goal_completion"`
	} `json:"workspace"`
	RunnerID    *uuid.UUID `json:"runner_id"` // the creator's computer: their directories and goals live there
	Directories []struct {
		Key  string `json:"key"`
		Path string `json:"path"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"directories"`
	CLIs      []setupCLI    `json:"clis"`
	Skills    []setupSkill  `json:"skills"`
	Members   []setupMember `json:"members"`
	People    []setupPerson `json:"people"`
	FirstGoal *struct {
		ManagerKey string `json:"manager_key"`
		Title      string `json:"title"`
		DoneWhen   string `json:"done_when"`
	} `json:"first_goal"`
}

func validAbsPath(p string) bool {
	p = strings.TrimSpace(p)
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/") || (len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.Contains(p, "\\") {
		return strings.TrimRight(p, "\\")
	}
	if p == "/" {
		return p
	}
	return strings.TrimRight(path.Clean(p), "/")
}

func validMode(m string) bool { return m == domain.Read || m == domain.ReadWrite }

// validateSetup checks the whole payload; field keys mirror the payload paths so the wizard can
// send people back to the right step.
func (s *Server) validateSetup(ctx context.Context, orgID uuid.UUID, p *setupPayload, enabledCLIs map[string]bool) error {
	v := httpx.Validation{}
	p.Workspace.Name = strings.TrimSpace(p.Workspace.Name)
	p.Workspace.About = strings.TrimSpace(p.Workspace.About)
	if p.Workspace.Name == "" {
		v.Add("workspace.name", "Enter a name for the workspace.")
	} else if utf8.RuneCountInString(p.Workspace.Name) > 80 {
		v.Add("workspace.name", "Use at most 80 characters.")
	}
	if p.Workspace.GoalCompletion == "" {
		p.Workspace.GoalCompletion = "manual"
	} else if p.Workspace.GoalCompletion != "manual" && p.Workspace.GoalCompletion != "auto" {
		v.Add("workspace.goal_completion", "Choose when goals finish.")
	}
	if p.RunnerID != nil {
		rn, err := s.Store.GetRunner(ctx, *p.RunnerID)
		if err != nil || rn.OrgID != orgID || rn.RevokedAt != nil {
			v.Add("runner_id", "Choose a computer connected to this organization.")
		}
	}

	dirKeys := map[string]bool{}
	seenPaths := map[string]bool{}
	for i := range p.Directories {
		d := &p.Directories[i]
		f := fmt.Sprintf("directories.%d.", i)
		if d.Key == "" || dirKeys[d.Key] {
			v.Add(f+"key", "Each directory needs its own key.")
		}
		dirKeys[d.Key] = true
		if !validAbsPath(d.Path) {
			v.Add(f+"path", "Enter a full path, like /home/you/payments.")
			continue
		}
		d.Path = cleanPath(d.Path)
		if seenPaths[d.Path] {
			v.Add(f+"path", "This directory is already in the list.")
		}
		seenPaths[d.Path] = true
		if strings.TrimSpace(d.Name) == "" {
			d.Name = domain.DirName(d.Path)
		}
		if d.Kind == "" {
			d.Kind = domain.ClassifyDir(d.Path)
		}
		if d.Kind != "code" && d.Kind != "docs" && d.Kind != "design" && d.Kind != "other" {
			v.Add(f+"kind", "Pick code, docs, design or other.")
		}
	}

	for i, c := range p.CLIs {
		f := fmt.Sprintf("clis.%d.", i)
		info, ok := domain.CLI(c.CLI)
		if !ok {
			v.Add(f+"cli", "Unknown CLI.")
			continue
		}
		if c.Enabled && !info.Supported {
			v.Add(f+"cli", info.Name+" isn’t supported yet.")
		}
		if c.Mode != "subscription" && c.Mode != "api_key" {
			v.Add(f+"mode", "Choose subscription or API key.")
		}
		if c.CapTokensPerDay != nil && *c.CapTokensPerDay <= 0 {
			v.Add(f+"cap_tokens_per_day", "Use a number above zero, or leave it empty.")
		}
		if c.Enabled {
			enabledCLIs[c.CLI] = true
		}
	}
	if len(p.CLIs) > 0 && len(enabledCLIs) == 0 {
		v.Add("clis", "Turn on at least one CLI.")
	}

	memberKeys := map[string]*setupMember{}
	managers := 0
	for i := range p.Members {
		m := &p.Members[i]
		if m.Key == "" || memberKeys[m.Key] != nil {
			v.Add(fmt.Sprintf("members.%d.key", i), "Each member needs its own key.")
		}
		memberKeys[m.Key] = m
		if m.Kind == "manager" {
			managers++
		}
	}
	if managers == 0 {
		v.Add("members", "Add at least one manager.")
	}
	skillKeys := map[string]bool{}
	skillNames := map[string]bool{}
	for i := range p.Skills {
		sk := &p.Skills[i]
		f := fmt.Sprintf("skills.%d.", i)
		sk.Name = strings.TrimSpace(sk.Name)
		if sk.Key == "" || skillKeys[sk.Key] {
			v.Add(f+"key", "Each skill needs its own key.")
		}
		skillKeys[sk.Key] = true
		if sk.Name == "" || utf8.RuneCountInString(sk.Name) > maxSkillName {
			v.Add(f+"name", "Give the skill a name under 80 characters.")
		} else if skillNames[strings.ToLower(sk.Name)] {
			v.Add(f+"name", "Two skills can’t share a name.")
		}
		skillNames[strings.ToLower(sk.Name)] = true
		if sk.SourceKind != "md" && sk.SourceKind != "github" {
			v.Add(f+"source_kind", "Choose a markdown file or a GitHub link.")
		}
		if sk.SourceKind == "github" {
			if _, err := skills.RawURL(sk.SourceURL); err != nil {
				v.Add(f+"source_url", err.Error())
			}
		}
		if sk.SourceKind == "md" {
			sk.SourceURL = ""
		}
		if c, err := skills.Check(sk.Content); err != nil {
			v.Add(f+"content", err.Error())
		} else {
			sk.Content = c
		}
	}
	grantsOf := map[string][]domain.Grant{} // by member key, directory keys mapped to fake IDs
	keyID := map[string]uuid.UUID{}
	for k := range dirKeys {
		keyID[k] = uuid.New()
	}
	for i := range p.Members {
		m := &p.Members[i]
		f := fmt.Sprintf("members.%d.", i)
		m.Name = strings.TrimSpace(m.Name)
		if m.Name == "" {
			v.Add(f+"name", "Give this member a name.")
		}
		switch m.Kind {
		case "manager":
			if m.ReportsToKey != "" {
				v.Add(f+"reports_to_key", "Managers don’t report to anyone.")
			}
			if m.Skill != "" && !domain.ValidSkill(m.Skill) {
				v.Add(f+"skill", "Pick a skill.")
			}
		case "staff":
			if !domain.ValidSkill(m.Skill) {
				v.Add(f+"skill", "Pick a skill.")
			}
			if m.ReportsToKey != "" {
				if mgr := memberKeys[m.ReportsToKey]; mgr == nil || mgr.Kind != "manager" {
					v.Add(f+"reports_to_key", "Choose a manager this member works for.")
				}
			}
			m.AllDirectories = false
		default:
			v.Add(f+"kind", "Choose manager or staff.")
		}
		for _, k := range m.SkillKeys {
			if !skillKeys[k] {
				v.Add(f+"skill_keys", "Unknown skill.")
			}
		}
		if len(enabledCLIs) > 0 && !enabledCLIs[m.CLI] {
			v.Add(f+"cli", "Choose a CLI that’s turned on.")
		} else if _, ok := domain.CLI(m.CLI); !ok {
			v.Add(f+"cli", "Choose a CLI.")
		}
		if m.Model == "" {
			m.Model = domain.DefaultModel(m.CLI, m.Kind)
		} else if !domain.ValidModel(m.CLI, m.Model) {
			v.Add(f+"model", "Choose a model this CLI offers.")
		}
		seen := map[string]bool{}
		for j, g := range m.Grants {
			gf := fmt.Sprintf("%sgrants.%d.", f, j)
			if !dirKeys[g.DirectoryKey] || seen[g.DirectoryKey] {
				v.Add(gf+"directory_key", "Unknown or repeated directory.")
				continue
			}
			seen[g.DirectoryKey] = true
			if !validMode(g.Mode) {
				v.Add(gf+"mode", "Choose read or read and write.")
			}
			if g.Source != "suggested" && g.Source != "human" {
				m.Grants[j].Source = "human"
			}
			grantsOf[m.Key] = append(grantsOf[m.Key], domain.Grant{DirectoryID: keyID[g.DirectoryKey], Mode: g.Mode})
		}
		if m.Kind == "manager" && m.AllDirectories {
			all := make([]domain.Grant, 0, len(keyID))
			for _, id := range keyID {
				all = append(all, domain.Grant{DirectoryID: id, Mode: domain.ReadWrite})
			}
			grantsOf[m.Key] = all
		}
	}
	// Staff grants are always a subset of their manager's.
	for i, m := range p.Members {
		if m.Kind != "staff" || m.ReportsToKey == "" || memberKeys[m.ReportsToKey] == nil {
			continue
		}
		if _, ok := domain.WithinLimit(grantsOf[m.Key], grantsOf[m.ReportsToKey]); !ok {
			v.Add(fmt.Sprintf("members.%d.grants", i), "Staff can only reach directories their manager can, with the same or less access.")
		}
	}

	for i := range p.People {
		pp := &p.People[i]
		f := fmt.Sprintf("people.%d.", i)
		if pp.Role != "admin" && pp.Role != "staff" {
			v.Add(f+"role", "Choose admin or staff.")
		}
		for _, k := range pp.ManagerKeys {
			if mm := memberKeys[k]; mm == nil || mm.Kind != "manager" {
				v.Add(f+"manager_keys", "Choose managers from this workspace.")
			}
		}
		if pp.UserID == nil {
			pp.Email = normalizeEmail(pp.Email)
			if !validEmail(pp.Email) {
				v.Add(f+"email", "Enter a valid email.")
			}
		} else if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, *pp.UserID); err != nil {
			v.Add(f+"user_id", "This person isn’t in the organization.")
		}
	}
	if g := p.FirstGoal; g != nil {
		g.Title = strings.TrimSpace(g.Title)
		if mm := memberKeys[g.ManagerKey]; mm == nil || mm.Kind != "manager" {
			v.Add("first_goal.manager_key", "Pick a manager for the goal.")
		}
		if g.Title == "" {
			v.Add("first_goal.title", "Describe the goal.")
		}
	}
	return v.Err()
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, id.UserID); err != nil {
		return err
	}
	var p setupPayload
	if err := httpx.Decode(r, &p); err != nil {
		return err
	}
	enabled := map[string]bool{}
	if len(p.CLIs) == 0 {
		conns, err := s.Store.ListOrgConnections(ctx, orgID)
		if err != nil {
			return err
		}
		for _, c := range conns {
			if c.Enabled {
				enabled[c.Cli] = true
			}
		}
	}
	if err := s.validateSetup(ctx, orgID, &p, enabled); err != nil {
		return err
	}
	org, err := s.Store.GetOrganization(ctx, orgID)
	if err != nil {
		return err
	}
	inviter, err := s.Store.GetUserByID(ctx, id.UserID)
	if err != nil {
		return err
	}

	// Directories come from the creator's computer: the one they connected during setup.
	machineID := s.personComputer(ctx, orgID, id.UserID, p.RunnerID)
	var machine db.Runner
	if machineID != nil {
		if machine, err = s.Store.GetRunner(ctx, *machineID); err != nil {
			return err
		}
	}
	repos := map[string]protocol.RepoInfoResult{}
	for _, d := range p.Directories {
		repos[d.Key] = s.detectRepo(ctx, machineID, d.Path)
	}

	type pendingInvite struct{ email, token string }
	var invites []pendingInvite
	var keys []relayKey
	var ws db.Workspace
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		var err error
		// The CLI step sets how the creator's computer signs in.
		for _, c := range p.CLIs {
			if machineID == nil {
				break
			}
			fp := ""
			if c.APIKey != "" {
				fp = keyFingerprint(c.APIKey)
				keys = append(keys, relayKey{cli: c.CLI, key: c.APIKey})
			}
			if _, err := q.UpsertCliConnection(ctx, db.UpsertCliConnectionParams{
				ID: newID(), OrgID: orgID, RunnerID: *machineID, Cli: c.CLI, Mode: c.Mode, CapTokensPerDay: c.CapTokensPerDay, KeyFingerprint: fp, Enabled: c.Enabled,
			}); err != nil {
				return err
			}
			if c.Enabled {
				if _, err := q.EnsureAccount(ctx, db.EnsureAccountParams{ID: newID(), OrgID: orgID, RunnerID: *machineID, Cli: c.CLI, Mode: c.Mode}); err != nil {
					return err
				}
			}
		}
		if machineID != nil && machine.Kind == "computer" {
			if err := q.SetDefaultRunnerIfEmpty(ctx, db.SetDefaultRunnerIfEmptyParams{OrgID: orgID, UserID: id.UserID, DefaultRunnerID: machineID}); err != nil {
				return err
			}
		}
		ws, err = q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			ID: newID(), OrgID: orgID, Name: p.Workspace.Name, About: p.Workspace.About, CreatedBy: &id.UserID,
		})
		if err != nil {
			return err
		}
		if p.Workspace.GoalCompletion != "manual" {
			ws, err = q.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: ws.ID, GoalCompletion: &p.Workspace.GoalCompletion})
		}
		if err != nil {
			return err
		}
		dirIDs := map[string]uuid.UUID{}
		for _, d := range p.Directories {
			repo := repos[d.Key]
			params := db.CreateDirectoryParams{ID: newID(), WorkspaceID: ws.ID, Path: d.Path, Name: d.Name, Kind: d.Kind, OwnerUserID: id.UserID, MachineID: machineID}
			if repo.IsRepo && repo.Remote != "" {
				params.RepoRemote, params.DefaultBranch = repo.Remote, repo.DefaultBranch
			}
			dir, err := q.CreateDirectory(ctx, params)
			if err == nil && dir.RepoRemote != "" && machineID != nil {
				_, err = q.UpsertClone(ctx, db.UpsertCloneParams{ID: newID(), DirectoryID: dir.ID, RunnerID: *machineID, UserID: id.UserID, Path: dir.Path,
					CurrentBranch: repo.CurrentBranch, Dirty: int32(repo.Dirty)})
			}
			if err != nil {
				return err
			}
			dirIDs[d.Key] = dir.ID
		}
		memberIDs := map[string]uuid.UUID{}
		// Managers first so staff can point at them.
		ordered := append([]setupMember{}, p.Members...)
		pos := 0
		skillIDs := map[string]uuid.UUID{}
		for _, sk := range p.Skills {
			row, err := q.CreateWorkspaceSkill(ctx, db.CreateWorkspaceSkillParams{ID: newID(), WorkspaceID: ws.ID, Name: sk.Name,
				SourceKind: sk.SourceKind, SourceUrl: strings.TrimSpace(sk.SourceURL), Content: sk.Content})
			if err != nil {
				return err
			}
			skillIDs[sk.Key] = row.ID
		}
		for _, pass := range []string{"manager", "staff"} {
			for _, m := range ordered {
				if m.Kind != pass {
					continue
				}
				var reports *uuid.UUID
				if m.ReportsToKey != "" {
					mid := memberIDs[m.ReportsToKey]
					reports = &mid
				}
				var skill *string
				if m.Skill != "" {
					sk := m.Skill
					skill = &sk
				}
				mem, err := q.CreateMember(ctx, db.CreateMemberParams{
					ID: newID(), WorkspaceID: ws.ID, Name: m.Name, Kind: m.Kind, Skill: skill, ReportsTo: reports,
					Cli: m.CLI, Model: m.Model, AvatarColor: domain.AvatarColors[pos%len(domain.AvatarColors)],
					AllDirectories: m.AllDirectories, Position: int32(pos), Instructions: m.Instructions,
				})
				if err != nil {
					return err
				}
				pos++
				memberIDs[m.Key] = mem.ID
				for _, k := range m.SkillKeys {
					if err := q.AttachMemberSkill(ctx, db.AttachMemberSkillParams{MemberID: mem.ID, SkillID: skillIDs[k]}); err != nil {
						return err
					}
				}
				if m.AllDirectories {
					for _, dirID := range dirIDs {
						if _, err := q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: mem.ID, DirectoryID: dirID,
							Mode: domain.ReadWrite, Source: "human", GrantedBy: &id.UserID}); err != nil {
							return err
						}
					}
					continue
				}
				for _, g := range m.Grants {
					var by *uuid.UUID
					if g.Source == "human" {
						by = &id.UserID
					}
					if _, err := q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: mem.ID, DirectoryID: dirIDs[g.DirectoryKey],
						Mode: g.Mode, Source: g.Source, Reason: g.Reason, GrantedBy: by}); err != nil {
						return err
					}
				}
			}
		}
		for _, pp := range p.People {
			mgrs := make([]uuid.UUID, 0, len(pp.ManagerKeys))
			for _, k := range pp.ManagerKeys {
				mgrs = append(mgrs, memberIDs[k])
			}
			if pp.UserID != nil {
				if _, err := q.UpsertWorkspaceAccess(ctx, db.UpsertWorkspaceAccessParams{ID: newID(), WorkspaceID: ws.ID, UserID: *pp.UserID,
					Role: pp.Role, ManagerIds: mgrs, IndependentStaff: pp.IndependentStaff}); err != nil {
					return err
				}
				continue
			}
			grants, _ := json.Marshal([]inviteGrant{{WorkspaceID: ws.ID, Role: pp.Role, ManagerIDs: mgrs, IndependentStaff: pp.IndependentStaff}})
			raw, hash := auth.NewToken()
			if _, err := q.CreateInvite(ctx, db.CreateInviteParams{ID: newID(), OrgID: orgID, Email: pp.Email, OrgRole: "member",
				TokenHash: hash, InvitedBy: &id.UserID, ExpiresAt: s.Now().Add(inviteTTL), WorkspaceGrants: grants}); err != nil {
				return err
			}
			invites = append(invites, pendingInvite{email: pp.Email, token: raw})
		}
		if g := p.FirstGoal; g != nil {
			if err := s.createGoal(ctx, t, ws, memberIDs[g.ManagerKey], g.Title, g.DoneWhen, id.UserID); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, q, &orgID, &id.UserID, "workspace.created", fmt.Sprintf("%s created the workspace %s", inviter.Name, ws.Name)); err != nil {
			return err
		}
		return t.Emit(ctx, core.Org(orgID, "workspace.created"), core.ToWorkspace(ws))
	})
	if err != nil {
		return err
	}
	for _, inv := range invites {
		s.sendInvite(ctx, inv.email, inv.token, inviter.Name, org.Name, ws.Name)
	}
	if machineID != nil {
		for _, k := range keys {
			s.relayKey(ctx, *machineID, k.cli, k.key)
		}
		s.rewatch(ctx, *machineID)
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"workspace": core.ToWorkspace(ws)})
	return nil
}

type relayKey struct{ cli, key string }

// keyFingerprint keeps only the last 4 characters (SPEC "Keys and secrets").
func keyFingerprint(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return "…" + key
	}
	return "…" + key[len(key)-4:]
}

const inviteTTL = 7 * 24 * time.Hour

func (s *Server) sendInvite(ctx context.Context, email, token, inviter, org, workspace string) {
	text := fmt.Sprintf("%s invited you to %s on Canopy", inviter, org)
	if workspace != "" {
		text += fmt.Sprintf(", starting in the %s workspace", workspace)
	}
	text += ".\n\nAccept the invitation: " + s.Cfg.PublicURL + "/invite/" + token + "\n\nThe link expires in 7 days.\n"
	if err := s.Mail.Send(ctx, mail.Message{To: email, Subject: inviter + " invited you to " + org + " on Canopy", Text: text}); err != nil {
		httpxLogError(ctx, "send invite", err)
	}
}

// ---- Workspace detail ----

type workspaceDetail struct {
	Workspace   core.WorkspaceView `json:"workspace"`
	Viewer      viewerView         `json:"viewer"`
	Members     []any              `json:"members"`
	Directories []directoryDetail  `json:"directories"`
	Grants      []core.GrantView   `json:"grants"`
	Clones      []core.CloneView   `json:"clones"`
}

type viewerView struct {
	Role        string      `json:"role"`
	OrgRole     string      `json:"org_role"`
	ManagerIDs  []uuid.UUID `json:"manager_ids"`
	Independent bool        `json:"independent"`
}

type directoryDetail struct {
	core.DirectoryView
	OwnerName string            `json:"owner_name"`
	YourMode  string            `json:"your_mode"`
	Access    map[string]string `json:"access,omitempty"` // admins: explicit person modes by user id
}

func toViewer(v authz.Viewer) viewerView {
	vv := viewerView{Role: v.Role, OrgRole: v.OrgRole, Independent: v.Independent, ManagerIDs: []uuid.UUID{}}
	for id := range v.Managers {
		vv.ManagerIDs = append(vv.ManagerIDs, id)
	}
	return vv
}

func (s *Server) workspaceDetail(ctx context.Context, ws db.Workspace, v authz.Viewer) (workspaceDetail, error) {
	q := s.Store.Queries
	d := workspaceDetail{Workspace: core.ToWorkspace(ws), Viewer: toViewer(v), Members: []any{}, Directories: []directoryDetail{}, Grants: []core.GrantView{}}
	members, err := q.ListMembers(ctx, ws.ID)
	if err != nil {
		return d, err
	}
	visible := map[uuid.UUID]bool{}
	for _, m := range members {
		if v.CanSeeMember(m) {
			visible[m.ID] = true
			d.Members = append(d.Members, core.ToMember(m))
		} else {
			d.Members = append(d.Members, core.ToHiddenMember(m))
		}
	}
	grants, err := q.ListGrants(ctx, ws.ID)
	if err != nil {
		return d, err
	}
	for _, g := range grants {
		if visible[g.MemberID] {
			d.Grants = append(d.Grants, core.ToGrant(g))
		}
	}
	dirs, err := q.ListDirectories(ctx, ws.ID)
	if err != nil {
		return d, err
	}
	access, err := q.ListDirectoryAccess(ctx, ws.ID)
	if err != nil {
		return d, err
	}
	byDir := map[uuid.UUID]map[uuid.UUID]string{}
	for _, a := range access {
		if byDir[a.DirectoryID] == nil {
			byDir[a.DirectoryID] = map[uuid.UUID]string{}
		}
		byDir[a.DirectoryID][a.UserID] = a.Mode
	}
	names := map[uuid.UUID]string{}
	for _, dir := range dirs {
		if _, ok := names[dir.OwnerUserID]; !ok {
			if u, err := q.GetUserByID(ctx, dir.OwnerUserID); err == nil {
				names[dir.OwnerUserID] = u.Name
			}
		}
		dd := directoryDetail{DirectoryView: core.ToDirectory(dir), OwnerName: names[dir.OwnerUserID],
			YourMode: authz.PersonDirMode(dir, v.UserID, byDir[dir.ID])}
		if v.Admin() || dir.OwnerUserID == v.UserID {
			dd.Access = map[string]string{}
			for uid, mode := range byDir[dir.ID] {
				dd.Access[uid.String()] = mode
			}
		}
		d.Directories = append(d.Directories, dd)
	}
	clones, err := q.ListWorkspaceClones(ctx, ws.ID)
	if err != nil {
		return d, err
	}
	d.Clones = make([]core.CloneView, 0, len(clones))
	for _, c := range clones {
		d.Clones = append(d.Clones, core.ToClone(c))
	}
	return d, nil
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	d, err := s.workspaceDetail(r.Context(), ws, v)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) handlePatchWorkspace(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	ws, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	var req struct {
		Name           *string `json:"name"`
		About          *string `json:"about"`
		GoalCompletion *string `json:"goal_completion"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	val := httpx.Validation{}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		req.Name = &n
		if n == "" {
			val.Add("name", "Enter a name for the workspace.")
		}
	}
	if req.GoalCompletion != nil && *req.GoalCompletion != "manual" && *req.GoalCompletion != "auto" {
		val.Add("goal_completion", "Choose when goals finish.")
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		ws, err = t.Q.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: wsID, Name: req.Name, About: req.About, GoalCompletion: req.GoalCompletion})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "workspace.updated"), core.ToWorkspace(ws))
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"workspace": core.ToWorkspace(ws)})
	return nil
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	ws, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, uid)
	if err != nil {
		return err
	}
	if !v.OrgAdmin() {
		return httpx.ErrForbidden
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteWorkspace(ctx, wsID); err != nil {
			return err
		}
		if err := s.audit(ctx, t.Q, &ws.OrgID, &uid, "workspace.deleted", "Deleted the workspace "+ws.Name); err != nil {
			return err
		}
		return t.Emit(ctx, core.Org(ws.OrgID, "workspace.deleted"), map[string]any{"id": wsID})
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleRestartSessions ends every agent conversation in the workspace (workspace admins).
// Work in progress continues in fresh conversations.
func (s *Server) handleRestartSessions(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	ws, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, uid)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	n, err := s.Orch.RestartSessions(ctx, wsID)
	if err != nil {
		return err
	}
	if err := s.audit(ctx, s.Store.Queries, &ws.OrgID, &uid, "workspace.sessions_restarted", "Restarted every session in "+ws.Name); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"restarted": n})
	return nil
}
