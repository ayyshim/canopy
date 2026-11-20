package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// createGoal records a goal for a manager and wakes the orchestrator to plan it.
func (s *Server) createGoal(ctx context.Context, t *core.Tx, ws db.Workspace, managerID uuid.UUID, title, doneWhen, description string, by uuid.UUID) error {
	_, err := core.CreateGoal(ctx, t, ws, core.NewGoal{ManagerID: managerID, Title: title, DoneWhen: doneWhen, Description: description, By: by})
	if err != nil {
		return err
	}
	if s.Orch != nil {
		t.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), ws.ID) })
	}
	return nil
}

// loadOwnGoal loads a goal the caller may finish or move: its person, or a workspace admin.
func (s *Server) loadOwnGoal(r *http.Request) (*wsView, db.Goal, error) {
	view, g, err := s.loadMyGoal(r)
	if err != nil {
		return nil, g, err
	}
	if s.Orch == nil {
		return nil, g, httpx.NewError(http.StatusServiceUnavailable, "unavailable", "Agents aren’t running on this hub.")
	}
	return view, g, nil
}

// loadMyGoal loads a goal its person, or a workspace admin, may change.
func (s *Server) loadMyGoal(r *http.Request) (*wsView, db.Goal, error) {
	goalID, err := pathID(r, "goalID")
	if err != nil {
		return nil, db.Goal{}, err
	}
	ctx := r.Context()
	g, err := s.Store.GetGoal(ctx, goalID)
	if err != nil {
		return nil, g, httpx.ErrNotFound
	}
	view, err := s.loadWSView(ctx, g.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, g, err
	}
	if !view.seesMember(g.ManagerID) {
		return nil, g, httpx.ErrNotFound
	}
	if (g.CreatedBy == nil || *g.CreatedBy != view.v.UserID) && !view.v.Admin() {
		return nil, g, httpx.ErrForbidden
	}
	return view, g, nil
}

// loadDraft loads a draft goal the caller may change.
func (s *Server) loadDraft(r *http.Request) (*wsView, db.Goal, error) {
	view, g, err := s.loadMyGoal(r)
	if err == nil && g.Status != "draft" {
		err = httpx.NewError(http.StatusConflict, "not_draft", "This goal was already given.")
	}
	return view, g, err
}

// handleUpdateDraft saves changes to a draft goal.
func (s *Server) handleUpdateDraft(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadDraft(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var in goalInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if _, err := s.checkGoal(ctx, view, view.v.UserID, &in); err != nil {
		return err
	}
	var ng db.Goal
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		ng, err = core.UpdateDraft(ctx, t, view.ws, g.ID, in.newGoal(view.v.UserID))
		return err
	})
	if store.IsNotFound(err) {
		return httpx.NewError(http.StatusConflict, "not_draft", "This goal was already given.")
	} else if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"goal": core.ToGoal(ng)})
	return nil
}

// handleGiveDraft hands a draft to its manager; planning starts right away.
func (s *Server) handleGiveDraft(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadDraft(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if g.ManagerID == nil || !view.seesMember(g.ManagerID) {
		return httpx.Validation{"manager_id": "Choose a manager."}.Err()
	}
	if reason, err := s.limitReason(ctx, view.ws, view.v.UserID); err != nil {
		return err
	} else if reason != "" {
		return httpx.NewError(http.StatusConflict, "over_limit", strings.Replace(reason, "This starts", "Give it", 1))
	}
	var ng db.Goal
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		if ng, err = core.GiveDraft(ctx, t, view.ws, g.ID); err != nil {
			return err
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), view.ws.ID) })
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &view.v.UserID, "goals.given", "Gave a draft goal: "+g.Title)
	})
	if store.IsNotFound(err) {
		return httpx.NewError(http.StatusConflict, "not_draft", "This goal was already given.")
	} else if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"goal": core.ToGoal(ng)})
	return nil
}

// handleDeleteGoal deletes a draft goal; given goals stay.
func (s *Server) handleDeleteGoal(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadDraft(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		_, err := core.DeleteDraft(ctx, t, view.ws, g)
		return err
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func goalError(err error) error {
	switch {
	case errors.Is(err, orch.ErrGoalNotReady):
		return httpx.NewError(http.StatusConflict, "goal_not_ready", err.Error())
	case errors.Is(err, orch.ErrNotAllowed):
		return httpx.Validation{"runner_id": err.Error()}.Err()
	}
	return err
}

// handleShipGoal: open a pull request, merge into the base, or keep the branch.
func (s *Server) handleShipGoal(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadOwnGoal(r)
	if err != nil {
		return err
	}
	var in struct {
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Action != "pr" && in.Action != "merge" && in.Action != "keep" {
		return httpx.Validation{"action": "Choose open a pull request, merge, or keep the branch."}.Err()
	}
	res, err := s.Orch.ShipGoal(r.Context(), g.ID, in.Action, view.v.UserID)
	if err != nil {
		if errors.Is(err, orch.ErrGoalNotReady) {
			return goalError(err)
		}
		return httpx.NewError(http.StatusConflict, "ship_failed", err.Error())
	}
	httpx.JSON(w, http.StatusOK, res)
	return nil
}

// handleGoalDone marks a ready goal done without shipping.
func (s *Server) handleGoalDone(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadOwnGoal(r)
	if err != nil {
		return err
	}
	if err := s.Orch.MarkGoalDone(r.Context(), g.ID, view.v.UserID); err != nil {
		return goalError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleMoveGoal runs a goal on another computer the goal's person may use.
func (s *Server) handleMoveGoal(w http.ResponseWriter, r *http.Request) error {
	view, g, err := s.loadOwnGoal(r)
	if err != nil {
		return err
	}
	var in struct {
		RunnerID uuid.UUID `json:"runner_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if rn, err := s.Store.GetRunner(r.Context(), in.RunnerID); err != nil || rn.OrgID != view.ws.OrgID {
		return httpx.Validation{"runner_id": "Choose a computer in this organization."}.Err()
	}
	if err := s.Orch.MoveGoal(r.Context(), g.ID, in.RunnerID, view.v.UserID, view.v.Admin()); err != nil {
		return goalError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handlePatchGoal shows or hides a done goal's tasks on the board.
func (s *Server) handlePatchGoal(w http.ResponseWriter, r *http.Request) error {
	_, g, err := s.loadOwnGoal(r)
	if err != nil {
		return err
	}
	var in struct {
		ShowTasks *bool `json:"show_tasks"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.ShowTasks != nil {
		if err := s.Orch.ShowGoalTasks(r.Context(), g.ID, *in.ShowTasks); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
