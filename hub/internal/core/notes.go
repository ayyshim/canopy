package core

import (
	"context"

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
