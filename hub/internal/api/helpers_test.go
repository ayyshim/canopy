package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/api"
	"canopy/hub/internal/auth"
	"canopy/hub/internal/config"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
)

var (
	testDBURL   string
	testDBAdmin string
)

// createDB makes an empty, migrated database on the dev Postgres and returns its URL and a drop func.
func createDB(ctx context.Context) (string, func(), error) {
	conn, err := pgx.Connect(ctx, testDBAdmin)
	if err != nil {
		return "", nil, fmt.Errorf("tests need Postgres (run `make infra`): %w", err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "canopy_test_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		conn.Close(ctx)
		return "", nil, err
	}
	u, _ := url.Parse(testDBAdmin)
	u.Path = "/" + name
	drop := func() {
		_, _ = conn.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		conn.Close(context.Background())
	}
	st, err := store.Open(ctx, u.String())
	if err == nil {
		err = st.Migrate(ctx)
		st.Close()
	}
	if err != nil {
		drop()
		return "", nil, err
	}
	return u.String(), drop, nil
}

// TestMain creates one shared database with the hub already set up.
func TestMain(m *testing.M) {
	testDBAdmin = os.Getenv("TEST_DATABASE_URL")
	if testDBAdmin == "" {
		testDBAdmin = "postgres://canopy:canopy@localhost:5432/postgres?sslmode=disable"
	}
	ctx := context.Background()
	dbURL, drop, err := createDB(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDBURL = dbURL
	code := 1
	if conn, err := pgx.Connect(ctx, dbURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		_, err = conn.Exec(ctx, `INSERT INTO hub_settings (name) VALUES ('Test hub')`)
		conn.Close(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			code = m.Run()
		}
	}
	drop()
	os.Exit(code)
}

type env struct {
	t     *testing.T
	srv   *api.Server
	ts    *httptest.Server
	store *store.Store
	mail  *mail.Memory
	ipSeq int
}

func newEnv(t *testing.T) *env { return newEnvOn(t, testDBURL) }

// newFreshEnv runs against a new database whose hub has not been set up.
func newFreshEnv(t *testing.T) *env {
	t.Helper()
	dbURL, drop, err := createDB(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return newEnvOn(t, dbURL)
}

func newEnvOn(t *testing.T, dbURL string) *env {
	t.Helper()
	st, err := store.Open(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	cfg := config.Config{
		PublicURL: "http://canopy.test", DefaultSessionLimit: 2_000_000, DefaultWeeklyLimit: 20_000_000,
		AuthRateLimit: 10, TrustProxy: true, SessionTTL: 30 * 24 * time.Hour, ResetTTL: 30 * time.Minute,
	}
	mem := &mail.Memory{}
	srv := api.New(cfg, st, mem, &auth.MemoryLimiter{}, realtime.NewMemory())
	srv.Argon2 = auth.Argon2Params{MemoryKiB: 1024, Iterations: 1, Threads: 1}
	srv.SetupToken = "setup-code-for-tests"
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return &env{t: t, srv: srv, ts: ts, store: st, mail: mem}
}

// client is a browser-like API client: cookie jar, CSRF header, its own client IP.
type client struct {
	e    *env
	http *http.Client
	ip   string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	e.ipSeq++
	return &client{e: e, ip: fmt.Sprintf("10.0.%d.%d", e.ipSeq/250, e.ipSeq%250+1),
		http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type resp struct {
	Status int
	Body   map[string]any
	Header http.Header
}

func (r resp) errCode() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (r resp) field(name string) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		if f, ok := e["fields"].(map[string]any); ok {
			s, _ := f[name].(string)
			return s
		}
	}
	return ""
}

func (c *client) csrf() string {
	u, _ := url.Parse(c.e.ts.URL)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == auth.CSRFCookie {
			return ck.Value
		}
	}
	r := c.raw(http.MethodGet, "/api/v1/auth/csrf", nil, false)
	tok, _ := r.Body["token"].(string)
	return tok
}

func (c *client) do(method, path string, body any) resp { return c.raw(method, path, body, true) }

func (c *client) raw(method, path string, body any, withCSRF bool) resp {
	c.e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", c.ip)
	if withCSRF && method != http.MethodGet {
		req.Header.Set(auth.CSRFHeader, c.csrf())
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	out := resp{Status: res.StatusCode, Header: res.Header}
	data, _ := io.ReadAll(res.Body)
	if len(bytes.TrimSpace(data)) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(data, &out.Body); err != nil {
			c.e.t.Fatalf("%s %s: non-JSON body %q", method, path, data)
		}
	}
	return out
}

func (c *client) cookie(name string) *http.Cookie {
	u, _ := url.Parse(c.e.ts.URL)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

var seq int

func uniqueEmail(prefix string) string {
	seq++
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%d-%s@example.com", prefix, seq, hex.EncodeToString(b))
}

func signupBody(orgName, name, email string) map[string]any {
	return map[string]any{
		"org":  map[string]any{"name": orgName, "size": "2_10", "about": "Payments"},
		"user": map[string]any{"name": name, "email": email, "password": "correct horse", "accept_terms": true},
	}
}

// signup registers a fresh user and organization and returns the signed-in client and /me body.
func (e *env) signup(orgName string) (*client, map[string]any) {
	e.t.Helper()
	c := e.client()
	email := uniqueEmail("owner")
	r := c.do("POST", "/api/v1/auth/signup", signupBody(orgName, "Riley Park", email))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("signup: %d %v", r.Status, r.Body)
	}
	return c, e.finishSignIn(c, email, r.Body)
}

// finishSignIn passes the two-factor step a sign-in response asks for and returns /me.
// New people enroll with a code computed from the secret; enrolled people verify with a
// fresh code (the used-step marker is cleared first so tests aren't limited to a few
// codes per half minute; the replay test doesn't use this helper).
func (e *env) finishSignIn(c *client, email string, signIn map[string]any) map[string]any {
	e.t.Helper()
	switch next := str(signIn["two_factor"]); next {
	case "enroll":
		setup := c.do("POST", "/api/v1/auth/two-factor/setup", nil)
		if setup.Status != 200 {
			e.t.Fatalf("two-factor setup: %d %v", setup.Status, setup.Body)
		}
		r := c.do("POST", "/api/v1/auth/two-factor/enable", map[string]any{"code": auth.TotpCode(str(setup.Body["secret"]), e.srv.Now())})
		if r.Status != 200 {
			e.t.Fatalf("two-factor enable: %d %v", r.Status, r.Body)
		}
		return r.Body["me"].(map[string]any)
	case "verify":
		r := c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": e.freshCode(email)})
		if r.Status != 200 {
			e.t.Fatalf("two-factor verify: %d %v", r.Status, r.Body)
		}
		return r.Body
	default:
		e.t.Fatalf("sign-in asked for %q, body %v", next, signIn)
		return nil
	}
}

// freshCode returns a valid code for email's authenticator and forgets earlier use.
func (e *env) freshCode(email string) string {
	e.t.Helper()
	var secret string
	err := e.store.Pool.QueryRow(context.Background(),
		`UPDATE users SET totp_last_step = 0 WHERE email = $1 RETURNING totp_secret`, normalized(email)).Scan(&secret)
	if err != nil {
		e.t.Fatal(err)
	}
	return auth.TotpCode(secret, e.srv.Now())
}

// login signs in with the password and completes two-factor, returning /me.
func (e *env) login(c *client, email, password string) map[string]any {
	e.t.Helper()
	r := c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": password})
	if r.Status != 200 {
		e.t.Fatalf("login: %d %v", r.Status, r.Body)
	}
	return e.finishSignIn(c, email, r.Body)
}

func str(v any) string { s, _ := v.(string); return s }

func currentOrg(me map[string]any) string { return str(me["current_org_id"]) }

func userEmail(me map[string]any) string {
	return str(me["user"].(map[string]any)["email"])
}

func resetTokenFrom(t *testing.T, m *mail.Memory) string {
	t.Helper()
	msg, ok := m.Last()
	if !ok {
		t.Fatal("no email sent")
	}
	i := strings.Index(msg.Text, "/reset/")
	if i < 0 {
		t.Fatalf("no reset link in %q", msg.Text)
	}
	return strings.Fields(msg.Text[i+len("/reset/"):])[0]
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
