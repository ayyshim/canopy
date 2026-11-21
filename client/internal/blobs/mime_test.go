package blobs

import (
	"strings"
	"testing"
)

func TestMimeOfWeb(t *testing.T) {
	want := map[string]string{
		"a.html": "text/html", "a.htm": "text/html", "a.css": "text/css", "a.js": "text/javascript",
		"a.mjs": "text/javascript", "a.svg": "image/svg+xml", "a.json": "application/json",
		"a.woff2": "font/woff2", "a.png": "image/png", "a.jpg": "image/jpeg", "a.jpeg": "image/jpeg",
		"a.gif": "image/gif", "a.webp": "image/webp", "a.ico": "image/", "a.woff": "font/woff",
	}
	for f, w := range want {
		if got := MimeOf(f); !strings.HasPrefix(got, w) {
			t.Errorf("%s: got %q want %q", f, got, w)
		}
	}
}
