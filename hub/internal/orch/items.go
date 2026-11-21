package orch

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/refs"
	"canopy/hub/internal/store/db"
)

func goalLabel(g db.Goal) string { return fmt.Sprintf("G-%d %s", g.Number, g.Title) }
func taskLabel(t db.Task) string { return fmt.Sprintf("T-%d %s", t.Number, t.Title) }
func noteLabel(n db.Note) string { return fmt.Sprintf("N-%d %s", n.Number, n.Title) }

const (
	maxRefItems = 10
	maxRefChars = 1500
)

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ReferencedItems is the short "Referenced items" block for text that mentions G-, T- or N-
// refs. It only lists items the given person may see; with no person, nothing is added.
func (o *Orch) ReferencedItems(ctx context.Context, ws db.Workspace, userID *uuid.UUID, text string) string {
	found := refs.Parse(text)
	if len(found) == 0 || userID == nil {
		return ""
	}
	if len(found) > maxRefItems {
		found = found[:maxRefItems]
	}
	sc, err := refs.ViewerScope(ctx, o.Store.Queries, ws, *userID)
	if err != nil {
		return ""
	}
	items, err := refs.Resolver{Q: o.Store.Queries}.Resolve(ctx, ws.ID, sc, found)
	if err != nil || len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nReferenced items (use read_item for the full detail):\n")
	for _, it := range items {
		fmt.Fprintf(&b, "- %s: %s [%s]", it.Ref, it.Title, it.Status())
		if body := trim(it.Text(), maxRefChars); body != "" {
			b.WriteString("\n  " + strings.ReplaceAll(body, "\n", "\n  "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (o *Orch) referencedFor(ctx context.Context, wsID uuid.UUID, userID *uuid.UUID, text string) string {
	ws, err := o.Store.GetWorkspace(ctx, wsID)
	if err != nil {
		return ""
	}
	return o.ReferencedItems(ctx, ws, userID, text)
}

// taskReferences expands refs in what a task prompt shows: the goal description, the task
// description and its steps. Visibility is the person the work is for.
func (o *Orch) taskReferences(ctx context.Context, st *wsState, t db.Task) string {
	var text strings.Builder
	user := t.AttributedUserID
	if g := goalOf(st, t); g != nil {
		text.WriteString(g.Description + "\n")
		if user == nil {
			user = g.CreatedBy
		}
	}
	if user == nil {
		user = t.CreatedBy
	}
	text.WriteString(t.Description + "\n")
	for _, s := range core.TaskSteps(t) {
		text.WriteString(s.Title + "\n")
	}
	return o.ReferencedItems(ctx, st.ws, user, text.String())
}

// toolReadItem shows one goal, task or note by its ref.
func (o *Orch) toolReadItem(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Ref string `json:"ref"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	ref, ok := refs.ParseOne(in.Ref)
	if !ok {
		return toolError("Give a ref like G-12, T-34 or N-5.")
	}
	it, ok, err := refs.Resolver{Q: o.Store.Queries}.One(ctx, tc.ws.ID, refs.AllScope(), ref)
	if err != nil {
		return err
	}
	if !ok {
		return toolError("No item " + ref.String() + " in this workspace.")
	}
	members, err := o.Store.ListMembers(ctx, tc.ws.ID)
	if err != nil {
		return err
	}
	name := func(id *uuid.UUID) string {
		if id != nil {
			for _, m := range members {
				if m.ID == *id {
					return m.Name
				}
			}
		}
		return "nobody"
	}
	var b strings.Builder
	switch {
	case it.Goal != nil:
		g := *it.Goal
		fmt.Fprintf(&b, "%s [%s], manager %s\n", goalLabel(g), g.Status, name(g.ManagerID))
		writeField(&b, "Description", g.Description)
		writeField(&b, "Done when", g.DoneWhen)
		writeField(&b, "Summary", g.Summary)
		tasks, err := o.Store.ListGoalTasks(ctx, &g.ID)
		if err != nil {
			return err
		}
		b.WriteString("Tasks:\n")
		for _, t := range tasks {
			if t.Kind == "plan" {
				continue
			}
			fmt.Fprintf(&b, "- %s [%s], %s\n", taskLabel(t), t.Status, name(t.MemberID))
		}
	case it.Task != nil:
		t := *it.Task
		fmt.Fprintf(&b, "%s [%s], owner %s\n", taskLabel(t), t.Status, name(t.MemberID))
		if t.GoalID != nil {
			if g, err := o.Store.GetGoal(ctx, *t.GoalID); err == nil {
				fmt.Fprintf(&b, "Goal: %s\n", goalLabel(g))
			}
		}
		writeField(&b, "Description", t.Description)
		if steps := core.TaskSteps(t); len(steps) > 0 {
			b.WriteString("Steps:\n")
			for i, s := range steps {
				mark := " "
				if s.Done {
					mark = "x"
				}
				fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, mark, s.Title)
			}
		}
		writeField(&b, "Summary", t.Summary)
		outs, err := o.Store.ListOutputs(ctx, tc.ws.ID)
		if err != nil {
			return err
		}
		for _, out := range outs {
			if out.TaskID != nil && *out.TaskID == t.ID {
				fmt.Fprintf(&b, "Output: %s (%s, %s)\n", out.Title, out.Name, out.Status)
			}
		}
	default:
		n := *it.Note
		fmt.Fprintf(&b, "%s (%s, updated %s)\n\n%s\n", noteLabel(n), n.Kind, n.UpdatedAt.Format("Jan 2"), strings.TrimSpace(n.Body))
	}
	o.reply(tc, b.String(), false)
	return nil
}

func writeField(b *strings.Builder, label, text string) {
	if strings.TrimSpace(text) != "" {
		fmt.Fprintf(b, "%s: %s\n", label, strings.TrimSpace(text))
	}
}
