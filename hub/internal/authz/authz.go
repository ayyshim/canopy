// Package authz is the single place where the hub decides who may do what.
// Objects in organizations the caller can't see are reported as not found, never forbidden.
package authz

import (
	"context"

	"github.com/google/uuid"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// OrgMember returns the caller's active membership in orgID, or ErrNotFound.
func OrgMember(ctx context.Context, q *db.Queries, orgID, userID uuid.UUID) (db.Membership, error) {
	m, err := q.GetActiveMembership(ctx, db.GetActiveMembershipParams{OrgID: orgID, UserID: userID})
	if store.IsNotFound(err) {
		return m, httpx.ErrNotFound
	}
	return m, err
}

// CanAdministerOrg covers organization settings, invites, roles, limits, CLI connections and
// creating or deleting workspaces (SPEC "People, permissions and presence").
func CanAdministerOrg(role string) bool { return role == RoleOwner || role == RoleAdmin }

// OrgAdmin returns the caller's membership if they may administer orgID.
func OrgAdmin(ctx context.Context, q *db.Queries, orgID, userID uuid.UUID) (db.Membership, error) {
	m, err := OrgMember(ctx, q, orgID, userID)
	if err != nil {
		return m, err
	}
	if !CanAdministerOrg(m.OrgRole) {
		return m, httpx.ErrForbidden
	}
	return m, nil
}
