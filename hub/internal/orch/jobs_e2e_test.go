package orch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/core"
	"canopy/hub/internal/jobs"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Phase 1 exit check (docs/design/job-scheduler.md section 13): both job types run on a fake clock,
// and the whole path is real: engine tick, run and goal creation, job_run_report, finishGoals.
type e2e struct {
	t      *testing.T
	st     *store.Store
	o      *Orch
	eng    *jobs.Engine
	clock  time.Time
	ws     db.Workspace
	user   uuid.UUID
	mgr    db.Member
	normal uuid.UUID // a normal goal with all work done, to prove it still gets a handover
}

var e2eDay = time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

func newE2E(t *testing.T) *e2e {
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
	st, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &e2e{t: t, st: st, clock: e2eDay.Add(9*time.Hour + 5*time.Second), user: uuid.New(), normal: uuid.New()}
	org, wsID, mgr := uuid.New(), uuid.New(), uuid.New()
	f.exec(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ($1, 'U', 'u@x.test', 'x', 'red')`, f.user)
	f.exec(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit) VALUES ($1, 'O', 'o', 'just_me', $2, 1000000, 10000000)`, org, f.user)
	f.exec(`INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'owner')`, org, f.user)
	f.exec(`INSERT INTO workspaces (id, org_id, name) VALUES ($1, $2, 'W')`, wsID, org)
	f.exec(`INSERT INTO members (id, workspace_id, name, kind, cli, model, avatar_color) VALUES ($1, $2, 'Maya', 'manager', 'claude', 'm', 'red')`, mgr, wsID)
	f.exec(`INSERT INTO goals (id, workspace_id, title, color, status, number, manager_id) VALUES ($1, $2, 'Normal goal', 'red', 'active', next_item_number($2, 'goal'), $3)`, f.normal, wsID, mgr)
	f.exec(`INSERT INTO tasks (id, workspace_id, goal_id, member_id, kind, title, status, number) VALUES (gen_random_uuid(), $1, $2, $3, 'work', 'Work', 'done', next_item_number($1, 'task'))`, wsID, f.normal, mgr)
	f.ws, _ = st.GetWorkspace(ctx, wsID)
	f.mgr, _ = st.GetMember(ctx, mgr)
	now := func() time.Time { return f.clock }
	svc := &core.Service{Store: st, Bus: realtime.NewMemory(), Now: now}
	f.o = &Orch{Store: st, R: noRunners{}, Core: svc}
	f.eng = &jobs.Engine{Core: svc, Now: now}
	return f
}

func (f *e2e) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.st.Pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *e2e) job(kind, title string, tweak func(*db.CreateJobParams)) db.Job {
	f.t.Helper()
	due := e2eDay.Add(9 * time.Hour)
	p := db.CreateJobParams{ID: uuid.New(), WorkspaceID: f.ws.ID, Title: title, Kind: kind, Brief: "Do the " + title + " work.",
		OwnerUserID: &f.user, ManagerID: &f.mgr.ID, Cron: "0 9 * * *", Timezone: "UTC", NextRunAt: &due,
		MissedPolicy: "skip", CatchupWindowSecs: 3600, RunTimeoutSecs: 30 * 86400, Notify: "attention", RequiredDirs: []byte("[]"), CreatedBy: &f.user}
	if kind == "continuous" {
		p.MissedPolicy, p.CatchupWindowSecs = "run_once", 6*3600
	}
	if tweak != nil {
		tweak(&p)
	}
	ctx := context.Background()
	j, err := f.st.CreateJob(ctx, p)
	if err != nil {
		f.t.Fatal(err)
	}
	if kind == "continuous" {
		n, err := f.st.CreateNote(ctx, db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.ws.ID, Kind: "job_memory", Title: "Memory: " + title, AuthorUserID: &f.user, JobID: &j.ID})
		if err != nil {
			f.t.Fatal(err)
		}
		if j, err = f.st.UpdateJob(ctx, db.UpdateJobParams{ID: j.ID, MemoryNoteID: &n.ID}); err != nil {
			f.t.Fatal(err)
		}
	}
	return j
}

func (f *e2e) at(d time.Duration) { f.clock = e2eDay.Add(d) }

func (f *e2e) tick() {
	f.t.Helper()
	if err := f.eng.Tick(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

// running returns the job's running run, its task and the task's prompt text.
func (f *e2e) running(j db.Job) (db.JobRun, db.Task, string) {
	f.t.Helper()
	ctx := context.Background()
	run, err := f.st.RunningJobRun(ctx, j.ID)
	if err != nil {
		f.t.Fatalf("no running run for %s: %v", j.Title, err)
	}
	tasks, err := f.st.ListGoalTasks(ctx, run.GoalID)
	if err != nil || len(tasks) != 1 || tasks[0].Kind != "work" {
		f.t.Fatalf("a job run wants exactly one work task and no plan task: %v %+v", err, tasks)
	}
	return run, tasks[0], tasks[0].Description
}

// report is the agent calling job_run_report through the real tool handler.
func (f *e2e) report(task db.Task, title, result string, memory *string) error {
	args, _ := json.Marshal(map[string]any{"title": title, "summary": title + " done.", "result": result, "new_memory": memory})
	tc := &toolCtx{call: protocol.ToolCall{Tool: protocol.ToolJobRunReport, Args: args}, task: &task, ws: f.ws, member: f.mgr}
	return f.o.toolJobRunReport(context.Background(), tc)
}

// finish marks the run's task done and lets the orchestrator close the goal, as it does after complete_task.
func (f *e2e) finish(task db.Task) {
	f.t.Helper()
	f.exec(`UPDATE tasks SET status = 'done' WHERE id = $1`, task.ID)
	err := f.o.Core.Tx(context.Background(), func(tx *core.Tx) error {
		st, err := f.o.load(context.Background(), tx.Q, f.ws.ID)
		if err != nil {
			return err
		}
		return f.o.finishGoals(context.Background(), tx, st)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// doRun is one normal run: the agent reports, then the goal finishes.
func (f *e2e) doRun(j db.Job, title string, memory *string) (db.JobRun, string) {
	f.t.Helper()
	run, task, prompt := f.running(j)
	if err := f.report(task, title, "ok", memory); err != nil {
		f.t.Fatalf("report: %v", err)
	}
	f.finish(task)
	got, err := f.st.GetJobRun(context.Background(), run.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return got, prompt
}

func (f *e2e) memory(j db.Job) string {
	f.t.Helper()
	text, err := jobs.Memory(context.Background(), f.st.Queries, j)
	if err != nil {
		f.t.Fatal(err)
	}
	return text
}

func (f *e2e) runs(j db.Job) map[int32]db.JobRun {
	f.t.Helper()
	rs, err := f.st.LastJobRuns(context.Background(), db.LastJobRunsParams{JobID: j.ID, Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[int32]db.JobRun{}
	for _, r := range rs {
		out[r.Number] = r
	}
	return out
}

func ptr2(s string) *string { return &s }

func TestJobsPhase1ExitCheck(t *testing.T) {
	f := newE2E(t)
	ctx := context.Background()
	single := f.job("single", "Log check", nil)
	cont := f.job("continuous", "Lighthouse", nil)

	// Three days, one run of each job per day.
	prompts := map[int32]string{}
	for day := 0; day < 3; day++ {
		f.at(time.Duration(day)*24*time.Hour + 9*time.Hour + 5*time.Second)
		f.tick()
		n := int32(day + 1)

		run, prompt := f.doRun(single, "Log check "+string(rune('1'+day)), nil)
		if run.Number != n || run.Status != "succeeded" || run.FinishedAt == nil {
			t.Errorf("single run %d: %+v", n, run)
		}
		if strings.Contains(prompt, "Memory") || strings.Contains(prompt, "Earlier runs") || !strings.Contains(prompt, jobs.Footer) {
			t.Errorf("single run %d prompt has memory or lacks the footer:\n%s", n, prompt)
		}

		mem := "Keeper chapter " + string(rune('1'+day))
		run, prompts[n] = f.doRun(cont, "Episode "+string(rune('1'+day)), &mem)
		if run.Number != n || run.Status != "succeeded" || run.MemoryAfter == nil || *run.MemoryAfter != mem {
			t.Errorf("continuous run %d: %+v", n, run)
		}
	}
	// A single run job has no memory to write.
	f.at(3*24*time.Hour + 9*time.Hour + 5*time.Second)
	f.tick()
	_, task, _ := f.running(single)
	if err := f.report(task, "x", "ok", ptr2("secret")); err == nil {
		t.Error("a single run job saved memory")
	}
	if err := f.report(task, "x", "ok", nil); err != nil {
		t.Fatal(err)
	}
	f.finish(task)

	// Run 1 has no history; run 2 sees run 1 and its memory; run 3 sees runs 1 and 2 and the memory saved by run 2.
	if strings.Contains(prompts[1], "Earlier runs") {
		t.Errorf("run 1 prompt has earlier runs:\n%s", prompts[1])
	}
	p3 := prompts[3]
	for _, want := range []string{"run 3", "Keeper chapter 2", `#1 Mar 2 · "Episode 1" · ok`, `#2 Mar 3 · "Episode 2" · ok`, jobs.Footer} {
		if !strings.Contains(p3, want) {
			t.Errorf("run 3 prompt lacks %q:\n%s", want, p3)
		}
	}
	if strings.Contains(p3, "Keeper chapter 1") || strings.Contains(p3, "Keeper chapter 3") {
		t.Errorf("run 3 prompt must carry run 2's memory only:\n%s", p3)
	}
	if !strings.Contains(prompts[2], "Keeper chapter 1") {
		t.Errorf("run 2 prompt lacks run 1's memory:\n%s", prompts[2])
	}
	if got := f.memory(cont); got != "Keeper chapter 3" {
		t.Errorf("memory after run 3: %q", got)
	}

	// A failed run (no report) leaves memory unchanged and counts as a failure.
	f.at(3*24*time.Hour + 9*time.Hour + 5*time.Second)
	f.tick()
	failed, task, _ := f.running(cont)
	f.finish(task)
	r4, _ := f.st.GetJobRun(ctx, failed.ID)
	if r4.Status != "failed" || r4.Error == "" || f.memory(cont) != "Keeper chapter 3" {
		t.Errorf("failed run: %+v memory %q", r4, f.memory(cont))
	}
	if j, _ := f.st.GetJob(ctx, cont.ID); j.ConsecutiveFailures != 1 {
		t.Errorf("failures: %d", j.ConsecutiveFailures)
	}
	// Even a run that proposed new memory and then failed keeps the old memory.
	f.at(4*24*time.Hour + 9*time.Hour + 5*time.Second)
	f.tick()
	_, task, _ = f.running(cont)
	if err := f.report(task, "Bad", "ok", ptr2("poisoned")); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE job_runs SET result = '' WHERE job_id = $1 AND status = 'running'`, cont.ID) // the report never counted
	f.finish(task)
	if f.memory(cont) != "Keeper chapter 3" {
		t.Errorf("a failed run changed memory: %q", f.memory(cont))
	}

	// A test run never writes memory and does not count as a failure or move the schedule.
	cj, _ := f.st.GetJob(ctx, cont.ID)
	next := *cj.NextRunAt
	tr, err := f.eng.TestRun(ctx, cont.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, task, prompt := f.running(cont)
	if !strings.Contains(prompt, "test run") {
		t.Errorf("test run prompt:\n%s", prompt)
	}
	if err := f.report(task, "Trial", "ok", ptr2("trial memory")); err != nil {
		t.Fatal(err)
	}
	f.finish(task)
	tgot, _ := f.st.GetJobRun(ctx, tr.ID)
	cj2, _ := f.st.GetJob(ctx, cont.ID)
	if tgot.Status != "succeeded" || tgot.MemoryAfter != nil || f.memory(cont) != "Keeper chapter 3" ||
		!cj2.NextRunAt.Equal(next) || cj2.ConsecutiveFailures != 2 {
		t.Errorf("test run: %+v memory %q next %v failures %d", tgot, f.memory(cont), cj2.NextRunAt, cj2.ConsecutiveFailures)
	}

	// Skipped runs are recorded with their reason: overlap...
	f.at(5*24*time.Hour + 9*time.Hour + 5*time.Second)
	f.tick() // starts a run that nobody finishes
	running, _, _ := f.running(cont)
	f.at(6*24*time.Hour + 9*time.Hour + 5*time.Second)
	f.tick()
	skipped := f.runs(cont)[running.Number+1]
	if skipped.Status != "skipped" || skipped.SkipReason != "previous run still going" || skipped.GoalID != nil {
		t.Errorf("overlap: %+v", skipped)
	}
	// ...and too late.
	late := f.job("single", "Late job", func(p *db.CreateJobParams) { d := e2eDay.Add(24 * time.Hour * 7); p.NextRunAt = &d })
	f.at(7*24*time.Hour + 12*time.Hour)
	f.tick()
	lr := f.runs(late)[1]
	if lr.Status != "skipped" || lr.SkipReason != "too late" {
		t.Errorf("too late: %+v", lr)
	}
	if j, _ := f.st.GetJob(ctx, late.ID); !j.NextRunAt.After(f.clock) {
		t.Errorf("late job next run %v is not after now %v", j.NextRunAt, f.clock)
	}

	// Job goals closed with no handover; the normal goal still gets exactly one.
	var jobGoals, withHandover, jobCards int
	_ = f.st.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE EXISTS (SELECT 1 FROM tasks t WHERE t.goal_id = g.id AND t.kind = 'handover'))
		FROM goals g WHERE g.job_run_id IS NOT NULL AND g.status = 'done'`).Scan(&jobGoals, &withHandover)
	_ = f.st.Pool.QueryRow(ctx, `SELECT count(*) FROM signals s JOIN goals g ON g.id = s.goal_id WHERE g.job_run_id IS NOT NULL AND s.type = 'goal_ready'`).Scan(&jobCards)
	if jobGoals < 8 || withHandover != 0 || jobCards != 0 {
		t.Errorf("job goals: %d closed, %d with a handover, %d goal-ready cards", jobGoals, withHandover, jobCards)
	}
	var normalHandovers int
	_ = f.st.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE goal_id = $1 AND kind = 'handover'`, f.normal).Scan(&normalHandovers)
	if ng, _ := f.st.GetGoal(ctx, f.normal); normalHandovers != 1 || ng.Status != "active" {
		t.Errorf("normal goal: %d handover tasks, status %s", normalHandovers, ng.Status)
	}
}
