package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestJobsMigrationUpAndDown(t *testing.T) {
	ctx := context.Background()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		admin = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Skipf("needs Postgres: %v", err)
	}
	defer conn.Close(ctx)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "canopy_test_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
	u, _ := url.Parse(admin)
	u.Path = "/" + name

	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) error {
		_, err := s.Pool.Exec(ctx, sql, args...)
		return err
	}
	must := func(sql string, args ...any) {
		t.Helper()
		if err := exec(sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	must(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ('00000000-0000-0000-0000-0000000000a1', 'U', 'u@x.test', 'x', 'red')`)
	must(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit)
	      VALUES ('00000000-0000-0000-0000-0000000000b1', 'O', 'o', 'just_me', '00000000-0000-0000-0000-0000000000a1', 1, 1)`)
	must(`INSERT INTO workspaces (id, org_id, name) VALUES ('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000b1', 'W')`)
	must(`INSERT INTO jobs (id, workspace_id, title, kind, cron) VALUES ('00000000-0000-0000-0000-0000000000d1', '00000000-0000-0000-0000-0000000000c1', 'J', 'continuous', '0 9 * * *')`)
	run := `INSERT INTO job_runs (id, job_id, number, scheduled_for, trigger, status) VALUES ($1, '00000000-0000-0000-0000-0000000000d1', $2, $3, $4, $5)`
	must(run, "00000000-0000-0000-0000-0000000000e1", 1, "2026-01-01T09:00:00Z", "schedule", "running")

	// A due time starts once, and a job never has two running runs.
	if err := exec(run, "00000000-0000-0000-0000-0000000000e2", 2, "2026-01-01T09:00:00Z", "schedule", "succeeded"); err == nil {
		t.Error("same due time started twice")
	}
	if err := exec(run, "00000000-0000-0000-0000-0000000000e3", 3, "2026-01-02T09:00:00Z", "schedule", "running"); err == nil {
		t.Error("two running runs for one job")
	}
	must(run, "00000000-0000-0000-0000-0000000000e4", 4, "2026-01-01T09:00:00Z", "manual", "succeeded")

	// New values the migration allows.
	must(`INSERT INTO notes (id, workspace_id, kind, title, number, job_id) VALUES ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-0000000000c1', 'job_memory', 'Memory', 1, '00000000-0000-0000-0000-0000000000d1')`)
	must(`INSERT INTO goals (id, workspace_id, title, color, status, number, job_run_id) VALUES ('00000000-0000-0000-0000-000000000101', '00000000-0000-0000-0000-0000000000c1', 'G', 'red', 'stopped', 1, '00000000-0000-0000-0000-0000000000e1')`)

	sqlDB := stdlib.OpenDBFromPool(s.Pool)
	defer sqlDB.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub(migrations, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 29); err != nil {
		t.Fatalf("down: %v", err)
	}
	var left int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM goals WHERE status = 'stopped'`).Scan(&left)
	if left != 0 {
		t.Error("stopped goals survived the down migration")
	}
	if err := exec(`SELECT 1 FROM jobs`); err == nil {
		t.Error("jobs table survived the down migration")
	}
	if err := exec(`INSERT INTO notes (id, workspace_id, kind, title, number) VALUES (gen_random_uuid(), '00000000-0000-0000-0000-0000000000c1', 'job_memory', 'x', 9)`); err == nil {
		t.Error("job_memory kind still allowed after down")
	}
	if _, err := p.UpByOne(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	must(`SELECT 1 FROM jobs`)
}
