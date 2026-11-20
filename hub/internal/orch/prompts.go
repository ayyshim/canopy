package orch

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// systemPrompt is appended to the CLI's own system prompt for every session of a member.
func systemPrompt(st *wsState, m db.Member, mounts []protocol.Mount, notes []db.Note) string {
	var b strings.Builder
	role := "a staff agent"
	if m.Kind == "manager" {
		role = "a manager agent"
	} else if m.ReportsTo == nil {
		role = "an independent staff agent (any manager can borrow you)"
	} else if mgr, ok := st.member[*m.ReportsTo]; ok {
		role = "a staff agent reporting to " + mgr.Name
	}
	skill := ""
	if m.Skill != nil {
		skill = " Your skill: " + *m.Skill + "."
	}
	fmt.Fprintf(&b, "You are %s, %s in the Canopy workspace %q.%s\n", m.Name, role, st.ws.Name, skill)
	if st.ws.About != "" {
		fmt.Fprintf(&b, "The workspace works on: %s\n", st.ws.About)
	}
	b.WriteString("\nPeople steer your work through Canopy. Use the `canopy` tools:\n")
	b.WriteString("- report_progress(step, progress, note): after each step of your task.\n")
	b.WriteString("- ask_question(question, options, suggested): when a person must decide. It waits for the answer.\n")
	b.WriteString("- request_directory(path, mode, reason): when you need a directory you can't see.\n")
	b.WriteString("- submit_output(path, title, kind, needs_review): for files people should see (screenshots, documents, diffs, reports).\n")
	b.WriteString("- get_attachments(name): download files people or agents attached to your goal into ./attachments.\n")
	b.WriteString("- attach_file(path, to): share a file with the goal (or your task) so others can use it.\n")
	b.WriteString("- complete_task(summary): when the task is done. Always call it; a task isn't finished until you do.\n")
	b.WriteString("- request_owner_change(directory, description): when a change is needed in a folder you can only read; its owner decides.\n")
	b.WriteString("- read_notes(query): the workspace's plans, decisions and notes. Read them before you start.\n")
	b.WriteString("- write_note(title, body, kind): record a decision or something the next person should know.\n")
	if m.Kind == "manager" {
		b.WriteString("- submit_plan(tasks): for plan tasks, your structured plan.\n")
		b.WriteString("- answer_question(signal_id, answer): when people ask you to decide a staff member's question.\n")
		b.WriteString("- assign_skill(member, skill, remove): give one of your staff a skill from the workspace skill library.\n")
		b.WriteString("- propose_staff(name, role, instructions, reason, skills): draft a new staff member for an admin to approve. Do this only when your team truly lacks someone for the work; check your current staff first. Skills: first search the web (WebSearch, WebFetch) for a fitting skill markdown file and pass its text with source_url; if none fits, write a short one yourself. You are told when an admin approves or rejects it; do not repeat a rejected draft.\n")
		b.WriteString("- read_board(include_done): the live board: all goals, tasks, who has them, progress, timings, the team and what waits on people. Use it for any question about progress, workload or how long things will take; never say you can't see tasks.\n")
		b.WriteString("- get_staff_instructions(member): read the current instructions of one of your staff.\n")
		b.WriteString("- update_staff_instructions(member, instructions, reason): draft new instructions for one of your staff for an admin to approve. Read the current ones first with get_staff_instructions, then send the full new text.\n")
	}
	b.WriteString("\nYou can only see these paths:\n")
	for _, mt := range mounts {
		access := "read only"
		if mt.Mode == "rw" {
			access = "read and write"
		}
		name := ""
		if mt.DirectoryID != nil {
			if d, ok := st.dir[*mt.DirectoryID]; ok {
				name = " (" + d.Name + ", " + d.Kind + ")"
			}
		}
		if mt.Repo != nil {
			where := "./" + mt.Path
			if mt.Repo.TaskBranch != "" {
				fmt.Fprintf(&b, "- %s%s: git repository on branch %s, %s. Commit your work there; Canopy merges it into %s when you call complete_task.\n",
					where, name, mt.Repo.TaskBranch, access, mt.Repo.GoalBranch)
			} else {
				fmt.Fprintf(&b, "- %s%s: git repository, read only\n", where, name)
			}
			continue
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", mt.Path, name, access)
	}
	if len(notes) > 0 {
		b.WriteString("\nWorkspace notes (read_notes shows them in full):\n")
		for i, n := range notes {
			if i == 8 {
				break
			}
			fmt.Fprintf(&b, "- %s (%s)\n", n.Title, n.Kind)
		}
	}
	if m.Instructions != "" {
		fmt.Fprintf(&b, "\nInstructions from your admins:\n%s\n", m.Instructions)
	}
	for _, sk := range st.memberSkills[m.ID] {
		fmt.Fprintf(&b, "\nSkill: %s\n%s\n", sk.Name, strings.TrimSpace(sk.Content))
	}
	return b.String()
}

// skillNames is " — skills: A, B" for a member with library skills attached, else "".
func skillNames(st *wsState, memberID uuid.UUID) string {
	list := st.memberSkills[memberID]
	if len(list) == 0 {
		return ""
	}
	names := make([]string, 0, len(list))
	for _, sk := range list {
		names = append(names, sk.Name)
	}
	return " — skills: " + strings.Join(names, ", ")
}

func goalOf(st *wsState, t db.Task) *db.Goal {
	if t.GoalID == nil {
		return nil
	}
	for i := range st.goals {
		if st.goals[i].ID == *t.GoalID {
			return &st.goals[i]
		}
	}
	return nil
}

// planPrompt asks a manager to plan a goal (SPEC "Orchestration loop" step 1).
func planPrompt(st *wsState, m db.Member, t db.Task, g db.Goal, notes []db.Note) string {
	var b strings.Builder
	fmt.Fprintf(&b, "New goal: %s\n", g.Title)
	if g.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", g.Description)
	}
	if g.DoneWhen != "" {
		fmt.Fprintf(&b, "Done when: %s\n", g.DoneWhen)
	}
	b.WriteString(attachmentsLine(taskAttachments(st, t)))
	b.WriteString("\nPlan this goal. Call read_notes first if earlier plans or decisions could help.\n")
	b.WriteString("Then call submit_plan with tasks. Each task: key (short id), title, skill (developer, reviewer, researcher, designer or writer), steps (3–6 short steps), depends_on (keys), directories it needs ({path, mode: read or read_write}).\n")
	b.WriteString("\nYour staff:\n")
	staff := 0
	for _, s := range st.members {
		if s.Kind != "staff" {
			continue
		}
		if s.ReportsTo != nil && *s.ReportsTo == m.ID {
			fmt.Fprintf(&b, "- %s (%s)%s\n", s.Name, deref(s.Skill), skillNames(st, s.ID))
			staff++
		}
	}
	if staff == 0 {
		b.WriteString("- none yet\n")
	}
	b.WriteString("Independent staff you can borrow:\n")
	ind := 0
	for _, s := range st.members {
		if s.Kind == "staff" && s.ReportsTo == nil {
			fmt.Fprintf(&b, "- %s (%s)%s\n", s.Name, deref(s.Skill), skillNames(st, s.ID))
			ind++
		}
	}
	if ind == 0 {
		b.WriteString("- none\n")
	}
	if len(st.skills) > 0 {
		b.WriteString("\nSkill library (give a skill to your staff with assign_skill):\n")
		for _, sk := range st.skills {
			fmt.Fprintf(&b, "- %s\n", sk.Name)
		}
	}
	b.WriteString("\nWorkspace directories:\n")
	for _, d := range st.dirs {
		yours := st.grantMode(m.ID, d.ID)
		if yours == "" {
			yours = "no access"
		}
		what := "folder"
		if d.RepoRemote != "" {
			what = "git repository; work happens on branch " + g.Branch
		}
		fmt.Fprintf(&b, "- %s (%s, %s, %s): you have %s\n", d.Path, d.Name, d.Kind, what, strings.ReplaceAll(yours, "_", " "))
	}
	_ = notes
	b.WriteString("\nAfter submit_plan succeeds, call complete_task with a one-line summary of the plan.\n")
	return b.String()
}

// taskPrompt hands a task to a member.
func taskPrompt(st *wsState, m db.Member, t db.Task, comments []db.Comment, mounts []protocol.Mount) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", t.Title)
	if g := goalOf(st, t); g != nil {
		fmt.Fprintf(&b, "Goal: %s\n", g.Title)
		if g.Description != "" {
			fmt.Fprintf(&b, "Goal description: %s\n", g.Description)
		}
		if g.DoneWhen != "" {
			fmt.Fprintf(&b, "Goal is done when: %s\n", g.DoneWhen)
		}
	}
	if t.Kind == "owner_change" {
		b.WriteString("\nAnother person asked for this change in a folder its owner controls. The owner approved it.\n")
	}
	if t.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", t.Description)
	}
	if steps := core.TaskSteps(t); len(steps) > 0 {
		b.WriteString("\nSteps:\n")
		for i, s := range steps {
			mark := " "
			if s.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, mark, s.Title)
		}
	}
	if dirs := taskDirs(t); len(dirs) > 0 {
		b.WriteString("\nDirectories for this task:\n")
		for _, d := range dirs {
			if dir, ok := st.dir[d.DirectoryID]; ok {
				where := dir.Path
				for _, mt := range mounts {
					if mt.Repo != nil && mt.Repo.DirectoryID == dir.ID {
						where = "./" + mt.Path + " (branch " + firstNonEmpty(mt.Repo.TaskBranch, mt.Repo.GoalBranch) + ")"
					}
				}
				fmt.Fprintf(&b, "- %s (%s)\n", where, strings.ReplaceAll(d.Mode, "_", " "))
			}
		}
	}
	for _, depID := range t.DependsOn {
		if dep, ok := st.task[depID]; ok && dep.Summary != "" {
			fmt.Fprintf(&b, "\nResult of “%s”: %s\n", dep.Title, dep.Summary)
		}
	}
	if len(comments) > 0 {
		b.WriteString("\nComments on this task:\n")
		for _, c := range comments {
			fmt.Fprintf(&b, "- %s\n", c.Text)
		}
	}
	b.WriteString(attachmentsLine(taskAttachments(st, t)))
	fmt.Fprintf(&b, "\nSave outputs people should see in ./%s and call submit_output for each. Call report_progress as you finish steps, and complete_task(summary) when done.\n",
		OutputsDir(t.ID))
	return b.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func taskDirs(t db.Task) []core.DirNeed { return core.TaskDirs(t) }

// handoff starts a new session on work that already began elsewhere: what's done so far, so
// the session needs nothing from the old computer (DECISIONS: Where work runs → Robust).
func handoff(st *wsState, k sessKey, notes []db.Note) string {
	g, ok := st.goal[k.goal]
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are picking up the goal “%s” in a new session. Done so far:\n", g.Title)
	any := false
	for _, t := range st.tasks {
		if t.GoalID != nil && *t.GoalID == g.ID && t.Status == "done" && t.Kind != "plan" {
			fmt.Fprintf(&b, "- %s: %s\n", t.Title, firstNonEmpty(t.Summary, "done"))
			any = true
		}
	}
	if !any {
		b.WriteString("- nothing finished yet\n")
	}
	for _, n := range notes {
		if n.GoalID != nil && *n.GoalID == g.ID && n.Kind == "plan" {
			fmt.Fprintf(&b, "The plan is in the note “%s” (read_notes).\n", n.Title)
			break
		}
	}
	return b.String() + "\n"
}
