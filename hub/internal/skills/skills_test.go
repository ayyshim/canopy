package skills

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRawURL(t *testing.T) {
	ok := map[string]string{
		"https://github.com/o/r/blob/main/skills/go/SKILL.md": "https://raw.githubusercontent.com/o/r/main/skills/go/SKILL.md",
		"  https://github.com/o/r/raw/v1/a.md ":               "https://raw.githubusercontent.com/o/r/v1/a.md",
		"https://www.github.com/o/r/blob/main/a.md":           "https://raw.githubusercontent.com/o/r/main/a.md",
		"https://raw.githubusercontent.com/o/r/main/a.md":     "https://raw.githubusercontent.com/o/r/main/a.md",
	}
	for in, want := range ok {
		if got, err := RawURL(in); err != nil || got != want {
			t.Errorf("RawURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "not a url", "http://github.com/o/r/blob/main/a.md", "https://github.com/o/r",
		"https://github.com/o/r/tree/main/skills", "https://example.com/o/r/blob/main/a.md", "https://raw.githubusercontent.com/o/r"} {
		if _, err := RawURL(in); err == nil {
			t.Errorf("RawURL(%q) succeeded", in)
		}
	}
}

// redirect sends every request to the stub server, whatever host it asked for.
type redirect struct {
	to   *url.URL
	seen *string
}

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	*r.seen = req.URL.String()
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestFetch(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/o/r/main/a.md":
			io.WriteString(w, "# Go\nWrite idiomatic Go.")
		case "/o/r/main/big.md":
			io.WriteString(w, strings.Repeat("x", MaxBytes+1))
		case "/o/r/main/bin.md":
			w.Write([]byte{0xff, 0xfe, 0})
		case "/o/r/main/empty.md":
			io.WriteString(w, " \n")
		case "/o/r/main/boom.md":
			w.WriteHeader(http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer stub.Close()
	u, _ := url.Parse(stub.URL)
	var seen string
	f := &Fetcher{Client: &http.Client{Transport: redirect{u, &seen}}}

	body, err := f.Fetch(context.Background(), "https://github.com/o/r/blob/main/a.md")
	if err != nil || body != "# Go\nWrite idiomatic Go." {
		t.Fatalf("Fetch = %q, %v", body, err)
	}
	if seen != "https://raw.githubusercontent.com/o/r/main/a.md" {
		t.Errorf("fetched %q, want the raw URL", seen)
	}
	for file, want := range map[string]string{"missing.md": "doesn’t exist", "big.md": "too long", "bin.md": "isn’t text",
		"empty.md": "empty", "boom.md": "502"} {
		_, err := f.Fetch(context.Background(), "https://github.com/o/r/blob/main/"+file)
		var se Error
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to mention %q", file, err, want)
		}
		if _, ok := err.(Error); !ok {
			t.Errorf("%s: %T isn’t a skills.Error: %v", file, err, se)
		}
	}
	if _, err := f.Fetch(context.Background(), "https://example.com/x.md"); err == nil {
		t.Error("fetched a non-GitHub link")
	}
}
