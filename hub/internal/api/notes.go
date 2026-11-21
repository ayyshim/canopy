package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

// Workspace notes: plans, decisions, summaries and free notes, kept in the hub.

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	view, err := s.loadWSView(ctx, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	notes, err := s.Store.ListNotes(ctx, db.ListNotesParams{WorkspaceID: wsID, ViewerID: &view.v.UserID})
	if err != nil {
		return err
	}
	goals, err := s.Store.ListGoals(ctx, wsID)
	if err != nil {
		return err
	}
	manager := map[uuid.UUID]*uuid.UUID{}
	for _, g := range goals {
		manager[g.ID] = g.ManagerID
	}
	out := make([]core.NoteView, 0, len(notes))
	for _, n := range notes {
		if n.GoalID != nil && !view.seesMember(manager[*n.GoalID]) {
			continue
		}
		out = append(out, core.ToNote(n))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"notes": out})
	return nil
}

type noteInput struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	Kind  *string `json:"kind"`
}

func (in *noteInput) validate(creating bool) error {
	v := httpx.Validation{}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
	}
	if (creating && (in.Title == nil || *in.Title == "")) || (in.Title != nil && *in.Title == "") {
		v.Add("title", "Give the note a title.")
	}
	if in.Kind != nil && *in.Kind != "note" && *in.Kind != "decision" {
		v.Add("kind", "Choose note or decision.")
	}
	return v.Err()
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	view, err := s.loadWSView(ctx, wsID, identity(r).UserID)
	if err != nil {
		return err
	}
	var in noteInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := in.validate(true); err != nil {
		return err
	}
	kind, body := "note", ""
	if in.Kind != nil {
		kind = *in.Kind
	}
	if in.Body != nil {
		body = *in.Body
	}
	var n db.Note
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		n, err = t.Q.CreateNote(ctx, db.CreateNoteParams{ID: newID(), WorkspaceID: wsID, Kind: kind, Title: *in.Title, Body: body, AuthorUserID: &view.v.UserID})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(view.ws.OrgID, wsID, "note.saved"), core.ToNote(n))
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"note": core.ToNote(n)})
	return nil
}

// loadNote: anyone in the workspace reads notes; their author or an admin changes them.
func (s *Server) loadNote(r *http.Request) (*wsView, db.Note, error) {
	id, err := pathID(r, "noteID")
	if err != nil {
		return nil, db.Note{}, err
	}
	n, err := s.Store.GetNote(r.Context(), id)
	if err != nil {
		return nil, n, httpx.ErrNotFound
	}
	view, err := s.loadWSView(r.Context(), n.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, n, err
	}
	if !view.v.Admin() && (n.AuthorUserID == nil || *n.AuthorUserID != view.v.UserID) {
		return nil, n, httpx.ErrForbidden
	}
	return view, n, nil
}

func (s *Server) handlePatchNote(w http.ResponseWriter, r *http.Request) error {
	view, n, err := s.loadNote(r)
	if err != nil {
		return err
	}
	var in noteInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := in.validate(false); err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		n, err = t.Q.UpdateNote(ctx, db.UpdateNoteParams{ID: n.ID, Title: in.Title, Body: in.Body, Kind: in.Kind})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(view.ws.OrgID, view.ws.ID, "note.saved"), core.ToNote(n))
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"note": core.ToNote(n)})
	return nil
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) error {
	view, n, err := s.loadNote(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteNote(ctx, n.ID); err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(view.ws.OrgID, view.ws.ID, "note.deleted"), map[string]any{"id": n.ID})
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
