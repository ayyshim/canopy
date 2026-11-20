// Package store owns the Postgres pool, migrations and transactions.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"canopy/hub/internal/store/db"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool *pgxpool.Pool
	*db.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	return &Store{Pool: pool, Queries: db.New(pool)}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Migrate applies all pending goose migrations.
func (s *Store) Migrate(ctx context.Context) error {
	sqlDB := stdlib.OpenDBFromPool(s.Pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub(migrations, "migrations"))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

// Tx runs fn in a transaction, committing if fn returns nil.
func (s *Store) Tx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return fn(s.Queries.WithTx(tx))
	})
}

// IsNotFound reports whether err means no row matched.
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports whether err is a unique-constraint violation on the named constraint
// (any unique constraint when constraint is empty).
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// MigrationStatus reports the schema version in the database, the newest this binary knows,
// and the names of migrations that would run.
type MigrationStatus struct {
	Current int64
	Latest  int64
	Pending []string
}

func (s *Store) MigrationStatus(ctx context.Context) (MigrationStatus, error) {
	sqlDB := stdlib.OpenDBFromPool(s.Pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub(migrations, "migrations"))
	if err != nil {
		return MigrationStatus{}, err
	}
	var st MigrationStatus
	if st.Current, err = provider.GetDBVersion(ctx); err != nil {
		return st, err
	}
	statuses, err := provider.Status(ctx)
	if err != nil {
		return st, err
	}
	for _, m := range statuses {
		if m.Source.Version > st.Latest {
			st.Latest = m.Source.Version
		}
		if m.State == goose.StatePending {
			st.Pending = append(st.Pending, filepath.Base(m.Source.Path))
		}
	}
	return st, nil
}

// LatestSchema is the newest migration this binary carries (no database needed).
func LatestSchema() int64 {
	entries, _ := fs.ReadDir(migrations, "migrations")
	var latest int64
	for _, e := range entries {
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err == nil && v > latest {
			latest = v
		}
	}
	return latest
}

// MigrationNames lists migrations newer than version (what an upgrade will change).
func MigrationNames(after int64) []string {
	entries, _ := fs.ReadDir(migrations, "migrations")
	var out []string
	for _, e := range entries {
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err == nil && v > after {
			out = append(out, strings.TrimSuffix(e.Name(), ".sql"))
		}
	}
	return out
}
