package jobs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

func TestRunText(t *testing.T) {
	at := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	var earlier []db.JobRun
	for n := int32(12); n >= 1; n-- { // newest first, as LastJobRuns returns them
		earlier = append(earlier, db.JobRun{ID: uuid.New(), Number: n, ScheduledFor: at, Status: "succeeded", Result: "ok", Title: fmt.Sprint("Episode ", n)})
	}
	earlier = append(earlier, db.JobRun{ID: uuid.New(), Number: 13, Status: "skipped"})
	cur := db.JobRun{ID: uuid.New(), Number: 14, ScheduledFor: at, Trigger: "schedule"}
	j := db.Job{Title: "Lighthouse", Kind: "continuous", Brief: "Write an episode."}

	got := RunText(j, cur, time.UTC, "Keeper lost his lamp.", earlier)
	for _, want := range []string{"Job: Lighthouse (run 14, scheduled for Jun 3 09:00 UTC)", "Brief:\nWrite an episode.", "Memory (rolling summary):\nKeeper lost his lamp.",
		`#12 Jun 3 · "Episode 12" · ok`, `#3 Jun 3 · "Episode 3" · ok`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "#2 ") || strings.Contains(got, "#13") {
		t.Errorf("only the newest 10 real runs belong:\n%s", got)
	}
	if !strings.HasSuffix(got, Footer+"\n") {
		t.Error("footer missing")
	}
	// Order: brief, memory, earlier runs, footer.
	if !(strings.Index(got, "Brief:") < strings.Index(got, "Memory") && strings.Index(got, "Memory") < strings.Index(got, "Earlier runs") && strings.Index(got, "Earlier runs") < strings.Index(got, Footer)) {
		t.Error("wrong order")
	}

	j.Kind = "single"
	single := RunText(j, cur, time.UTC, "secret memory", earlier)
	if strings.Contains(single, "Memory") || strings.Contains(single, "Earlier runs") || !strings.Contains(single, Footer) {
		t.Errorf("single run text:\n%s", single)
	}
}
