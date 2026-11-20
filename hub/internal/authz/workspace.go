package authz

import (
	"context"

	"github.com/google/uuid"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Viewer is what one person may see and do in one workspace
// (SPEC "People, permissions and presence").
type Viewer struct {
	UserID      uuid.UUID
	OrgRole     string
	Role        string // "admin" (org owner/admin or workspace admin) or "staff"
	Managers    map[uuid.UUID]bool
	Independent bool
}

func (v Viewer) Admin() bool { return v.Role == "admin" }

// OrgAdmin reports whether the viewer administers the whole organization.
func (v Viewer) OrgAdmin() bool { return CanAdministerOrg(v.OrgRole) }

// CanSeeMember: admins see every agent; staff see granted managers' teams and, if granted,
// independent staff.
func (v Viewer) CanSeeMember(m db.Member) bool {
	if v.Admin() {
		return true
	}
	switch {
	case m.Kind == "manager":
		return v.Managers[m.ID]
	case m.ReportsTo != nil:
		return v.Managers[*m.ReportsTo]
	default:
		return v.Independent
	}
}

// WorkspaceViewer loads the caller's view of a workspace; no access is reported as not found.
func WorkspaceViewer(ctx context.Context, q *db.Queries, ws db.Workspace, userID uuid.UUID) (Viewer, error) {
	m, err := OrgMember(ctx, q, ws.OrgID, userID)
	if err != nil {
		return Viewer{}, err
	}
	v := Viewer{UserID: userID, OrgRole: m.OrgRole, Managers: map[uuid.UUID]bool{}}
	if CanAdministerOrg(m.OrgRole) {
		v.Role = "admin"
		return v, nil
	}
	a, err := q.GetWorkspaceAccess(ctx, db.GetWorkspaceAccessParams{WorkspaceID: ws.ID, UserID: userID})
	if store.IsNotFound(err) {
		return v, httpx.ErrNotFound
	} else if err != nil {
		return v, err
	}
	v.Role = a.Role
	v.Independent = a.IndependentStaff
	for _, id := range a.ManagerIds {
		v.Managers[id] = true
	}
	return v, nil
}

// LoadWorkspace fetches a workspace and the caller's view of it.
func LoadWorkspace(ctx context.Context, q *db.Queries, wsID, userID uuid.UUID) (db.Workspace, Viewer, error) {
	ws, err := q.GetWorkspace(ctx, wsID)
	if store.IsNotFound(err) {
		return ws, Viewer{}, httpx.ErrNotFound
	} else if err != nil {
		return ws, Viewer{}, err
	}
	v, err := WorkspaceViewer(ctx, q, ws, userID)
	return ws, v, err
}

// RequireAdmin returns ErrForbidden unless the viewer administers the workspace.
func (v Viewer) RequireAdmin() error {
	if !v.Admin() {
		return httpx.ErrForbidden
	}
	return nil
}

// PersonDirMode is a person's access to a directory: the owner always read_write; an explicit
// row wins; otherwise everyone who can open the workspace may read (DECISIONS: Directories).
func PersonDirMode(dir db.Directory, userID uuid.UUID, explicit map[uuid.UUID]string) string {
	if dir.OwnerUserID == userID {
		return "read_write"
	}
	if mode, ok := explicit[userID]; ok {
		return mode
	}
	return "read"
}
