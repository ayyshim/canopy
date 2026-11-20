// Package daemon runs a command in the background and finds it again later. A running copy
// holds a lock on a pid file; the lock goes away when the process exits, so a file left behind
// by a crash never blocks the next start.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxLogSize = 10 << 20

// RunningError says another process holds the lock.
type RunningError struct{ PID int }

func (e RunningError) Error() string { return fmt.Sprintf("already running (pid %d)", e.PID) }

// Lock takes the lock at path for this process and writes its pid, and the command line that
// started it, into the file. Call the returned function to let go.
func Lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, RunningError{PID: PID(path)}
	}
	args, _ := json.Marshal(os.Args)
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"+string(args)+"\n"), 0)
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// PID returns the pid of the process holding the lock at path, or 0 if none does.
func PID(path string) int {
	pid, _ := holder(path)
	return pid
}

// Args returns the command line of the process holding the lock at path, or nil.
func Args(path string) []string {
	_, args := holder(path)
	return args
}

func holder(path string) (int, []string) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer f.Close()
	// Taking the lock works only when nobody holds it.
	if syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, nil
	}
	b, _ := io.ReadAll(f)
	first, rest, _ := strings.Cut(string(b), "\n")
	pid, _ := strconv.Atoi(strings.TrimSpace(first))
	var args []string
	_ = json.Unmarshal([]byte(strings.TrimSpace(rest)), &args)
	return pid, args
}

// Start runs bin with args in its own session, away from this terminal, with its output added
// to logPath. The returned channel closes when the process exits.
func Start(bin string, args []string, logPath string) (*exec.Cmd, <-chan struct{}, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, nil, err
	}
	if st, err := os.Stat(logPath); err == nil && st.Size() > maxLogSize {
		_ = os.Rename(logPath, logPath+".old")
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	defer logFile.Close()
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	return cmd, exited, nil
}

// Stop asks the process holding the lock at path to stop and waits until it has. It returns
// the pid it stopped, or 0 if nothing was running.
func Stop(path string, wait time.Duration) (int, error) {
	pid := PID(path)
	if pid == 0 {
		return 0, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return pid, err
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if PID(path) == 0 {
			return pid, nil
		}
	}
	return pid, fmt.Errorf("pid %d is still stopping after %s", pid, wait)
}

// Tail returns the last n lines of a file, for showing why a start failed.
func Tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
