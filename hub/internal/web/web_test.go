package web

import (
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
