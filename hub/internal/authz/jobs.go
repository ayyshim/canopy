package authz

import (
	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// CanSeeJob: whoever can see the job's manager. A job without a manager is for admins and its owner.
func (v Viewer) CanSeeJob(j db.Job, members map[uuid.UUID]db.Member) bool {
	if v.Admin() {
		return true
	}
	if j.ManagerID == nil {
		return j.OwnerUserID != nil && *j.OwnerUserID == v.UserID
	}
	m, ok := members[*j.ManagerID]
	return ok && v.CanSeeMember(m)
}

// CanEditJob: the owner and workspace admins edit, pause, resume, run and delete.
func (v Viewer) CanEditJob(j db.Job) bool {
	return v.Admin() || (j.OwnerUserID != nil && *j.OwnerUserID == v.UserID)
}
