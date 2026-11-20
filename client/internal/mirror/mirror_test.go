package mirror

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanHonorsIgnoresAndSecrets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.go", "package main")
	write(t, root, "docs/readme.md", "# hi")
	write(t, root, ".gitignore", "generated/\n*.tmp\n")
	write(t, root, "generated/out.go", "x")
	write(t, root, "notes.tmp", "x")
	write(t, root, "node_modules/lib/index.js", "x")
	write(t, root, ".env", "SECRET=1")
	write(t, root, ".env.example", "SECRET=")
	write(t, root, "certs/server.pem", "x")
	write(t, root, "web/.dockerignore", "cache/\n")
	write(t, root, "web/cache/a.txt", "x")
	write(t, root, "web/app.js", "x")
	write(t, root, ".canopyignore", "private/\n")
	write(t, root, "private/plan.md", "x")
	files, _, err := scan(root, &hasher{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.go", "docs/readme.md", ".gitignore", ".env.example", "web/app.js", "web/.dockerignore", ".canopyignore"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	for _, skip := range []string{"generated/out.go", "notes.tmp", "node_modules/lib/index.js", ".env", "certs/server.pem", "web/cache/a.txt", "private/plan.md"} {
		if _, ok := files[skip]; ok {
			t.Errorf("mirrored %s", skip)
		}
	}
}

func TestDiffAndSafeJoin(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", "1")
	write(t, root, "b.txt", "2")
	h := &hasher{}
	before, _, _ := scan(root, h)
	write(t, root, "a.txt", "changed")
	os.Remove(filepath.Join(root, "b.txt"))
	write(t, root, "c.txt", "new")
	after, _, _ := scan(root, h)
	got := map[string]bool{}
	for _, f := range diff(before, after) {
		got[f.Path] = f.Deleted
	}
	if len(got) != 3 || got["a.txt"] || !got["b.txt"] || got["c.txt"] {
		t.Errorf("diff: %v", got)
	}
	if _, ok := safeJoin(root, "../escape"); ok {
		t.Error("allowed ../ escape")
	}
	if _, ok := safeJoin(root, "sub/ok.txt"); !ok {
		t.Error("refused a normal path")
	}
}
