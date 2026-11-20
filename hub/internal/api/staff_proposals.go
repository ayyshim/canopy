package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/skills"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Managers draft new staff, or new instructions for existing staff. Nothing changes until a
// workspace admin approves; admins may edit the draft first.

var errProposalDecided = httpx.NewError(http.StatusConflict, "proposal_decided", "This draft was already decided.")

func (s *Server) handleListStaffProposals(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	_, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		return httpx.Validation{"status": "Choose pending, approved or rejected."}.Err()
	}
	list, err := s.Store.ListStaffProposals(ctx, db.ListStaffProposalsParams{WorkspaceID: wsID, Status: status})
	if err != nil {
		return err
	}
	out := make([]core.StaffProposalView, 0, len(list))
	for _, p := range list {
		out = append(out, core.ToStaffProposal(p))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"proposals": out})
	return nil
}

type decideInput struct {
	Name         *string            `json:"name"`
	Role         *string            `json:"role"`
	Instructions *string            `json:"instructions"`
	Skills       *[]core.DraftSkill `json:"skills"`
	Reason       string             `json:"reason"` // rejections only
}

// loadProposal fetches a draft for an admin and locks it for the decision.
func (s *Server) loadProposal(r *http.Request, t *core.Tx, ws db.Workspace) (db.StaffProposal, error) {
	id, err := pathID(r, "proposalID")
	if err != nil {
		return db.StaffProposal{}, err
	}
	p, err := t.Q.GetStaffProposalForUpdate(r.Context(), id)
	if store.IsNotFound(err) || (err == nil && p.WorkspaceID != ws.ID) {
		return p, httpx.ErrNotFound
	} else if err != nil {
		return p, err
	}
	if p.Status != "pending" {
		return p, errProposalDecided
	}
	return p, nil
}

func (s *Server) adminWorkspace(r *http.Request) (db.Workspace, authz.Viewer, error) {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return db.Workspace{}, authz.Viewer{}, err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return ws, v, err
	}
	return ws, v, v.RequireAdmin()
}

func (s *Server) handleRejectStaffProposal(w http.ResponseWriter, r *http.Request) error {
	ws, v, err := s.adminWorkspace(r)
	if err != nil {
		return err
	}
	var in decideInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	var done db.StaffProposal
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		p, err := s.loadProposal(r, t, ws)
		if err != nil {
			return err
		}
		done, err = t.Q.DecideStaffProposal(ctx, db.DecideStaffProposalParams{ID: p.ID, Status: "rejected", DecidedBy: &v.UserID,
			Reason: strings.TrimSpace(in.Reason), Name: p.Name, Role: p.Role, Instructions: p.Instructions, Skills: p.Skills})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "staff_proposal.updated"), map[string]any{"proposal": core.ToStaffProposal(done)})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"proposal": core.ToStaffProposal(done)})
	return nil
}

func (s *Server) handleApproveStaffProposal(w http.ResponseWriter, r *http.Request) error {
	ws, v, err := s.adminWorkspace(r)
	if err != nil {
		return err
	}
	var in decideInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	var done db.StaffProposal
	var member db.Member
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		p, err := s.loadProposal(r, t, ws)
		if err != nil {
			return err
		}
		// The admin's edits replace the draft's fields.
		name, instructions, role := p.Name, p.Instructions, p.Role
		var draft []core.DraftSkill
		_ = json.Unmarshal(p.Skills, &draft)
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Instructions != nil {
			instructions = *in.Instructions
		}
		if in.Role != nil {
			role = in.Role
		}
		if in.Skills != nil {
			draft = *in.Skills
		}
		draft, err = checkDraftSkills(draft)
		if err != nil {
			return err
		}
		val := httpx.Validation{}
		if p.Kind == "new" {
			if name == "" {
				val.Add("name", "Give this member a name.")
			}
			if role == nil || !domain.ValidSkill(*role) {
				val.Add("role", "Pick a role.")
			}
		}
		if err := val.Err(); err != nil {
			return err
		}

		if p.Kind == "new" {
			member, err = s.createProposedStaff(ctx, t, ws, p, name, *role, instructions)
		} else {
			member, err = t.Q.GetMember(ctx, *p.MemberID)
			if err == nil {
				member, err = t.Q.UpdateMember(ctx, db.UpdateMemberParams{ID: member.ID, Instructions: &instructions})
			}
		}
		if err != nil {
			return err
		}
		if err := attachDraftSkills(ctx, t, ws, member, draft); err != nil {
			return err
		}
		if err := emitMember(ctx, t, ws, member); err != nil {
			return err
		}
		raw, _ := json.Marshal(draft)
		done, err = t.Q.DecideStaffProposal(ctx, db.DecideStaffProposalParams{ID: p.ID, Status: "approved", DecidedBy: &v.UserID,
			Name: name, Role: role, Instructions: instructions, Skills: raw, ResultMemberID: &member.ID})
		if err != nil {
			return err
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.MemberChanged(context.WithoutCancel(ctx), member.ID) })
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "staff_proposal.updated"), map[string]any{"proposal": core.ToStaffProposal(done)})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"proposal": core.ToStaffProposal(done), "member": core.ToMember(member)})
	return nil
}

// createProposedStaff makes the staff member under the proposing manager, with grants suggested
// for the role and limited to the manager's.
func (s *Server) createProposedStaff(ctx context.Context, t *core.Tx, ws db.Workspace, p db.StaffProposal, name, role, instructions string) (db.Member, error) {
	mgr, err := t.Q.GetMember(ctx, p.ManagerID)
	if err != nil || mgr.Kind != "manager" {
		return db.Member{}, httpx.Validation{"manager": "The manager who drafted this is gone."}.Err()
	}
	existing, err := t.Q.ListMembers(ctx, ws.ID)
	if err != nil {
		return db.Member{}, err
	}
	m, err := t.Q.CreateMember(ctx, db.CreateMemberParams{ID: newID(), WorkspaceID: ws.ID, Name: name, Kind: "staff", Skill: &role,
		ReportsTo: &mgr.ID, Cli: "claude", Model: domain.DefaultModel("claude", "staff"), Instructions: instructions,
		AvatarColor: domain.AvatarColors[len(existing)%len(domain.AvatarColors)], Position: int32(len(existing))})
	if err != nil {
		return m, err
	}
	dirs, err := t.Q.ListDirectories(ctx, ws.ID)
	if err != nil {
		return m, err
	}
	limit, err := limitFor(ctx, t.Q, &mgr.ID, dirs)
	if err != nil {
		return m, err
	}
	return m, setGrants(ctx, t, ws, m, domain.Suggest(role, dirsOf(dirs), ws.Name, limit), "suggested", nil)
}

// checkDraftSkills trims and validates drafted skills.
func checkDraftSkills(list []core.DraftSkill) ([]core.DraftSkill, error) {
	val := httpx.Validation{}
	out := make([]core.DraftSkill, 0, len(list))
	for _, sk := range list {
		sk.Name, sk.SourceURL = strings.TrimSpace(sk.Name), strings.TrimSpace(sk.SourceURL)
		if sk.Name == "" || utf8.RuneCountInString(sk.Name) > maxSkillName {
			val.Add("skills", "Give every skill a name under 80 characters.")
			break
		}
		c, err := skills.Check(sk.Content)
		if err != nil {
			val.Add("skills", sk.Name+": "+err.Error())
			break
		}
		sk.Content = c
		out = append(out, sk)
	}
	return out, val.Err()
}

// attachDraftSkills links each skill to the member, adding it to the workspace library first
// unless a skill with that name is already there (the library's copy wins).
func attachDraftSkills(ctx context.Context, t *core.Tx, ws db.Workspace, m db.Member, list []core.DraftSkill) error {
	for _, d := range list {
		sk, err := t.Q.GetWorkspaceSkillByName(ctx, db.GetWorkspaceSkillByNameParams{WorkspaceID: ws.ID, Lower: d.Name})
		if store.IsNotFound(err) {
			kind := "md"
			if d.SourceURL != "" {
				kind = "github"
			}
			sk, err = t.Q.CreateWorkspaceSkill(ctx, db.CreateWorkspaceSkillParams{ID: newID(), WorkspaceID: ws.ID, Name: d.Name,
				SourceKind: kind, SourceUrl: d.SourceURL, Content: d.Content})
			if err != nil {
				return err
			}
			if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "skill.saved"), core.ToSkill(sk)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := t.Q.AttachMemberSkill(ctx, db.AttachMemberSkillParams{MemberID: m.ID, SkillID: sk.ID}); err != nil {
			return err
		}
	}
	linked, err := t.Q.ListMemberSkills(ctx, m.ID)
	if err != nil {
		return err
	}
	return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, m.ID, "member.skills"), map[string]any{"member_id": m.ID, "skills": core.ToSkills(linked)})
}
