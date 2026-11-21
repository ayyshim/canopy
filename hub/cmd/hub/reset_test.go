package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/store"
)

func TestResetTwoFactorCommand(t *testing.T) {
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		admin = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Skipf("needs Postgres: %v", err)
	}
	defer conn.Close(ctx)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "canopy_cmd_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	st, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	uid, sid := uuid.New(), uuid.New()
	for _, q := range []string{
		`INSERT INTO users (id, email, name, avatar_color, totp_secret, totp_enabled_at, totp_last_step) VALUES ('` + uid.String() + `', 'locked@example.com', 'Lou', '#000000', 'ABC', now(), 5)`,
		`INSERT INTO user_recovery_codes (id, user_id, code_hash) VALUES (gen_random_uuid(), '` + uid.String() + `', '\x01')`,
		`INSERT INTO auth_sessions (id, user_id, token_hash, expires_at) VALUES ('` + sid.String() + `', '` + uid.String() + `', '\x02', now() + interval '1 day')`,
	} {
		if _, err := st.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	t.Setenv("DATABASE_URL", u.String())
	data := t.TempDir()
	if err := resetTwoFactorCmd(nil); err == nil {
		t.Error("no email accepted")
	}
	if err := resetTwoFactorCmd([]string{"nobody@example.com", "--data", data}); err == nil {
		t.Error("unknown email accepted")
	}
	if err := resetTwoFactorCmd([]string{"Locked@Example.com", "--data", data}); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var secret *string
	var codes, sessions, events int
	_ = st.Pool.QueryRow(ctx, `SELECT totp_secret FROM users WHERE id = $1`, uid).Scan(&secret)
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, uid).Scan(&codes)
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM auth_sessions WHERE user_id = $1`, uid).Scan(&sessions)
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE type = 'user.two_factor_reset'`).Scan(&events)
	if secret != nil || codes != 0 || sessions != 0 || events != 1 {
		t.Errorf("secret=%v codes=%d sessions=%d events=%d", secret, codes, sessions, events)
	}
}
