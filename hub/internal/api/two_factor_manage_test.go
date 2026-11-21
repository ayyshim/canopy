package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"canopy/hub/internal/api"
)

func (e *env) auditCount(typ string) int {
	var n int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE type = $1`, typ).Scan(&n)
	return n
}

func TestMeShowsTwoFactorAndRecoveryCodesLeft(t *testing.T) {
	e := newEnv(t)
	c, _ := e.signup("Me Co")
	tf := c.do("GET", "/api/v1/me", nil).Body["two_factor"].(map[string]any)
	if tf["enabled_at"] == nil || tf["recovery_codes_left"] != float64(10) {
		t.Errorf("two_factor: %v", tf)
	}
	if _, has := toMap(c.do("GET", "/api/v1/me", nil).Body)["totp_secret"]; has {
		t.Error("secret in /me")
	}
}

func toMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func TestNewRecoveryCodesReplaceTheOldOnes(t *testing.T) {
	e := newEnv(t)
	c, me := e.signup("Renew Co")
	email := userEmail(me)
	old := e.recoveryCodes(c, email)

	if r := c.do("POST", "/api/v1/me/two-factor/recovery-codes", map[string]any{"code": "000000"}); r.Status != 422 || r.errCode() != "invalid_code" {
		t.Errorf("wrong code: %d %v", r.Status, r.Body)
	}
	r := c.do("POST", "/api/v1/me/two-factor/recovery-codes", map[string]any{"code": e.freshCode(email)})
	codes, _ := r.Body["recovery_codes"].([]any)
	if r.Status != 200 || len(codes) != 10 {
		t.Fatalf("new codes: %d %v", r.Status, r.Body)
	}
	if got := c.do("GET", "/api/v1/me", nil).Body["two_factor"].(map[string]any)["recovery_codes_left"]; got != float64(10) {
		t.Errorf("left: %v", got)
	}
	signIn := func() *client {
		c := e.client()
		c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
		return c
	}
	if r := signIn().do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": old[0]}); r.Status != 422 {
		t.Errorf("old recovery code still works: %d %v", r.Status, r.Body)
	}
	if r := signIn().do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": str(codes[0])}); r.Status != 200 {
		t.Errorf("new recovery code: %d %v", r.Status, r.Body)
	}
	if e.auditCount("user.recovery_codes_renewed") == 0 {
		t.Error("no audit event for new codes")
	}
}

// recoveryCodes makes a fresh set and returns it.
func (e *env) recoveryCodes(c *client, email string) []string {
	e.t.Helper()
	r := c.do("POST", "/api/v1/me/two-factor/recovery-codes", map[string]any{"code": e.freshCode(email)})
	var out []string
	for _, v := range r.Body["recovery_codes"].([]any) {
		out = append(out, str(v))
	}
	return out
}

func TestRecoveryCodeUseSendsEmail(t *testing.T) {
	e := newEnv(t)
	c, me := e.signup("Warn Co")
	email := userEmail(me)
	codes := e.recoveryCodes(c, email)
	c2 := e.client()
	c2.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	if r := c2.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"recovery_code": codes[0]}); r.Status != 200 {
		t.Fatalf("recovery: %d %v", r.Status, r.Body)
	}
	msg, ok := e.mail.Last()
	if !ok || msg.To != normalized(email) || !strings.Contains(msg.Subject, "recovery code") || !strings.Contains(msg.Text, "9 left") {
		t.Errorf("mail: %+v", msg)
	}
	if e.auditCount("user.recovery_code_used") == 0 {
		t.Error("no audit event")
	}
	// An authenticator code sends no such email.
	n := len(e.mail.Sent)
	c3 := e.client()
	c3.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	c3.do("POST", "/api/v1/auth/two-factor/verify", map[string]any{"code": e.freshCode(email)})
	if len(e.mail.Sent) != n {
		t.Error("email sent for an authenticator code")
	}
}

func TestChangeAuthenticatorSendsThisSessionBackToEnroll(t *testing.T) {
	e := newEnv(t)
	c, me := e.signup("Change Co")
	email := userEmail(me)
	other := e.client()
	e.login(other, email, "correct horse")
	oldSecret := e.secretOf(email)

	if r := c.do("POST", "/api/v1/me/two-factor/change", map[string]any{"code": "000000"}); r.Status != 422 {
		t.Errorf("wrong code: %d", r.Status)
	}
	if r := c.do("POST", "/api/v1/me/two-factor/change", map[string]any{"code": e.freshCode(email)}); r.Status != 200 || r.Body["next"] != "enroll" {
		t.Fatalf("change: %d %v", r.Status, r.Body)
	}
	if r := other.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "unauthorized" {
		t.Errorf("other session survived: %d %v", r.Status, r.Body)
	}
	if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "two_factor_required" || str(r.Body["error"].(map[string]any)["next"]) != "enroll" {
		t.Errorf("this session: %d %v", r.Status, r.Body)
	}
	e.finishSignIn(c, email, map[string]any{"two_factor": "enroll"})
	if e.secretOf(email) == oldSecret {
		t.Error("secret not replaced")
	}
	tf := c.do("GET", "/api/v1/me", nil).Body["two_factor"].(map[string]any)
	if tf["recovery_codes_left"] != float64(10) {
		t.Errorf("new enrollment codes: %v", tf)
	}
}

func TestHubAdminResetsTwoFactor(t *testing.T) {
	e := newFreshEnv(t)
	admin := e.hubAdmin()
	member := e.client()
	email := uniqueEmail("locked")
	r := member.do("POST", "/api/v1/auth/signup", signupBody("Locked Co", "Lou", email))
	me := e.finishSignIn(member, email, r.Body)
	uid := userID(me)
	second := e.client()
	e.login(second, email, "correct horse")

	// Not a hub admin: 404, and nothing changes.
	bystander, _ := e.signup("Bystander Co")
	if r := bystander.do("POST", "/api/v1/admin/users/"+uid+"/two-factor/reset", nil); r.Status != http.StatusNotFound {
		t.Errorf("non-admin reset: %d %v", r.Status, r.Body)
	}
	if r := member.do("GET", "/api/v1/me", nil); r.Status != 200 {
		t.Errorf("session ended by a non-admin: %d", r.Status)
	}
	if r := admin.do("POST", "/api/v1/admin/users/"+uuidZero+"/two-factor/reset", nil); r.Status != http.StatusNotFound {
		t.Errorf("unknown user: %d", r.Status)
	}

	before := len(e.mail.Sent)
	if r := admin.do("POST", "/api/v1/admin/users/"+uid+"/two-factor/reset", nil); r.Status != http.StatusNoContent {
		t.Fatalf("reset: %d %v", r.Status, r.Body)
	}
	for name, c := range map[string]*client{"first": member, "second": second} {
		if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 || r.errCode() != "unauthorized" {
			t.Errorf("%s session not ended: %d %v", name, r.Status, r.Body)
		}
	}
	msg, _ := e.mail.Last()
	if len(e.mail.Sent) != before+1 || msg.To != normalized(email) || !strings.Contains(msg.Subject, "two-factor") || !strings.Contains(msg.Text, "Hub Admin") {
		t.Errorf("mail: %+v", msg)
	}
	var codes int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, uid).Scan(&codes)
	if codes != 0 || e.auditCount("user.two_factor_reset") == 0 {
		t.Errorf("codes left %d, audit %d", codes, e.auditCount("user.two_factor_reset"))
	}
	// Next sign-in enrolls again, with a new secret.
	again := e.client()
	li := again.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse"})
	if li.Body["two_factor"] != "enroll" {
		t.Fatalf("login after reset: %v", li.Body)
	}
	e.finishSignIn(again, email, li.Body)
	if r := again.do("GET", "/api/v1/me", nil); r.Status != 200 {
		t.Errorf("after re-enrolling: %d", r.Status)
	}
}

func TestResetTwoFactorByEmailForTheCommand(t *testing.T) {
	e := newEnv(t)
	c, me := e.signup("Cli Co")
	email := userEmail(me)
	ctx := context.Background()
	if err := api.ResetTwoFactorByEmail(ctx, e.store, e.mail, "  "+strings.ToUpper(email)); err != nil {
		t.Fatal(err)
	}
	if r := c.do("GET", "/api/v1/me", nil); r.Status != 401 {
		t.Errorf("session survived: %d", r.Status)
	}
	if msg, _ := e.mail.Last(); msg.To != normalized(email) || !strings.Contains(msg.Text, "server") {
		t.Errorf("mail: %+v", msg)
	}
	if err := api.ResetTwoFactorByEmail(ctx, e.store, e.mail, "nobody@example.com"); err == nil {
		t.Error("unknown email accepted")
	}
}

const uuidZero = "018f0000-0000-7000-8000-000000000000"
