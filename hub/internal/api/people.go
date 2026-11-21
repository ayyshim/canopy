package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

type inviteGrant struct {
	WorkspaceID      uuid.UUID   `json:"workspace_id"`
	Role             string      `json:"role"`
	ManagerIDs       []uuid.UUID `json:"manager_ids"`
	IndependentStaff bool        `json:"independent_staff"`
}

type personRow struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Email                string    `json:"email"`
	AvatarColor          string    `json:"avatar_color"`
	Role                 string    `json:"role"`
	JoinedAt             time.Time `json:"joined_at"`
	PersonalSessionLimit *int64    `json:"personal_session_limit"`
	PersonalWeeklyLimit  *int64    `json:"personal_weekly_limit"`
	Workspaces           []any     `json:"workspaces"`
}

// handleListPeople backs /org/people: everyone in the organization with workspace chips.
func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	rows, err := s.Store.ListOrgPeople(ctx, orgID)
	if err != nil {
		return err
	}
	out := make([]personRow, 0, len(rows))
	for _, p := range rows {
		pr := personRow{ID: p.ID, Name: p.Name, Email: p.Email, AvatarColor: p.AvatarColor, Role: p.OrgRole, JoinedAt: p.JoinedAt,
			PersonalSessionLimit: p.PersonalSessionLimit, PersonalWeeklyLimit: p.PersonalWeeklyLimit, Workspaces: []any{}}
		access, err := s.Store.ListUserWorkspaceAccessInOrg(ctx, db.ListUserWorkspaceAccessInOrgParams{OrgID: orgID, UserID: p.ID})
		if err != nil {
			return err
		}
		for _, a := range access {
			pr.Workspaces = append(pr.Workspaces, map[string]any{"id": a.WorkspaceID, "name": a.WorkspaceName, "role": a.Role})
		}
		out = append(out, pr)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"people": out})
	return nil
}

func (s *Server) handlePatchMembership(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := identity(r).UserID
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, actor); err != nil {
		return err
	}
	target, err := authz.OrgMember(ctx, s.Store.Queries, orgID, userID)
	if err != nil {
		return err
	}
	var in struct {
		Role                 *string `json:"role"`
		PersonalSessionLimit *int64  `json:"personal_session_limit"`
		SetSessionLimit      bool    `json:"set_session_limit"`
		PersonalWeeklyLimit  *int64  `json:"personal_weekly_limit"`
		SetWeeklyLimit       bool    `json:"set_weekly_limit"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	val := httpx.Validation{}
	if in.Role != nil {
		if *in.Role != "admin" && *in.Role != "member" {
			val.Add("role", "Choose admin or member.")
		}
		if target.OrgRole == "owner" {
			val.Add("role", "The owner’s role can’t be changed.")
		}
	}
	if in.SetSessionLimit && in.PersonalSessionLimit != nil && *in.PersonalSessionLimit <= 0 {
		val.Add("personal_session_limit", "Use a number above zero, or clear it.")
	}
	if in.SetWeeklyLimit && in.PersonalWeeklyLimit != nil && *in.PersonalWeeklyLimit <= 0 {
		val.Add("personal_weekly_limit", "Use a number above zero, or clear it.")
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if _, err := t.Q.UpdateMembership(ctx, db.UpdateMembershipParams{OrgID: orgID, UserID: userID, OrgRole: in.Role,
			SetSessionLimit: in.SetSessionLimit, PersonalSessionLimit: in.PersonalSessionLimit,
			SetWeeklyLimit: in.SetWeeklyLimit, PersonalWeeklyLimit: in.PersonalWeeklyLimit}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &orgID, &actor, "people.changed", "Changed a person’s role or limits")
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDeleteMembership(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := identity(r).UserID
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, actor); err != nil {
		return err
	}
	target, err := authz.OrgMember(ctx, s.Store.Queries, orgID, userID)
	if err != nil {
		return err
	}
	if target.OrgRole == "owner" {
		return httpx.NewError(http.StatusConflict, "owner", "The owner can’t be removed. Transfer ownership first.")
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteUserWorkspaceAccessInOrg(ctx, db.DeleteUserWorkspaceAccessInOrgParams{OrgID: orgID, UserID: userID}); err != nil {
			return err
		}
		if err := t.Q.DeleteMembership(ctx, db.DeleteMembershipParams{OrgID: orgID, UserID: userID}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &orgID, &actor, "people.removed", "Removed a person from the organization")
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- Invitations (SPEC "Invitations") ----

type inviteRow struct {
	ID          uuid.UUID     `json:"id"`
	Email       string        `json:"email"`
	Role        string        `json:"role"`
	InviterName string        `json:"inviter_name"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Workspaces  []inviteGrant `json:"workspaces"`
	CreatedAt   time.Time     `json:"created_at"`
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	rows, err := s.Store.ListOpenInvites(ctx, orgID)
	if err != nil {
		return err
	}
	out := make([]inviteRow, 0, len(rows))
	for _, i := range rows {
		ir := inviteRow{ID: i.ID, Email: i.Email, Role: i.OrgRole, InviterName: deref(i.InviterName), ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
		_ = json.Unmarshal(i.WorkspaceGrants, &ir.Workspaces)
		if ir.Workspaces == nil {
			ir.Workspaces = []inviteGrant{}
		}
		out = append(out, ir)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": out})
	return nil
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := identity(r).UserID
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, actor); err != nil {
		return err
	}
	var in struct {
		Email           string        `json:"email"`
		Role            string        `json:"role"`
		WorkspaceGrants []inviteGrant `json:"workspace_grants"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Email = normalizeEmail(in.Email)
	val := httpx.Validation{}
	if !validEmail(in.Email) {
		val.Add("email", "Enter a valid email.")
	}
	if in.Role == "" {
		in.Role = "member"
	}
	if in.Role != "admin" && in.Role != "member" {
		val.Add("role", "Choose admin or member.")
	}
	firstWS := ""
	for i, g := range in.WorkspaceGrants {
		ws, err := s.Store.GetWorkspace(ctx, g.WorkspaceID)
		if err != nil || ws.OrgID != orgID {
			val.Add(fmt.Sprintf("workspace_grants.%d.workspace_id", i), "Choose a workspace in this organization.")
			continue
		}
		if firstWS == "" {
			firstWS = ws.Name
		}
		if g.Role != "admin" && g.Role != "staff" {
			val.Add(fmt.Sprintf("workspace_grants.%d.role", i), "Choose admin or staff.")
		}
		for _, mid := range g.ManagerIDs {
			if m, err := s.Store.GetMember(ctx, mid); err != nil || m.WorkspaceID != ws.ID || m.Kind != "manager" {
				val.Add(fmt.Sprintf("workspace_grants.%d.manager_ids", i), "Choose managers from that workspace.")
			}
		}
	}
	if err := val.Err(); err != nil {
		return err
	}
	if u, err := s.Store.GetUserByEmail(ctx, in.Email); err == nil {
		if _, err := s.Store.GetActiveMembership(ctx, db.GetActiveMembershipParams{OrgID: orgID, UserID: u.ID}); err == nil {
			return httpx.FieldError(http.StatusConflict, "already_member", "email", "This person is already in the organization.")
		}
	}
	if open, err := s.Store.OpenInviteForEmail(ctx, db.OpenInviteForEmailParams{OrgID: orgID, Email: in.Email}); err != nil {
		return err
	} else if open {
		return httpx.FieldError(http.StatusConflict, "already_invited", "email", "This person already has an open invitation.")
	}
	org, err := s.Store.GetOrganization(ctx, orgID)
	if err != nil {
		return err
	}
	inviter, err := s.Store.GetUserByID(ctx, actor)
	if err != nil {
		return err
	}
	if in.WorkspaceGrants == nil {
		in.WorkspaceGrants = []inviteGrant{}
	}
	grants, _ := json.Marshal(in.WorkspaceGrants)
	raw, hash := auth.NewToken()
	var inv db.Invite
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		inv, err = t.Q.CreateInvite(ctx, db.CreateInviteParams{ID: newID(), OrgID: orgID, Email: in.Email, OrgRole: in.Role, TokenHash: hash,
			InvitedBy: &actor, ExpiresAt: s.Now().Add(inviteTTL), WorkspaceGrants: grants})
		if err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &orgID, &actor, "people.invited", "Invited "+in.Email)
	})
	if err != nil {
		return err
	}
	s.sendInvite(ctx, in.Email, raw, inviter.Name, org.Name, firstWS)
	httpx.JSON(w, http.StatusCreated, map[string]any{"invite": map[string]any{"id": inv.ID, "email": inv.Email},
		"link": s.Cfg.PublicURL + "/invite/" + raw})
	return nil
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	inviteID, err := pathID(r, "inviteID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	if err := s.Store.RevokeInvite(ctx, db.RevokeInviteParams{ID: inviteID, OrgID: orgID}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

var errInvalidInvite = httpx.NewError(http.StatusNotFound, "invalid_invite", "This invitation is no longer valid.")

func (s *Server) loadInvite(r *http.Request) (db.Invite, error) {
	inv, err := s.Store.GetInviteByToken(r.Context(), auth.HashToken(pathParam(r, "token")))
	if store.IsNotFound(err) {
		return inv, errInvalidInvite
	} else if err != nil {
		return inv, err
	}
	if inv.AcceptedAt != nil || inv.DeclinedAt != nil || inv.RevokedAt != nil || !inv.ExpiresAt.After(s.Now()) {
		return inv, errInvalidInvite
	}
	return inv, nil
}

// handleGetInvite shows who invited them, the organization, the starting workspace, their
// role and what they'll see.
func (s *Server) handleGetInvite(w http.ResponseWriter, r *http.Request) error {
	inv, err := s.loadInvite(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	org, err := s.Store.GetOrganization(ctx, inv.OrgID)
	if err != nil {
		return err
	}
	inviter := ""
	if inv.InvitedBy != nil {
		if u, err := s.Store.GetUserByID(ctx, *inv.InvitedBy); err == nil {
			inviter = u.Name
		}
	}
	var grants []inviteGrant
	_ = json.Unmarshal(inv.WorkspaceGrants, &grants)
	workspaces := []map[string]any{}
	for _, g := range grants {
		ws, err := s.Store.GetWorkspace(ctx, g.WorkspaceID)
		if err != nil {
			continue
		}
		sees := "Everything in this workspace"
		if g.Role == "staff" {
			names := []string{}
			for _, mid := range g.ManagerIDs {
				if m, err := s.Store.GetMember(ctx, mid); err == nil {
					names = append(names, m.Name+"’s team")
				}
			}
			if g.IndependentStaff {
				names = append(names, "independent staff")
			}
			if len(names) == 0 {
				sees = "Nothing yet; an admin will give you access"
			} else {
				sees = strings.Join(names, ", ")
			}
		}
		workspaces = append(workspaces, map[string]any{"id": ws.ID, "name": ws.Name, "role": g.Role, "sees": sees})
	}
	_, existsErr := s.Store.GetUserByEmail(ctx, inv.Email)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"email": inv.Email, "organization": org.Name, "inviter": inviter, "role": inv.OrgRole,
		"workspaces": workspaces, "account_exists": existsErr == nil, "expires_at": inv.ExpiresAt,
	})
	return nil
}

// handleAcceptInvite: no account → name + password creates only the user, then the
// membership; an existing account → password only, and only the membership is added.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) error {
	inv, err := s.loadInvite(r)
	if err != nil {
		return err
	}
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	existing, err := s.Store.GetUserByEmail(ctx, inv.Email)
	exists := err == nil
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	var hash string
	if exists {
		if existing.PasswordHash == nil {
			return errNoPassword
		}
		ok, err := auth.VerifyPassword(in.Password, *existing.PasswordHash)
		if err != nil {
			return err
		}
		if !ok {
			return errWrongPass
		}
	} else {
		val := httpx.Validation{}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			val.Add("name", "Enter your name.")
		}
		checkPassword(val, "password", in.Password)
		if err := val.Err(); err != nil {
			return err
		}
		if hash, err = auth.HashPassword(s.Argon2, in.Password); err != nil {
			return err
		}
	}
	var next string
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if _, err := q.MarkInviteAccepted(ctx, inv.ID); store.IsNotFound(err) {
			return errInvalidInvite
		} else if err != nil {
			return err
		}
		userID := existing.ID
		if !exists {
			u, err := q.CreateUser(ctx, db.CreateUserParams{ID: newID(), Email: inv.Email, Name: in.Name, PasswordHash: &hash,
				AvatarColor: avatarColorFor(inv.Email), LastOrgID: &inv.OrgID})
			if err != nil {
				return err
			}
			userID = u.ID
		} else if err := q.SetUserLastOrg(ctx, db.SetUserLastOrgParams{ID: userID, LastOrgID: &inv.OrgID}); err != nil {
			return err
		}
		if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{ID: newID(), OrgID: inv.OrgID, UserID: userID, OrgRole: inv.OrgRole, Status: "active"}); err != nil {
			if store.IsUniqueViolation(err, "") {
				return httpx.NewError(http.StatusConflict, "already_member", "You’re already in this organization.")
			}
			return err
		}
		var grants []inviteGrant
		_ = json.Unmarshal(inv.WorkspaceGrants, &grants)
		for _, g := range grants {
			if _, err := q.GetWorkspace(ctx, g.WorkspaceID); err != nil {
				continue
			}
			if g.ManagerIDs == nil {
				g.ManagerIDs = []uuid.UUID{}
			}
			if _, err := q.UpsertWorkspaceAccess(ctx, db.UpsertWorkspaceAccessParams{ID: newID(), WorkspaceID: g.WorkspaceID, UserID: userID,
				Role: g.Role, ManagerIds: g.ManagerIDs, IndependentStaff: g.IndependentStaff}); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, q, &inv.OrgID, &userID, "people.joined", inv.Email+" accepted the invitation"); err != nil {
			return err
		}
		id, err := s.Sessions.Start(ctx, w, r, q, userID)
		next = id.Next()
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, twoFactorNext(next))
	return nil
}

// handleDeclineInvite marks the invitation declined and tells the inviter.
func (s *Server) handleDeclineInvite(w http.ResponseWriter, r *http.Request) error {
	inv, err := s.loadInvite(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := s.Store.MarkInviteDeclined(ctx, inv.ID); err != nil {
		if store.IsNotFound(err) {
			return errInvalidInvite
		}
		return err
	}
	if inv.InvitedBy != nil {
		if u, err := s.Store.GetUserByID(ctx, *inv.InvitedBy); err == nil {
			org, _ := s.Store.GetOrganization(ctx, inv.OrgID)
			_ = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: inv.Email + " declined your invitation",
				Text: inv.Email + " declined the invitation to " + org.Name + " on Canopy.\n"})
		}
	}
	_ = s.audit(ctx, s.Store.Queries, &inv.OrgID, nil, "people.declined", inv.Email+" declined the invitation")
	w.WriteHeader(http.StatusNoContent)
	return nil
}
