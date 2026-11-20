package update

import (
	"os"
	"path/filepath"
	"testing"
)

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
