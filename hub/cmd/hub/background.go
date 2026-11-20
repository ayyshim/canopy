package main

// `canopy-hub` starts a copy of itself with --foreground, detached from the terminal, waits
// until it is listening, prints where to open it, and returns. The running hub writes
// DATA/hub.json with its address; `canopy-hub status` and `stop` read it and DATA/hub.lock.

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"canopy/shared/daemon"
)

const startTimeout = 3 * time.Minute // a first start unpacks PostgreSQL and creates the database

func lockPath(data string) string    { return filepath.Join(data, "hub.lock") }
func statePath(data string) string   { return filepath.Join(data, "hub.json") }
func logPath(data string) string     { return filepath.Join(data, "hub.log") }
func envFilePath(data string) string { return filepath.Join(data, "hub.env") }

// hubState is what a running hub writes to hub.json once it is listening.
type hubState struct {
	PID       int       `json:"pid"`
	URL       string    `json:"url"`       // the address people open
	LocalURL  string    `json:"local_url"` // the same hub, reachable from this machine
	SetupURL  string    `json:"setup_url,omitempty"`
	StartedAt time.Time `json:"started_at"`
	// Settings it was started with from the environment, so an update can restart it the same way.
	Env map[string]string `json:"env,omitempty"`
}

func writeState(data string, s hubState) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := statePath(data) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(data))
}

func readState(data string) (hubState, error) {
	var s hubState
	b, err := os.ReadFile(statePath(data))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// loopback turns a listen address such as :8080 into one this machine can dial.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// startBackground runs `canopy-hub serve --foreground args...` detached and waits for it.
func startBackground(o options, args []string) error {
	if pid := daemon.PID(lockPath(o.data)); pid > 0 {
		return fmt.Errorf("the hub is already running (pid %d). See canopy-hub status, or stop it with canopy-hub stop", pid)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	cmd, exited, err := daemon.Start(bin, append([]string{"serve", "--foreground"}, args...), logPath(o.data))
	if err != nil {
		return err
	}
	fmt.Println("Starting the hub…")
	s, err := waitForHub(o.data, cmd.Process.Pid, exited)
	if err != nil {
		return err
	}
	printRunning(s, o.data, logPath(o.data))
	return nil
}

// waitForHub waits until the hub with this pid has written hub.json.
func waitForHub(data string, pid int, exited <-chan struct{}) (hubState, error) {
	deadline := time.After(startTimeout)
	for {
		if s, err := readState(data); err == nil && s.PID == pid {
			return s, nil
		}
		select {
		case <-exited:
			return hubState{}, fmt.Errorf("the hub stopped while starting. The end of its log (%s):\n%s",
				logPath(data), daemon.Tail(logPath(data), 15))
		case <-deadline:
			return hubState{}, fmt.Errorf("the hub is still starting after %s; see its log: %s", startTimeout, logPath(data))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func printRunning(s hubState, data, logs string) {
	fmt.Printf("The hub is running (pid %d).\n", s.PID)
	fmt.Printf("  Open:   %s\n", s.URL)
	if s.SetupURL != "" {
		if required, err := setupRequired(s.LocalURL); required || err != nil {
			fmt.Printf("  Set up: %s\n", s.SetupURL)
		}
	}
	fmt.Printf("  Data:   %s\n", data)
	fmt.Printf("  Logs:   %s\n", logs)
}

// setupRequired asks the running hub whether its first-run setup is still to do.
func setupRequired(localURL string) (bool, error) {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(localURL + "/api/v1/hub")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var v struct {
		SetupRequired bool `json:"setup_required"`
	}
	return v.SetupRequired, json.NewDecoder(resp.Body).Decode(&v)
}

// target is the hub that status and stop look at: --data if given, else the boot service when
// there is one, else this user's hub.
type target struct {
	data    string
	service bool
}

func pickTarget(name string, args []string) (target, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	dataFlag := fs.String("data", os.Getenv("CANOPY_DATA_DIR"), "data directory")
	if err := fs.Parse(args); err != nil {
		return target{}, err
	}
	if *dataFlag != "" {
		abs, err := filepath.Abs(*dataFlag)
		return target{data: abs, service: serviceInstalled() && abs == serviceDataDir()}, err
	}
	own := defaultDataDir()
	if serviceInstalled() && (os.Geteuid() == 0 || daemon.PID(lockPath(own)) == 0) {
		return target{data: serviceDataDir(), service: true}, nil
	}
	return target{data: own}, nil
}

func statusCmd(args []string) error {
	t, err := pickTarget("status", args)
	if err != nil {
		return err
	}
	if t.service {
		if active := serviceState("is-active"); active == "unknown" {
			fmt.Println("Boot service: installed.")
		} else {
			fmt.Printf("Boot service: %s, %s at boot.\n", active, serviceState("is-enabled"))
		}
		if os.Geteuid() != 0 {
			fmt.Println("For its address and setup link: sudo canopy-hub status")
			return nil
		}
	}
	pid := daemon.PID(lockPath(t.data))
	if pid == 0 {
		start := "canopy-hub"
		if t.service {
			start = "sudo canopy-hub"
		}
		fmt.Printf("The hub isn’t running. Start it with: %s\n  Data:   %s\n", start, t.data)
		return nil
	}
	s, err := readState(t.data)
	if err != nil || s.PID != pid {
		fmt.Printf("The hub is starting, or another canopy-hub command is using its data (pid %d).\n", pid)
		return nil
	}
	logs := logPath(t.data)
	if t.service {
		logs = "journalctl -u " + serviceName + " -f"
	}
	printRunning(s, t.data, logs)
	return nil
}

func stopCmd(args []string) error {
	t, err := pickTarget("stop", args)
	if err != nil {
		return err
	}
	if t.service {
		if os.Geteuid() != 0 {
			return errors.New("the hub runs as a boot service. Stop it with: sudo canopy-hub stop")
		}
		if err := systemctl("stop", serviceName); err != nil {
			return err
		}
		fmt.Println("Stopped the hub. The boot service starts it again at the next boot; start it now with: sudo canopy-hub")
		return nil
	}
	pid, err := daemon.Stop(lockPath(t.data), 30*time.Second)
	if err != nil {
		return err
	}
	if pid == 0 {
		fmt.Println("The hub isn’t running.")
	} else {
		fmt.Println("Stopped the hub.")
	}
	return nil
}

// loadEnvFile reads KEY=value lines from DATA/hub.env into the environment. Settings already
// in the environment win.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}
