package orch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/core"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
)

// jobGoalEnv seeds a workspace with a manager, a job and two finished goals:
// one for a job run and one normal.
func TestJobGoalSkipsHandover(t *testing.T) {
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

	user, org, ws, mgr, job, run := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	jobGoal, normalGoal := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ($1, 'U', 'u@x.test', 'x', 'red')`, user)
	exec(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit) VALUES ($1, 'O', 'o', 'just_me', $2, 1, 1)`, org, user)
	exec(`INSERT INTO workspaces (id, org_id, name) VALUES ($1, $2, 'W')`, ws, org)
	exec(`INSERT INTO members (id, workspace_id, name, kind, cli, model, avatar_color) VALUES ($1, $2, 'Maya', 'manager', 'claude', 'm', 'red')`, mgr, ws)
	exec(`INSERT INTO jobs (id, workspace_id, title, kind, cron) VALUES ($1, $2, 'J', 'single', '0 9 * * *')`, job, ws)
	exec(`INSERT INTO job_runs (id, job_id, number, scheduled_for, status, result, title) VALUES ($1, $2, 1, now(), 'running', 'ok', 'Checked')`, run, job)
	exec(`INSERT INTO goals (id, workspace_id, title, color, status, number, manager_id, job_run_id) VALUES ($1, $2, 'Job goal', 'red', 'active', 1, $3, $4)`, jobGoal, ws, mgr, run)
	exec(`INSERT INTO goals (id, workspace_id, title, color, status, number, manager_id) VALUES ($1, $2, 'Normal goal', 'red', 'active', 2, $3)`, normalGoal, ws, mgr)
	for _, g := range []uuid.UUID{jobGoal, normalGoal} {
		exec(`INSERT INTO tasks (id, workspace_id, goal_id, member_id, kind, title, status, number) VALUES ($1, $2, $3, $4, 'work', 'Work', 'done', next_item_number($2, 'task'))`, uuid.New(), ws, g, mgr)
	}

	o := &Orch{Store: s, R: noRunners{}, Core: &core.Service{Store: s, Bus: realtime.NewMemory(), Now: time.Now}}
	err = o.Core.Tx(ctx, func(tx *core.Tx) error {
		st, err := o.load(ctx, tx.Q, ws)
		if err != nil {
			return err
		}
		return o.finishGoals(ctx, tx, st)
	})
	if err != nil {
		t.Fatal(err)
	}

	count := func(sql string, args ...any) (n int) {
		t.Helper()
		if err := s.Pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	g, _ := s.GetGoal(ctx, jobGoal)
	if g.Status != "done" {
		t.Errorf("job goal is %s, want done", g.Status)
	}
	if n := count(`SELECT count(*) FROM tasks WHERE goal_id = $1 AND kind = 'handover'`, jobGoal); n != 0 {
		t.Errorf("job goal got %d handover tasks", n)
	}
	if n := count(`SELECT count(*) FROM signals WHERE goal_id = $1 AND type = 'goal_ready'`, jobGoal); n != 0 {
		t.Errorf("job goal opened a goal-ready card")
	}
	r, _ := s.GetJobRun(ctx, run)
	if r.Status != "succeeded" || r.FinishedAt == nil {
		t.Errorf("run is %s", r.Status)
	}
	// A normal goal still waits for its handover brief.
	if n := count(`SELECT count(*) FROM tasks WHERE goal_id = $1 AND kind = 'handover'`, normalGoal); n != 1 {
		t.Errorf("normal goal got %d handover tasks, want 1", n)
	}
	ng, _ := s.GetGoal(ctx, normalGoal)
	if ng.Status != "active" {
		t.Errorf("normal goal is %s, want active", ng.Status)
	}
}

type noRunners struct{}

func (noRunners) Online(uuid.UUID) bool                                      { return false }
func (noRunners) Send(uuid.UUID, string, any) error                          { return nil }
func (noRunners) Request(context.Context, uuid.UUID, string, any, any) error { return nil }
