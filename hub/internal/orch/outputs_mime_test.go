package orch

import "testing"

func TestSniffMimeOnlyFixesFilesWithoutExtension(t *testing.T) {
	page := []byte("<!DOCTYPE html>\n<html><body>Hi</body></html>")
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	cases := []struct {
		name, mime, want string
		head             []byte
	}{
		{"landing", "application/octet-stream", "text/html; charset=utf-8", page},
		{"landing", "", "text/html; charset=utf-8", page},
		{"shots/home", "application/octet-stream", "image/png", png},
		{"landing", "text/plain; charset=utf-8", "text/html; charset=utf-8", page},
		{"notes", "application/octet-stream", "text/plain; charset=utf-8", []byte("just words")},
		{"blob", "application/octet-stream", "application/octet-stream", []byte{0, 1, 2, 3, 0xff}},
		{"page.txt", "text/plain", "text/plain", page},      // has an extension: left alone
		{"landing", "text/markdown", "text/markdown", page}, // a specific type is kept
		{"landing", "application/octet-stream", "application/octet-stream", nil},
	}
	for _, c := range cases {
		if got := sniffMime(c.name, c.mime, c.head); got != c.want {
			t.Errorf("sniffMime(%q, %q) = %q, want %q", c.name, c.mime, got, c.want)
		}
	}
}
