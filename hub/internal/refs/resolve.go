package refs

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Scope says which items one reader may see. It mirrors the board snapshot: a goal or task
// is visible when its manager or owner is, and a note when it has no goal or its goal is.
type Scope struct {
	member func(*uuid.UUID) bool
	user   *uuid.UUID // whose private notes show; nil sees none
}

// AllScope sees everything in the workspace except private notes.
func AllScope() Scope { return ForUser(nil) }

// ForUser is the scope of an agent working for one person: the whole workspace, plus that
// person's private notes.
func ForUser(userID *uuid.UUID) Scope {
	return Scope{member: func(*uuid.UUID) bool { return true }, user: userID}
}

// NoteVisible is the private-note rule: only the author, never admins.
func (sc Scope) NoteVisible(n db.Note) bool { return NoteReadableBy(n, sc.user) }

// NoteReadableBy says whether a note's visibility lets this person read it.
func NoteReadableBy(n db.Note, userID *uuid.UUID) bool {
	return n.Visibility != "private" || (userID != nil && n.AuthorUserID != nil && *n.AuthorUserID == *userID)
}

// ViewerScope is what one person sees in one workspace.
func ViewerScope(ctx context.Context, q *db.Queries, ws db.Workspace, userID uuid.UUID) (Scope, error) {
	v, err := authz.WorkspaceViewer(ctx, q, ws, userID)
	if err != nil {
		return Scope{}, err
	}
	members, err := q.ListMembers(ctx, ws.ID)
	if err != nil {
		return Scope{}, err
	}
	byID := map[uuid.UUID]db.Member{}
	for _, m := range members {
		byID[m.ID] = m
	}
	return Scope{user: &userID, member: func(id *uuid.UUID) bool {
		if id == nil {
			return v.Admin()
		}
		m, ok := byID[*id]
		return ok && v.CanSeeMember(m)
	}}, nil
}

// Item is one resolved goal, task or note.
type Item struct {
	Ref   Ref
	ID    uuid.UUID
	Title string
	Goal  *db.Goal
	Task  *db.Task
	Note  *db.Note
}

func (it Item) Status() string {
	switch {
	case it.Goal != nil:
		return it.Goal.Status
	case it.Task != nil:
		return it.Task.Status
	}
	return it.Note.Kind
}

// Text is the summary, description or body, whichever the item has.
func (it Item) Text() string {
	switch {
	case it.Goal != nil:
		return firstText(it.Goal.Summary, it.Goal.Description, it.Goal.DoneWhen)
	case it.Task != nil:
		return firstText(it.Task.Summary, it.Task.Description)
	}
	return it.Note.Body
}

func firstText(parts ...string) string {
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			return p
		}
	}
	return ""
}

type Resolver struct{ Q *db.Queries }

// Resolve looks refs up in one workspace. Unknown and hidden refs are left out.
func (r Resolver) Resolve(ctx context.Context, wsID uuid.UUID, sc Scope, refs []Ref) ([]Item, error) {
	var out []Item
	for _, ref := range refs {
		it, ok, err := r.One(ctx, wsID, sc, ref)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// One resolves a single ref; ok is false when it is unknown or hidden.
func (r Resolver) One(ctx context.Context, wsID uuid.UUID, sc Scope, ref Ref) (Item, bool, error) {
	it := Item{Ref: ref}
	switch ref.Kind {
	case Goal:
		g, err := r.Q.GetGoalByNumber(ctx, db.GetGoalByNumberParams{WorkspaceID: wsID, Number: ref.Number})
		if err != nil {
			return Item{}, false, notFoundOK(err)
		}
		if !goalVisible(sc, g) {
			return Item{}, false, nil
		}
		it.ID, it.Title, it.Goal = g.ID, g.Title, &g
	case Task:
		t, err := r.Q.GetTaskByNumber(ctx, db.GetTaskByNumberParams{WorkspaceID: wsID, Number: ref.Number})
		if err != nil {
			return Item{}, false, notFoundOK(err)
		}
		if !sc.member(t.MemberID) {
			return Item{}, false, nil
		}
		it.ID, it.Title, it.Task = t.ID, t.Title, &t
	case Note:
		n, err := r.Q.GetNoteByNumber(ctx, db.GetNoteByNumberParams{WorkspaceID: wsID, Number: ref.Number})
		if err != nil {
			return Item{}, false, notFoundOK(err)
		}
		if !sc.NoteVisible(n) {
			return Item{}, false, nil
		}
		if n.GoalID != nil {
			g, err := r.Q.GetGoal(ctx, *n.GoalID)
			if err != nil {
				return Item{}, false, notFoundOK(err)
			}
			if !goalVisible(sc, g) {
				return Item{}, false, nil
			}
		}
		it.ID, it.Title, it.Note = n.ID, n.Title, &n
	default:
		return Item{}, false, nil
	}
	return it, true, nil
}

func goalVisible(sc Scope, g db.Goal) bool { return sc.member(g.ManagerID) }

func notFoundOK(err error) error {
	if store.IsNotFound(err) {
		return nil
	}
	return err
}

// Search lists visible items whose number or title matches q, for the # picker. An empty q
// lists the newest items.
func (r Resolver) Search(ctx context.Context, wsID uuid.UUID, sc Scope, q string, limit int) ([]Item, error) {
	q = strings.TrimSpace(q)
	var wantNum int32
	var wantKind Kind
	words := strings.Fields(strings.ToLower(q))
	if ref, ok := ParseOne(q); ok {
		wantKind, wantNum, words = ref.Kind, ref.Number, nil
	} else if n, err := strconv.Atoi(strings.TrimPrefix(q, "#")); err == nil && n > 0 {
		wantNum, words = int32(n), nil
	}
	match := func(k Kind, num int32, title string) bool {
		if wantNum != 0 {
			return num == wantNum && (wantKind == 0 || wantKind == k)
		}
		low := strings.ToLower(title)
		for _, w := range words {
			if !strings.Contains(low, w) {
				return false
			}
		}
		return true
	}

	goals, err := r.Q.ListGoals(ctx, wsID)
	if err != nil {
		return nil, err
	}
	visibleGoals := map[uuid.UUID]bool{}
	var items []Item
	for _, g := range goals {
		if !goalVisible(sc, g) {
			continue
		}
		visibleGoals[g.ID] = true
		if match(Goal, g.Number, g.Title) {
			items = append(items, Item{Ref: Ref{Goal, g.Number}, ID: g.ID, Title: g.Title, Goal: &g})
		}
	}
	tasks, err := r.Q.ListTasks(ctx, wsID)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if sc.member(t.MemberID) && match(Task, t.Number, t.Title) {
			items = append(items, Item{Ref: Ref{Task, t.Number}, ID: t.ID, Title: t.Title, Task: &t})
		}
	}
	notes, err := r.Q.ListNotes(ctx, db.ListNotesParams{WorkspaceID: wsID, ViewerID: sc.user})
	if err != nil {
		return nil, err
	}
	for _, n := range notes {
		if (n.GoalID == nil || visibleGoals[*n.GoalID]) && match(Note, n.Number, n.Title) {
			items = append(items, Item{Ref: Ref{Note, n.Number}, ID: n.ID, Title: n.Title, Note: &n})
		}
	}
	// Newest first within a type, goals before tasks before notes.
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Ref, items[j].Ref
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		return a.Number > b.Number
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func kindOrder(k Kind) int { return strings.IndexByte("GTN", byte(k)) }

// Label is "T-34 Title", how agents see an item.
func (it Item) Label() string { return fmt.Sprintf("%s %s", it.Ref, it.Title) }
