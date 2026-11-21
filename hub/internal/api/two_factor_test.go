package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"canopy/hub/internal/auth"
)

// newPending signs someone up and leaves the session pending.
func (e *env) newPending() (*client, string) {
	e.t.Helper()
	c := e.client()
	email := uniqueEmail("pending")
	r := c.do("POST", "/api/v1/auth/signup", signupBody("Pending Co", "Pat Pending", email))
	if r.Status != http.StatusCreated || r.Body["two_factor"] != "enroll" {
		e.t.Fatalf("signup: %d %v", r.Status, r.Body)
	}
	return c, email
}

func (e *env) secretOf(email string) string {
	e.t.Helper()
	var s string
	if err := e.store.Pool.QueryRow(context.Background(), `SELECT totp_secret FROM users WHERE email = $1`, normalized(email)).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestEnrollThenVerifyOnNextSignIn(t *testing.T) {
	e := newEnv(t)
	c, email := e.newPending()

	if r := c.do("GET", "/api/v1/auth/two-factor", nil); r.Body["next"] != "enroll" {
		t.Fatalf("state: %v", r.Body)
	}
	if r := c.do("POST", "/api/v1/auth/two-factor/enable", map[string]any{"code": "123456"}); r.errCode() != "setup_required" {
		t.Errorf("enable before setup: %d %v", r.Status, r.Body)
	}
	if r := c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": "123456"}); r.errCode() != "not_enrolled" {
		t.Errorf("verify before enrolling: %d %v", r.Status, r.Body)
	}
	setup := c.do("POST", "/api/v1/auth/two-factor/setup", nil)
	secret := str(setup.Body["secret"])
	if setup.Status != 200 || secret == "" || str(setup.Body["otpauth_url"]) == "" || len(str(setup.Body["qr_png"])) < 100 ||
		str(setup.Body["qr_png"])[:22] != "data:image/png;base64," {
		t.Fatalf("setup: %d %v", setup.Status, setup.Body)
	}
	en := c.do("POST", "/api/v1/auth/two-factor/enable", map[string]any{"code": auth.TotpCode(secret, time.Now())})
	if en.Status != 200 {
		t.Fatalf("enable: %d %v", en.Status, en.Body)
	}
	if codes, _ := en.Body["recovery_codes"].([]any); len(codes) != 10 {
		t.Errorf("recovery codes: %v", en.Body["recovery_codes"])
	}
	if got := c.do("GET", "/api/v1/me", nil); got.Status != 200 {
		t.Errorf("/me after enabling: %d", got.Status)
	}
	// The secret is never handed out again.
	if r := c.do("POST", "/api/v1/auth/two-factor/setup", nil); r.Status != http.StatusConflict || r.errCode() != "already_enrolled" {
		t.Errorf("setup after enrolling: %d %v", r.Status, r.Body)
	}
	if r := c.do("GET", "/api/v1/me", nil); toJSON(r.Body) != toJSON(en.Body["me"]) || contains(toJSON(r.Body), secret) {
		t.Errorf("/me differs or leaks the secret")
	}

	// A new sign-in lands pending and asks for a code, not enrollment.
	c2 := e.client()
	login := c2.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	if login.Body["two_factor"] != "verify" {
		t.Fatalf("login: %v", login.Body)
	}
	if r := c2.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "two_factor_required" {
		t.Fatalf("/me while pending: %d %v", r.Status, r.Body)
	}
	if r := c2.do("POST", "/api/v1/auth/two-factor/setup", nil); r.Status != http.StatusConflict {
		t.Errorf("setup by an enrolled person: %d", r.Status)
	}
	e.finishSignIn(c2, email, login.Body)
	if r := c2.do("GET", "/api/v1/me", nil); r.Status != 200 {
		t.Errorf("/me after verify: %d", r.Status)
	}
}

func TestWrongReusedAndExpiredCodes(t *testing.T) {
	e := newEnv(t)
	_, me := e.signup("Codes Co")
	email := userEmail(me)
	secret := e.secretOf(email)
	login := func() *client {
		c := e.client()
		if r := c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"}); r.Body["two_factor"] != "verify" {
			t.Fatalf("login: %v", r.Body)
		}
		return c
	}
	verify := func(c *client, code string) resp {
		return c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": code})
	}
	now := time.Now()

	if r := verify(login(), "000000"); r.Status != 422 || r.errCode() != "invalid_code" {
		t.Errorf("wrong code: %d %v", r.Status, r.Body)
	}
	if r := verify(login(), auth.TotpCode(secret, now.Add(-5*time.Minute))); r.errCode() != "invalid_code" {
		t.Errorf("expired code: %d %v", r.Status, r.Body)
	}
	if r := verify(login(), auth.TotpCode(secret, now.Add(5*time.Minute))); r.errCode() != "invalid_code" {
		t.Errorf("code from the future: %d %v", r.Status, r.Body)
	}

	// The code accepted once can't be used again, even from another session and even a step apart.
	e.freshCode(email)
	good := auth.TotpCode(secret, now)
	if r := verify(login(), good); r.Status != 200 {
		t.Fatalf("good code: %d %v", r.Status, r.Body)
	}
	if r := verify(login(), good); r.Status != 422 || r.errCode() != "invalid_code" {
		t.Errorf("reused code: %d %v", r.Status, r.Body)
	}
	if r := verify(login(), auth.TotpCode(secret, now.Add(-30*time.Second))); r.errCode() != "invalid_code" {
		t.Errorf("older step than the last used: %d %v", r.Status, r.Body)
	}
}

func TestFiveWrongCodesEndTheSession(t *testing.T) {
	e := newEnv(t)
	_, me := e.signup("Lock Co")
	email := userEmail(me)
	c := e.client()
	c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	for i := 1; i <= 4; i++ {
		if r := c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": "000000"}); r.errCode() != "invalid_code" {
			t.Fatalf("attempt %d: %d %v", i, r.Status, r.Body)
		}
	}
	if r := c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": "000000"}); r.Status != 401 || r.errCode() != "too_many_attempts" {
		t.Fatalf("fifth attempt: %d %v", r.Status, r.Body)
	}
	// Even the right code can't revive it.
	if r := c.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": e.freshCode(email)}); r.Status != 401 {
		t.Errorf("session survived: %d %v", r.Status, r.Body)
	}
}

func TestRecoveryCodeWorksOnce(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	email := uniqueEmail("recover")
	r := c.do("POST", "/api/v1/auth/signup", signupBody("Recovery Co", "Rae", email))
	secret := str(c.do("POST", "/api/v1/auth/two-factor/setup", nil).Body["secret"])
	en := c.do("POST", "/api/v1/auth/two-factor/enable", map[string]any{"code": auth.TotpCode(secret, time.Now())})
	_ = r
	codes := en.Body["recovery_codes"].([]any)
	code := str(codes[0])

	signIn := func() *client {
		c := e.client()
		c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
		return c
	}
	c1 := signIn()
	if r := c1.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": "aaaaa-bbbbb"}); r.errCode() != "invalid_code" {
		t.Errorf("unknown recovery code: %d %v", r.Status, r.Body)
	}
	if r := c1.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": code}); r.Status != 200 {
		t.Fatalf("recovery code: %d %v", r.Status, r.Body)
	}
	if r := c1.do("GET", "/api/v1/me", nil); r.Status != 200 {
		t.Errorf("/me after recovery: %d", r.Status)
	}
	if r := signIn().do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": code}); r.Status != 422 || r.errCode() != "invalid_code" {
		t.Errorf("recovery code reused: %d %v", r.Status, r.Body)
	}
	if r := signIn().do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": str(codes[1])}); r.Status != 200 {
		t.Errorf("second recovery code: %d %v", r.Status, r.Body)
	}
	var stored int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM user_recovery_codes r JOIN users u ON u.id = r.user_id WHERE u.email = $1 AND r.code_hash = $2`,
		normalized(email), []byte(code)).Scan(&stored)
	if stored != 0 {
		t.Error("recovery code stored in plain text")
	}
}

func TestPendingSessionsAreRefusedEverywhere(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.client()
	if r := admin.do("POST", "/api/v1/hub/setup", setupBody("setup-code-for-tests")); r.Body["two_factor"] != "enroll" {
		t.Fatalf("hub setup: %d %v", r.Status, r.Body)
	}
	member, _ := e.newPending()

	for name, c := range map[string]*client{"member": member, "hub admin": admin} {
		for _, path := range []string{"/api/v1/me", "/api/v1/admin/overview", "/api/v1/admin/users", "/ws/workspaces/018f0000-0000-7000-8000-000000000000"} {
			r := c.do("GET", path, nil)
			if r.Status != 401 || r.errCode() != "two_factor_required" || str(r.Body["error"].(map[string]any)["next"]) != "enroll" {
				t.Errorf("%s GET %s: %d %v", name, path, r.Status, r.Body)
			}
		}
		if r := c.do("POST", "/api/v1/orgs", map[string]any{"name": "x"}); r.Status != 401 {
			t.Errorf("%s POST /orgs: %d", name, r.Status)
		}
	}
	// Once through, the admin dashboard opens.
	e.finishSignIn(admin, "", map[string]any{"two_factor": "enroll"})
	if r := admin.do("GET", "/api/v1/admin/overview", nil); r.Status != 200 {
		t.Errorf("admin overview after enrolling: %d %v", r.Status, r.Body)
	}
	// Logout works while pending.
	if r := member.do("POST", "/api/v1/auth/logout", nil); r.Status != http.StatusNoContent {
		t.Errorf("logout while pending: %d", r.Status)
	}
	if r := member.do("GET", "/api/v1/auth/two-factor", nil); r.Status != 401 || r.errCode() != "unauthorized" {
		t.Errorf("session after logout: %d %v", r.Status, r.Body)
	}
}

func TestSignInPathsLandPending(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Paths Co")
	email := userEmail(me)
	org := currentOrg(me)

	// Login.
	c := e.client()
	c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "two_factor_required" {
		t.Errorf("login session is not pending: %d %v", r.Status, r.Body)
	}
	// Invite accept (first, because a reset signs out the owner).
	inv := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": uniqueEmail("guest"), "role": "member"})
	c3 := e.client()
	acc := c3.do("POST", "/api/v1/invites/"+tokenOf(inv)+"/accept", map[string]any{"name": "Gus", "password": "guest password"})
	if acc.Body["two_factor"] != "enroll" {
		t.Errorf("invite accept: %v", acc.Body)
	}
	// Password reset (existing person: verify, never skipped).
	c2 := e.client()
	c2.do("POST", "/api/v1/auth/forgot", map[string]any{"email": email})
	rr := c2.do("POST", "/api/v1/auth/reset", map[string]any{"token": resetTokenFrom(t, e.mail), "password": "brand new pass"})
	if rr.Body["two_factor"] != "verify" {
		t.Errorf("reset: %v", rr.Body)
	}
	for name, cl := range map[string]*client{"reset": c2, "invite": c3} {
		if r := cl.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "two_factor_required" {
			t.Errorf("%s session is not pending: %d %v", name, r.Status, r.Body)
		}
	}
}

// Sessions that exist when the migration runs have no two_factor_at, so they must enroll.
func TestSessionsFromBeforeTheMigrationArePending(t *testing.T) {
	e := newEnv(t)
	_, me := e.signup("Old Session Co")
	raw, hash := auth.NewToken()
	// The insert lists only the columns the old table had.
	if _, err := e.store.Pool.Exec(context.Background(),
		`INSERT INTO auth_sessions (id, user_id, token_hash, expires_at) VALUES (gen_random_uuid(), $1, $2, now() + interval '1 day')`,
		userID(me), hash); err != nil {
		t.Fatal(err)
	}
	old := e.client()
	old.http.Jar.SetCookies(mustURL(e.ts.URL), []*http.Cookie{{Name: auth.SessionCookie, Value: raw, Path: "/"}})
	r := old.do("GET", "/api/v1/me", nil)
	if r.Status != 401 || r.errCode() != "two_factor_required" {
		t.Fatalf("old session: %d %v", r.Status, r.Body)
	}
	// This person is already enrolled, so the old session just needs a code.
	if next := str(r.Body["error"].(map[string]any)["next"]); next != "verify" {
		t.Errorf("next = %q, want verify", next)
	}
}

func tokenOf(inv resp) string {
	l := str(inv.Body["link"])
	return l[len("http://canopy.test/invite/"):]
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
