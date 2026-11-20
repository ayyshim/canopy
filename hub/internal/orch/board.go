package orch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// toolReadBoard gives a manager the live state of the workspace: goals, tasks, staff and what waits on people.
func (o *Orch) toolReadBoard(ctx context.Context, tc *toolCtx) error {
	if err := requireManager(tc); err != nil {
		return err
	}
	var in struct {
		Done bool `json:"include_done"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	st, err := o.load(ctx, o.Store.Queries, tc.ws.ID)
	if err != nil {
		return err
	}
	o.reply(tc, boardText(st, time.Now(), in.Done), false)
	return nil
}

// boardText writes the board as plain text, with times so a manager can estimate what is left.
func boardText(st *wsState, now time.Time, withDone bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace %s, now %s.\n", st.ws.Name, now.Format("Jan 2 15:04"))
	name := func(id *uuid.UUID) string {
		if id == nil {
			return "nobody"
		}
		if m, ok := st.member[*id]; ok {
			return m.Name
		}
		return "someone removed"
	}

	b.WriteString("\nTeam:\n")
	for _, m := range st.members {
		if m.Isolated {
			continue
		}
		role := "manager"
		if m.Kind == "staff" {
			role = deref(m.Skill)
			if m.ReportsTo != nil {
				role += ", reports to " + name(m.ReportsTo)
			} else {
				role += ", independent"
			}
		}
		doing := "idle"
		for _, t := range st.tasks {
			if t.MemberID != nil && *t.MemberID == m.ID && (t.Status == "doing" || t.Status == "review") {
				doing = "on “" + t.Title + "”"
				break
			}
		}
		if m.Sleeping {
			doing = "asleep"
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", m.Name, role, doing)
	}

	goals := append([]db.Goal{}, st.goals...)
	sort.Slice(goals, func(i, j int) bool { return goals[i].CreatedAt.Before(goals[j].CreatedAt) })
	var took []time.Duration
	hidden := 0
	b.WriteString("\nGoals:\n")
	for _, g := range goals {
		tasks := tasksOf(st, &g.ID)
		for _, t := range tasks {
			if t.Status == "done" && t.StartedAt != nil && t.FinishedAt != nil {
				took = append(took, t.FinishedAt.Sub(*t.StartedAt))
			}
		}
		if g.Status == "done" && !withDone {
			hidden++
			continue
		}
		done := 0
		for _, t := range tasks {
			if t.Status == "done" {
				done++
			}
		}
		fmt.Fprintf(&b, "\n## %s [%s], manager %s, %d of %d tasks done, started %s\n", g.Title, g.Status, name(g.ManagerID), done, len(tasks), ago(now, g.CreatedAt))
		if g.DoneWhen != "" {
			fmt.Fprintf(&b, "Done when: %s\n", g.DoneWhen)
		}
		writeTasks(&b, st, tasks, now, name)
	}
	if len(goals) == 0 {
		b.WriteString("- none yet\n")
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "\n(%d finished goals left out; call with include_done to see them.)\n", hidden)
	}
	if loose := tasksOf(st, nil); len(loose) > 0 {
		b.WriteString("\nTasks outside any goal:\n")
		writeTasks(&b, st, loose, now, name)
	}

	b.WriteString("\nWaiting on people:\n")
	for _, s := range st.signals {
		what := strings.ReplaceAll(s.Type, "_", " ")
		if s.TaskID != nil {
			what += " on “" + st.task[*s.TaskID].Title + "”"
		}
		fmt.Fprintf(&b, "- %s from %s, open %s\n", what, name(s.MemberID), ago(now, s.CreatedAt))
	}
	if len(st.signals) == 0 {
		b.WriteString("- nothing\n")
	}
	if len(took) > 0 {
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		fmt.Fprintf(&b, "\nPace: %d finished tasks took %s on average (median %s).\n", len(took), dur(avg(took)), dur(took[len(took)/2]))
	}
	return b.String()
}

func tasksOf(st *wsState, goal *uuid.UUID) []db.Task {
	var out []db.Task
	for _, t := range st.tasks {
		if (goal == nil && t.GoalID == nil) || (goal != nil && t.GoalID != nil && *t.GoalID == *goal) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func writeTasks(b *strings.Builder, st *wsState, tasks []db.Task, now time.Time, name func(*uuid.UUID) string) {
	for _, t := range tasks {
		line := fmt.Sprintf("- [%s] %s, %s", t.Status, t.Title, name(t.MemberID))
		switch {
		case t.Status == "done" && t.StartedAt != nil && t.FinishedAt != nil:
			line += ", took " + dur(t.FinishedAt.Sub(*t.StartedAt))
		case t.StartedAt != nil:
			line += fmt.Sprintf(", %d%% done, started %s", t.Progress, ago(now, *t.StartedAt))
		}
		var waits []string
		for _, d := range t.DependsOn {
			if dt, ok := st.task[d]; ok && dt.Status != "done" {
				waits = append(waits, "“"+dt.Title+"”")
			}
		}
		if len(waits) > 0 {
			line += ", waits on " + strings.Join(waits, ", ")
		}
		b.WriteString(line + "\n")
	}
	if len(tasks) == 0 {
		b.WriteString("- no tasks yet\n")
	}
}

func ago(now, t time.Time) string {
	d := now.Sub(t).Round(time.Minute)
	if d < time.Minute {
		return "just now"
	}
	return dur(d) + " ago"
}

func avg(ds []time.Duration) time.Duration {
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return sum / time.Duration(len(ds))
}

// dur reads like "1h 5m" or "12m".
func dur(d time.Duration) string {
	d = d.Round(time.Minute)
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	}
	return "under a minute"
}
