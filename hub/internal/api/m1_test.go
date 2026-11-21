package api_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"canopy/hub/internal/auth"
)

// M1: Sign-up creates organization, then user, owner membership and default limits in one transaction.
func TestSignupCreatesOrgOwnerMembershipAndLimits(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	email := uniqueEmail("Riley")
	r := c.do("POST", "/api/v1/auth/signup", signupBody("Acme Robotics", "Riley Park", email))
	if r.Status != http.StatusCreated {
		t.Fatalf("status %d %v", r.Status, r.Body)
	}
	r.Body = e.finishSignIn(c, email, r.Body)
	orgID := currentOrg(r.Body)
	if orgID == "" {
		t.Fatal("no current org after signup")
	}
	ctx := context.Background()
	var owner, role, slug string
	var sessionLimit, weeklyLimit int64
	err := e.store.Pool.QueryRow(ctx, `
		SELECT o.owner_user_id::text, m.org_role, o.slug, o.default_session_limit, o.default_weekly_limit
		FROM organizations o JOIN memberships m ON m.org_id = o.id AND m.user_id = o.owner_user_id
		WHERE o.id = $1`, orgID).Scan(&owner, &role, &slug, &sessionLimit, &weeklyLimit)
	if err != nil {
		t.Fatal(err)
	}
	me := r.Body["user"].(map[string]any)
	if owner != str(me["id"]) || role != "owner" {
		t.Errorf("owner=%s role=%s, want user %s as owner", owner, role, me["id"])
	}
	if str(me["email"]) != normalized(email) {
		t.Errorf("email not normalized: %v", me["email"])
	}
	if sessionLimit != 2_000_000 || weeklyLimit != 20_000_000 {
		t.Errorf("limits %d/%d", sessionLimit, weeklyLimit)
	}
	if slug != "acme-robotics" && len(slug) < len("acme-robotics") {
		t.Errorf("slug %q", slug)
	}
	if ck := c.cookie(auth.SessionCookie); ck == nil {
		t.Error("signup did not sign in")
	}
	if got := c.do("GET", "/api/v1/me", nil); got.Status != 200 || currentOrg(got.Body) != orgID {
		t.Errorf("/me after signup: %d %v", got.Status, got.Body)
	}
}

func normalized(s string) string {
	b := []byte(s)
	for i, ch := range b {
		if ch >= 'A' && ch <= 'Z' {
			b[i] = ch + 32
		}
	}
	return string(b)
}

func TestSignupIsAtomicUnderConcurrentDuplicateEmails(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail("race")
	orgName := "Race " + email
	var wg sync.WaitGroup
	statuses := make([]int, 6)
	clients := make([]*client, len(statuses))
	for i := range clients {
		clients[i] = e.client()
		clients[i].csrf()
	}
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = clients[i].do("POST", "/api/v1/auth/signup", signupBody(orgName, "Racer", email)).Status
		}()
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", s)
		}
	}
	if created != 1 {
		t.Fatalf("created %d accounts, want 1 (%v)", created, statuses)
	}
	var orgs int
	if err := e.store.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM organizations WHERE name = $1`, orgName).Scan(&orgs); err != nil {
		t.Fatal(err)
	}
	if orgs != 1 {
		t.Errorf("%d organizations left behind, want 1 — signup is not atomic", orgs)
	}
}

func TestSignupValidationAndDuplicateEmail(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	r := c.do("POST", "/api/v1/auth/signup", map[string]any{
		"org":  map[string]any{"name": " ", "size": "huge"},
		"user": map[string]any{"name": "", "email": "nope", "password": "short", "accept_terms": false},
	})
	if r.Status != http.StatusUnprocessableEntity || r.errCode() != "validation" {
		t.Fatalf("got %d %v", r.Status, r.Body)
	}
	for _, f := range []string{"org.name", "org.size", "user.name", "user.email", "user.password", "user.accept_terms"} {
		if r.field(f) == "" {
			t.Errorf("missing inline error for %s", f)
		}
	}

	_, me := e.signup("First")
	r = e.client().do("POST", "/api/v1/auth/signup", signupBody("Second", "Someone", userEmail(me)))
	if r.Status != http.StatusConflict || r.errCode() != "email_taken" || r.field("user.email") == "" {
		t.Fatalf("duplicate email: %d %v", r.Status, r.Body)
	}
}

// M1: Login rejects unknown emails and wrong passwords with distinct messages.
func TestLoginDistinctErrors(t *testing.T) {
	e := newEnv(t)
	_, me := e.signup("Login Co")
	email := userEmail(me)

	unknown := e.client().do("POST", "/api/v1/auth/login", map[string]any{"email": uniqueEmail("ghost"), "password": "whatever1"})
	wrong := e.client().do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "not the one"})
	if unknown.Status != 401 || unknown.errCode() != "unknown_email" || unknown.field("email") == "" {
		t.Errorf("unknown email: %d %v", unknown.Status, unknown.Body)
	}
	if wrong.Status != 401 || wrong.errCode() != "wrong_password" || wrong.field("password") == "" {
		t.Errorf("wrong password: %d %v", wrong.Status, wrong.Body)
	}
	if unknown.field("email") == wrong.field("password") {
		t.Error("messages are not distinct")
	}

	c := e.client()
	ok := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "  " + email + " ", "password": "correct horse"})
	if ok.Status != 200 || ok.Body["two_factor"] != "verify" {
		t.Fatalf("login: %d %v", ok.Status, ok.Body)
	}
	if currentOrg(e.finishSignIn(c, email, ok.Body)) == "" {
		t.Fatal("no current org after login")
	}
	ck := c.cookie(auth.SessionCookie)
	if ck == nil {
		t.Fatal("no session cookie")
	}
}

// M1: reset link works once within 30 minutes.
func TestPasswordResetWorksOnceWithin30Minutes(t *testing.T) {
	e := newEnv(t)
	old, me := e.signup("Reset Co")
	email := userEmail(me)

	c := e.client()
	if r := c.do("POST", "/api/v1/auth/forgot", map[string]any{"email": email}); r.Status != http.StatusAccepted {
		t.Fatalf("forgot: %d %v", r.Status, r.Body)
	}
	token := resetTokenFrom(t, e.mail)
	if r := c.do("GET", "/api/v1/auth/reset/"+token, nil); r.Status != 200 {
		t.Fatalf("check: %d %v", r.Status, r.Body)
	}
	r := c.do("POST", "/api/v1/auth/reset", map[string]any{"token": token, "password": "brand new pass"})
	if r.Status != 200 || c.cookie(auth.SessionCookie) == nil {
		t.Fatalf("reset: %d %v", r.Status, r.Body)
	}
	if r.Body["two_factor"] != "verify" {
		t.Errorf("reset must not skip the code: %v", r.Body)
	}
	// Single use.
	again := e.client().do("POST", "/api/v1/auth/reset", map[string]any{"token": token, "password": "another pass 2"})
	if again.Status != 400 || again.errCode() != "invalid_token" {
		t.Errorf("second use: %d %v", again.Status, again.Body)
	}
	if r := c.do("GET", "/api/v1/auth/reset/"+token, nil); r.Status != 400 {
		t.Errorf("check after use: %d", r.Status)
	}
	// Password changed: old sessions are revoked, old password fails, new works.
	if r := old.do("GET", "/api/v1/me", nil); r.Status != 401 {
		t.Errorf("old session still valid after reset: %d", r.Status)
	}
	if r := e.client().do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"}); r.errCode() != "wrong_password" {
		t.Errorf("old password: %d %v", r.Status, r.Body)
	}
	e.login(e.client(), email, "brand new pass")
}

func TestPasswordResetExpiresAfter30Minutes(t *testing.T) {
	e := newEnv(t)
	_, me := e.signup("Expiry Co")
	e.srv.Now = func() time.Time { return time.Now().Add(-31 * time.Minute) } // link issued 31 minutes ago
	c := e.client()
	c.do("POST", "/api/v1/auth/forgot", map[string]any{"email": userEmail(me)})
	e.srv.Now = time.Now
	token := resetTokenFrom(t, e.mail)
	r := c.do("POST", "/api/v1/auth/reset", map[string]any{"token": token, "password": "brand new pass"})
	if r.Status != 400 || r.errCode() != "invalid_token" {
		t.Fatalf("expired link accepted: %d %v", r.Status, r.Body)
	}

	// 29 minutes old still works.
	e.srv.Now = func() time.Time { return time.Now().Add(-29 * time.Minute) }
	c.do("POST", "/api/v1/auth/forgot", map[string]any{"email": userEmail(me)})
	e.srv.Now = time.Now
	if r := c.do("POST", "/api/v1/auth/reset", map[string]any{"token": resetTokenFrom(t, e.mail), "password": "brand new pass"}); r.Status != 200 {
		t.Fatalf("29-minute link rejected: %d %v", r.Status, r.Body)
	}
}

func TestForgotDoesNotRevealAccounts(t *testing.T) {
	e := newEnv(t)
	r := e.client().do("POST", "/api/v1/auth/forgot", map[string]any{"email": uniqueEmail("nobody")})
	if r.Status != http.StatusAccepted {
		t.Fatalf("got %d", r.Status)
	}
	if _, ok := e.mail.Last(); ok {
		t.Error("sent mail for an unknown address")
	}
}

// M1: A user in two organizations can switch between them; data never leaks across organizations.
func TestOrgSwitchingAndNoCrossOrgLeaks(t *testing.T) {
	e := newEnv(t)
	alice, me := e.signup("Alpha")
	alpha := currentOrg(me)

	r := alice.do("POST", "/api/v1/orgs", map[string]any{"name": "Beta", "size": "just_me"})
	if r.Status != http.StatusCreated {
		t.Fatalf("create org: %d %v", r.Status, r.Body)
	}
	beta := str(r.Body["organization"].(map[string]any)["id"])

	me2 := alice.do("GET", "/api/v1/me", nil).Body
	if n := len(me2["organizations"].([]any)); n != 2 {
		t.Fatalf("want 2 orgs, got %d", n)
	}
	if currentOrg(me2) != beta {
		t.Errorf("new org should become current")
	}
	if r := alice.do("PATCH", "/api/v1/me", map[string]any{"last_org_id": alpha}); r.Status != 200 || currentOrg(r.Body) != alpha {
		t.Errorf("switch: %d %v", r.Status, r.Body)
	}
	// Switching survives a fresh sign-in.
	again := e.login(e.client(), userEmail(me), "correct horse")
	if currentOrg(again) != alpha {
		t.Errorf("last org not restored on sign-in")
	}

	// Another organization's user sees nothing of Alpha.
	bob, bobMe := e.signup("Gamma")
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/orgs/" + alpha, nil},
		{"PATCH", "/api/v1/orgs/" + alpha, map[string]any{"name": "Pwned"}},
		{"PATCH", "/api/v1/me", map[string]any{"last_org_id": alpha}},
		{"GET", "/api/v1/orgs/not-a-uuid", nil},
	} {
		if r := bob.do(tc.method, tc.path, tc.body); r.Status != http.StatusNotFound {
			t.Errorf("%s %s as outsider: %d %v", tc.method, tc.path, r.Status, r.Body)
		}
	}
	for _, o := range bob.do("GET", "/api/v1/me", nil).Body["organizations"].([]any) {
		if id := str(o.(map[string]any)["id"]); id == alpha || id == beta {
			t.Errorf("outsider lists org %s", id)
		}
	}
	if got := alice.do("GET", "/api/v1/orgs/"+alpha, nil); str(got.Body["organization"].(map[string]any)["name"]) != "Alpha" {
		t.Errorf("Alpha renamed by outsider: %v", got.Body)
	}
	_ = bobMe
}

func TestOnlyAdminsChangeOrgSettings(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Settings Co")
	org := currentOrg(me)
	member, memberMe := e.signup("Member Home")
	// Add the second user to the first org as a plain member.
	if _, err := e.store.Pool.Exec(context.Background(),
		`INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'member')`,
		org, str(memberMe["user"].(map[string]any)["id"])); err != nil {
		t.Fatal(err)
	}
	if r := member.do("GET", "/api/v1/orgs/"+org, nil); r.Status != 200 || str(r.Body["role"]) != "member" {
		t.Fatalf("member read: %d %v", r.Status, r.Body)
	}
	if r := member.do("PATCH", "/api/v1/orgs/"+org, map[string]any{"name": "Nope"}); r.Status != http.StatusForbidden {
		t.Errorf("member patch: %d %v", r.Status, r.Body)
	}
	r := owner.do("PATCH", "/api/v1/orgs/"+org, map[string]any{"name": "Renamed", "default_weekly_limit": 5, "timezone": "Asia/Kathmandu"})
	if r.Status != 200 {
		t.Fatalf("owner patch: %d %v", r.Status, r.Body)
	}
	o := r.Body["organization"].(map[string]any)
	if str(o["name"]) != "Renamed" || o["default_weekly_limit"].(float64) != 5 || str(o["timezone"]) != "Asia/Kathmandu" {
		t.Errorf("not updated: %v", o)
	}
	if r := owner.do("PATCH", "/api/v1/orgs/"+org, map[string]any{"default_session_limit": 0, "timezone": "Mars/Base"}); r.field("default_session_limit") == "" || r.field("timezone") == "" {
		t.Errorf("bad values accepted: %d %v", r.Status, r.Body)
	}
	var audits int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE org_id = $1 AND type = 'organization.updated'`, org).Scan(&audits)
	if audits != 1 {
		t.Errorf("audit events: %d", audits)
	}
}

func TestLogoutEndsSession(t *testing.T) {
	e := newEnv(t)
	c, _ := e.signup("Logout Co")
	if r := c.do("POST", "/api/v1/auth/logout", nil); r.Status != http.StatusNoContent {
		t.Fatalf("logout: %d", r.Status)
	}
	if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 {
		t.Errorf("/me after logout: %d", r.Status)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	a, me := e.signup("Pw Co")
	b := e.client()
	b.do("POST", "/api/v1/auth/login", map[string]any{"email": userEmail(me), "password": "correct horse"})

	if r := a.do("PATCH", "/api/v1/me", map[string]any{"current_password": "wrong", "new_password": "next password"}); r.field("current_password") == "" {
		t.Fatalf("wrong current password accepted: %d %v", r.Status, r.Body)
	}
	if r := a.do("PATCH", "/api/v1/me", map[string]any{"current_password": "correct horse", "new_password": "next password"}); r.Status != 200 {
		t.Fatalf("change: %d %v", r.Status, r.Body)
	}
	if r := a.do("GET", "/api/v1/me", nil); r.Status != 200 {
		t.Errorf("current session revoked: %d", r.Status)
	}
	if r := b.do("GET", "/api/v1/me", nil); r.Status != 401 {
		t.Errorf("other session survived password change: %d", r.Status)
	}
}

func TestCSRFRequiredForUnsafeRequests(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	r := c.raw("POST", "/api/v1/auth/login", map[string]any{"email": "a@b.co", "password": "x"}, false)
	if r.Status != http.StatusForbidden || r.errCode() != "csrf" {
		t.Fatalf("no token: %d %v", r.Status, r.Body)
	}
}

func TestSessionCookieIsHTTPOnly(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	r := c.do("POST", "/api/v1/auth/signup", signupBody("Cookie Co", "Cookie", uniqueEmail("cookie")))
	var found bool
	for _, line := range r.Header.Values("Set-Cookie") {
		if len(line) > len(auth.SessionCookie) && line[:len(auth.SessionCookie)] == auth.SessionCookie {
			found = true
			if !contains(line, "HttpOnly") || !contains(line, "SameSite=Lax") || !contains(line, "Max-Age=2592000") {
				t.Errorf("cookie attributes: %s", line)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie set")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAuthRateLimitPerIP(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	var last resp
	for range 11 {
		last = c.do("POST", "/api/v1/auth/login", map[string]any{"email": "nobody@example.com", "password": "whatever1"})
	}
	if last.Status != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Fatalf("11th attempt: %d %v", last.Status, last.Body)
	}
	if r := e.client().do("POST", "/api/v1/auth/login", map[string]any{"email": "nobody@example.com", "password": "whatever1"}); r.Status == http.StatusTooManyRequests {
		t.Error("limit leaked to another IP")
	}
}
