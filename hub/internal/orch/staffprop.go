package orch

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/skills"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

const sentForApproval = "Sent for approval. A workspace admin will approve or reject it, and you will be told which."

// staffDecisionTurn marks the turn telling a manager about an admin decision; its reply goes to chat.
const staffDecisionTurn = "staff_decision"

// requireManager checks the caller is a manager; staff never get these tools.
func requireManager(tc *toolCtx) error {
	if tc.member.Kind != "manager" {
		return toolError("Only managers can do that.")
	}
	return nil
}

// checkDraftSkills trims and validates the skills a manager drafted.
func checkDraftSkills(in []core.DraftSkill) ([]core.DraftSkill, error) {
	if len(in) > 8 {
		return nil, toolError("Attach at most 8 skills to one draft.")
	}
	out := make([]core.DraftSkill, 0, len(in))
	for _, sk := range in {
		sk.Name, sk.SourceURL = strings.TrimSpace(sk.Name), strings.TrimSpace(sk.SourceURL)
		if sk.Name == "" || utf8.RuneCountInString(sk.Name) > 80 {
			return nil, toolError("Give every skill a name under 80 characters.")
		}
		c, err := skills.Check(sk.Content)
		if err != nil {
			return nil, toolError(sk.Name + ": " + err.Error())
		}
		sk.Content = c
		out = append(out, sk)
	}
	return out, nil
}

// toolProposeStaff drafts a new staff member for an admin to approve.
func (o *Orch) toolProposeStaff(ctx context.Context, tc *toolCtx) error {
	if err := requireManager(tc); err != nil {
		return err
	}
	var in struct {
		Name         string            `json:"name"`
		Role         string            `json:"role"`
		Instructions string            `json:"instructions"`
		Reason       string            `json:"reason"`
		Skills       []core.DraftSkill `json:"skills"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		return toolError("Give the new staff member a name.")
	case !domain.ValidSkill(in.Role):
		return toolError("role must be one of: " + strings.Join(domain.Skills, ", ") + ".")
	case strings.TrimSpace(in.Instructions) == "":
		return toolError("Write instructions for the new staff member.")
	}
	draft, err := checkDraftSkills(in.Skills)
	if err != nil {
		return err
	}
	members, err := o.Store.ListMembers(ctx, tc.ws.ID)
	if err != nil {
		return err
	}
	for _, m := range members {
		if strings.EqualFold(m.Name, name) {
			return toolError("Someone called “" + name + "” is already in the workspace. Pick another name.")
		}
	}
	if err := o.draft(ctx, tc, db.CreateStaffProposalParams{Kind: "new", Name: name, Role: &in.Role,
		Instructions: strings.TrimSpace(in.Instructions), Note: strings.TrimSpace(in.Reason)}, draft); err != nil {
		return err
	}
	o.reply(tc, sentForApproval, false)
	return nil
}

// toolUpdateStaffInstructions drafts new instructions for one of the manager's staff.
func (o *Orch) toolUpdateStaffInstructions(ctx context.Context, tc *toolCtx) error {
	if err := requireManager(tc); err != nil {
		return err
	}
	var in struct {
		Member       string `json:"member"`
		Instructions string `json:"instructions"`
		Reason       string `json:"reason"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Instructions) == "" {
		return toolError("Write the new instructions in full.")
	}
	target, err := o.ownStaff(ctx, tc, in.Member)
	if err != nil {
		return err
	}
	if err := o.draft(ctx, tc, db.CreateStaffProposalParams{Kind: "update", MemberID: &target.ID, Name: target.Name,
		Instructions: strings.TrimSpace(in.Instructions), Note: strings.TrimSpace(in.Reason)}, nil); err != nil {
		return err
	}
	o.reply(tc, sentForApproval, false)
	return nil
}

// toolGetStaffInstructions shows a manager the current instructions of one of their staff.
func (o *Orch) toolGetStaffInstructions(ctx context.Context, tc *toolCtx) error {
	if err := requireManager(tc); err != nil {
		return err
	}
	var in struct {
		Member string `json:"member"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	target, err := o.ownStaff(ctx, tc, in.Member)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(target.Instructions)
	if text == "" {
		text = "(no instructions yet)"
	}
	o.reply(tc, text, false)
	return nil
}

// ownStaff finds a staff member, by name, who reports to the calling manager.
func (o *Orch) ownStaff(ctx context.Context, tc *toolCtx, name string) (*db.Member, error) {
	members, err := o.Store.ListMembers(ctx, tc.ws.ID)
	if err != nil {
		return nil, err
	}
	for i, m := range members {
		if m.Kind == "staff" && m.ReportsTo != nil && *m.ReportsTo == tc.member.ID && strings.EqualFold(m.Name, strings.TrimSpace(name)) {
			return &members[i], nil
		}
	}
	return nil, toolError("“" + name + "” isn’t one of your staff.")
}

func (o *Orch) draft(ctx context.Context, tc *toolCtx, p db.CreateStaffProposalParams, sk []core.DraftSkill) error {
	raw, _ := json.Marshal(append([]core.DraftSkill{}, sk...))
	p.ID, p.WorkspaceID, p.ManagerID, p.Skills, p.SessionID = newID(), tc.ws.ID, tc.member.ID, raw, &tc.sess.ID
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		row, err := t.Q.CreateStaffProposal(ctx, p)
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(tc.ws.OrgID, tc.ws.ID, "staff_proposal.updated"), map[string]any{"proposal": core.ToStaffProposal(row)})
	})
}

// StaffProposalDecided tells the manager who drafted a proposal what the admin decided.
func (o *Orch) StaffProposalDecided(ctx context.Context, p db.StaffProposal) {
	if p.SessionID == nil {
		return
	}
	sess, err := o.Store.GetAgentSession(ctx, *p.SessionID)
	if err != nil {
		return
	}
	o.sendTurn(ctx, p.WorkspaceID, keyOfSession(sess), protocol.Turn{TurnID: newID().String(), Kind: staffDecisionTurn,
		Text: decisionText(p)})
}

func decisionText(p db.StaffProposal) string {
	what := "your draft for " + p.Name
	if p.Kind == "update" {
		what = "your new instructions for " + p.Name
	}
	if p.Status == "approved" {
		if p.Kind == "update" {
			return "An admin approved " + what + ". They apply from " + p.Name + "’s next session."
		}
		return "An admin approved " + what + ". " + p.Name + " now reports to you. Give them work in your next plan."
	}
	msg := "An admin rejected " + what + "."
	if r := strings.TrimSpace(p.Reason); r != "" {
		msg += " Their reason: " + r
	}
	return msg + " Don’t resend it unchanged."
}
