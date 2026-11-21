package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		admin = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Skipf("needs Postgres: %v", err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "canopy_test_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		conn.Close(context.Background())
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	s, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

type finFixture struct {
	s    *store.Store
	job  db.Job
	note uuid.UUID
	n    int32
}

func newFinFixture(t *testing.T, kind string) *finFixture {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"user", "org", "ws", "job", "note"} {
		ids[k] = uuid.New()
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ($1, 'U', 'u@x.test', 'x', 'red')`, ids["user"])
	exec(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit) VALUES ($1, 'O', 'o', 'just_me', $2, 1, 1)`, ids["org"], ids["user"])
	exec(`INSERT INTO workspaces (id, org_id, name) VALUES ($1, $2, 'W')`, ids["ws"], ids["org"])
	exec(`INSERT INTO jobs (id, workspace_id, title, kind, cron, brief) VALUES ($1, $2, 'J', $3, '0 9 * * *', 'Write an episode')`, ids["job"], ids["ws"], kind)
	f := &finFixture{s: s, note: ids["note"]}
	if kind == "continuous" {
		exec(`INSERT INTO notes (id, workspace_id, kind, title, body, number, job_id) VALUES ($1, $2, 'job_memory', 'Memory', 'old memory', 1, $3)`, ids["note"], ids["ws"], ids["job"])
		exec(`UPDATE jobs SET memory_note_id = $1 WHERE id = $2`, ids["note"], ids["job"])
	}
	var err error
	if f.job, err = s.GetJob(ctx, ids["job"]); err != nil {
		t.Fatal(err)
	}
	return f
}

// start makes a running run with its goal.
func (f *finFixture) start(t *testing.T, trigger string) (db.JobRun, db.Goal) {
	t.Helper()
	ctx := context.Background()
	f.n++
	now := time.Now().UTC().Add(time.Duration(f.n) * time.Hour)
	run, err := f.s.CreateJobRun(ctx, db.CreateJobRunParams{ID: uuid.New(), JobID: f.job.ID, Number: f.n, ScheduledFor: now,
		Trigger: trigger, Status: "running", StartedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	gid := uuid.New()
	if _, err := f.s.Pool.Exec(ctx, `INSERT INTO goals (id, workspace_id, title, color, status, number, job_run_id) VALUES ($1, $2, 'G', 'red', 'active', $3, $4)`,
		gid, f.job.WorkspaceID, f.n, run.ID); err != nil {
		t.Fatal(err)
	}
	g, err := f.s.GetGoal(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	return run, g
}

func (f *finFixture) report(t *testing.T, run db.JobRun, mem string) error {
	t.Helper()
	_, err := SaveReport(context.Background(), f.s.Queries, f.job, run, Report{Title: "Ep", Summary: "did it", Result: "ok", NewMemory: &mem})
	return err
}

func (f *finFixture) finish(t *testing.T, g db.Goal) Outcome {
	t.Helper()
	var out Outcome
	err := f.s.Tx(context.Background(), func(q *db.Queries) (err error) {
		out, err = RunFinished(context.Background(), &core.Tx{Q: q}, g, time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *finFixture) memory(t *testing.T) string {
	t.Helper()
	m, err := Memory(context.Background(), f.s.Queries, f.job)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMemorySavedOnSuccess(t *testing.T) {
	f := newFinFixture(t, "continuous")
	run, g := f.start(t, "schedule")
	if err := f.report(t, run, "new memory"); err != nil {
		t.Fatal(err)
	}
	if f.memory(t) != "old memory" {
		t.Fatal("memory changed before the run finished")
	}
	out := f.finish(t, g)
	if out.Run.Status != "succeeded" || f.memory(t) != "new memory" {
		t.Fatalf("status %s memory %q", out.Run.Status, f.memory(t))
	}
	if out.Run.MemoryAfter == nil || *out.Run.MemoryAfter != "new memory" || out.Run.PendingMemory != nil {
		t.Fatalf("run memory fields wrong: %+v", out.Run)
	}
	revs, _ := f.s.ListNoteRevisions(context.Background(), f.note)
	if len(revs) != 1 || revs[0].Body != "old memory" {
		t.Fatalf("revisions %+v", revs)
	}
}

func TestFailedRunKeepsMemory(t *testing.T) {
	f := newFinFixture(t, "continuous")
	run, g := f.start(t, "schedule")
	// Memory is held, but the run never reports a result.
	if _, err := f.s.UpdateJobRun(context.Background(), db.UpdateJobRunParams{ID: run.ID, SetPending: true, PendingMemory: ptr("lost")}); err != nil {
		t.Fatal(err)
	}
	out := f.finish(t, g)
	if out.Run.Status != "failed" || out.Run.Error == "" || f.memory(t) != "old memory" || out.Run.PendingMemory != nil {
		t.Fatalf("got %+v memory %q", out.Run, f.memory(t))
	}
}

func TestTestRunNeverWritesMemory(t *testing.T) {
	f := newFinFixture(t, "continuous")
	run, g := f.start(t, "test")
	if err := f.report(t, run, "from a test"); err != nil {
		t.Fatal(err)
	}
	out := f.finish(t, g)
	if out.Run.Status != "succeeded" || f.memory(t) != "old memory" {
		t.Fatalf("status %s memory %q", out.Run.Status, f.memory(t))
	}
}

func TestMemoryTooLongRefused(t *testing.T) {
	f := newFinFixture(t, "continuous")
	run, _ := f.start(t, "schedule")
	if err := f.report(t, run, strings.Repeat("é", MaxMemoryChars)); err != nil {
		t.Fatalf("exactly the cap should pass: %v", err)
	}
	err := f.report(t, run, strings.Repeat("a", MaxMemoryChars+1))
	if err != ErrMemoryTooLong || !strings.Contains(err.Error(), "Fold the oldest details") {
		t.Fatalf("got %v", err)
	}
}

func TestSingleJobRefusesMemory(t *testing.T) {
	f := newFinFixture(t, "single")
	run, _ := f.start(t, "schedule")
	if err := f.report(t, run, "x"); err == nil {
		t.Fatal("single job accepted memory")
	}
	if _, err := SaveReport(context.Background(), f.s.Queries, f.job, run, Report{Result: "maybe"}); err == nil {
		t.Fatal("bad result accepted")
	}
}

func TestAutoPauseAfterThreeFailures(t *testing.T) {
	f := newFinFixture(t, "single")
	for i := 1; i <= 3; i++ {
		_, g := f.start(t, "schedule")
		out := f.finish(t, g)
		if out.Paused != (i == 3) {
			t.Fatalf("failure %d: paused=%v", i, out.Paused)
		}
		if i == 2 {
			// A test run in between changes nothing.
			_, tg := f.start(t, "test")
			f.finish(t, tg)
		}
	}
	job, _ := f.s.GetJob(context.Background(), f.job.ID)
	if job.Status != "paused" || job.PauseReason != "failures" || job.ConsecutiveFailures != 3 {
		t.Fatalf("job %s %s %d", job.Status, job.PauseReason, job.ConsecutiveFailures)
	}
}

func TestSuccessResetsFailures(t *testing.T) {
	f := newFinFixture(t, "single")
	for i := 0; i < 2; i++ {
		_, g := f.start(t, "schedule")
		f.finish(t, g)
	}
	run, g := f.start(t, "schedule")
	if err := f.report(t, run, ""); err == nil {
		// single job: empty memory pointer is still memory; use no memory instead
		t.Fatal("expected refusal")
	}
	if _, err := SaveReport(context.Background(), f.s.Queries, f.job, run, Report{Title: "t", Result: "nothing_to_do"}); err != nil {
		t.Fatal(err)
	}
	f.finish(t, g)
	job, _ := f.s.GetJob(context.Background(), f.job.ID)
	if job.ConsecutiveFailures != 0 || job.Status != "active" {
		t.Fatalf("job %s %d", job.Status, job.ConsecutiveFailures)
	}
}
