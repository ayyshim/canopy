package api

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/refs"
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
		if !refs.NoteReadableBy(n, &view.v.UserID) || (n.GoalID != nil && !view.seesMember(manager[*n.GoalID])) {
			continue
		}
		out = append(out, core.ToNote(n))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"notes": out})
	return nil
}

type noteInput struct {
	Title      *string         `json:"title"`
	Body       *string         `json:"body"`
	Kind       *string         `json:"kind"`
	Visibility *string         `json:"visibility"`
	Color      *string         `json:"color"`
	Board      json.RawMessage `json:"board"` // {"x":1,"y":2} pins the note; null unpins it
}

type boardPos struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

// pos returns the pinned position; set is true when the request mentions the board at all.
func (in *noteInput) pos() (p *boardPos, set bool) {
	if len(in.Board) == 0 {
		return nil, false
	}
	if bytes.Equal(bytes.TrimSpace(in.Board), []byte("null")) {
		return nil, true
	}
	var b boardPos
	if json.Unmarshal(in.Board, &b) != nil || b.X == nil || b.Y == nil || !finite(*b.X) || !finite(*b.Y) {
		return &boardPos{}, true
	}
	return &b, true
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e7 }

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
	if in.Visibility != nil && *in.Visibility != "shared" && *in.Visibility != "private" {
		v.Add("visibility", "Choose shared or private.")
	}
	if in.Color != nil && !slices.Contains(core.NoteColors, *in.Color) {
		v.Add("color", "Choose one of: "+strings.Join(core.NoteColors, ", ")+".")
	}
	if p, set := in.pos(); set && p != nil && p.X == nil {
		v.Add("board", "Give the board position as x and y, or null.")
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
	params := db.CreateNoteParams{ID: newID(), WorkspaceID: wsID, Kind: kind, Title: *in.Title, Body: body, AuthorUserID: &view.v.UserID}
	if in.Visibility != nil {
		params.Visibility = *in.Visibility
	}
	if in.Color != nil {
		params.Color = *in.Color
	}
	if p, _ := in.pos(); p != nil {
		params.BoardX, params.BoardY = p.X, p.Y
	}
	var n db.Note
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		n, err = t.Q.CreateNote(ctx, params)
		if err != nil {
			return err
		}
		return t.EmitNoteSaved(ctx, view.ws, nil, n)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"note": core.ToNote(n)})
	return nil
}

// seeNote loads a note the caller may see. Private notes are the author's alone, admins
// included, and notes on a goal the caller can't see are hidden too; both answer 404.
func (s *Server) seeNote(r *http.Request) (*wsView, db.Note, error) {
	id, err := pathID(r, "noteID")
	if err != nil {
		return nil, db.Note{}, err
	}
	ctx := r.Context()
	n, err := s.Store.GetNote(ctx, id)
	if err != nil {
		return nil, n, httpx.ErrNotFound
	}
	view, err := s.loadWSView(ctx, n.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, n, err
	}
	if !refs.NoteReadableBy(n, &view.v.UserID) {
		return nil, n, httpx.ErrNotFound
	}
	if n.GoalID != nil {
		g, err := s.Store.GetGoal(ctx, *n.GoalID)
		if err != nil || !view.seesMember(g.ManagerID) {
			return nil, n, httpx.ErrNotFound
		}
	}
	return view, n, nil
}

func isAuthor(view *wsView, n db.Note) bool {
	return n.AuthorUserID != nil && *n.AuthorUserID == view.v.UserID
}

// loadNote is seeNote plus the right to change: the author or an admin (admins only for
// shared notes, which seeNote already guarantees for them).
func (s *Server) loadNote(r *http.Request) (*wsView, db.Note, error) {
	view, n, err := s.seeNote(r)
	if err != nil {
		return nil, n, err
	}
	if !view.v.Admin() && !isAuthor(view, n) {
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
	if in.Visibility != nil && *in.Visibility != n.Visibility && !isAuthor(view, n) {
		return httpx.ErrForbidden
	}
	ctx := r.Context()
	if err := s.guardJobMemory(ctx, n, in.Kind != nil || in.Body != nil || in.Title != nil); err != nil {
		return err
	}
	prev := n
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		p := db.UpdateNoteParams{ID: n.ID, Visibility: in.Visibility, Color: in.Color}
		changed := (in.Title != nil && *in.Title != n.Title) || (in.Body != nil && *in.Body != n.Body) || (in.Kind != nil && *in.Kind != n.Kind)
		if changed {
			if err := t.SaveNoteRevision(ctx, n.ID); err != nil {
				return err
			}
			p.Title, p.Body, p.Kind = in.Title, in.Body, in.Kind
			p.SetEditor, p.EditedByUserID = true, &view.v.UserID
		}
		if pos, set := in.pos(); set {
			p.SetPos = true
			if pos != nil {
				p.BoardX, p.BoardY = pos.X, pos.Y
			}
		}
		var err error
		n, err = t.Q.UpdateNote(ctx, p)
		if err != nil {
			return err
		}
		return t.EmitNoteSaved(ctx, view.ws, &prev, n)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"note": core.ToNote(n)})
	return nil
}

// guardJobMemory keeps a job's memory note safe: it is kept with its job, and changes wait
// until the job's run has finished.
func (s *Server) guardJobMemory(ctx context.Context, n db.Note, changing bool) error {
	if n.Kind != "job_memory" || n.JobID == nil || !changing {
		return nil
	}
	if _, err := s.Store.RunningJobRun(ctx, *n.JobID); err == nil {
		return httpx.NewError(http.StatusConflict, "run_in_progress", "A run is going. Edit the memory when it has finished.")
	}
	return nil
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) error {
	view, n, err := s.loadNote(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if n.Kind == "job_memory" {
		return httpx.NewError(http.StatusConflict, "job_memory", "This is a job’s memory. Delete the job instead.")
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.DeleteNote(ctx, n.ID); err != nil {
			return err
		}
		return t.EmitNoteDeleted(ctx, view.ws, n)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleListNoteRevisions(w http.ResponseWriter, r *http.Request) error {
	_, n, err := s.seeNote(r)
	if err != nil {
		return err
	}
	revs, err := s.Store.ListNoteRevisions(r.Context(), n.ID)
	if err != nil {
		return err
	}
	out := make([]core.NoteRevisionView, 0, len(revs))
	for _, rv := range revs {
		out = append(out, core.ToNoteRevision(rv))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"revisions": out})
	return nil
}

// handleRestoreNoteRevision puts an old version back. The current text is kept as a new
// revision first, so a restore can be undone.
func (s *Server) handleRestoreNoteRevision(w http.ResponseWriter, r *http.Request) error {
	view, n, err := s.loadNote(r)
	if err != nil {
		return err
	}
	rev, err := strconv.ParseInt(pathParam(r, "rev"), 10, 32)
	if err != nil {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	if err := s.guardJobMemory(ctx, n, true); err != nil {
		return err
	}
	old, err := s.Store.GetNoteRevision(ctx, db.GetNoteRevisionParams{NoteID: n.ID, Rev: int32(rev)})
	if err != nil {
		return httpx.ErrNotFound
	}
	prev := n
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.SaveNoteRevision(ctx, n.ID); err != nil {
			return err
		}
		var err error
		n, err = t.Q.UpdateNote(ctx, db.UpdateNoteParams{ID: n.ID, Title: &old.Title, Body: &old.Body, Kind: &old.Kind,
			SetEditor: true, EditedByUserID: &view.v.UserID})
		if err != nil {
			return err
		}
		return t.EmitNoteSaved(ctx, view.ws, &prev, n)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"note": core.ToNote(n)})
	return nil
}
