package orch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// A goal is planning → active → ready → done. "Ready" means every task is done and the goal's
// person decides what happens to the code: open a pull request, merge it, keep the branch, or
// just mark the goal done when it changed no code.

var (
	ErrGoalNotReady = errors.New("This goal still has work going on.")
	ErrComputerOff  = errors.New("computer off")
	ErrNotAllowed   = errors.New("You can’t run work on that computer.")
)

// finishGoals moves goals whose tasks are all done to ready (or straight to done when the
// workspace finishes goals automatically).
func (o *Orch) finishGoals(ctx context.Context, t *core.Tx, st *wsState) error {
	for _, g := range st.goals {
		if g.Status != "active" {
			continue
		}
		var tasks []db.Task
		allDone := true
		for _, x := range st.tasks {
			if x.GoalID != nil && *x.GoalID == g.ID {
				tasks = append(tasks, x)
				allDone = allDone && x.Status == "done"
			}
		}
		if len(tasks) == 0 || !allDone {
			continue
		}
		if err := o.goalTasksDone(ctx, t, st, g, tasks); err != nil {
			return err
		}
	}
	return nil
}

func (o *Orch) goalTasksDone(ctx context.Context, t *core.Tx, st *wsState, g db.Goal, tasks []db.Task) error {
	summary := goalSummary(st, g, tasks, o.now())
	if _, err := o.saveNote(ctx, t, st.ws, &g.ID, "summary", "Summary: "+g.Title, summary, nil, g.ManagerID); err != nil {
		return err
	}
	hasCode := goalHasCode(st.goalRepos[g.ID])
	if st.ws.GoalCompletion == "auto" {
		shipped := map[string]any{}
		if hasCode {
			shipped["kind"] = "keep"
		}
		return o.closeGoal(ctx, t, st.ws, g, nil, shipped, summary)
	}
	ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, Status: ptr("ready"), Summary: &summary, SetReady: true})
	if err != nil {
		return err
	}
	st.putGoal(ng)
	if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
		return err
	}
	next := "mark it done"
	if hasCode {
		next = "ship it"
	}
	if err := core.GoalEvent(ctx, t, st.ws, ng, "ready", "All tasks done. Waiting for its person to "+next, nil); err != nil {
		return err
	}
	sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: st.ws.ID, Type: "goal_ready", MemberID: g.ManagerID,
		GoalID: &g.ID, ForUserID: g.CreatedBy,
		Payload: mustJSON(map[string]any{"title": g.Title, "has_code": hasCode, "branch": g.Branch, "base": g.Base})})
	if err != nil {
		return err
	}
	if err := emitSignal(ctx, t, st.ws, sig, "signal.opened"); err != nil {
		return err
	}
	if g.ManagerID != nil {
		return o.chat(ctx, t, st.ws, *g.ManagerID, "agent", "Every task for “"+g.Title+"” is done.\n\n"+summary, map[string]any{"goal_id": g.ID})
	}
	return nil
}

func goalHasCode(repos []db.GoalRepo) bool {
	for _, r := range repos {
		if r.TipSha != "" && r.TipSha != r.BaseSha {
			return true
		}
	}
	return false
}

func goalSummary(st *wsState, g db.Goal, tasks []db.Task, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nFinished %s.\n\n", g.Title, now.Format("Jan 2, 2006"))
	if g.DoneWhen != "" {
		fmt.Fprintf(&b, "Done when: %s\n\n", g.DoneWhen)
	}
	for _, x := range tasks {
		if x.Kind == "plan" {
			continue
		}
		who := ""
		if x.MemberID != nil {
			who = " (" + st.member[*x.MemberID].Name + ")"
		}
		fmt.Fprintf(&b, "- **%s**%s: %s\n", x.Title, who, firstNonEmpty(x.Summary, "done"))
	}
	return b.String()
}

// closeGoal marks a goal done, closes its "ready" card and frees its worktrees.
func (o *Orch) closeGoal(ctx context.Context, t *core.Tx, ws db.Workspace, g db.Goal, by *uuid.UUID, shipped map[string]any, summary string) error {
	params := db.UpdateGoalParams{ID: g.ID, Status: ptr("done"), SetFinished: true, SetDoneBy: true, DoneBy: by, Shipped: mustJSON(shipped), ShowTasks: ptr(false)}
	if summary != "" {
		params.Summary = &summary
	}
	ng, err := t.Q.UpdateGoal(ctx, params)
	if err != nil {
		return err
	}
	if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
		return err
	}
	if err := o.closeReadyCard(ctx, t, ws, g.ID, "done"); err != nil {
		return err
	}
	if err := core.GoalEvent(ctx, t, ws, ng, "complete", "Goal done. "+shippedText(shipped), shipped); err != nil {
		return err
	}
	if g.ManagerID != nil {
		if err := o.chat(ctx, t, ws, *g.ManagerID, "event", "Goal done: "+g.Title, map[string]any{"event": "goal_complete", "goal_id": g.ID}); err != nil {
			return err
		}
	}
	if g.RunnerID != nil {
		runner, id := *g.RunnerID, g.ID
		t.AfterCommit(func() { o.send(runner, protocol.TGoalCleanup, protocol.GoalCleanup{GoalID: id}) })
	}
	return nil
}

func shippedText(s map[string]any) string {
	switch s["kind"] {
	case "pr":
		return "Pull request opened."
	case "merge":
		return "Merged into " + fmt.Sprint(s["base"]) + "."
	case "keep":
		return "Branch kept."
	}
	return ""
}

func (o *Orch) closeReadyCard(ctx context.Context, t *core.Tx, ws db.Workspace, goalID uuid.UUID, action string) error {
	sig, err := t.Q.OpenGoalSignal(ctx, db.OpenGoalSignalParams{GoalID: &goalID, Type: "goal_ready"})
	if store.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	ns, err := t.Q.ResolveSignal(ctx, db.ResolveSignalParams{ID: sig.ID, Status: "resolved", Resolution: mustJSON(map[string]any{"action": action})})
	if err != nil {
		return err
	}
	return emitSignal(ctx, t, ws, ns, "signal.resolved")
}

// ReopenGoal: a new task for a ready or done goal puts it back to work.
func (o *Orch) ReopenGoal(ctx context.Context, t *core.Tx, ws db.Workspace, g db.Goal) error {
	if g.Status != "ready" && g.Status != "done" {
		return nil
	}
	ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, Status: ptr("active"), ShowTasks: ptr(true)})
	if err != nil {
		return err
	}
	if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
		return err
	}
	if err := o.closeReadyCard(ctx, t, ws, g.ID, "reopened"); err != nil {
		return err
	}
	return core.GoalEvent(ctx, t, ws, ng, "reopened", "Reopened with a new task", nil)
}

// MarkGoalDone closes a ready goal without shipping code (its branch, if any, is kept).
func (o *Orch) MarkGoalDone(ctx context.Context, goalID, by uuid.UUID) error {
	return o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		if g.Status != "ready" {
			return ErrGoalNotReady
		}
		shipped := map[string]any{}
		if goalHasCode(st.goalRepos[g.ID]) {
			shipped["kind"] = "keep"
		}
		return o.closeGoal(ctx, t, st.ws, g, &by, shipped, "")
	})
}

// ShowGoalTasks shows or hides a done goal's tasks on the board.
func (o *Orch) ShowGoalTasks(ctx context.Context, goalID uuid.UUID, show bool) error {
	return o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, ShowTasks: &show})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.updated"), core.ToGoal(ng))
	})
}

// MoveGoal runs a goal on another computer. Its sessions there start from a handoff, and its
// code comes from the hub's backup of the goal branch (DECISIONS: When a computer is off).
func (o *Orch) MoveGoal(ctx context.Context, goalID, runnerID, by uuid.UUID, admin bool) error {
	err := o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		r, ok := st.runners[runnerID]
		if !ok {
			return ErrNotAllowed
		}
		person := deref2(g.CreatedBy)
		if !r.Shared && r.OwnerUserID != person && !(admin && r.OwnerUserID == by) {
			return ErrNotAllowed
		}
		if g.RunnerID != nil && *g.RunnerID == runnerID {
			return nil
		}
		ng, err := t.Q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, SetRunner: true, RunnerID: &runnerID})
		if err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
			return err
		}
		live, err := t.Q.ListGoalLiveSessions(ctx, &g.ID)
		if err != nil {
			return err
		}
		for _, s := range live {
			ns, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "ended"})
			if err != nil {
				return err
			}
			if err := emitSession(ctx, t, st.ws, ns); err != nil {
				return err
			}
			if s.RunnerID != nil {
				old, id := *s.RunnerID, s.ID
				t.AfterCommit(func() { o.send(old, protocol.TStop, protocol.SessionRef{SessionID: id}) })
			}
		}
		for _, x := range st.tasks {
			if x.GoalID == nil || *x.GoalID != g.ID || x.Kind == "owner_change" || (x.Status != "doing" && x.Status != "review") {
				continue
			}
			nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: x.ID, Status: ptr("todo")})
			if err != nil {
				return err
			}
			if err := emitTask(ctx, t, st.ws, nt); err != nil {
				return err
			}
		}
		text := "Moved to " + r.Name
		if len(st.goalRepos[g.ID]) > 0 {
			text += "; its branch comes from the hub’s backup"
		}
		return core.GoalEvent(ctx, t, st.ws, ng, "moved", text, map[string]any{"runner_id": runnerID, "by": by})
	})
	if err == nil {
		o.Kick(ctx, uuid.Nil)
	}
	return err
}

// ShipResult tells the person what happened when they shipped a goal.
type ShipResult struct {
	Status  string   `json:"status"` // done, conflicts, dirty
	Message string   `json:"message"`
	PRURLs  []string `json:"pr_urls,omitempty"`
}

// ShipGoal finishes a ready goal's branches on its computer: a pull request, a merge into the
// base, or keep the branch. Conflicts with the base become a task for the goal's manager.
func (o *Orch) ShipGoal(ctx context.Context, goalID uuid.UUID, action string, by uuid.UUID) (ShipResult, error) {
	var msg protocol.GitShip
	var runner uuid.UUID
	var runnerName string
	err := o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		if g.Status != "ready" {
			return ErrGoalNotReady
		}
		if action == "keep" || !goalHasCode(st.goalRepos[g.ID]) {
			return nil
		}
		if g.RunnerID == nil {
			return ErrComputerOff
		}
		runner, runnerName = *g.RunnerID, st.runners[*g.RunnerID].Name
		if !st.online(runner) {
			return ErrComputerOff
		}
		msg = protocol.GitShip{GoalID: g.ID, Action: action, Title: g.Title, Body: g.Summary}
		for _, r := range st.goalRepos[g.ID] {
			dir, ok := st.dir[r.DirectoryID]
			if !ok || r.TipSha == "" {
				continue
			}
			mt := st.repoMount(runner, goalKey(uuid.Nil, g.ID), dir, "rw", nil, map[string]bool{})
			msg.Repos = append(msg.Repos, *mt.Repo)
		}
		return nil
	})
	if errors.Is(err, ErrComputerOff) {
		if runnerName == "" {
			runnerName = "its computer"
		}
		return ShipResult{}, fmt.Errorf("%s is off. Ship when it’s back, or move the goal to another computer.", runnerName)
	}
	if err != nil {
		return ShipResult{}, err
	}
	if len(msg.Repos) == 0 {
		return ShipResult{Status: "done", Message: "Done."}, o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
			shipped := map[string]any{}
			if goalHasCode(st.goalRepos[g.ID]) {
				shipped["kind"] = "keep"
			}
			return o.closeGoal(ctx, t, st.ws, g, &by, shipped, "")
		})
	}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var res protocol.GitShipResult
	if err := o.R.Request(rctx, runner, protocol.TGitShip, msg, &res); err != nil {
		return ShipResult{}, fmt.Errorf("%s couldn’t finish the branch: %v", runnerName, err)
	}
	return o.applyShip(ctx, goalID, action, by, res)
}

func (o *Orch) applyShip(ctx context.Context, goalID uuid.UUID, action string, by uuid.UUID, res protocol.GitShipResult) (ShipResult, error) {
	var out ShipResult
	err := o.withGoal(ctx, goalID, func(t *core.Tx, st *wsState, g db.Goal) error {
		var conflicts, problems []string
		dirty := false
		for _, r := range res.Repos {
			name := st.dir[r.DirectoryID].Name
			switch {
			case r.Dirty:
				dirty = true
			case len(r.Conflicts) > 0:
				conflicts = append(conflicts, name+": "+strings.Join(r.Conflicts, ", "))
			case r.Error != "":
				problems = append(problems, name+": "+r.Error)
			}
		}
		switch {
		case dirty:
			out = ShipResult{Status: "dirty", Message: "You have uncommitted changes on " + g.Base + ". Commit or stash them, then try again."}
			return nil
		case len(conflicts) > 0:
			out = ShipResult{Status: "conflicts", Message: "The branch conflicts with " + firstNonEmpty(g.Base, "its base") + ". " +
				"The manager is bringing it up to date; it will be ready again soon."}
			return o.addConflictTask(ctx, t, st, g, conflicts)
		case len(problems) > 0:
			return errors.New(strings.Join(problems, "; "))
		}
		shipped := map[string]any{"kind": action, "base": g.Base}
		var prs []string
		for _, r := range res.Repos {
			status := map[string]string{"pr": "pushed", "merge": "merged"}[action]
			params := db.UpdateGoalRepoParams{GoalID: g.ID, DirectoryID: r.DirectoryID, Status: &status}
			if r.PRURL != "" {
				params.PrUrl = &r.PRURL
				prs = append(prs, r.PRURL)
			}
			nr, err := t.Q.UpdateGoalRepo(ctx, params)
			if err != nil {
				return err
			}
			if err := t.Emit(ctx, core.WS(st.ws.OrgID, st.ws.ID, "goal.repo"), core.ToGoalRepo(nr)); err != nil {
				return err
			}
		}
		if len(prs) > 0 {
			shipped["prs"] = prs
		}
		out = ShipResult{Status: "done", Message: shippedText(shipped), PRURLs: prs}
		return o.closeGoal(ctx, t, st.ws, g, &by, shipped, "")
	})
	return out, err
}

// addConflictTask asks the goal's manager to bring the goal branch up to date with its base.
func (o *Orch) addConflictTask(ctx context.Context, t *core.Tx, st *wsState, g db.Goal, conflicts []string) error {
	base := firstNonEmpty(g.Base, "the default branch")
	task, err := t.Q.CreateTask(ctx, db.CreateTaskParams{ID: newID(), WorkspaceID: st.ws.ID, GoalID: &g.ID, MemberID: g.ManagerID, Kind: "work",
		Title:       "Bring " + g.Branch + " up to date with " + base,
		Description: "Merging " + base + " into the goal branch conflicts:\n" + strings.Join(conflicts, "\n") + "\n\nIn each repository run `git merge " + baseRef(g.Base) + "`, resolve the conflicts, commit, and check the code still works.",
		Status:      "todo", DependsOn: []uuid.UUID{}, Steps: mustJSON([]core.Step{{Title: "Merge the base branch"}, {Title: "Resolve conflicts"}, {Title: "Check it still works"}}),
		RequiredDirs: []byte("[]"), AttributedUserID: g.CreatedBy, CreatedBy: g.CreatedBy})
	if err != nil {
		return err
	}
	if err := emitTask(ctx, t, st.ws, task); err != nil {
		return err
	}
	return o.ReopenGoal(ctx, t, st.ws, g)
}

// baseRef is how a task refers to the base in its worktree.
func baseRef(base string) string {
	if base == "" {
		return "origin/HEAD"
	}
	return base
}

// recordRepoResults stores a goal branch after a task's work merged into it, and the task's
// changes as a diff output.
func (o *Orch) recordRepoResults(ctx context.Context, t *core.Tx, ws db.Workspace, task db.Task, m db.Member, sess *db.Session, results []protocol.RepoResult) error {
	for _, r := range results {
		dir, err := t.Q.GetDirectory(ctx, r.DirectoryID)
		if err != nil || dir.WorkspaceID != ws.ID {
			continue
		}
		if task.GoalID != nil {
			commits := int32(r.Commits)
			nr, err := t.Q.UpdateGoalRepo(ctx, db.UpdateGoalRepoParams{GoalID: *task.GoalID, DirectoryID: r.DirectoryID,
				BaseSha: &r.BaseSHA, TipSha: &r.TipSHA, Commits: &commits, BundleKey: &r.BundleKey})
			if store.IsNotFound(err) {
				if _, err = t.Q.UpsertGoalRepo(ctx, db.UpsertGoalRepoParams{GoalID: *task.GoalID, DirectoryID: r.DirectoryID, Base: r.Base}); err != nil {
					return err
				}
				nr, err = t.Q.UpdateGoalRepo(ctx, db.UpdateGoalRepoParams{GoalID: *task.GoalID, DirectoryID: r.DirectoryID,
					BaseSha: &r.BaseSHA, TipSha: &r.TipSHA, Commits: &commits, BundleKey: &r.BundleKey})
			}
			if err != nil {
				return err
			}
			if err := t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "goal.repo"), core.ToGoalRepo(nr)); err != nil {
				return err
			}
		}
		if r.DiffKey != "" && r.Files > 0 {
			name := slug(dir.Name) + ".diff"
			title := fmt.Sprintf("Changes to %s (%d file", dir.Name, r.Files)
			if r.Files != 1 {
				title += "s"
			}
			if err := o.addOutputVersion(ctx, t, ws, task, m, sess, outputFile{Name: name, Title: title + ")", Kind: "diff",
				BlobKey: r.DiffKey, Mime: "text/x-diff", Size: r.DiffSize}, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// withGoal runs fn in a transaction with the goal's workspace locked and loaded.
func (o *Orch) withGoal(ctx context.Context, goalID uuid.UUID, fn func(t *core.Tx, st *wsState, g db.Goal) error) error {
	g, err := o.Store.GetGoal(ctx, goalID)
	if err != nil {
		return err
	}
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.LockWorkspace(ctx, g.WorkspaceID); err != nil {
			return err
		}
		st, err := o.load(ctx, t.Q, g.WorkspaceID)
		if err != nil {
			return err
		}
		return fn(t, st, st.goal[goalID])
	})
	if err == nil {
		o.Kick(ctx, g.WorkspaceID)
	}
	return err
}
