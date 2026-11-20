package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestTag(t *testing.T) {
	t.Setenv("CANOPY_REPO", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ayyshim/canopy/releases/latest" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, githubURL+"/ayyshim/canopy/releases/tag/v0.4.3", http.StatusFound)
	}))
	defer srv.Close()
	old := githubURL
	githubURL = srv.URL
	defer func() { githubURL = old }()

	tag, err := LatestTag(context.Background())
	if err != nil || tag != "v0.4.3" {
		t.Fatalf("LatestTag() = %q, %v; want v0.4.3", tag, err)
	}

	t.Setenv("CANOPY_REPO", "someone/missing")
	if tag, err := LatestTag(context.Background()); err == nil {
		t.Errorf("LatestTag() for a missing repository = %q, want an error", tag)
	}
}

func TestLatestTagWithoutReleases(t *testing.T) {
	t.Setenv("CANOPY_REPO", "someone/empty")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, githubURL+"/someone/empty/releases", http.StatusFound)
	}))
	defer srv.Close()
	old := githubURL
	githubURL = srv.URL
	defer func() { githubURL = old }()

	if tag, err := LatestTag(context.Background()); err == nil {
		t.Errorf("LatestTag() for a repository without releases = %q, want an error", tag)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		{"v1.0.0", "0.9.9", true},
		{"v0.1.10", "0.1.9", true},
		{"v0.2.0", "0.2.0-rc.1", true},
		{"v0.2.0-rc.1", "0.2.0", false},
		{"v0.2.0", "0.1.0-dev", true},
		{"v0.2.0", "c2484f0", false}, // a development build: no version to compare
		{"v0.1.0", "v0.1.0-17-gabc123", false},
		{"v0.2.0", "v0.1.0-17-gabc123", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestReplace(t *testing.T) {
	target := filepath.Join(t.TempDir(), "canopy")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(target, []byte("not a program")); err == nil {
		t.Error("Replace took a file that isn’t a program")
	}
	elf := append([]byte("\x7fELF"), make([]byte, 64)...)
	if err := Replace(target, elf); err != nil {
		t.Fatal(err)
	}
	if sum, _ := FileSum(target); sum != Sum(elf) {
		t.Error("target wasn’t replaced")
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", st.Mode().Perm())
	}
}
