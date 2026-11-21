package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/core"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

var adminURL string

func TestMain(m *testing.M) {
	adminURL = os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"
	}
	os.Exit(m.Run())
}

type fakeHost struct {
	mu      sync.Mutex
	kicks   int
	stopped []uuid.UUID
}

func (h *fakeHost) Kick(context.Context, uuid.UUID) { h.mu.Lock(); h.kicks++; h.mu.Unlock() }
func (h *fakeHost) StopGoal(_ context.Context, id uuid.UUID, _ string) error {
	h.mu.Lock()
	h.stopped = append(h.stopped, id)
	h.mu.Unlock()
	return nil
}

type fixture struct {
	t     *testing.T
	st    *store.Store
	host  *fakeHost
	clock time.Time
	eng   *Engine
	org   db.Organization
	ws    uuid.UUID
	user  uuid.UUID
	mgr   uuid.UUID
}

var day = time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminURL)
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
	u, _ := url.Parse(adminURL)
	u.Path = "/" + name
	st, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, host: &fakeHost{}, clock: day.Add(9*time.Hour + 5*time.Second),
		user: uuid.New(), ws: uuid.New(), mgr: uuid.New()}
	org := uuid.New()
	f.exec(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ($1, 'U', 'u@x.test', 'x', 'red')`, f.user)
	f.exec(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit)
		VALUES ($1, 'O', 'o', 'just_me', $2, 1000, 100000)`, org, f.user)
	f.exec(`INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'owner')`, org, f.user)
	f.exec(`INSERT INTO workspaces (id, org_id, name) VALUES ($1, $2, 'W')`, f.ws, org)
	f.exec(`INSERT INTO members (id, workspace_id, name, kind, cli, model, avatar_color) VALUES ($1, $2, 'Riley', 'manager', 'claude', 'sonnet', 'red')`, f.mgr, f.ws)
	if f.org, err = st.GetOrganization(ctx, org); err != nil {
		t.Fatal(err)
	}
	svc := &core.Service{Store: st, Bus: realtime.NewMemory(), Now: func() time.Time { return f.clock }}
	f.eng = &Engine{Core: svc, Host: f.host, Now: func() time.Time { return f.clock }}
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.st.Pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

// job makes an active job due at 09:00 on day; tweak edits it before saving.
func (f *fixture) job(kind string, tweak func(*db.CreateJobParams)) db.Job {
	f.t.Helper()
	due := day.Add(9 * time.Hour)
	p := db.CreateJobParams{ID: uuid.New(), WorkspaceID: f.ws, Title: "Daily logs", Kind: kind, Brief: "Check the logs.",
		OwnerUserID: &f.user, ManagerID: &f.mgr, Cron: "0 9 * * *", Timezone: "UTC", NextRunAt: &due,
		MissedPolicy: "skip", CatchupWindowSecs: 3600, RunTimeoutSecs: 7200, Notify: "attention", RequiredDirs: []byte("[]"), CreatedBy: &f.user}
	if tweak != nil {
		tweak(&p)
	}
	j, err := f.st.CreateJob(context.Background(), p)
	if err != nil {
		f.t.Fatal(err)
	}
	return j
}

func (f *fixture) tick() {
	f.t.Helper()
	if err := f.eng.Tick(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) runs(j db.Job) []db.JobRun {
	f.t.Helper()
	rs, err := f.st.LastJobRuns(context.Background(), db.LastJobRunsParams{JobID: j.ID, Limit: 50})
	if err != nil {
		f.t.Fatal(err)
	}
	return rs
}

func (f *fixture) get(j db.Job) db.Job {
	f.t.Helper()
	g, err := f.st.GetJob(context.Background(), j.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return g
}

func TestDueJobStartsRunAndGoal(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", nil)
	f.tick()
	rs := f.runs(j)
	if len(rs) != 1 || rs[0].Status != "running" || rs[0].Number != 1 || rs[0].GoalID == nil {
		t.Fatalf("runs: %+v", rs)
	}
	g, err := f.st.GetGoal(context.Background(), *rs[0].GoalID)
	if err != nil || g.Status != "active" || g.JobRunID == nil || *g.JobRunID != rs[0].ID {
		t.Fatalf("goal: %+v %v", g, err)
	}
	var kinds string
	_ = f.st.Pool.QueryRow(context.Background(), `SELECT string_agg(kind || ':' || description, '|') FROM tasks WHERE goal_id = $1`, g.ID).Scan(&kinds)
	if !strings.HasPrefix(kinds, "work:Job: Daily logs (run 1") || !strings.Contains(kinds, Footer) || strings.Contains(kinds, "|") {
		t.Errorf("tasks: %q", kinds)
	}
	after := f.get(j)
	if want := day.Add(33 * time.Hour); !after.NextRunAt.Equal(want) {
		t.Errorf("next run %v, want %v", after.NextRunAt, want)
	}
	if f.host.kicks == 0 {
		t.Error("orchestrator not kicked")
	}
	// Another tick the same minute starts nothing.
	f.tick()
	if len(f.runs(j)) != 1 {
		t.Error("same due time started twice")
	}
}

func TestNotDueDoesNothing(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", nil)
	f.clock = day.Add(8 * time.Hour)
	f.tick()
	if len(f.runs(j)) != 0 {
		t.Error("ran early")
	}
}

func TestLateRuns(t *testing.T) {
	f := newFixture(t)
	skip := f.job("single", nil)
	once := f.job("continuous", func(p *db.CreateJobParams) { p.MissedPolicy, p.CatchupWindowSecs = "run_once", 6*3600 })
	missed := f.job("continuous", func(p *db.CreateJobParams) { p.MissedPolicy, p.CatchupWindowSecs = "run_once", 3600 })
	f.clock = day.Add(12 * time.Hour) // three hours late
	f.tick()
	if rs := f.runs(skip); len(rs) != 1 || rs[0].Status != "skipped" || rs[0].SkipReason != "too late" {
		t.Errorf("skip policy: %+v", rs)
	}
	if rs := f.runs(once); len(rs) != 1 || rs[0].Status != "running" {
		t.Errorf("run_once inside window: %+v", rs)
	}
	if rs := f.runs(missed); len(rs) != 1 || rs[0].SkipReason != "too late" {
		t.Errorf("run_once outside window: %+v", rs)
	}
	// Never a pile: after a week down, one skipped row and the next time is in the future.
	f.clock = day.Add(7*24*time.Hour + 12*time.Hour)
	f.tick()
	if len(f.runs(skip)) != 2 || !f.get(skip).NextRunAt.After(f.clock) {
		t.Errorf("after outage: %d runs, next %v", len(f.runs(skip)), f.get(skip).NextRunAt)
	}
}

func TestOverlapIsSkipped(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", func(p *db.CreateJobParams) { p.RunTimeoutSecs = 3 * 86400 })
	f.tick()
	f.clock = day.Add(33*time.Hour + time.Second) // the next day's run is due while run 1 goes on
	f.tick()
	rs := f.runs(j)
	if len(rs) != 2 || rs[0].Status != "skipped" || rs[0].SkipReason != "previous run still going" || rs[1].Status != "running" {
		t.Fatalf("runs: %+v", rs)
	}
}

func TestRestartMidRun(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", func(p *db.CreateJobParams) { p.RunTimeoutSecs = 3 * 86400 })
	f.tick()
	// A new engine (the hub restarted) neither repeats the run nor loses it.
	f.eng = &Engine{Core: f.eng.Core, Host: f.host, Now: f.eng.Now}
	f.tick()
	if rs := f.runs(j); len(rs) != 1 || rs[0].Status != "running" {
		t.Fatalf("after restart: %+v", rs)
	}
	f.clock = day.Add(33*time.Hour + time.Second)
	f.tick()
	if rs := f.runs(j); len(rs) != 2 || rs[0].SkipReason != "previous run still going" {
		t.Fatalf("next day: %+v", rs)
	}
	// The run is stopped by its timeout once it has been going too long.
	f.clock = day.Add(9*time.Hour + 4*24*time.Hour)
	f.tick()
	if rs := f.runs(j); rs[len(rs)-1].Status != "skipped" || rs[len(rs)-1].SkipReason != "computer off" {
		t.Fatalf("after timeout: %+v", rs[len(rs)-1])
	}
}

func TestTimeout(t *testing.T) {
	f := newFixture(t)
	off := f.job("single", nil)
	f.tick()
	f.clock = f.clock.Add(2*time.Hour + time.Minute)
	f.tick()
	rs := f.runs(off)
	if rs[0].Status != "skipped" || rs[0].SkipReason != "computer off" || len(f.host.stopped) != 1 || *rs[0].GoalID != f.host.stopped[0] {
		t.Errorf("computer off: %+v stopped %v", rs[0], f.host.stopped)
	}

	f = newFixture(t)
	slow := f.job("single", nil)
	f.tick()
	f.exec(`UPDATE tasks SET status = 'doing' WHERE goal_id = (SELECT goal_id FROM job_runs WHERE job_id = $1)`, slow.ID)
	f.clock = f.clock.Add(2*time.Hour + time.Minute)
	f.tick()
	rs = f.runs(slow)
	if rs[0].Status != "failed" || rs[0].Error == "" || rs[0].FinishedAt == nil || f.get(slow).ConsecutiveFailures != 1 {
		t.Errorf("timed out: %+v", rs[0])
	}
}

func TestTokenCap(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", func(p *db.CreateJobParams) { c := int64(1000); p.TokenCapPerRun = &c })
	f.tick()
	r := f.runs(j)[0]
	f.exec(`INSERT INTO usage_work (goal_id, task_id, model, tokens_in, tokens_out) VALUES ($1, gen_random_uuid(), 'sonnet', 900, 100)`, r.GoalID)
	f.tick()
	r = f.runs(j)[0]
	if r.Status != "capped" || r.Tokens != 1000 || len(f.host.stopped) != 1 {
		t.Errorf("cap: %+v", r)
	}
	if f.get(j).ConsecutiveFailures != 0 {
		t.Error("a capped run counted as a failure")
	}
}

func TestOwnerChecks(t *testing.T) {
	f := newFixture(t)
	over := f.job("single", nil)
	f.exec(`INSERT INTO usage_events (org_id, workspace_id, attributed_user_id, cli, model, tokens_in, tokens_out, at)
		VALUES ($1, $2, $3, 'claude', 'sonnet', 5000, 0, $4)`, f.org.ID, f.ws, f.user, f.clock.Add(-time.Hour))
	f.tick()
	if rs := f.runs(over); len(rs) != 1 || rs[0].SkipReason != "owner over limit" {
		t.Errorf("over limit: %+v", rs)
	}

	f = newFixture(t)
	gone := f.job("single", nil)
	f.exec(`DELETE FROM memberships WHERE user_id = $1`, f.user)
	f.tick()
	g := f.get(gone)
	if g.Status != "paused" || g.PauseReason != "owner_left" || len(f.runs(gone)) != 0 {
		t.Errorf("owner left: %s %s", g.Status, g.PauseReason)
	}
}

func TestEnds(t *testing.T) {
	f := newFixture(t)
	end := day.Add(8 * time.Hour)
	j := f.job("single", func(p *db.CreateJobParams) { p.EndsAt = &end })
	f.tick()
	if g := f.get(j); g.Status != "ended" || len(f.runs(j)) != 0 {
		t.Errorf("ends_at: %s", g.Status)
	}
}

func TestRunNowAndTestRun(t *testing.T) {
	f := newFixture(t)
	f.clock = day.Add(7 * time.Hour)
	j := f.job("continuous", nil)
	r, err := f.eng.RunNow(context.Background(), j.ID)
	if err != nil || r.Trigger != "manual" || r.Status != "running" {
		t.Fatalf("run now: %+v %v", r, err)
	}
	if !f.get(j).NextRunAt.Equal(*j.NextRunAt) {
		t.Error("run now moved next_run_at")
	}
	if _, err := f.eng.TestRun(context.Background(), j.ID); !errors.Is(err, ErrRunning) {
		t.Errorf("test run during a run: %v", err)
	}
	f.exec(`UPDATE job_runs SET status = 'succeeded' WHERE id = $1`, r.ID)
	tr, err := f.eng.TestRun(context.Background(), j.ID)
	if err != nil || tr.Trigger != "test" || !strings.Contains(fmt.Sprint(taskText(f, tr)), "test run") {
		t.Fatalf("test run: %+v %v", tr, err)
	}
	if !f.get(j).NextRunAt.Equal(*j.NextRunAt) {
		t.Error("test run moved next_run_at")
	}
}

func taskText(f *fixture, r db.JobRun) string {
	var s string
	_ = f.st.Pool.QueryRow(context.Background(), `SELECT description FROM tasks WHERE goal_id = $1`, r.GoalID).Scan(&s)
	return s
}

func TestPauseAndResume(t *testing.T) {
	f := newFixture(t)
	j := f.job("single", nil)
	if _, err := f.eng.Pause(context.Background(), j.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if len(f.runs(j)) != 0 {
		t.Error("a paused job ran")
	}
	f.clock = day.Add(5 * 24 * time.Hour) // paused for days: nothing is replayed
	g, err := f.eng.Resume(context.Background(), j.ID)
	if err != nil || g.Status != "active" || g.PauseReason != "" || !g.NextRunAt.After(f.clock) {
		t.Fatalf("resume: %+v %v", g, err)
	}
	f.tick()
	if len(f.runs(j)) != 0 {
		t.Error("resume replayed a missed run")
	}
	if _, err := f.eng.Resume(context.Background(), j.ID); !errors.Is(err, ErrNotPaused) {
		t.Errorf("resume active: %v", err)
	}
}
