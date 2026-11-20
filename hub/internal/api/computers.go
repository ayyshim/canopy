package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Computers (runners): each person's own, plus optional shared ones (DECISIONS: Where work runs).

// handlePatchRunner renames a computer, or (admins) shares it with people who have none.
func (s *Server) handlePatchRunner(w http.ResponseWriter, r *http.Request) error {
	runnerID, err := pathID(r, "runnerID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	rn, err := s.Store.GetRunner(ctx, runnerID)
	if err != nil {
		return httpx.ErrNotFound
	}
	if err := s.canManageRunner(ctx, rn, uid); err != nil {
		return err
	}
	var in struct {
		Name   *string `json:"name"`
		Shared *bool   `json:"shared"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	val := httpx.Validation{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" {
			val.Add("name", "Give the computer a name.")
		}
	}
	if in.Shared != nil && *in.Shared {
		m, err := authz.OrgMember(ctx, s.Store.Queries, rn.OrgID, uid)
		if err != nil || !authz.CanAdministerOrg(m.OrgRole) {
			return httpx.ErrForbidden
		}
		if !s.signsInWithKey(ctx, rn.ID) {
			val.Add("shared", "Only a computer signed in with an API key can be shared. A subscription is personal.")
		}
	}
	if err := val.Err(); err != nil {
		return err
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		nr, err := t.Q.UpdateRunner(ctx, db.UpdateRunnerParams{ID: rn.ID, Name: in.Name, Shared: in.Shared})
		if err != nil {
			return err
		}
		rn = nr
		summary := "Changed the computer " + nr.Name
		if in.Shared != nil {
			summary = map[bool]string{true: "Shared the computer ", false: "Stopped sharing the computer "}[*in.Shared] + nr.Name
		}
		if err := t.Emit(ctx, core.Org(rn.OrgID, "runner.updated"), s.liveRunnerView(nr)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &rn.OrgID, &uid, "runners.changed", summary)
	})
	if err != nil {
		return err
	}
	if s.Orch != nil {
		go s.Orch.Kick(context.WithoutCancel(ctx), uuid.Nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"runner": s.liveRunnerView(rn)})
	return nil
}

func (s *Server) signsInWithKey(ctx context.Context, runnerID uuid.UUID) bool {
	conns, err := s.Store.ListRunnerConnections(ctx, runnerID)
	if err != nil || len(conns) == 0 {
		return false
	}
	for _, c := range conns {
		if c.Enabled && c.Mode != "api_key" {
			return false
		}
	}
	return true
}

func (s *Server) liveRunnerView(rn db.Runner) core.RunnerView {
	v := toRunnerView(rn)
	if s.Runners != nil {
		v.Online = s.Runners.Online(rn.ID)
	}
	return v
}

// handleSetMyComputer chooses the computer a person's goals run on: their own or a shared one.
func (s *Server) handleSetMyComputer(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, uid); err != nil {
		return err
	}
	var in struct {
		RunnerID *uuid.UUID `json:"runner_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.RunnerID != nil {
		rn, err := s.Store.GetRunner(ctx, *in.RunnerID)
		if err != nil || rn.OrgID != orgID || rn.RevokedAt != nil || (rn.OwnerUserID != uid && !rn.Shared) {
			return httpx.Validation{"runner_id": "Choose your own computer or a shared one."}.Err()
		}
	}
	if err := s.Store.SetDefaultRunner(ctx, db.SetDefaultRunnerParams{OrgID: orgID, UserID: uid, RunnerID: in.RunnerID}); err != nil {
		return err
	}
	if s.Orch != nil {
		go s.Orch.Kick(context.WithoutCancel(ctx), uuid.Nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"my_computer": in.RunnerID})
	return nil
}

// handleRepoInfo tells the directory picker whether a folder is a git repository, its branch
// and how many files have uncommitted changes.
func (s *Server) handleRepoInfo(w http.ResponseWriter, r *http.Request) error {
	runnerID, err := pathID(r, "runnerID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	rn, err := s.Store.GetRunner(ctx, runnerID)
	if err != nil || rn.RevokedAt != nil || rn.OwnerUserID != identity(r).UserID {
		return httpx.ErrNotFound
	}
	info, err := s.repoInfo(ctx, runnerID, r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, info)
	return nil
}

func (s *Server) repoInfo(ctx context.Context, runnerID uuid.UUID, path string) (protocol.RepoInfoResult, error) {
	var out protocol.RepoInfoResult
	if s.Runners == nil || !s.Runners.Online(runnerID) {
		return out, httpx.NewError(http.StatusConflict, "runner_offline", "That computer isn’t connected right now.")
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := s.Runners.Request(rctx, runnerID, protocol.TRepoInfo, protocol.RepoInfo{Path: path}, &out); err != nil {
		return out, httpx.NewError(http.StatusBadGateway, "repo_info_failed", "Couldn’t read that folder: "+err.Error())
	}
	return out, nil
}

// bindOwnerDirectories points directories a person added before pairing at their new
// computer, and records repositories there as their clone.
func bindOwnerDirectories(ctx context.Context, q *db.Queries, rn db.Runner) error {
	dirs, err := q.BindUnboundDirectories(ctx, db.BindUnboundDirectoriesParams{OwnerUserID: rn.OwnerUserID, MachineID: &rn.ID, OrgID: rn.OrgID})
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if d.RepoRemote == "" {
			continue
		}
		if _, err := q.UpsertClone(ctx, db.UpsertCloneParams{ID: newID(), DirectoryID: d.ID, RunnerID: rn.ID, UserID: rn.OwnerUserID, Path: d.Path}); err != nil {
			return err
		}
	}
	return nil
}
