package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockFindsTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.pid")
	if pid := PID(path); pid != 0 {
		t.Fatalf("PID before any lock = %d, want 0", pid)
	}
	release, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if pid := PID(path); pid != os.Getpid() {
		t.Errorf("PID while locked = %d, want %d", pid, os.Getpid())
	}
	var running RunningError
	if _, err := Lock(path); !errors.As(err, &running) || running.PID != os.Getpid() {
		t.Errorf("second Lock = %v, want RunningError with our pid", err)
	}
	release()
	if pid := PID(path); pid != 0 {
		t.Errorf("PID after release = %d, want 0", pid)
	}
}

func TestStopWithNothingRunning(t *testing.T) {
	pid, err := Stop(filepath.Join(t.TempDir(), "x.pid"), 0)
	if pid != 0 || err != nil {
		t.Errorf("Stop = %d, %v; want 0, nil", pid, err)
	}
}
