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
	"canopy/shared/protocol"
)

// handleCreateDirectory adds a folder from the person's computer. A git repository whose
// remote the workspace already has becomes that person's clone of it instead
// (DECISIONS: Code in git repositories). Adding a new directory takes a workspace admin.
func (s *Server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) error {
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
	var in struct {
		Path     string     `json:"path"`
		Name     string     `json:"name"`
		Kind     string     `json:"kind"`
		RunnerID *uuid.UUID `json:"runner_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if !validAbsPath(in.Path) {
		return httpx.Validation{"path": "Enter a full path, like /home/you/payments."}.Err()
	}
	in.Path = cleanPath(in.Path)
	if strings.TrimSpace(in.Name) == "" {
		in.Name = domain.DirName(in.Path)
	}
	if in.Kind == "" {
		in.Kind = domain.ClassifyDir(in.Path)
	}
	machineID := s.personComputer(ctx, ws.OrgID, uid, in.RunnerID)
	repo := s.detectRepo(ctx, machineID, in.Path)

	existing, err := s.Store.ListDirectories(ctx, ws.ID)
	if err != nil {
		return err
	}
	if repo.IsRepo && repo.Remote != "" {
		for _, d := range existing {
			if domain.SameRemote(d.RepoRemote, repo.Remote) {
				return s.linkClone(w, ctx, ws, d, uid, machineID, in.Path, repo)
			}
		}
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	var dir db.Directory
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		params := db.CreateDirectoryParams{ID: newID(), WorkspaceID: ws.ID, Path: in.Path, Name: strings.TrimSpace(in.Name), Kind: in.Kind,
			OwnerUserID: uid, MachineID: machineID}
		if repo.IsRepo && repo.Remote != "" {
			params.RepoRemote, params.DefaultBranch = repo.Remote, repo.DefaultBranch
		}
		var err error
		dir, err = t.Q.CreateDirectory(ctx, params)
		if store.IsUniqueViolation(err, "") {
			return httpx.Validation{"path": "This directory is already in the workspace."}.Err()
		} else if err != nil {
			return err
		}
		if dir.RepoRemote != "" && machineID != nil {
			if _, err := t.Q.UpsertClone(ctx, db.UpsertCloneParams{ID: newID(), DirectoryID: dir.ID, RunnerID: *machineID, UserID: uid, Path: dir.Path,
				CurrentBranch: repo.CurrentBranch, Dirty: int32(repo.Dirty)}); err != nil {
				return err
			}
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.updated"), core.ToDirectory(dir)); err != nil {
			return err
		}
		// Managers with "all directories" get the new one.
		members, err := t.Q.ListMembers(ctx, ws.ID)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.Kind == "manager" && m.AllDirectories {
				if _, err := t.Q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: m.ID, DirectoryID: dir.ID, Mode: domain.ReadWrite, Source: "human", GrantedBy: &uid}); err != nil {
					return err
				}
				if err := emitMember(ctx, t, ws, m); err != nil {
					return err
				}
			}
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.DirectoriesChanged(context.WithoutCancel(ctx), ws.ID) })
		}
		if dir.MachineID != nil && dir.RepoRemote == "" {
			machine := *dir.MachineID
			t.AfterCommit(func() { s.rewatch(context.WithoutCancel(ctx), machine) })
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &uid, "access.directory_added", "Added the directory "+dir.Path)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"directory": core.ToDirectory(dir)})
	return nil
}

// linkClone records a person's own clone of a repository the workspace already has.
func (s *Server) linkClone(w http.ResponseWriter, ctx context.Context, ws db.Workspace, d db.Directory, uid uuid.UUID, machineID *uuid.UUID, path string, repo protocol.RepoInfoResult) error {
	if machineID == nil {
		return httpx.Validation{"path": "Connect your computer first; your clone lives there."}.Err()
	}
	var c db.DirectoryClone
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		c, err = t.Q.UpsertClone(ctx, db.UpsertCloneParams{ID: newID(), DirectoryID: d.ID, RunnerID: *machineID, UserID: uid, Path: path,
			CurrentBranch: repo.CurrentBranch, Dirty: int32(repo.Dirty)})
		if err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.clone"), core.ToClone(c)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &uid, "access.clone_linked", "Linked a clone of "+d.Name)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"directory": core.ToDirectory(d), "clone": core.ToClone(c), "linked": true})
	return nil
}

// personComputer is the computer a person adds directories from: the one they picked, else
// their computer for goals, else any computer of theirs.
func (s *Server) personComputer(ctx context.Context, orgID, uid uuid.UUID, picked *uuid.UUID) *uuid.UUID {
	runners, err := s.Store.ListOrgRunners(ctx, orgID)
	if err != nil {
		return nil
	}
	m, _ := authz.OrgMember(ctx, s.Store.Queries, orgID, uid)
	var mine *uuid.UUID
	for _, rn := range runners {
		if rn.OwnerUserID != uid {
			continue
		}
		id := rn.ID
		if picked != nil && *picked == id {
			return &id
		}
		if m.DefaultRunnerID != nil && *m.DefaultRunnerID == id {
			mine = &id
		} else if mine == nil && rn.Kind == "computer" {
			mine = &id
		}
	}
	return mine
}

// detectRepo asks the computer about a folder; an offline computer means "a plain folder".
func (s *Server) detectRepo(ctx context.Context, runnerID *uuid.UUID, path string) protocol.RepoInfoResult {
	if runnerID == nil || s.Runners == nil || !s.Runners.Online(*runnerID) {
		return protocol.RepoInfoResult{}
	}
	info, err := s.repoInfo(ctx, *runnerID, path)
	if err != nil {
		return protocol.RepoInfoResult{}
	}
	return info
}

func (s *Server) loadDirectory(r *http.Request) (db.Workspace, authz.Viewer, db.Directory, error) {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return db.Workspace{}, authz.Viewer{}, db.Directory{}, err
	}
	dirID, err := pathID(r, "dirID")
	if err != nil {
		return db.Workspace{}, authz.Viewer{}, db.Directory{}, err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return ws, v, db.Directory{}, err
	}
	d, err := s.Store.GetDirectory(r.Context(), dirID)
	if store.IsNotFound(err) || (err == nil && d.WorkspaceID != ws.ID) {
		return ws, v, d, httpx.ErrNotFound
	}
	return ws, v, d, err
}

func (s *Server) handlePatchDirectory(w http.ResponseWriter, r *http.Request) error {
	ws, v, d, err := s.loadDirectory(r)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	var in struct {
		Name         *string   `json:"name"`
		Kind         *string   `json:"kind"`
		SetupCommand *string   `json:"setup_command"`
		CopyFiles    *[]string `json:"copy_files"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Kind != nil && *in.Kind != "code" && *in.Kind != "docs" && *in.Kind != "design" && *in.Kind != "other" {
		return httpx.Validation{"kind": "Pick code, docs, design or other."}.Err()
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		var copyFiles []string
		if in.CopyFiles != nil {
			copyFiles = *in.CopyFiles
		}
		d, err = t.Q.UpdateDirectory(ctx, db.UpdateDirectoryParams{ID: d.ID, Name: in.Name, Kind: in.Kind, SetupCommand: in.SetupCommand, CopyFiles: copyFiles})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.updated"), core.ToDirectory(d))
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"directory": core.ToDirectory(d)})
	return nil
}

func (s *Server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) error {
	ws, v, d, err := s.loadDirectory(r)
	if err != nil {
		return err
	}
	if err := v.RequireAdmin(); err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteDirectory(ctx, d.ID); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.deleted"), map[string]any{"id": d.ID}); err != nil {
			return err
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.DirectoriesChanged(context.WithoutCancel(ctx), ws.ID) })
		}
		if d.MachineID != nil {
			machine := *d.MachineID
			t.AfterCommit(func() { s.rewatch(context.WithoutCancel(ctx), machine) })
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &v.UserID, "access.directory_removed", "Removed the directory "+d.Path)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handlePutDirectoryAccess sets one person's access to a directory. Only the directory's owner
// or a workspace admin may (DECISIONS: Directories).
func (s *Server) handlePutDirectoryAccess(w http.ResponseWriter, r *http.Request) error {
	ws, v, d, err := s.loadDirectory(r)
	if err != nil {
		return err
	}
	if !v.Admin() && d.OwnerUserID != v.UserID {
		return httpx.ErrForbidden
	}
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	var in struct {
		Mode string `json:"mode"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Mode != "none" && in.Mode != "read" && in.Mode != "read_write" {
		return httpx.Validation{"mode": "Choose no access, read, or read and write."}.Err()
	}
	if userID == d.OwnerUserID {
		return httpx.Validation{"user_id": "The owner always has full access."}.Err()
	}
	if _, err := authz.OrgMember(ctx, s.Store.Queries, ws.OrgID, userID); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if _, err := t.Q.UpsertDirectoryAccess(ctx, db.UpsertDirectoryAccessParams{ID: newID(), DirectoryID: d.ID, UserID: userID, Mode: in.Mode, GrantedBy: &v.UserID}); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.access"), map[string]any{"directory_id": d.ID, "user_id": userID, "mode": in.Mode}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &v.UserID, "access.person_directory", "Set "+in.Mode+" access to "+d.Path)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"directory_id": d.ID, "user_id": userID, "mode": in.Mode})
	return nil
}

// ---- People's workspace access ----

type accessView struct {
	UserID           string   `json:"user_id"`
	Name             string   `json:"name"`
	Email            string   `json:"email"`
	AvatarColor      string   `json:"avatar_color"`
	Role             string   `json:"role"`
	ManagerIDs       []string `json:"manager_ids"`
	IndependentStaff bool     `json:"independent_staff"`
}

func (s *Server) handleListAccess(w http.ResponseWriter, r *http.Request) error {
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
	rows, err := s.Store.ListWorkspaceAccess(ctx, ws.ID)
	if err != nil {
		return err
	}
	out := make([]accessView, 0, len(rows))
	for _, a := range rows {
		ids := make([]string, 0, len(a.ManagerIds))
		for _, id := range a.ManagerIds {
			ids = append(ids, id.String())
		}
		out = append(out, accessView{UserID: a.UserID.String(), Name: a.UserName, Email: a.UserEmail, AvatarColor: a.UserAvatarColor,
			Role: a.Role, ManagerIDs: ids, IndependentStaff: a.IndependentStaff})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"access": out})
	return nil
}

func (s *Server) handlePutAccess(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userID")
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
	var in struct {
		Role             string   `json:"role"`
		ManagerIDs       []string `json:"manager_ids"`
		IndependentStaff bool     `json:"independent_staff"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Role != "admin" && in.Role != "staff" {
		return httpx.Validation{"role": "Choose admin or staff."}.Err()
	}
	if _, err := authz.OrgMember(ctx, s.Store.Queries, ws.OrgID, userID); err != nil {
		return err
	}
	members, err := s.Store.ListMembers(ctx, ws.ID)
	if err != nil {
		return err
	}
	managers := map[string]bool{}
	for _, m := range members {
		if m.Kind == "manager" {
			managers[m.ID.String()] = true
		}
	}
	ids, err := parseUUIDs(in.ManagerIDs)
	if err != nil {
		return httpx.Validation{"manager_ids": "Choose managers from this workspace."}.Err()
	}
	for _, id := range in.ManagerIDs {
		if !managers[id] {
			return httpx.Validation{"manager_ids": "Choose managers from this workspace."}.Err()
		}
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if _, err := t.Q.UpsertWorkspaceAccess(ctx, db.UpsertWorkspaceAccessParams{ID: newID(), WorkspaceID: ws.ID, UserID: userID, Role: in.Role,
			ManagerIds: ids, IndependentStaff: in.IndependentStaff}); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "access.updated"), map[string]any{"user_id": userID}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &v.UserID, "people.workspace_access", "Changed workspace access in "+ws.Name)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDeleteAccess(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	userID, err := pathID(r, "userID")
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
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteWorkspaceAccess(ctx, db.DeleteWorkspaceAccessParams{WorkspaceID: ws.ID, UserID: userID}); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "access.updated"), map[string]any{"user_id": userID}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &v.UserID, "people.workspace_access", "Removed workspace access in "+ws.Name)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// rewatch tells a machine to start or stop syncing directories it shares.
func (s *Server) rewatch(ctx context.Context, machineID uuid.UUID) {
	if rw, ok := s.Mirror.(interface {
		Rewatch(ctx context.Context, machineID uuid.UUID)
	}); ok {
		rw.Rewatch(ctx, machineID)
	}
}
