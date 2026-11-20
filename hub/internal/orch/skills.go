package orch

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

// toolAssignSkill lets a manager give one of its own staff a library skill, or take it away.
func (o *Orch) toolAssignSkill(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Member string `json:"member"`
		Skill  string `json:"skill"`
		Remove bool   `json:"remove"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	msg, err := o.AssignSkill(ctx, tc.member.ID, in.Member, in.Skill, in.Remove)
	if err != nil {
		return err
	}
	o.reply(tc, msg, false)
	return nil
}

// AssignSkill attaches (or detaches) a library skill, by name, to a staff member who reports to
// the manager. Failures the agent can act on come back as a toolError.
func (o *Orch) AssignSkill(ctx context.Context, managerID uuid.UUID, memberName, skillName string, remove bool) (string, error) {
	mgr, err := o.Store.GetMember(ctx, managerID)
	if err != nil {
		return "", err
	}
	if mgr.Kind != "manager" {
		return "", toolError("Only managers can assign skills.")
	}
	members, err := o.Store.ListMembers(ctx, mgr.WorkspaceID)
	if err != nil {
		return "", err
	}
	var target *db.Member
	for i, m := range members {
		if m.Kind == "staff" && m.ReportsTo != nil && *m.ReportsTo == mgr.ID && strings.EqualFold(m.Name, strings.TrimSpace(memberName)) {
			target = &members[i]
		}
	}
	if target == nil {
		return "", toolError("“" + memberName + "” isn’t one of your staff.")
	}
	library, err := o.Store.ListWorkspaceSkills(ctx, mgr.WorkspaceID)
	if err != nil {
		return "", err
	}
	var skill *db.WorkspaceSkill
	names := make([]string, 0, len(library))
	for i, sk := range library {
		names = append(names, sk.Name)
		if strings.EqualFold(sk.Name, strings.TrimSpace(skillName)) {
			skill = &library[i]
		}
	}
	if skill == nil {
		if len(names) == 0 {
			return "", toolError("The workspace skill library is empty.")
		}
		return "", toolError("No skill called “" + skillName + "”. The library has: " + strings.Join(names, ", ") + ".")
	}
	ws, err := o.Store.GetWorkspace(ctx, mgr.WorkspaceID)
	if err != nil {
		return "", err
	}
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		if remove {
			err = t.Q.DetachMemberSkill(ctx, db.DetachMemberSkillParams{MemberID: target.ID, SkillID: skill.ID})
		} else {
			err = t.Q.AttachMemberSkill(ctx, db.AttachMemberSkillParams{MemberID: target.ID, SkillID: skill.ID})
		}
		if err != nil {
			return err
		}
		list, err := t.Q.ListMemberSkills(ctx, target.ID)
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, target.ID, "member.skills"),
			map[string]any{"member_id": target.ID, "skills": core.ToSkills(list)})
	})
	if err != nil {
		return "", err
	}
	if remove {
		return "Took the skill “" + skill.Name + "” away from " + target.Name + ".", nil
	}
	return "Gave " + target.Name + " the skill “" + skill.Name + "”. It applies from their next session.", nil
}

// MemberPrompt is the system prompt a member's next session starts with (no mounts or notes).
func (o *Orch) MemberPrompt(ctx context.Context, memberID uuid.UUID) (string, error) {
	m, err := o.Store.GetMember(ctx, memberID)
	if err != nil {
		return "", err
	}
	st, err := o.load(ctx, o.Store.Queries, m.WorkspaceID)
	if err != nil {
		return "", err
	}
	return systemPrompt(st, m, nil, nil), nil
}
