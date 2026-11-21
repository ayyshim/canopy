package orch

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// schedule is one pass over a workspace (SPEC "Orchestration loop").
func (o *Orch) schedule(ctx context.Context, wsID uuid.UUID) error {
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if err := q.LockWorkspace(ctx, wsID); err != nil {
			return err
		}
		st, err := o.load(ctx, q, wsID)
		if err != nil {
			return err
		}
		if err := o.placeGoals(ctx, t, st); err != nil {
			return err
		}
		if err := o.addPlanTasks(ctx, t, st); err != nil {
			return err
		}
		if err := o.updateWaiting(ctx, t, st); err != nil {
			return err
		}
		if err := o.startWork(ctx, t, st); err != nil {
			return err
		}
		return o.finishGoals(ctx, t, st)
	})
}

// placeGoals gives each new goal a computer: its person's own, else a shared one.
func (o *Orch) placeGoals(ctx context.Context, t *core.Tx, st *wsState) error {
	for _, g := range st.goals {
		if g.RunnerID != nil || g.CreatedBy == nil || (g.Status != "planning" && g.Status != "active") {
			continue
		}
		runner := st.computerFor(*g.CreatedBy)
		if runner == nil {
			continue
		}
		ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, SetRunner: true, RunnerID: runner})
		if err != nil {
			return err
		}
		st.putGoal(ng)
		if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
			return err
		}
	}
	return nil
}

// addPlanTasks: goals waiting for a plan get a Plan task for their manager.
func (o *Orch) addPlanTasks(ctx context.Context, t *core.Tx, st *wsState) error {
	for _, g := range st.goals {
		if g.Status != "planning" || g.ManagerID == nil {
			continue
		}
		if slices.ContainsFunc(st.tasks, func(x db.Task) bool { return x.GoalID != nil && *x.GoalID == g.ID && x.Kind == "plan" }) {
			continue
		}
		pt, err := t.Q.CreateTask(ctx, db.CreateTaskParams{ID: newID(), WorkspaceID: st.ws.ID, GoalID: &g.ID, MemberID: g.ManagerID, Kind: "plan",
			Title: "Plan: " + g.Title, Status: "todo", DependsOn: []uuid.UUID{},
			Steps:        mustJSON([]core.Step{{Title: "Read the goal and the workspace notes"}, {Title: "Write the plan"}, {Title: "Submit the plan"}}),
			RequiredDirs: []byte("[]"), AttributedUserID: g.CreatedBy, CreatedBy: g.CreatedBy})
		if err != nil {
			return err
		}
		st.tasks = append(st.tasks, pt)
		st.task[pt.ID] = pt
		if err := emitTask(ctx, t, st.ws, pt); err != nil {
			return err
		}
	}
	return nil
}

// updateWaiting: tasks wait until their dependencies are done (and owner changes until approved).
func (o *Orch) updateWaiting(ctx context.Context, t *core.Tx, st *wsState) error {
	for _, task := range st.tasks {
		if task.Status != "todo" && task.Status != "waiting" {
			continue
		}
		want := "todo"
		for _, dep := range task.DependsOn {
			if d, ok := st.task[dep]; ok && d.Status != "done" {
				want = "waiting"
			}
		}
		if task.Kind == "owner_change" && st.openSignal(func(s db.Signal) bool { return s.Type == "owner_change" && s.TaskID != nil && *s.TaskID == task.ID }) != nil {
			want = "waiting"
		}
		if want == task.Status {
			continue
		}
		nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: &want})
		if err != nil {
			return err
		}
		st.putTask(nt)
		if err := emitTask(ctx, t, st.ws, nt); err != nil {
			return err
		}
	}
	return nil
}

// startWork starts the next task of every session that is free and whose computer is ready.
// A member works on one task at a time per goal, so it can work for several goals at once.
func (o *Orch) startWork(ctx context.Context, t *core.Tx, st *wsState) error {
	var todo []db.Task
	for _, x := range st.tasks {
		if x.Status == "todo" && x.MemberID != nil {
			todo = append(todo, x)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool {
		if (todo[i].Kind == "plan") != (todo[j].Kind == "plan") {
			return todo[i].Kind == "plan"
		}
		return todo[i].CreatedAt.Before(todo[j].CreatedAt)
	})
	started := map[sessKey]bool{}
	for _, task := range todo {
		m, ok := st.member[*task.MemberID]
		k := taskKey(task)
		if !ok || m.Sleeping || started[k] || st.busy(k) {
			continue
		}
		if s, ok := st.session[k]; ok && (s.Status == "gate" || s.Status == "error") {
			continue
		}
		if g, ok := st.goal[k.goal]; ok && g.Status == "done" {
			continue
		}
		runner := st.runnerFor(k, &task)
		if st.whyWaiting(runner, m.Cli, alwaysTrue) != "" {
			continue
		}
		started[k] = true
		if err := o.startTask(ctx, t, st, *runner, k, m, task); err != nil {
			return err
		}
	}
	return nil
}

func alwaysTrue() bool { return true }

// startTask checks directory access, marks the task Doing and hands it to its session.
func (o *Orch) startTask(ctx context.Context, t *core.Tx, st *wsState, runner uuid.UUID, k sessKey, m db.Member, task db.Task) error {
	q := t.Q
	// A missing grant becomes a directory-access request before the task starts.
	for _, need := range taskDirs(task) {
		dir, ok := st.dir[need.DirectoryID]
		if !ok || rank(st.grantMode(m.ID, dir.ID)) >= rank(need.Mode) {
			continue
		}
		if st.openSignal(func(s db.Signal) bool {
			return s.Type == "dir_access" && s.TaskID != nil && *s.TaskID == task.ID && strings.Contains(string(s.Payload), dir.ID.String())
		}) != nil {
			return nil
		}
		sig, err := q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: st.ws.ID, Type: "dir_access", MemberID: &m.ID, TaskID: &task.ID,
			GoalID:    task.GoalID,
			Payload:   mustJSON(map[string]any{"directory_id": dir.ID, "path": dir.Path, "mode": need.Mode, "reason": "Needed for “" + task.Title + "”"}),
			ExpiresAt: ptr(o.now().Add(o.GateTimeout))})
		if err != nil {
			return err
		}
		st.signals = append(st.signals, sig)
		return emitSignal(ctx, t, st.ws, sig, "signal.opened")
	}
	mounts, unavailable := o.mounts(ctx, st, runner, k, m, &task, nil)
	if unavailable != "" {
		return nil // the folder's owner hasn't synced it yet
	}
	if err := o.trackRepos(ctx, t, st, task, mounts); err != nil {
		return err
	}
	comments, err := q.ListComments(ctx, task.ID)
	if err != nil {
		return err
	}
	notes, err := q.ListNotes(ctx, st.ws.ID)
	if err != nil {
		return err
	}
	text := taskPrompt(st, m, task, comments, mounts)
	if task.Kind == "plan" {
		if g := goalOf(st, task); g != nil {
			text = planPrompt(st, m, task, *g, notes)
		}
	}
	if task.Kind == "triage" {
		if g := goalOf(st, task); g != nil {
			text = triagePrompt(st, m, task, *g, notes)
		}
	}
	if task.Kind == "handover" {
		if g := goalOf(st, task); g != nil {
			text = handoverPrompt(st, m, task, *g, notes)
		}
	}
	nt, err := q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: ptr("doing"), SetStarted: task.StartedAt == nil})
	if err != nil {
		return err
	}
	st.putTask(nt)
	if err := emitTask(ctx, t, st.ws, nt); err != nil {
		return err
	}
	if g := goalOf(st, nt); g != nil && nt.Kind != "plan" {
		if err := core.GoalEvent(ctx, t, st.ws, *g, "task_started", "Started “"+nt.Title+"”", map[string]any{"task_id": nt.ID, "member_id": m.ID}); err != nil {
			return err
		}
	}
	if err := o.chat(ctx, t, st.ws, m.ID, "event", "Started “"+nt.Title+"”", map[string]any{"event": "started", "task_id": nt.ID}); err != nil {
		return err
	}
	turn := protocol.Turn{TurnID: newID().String(), Kind: "task", Text: text, TaskID: &nt.ID, AttributedUserID: nt.AttributedUserID}
	return o.deliver(ctx, t, st, runner, k, m, &nt, mounts, turn, notes)
}

// trackRepos records which repositories a goal changes, so the board can show its branches.
func (o *Orch) trackRepos(ctx context.Context, t *core.Tx, st *wsState, task db.Task, mounts []protocol.Mount) error {
	if task.GoalID == nil || task.Kind == "owner_change" {
		return nil
	}
	for _, mt := range mounts {
		if mt.Repo == nil || mt.Repo.TaskBranch == "" {
			continue
		}
		known := slices.ContainsFunc(st.goalRepos[*task.GoalID], func(r db.GoalRepo) bool { return r.DirectoryID == mt.Repo.DirectoryID })
		if known {
			continue
		}
		r, err := t.Q.UpsertGoalRepo(ctx, db.UpsertGoalRepoParams{GoalID: *task.GoalID, DirectoryID: mt.Repo.DirectoryID, Base: mt.Repo.Base})
		if err != nil {
			return err
		}
		st.goalRepos[r.GoalID] = append(st.goalRepos[r.GoalID], r)
		if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.repo"), core.ToGoalRepo(r)); err != nil {
			return err
		}
	}
	return nil
}

var liveStatuses = []string{"starting", "running", "waiting", "review"}

// deliver hands a turn to a session, starting, waking or restarting it as needed.
func (o *Orch) deliver(ctx context.Context, t *core.Tx, st *wsState, runner uuid.UUID, k sessKey, m db.Member, task *db.Task, mounts []protocol.Mount, turn protocol.Turn, notes []db.Note) error {
	q := t.Q
	var taskID *uuid.UUID
	if task != nil {
		taskID = &task.ID
	}
	prev, hasPrev := st.session[k]
	var oldMounts []protocol.Mount
	if hasPrev {
		_ = json.Unmarshal(prev.Mounts, &oldMounts)
	}
	sameRunner := hasPrev && prev.RunnerID != nil && *prev.RunnerID == runner
	live := hasPrev && slices.Contains(liveStatuses, prev.Status)
	fresh := hasPrev && isolationNeeded(oldMounts, mounts)
	work := k.work()

	if live && sameRunner && !fresh {
		s, err := q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: prev.ID, Status: ptr("running"), SetTask: true, TaskID: taskID})
		if err != nil {
			return err
		}
		st.session[k] = s
		if err := emitSession(ctx, t, st.ws, s); err != nil {
			return err
		}
		if mountsKey(oldMounts) == mountsKey(mounts) {
			t.AfterCommit(func() {
				o.send(runner, protocol.TSendInput, protocol.SendInput{SessionID: s.ID, Turn: turn, Mounts: mounts})
			})
			return nil
		}
		if _, err := q.SetSessionMounts(ctx, db.SetSessionMountsParams{ID: s.ID, Mounts: mustJSON(mounts), Cwd: work, RunnerID: &runner}); err != nil {
			return err
		}
		t.AfterCommit(func() {
			o.send(runner, protocol.TApplyGrants, protocol.ApplyGrants{SessionID: s.ID, Mounts: mounts, Work: work, Turn: &turn})
		})
		return nil
	}
	if live {
		// Isolation, or the goal moved computers: the old conversation ends.
		if _, err := q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: prev.ID, Status: "ended"}); err != nil {
			return err
		}
		if prev.RunnerID != nil {
			old := *prev.RunnerID
			t.AfterCommit(func() { o.send(old, protocol.TStop, protocol.SessionRef{SessionID: prev.ID}) })
		}
	}

	resume := ""
	var s db.Session
	var err error
	if hasPrev && prev.Status == "asleep" && sameRunner && !fresh && prev.CliSessionID != "" {
		resume = prev.CliSessionID // wake: same session, same conversation
		s, err = q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: prev.ID, Status: ptr("starting"), SetTask: true, TaskID: taskID})
	} else {
		s, err = q.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: newID(), WorkspaceID: st.ws.ID, MemberID: m.ID, Status: "starting",
			RunnerID: &runner, TaskID: taskID, GoalID: k.goalID(), PersonID: k.personID()})
		if hasPrev && task != nil && turn.Kind != "chat" {
			turn.Text = handoff(st, k, notes) + turn.Text
		}
	}
	if err != nil {
		return err
	}
	if s, err = q.SetSessionMounts(ctx, db.SetSessionMountsParams{ID: s.ID, Mounts: mustJSON(mounts), Cwd: work, RunnerID: &runner}); err != nil {
		return err
	}
	st.session[k] = s
	if err := emitSession(ctx, t, st.ws, s); err != nil {
		return err
	}
	msg := protocol.StartSession{SessionID: s.ID, WorkspaceID: st.ws.ID, MemberID: m.ID, MemberName: m.Name, CLI: m.Cli, Model: m.Model,
		Resume: resume, AppendSystemPrompt: systemPrompt(st, m, mounts, notes), Work: work, Mounts: mounts, Turn: &turn,
		UseAPIKey: o.useKey(st, runner, m.Cli)}
	t.AfterCommit(func() { o.send(runner, protocol.TStartSession, msg) })
	return nil
}

// useKey: sign in with the computer's API key when its connection uses one, or while its
// owner chose "Use an API key until reset" during a subscription limit.
func (o *Orch) useKey(st *wsState, runner uuid.UUID, cli string) bool {
	if c, ok := st.conns[acctKey{runner, cli}]; ok && c.Mode == "api_key" {
		return true
	}
	a, ok := st.accounts[acctKey{runner, cli}]
	return ok && a.UseKeyUntil != nil && a.UseKeyUntil.After(o.now())
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func slug(s string) string { return domain.Slug(s) }
