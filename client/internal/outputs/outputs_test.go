package outputs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	d := filepath.Join("a", "out")
	if n, ok := Name(d, filepath.Join(d, "assets", "app.css")); !ok || n != "assets/app.css" {
		t.Fatalf("got %q %v", n, ok)
	}
	if _, ok := Name(d, filepath.Join("a", "other.txt")); ok {
		t.Fatal("outside path accepted")
	}
}

func TestWatchSubfolders(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	got := map[string]bool{}
	w, err := Watch(dir, func(p string) { mu.Lock(); got[p] = true; mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	sub := filepath.Join(dir, "assets", "img")
	os.MkdirAll(sub, 0o755)
	time.Sleep(200 * time.Millisecond)
	f1 := filepath.Join(sub, "a.svg")
	os.WriteFile(f1, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	time.Sleep(2500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !got[f1] || !got[filepath.Join(dir, "index.html")] {
		t.Fatalf("missing files: %v", got)
	}
}
