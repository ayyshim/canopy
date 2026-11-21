package orch

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/refs"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Workspace knowledge lives in the hub as notes (DECISIONS: Workspace knowledge): plans and
// summaries per goal, decisions and free notes. Agents read and write them with tools.

// saveNote writes a note. A goal has one plan and one summary, so those are replaced.
func (o *Orch) saveNote(ctx context.Context, t *core.Tx, ws db.Workspace, goalID *uuid.UUID, kind, title, body, visibility string, byUser, byMember *uuid.UUID) (db.Note, error) {
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
			AuthorUserID: byUser, AuthorMemberID: byMember, Visibility: visibility})
	}
	if err != nil {
		return n, err
	}
	return n, t.EmitNoteSaved(ctx, ws, nil, n)
}

// workingFor is the person this session works for: the person in a chat, or the person who
// created the goal. Private notes of that person are the only ones the agent may see.
func (o *Orch) workingFor(ctx context.Context, tc *toolCtx) *uuid.UUID {
	if tc.sess.PersonID != nil {
		return tc.sess.PersonID
	}
	goalID := tc.sess.GoalID
	if goalID == nil && tc.task != nil {
		goalID = tc.task.GoalID
	}
	if goalID != nil {
		if g, err := o.Store.GetGoal(ctx, *goalID); err == nil {
			return g.CreatedBy
		}
	}
	return nil
}

var errNoNote = fmt.Errorf("no note")

func (o *Orch) toolReadNotes(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Query string `json:"query"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	notes, err := o.Store.ListNotes(ctx, db.ListNotesParams{WorkspaceID: tc.ws.ID, ViewerID: o.workingFor(ctx, tc)})
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
		Title      string `json:"title"`
		Body       string `json:"body"`
		Kind       string `json:"kind"`
		Visibility string `json:"visibility"`
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
	visibility := "shared"
	var byUser *uuid.UUID
	if in.Visibility == "private" {
		byUser = o.workingFor(ctx, tc)
		if byUser == nil {
			return toolError("A private note belongs to a person. Make it shared, or write it while working for someone.")
		}
		visibility = "private"
	}
	var saved db.Note
	err := o.Core.Tx(ctx, func(t *core.Tx) (err error) {
		saved, err = o.saveNote(ctx, t, tc.ws, goalID, in.Kind, in.Title, in.Body, visibility, byUser, &tc.member.ID)
		return err
	})
	if err != nil {
		return err
	}
	o.reply(tc, "Saved the note "+noteLabel(saved)+".", false)
	return nil
}

// toolUpdateNote edits a shared note, or a private note of the person the agent works for.
// The old text is saved as a revision first and the agent is recorded as the editor.
func (o *Orch) toolUpdateNote(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Ref   string  `json:"ref"`
		Title *string `json:"title"`
		Body  *string `json:"body"`
		Mode  string  `json:"mode"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	ref, ok := refs.ParseOne(in.Ref)
	if !ok || ref.Kind != refs.Note {
		return toolError("Give a note ID like N-5.")
	}
	if in.Mode != "" && in.Mode != "append" && in.Mode != "replace" {
		return toolError("Mode is append or replace.")
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return toolError("The title can't be empty.")
	}
	if in.Title == nil && in.Body == nil {
		return toolError("Give a new title or body.")
	}
	it, ok, err := refs.Resolver{Q: o.Store.Queries}.One(ctx, tc.ws.ID, refs.ForUser(o.workingFor(ctx, tc)), ref)
	if err != nil {
		return err
	}
	if !ok {
		return toolError("No note " + ref.String() + " in this workspace.")
	}
	var saved db.Note
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		cur, err := t.Q.GetNote(ctx, it.ID)
		if err != nil {
			return err
		}
		p := db.UpdateNoteParams{ID: cur.ID, Title: in.Title, SetEditor: true, EditedByMemberID: &tc.member.ID}
		if in.Body != nil {
			body := *in.Body
			if in.Mode != "replace" && strings.TrimSpace(cur.Body) != "" {
				body = strings.TrimRight(cur.Body, "\n ") + "\n\n" + body
			}
			p.Body = &body
		}
		if err := t.SaveNoteRevision(ctx, cur.ID); err != nil {
			return err
		}
		if saved, err = t.Q.UpdateNote(ctx, p); err != nil {
			return err
		}
		return t.EmitNoteSaved(ctx, tc.ws, &cur, saved)
	})
	if err != nil {
		return err
	}
	o.reply(tc, "Updated the note "+noteLabel(saved)+".", false)
	return nil
}
