package core

import (
	"time"

	"context"
	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// EmitNoteSaved tells boards about a saved note. A private note goes to its author's
// sockets only. prev is the note before the change, so a note turning private is first
// removed from everyone else's board.
func (t *Tx) EmitNoteSaved(ctx context.Context, ws db.Workspace, prev *db.Note, n db.Note) error {
	if n.Visibility != "private" || n.AuthorUserID == nil {
		return t.Emit(ctx, WS(ws.OrgID, ws.ID, "note.saved"), ToNote(n))
	}
	if prev != nil && prev.Visibility != "private" {
		if err := t.Emit(ctx, WS(ws.OrgID, ws.ID, "note.deleted"), map[string]any{"id": n.ID}); err != nil {
			return err
		}
	}
	return t.Emit(ctx, WSUser(ws.OrgID, ws.ID, *n.AuthorUserID, "note.saved"), ToNote(n))
}

// EmitNoteDeleted is the matching event for a removed note.
func (t *Tx) EmitNoteDeleted(ctx context.Context, ws db.Workspace, n db.Note) error {
	data := map[string]any{"id": n.ID}
	if n.Visibility == "private" && n.AuthorUserID != nil {
		return t.Emit(ctx, WSUser(ws.OrgID, ws.ID, *n.AuthorUserID, "note.deleted"), data)
	}
	return t.Emit(ctx, WS(ws.OrgID, ws.ID, "note.deleted"), data)
}

// NoteColors is the fixed sticky palette.
var NoteColors = []string{"yellow", "green", "blue", "pink", "purple", "orange", "gray"}

// SaveNoteRevision keeps the note's current text as a revision and drops all but the last 20.
// Call it before a change to the title, body or kind.
func (t *Tx) SaveNoteRevision(ctx context.Context, noteID uuid.UUID) error {
	if _, err := t.Q.AddNoteRevision(ctx, db.AddNoteRevisionParams{ID: uuid.New(), ForNote: noteID}); err != nil {
		return err
	}
	return t.Q.PruneNoteRevisions(ctx, noteID)
}

type NoteRevisionView struct {
	Rev              int32      `json:"rev"`
	Title            string     `json:"title"`
	Body             string     `json:"body"`
	Kind             string     `json:"kind"`
	EditedByUserID   *uuid.UUID `json:"edited_by_user_id"`
	EditedByMemberID *uuid.UUID `json:"edited_by_member_id"`
	CreatedAt        time.Time  `json:"created_at"`
}

func ToNoteRevision(r db.NoteRevision) NoteRevisionView {
	return NoteRevisionView{Rev: r.Rev, Title: r.Title, Body: r.Body, Kind: r.Kind, EditedByUserID: r.EditedByUserID,
		EditedByMemberID: r.EditedByMemberID, CreatedAt: r.CreatedAt}
}
