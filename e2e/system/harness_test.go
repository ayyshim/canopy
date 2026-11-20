// Package system runs the real hub and client binaries together: pairing over mutual TLS,
// sessions driven by fakeclaude, signals answered through the REST API.
package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var bins struct{ hub, canopy, fakeclaude string }

const adminDB = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "canopy-system-bins")
	if err != nil {
		panic(err)
	}
	root, _ := filepath.Abs("../..")
	builds := []struct{ out, mod, pkg string }{
		{filepath.Join(dir, "hub"), "hub", "./cmd/hub"},
		{filepath.Join(dir, "canopy"), "client", "./cmd/canopy"},
		{filepath.Join(dir, "fakeclaude"), "client", "./cmd/fakeclaude"},
	}
	for _, b := range builds {
		cmd := exec.Command("go", "build", "-o", b.out, b.pkg)
		cmd.Dir = filepath.Join(root, b.mod)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n%s", b.pkg, err, out)
			os.Exit(1)
		}
	}
	bins.hub, bins.canopy, bins.fakeclaude = builds[0].out, builds[1].out, builds[2].out
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type hub struct {
	t     *testing.T
	url   string
	dbURL string
	tmp   string
	cmd   *exec.Cmd
	logs  *lockedBuf
	db    *pgx.Conn
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// startHub runs a hub on a fresh database.
func startHub(t *testing.T) *hub {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminDB)
	if err != nil {
		t.Skipf("needs Postgres (make infra): %v", err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "canopy_sys_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close(context.Background())
	})
	u, _ := url.Parse(adminDB)
	u.Path = "/" + name
	h := &hub{t: t, dbURL: u.String(), tmp: t.TempDir(), logs: &lockedBuf{}}
	port, rport := freePort(t), freePort(t)
	h.url = fmt.Sprintf("http://127.0.0.1:%d", port)
	h.cmd = exec.Command(bins.hub)
	h.cmd.Env = append(os.Environ(), "DATABASE_URL="+h.dbURL, fmt.Sprintf("HUB_ADDR=127.0.0.1:%d", port), fmt.Sprintf("RUNNER_ADDR=127.0.0.1:%d", rport),
		"PUBLIC_URL="+h.url, "COOKIE_SECURE=false", "BLOB_DIR="+filepath.Join(h.tmp, "blobs"), "HUB_SETUP_TOKEN=system-test", "REDIS_URL=", "SMTP_ADDR=", "AUTH_RATE_LIMIT=1000")
	h.cmd.Stdout, h.cmd.Stderr = h.logs, h.logs
	if err := h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.cmd.Process.Signal(os.Interrupt)
		_ = h.cmd.Wait()
		if t.Failed() {
			t.Logf("hub log:\n%s", tail(h.logs.String(), 80))
		}
	})
	waitFor(t, 20*time.Second, "hub to start", func() bool {
		resp, err := http.Get(h.url + "/healthz")
		if err == nil {
			resp.Body.Close()
		}
		return err == nil && resp.StatusCode == http.StatusNoContent
	})
	if h.db, err = pgx.Connect(ctx, h.dbURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			h.dump()
		}
		h.db.Close(context.Background())
	})
	return h
}

func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func waitFor(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// client is a signed-in browser stand-in.
type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
	me   map[string]any
}

func (h *hub) client() *client {
	jar, _ := cookiejar.New(nil)
	c := &client{t: h.t, base: h.url, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	c.csrf = str(c.do("GET", "/api/v1/auth/csrf", nil, 200)["token"])
	return c
}

func (c *client) do(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	if want != 0 && resp.StatusCode != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, data)
	}
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func m(v any) map[string]any { x, _ := v.(map[string]any); return x }

func list(v any) []any { x, _ := v.([]any); return x }

// setupOrg sets up the hub and signs up an organization owner.
func (h *hub) setupOrg(org, name, email string) (*client, string) {
	admin := h.client()
	admin.do("POST", "/api/v1/hub/setup", map[string]any{"token": "system-test", "hub": map[string]any{"name": "Test hub"},
		"admin": map[string]any{"name": "Hub Admin", "email": "admin@hub.test", "password": "admin-pass-1"}}, 201)
	owner := h.client()
	me := owner.do("POST", "/api/v1/auth/signup", map[string]any{"org": map[string]any{"name": org, "size": "2_10"},
		"user": map[string]any{"name": name, "email": email, "password": "owner-pass-1", "accept_terms": true}}, 201)
	owner.me = me
	return owner, str(me["current_org_id"])
}

// machine is a computer running `canopy`, with its own home directory.
type machine struct {
	t       *testing.T
	home    string
	sandbox string
	cmd     *exec.Cmd
	logs    *lockedBuf
	id      string
}

func (h *hub) pair(c *client, org, kind, name, sandbox string) *machine {
	h.t.Helper()
	code := str(c.do("POST", "/api/v1/orgs/"+org+"/runners/pairing", map[string]any{"name": name, "kind": kind}, 201)["code"])
	mc := &machine{t: h.t, home: h.t.TempDir(), sandbox: sandbox, logs: &lockedBuf{}}
	out, err := mc.command("pair", "--hub", h.url, "--code", code).CombinedOutput()
	if err != nil {
		h.t.Fatalf("canopy pair: %v\n%s", err, out)
	}
	runners := list(c.do("GET", "/api/v1/orgs/"+org+"/runners", nil, 200)["runners"])
	for _, r := range runners {
		if str(m(r)["name"]) == name {
			mc.id = str(m(r)["id"])
		}
	}
	if mc.id == "" {
		h.t.Fatalf("paired machine not listed: %v", runners)
	}
	return mc
}

func (mc *machine) command(args ...string) *exec.Cmd {
	cmd := exec.Command(bins.canopy, args...)
	cmd.Env = append(os.Environ(), "HOME="+mc.home, "CANOPY_HOME="+filepath.Join(mc.home, ".config", "canopy"),
		"CANOPY_DATA="+filepath.Join(mc.home, ".local", "share", "canopy"), "CANOPY_CLAUDE_BIN="+bins.fakeclaude, "XDG_DATA_HOME=", "XDG_CONFIG_HOME=")
	return cmd
}

func (mc *machine) start() {
	args := []string{"run", "--foreground", "--sandbox", mc.sandbox}
	if mc.sandbox == "none" {
		args = append(args, "--unsafe-no-sandbox")
	}
	mc.cmd = mc.command(args...)
	mc.cmd.Stdout, mc.cmd.Stderr = mc.logs, mc.logs
	if err := mc.cmd.Start(); err != nil {
		mc.t.Fatal(err)
	}
	mc.t.Cleanup(func() {
		mc.stop()
		if mc.t.Failed() {
			mc.t.Logf("canopy log (%s):\n%s", mc.home, tail(mc.logs.String(), 60))
		}
	})
}

func (mc *machine) stop() {
	if mc.cmd == nil || mc.cmd.Process == nil {
		return
	}
	_ = mc.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = mc.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = mc.cmd.Process.Kill()
	}
	mc.cmd = nil
}

func (h *hub) waitOnline(c *client, org, runnerID string, online bool) {
	h.t.Helper()
	waitFor(h.t, 20*time.Second, fmt.Sprintf("machine online=%v", online), func() bool {
		for _, r := range list(c.do("GET", "/api/v1/orgs/"+org+"/runners", nil, 200)["runners"]) {
			if str(m(r)["id"]) == runnerID {
				return m(r)["online"] == online
			}
		}
		return false
	})
}

func sandboxKind() string {
	if _, err := exec.LookPath("bwrap"); err == nil {
		return "bwrap"
	}
	return "none"
}

// board fetches the workspace board.
func (c *client) board(ws string) map[string]any {
	return c.do("GET", "/api/v1/workspaces/"+ws+"/board", nil, 200)
}

func (c *client) goal(ws, managerID, title string) string {
	r := c.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": managerID, "title": title}, 201)
	return str(m(r["goal"])["id"])
}

func goalStatus(b map[string]any, goalID string) string {
	for _, g := range list(b["goals"]) {
		if str(m(g)["id"]) == goalID {
			return str(m(g)["status"])
		}
	}
	return ""
}

// waitSignal waits for an open signal of a type and returns it.
func (c *client) waitSignal(ws, typ string, d time.Duration) map[string]any {
	c.t.Helper()
	var found map[string]any
	waitFor(c.t, d, "a "+typ+" signal", func() bool {
		for _, s := range list(c.board(ws)["signals"]) {
			if str(m(s)["type"]) == typ {
				found = m(s)
				return true
			}
		}
		return false
	})
	return found
}

func (c *client) waitGoalDone(ws, goalID string, d time.Duration) map[string]any {
	c.t.Helper()
	var b map[string]any
	waitFor(c.t, d, "goal done", func() bool {
		b = c.board(ws)
		return goalStatus(b, goalID) == "done"
	})
	return b
}

func memberID(b map[string]any, name string) string {
	for _, x := range list(b["members"]) {
		if str(m(x)["name"]) == name {
			return str(m(x)["id"])
		}
	}
	return ""
}

// dump logs the agent state when a test fails.
func (h *hub) dump() {
	ctx := context.Background()
	for _, q := range []string{
		`SELECT 'goal', title, status FROM goals`,
		`SELECT 'task', title || ' [' || kind || ']', status || ' ' || progress FROM tasks`,
		`SELECT 'session', number::text || ' ' || coalesce(cli_session_id, ''), status FROM sessions`,
		`SELECT 'signal', type, status || ' ' || payload::text FROM signals`,
		`SELECT 'log', kind, left(text, 160) FROM log_lines ORDER BY id DESC LIMIT 15`,
	} {
		rows, err := h.db.Query(ctx, q)
		if err != nil {
			h.t.Logf("dump: %v", err)
			continue
		}
		for rows.Next() {
			var a, b, c string
			_ = rows.Scan(&a, &b, &c)
			h.t.Logf("%s: %s — %s", a, b, c)
		}
		rows.Close()
	}
}
