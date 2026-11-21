package jobs

import (
	"fmt"
	"strings"
	"time"

	"canopy/hub/internal/store/db"
)

// Footer ends every job run; the brief cannot override it.
const Footer = "This is an automated run. Nobody is watching. Do not ask questions; decide and write down your assumptions. " +
	"If you cannot finish, say what is missing in your summary and call complete_task. When you are done, call job_run_report."

// earlierRuns is how many past run lines a continuous run sees.
const earlierRuns = 10

// RunText is the task text of one run: header, brief, then memory and earlier runs
// (continuous jobs only), then the footer. earlier is newest first, as LastJobRuns returns it.
func RunText(j db.Job, run db.JobRun, loc *time.Location, memory string, earlier []db.JobRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Job: %s (run %d, scheduled for %s)\n", j.Title, run.Number, run.ScheduledFor.In(loc).Format("Jan 2 15:04 MST"))
	if run.Trigger == "test" {
		b.WriteString("This is a test run. Nothing it learns is kept.\n")
	}
	fmt.Fprintf(&b, "\nBrief:\n%s\n", j.Brief)
	if j.Kind == "continuous" {
		fmt.Fprintf(&b, "\nMemory (rolling summary):\n%s\n", strings.TrimSpace(memory))
		if lines := runLines(run, loc, earlier); len(lines) > 0 {
			b.WriteString("\nEarlier runs:\n" + strings.Join(lines, "\n") + "\n")
		}
	}
	fmt.Fprintf(&b, "\n%s\n", Footer)
	return b.String()
}

// runLines are the oldest-to-newest log lines of the last runs before this one that did work.
func runLines(cur db.JobRun, loc *time.Location, earlier []db.JobRun) []string {
	var lines []string
	for _, r := range earlier {
		if r.ID == cur.ID || r.Number >= cur.Number || r.Status == "skipped" || r.Status == "running" || r.Trigger == "test" {
			continue
		}
		if len(lines) == earlierRuns {
			break
		}
		outcome := r.Result
		if outcome == "" {
			outcome = r.Status
		}
		line := fmt.Sprintf("#%d %s", r.Number, r.ScheduledFor.In(loc).Format("Jan 2"))
		if r.Title != "" {
			line += fmt.Sprintf(" · %q", r.Title)
		}
		lines = append(lines, line+" · "+outcome)
	}
	for i, k := 0, len(lines)-1; i < k; i, k = i+1, k-1 {
		lines[i], lines[k] = lines[k], lines[i]
	}
	return lines
}
