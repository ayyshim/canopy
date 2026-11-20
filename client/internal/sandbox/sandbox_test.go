package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"canopy/shared/protocol"
)

// M3: a member's sandbox can read only the workspace folder and its granted directories
// (tested by attempting outside reads).
func TestBwrapShowsOnlyMounts(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap not installed")
	}
	base := t.TempDir()
	folder := filepath.Join(base, "workspace-folder")
	granted := filepath.Join(base, "payments-api")
	readOnly := filepath.Join(base, "docs")
	secret := filepath.Join(base, "other-project")
	for _, d := range []string{folder, granted, readOnly, secret} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(folder, "plan.md"), []byte("plan"), 0o644)
	os.WriteFile(filepath.Join(granted, "main.go"), []byte("code"), 0o644)
	os.WriteFile(filepath.Join(readOnly, "guide.md"), []byte("guide"), 0o644)
	os.WriteFile(filepath.Join(secret, "keys.txt"), []byte("TOP SECRET"), 0o644)
	home, _ := os.UserHomeDir()

	outputs := filepath.Join(folder, "outputs", "task-1")
	spec := Spec{Kind: "bwrap", Cwd: folder, Mounts: []protocol.Mount{
		{Path: folder, Mode: "ro"},
		{Path: outputs, Mode: "rw"},
		{Path: granted, Mode: "rw"},
		{Path: readOnly, Mode: "ro"},
	}}
	script := strings.Join([]string{
		"cat plan.md", "echo",
		"cat " + filepath.Join(granted, "main.go"), "echo",
		"echo new > " + filepath.Join(granted, "written.txt") + " && echo wrote-granted",
		"echo out > " + filepath.Join(outputs, "shot.txt") + " && echo wrote-outputs",
		"(echo x > " + filepath.Join(readOnly, "nope.txt") + ") 2>/dev/null || echo readonly-refused",
		"(echo x > plan2.md) 2>/dev/null || echo folder-readonly",
		"test -e " + secret + " || echo secret-invisible",
		"test -e " + filepath.Join(home, ".ssh") + " || echo home-hidden",
	}, "; ")
	cmd, err := Command(context.Background(), spec, []string{"/bin/sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bwrap: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"plan", "code", "wrote-granted", "wrote-outputs", "readonly-refused", "folder-readonly", "secret-invisible", "home-hidden"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TOP SECRET") {
		t.Error("read a directory outside the grants")
	}
	if b, err := os.ReadFile(filepath.Join(granted, "written.txt")); err != nil || string(b) != "new\n" {
		t.Errorf("write to granted directory didn’t land: %q %v", b, err)
	}
}

func TestVisibleMapsMirrorSources(t *testing.T) {
	mounts := []protocol.Mount{{Path: "/home/alice/api", Source: "/var/canopy/mirror/x", Mode: "rw", Mirror: true}, {Path: "/w", Mode: "ro"}}
	if _, real, ok := Visible(mounts, "/home/alice/api/src/a.go"); !ok || real != "/var/canopy/mirror/x/src/a.go" {
		t.Errorf("mirror path: %v %s", ok, real)
	}
	if _, _, ok := Visible(mounts, "/home/alice/other"); ok {
		t.Error("path outside mounts visible")
	}
}
