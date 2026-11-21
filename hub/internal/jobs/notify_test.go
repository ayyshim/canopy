package jobs

import (
	"context"
	"fmt"
	"testing"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

func TestNotifyFollowsTheJobSetting(t *testing.T) {
	cases := []struct {
		setting, kind, status, result string
		paused                        bool
		want                          int
	}{
		{"attention", "single", "succeeded", "ok", false, 0},
		{"attention", "single", "succeeded", "nothing_to_do", false, 0},
		{"always", "single", "succeeded", "ok", false, 0}, // a single run stays silent when fine
		{"attention", "single", "succeeded", "attention", false, 1},
		{"always", "single", "succeeded", "attention", false, 1},
		{"failures", "single", "succeeded", "attention", false, 0},
		{"attention", "continuous", "succeeded", "ok", false, 0},
		{"always", "continuous", "succeeded", "ok", false, 1},
		{"failures", "continuous", "succeeded", "ok", false, 0},
		{"attention", "single", "failed", "", false, 1},
		{"failures", "single", "failed", "", false, 1},
		{"attention", "single", "capped", "", false, 1},
		{"attention", "single", "skipped", "", false, 0},
		{"failures", "single", "failed", "", true, 2}, // the failure and the auto-pause
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i, c.setting, c.kind, c.status, c.result), func(t *testing.T) {
			f := newFixture(t)
			j := f.job(c.kind, func(p *db.CreateJobParams) { p.Notify = c.setting })
			run := db.JobRun{ID: j.ID, JobID: j.ID, Number: 1, Trigger: "schedule", Status: c.status, Result: c.result, Summary: "Odd entries.", Error: "boom"}
			err := f.eng.Core.Tx(context.Background(), func(t *core.Tx) error { return Notify(context.Background(), t, j, run, c.paused) })
			if err != nil {
				t.Fatal(err)
			}
			var n int
			var kind string
			_ = f.st.Pool.QueryRow(context.Background(), `SELECT count(*), coalesce(min(kind), '') FROM notification_outbox WHERE workspace_id = $1`, f.ws).Scan(&n, &kind)
			if n != c.want || (n > 0 && kind != "jobs") {
				t.Errorf("got %d notifications of kind %q, want %d", n, kind, c.want)
			}
		})
	}
}

func TestTestRunsAreSilent(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", nil)
	run := db.JobRun{JobID: j.ID, Number: 1, Trigger: "test", Status: "failed"}
	_ = f.eng.Core.Tx(context.Background(), func(t *core.Tx) error { return Notify(context.Background(), t, j, run, false) })
	var n int
	_ = f.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox`).Scan(&n)
	if n != 0 {
		t.Errorf("test run sent %d", n)
	}
}

func TestJobSignalTypesMapToJobsKind(t *testing.T) {
	for _, s := range []string{"job_attention", "job_failed", "job_paused"} {
		if notify.KindOf(s) != "jobs" {
			t.Errorf("%s -> %q", s, notify.KindOf(s))
		}
	}
}
