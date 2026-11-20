package main

// `canopy run` starts a copy of itself with --foreground, detached from the terminal, and
// returns. The running copy locks canopy.pid so only one runs at a time, and `canopy stop`
// reads that file to find it.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"canopy/client/internal/config"
	"canopy/shared/daemon"
)

func pidPath() string { return filepath.Join(config.DataDir(), "canopy.pid") }
func logPath() string { return filepath.Join(config.DataDir(), "canopy.log") }

// startBackground starts `canopy run --foreground args...` and waits a moment to make sure it
// didn't stop right away.
func startBackground(args []string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	cmd, exited, err := daemon.Start(bin, append([]string{"run", "--foreground"}, args...), logPath())
	if err != nil {
		return err
	}
	select {
	case <-exited:
		return fmt.Errorf("canopy stopped right after starting. See the log: %s", logPath())
	case <-time.After(2 * time.Second):
	}
	fmt.Printf("Canopy is running in the background (pid %d).\nLogs: %s\nStop it with: canopy stop\n", cmd.Process.Pid, logPath())
	return nil
}

// holdPIDFile locks canopy.pid for this process.
func holdPIDFile() (func(), error) {
	release, err := daemon.Lock(pidPath())
	var running daemon.RunningError
	if errors.As(err, &running) {
		return nil, fmt.Errorf("canopy is already running (pid %d). Stop it with: canopy stop", running.PID)
	}
	return release, err
}

func runningPID() int { return daemon.PID(pidPath()) }

func cmdStop() error {
	pid, err := daemon.Stop(pidPath(), 15*time.Second)
	if err != nil {
		return err
	}
	if pid == 0 {
		fmt.Println("Canopy isn’t running.")
	} else {
		fmt.Println("Stopped canopy.")
	}
	return nil
}
