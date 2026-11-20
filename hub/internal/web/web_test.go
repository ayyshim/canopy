package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestResolve(t *testing.T) {
	files := fstest.MapFS{
		"index.html":                 {Data: []byte("home")},
		"login.html":                 {Data: []byte("login")},
		"reset/_.html":               {Data: []byte("reset")},
		"reset/_.txt":                {Data: []byte("payload")},
		"admin/organizations/_.html": {Data: []byte("org")},
		"admin/organizations.html":   {Data: []byte("orgs")},
		"_next/static/chunks/app.js": {Data: []byte("js")},
	}
	for in, want := range map[string]string{
		"/": "index.html", "/login": "login.html", "/reset/abc123": "reset/_.html", "/reset/abc123.txt": "reset/_.txt",
		"/admin/organizations": "admin/organizations.html", "/admin/organizations/0193-uuid": "admin/organizations/_.html",
		"/_next/static/chunks/app.js": "_next/static/chunks/app.js",
	} {
		if got, ok := resolve(files, in); !ok || got != want {
			t.Errorf("resolve(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := resolve(files, "/nope"); ok {
		t.Error("resolved a missing page")
	}
}

func TestServeSiteHasNoRedirectLoop(t *testing.T) {
	h := serveSite(fstest.MapFS{
		"index.html":        {Data: []byte("home")},
		"board.html":        {Data: []byte("board")},
		"docs/index.html":   {Data: []byte("docs")},
		"404.html":          {Data: []byte("missing")},
		"_next/static/a.js": {Data: []byte("js")},
	})
	for path, want := range map[string]string{"/": "home", "/board": "board", "/docs/": "docs", "/docs": "docs", "/_next/static/a.js": "js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("GET %s = %d %q (Location %q), want 200 %q", path, rec.Code, rec.Body.String(), rec.Header().Get("Location"), want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "missing" {
		t.Errorf("GET /nope = %d %q, want 404 with the 404 page", rec.Code, rec.Body.String())
	}
}
