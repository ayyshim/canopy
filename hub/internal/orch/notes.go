package orch

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Workspace knowledge lives in the hub as notes (DECISIONS: Workspace knowledge): plans and
// summaries per goal, decisions and free notes. Agents read and write them with tools.

// saveNote writes a note. A goal has one plan and one summary, so those are replaced.
func (o *Orch) saveNote(ctx context.Context, t *core.Tx, ws db.Workspace, goalID *uuid.UUID, kind, title, body string, byUser, byMember *uuid.UUID) (db.Note, error) {
	var n db.Note
	var err error
	if goalID != nil && (kind == "plan" || kind == "summary") {
		n, err = t.Q.GoalNote(ctx, db.GoalNoteParams{GoalID: goalID, Kind: kind})
		if err == nil {
			n, err = t.Q.UpdateNote(ctx, db.UpdateNoteParams{ID: n.ID, Title: &title, Body: &body})
		} else if store.IsNotFound(err) {
			err = errNoNote
		}
	} else {
		err = errNoNote
	}
	if err == errNoNote {
		n, err = t.Q.CreateNote(ctx, db.CreateNoteParams{ID: newID(), WorkspaceID: ws.ID, GoalID: goalID, Kind: kind, Title: title, Body: body,
			AuthorUserID: byUser, AuthorMemberID: byMember})
	}
	if err != nil {
		return n, err
	}
	return n, t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "note.saved"), core.ToNote(n))
}

var errNoNote = fmt.Errorf("no note")

func (o *Orch) toolReadNotes(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Query string `json:"query"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	notes, err := o.Store.ListNotes(ctx, tc.ws.ID)
	if err != nil {
		return err
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	var b strings.Builder
	shown := 0
	for _, n := range notes {
		if q != "" && !strings.Contains(strings.ToLower(n.Title+"\n"+n.Body), q) {
			continue
		}
		fmt.Fprintf(&b, "## %s (%s, %s)\n\n%s\n\n", noteLabel(n), n.Kind, n.UpdatedAt.Format("Jan 2"), strings.TrimSpace(n.Body))
		shown++
		if b.Len() > 40000 {
			b.WriteString("(more notes left out; search with a query)\n")
			break
		}
	}
	if shown == 0 {
		o.reply(tc, "No notes yet.", false)
		return nil
	}
	o.reply(tc, b.String(), false)
	return nil
}

func (o *Orch) toolWriteNote(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Kind  string `json:"kind"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Body) == "" {
		return toolError("Give the note a title and a body.")
	}
	if in.Kind != "decision" {
		in.Kind = "note"
	}
	var goalID *uuid.UUID
	if tc.task != nil {
		goalID = tc.task.GoalID
	}
	var saved db.Note
	err := o.Core.Tx(ctx, func(t *core.Tx) (err error) {
		saved, err = o.saveNote(ctx, t, tc.ws, goalID, in.Kind, in.Title, in.Body, nil, &tc.member.ID)
		return err
	})
	if err != nil {
		return err
	}
	o.reply(tc, "Saved the note "+noteLabel(saved)+".", false)
	return nil
}
