package orch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

type planDir struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type planTask struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Skill       string    `json:"skill"`
	Description string    `json:"description"`
	Steps       []string  `json:"steps"`
	DependsOn   []string  `json:"depends_on"`
	Directories []planDir `json:"directories"`
}

// validatePlan checks a manager's plan: keys, skills, known directories and no cycles.
func validatePlan(tasks []planTask, dirs []db.Directory) []string {
	var problems []string
	if len(tasks) == 0 {
		return []string{"The plan has no tasks."}
	}
	if len(tasks) > 40 {
		problems = append(problems, "Keep the plan to 40 tasks or fewer.")
	}
	keys := map[string]int{}
	for i, t := range tasks {
		switch {
		case strings.TrimSpace(t.Key) == "":
			problems = append(problems, fmt.Sprintf("Task %d has no key.", i+1))
		case keys[t.Key] > 0:
			problems = append(problems, fmt.Sprintf("Key %q is used twice.", t.Key))
		}
		keys[t.Key] = i + 1
		if strings.TrimSpace(t.Title) == "" {
			problems = append(problems, fmt.Sprintf("Task %q has no title.", t.Key))
		}
		if !domain.ValidSkill(t.Skill) {
			problems = append(problems, fmt.Sprintf("Task %q: skill %q doesn’t exist (use a catalog key such as developer or marketing.seo).", t.Key, t.Skill))
		}
		for _, d := range t.Directories {
			if findDirectory(dirs, d.Path) == nil {
				problems = append(problems, fmt.Sprintf("Task %q: %q isn’t a workspace directory.", t.Key, d.Path))
			}
			if d.Mode != "read" && d.Mode != "read_write" {
				problems = append(problems, fmt.Sprintf("Task %q: mode for %q must be read or read_write.", t.Key, d.Path))
			}
		}
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if keys[dep] == 0 {
				problems = append(problems, fmt.Sprintf("Task %q depends on unknown key %q.", t.Key, dep))
			} else if dep == t.Key {
				problems = append(problems, fmt.Sprintf("Task %q depends on itself.", t.Key))
			}
		}
	}
	if len(problems) > 0 {
		return problems
	}
	// Cycles.
	state := map[string]int{} // 1 visiting, 2 done
	byKey := map[string]planTask{}
	for _, t := range tasks {
		byKey[t.Key] = t
	}
	var visit func(k string) bool
	visit = func(k string) bool {
		switch state[k] {
		case 1:
			return true
		case 2:
			return false
		}
		state[k] = 1
		for _, d := range byKey[k].DependsOn {
			if visit(d) {
				return true
			}
		}
		state[k] = 2
		return false
	}
	for _, t := range tasks {
		if visit(t.Key) {
			return []string{fmt.Sprintf("The dependencies form a cycle through %q.", t.Key)}
		}
	}
	return nil
}

// assign follows SPEC "Assignment": own staff with the skill → independent staff with the
// skill (borrowed) → any own staff → the manager itself. A skill matches by exact key or by
// base role; an exact key wins, then the least busy.
func assign(st *wsState, manager db.Member, skill string, load map[uuid.UUID]int) (uuid.UUID, bool) {
	exact := func(m db.Member) bool { return m.Skill != nil && *m.Skill == skill }
	pick := func(match func(db.Member) bool) *db.Member {
		var best *db.Member
		for i := range st.members {
			m := &st.members[i]
			if m.Kind != "staff" || m.Isolated || !match(*m) {
				continue
			}
			if best == nil || (exact(*m) && !exact(*best)) || (exact(*m) == exact(*best) && load[m.ID] < load[best.ID]) {
				best = m
			}
		}
		return best
	}
	own := func(m db.Member) bool { return m.ReportsTo != nil && *m.ReportsTo == manager.ID }
	hasSkill := func(m db.Member) bool { return m.Skill != nil && domain.SkillMatches(*m.Skill, skill) }
	if m := pick(func(m db.Member) bool { return own(m) && hasSkill(m) }); m != nil {
		return m.ID, false
	}
	if m := pick(func(m db.Member) bool { return m.ReportsTo == nil && hasSkill(m) }); m != nil {
		return m.ID, true
	}
	if m := pick(own); m != nil {
		return m.ID, false
	}
	return manager.ID, false
}

func (o *Orch) toolSubmitPlan(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Tasks []planTask `json:"tasks"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if tc.member.Kind != "manager" || tc.task == nil || tc.task.Kind != "plan" || tc.task.GoalID == nil {
		return toolError("Only a manager working on a plan task can submit a plan.")
	}
	var accepted int
	var planDoc string
	var g db.Goal
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if err := q.LockWorkspace(ctx, tc.ws.ID); err != nil {
			return err
		}
		st, err := o.load(ctx, q, tc.ws.ID)
		if err != nil {
			return err
		}
		if g, err = q.GetGoal(ctx, *tc.task.GoalID); err != nil {
			return err
		}
		if g.Status != "planning" {
			return toolError("This goal already has a plan.")
		}
		if problems := validatePlan(in.Tasks, st.dirs); len(problems) > 0 {
			return toolError("The plan needs changes:\n- " + strings.Join(problems, "\n- ") + "\nFix these and call submit_plan again.")
		}
		var doc strings.Builder
		fmt.Fprintf(&doc, "# Plan: %s\n\nBy %s.\n", g.Title, tc.member.Name)
		if g.DoneWhen != "" {
			fmt.Fprintf(&doc, "Done when: %s\n", g.DoneWhen)
		}
		doc.WriteString("\n")
		sections, err := o.createPlanTasks(ctx, t, tc, st, g, in.Tasks, nil)
		if err != nil {
			return err
		}
		accepted = len(in.Tasks)
		doc.WriteString(sections)
		planDoc = doc.String()
		ng, err := q.UpdateGoal(ctx, db.UpdateGoalParams{ID: g.ID, Status: ptr("active"), SetPlanned: true})
		if err != nil {
			return err
		}
		if err := t.Emit(ctx, core.WSMember(tc.ws.OrgID, tc.ws.ID, tc.member.ID, "goal.updated"), core.ToGoal(ng)); err != nil {
			return err
		}
		if err := core.GoalEvent(ctx, t, tc.ws, ng, "plan_ready", fmt.Sprintf("Plan ready: %d tasks", accepted), nil); err != nil {
			return err
		}
		if err := o.chat(ctx, t, tc.ws, tc.member.ID, "event", "Plan ready", map[string]any{"event": "plan_ready", "goal_id": g.ID}); err != nil {
			return err
		}
		// The plan is an output of the plan task, and a workspace note.
		if err := o.storeTextOutput(ctx, t, tc.ws, *tc.task, tc.member, &tc.sess, "plan.md", "Plan: "+g.Title, "document", planDoc); err != nil {
			return err
		}
		if _, err := o.saveNote(ctx, t, tc.ws, &g.ID, "plan", "Plan: "+g.Title, planDoc, nil, &tc.member.ID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	o.reply(tc, fmt.Sprintf("Plan accepted: %d tasks. It’s saved as the note “Plan: %s”. Now call complete_task.", accepted, g.Title), false)
	o.Kick(ctx, tc.ws.ID)
	return nil
}

// createPlanTasks adds planned tasks to a goal for the manager, picking staff by skill and load.
// parent, when set, is the task they came from. It returns the plan text that describes them.
func (o *Orch) createPlanTasks(ctx context.Context, t *core.Tx, tc *toolCtx, st *wsState, g db.Goal, tasks []planTask, parent *uuid.UUID) (string, error) {
	q := t.Q
	load := map[uuid.UUID]int{}
	for _, x := range st.tasks {
		if x.MemberID != nil && x.Status != "done" {
			load[*x.MemberID]++
		}
	}
	ids := map[string]uuid.UUID{}
	for _, pt := range tasks {
		ids[pt.Key] = newID()
	}
	var doc strings.Builder
	for _, pt := range tasks {
		memberID, borrowed := assign(st, tc.member, pt.Skill, load)
		load[memberID]++
		deps := make([]uuid.UUID, 0, len(pt.DependsOn))
		for _, d := range pt.DependsOn {
			deps = append(deps, ids[d])
		}
		steps := make([]core.Step, 0, len(pt.Steps))
		for _, s := range pt.Steps {
			if strings.TrimSpace(s) != "" {
				steps = append(steps, core.Step{Title: s})
			}
		}
		var needs []core.DirNeed
		crossOwner := (*db.Directory)(nil)
		for _, d := range pt.Directories {
			dir := findDirectory(st.dirs, d.Path)
			needs = append(needs, core.DirNeed{DirectoryID: dir.ID, Mode: d.Mode})
			// Writing a directory the goal giver can't write needs its owner's approval.
			// Repositories don't need it: work stays on the goal's branch until its person ships it.
			if d.Mode == "read_write" && dir.RepoRemote == "" && g.CreatedBy != nil && authz.PersonDirMode(*dir, *g.CreatedBy, st.dirAccess[dir.ID]) != "read_write" {
				crossOwner = dir
			}
		}
		skill := pt.Skill
		params := db.CreateTaskParams{ID: ids[pt.Key], WorkspaceID: tc.ws.ID, GoalID: &g.ID, MemberID: &memberID, Kind: "work", Title: pt.Title,
			Description: pt.Description, Skill: &skill, Status: "waiting", DependsOn: deps, Steps: mustJSON(nonNilSteps(steps)),
			RequiredDirs: mustJSON(nonNilNeeds(needs)), Borrowed: borrowed, AttributedUserID: g.CreatedBy, CreatedBy: g.CreatedBy,
			CreatedByMember: &tc.member.ID, ParentTaskID: parent, PlanKey: pt.Key}
		if crossOwner != nil {
			params.Kind, params.AttributedUserID = "owner_change", &crossOwner.OwnerUserID
			if w := pickWriter(st, crossOwner.ID, nil); w != nil {
				params.MemberID = w
			}
		}
		task, err := q.CreateTask(ctx, params)
		if err != nil {
			return "", err
		}
		st.tasks = append(st.tasks, task)
		st.task[task.ID] = task
		if err := emitTask(ctx, t, tc.ws, task); err != nil {
			return "", err
		}
		if crossOwner != nil {
			sig, err := q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: tc.ws.ID, Type: "owner_change", MemberID: task.MemberID, TaskID: &task.ID,
				Payload: mustJSON(map[string]any{"directory_id": crossOwner.ID, "path": crossOwner.Path, "owner_user_id": crossOwner.OwnerUserID,
					"requested_by_user_id": g.CreatedBy, "requested_by_member_id": tc.member.ID, "title": task.Title, "description": task.Description})})
			if err != nil {
				return "", err
			}
			e := core.WS(tc.ws.OrgID, tc.ws.ID, "signal.opened")
			e.MemberID, e.ForUserID = sig.MemberID, &crossOwner.OwnerUserID
			if err := t.Emit(ctx, e, core.ToSignal(sig)); err != nil {
				return "", err
			}
		}
		who := st.member[*task.MemberID].Name
		if borrowed {
			who += " (borrowed)"
		}
		fmt.Fprintf(&doc, "## %s\n\n- Assigned to: %s\n- Skill: %s\n", pt.Title, who, pt.Skill)
		if len(pt.DependsOn) > 0 {
			fmt.Fprintf(&doc, "- After: %s\n", strings.Join(pt.DependsOn, ", "))
		}
		if crossOwner != nil {
			fmt.Fprintf(&doc, "- Needs approval from the owner of %s\n", crossOwner.Path)
		}
		for i, s := range steps {
			fmt.Fprintf(&doc, "%d. %s\n", i+1, s.Title)
		}
		doc.WriteString("\n")
	}
	return doc.String(), nil
}

// toolAssignTasks lets a manager hand a triage task's work to staff as new tasks on the goal.
func (o *Orch) toolAssignTasks(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Tasks []planTask `json:"tasks"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if tc.member.Kind != "manager" || tc.task == nil || tc.task.Kind != "triage" || tc.task.GoalID == nil {
		return toolError("Only a manager working on a triage task can assign tasks.")
	}
	var g db.Goal
	var sections string
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if err := q.LockWorkspace(ctx, tc.ws.ID); err != nil {
			return err
		}
		st, err := o.load(ctx, q, tc.ws.ID)
		if err != nil {
			return err
		}
		if g, err = q.GetGoal(ctx, *tc.task.GoalID); err != nil {
			return err
		}
		if g.Status == "planning" || g.Status == "draft" {
			return toolError("This goal has no plan yet.")
		}
		if problems := validatePlan(in.Tasks, st.dirs); len(problems) > 0 {
			return toolError("The tasks need changes:\n- " + strings.Join(problems, "\n- ") + "\nFix these and call assign_tasks again.")
		}
		if sections, err = o.createPlanTasks(ctx, t, tc, st, g, in.Tasks, &tc.task.ID); err != nil {
			return err
		}
		if err := core.GoalEvent(ctx, t, tc.ws, g, "tasks_assigned", fmt.Sprintf("Assigned %d tasks from “%s”", len(in.Tasks), tc.task.Title),
			map[string]any{"task_id": tc.task.ID}); err != nil {
			return err
		}
		title := "Plan: " + g.Title
		body := "# " + title + "\n\n"
		if n, err := q.GoalNote(ctx, db.GoalNoteParams{GoalID: &g.ID, Kind: "plan"}); err == nil {
			body = strings.TrimRight(n.Body, "\n") + "\n\n"
		} else if !store.IsNotFound(err) {
			return err
		}
		body += fmt.Sprintf("# Added from “%s”\n\nBy %s.\n\n%s", tc.task.Title, tc.member.Name, sections)
		_, err = o.saveNote(ctx, t, tc.ws, &g.ID, "plan", title, body, nil, &tc.member.ID)
		return err
	})
	if err != nil {
		return err
	}
	o.reply(tc, fmt.Sprintf("Assigned %d tasks. Now call complete_task with a one-line summary.", len(in.Tasks)), false)
	o.Kick(ctx, tc.ws.ID)
	return nil
}

func nonNilSteps(s []core.Step) []core.Step {
	if s == nil {
		return []core.Step{}
	}
	return s
}

func nonNilNeeds(s []core.DirNeed) []core.DirNeed {
	if s == nil {
		return []core.DirNeed{}
	}
	return s
}

// storeTextOutput saves text the hub produced (a plan) as an output version.
func (o *Orch) storeTextOutput(ctx context.Context, t *core.Tx, ws db.Workspace, task db.Task, m db.Member, sess *db.Session, name, title, kind, text string) error {
	sum := sha256.Sum256([]byte(text))
	sha := hex.EncodeToString(sum[:])
	key := blob.ContentKey(sha)
	if o.Blobs != nil {
		if err := o.Blobs.Put(ctx, key, bytes.NewReader([]byte(text)), int64(len(text)), "text/markdown"); err != nil {
			return err
		}
	}
	return o.addOutputVersion(ctx, t, ws, task, m, sess, outputFile{Name: name, Title: title, Kind: kind, BlobKey: key, Mime: "text/markdown", Size: int64(len(text))}, false)
}
