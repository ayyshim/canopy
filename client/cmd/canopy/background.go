package main

// `canopy run` starts a copy of itself with --foreground, detached from the terminal, and
// returns. The running copy locks canopy.pid so only one runs at a time, and `canopy stop`
// reads that file to find it.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"canopy/client/internal/config"
)

const maxLogSize = 10 << 20

func pidPath() string { return filepath.Join(config.DataDir(), "canopy.pid") }
func logPath() string { return filepath.Join(config.DataDir(), "canopy.log") }

// startBackground starts `canopy run --foreground args...` in its own session, with its
// output going to canopy.log, and waits a moment to make sure it didn't stop right away.
func startBackground(args []string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(logPath()); err == nil && st.Size() > maxLogSize {
		_ = os.Rename(logPath(), logPath()+".old")
	}
	logFile, err := os.OpenFile(logPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(bin, append([]string{"run", "--foreground"}, args...)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	select {
	case <-exited:
		return fmt.Errorf("canopy stopped right after starting. See the log: %s", logPath())
	case <-time.After(2 * time.Second):
	}
	fmt.Printf("Canopy is running in the background (pid %d).\nLogs: %s\nStop it with: canopy stop\n", cmd.Process.Pid, logPath())
	return nil
}

// holdPIDFile locks canopy.pid for this process and writes its pid into it. The lock goes
// away when the process exits, so a file left behind by a crash never blocks the next start.
func holdPIDFile() (*os.File, error) {
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(pidPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("canopy is already running (pid %d). Stop it with: canopy stop", runningPID())
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return f, nil
}

// runningPID returns the pid of the canopy that is running on this machine, or 0.
func runningPID() int {
	f, err := os.Open(pidPath())
	if err != nil {
		return 0
	}
	defer f.Close()
	// Taking the lock works only when no running canopy holds it.
	if syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0
	}
	b, _ := io.ReadAll(f)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

func cmdStop() error {
	pid := runningPID()
	if pid == 0 {
		fmt.Println("Canopy isn’t running.")
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	for range 150 {
		if runningPID() == 0 {
			fmt.Println("Stopped canopy.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("canopy (pid %d) is still stopping after 15 seconds", pid)
}
