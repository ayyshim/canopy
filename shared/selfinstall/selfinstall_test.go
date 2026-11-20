package selfinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAddsFolderToPathOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin")
	bin := filepath.Join(home, ".local", "bin")

	for range 2 {
		if err := Run("canopy", []string{"--dir", bin}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(filepath.Join(bin, "canopy"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", st.Mode().Perm())
	}
	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if n := strings.Count(string(rc), `export PATH="$HOME/.local/bin:$PATH"`); n != 1 {
		t.Errorf(".zshrc has the PATH line %d times, want once:\n%s", n, rc)
	}
}

func TestInstallLeavesShellAloneWhenOnPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	bin := filepath.Join(home, "bin")
	t.Setenv("PATH", "/usr/bin:"+bin)

	if err := Run("canopy-hub", []string{"--dir", bin}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bin, "canopy-hub")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".bashrc", ".bash_profile", ".profile"} {
		if _, err := os.Stat(filepath.Join(home, f)); err == nil {
			t.Errorf("%s was written, but the folder was already on PATH", f)
		}
	}
}

func TestFishUsesFishAddPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	file, _, err := addToPath("/opt/canopy/bin")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), `fish_add_path "/opt/canopy/bin"`) {
		t.Errorf("%s:\n%s", file, b)
	}
}
