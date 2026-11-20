package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

var errLastManager = httpx.NewError(http.StatusConflict, "last_manager", "A workspace needs at least one manager.")

// loadMember fetches a member of the workspace the caller can see.
func (s *Server) loadMember(r *http.Request) (db.Workspace, authz.Viewer, db.Member, error) {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return db.Workspace{}, authz.Viewer{}, db.Member{}, err
	}
	memberID, err := pathID(r, "memberID")
	if err != nil {
		return db.Workspace{}, authz.Viewer{}, db.Member{}, err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return ws, v, db.Member{}, err
	}
	m, err := s.Store.GetMember(r.Context(), memberID)
	if store.IsNotFound(err) || (err == nil && (m.WorkspaceID != ws.ID || !v.CanSeeMember(m))) {
		return ws, v, m, httpx.ErrNotFound
	}
	return ws, v, m, err
}

func dirsOf(list []db.Directory) []domain.Dir {
	out := make([]domain.Dir, 0, len(list))
	for _, d := range list {
		out = append(out, domain.Dir{ID: d.ID, Name: d.Name, Kind: d.Kind})
	}
	return out
}

func grantsOf(list []db.DirectoryGrant) []domain.Grant {
	out := make([]domain.Grant, 0, len(list))
	for _, g := range list {
		out = append(out, domain.Grant{DirectoryID: g.DirectoryID, Mode: g.Mode})
	}
	return out
}

// emitMemberGrants publishes a member and its current grants.
func emitMember(ctx context.Context, t *core.Tx, ws db.Workspace, m db.Member) error {
	grants, err := t.Q.ListMemberGrants(ctx, m.ID)
	if err != nil {
		return err
	}
	gv := make([]core.GrantView, 0, len(grants))
	for _, g := range grants {
		gv = append(gv, core.ToGrant(g))
	}
	return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, m.ID, "member.updated"), map[string]any{"member": core.ToMember(m), "grants": gv})
}

// setGrants replaces a member's grants, then keeps every staff member within its manager.
func setGrants(ctx context.Context, t *core.Tx, ws db.Workspace, m db.Member, grants []domain.Grant, source string, by *uuid.UUID) error {
	q := t.Q
	if err := q.DeleteMemberGrants(ctx, m.ID); err != nil {
		return err
	}
	for _, g := range grants {
		if _, err := q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: m.ID, DirectoryID: g.DirectoryID, Mode: g.Mode,
			Source: source, Reason: g.Reason, GrantedBy: by}); err != nil {
			return err
		}
	}
	if err := emitMember(ctx, t, ws, m); err != nil {
		return err
	}
	if m.Kind == "manager" {
		return pruneStaff(ctx, t, ws, m, grants)
	}
	return nil
}

// pruneStaff narrows a manager's staff to the manager's access (SPEC "Narrowing a manager's
// access narrows its staff").
func pruneStaff(ctx context.Context, t *core.Tx, ws db.Workspace, manager db.Member, limit []domain.Grant) error {
	members, err := t.Q.ListMembers(ctx, ws.ID)
	if err != nil {
		return err
	}
	for _, sm := range members {
		if sm.ReportsTo == nil || *sm.ReportsTo != manager.ID {
			continue
		}
		cur, err := t.Q.ListMemberGrants(ctx, sm.ID)
		if err != nil {
			return err
		}
		clamped := domain.Clamp(grantsOf(cur), limit)
		if len(clamped) == len(cur) {
			same := true
			for i := range cur {
				if cur[i].Mode != clamped[i].Mode {
					same = false
				}
			}
			if same {
				continue
			}
		}
		modes := map[uuid.UUID]string{}
		for _, g := range clamped {
			modes[g.DirectoryID] = g.Mode
		}
		for _, g := range cur {
			mode, keep := modes[g.DirectoryID]
			switch {
			case !keep:
				err = t.Q.DeleteGrant(ctx, db.DeleteGrantParams{MemberID: sm.ID, DirectoryID: g.DirectoryID})
			case mode != g.Mode:
				_, err = t.Q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: sm.ID, DirectoryID: g.DirectoryID, Mode: mode,
					Source: g.Source, Reason: g.Reason, GrantedBy: g.GrantedBy})
			}
			if err != nil {
				return err
			}
		}
		if err := emitMember(ctx, t, ws, sm); err != nil {
			return err
		}
	}
	return nil
}

// limitFor is the directory access a new or edited staff member may get: its manager's,
// or every directory for independent staff.
func limitFor(ctx context.Context, q *db.Queries, reportsTo *uuid.UUID, dirs []db.Directory) ([]domain.Grant, error) {
	if reportsTo == nil {
		return domain.AllDirectories(dirsOf(dirs)), nil
	}
	g, err := q.ListMemberGrants(ctx, *reportsTo)
	return grantsOf(g), err
}

type memberInput struct {
	Name         *string    `json:"name"`
	Kind         string     `json:"kind"`
	Skill        *string    `json:"skill"`
	ReportsTo    *uuid.UUID `json:"reports_to"`
	SetReportsTo bool       `json:"set_reports_to"`
	CLI          *string    `json:"cli"`
	Model        *string    `json:"model"`
	Instructions *string    `json:"instructions"`
	Sleeping     *bool      `json:"sleeping"`
	Isolated     *bool      `json:"isolated"`
}

func (s *Server) handleCreateMember(w http.ResponseWriter, r *http.Request) error {
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
	var in memberInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	val := httpx.Validation{}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		val.Add("name", "Give this member a name.")
	}
	if in.Kind != "manager" && in.Kind != "staff" {
		val.Add("kind", "Choose manager or staff.")
	}
	var skill *string
	if in.Skill != nil && *in.Skill != "" {
		if !domain.ValidSkill(*in.Skill) {
			val.Add("skill", "Pick a skill.")
		}
		skill = in.Skill
	} else if in.Kind == "staff" {
		val.Add("skill", "Pick a skill.")
	}
	cli := "claude"
	if in.CLI != nil {
		cli = *in.CLI
	}
	if c, ok := domain.CLI(cli); !ok || !c.Supported {
		val.Add("cli", "Choose a supported CLI.")
	}
	model := domain.DefaultModel(cli, in.Kind)
	if in.Model != nil && *in.Model != "" {
		if !domain.ValidModel(cli, *in.Model) {
			val.Add("model", "Choose a model this CLI offers.")
		}
		model = *in.Model
	}
	var reports *uuid.UUID
	if in.Kind == "staff" && in.ReportsTo != nil {
		mgr, err := s.Store.GetMember(ctx, *in.ReportsTo)
		if err != nil || mgr.WorkspaceID != wsID || mgr.Kind != "manager" {
			val.Add("reports_to", "Choose a manager in this workspace.")
		}
		reports = in.ReportsTo
	}
	isolated := in.Isolated != nil && *in.Isolated
	if isolated && (in.Kind != "staff" || reports != nil) {
		val.Add("isolated", "Only independent staff can be isolated.")
	}
	if err := val.Err(); err != nil {
		return err
	}
	var m db.Member
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		existing, err := t.Q.ListMembers(ctx, wsID)
		if err != nil {
			return err
		}
		m, err = t.Q.CreateMember(ctx, db.CreateMemberParams{ID: newID(), WorkspaceID: wsID, Name: name, Kind: in.Kind, Skill: skill,
			ReportsTo: reports, Cli: cli, Model: model, Instructions: deref(in.Instructions),
			AvatarColor: domain.AvatarColors[len(existing)%len(domain.AvatarColors)], Position: int32(len(existing)),
			Isolated: isolated, CreatedBy: &uid})
		if err != nil {
			return err
		}
		dirs, err := t.Q.ListDirectories(ctx, wsID)
		if err != nil {
			return err
		}
		var grants []domain.Grant
		if in.Kind == "manager" {
			// New managers start with every directory; admins narrow it in settings.
			m, err = t.Q.UpdateMember(ctx, db.UpdateMemberParams{ID: m.ID, AllDirectories: ptr(true)})
			if err != nil {
				return err
			}
			grants = domain.AllDirectories(dirsOf(dirs))
		} else {
			limit, err := limitFor(ctx, t.Q, reports, dirs)
			if err != nil {
				return err
			}
			grants = domain.Suggest(*skill, dirsOf(dirs), ws.Name, limit)
		}
		source := "suggested"
		if in.Kind == "manager" {
			source = "human"
		}
		return setGrants(ctx, t, ws, m, grants, source, nil)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"member": core.ToMember(m)})
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptr[T any](v T) *T { return &v }

// handlePatchMember edits a member from the chat window's Settings tab.
func (s *Server) handlePatchMember(w http.ResponseWriter, r *http.Request) error {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var in memberInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	// Sleep and wake are allowed wherever the person can see the member; the rest needs admin.
	onlySleep := in.Sleeping != nil && in.Name == nil && in.Skill == nil && !in.SetReportsTo && in.CLI == nil && in.Model == nil && in.Instructions == nil && in.Isolated == nil
	if !onlySleep {
		if err := v.RequireAdmin(); err != nil {
			return err
		}
	}
	val := httpx.Validation{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" {
			val.Add("name", "Give this member a name.")
		}
	}
	setSkill := in.Skill != nil
	if setSkill && *in.Skill == "" {
		if m.Kind == "staff" {
			val.Add("skill", "Pick a skill.")
		}
		in.Skill = nil
	} else if setSkill && !domain.ValidSkill(*in.Skill) {
		val.Add("skill", "Pick a skill.")
	}
	model := in.Model
	if in.CLI != nil && *in.CLI != m.Cli {
		if c, ok := domain.CLI(*in.CLI); !ok || !c.Supported {
			val.Add("cli", "Choose a supported CLI.")
		}
		// Changing CLI resets the model to that CLI's default (and billing follows the CLI).
		if model == nil {
			model = ptr(domain.DefaultModel(*in.CLI, m.Kind))
		}
	}
	if model != nil {
		cli := m.Cli
		if in.CLI != nil {
			cli = *in.CLI
		}
		if !domain.ValidModel(cli, *model) {
			val.Add("model", "Choose a model this CLI offers.")
		}
	}
	if in.SetReportsTo {
		if m.Kind != "staff" {
			val.Add("reports_to", "Only staff work for a manager.")
		} else if in.ReportsTo != nil {
			mgr, err := s.Store.GetMember(ctx, *in.ReportsTo)
			if err != nil || mgr.WorkspaceID != ws.ID || mgr.Kind != "manager" {
				val.Add("reports_to", "Choose a manager in this workspace.")
			}
		}
	}
	wantIsolated := m.Isolated
	if in.Isolated != nil {
		wantIsolated = *in.Isolated
	}
	reports := m.ReportsTo
	if in.SetReportsTo {
		reports = in.ReportsTo
	}
	if wantIsolated && (m.Kind != "staff" || reports != nil) {
		val.Add("isolated", "Only independent staff can be isolated.")
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		updated, err := t.Q.UpdateMember(ctx, db.UpdateMemberParams{ID: m.ID, Name: in.Name, SetSkill: setSkill, Skill: in.Skill,
			SetReportsTo: in.SetReportsTo, ReportsTo: in.ReportsTo, Cli: in.CLI, Model: model, Instructions: in.Instructions, Sleeping: in.Sleeping, Isolated: in.Isolated})
		if err != nil {
			return err
		}
		if in.SetReportsTo {
			// A new manager (or none) means a new limit for this member's directories.
			dirs, err := t.Q.ListDirectories(ctx, ws.ID)
			if err != nil {
				return err
			}
			limit, err := limitFor(ctx, t.Q, updated.ReportsTo, dirs)
			if err != nil {
				return err
			}
			cur, err := t.Q.ListMemberGrants(ctx, m.ID)
			if err != nil {
				return err
			}
			if err := setGrants(ctx, t, ws, updated, domain.Clamp(grantsOf(cur), limit), "human", &v.UserID); err != nil {
				return err
			}
		} else if err := emitMember(ctx, t, ws, updated); err != nil {
			return err
		}
		m = updated
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.MemberChanged(context.WithoutCancel(ctx), m.ID) })
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"member": core.ToMember(m)})
	return nil
}

// handleDeleteMember removes a member, handing its open tasks to a teammate or deleting them,
// and moving a manager's staff to another manager or making them independent.
func (s *Server) handleDeleteMember(w http.ResponseWriter, r *http.Request) error {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	ctx := r.Context()
	var in struct {
		OpenTasks string     `json:"open_tasks"` // "hand_to" or "delete"
		HandTo    *uuid.UUID `json:"hand_to"`
		StaffTo   *uuid.UUID `json:"staff_to"` // another manager; null = independent
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
	}
	val := httpx.Validation{}
	if in.OpenTasks == "" {
		in.OpenTasks = "delete"
	}
	if in.OpenTasks == "hand_to" {
		to, err := s.Store.GetMember(ctx, deref2(in.HandTo))
		if in.HandTo == nil || err != nil || to.WorkspaceID != ws.ID || to.ID == m.ID {
			val.Add("hand_to", "Choose a teammate to take the open tasks.")
		}
	} else if in.OpenTasks != "delete" {
		val.Add("open_tasks", "Choose what happens to open tasks.")
	}
	if m.Kind == "manager" && in.StaffTo != nil {
		to, err := s.Store.GetMember(ctx, *in.StaffTo)
		if err != nil || to.WorkspaceID != ws.ID || to.Kind != "manager" || to.ID == m.ID {
			val.Add("staff_to", "Choose another manager, or make the staff independent.")
		}
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.LockWorkspace(ctx, ws.ID); err != nil {
			return err
		}
		if m.Kind == "manager" {
			n, err := t.Q.CountManagers(ctx, ws.ID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastManager
			}
			if err := t.Q.ReassignStaff(ctx, db.ReassignStaffParams{OldManager: &m.ID, NewManager: in.StaffTo}); err != nil {
				return err
			}
		}
		tasks, err := t.Q.ListMemberOpenTasks(ctx, &m.ID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if in.OpenTasks == "hand_to" {
				nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, SetMember: true, MemberID: in.HandTo,
					Status: ptr(map[bool]string{true: "todo", false: task.Status}[task.Status == "doing"])})
				if err != nil {
					return err
				}
				if err := t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, *in.HandTo, "task.updated"), core.ToTask(nt)); err != nil {
					return err
				}
			} else {
				if err := t.Q.DeleteTask(ctx, task.ID); err != nil {
					return err
				}
				if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "task.deleted"), map[string]any{"id": task.ID}); err != nil {
					return err
				}
			}
		}
		if err := t.Q.DeleteMember(ctx, m.ID); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "member.deleted"), map[string]any{"id": m.ID}); err != nil {
			return err
		}
		if m.Kind == "manager" {
			members, err := t.Q.ListMembers(ctx, ws.ID)
			if err != nil {
				return err
			}
			for _, sm := range members {
				if sm.Kind == "staff" && ((in.StaffTo != nil && sm.ReportsTo != nil && *sm.ReportsTo == *in.StaffTo) || sm.ReportsTo == nil) {
					// Moved staff stay within their new manager.
					dirs, err := t.Q.ListDirectories(ctx, ws.ID)
					if err != nil {
						return err
					}
					limit, err := limitFor(ctx, t.Q, sm.ReportsTo, dirs)
					if err != nil {
						return err
					}
					cur, err := t.Q.ListMemberGrants(ctx, sm.ID)
					if err != nil {
						return err
					}
					if err := setGrants(ctx, t, ws, sm, domain.Clamp(grantsOf(cur), limit), "human", &v.UserID); err != nil {
						return err
					}
				}
			}
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.MemberRemoved(context.WithoutCancel(ctx), ws.ID, m.ID) })
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func deref2(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// handlePutGrants replaces a member's directory access. Staff stay within their manager;
// narrowing a manager prunes its staff.
func (s *Server) handlePutGrants(w http.ResponseWriter, r *http.Request) error {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	ctx := r.Context()
	var in struct {
		AllDirectories *bool          `json:"all_directories"`
		Grants         []domain.Grant `json:"grants"`
		Source         string         `json:"source"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Source != "suggested" {
		in.Source = "human"
	}
	dirs, err := s.Store.ListDirectories(ctx, ws.ID)
	if err != nil {
		return err
	}
	known := map[uuid.UUID]bool{}
	for _, d := range dirs {
		known[d.ID] = true
	}
	val := httpx.Validation{}
	seen := map[uuid.UUID]bool{}
	for _, g := range in.Grants {
		if !known[g.DirectoryID] || seen[g.DirectoryID] {
			val.Add("grants", "Unknown or repeated directory.")
		}
		seen[g.DirectoryID] = true
		if !validMode(g.Mode) {
			val.Add("grants", "Choose read or read and write.")
		}
	}
	all := m.Kind == "manager" && in.AllDirectories != nil && *in.AllDirectories
	grants := in.Grants
	if all {
		grants = domain.AllDirectories(dirsOf(dirs))
	}
	if m.Kind == "staff" && m.ReportsTo != nil {
		limit, err := limitFor(ctx, s.Store.Queries, m.ReportsTo, dirs)
		if err != nil {
			return err
		}
		if _, ok := domain.WithinLimit(grants, limit); !ok {
			val.Add("grants", "Staff can only reach directories their manager can, with the same or less access.")
		}
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if m.Kind == "manager" {
			var err error
			m, err = t.Q.UpdateMember(ctx, db.UpdateMemberParams{ID: m.ID, AllDirectories: ptr(all)})
			if err != nil {
				return err
			}
		}
		if err := setGrants(ctx, t, ws, m, grants, in.Source, &v.UserID); err != nil {
			return err
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.MemberChanged(context.WithoutCancel(ctx), m.ID) })
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &v.UserID, "access.grants_changed", "Changed directory access for "+m.Name)
	})
	if err != nil {
		return err
	}
	grantsNow, err := s.Store.ListMemberGrants(ctx, m.ID)
	if err != nil {
		return err
	}
	out := make([]core.GrantView, 0, len(grantsNow))
	for _, g := range grantsNow {
		out = append(out, core.ToGrant(g))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"member": core.ToMember(m), "grants": out})
	return nil
}

// handleSuggestGrants is "Ask <manager> to suggest again": the fallback heuristic, clamped to
// the manager. (A running manager can refine these through its tools.)
func (s *Server) handleSuggestGrants(w http.ResponseWriter, r *http.Request) error {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	if m.Kind != "staff" || m.Skill == nil {
		return httpx.Validation{"member": "Only staff get suggestions."}.Err()
	}
	ctx := r.Context()
	dirs, err := s.Store.ListDirectories(ctx, ws.ID)
	if err != nil {
		return err
	}
	limit, err := limitFor(ctx, s.Store.Queries, m.ReportsTo, dirs)
	if err != nil {
		return err
	}
	suggested := domain.Suggest(*m.Skill, dirsOf(dirs), ws.Name, limit)
	err = s.Core.Tx(ctx, func(t *core.Tx) error { return setGrants(ctx, t, ws, m, suggested, "suggested", nil) })
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"grants": suggested})
	return nil
}
