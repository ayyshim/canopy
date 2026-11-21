package api_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"canopy/hub/internal/auth"
)

func setupBody(token string) map[string]any {
	return map[string]any{
		"token": token,
		"hub":   map[string]any{"name": "Sageflick hub"},
		"admin": map[string]any{"name": "Hub Admin", "email": "admin@hub.example", "password": "admin password"},
	}
}

// The hub is set up once (name, providers, first hub admin) before organizations can register.
func TestHubSetupComesFirst(t *testing.T) {
	e := newFreshEnv(t)
	c := e.client()

	hub := c.do("GET", "/api/v1/hub", nil)
	if hub.Status != 200 || hub.Body["setup_required"] != true {
		t.Fatalf("GET /hub before setup: %d %v", hub.Status, hub.Body)
	}
	if r := c.do("POST", "/api/v1/auth/signup", signupBody("Too Early", "Early", uniqueEmail("early"))); r.Status != http.StatusConflict || r.errCode() != "setup_required" {
		t.Fatalf("signup before setup: %d %v", r.Status, r.Body)
	}
	if r := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "a@b.co", "password": "whatever1"}); r.errCode() != "setup_required" {
		t.Errorf("login before setup: %d %v", r.Status, r.Body)
	}

	if r := c.do("POST", "/api/v1/hub/setup", setupBody("wrong")); r.Status != http.StatusForbidden || r.errCode() != "bad_setup_token" {
		t.Fatalf("wrong token: %d %v", r.Status, r.Body)
	}
	bad := setupBody("setup-code-for-tests")
	bad["providers"] = map[string]any{"github": map[string]any{"enabled": true, "client_id": ""}}
	if r := c.do("POST", "/api/v1/hub/setup", bad); r.field("providers.github.client_id") == "" || r.field("providers.github.client_secret") == "" {
		t.Fatalf("enabled provider without credentials: %d %v", r.Status, r.Body)
	}

	r := c.do("POST", "/api/v1/hub/setup", setupBody("setup-code-for-tests"))
	if r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %v", r.Status, r.Body)
	}
	if r.Body["two_factor"] != "enroll" {
		t.Errorf("setup should start a pending session: %v", r.Body)
	}
	if got := c.do("GET", "/api/v1/me", nil); got.Status != 401 || got.errCode() != "two_factor_required" {
		t.Errorf("/me while pending: %d %v", got.Status, got.Body)
	}
	r.Body = e.finishSignIn(c, "", r.Body)
	user := r.Body["user"].(map[string]any)
	if user["is_hub_admin"] != true || r.Body["current_org_id"] != nil {
		t.Errorf("setup should create a hub admin with no organization: %v", r.Body)
	}
	if c.cookie(auth.SessionCookie) == nil {
		t.Error("setup did not sign the admin in")
	}
	if again := e.client().do("POST", "/api/v1/hub/setup", setupBody("setup-code-for-tests")); again.Status != http.StatusConflict || again.errCode() != "already_set_up" {
		t.Errorf("second setup: %d %v", again.Status, again.Body)
	}

	hub = c.do("GET", "/api/v1/hub", nil)
	if hub.Body["setup_required"] != false || str(hub.Body["name"]) != "Sageflick hub" || hub.Body["signup_open"] != true {
		t.Errorf("GET /hub after setup: %v", hub.Body)
	}
	if r := e.client().do("POST", "/api/v1/auth/signup", signupBody("Now Open", "Ok", uniqueEmail("ok"))); r.Status != http.StatusCreated {
		t.Errorf("signup after setup: %d %v", r.Status, r.Body)
	}
}

// hubAdmin sets up a fresh hub and returns its admin client.
func (e *env) hubAdmin() *client {
	e.t.Helper()
	c := e.client()
	r := c.do("POST", "/api/v1/hub/setup", setupBody("setup-code-for-tests"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("setup: %d %v", r.Status, r.Body)
	}
	e.finishSignIn(c, "", r.Body)
	return c
}

func TestHubAdminSeesEveryOrganizationAndUser(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.hubAdmin()
	alice, aliceMe := e.signup("Alpha Labs")
	_, _ = e.signup("Beta Works")
	alice.do("POST", "/api/v1/orgs", map[string]any{"name": "Alpha Two", "size": "just_me"})

	ov := admin.do("GET", "/api/v1/admin/overview", nil)
	if ov.Status != 200 {
		t.Fatalf("overview: %d %v", ov.Status, ov.Body)
	}
	totals := ov.Body["totals"].(map[string]any)
	if totals["organizations"].(float64) != 3 || totals["users"].(float64) != 3 || totals["hub_admins"].(float64) != 1 {
		t.Errorf("totals: %v", totals)
	}
	if len(ov.Body["recent_events"].([]any)) == 0 {
		t.Error("no hub events (setup should be logged)")
	}

	orgs := admin.do("GET", "/api/v1/admin/organizations", nil)
	if orgs.Body["total"].(float64) != 3 || len(orgs.Body["items"].([]any)) != 3 {
		t.Fatalf("orgs: %v", orgs.Body)
	}
	search := admin.do("GET", "/api/v1/admin/organizations?q=alpha", nil)
	if search.Body["total"].(float64) != 2 {
		t.Errorf("search alpha: %v", search.Body)
	}
	alpha := currentOrg(aliceMe)
	detail := admin.do("GET", "/api/v1/admin/organizations/"+alpha, nil)
	members := detail.Body["members"].([]any)
	if detail.Status != 200 || len(members) != 1 || str(members[0].(map[string]any)["role"]) != "owner" {
		t.Errorf("org detail: %d %v", detail.Status, detail.Body)
	}

	users := admin.do("GET", "/api/v1/admin/users?q="+url.QueryEscape(userEmail(aliceMe)), nil)
	items := users.Body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["organizations"].(float64) != 2 || items[0].(map[string]any)["last_active_at"] == nil {
		t.Errorf("user search: %v", users.Body)
	}

	// Regular users can't open the dashboard.
	for _, path := range []string{"/api/v1/admin/overview", "/api/v1/admin/organizations", "/api/v1/admin/users", "/api/v1/admin/settings"} {
		if r := alice.do("GET", path, nil); r.Status != http.StatusForbidden {
			t.Errorf("%s as non-admin: %d", path, r.Status)
		}
	}
	// The hub admin isn't a member of Alpha, so organization endpoints stay closed to them.
	if r := admin.do("GET", "/api/v1/orgs/"+alpha, nil); r.Status != http.StatusNotFound {
		t.Errorf("hub admin via org API: %d", r.Status)
	}
}

func TestHubAdminRolesKeepAtLeastOneAdmin(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.hubAdmin()
	adminID := str(admin.do("GET", "/api/v1/me", nil).Body["user"].(map[string]any)["id"])
	if r := admin.do("PATCH", "/api/v1/admin/users/"+adminID, map[string]any{"is_hub_admin": false}); r.Status != http.StatusConflict || r.errCode() != "last_hub_admin" {
		t.Fatalf("removing last admin: %d %v", r.Status, r.Body)
	}
	bob, bobMe := e.signup("Bob Co")
	bobID := str(bobMe["user"].(map[string]any)["id"])
	if r := admin.do("PATCH", "/api/v1/admin/users/"+bobID, map[string]any{"is_hub_admin": true}); r.Status != 200 {
		t.Fatalf("promote: %d %v", r.Status, r.Body)
	}
	if r := bob.do("GET", "/api/v1/admin/overview", nil); r.Status != 200 {
		t.Errorf("promoted user can't open dashboard: %d", r.Status)
	}
	if r := bob.do("PATCH", "/api/v1/admin/users/"+adminID, map[string]any{"is_hub_admin": false}); r.Status != 200 {
		t.Errorf("demote first admin: %d %v", r.Status, r.Body)
	}
	if r := admin.do("GET", "/api/v1/admin/overview", nil); r.Status != http.StatusForbidden {
		t.Errorf("demoted admin still has access: %d", r.Status)
	}
}

func TestClosedSignupsAndProviderSettings(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.hubAdmin()

	r := admin.do("PATCH", "/api/v1/admin/settings", map[string]any{
		"signup_open": false,
		"providers":   map[string]any{"github": map[string]any{"enabled": true, "client_id": "gh-id", "client_secret": "gh-secret"}},
	})
	if r.Status != 200 {
		t.Fatalf("settings: %d %v", r.Status, r.Body)
	}
	gh := r.Body["providers"].(map[string]any)["github"].(map[string]any)
	if gh["has_secret"] != true || gh["client_id"] != "gh-id" || !strings.HasSuffix(str(gh["callback_url"]), "/api/v1/auth/oauth/github/callback") {
		t.Errorf("github settings: %v", gh)
	}
	settings := admin.do("GET", "/api/v1/admin/settings", nil)
	if strings.Contains(strings.ToLower(toJSON(settings.Body)), "gh-secret") {
		t.Error("client secret returned by the API")
	}
	// An empty secret keeps the stored one.
	admin.do("PATCH", "/api/v1/admin/settings", map[string]any{"providers": map[string]any{"github": map[string]any{"enabled": true, "client_id": "gh-id-2"}}})
	var secret string
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT client_secret FROM auth_providers WHERE provider = 'github'`).Scan(&secret)
	if secret != "gh-secret" {
		t.Errorf("secret overwritten: %q", secret)
	}

	if hub := e.client().do("GET", "/api/v1/hub", nil); hub.Body["signup_open"] != false || hub.Body["providers"].(map[string]any)["github"] != true {
		t.Errorf("public hub view: %v", hub.Body)
	}
	if r := e.client().do("POST", "/api/v1/auth/signup", signupBody("Closed", "Nope", uniqueEmail("closed"))); r.Status != http.StatusForbidden || r.errCode() != "signups_closed" {
		t.Errorf("signup while closed: %d %v", r.Status, r.Body)
	}
}

type fakeOAuth struct{ profile auth.OAuthProfile }

func (f fakeOAuth) AuthCodeURL(state string) string {
	return "https://provider.example/authorize?state=" + state
}
func (f fakeOAuth) Exchange(context.Context, string) (auth.OAuthProfile, error) {
	return f.profile, nil
}

func TestOAuthSignInUsesStoredProvider(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.hubAdmin()
	_, me := e.signup("OAuth Co")
	email := userEmail(me)
	profile := auth.OAuthProfile{Subject: "gh-123", Email: email, EmailVerified: true, Name: "Riley"}
	var gotClientID string
	e.srv.NewOAuth = func(provider, clientID, secret, callback string) auth.OAuthProvider {
		gotClientID = clientID
		return fakeOAuth{profile: profile}
	}

	c := e.client()
	if r := c.do("GET", "/api/v1/auth/oauth/github/start", nil); r.Status != http.StatusNotFound {
		t.Fatalf("start while disabled: %d", r.Status)
	}
	admin.do("PATCH", "/api/v1/admin/settings", map[string]any{"providers": map[string]any{"github": map[string]any{"enabled": true, "client_id": "gh-id", "client_secret": "s"}}})

	start := c.do("GET", "/api/v1/auth/oauth/github/start", nil)
	loc := start.Header.Get("Location")
	if start.Status != http.StatusFound || !strings.HasPrefix(loc, "https://provider.example/authorize?state=") || gotClientID != "gh-id" {
		t.Fatalf("start: %d %s %s", start.Status, loc, gotClientID)
	}
	state := strings.TrimPrefix(loc, "https://provider.example/authorize?state=")

	// A forged state is refused.
	if r := e.client().do("GET", "/api/v1/auth/oauth/github/callback?code=x&state="+state, nil); !strings.Contains(r.Header.Get("Location"), "error=oauth_failed") {
		t.Errorf("callback without state cookie: %s", r.Header.Get("Location"))
	}
	cb := c.do("GET", "/api/v1/auth/oauth/github/callback?code=x&state="+url.QueryEscape(state), nil)
	if cb.Status != http.StatusFound || cb.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %s", cb.Status, cb.Header.Get("Location"))
	}
	if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "two_factor_required" {
		t.Fatalf("OAuth skipped the code: %d %v", r.Status, r.Body)
	}
	if r := c.do("GET", "/api/v1/auth/two-factor", nil); r.Status != 200 || r.Body["next"] != "verify" {
		t.Fatalf("two-factor state after OAuth: %d %v", r.Status, r.Body)
	}
	if me := e.finishSignIn(c, email, map[string]any{"two_factor": "verify"}); userEmail(me) != email {
		t.Fatalf("not signed in after OAuth and code: %v", me)
	}

	// Unknown emails are sent to create an organization instead.
	profile.Subject, profile.Email = "gh-999", uniqueEmail("stranger")
	c2 := e.client()
	start = c2.do("GET", "/api/v1/auth/oauth/github/start", nil)
	state = strings.TrimPrefix(start.Header.Get("Location"), "https://provider.example/authorize?state=")
	cb = c2.do("GET", "/api/v1/auth/oauth/github/callback?code=x&state="+url.QueryEscape(state), nil)
	if !strings.Contains(cb.Header.Get("Location"), "error=oauth_no_account") {
		t.Errorf("unknown email: %s", cb.Header.Get("Location"))
	}
}
