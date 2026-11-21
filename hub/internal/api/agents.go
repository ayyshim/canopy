package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Resolver answers signals (the orchestrator).
type Resolver interface {
	Resolve(ctx context.Context, signalID uuid.UUID, by *uuid.UUID, r orch.Resolution) (db.Signal, error)
}

// visibility helpers for one workspace

type wsView struct {
	ws      db.Workspace
	v       authz.Viewer
	members map[uuid.UUID]db.Member
}

func (s *Server) loadWSView(ctx context.Context, wsID, userID uuid.UUID) (*wsView, error) {
	ws, v, err := authz.LoadWorkspace(ctx, s.Store.Queries, wsID, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.Store.ListMembers(ctx, wsID)
	if err != nil {
		return nil, err
	}
	w := &wsView{ws: ws, v: v, members: map[uuid.UUID]db.Member{}}
	for _, m := range members {
		w.members[m.ID] = m
	}
	return w, nil
}

func (w *wsView) seesMember(id *uuid.UUID) bool {
	if id == nil {
		return w.v.Admin()
	}
	m, ok := w.members[*id]
	return ok && w.v.CanSeeMember(m)
}

func (w *wsView) seesSignal(sig db.Signal) bool {
	if sig.ForUserID != nil {
		// Limit, cap and "goal ready" cards are for one person: the computer's owner or the
		// goal's person. Admins also see goal cards and handover briefs.
		return *sig.ForUserID == w.v.UserID || ((sig.Type == "goal_ready" || sig.Type == "review") && w.v.Admin())
	}
	if core.PersonalSignal(sig.Type) {
		return w.v.Admin() // a shared computer's limits
	}
	if sig.Type == "owner_change" || sig.Type == "sync_conflict" {
		var p map[string]any
		_ = json.Unmarshal(sig.Payload, &p)
		if fmt.Sprint(p["owner_user_id"]) == w.v.UserID.String() {
			return true
		}
	}
	return w.seesMember(sig.MemberID)
}

// ---- Goals ----

// goalInput is what a person writes when giving a goal or saving a draft.
type goalInput struct {
	ManagerID   uuid.UUID  `json:"manager_id"`
	Title       string     `json:"title"`
	DoneWhen    string     `json:"done_when"`
	Description string     `json:"description"`
	RunnerID    *uuid.UUID `json:"runner_id"` // empty: the person's own computer, else a shared one
	Base        string     `json:"base"`      // empty: each repository's default branch
	Snapshot    bool       `json:"snapshot"`  // include their uncommitted changes
	Draft       bool       `json:"draft"`     // save without starting work
}

func (in goalInput) newGoal(by uuid.UUID) core.NewGoal {
	return core.NewGoal{ManagerID: in.ManagerID, Title: in.Title, DoneWhen: in.DoneWhen, Description: in.Description, By: by,
		RunnerID: in.RunnerID, Base: in.Base, Snapshot: in.Snapshot, Draft: in.Draft}
}

// checkGoal trims and validates a goal; it returns the manager's name.
func (s *Server) checkGoal(ctx context.Context, view *wsView, uid uuid.UUID, in *goalInput) (string, error) {
	val := httpx.Validation{}
	in.Title, in.DoneWhen, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.DoneWhen), strings.TrimSpace(in.Description)
	m, ok := view.members[in.ManagerID]
	// People give goals only to managers they can see.
	if !ok || m.Kind != "manager" || !view.v.CanSeeMember(m) {
		val.Add("manager_id", "Choose a manager.")
	}
	if in.Title == "" {
		val.Add("title", "Describe the goal.")
	} else if utf8.RuneCountInString(in.Title) > 2000 {
		val.Add("title", "Keep the goal under 2,000 characters.")
	}
	in.Base = strings.TrimSpace(in.Base)
	if strings.ContainsAny(in.Base, " ~^:?*[\\") || strings.HasPrefix(in.Base, "-") {
		val.Add("base", "That isn’t a branch name.")
	}
	if in.RunnerID != nil {
		rn, err := s.Store.GetRunner(ctx, *in.RunnerID)
		if err != nil || rn.OrgID != view.ws.OrgID || rn.RevokedAt != nil || (rn.OwnerUserID != uid && !rn.Shared) {
			val.Add("runner_id", "Choose your own computer or a shared one.")
		}
	}
	return m.Name, val.Err()
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) error {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	view, err := s.loadWSView(ctx, wsID, uid)
	if err != nil {
		return err
	}
	var in goalInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	name, err := s.checkGoal(ctx, view, uid, &in)
	if err != nil {
		return err
	}
	if !in.Draft {
		if queued, reason, err := s.queueIfOverLimit(ctx, view.ws, uid, "goal", map[string]any{"manager_id": in.ManagerID, "title": in.Title, "done_when": in.DoneWhen, "description": in.Description}); err != nil {
			return err
		} else if queued {
			httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true, "reason": reason})
			return nil
		}
	}
	var goal db.Goal
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		goal, err = core.CreateGoal(ctx, t, view.ws, in.newGoal(uid))
		if err != nil {
			return err
		}
		if in.Draft {
			return s.audit(ctx, t.Q, &view.ws.OrgID, &uid, "goals.drafted", "Saved a draft goal for "+name+": "+in.Title)
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), view.ws.ID) })
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &uid, "goals.given", "Gave "+name+" a goal: "+in.Title)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"goal": core.ToGoal(goal)})
	return nil
}

func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) error {
	goalID, err := pathID(r, "goalID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	g, err := s.Store.GetGoal(ctx, goalID)
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	view, err := s.loadWSView(ctx, g.WorkspaceID, identity(r).UserID)
	if err != nil {
		return err
	}
	if !view.seesMember(g.ManagerID) {
		return httpx.ErrNotFound
	}
	tasks, err := s.Store.ListGoalTasks(ctx, &g.ID)
	if err != nil {
		return err
	}
	urows, err := s.Store.UsageWorkForGoal(ctx, g.ID)
	if err != nil {
		return err
	}
	gu, tu := core.SumUsage(urows)
	tv := []core.TaskView{}
	for _, t := range tasks {
		if view.seesMember(t.MemberID) {
			x := core.ToTask(t)
			x.Usage = usageOrZero(tu[t.ID])
			tv = append(tv, x)
		}
	}
	events, err := s.Store.ListGoalEvents(ctx, g.ID)
	if err != nil {
		return err
	}
	ev := make([]core.GoalEventView, 0, len(events))
	for _, e := range events {
		ev = append(ev, core.ToGoalEvent(e))
	}
	repos, err := s.Store.ListGoalRepos(ctx, g.ID)
	if err != nil {
		return err
	}
	rv := make([]core.GoalRepoView, 0, len(repos))
	for _, x := range repos {
		rv = append(rv, core.ToGoalRepo(x))
	}
	gv := core.ToGoal(g)
	gv.Usage = usageOrZero(gu[g.ID])
	httpx.JSON(w, http.StatusOK, map[string]any{"goal": gv, "tasks": tv, "events": ev, "repos": rv})
	return nil
}

// ---- Tasks ----

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	uid := identity(r).UserID
	var goal *db.Goal
	var wsID uuid.UUID
	if gid := pathParam(r, "goalID"); gid != "" {
		id, err := uuid.Parse(gid)
		if err != nil {
			return httpx.ErrNotFound
		}
		g, err := s.Store.GetGoal(ctx, id)
		if err != nil {
			return httpx.ErrNotFound
		}
		goal, wsID = &g, g.WorkspaceID
	} else {
		id, err := pathID(r, "wsID")
		if err != nil {
			return err
		}
		wsID = id
	}
	view, err := s.loadWSView(ctx, wsID, uid)
	if err != nil {
		return err
	}
	if goal != nil && !view.seesMember(goal.ManagerID) {
		return httpx.ErrNotFound
	}
	var in struct {
		Title       string      `json:"title"`
		Description string      `json:"description"`
		MemberID    uuid.UUID   `json:"member_id"`
		DependsOn   []uuid.UUID `json:"depends_on"`
		Steps       []string    `json:"steps"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	val := httpx.Validation{}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		val.Add("title", "Give the task a title.")
	}
	m, ok := view.members[in.MemberID]
	if !ok || !view.v.CanSeeMember(m) {
		val.Add("member_id", "Choose who does it.")
	}
	for _, dep := range in.DependsOn {
		t, err := s.Store.GetTask(ctx, dep)
		if err != nil || t.WorkspaceID != wsID || !view.seesMember(t.MemberID) {
			val.Add("depends_on", "Choose tasks from this workspace.")
		}
	}
	if err := val.Err(); err != nil {
		return err
	}
	if in.DependsOn == nil {
		in.DependsOn = []uuid.UUID{}
	}
	steps := []core.Step{}
	for _, st := range in.Steps {
		if strings.TrimSpace(st) != "" {
			steps = append(steps, core.Step{Title: strings.TrimSpace(st)})
		}
	}
	stepsJSON, _ := json.Marshal(steps)
	var task db.Task
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var goalID *uuid.UUID
		if goal != nil {
			goalID = &goal.ID
		}
		if goal != nil && s.Orch != nil {
			// A follow-up task puts a ready or done goal back to work.
			if err := s.Orch.ReopenGoal(ctx, t, view.ws, *goal); err != nil {
				return err
			}
		}
		kind := "work"
		if goalID != nil && m.Kind == "manager" {
			kind = "triage"
		}
		var err error
		task, err = t.Q.CreateTask(ctx, db.CreateTaskParams{ID: newID(), WorkspaceID: wsID, GoalID: goalID, MemberID: &m.ID, Kind: kind,
			Title: in.Title, Description: in.Description, Skill: m.Skill, Status: "waiting", DependsOn: in.DependsOn, Steps: stepsJSON,
			RequiredDirs: []byte("[]"), AttributedUserID: &uid, CreatedBy: &uid})
		if err != nil {
			return err
		}
		e := core.WSMember(view.ws.OrgID, wsID, m.ID, "task.created")
		if err := t.Emit(ctx, e, core.ToTask(task)); err != nil {
			return err
		}
		if s.Orch != nil {
			t.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), wsID) })
		}
		return s.audit(ctx, t.Q, &view.ws.OrgID, &uid, "tasks.created", "Created the task "+in.Title+" for "+m.Name)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"task": core.ToTask(task)})
	return nil
}

func (s *Server) loadTask(r *http.Request) (*wsView, db.Task, error) {
	taskID, err := pathID(r, "taskID")
	if err != nil {
		return nil, db.Task{}, err
	}
	t, err := s.Store.GetTask(r.Context(), taskID)
	if store.IsNotFound(err) {
		return nil, t, httpx.ErrNotFound
	} else if err != nil {
		return nil, t, err
	}
	view, err := s.loadWSView(r.Context(), t.WorkspaceID, identity(r).UserID)
	if err != nil {
		return nil, t, err
	}
	if !view.seesMember(t.MemberID) {
		return nil, t, httpx.ErrNotFound
	}
	return view, t, nil
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) error {
	view, t, err := s.loadTask(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	signals, err := s.Store.ListTaskOpenSignals(ctx, &t.ID)
	if err != nil {
		return err
	}
	sv := []core.SignalView{}
	for _, sig := range signals {
		if view.seesSignal(sig) {
			sv = append(sv, core.ToSignal(sig))
		}
	}
	comments, err := s.Store.ListComments(ctx, t.ID)
	if err != nil {
		return err
	}
	cv := []map[string]any{}
	for _, c := range comments {
		cv = append(cv, map[string]any{"id": c.ID, "author_user_id": c.AuthorUserID, "author_member_id": c.AuthorMemberID, "text": c.Text, "created_at": c.CreatedAt})
	}
	logs, err := s.Store.ListTaskLog(ctx, db.ListTaskLogParams{TaskID: &t.ID, Limit: 200})
	if err != nil {
		return err
	}
	lv := []core.LogLineView{}
	for i := len(logs) - 1; i >= 0; i-- {
		lv = append(lv, core.ToLog(logs[i]))
	}
	urows, err := s.Store.UsageWorkForTask(ctx, t.ID)
	if err != nil {
		return err
	}
	_, tu := core.SumUsage(urows)
	tvw := core.ToTask(t)
	tvw.Usage = usageOrZero(tu[t.ID])
	httpx.JSON(w, http.StatusOK, map[string]any{"task": tvw, "signals": sv, "comments": cv, "activity": lv})
	return nil
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) error {
	view, t, err := s.loadTask(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	var in struct {
		MemberID  *uuid.UUID  `json:"member_id"`
		DependsOn []uuid.UUID `json:"depends_on"`
		Reopen    bool        `json:"reopen"`
		Note      string      `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.MemberID != nil {
		if m, ok := view.members[*in.MemberID]; !ok || !view.v.CanSeeMember(m) {
			return httpx.Validation{"member_id": "Choose who does it."}.Err()
		}
		if t.Status == "doing" || t.Status == "review" {
			return httpx.Validation{"member_id": "This task is in progress; wait for it to finish."}.Err()
		}
	}
	var out db.Task
	err = s.Core.Tx(ctx, func(tx *core.Tx) error {
		if in.Reopen {
			// Done never goes back: reopening creates a follow-up task.
			if t.Status != "done" {
				return httpx.Validation{"reopen": "Only done tasks can be reopened."}.Err()
			}
			title := "Follow up: " + t.Title
			desc := t.Description
			if in.Note != "" {
				desc = in.Note + "\n\n" + desc
			}
			nt, err := tx.Q.CreateTask(ctx, db.CreateTaskParams{ID: newID(), WorkspaceID: t.WorkspaceID, GoalID: t.GoalID, MemberID: t.MemberID, Kind: "work",
				Title: title, Description: desc, Skill: t.Skill, Status: "waiting", DependsOn: []uuid.UUID{t.ID}, Steps: []byte("[]"),
				RequiredDirs: t.RequiredDirs, AttributedUserID: &uid, CreatedBy: &uid})
			if err != nil {
				return err
			}
			out = nt
		} else {
			var err error
			out, err = tx.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: t.ID, SetMember: in.MemberID != nil, MemberID: in.MemberID, DependsOn: in.DependsOn})
			if err != nil {
				return err
			}
		}
		e := core.WS(view.ws.OrgID, view.ws.ID, "task.updated")
		e.MemberID = out.MemberID
		if err := tx.Emit(ctx, e, core.ToTask(out)); err != nil {
			return err
		}
		if s.Orch != nil {
			tx.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), view.ws.ID) })
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"task": core.ToTask(out)})
	return nil
}

// ---- Signals ----

func (s *Server) handleResolveSignal(w http.ResponseWriter, r *http.Request) error {
	signalID, err := pathID(r, "signalID")
	if err != nil {
		return err
	}
	var in orch.Resolution
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	out, err := s.resolveSignalAs(r.Context(), identity(r).UserID, signalID, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"signal": core.ToSignal(out)})
	return nil
}

// resolveSignalAs answers a signal for a person after the same checks the board uses:
// they must see it, and only a directory's owner decides owner changes and sync conflicts.
func (s *Server) resolveSignalAs(ctx context.Context, uid, signalID uuid.UUID, in orch.Resolution) (db.Signal, error) {
	sig, err := s.Store.GetSignal(ctx, signalID)
	if store.IsNotFound(err) {
		return db.Signal{}, httpx.ErrNotFound
	} else if err != nil {
		return db.Signal{}, err
	}
	view, err := s.loadWSView(ctx, sig.WorkspaceID, uid)
	if err != nil {
		return db.Signal{}, err
	}
	if !view.seesSignal(sig) {
		return db.Signal{}, httpx.ErrNotFound
	}
	if sig.Type == "owner_change" || sig.Type == "sync_conflict" {
		var p map[string]any
		_ = json.Unmarshal(sig.Payload, &p)
		if fmt.Sprint(p["owner_user_id"]) != uid.String() {
			return db.Signal{}, httpx.NewError(http.StatusForbidden, "forbidden", "Only the directory’s owner can decide this.")
		}
	}
	if s.Resolver == nil {
		return db.Signal{}, httpx.NewError(http.StatusServiceUnavailable, "unavailable", "Agents aren’t running on this hub.")
	}
	out, err := s.Resolver.Resolve(ctx, signalID, &uid, in)
	switch {
	case errors.Is(err, orch.ErrSignalClosed):
		return db.Signal{}, httpx.NewError(http.StatusConflict, "already_resolved", err.Error())
	case errors.Is(err, orch.ErrBadAction):
		return db.Signal{}, httpx.Validation{"action": err.Error()}.Err()
	case errors.Is(err, orch.ErrNoteRequired):
		return db.Signal{}, httpx.Validation{"note": err.Error()}.Err()
	case err != nil:
		return db.Signal{}, err
	}
	return out, nil
}

// handleMemberActivity is the chat window's Activity tab: sessions and the live log.
func (s *Server) handleMemberActivity(w http.ResponseWriter, r *http.Request) error {
	_, v, m, err := s.loadMember(r)
	if err != nil {
		return err
	}
	_ = v
	ctx := r.Context()
	limit := int32(300)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 1000 {
		limit = int32(n)
	}
	logs, err := s.Store.ListMemberLog(ctx, db.ListMemberLogParams{MemberID: &m.ID, Limit: limit})
	if err != nil {
		return err
	}
	lv := []core.LogLineView{}
	for i := len(logs) - 1; i >= 0; i-- {
		lv = append(lv, core.ToLog(logs[i]))
	}
	sessions, err := s.Store.ListMemberSessions(ctx, db.ListMemberSessionsParams{MemberID: m.ID, Limit: 20})
	if err != nil {
		return err
	}
	sv := []core.SessionView{}
	for _, x := range sessions {
		sv = append(sv, core.ToSession(x))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"log": lv, "sessions": sv})
	return nil
}

var _ = slices.Contains[[]string]

// usageOrZero gives a task or goal with no recorded work empty totals instead of null.
func usageOrZero(u *core.UsageTotals) *core.UsageTotals {
	if u == nil {
		return &core.UsageTotals{ByModel: []core.ModelUsage{}}
	}
	return u
}
