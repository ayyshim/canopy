package auth

import (
	"crypto/subtle"
	"net/http"

	"canopy/hub/internal/httpx"
)

// Double-submit CSRF protection: the browser reads the canopy_csrf cookie and echoes it in the
// X-CSRF-Token header on every unsafe request. Another origin can't read the cookie.
const (
	CSRFCookie = "canopy_csrf"
	CSRFHeader = "X-CSRF-Token"
)

var errCSRF = httpx.NewError(http.StatusForbidden, "csrf", "Your session expired. Reload the page and try again.")

type CSRF struct{ Secure bool }

// Token returns the request's CSRF token, issuing a cookie when there is none.
func (c CSRF) Token(w http.ResponseWriter, r *http.Request) string {
	if ck, err := r.Cookie(CSRFCookie); err == nil && len(ck.Value) >= 32 {
		return ck.Value
	}
	raw, _ := NewToken()
	http.SetCookie(w, &http.Cookie{
		Name: CSRFCookie, Value: raw, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: false, Secure: c.Secure, SameSite: http.SameSiteLaxMode,
	})
	return raw
}

// Exempt paths are called by programs, not browsers, and never use the session cookie.
// The act route is for service workers: it has no CSRF cookie to echo, but it needs the
// person's session cookie (SameSite=Lax) and a secret one-use token.
var csrfExempt = map[string]bool{"/api/v1/runners/pair": true, "/api/v1/notifications/act": true}

func (c CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if csrfExempt[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		ck, err := r.Cookie(CSRFCookie)
		header := r.Header.Get(CSRFHeader)
		if err != nil || ck.Value == "" || header == "" ||
			subtle.ConstantTimeCompare([]byte(ck.Value), []byte(header)) != 1 {
			httpx.Fail(w, r, errCSRF)
			return
		}
		next.ServeHTTP(w, r)
	})
}
