package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

type orgResponse struct {
	Organization orgView `json:"organization"`
	Role         string  `json:"role"`
}

// handleCreateOrg creates another organization owned by the caller and makes it current.
func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) error {
	var in orgInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v := httpx.Validation{}
	in.validate(v, "")
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	var org db.Organization
	err := s.Store.Tx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByID(ctx, id.UserID)
		if err != nil {
			return err
		}
		if org, err = s.createOrg(ctx, q, in, id.UserID); err != nil {
			return err
		}
		if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{
			ID: newID(), OrgID: org.ID, UserID: id.UserID, OrgRole: authz.RoleOwner, Status: "active",
		}); err != nil {
			return err
		}
		if err := q.SetUserLastOrg(ctx, db.SetUserLastOrgParams{ID: id.UserID, LastOrgID: &org.ID}); err != nil {
			return err
		}
		return s.audit(ctx, q, &org.ID, &id.UserID, "organization.created", fmt.Sprintf("%s created the organization", u.Name))
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, orgResponse{Organization: toOrgView(org), Role: authz.RoleOwner})
	return nil
}

func (s *Server) handleGetOrg(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	m, err := authz.OrgMember(r.Context(), s.Store.Queries, orgID, identity(r).UserID)
	if err != nil {
		return err
	}
	org, err := s.Store.GetOrganization(r.Context(), orgID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, orgResponse{Organization: toOrgView(org), Role: m.OrgRole})
	return nil
}

type patchOrgRequest struct {
	Name                *string `json:"name"`
	DefaultSessionLimit *int64  `json:"default_session_limit"`
	DefaultWeeklyLimit  *int64  `json:"default_weekly_limit"`
	Timezone            *string `json:"timezone"`
}

func (s *Server) handlePatchOrg(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	m, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, id.UserID)
	if err != nil {
		return err
	}
	var req patchOrgRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		req.Name = &n
		if n == "" {
			v.Add("name", "Enter your organization’s name.")
		} else if utf8.RuneCountInString(n) > 80 {
			v.Add("name", "Use at most 80 characters.")
		}
	}
	if req.DefaultSessionLimit != nil && *req.DefaultSessionLimit <= 0 {
		v.Add("default_session_limit", "Use a number above zero.")
	}
	if req.DefaultWeeklyLimit != nil && *req.DefaultWeeklyLimit <= 0 {
		v.Add("default_weekly_limit", "Use a number above zero.")
	}
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil || *req.Timezone == "" {
			v.Add("timezone", "Pick a time zone.")
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	var org db.Organization
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		var err error
		org, err = q.UpdateOrganization(ctx, db.UpdateOrganizationParams{
			ID: orgID, Name: req.Name, DefaultSessionLimit: req.DefaultSessionLimit,
			DefaultWeeklyLimit: req.DefaultWeeklyLimit, Timezone: req.Timezone,
		})
		if err != nil {
			return err
		}
		return s.audit(ctx, q, &orgID, &id.UserID, "organization.updated", "Organization settings changed")
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, orgResponse{Organization: toOrgView(org), Role: m.OrgRole})
	return nil
}

// handleTransferOrg makes another member the owner; the old owner becomes an admin.
func (s *Server) handleTransferOrg(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	m, err := authz.OrgMember(ctx, s.Store.Queries, orgID, id.UserID)
	if err != nil {
		return err
	}
	if m.OrgRole != authz.RoleOwner {
		return httpx.ErrForbidden
	}
	var req struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == id.UserID {
		return httpx.Validation{"user_id": "You already own this organization."}.Err()
	}
	to, err := authz.OrgMember(ctx, s.Store.Queries, orgID, req.UserID)
	if err != nil {
		return httpx.Validation{"user_id": "Pick someone in this organization."}.Err()
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		// Only one owner at a time: step down first.
		admin, owner := authz.RoleAdmin, authz.RoleOwner
		if _, err := t.Q.UpdateMembership(ctx, db.UpdateMembershipParams{OrgID: orgID, UserID: id.UserID, OrgRole: &admin}); err != nil {
			return err
		}
		if _, err := t.Q.UpdateMembership(ctx, db.UpdateMembershipParams{OrgID: orgID, UserID: to.UserID, OrgRole: &owner}); err != nil {
			return err
		}
		u, err := t.Q.GetUserByID(ctx, to.UserID)
		if err != nil {
			return err
		}
		if err := s.audit(ctx, t.Q, &orgID, &id.UserID, "organization.transferred", "Made "+u.Name+" the owner"); err != nil {
			return err
		}
		return t.Emit(ctx, core.Org(orgID, "people.updated"), map[string]any{"id": to.UserID})
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleDeleteOrg deletes an organization and everything in it. The owner types its name to
// confirm. Its computers are disconnected; their folders stay on disk.
func (s *Server) handleDeleteOrg(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := identity(r)
	m, err := authz.OrgMember(ctx, s.Store.Queries, orgID, id.UserID)
	if err != nil {
		return err
	}
	if m.OrgRole != authz.RoleOwner {
		return httpx.ErrForbidden
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	org, err := s.Store.GetOrganization(ctx, orgID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Confirm) != org.Name {
		return httpx.Validation{"confirm": "Type the organization’s name to confirm."}.Err()
	}
	computers, err := s.Store.ListOrgRunners(ctx, orgID)
	if err != nil {
		return err
	}
	workspaces, err := s.Store.ListOrgWorkspaces(ctx, orgID)
	if err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		// Open boards close themselves on this event.
		for _, ws := range workspaces {
			if err := t.Emit(ctx, core.Org(orgID, "workspace.deleted"), map[string]any{"id": ws.ID}); err != nil {
				return err
			}
		}
		return t.Q.DeleteOrganization(ctx, orgID)
	})
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "organization deleted", "org", orgID, "by", id.UserID)
	if s.Runners != nil {
		for _, c := range computers {
			_ = s.Runners.Send(c.ID, "revoked", map[string]any{})
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
