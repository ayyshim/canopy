package main

// `canopy-hub --as-srv` installs the hub as a systemd service that starts at boot. The service
// runs as the user canopy, because the built-in PostgreSQL refuses to run as root, and keeps
// its data in /var/lib/canopy unless --data says otherwise.

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"canopy/shared/daemon"
	"canopy/shared/selfinstall"
)

const (
	serviceName = "canopy-hub"
	unitPath    = "/etc/systemd/system/canopy-hub.service"
	serviceUser = "canopy"
	serviceBin  = "/usr/local/bin/canopy-hub"
)

var errRootPostgres = errors.New("the hub’s built-in PostgreSQL can’t run as root. Start canopy-hub as a normal user, " +
	"or install it as a boot service that runs as its own user: sudo canopy-hub --as-srv")

// settingKeys are the environment settings --as-srv saves to DATA/hub.env for the service.
var settingKeys = []string{
	"DATABASE_URL", "PUBLIC_URL", "HUB_ADDR", "RUNNER_ADDR", "RUNNER_URL", "RUNNER_HOSTS",
	"SMTP_ADDR", "SMTP_FROM", "HUB_SETUP_TOKEN", "REDIS_URL",
	"S3_ENDPOINT", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_BUCKET", "S3_SECURE", "BLOB_DIR",
	"COOKIE_SECURE", "TRUST_PROXY", "AUTH_RATE_LIMIT", "DEFAULT_SESSION_LIMIT", "DEFAULT_WEEKLY_LIMIT",
}

// startEnv holds the settings as they were when the command started, before flags changed them.
var startEnv map[string]string

func settingsFromEnv() map[string]string {
	m := map[string]string{}
	for _, k := range settingKeys {
		if v := os.Getenv(k); v != "" {
			m[k] = v
		}
	}
	return m
}

func installService(o options) error {
	if runtime.GOOS != "linux" {
		return errors.New("--as-srv sets up a systemd service, which needs Linux. On this system, start the hub in the background with canopy-hub, or run canopy-hub --foreground from your own service manager")
	}
	if os.Geteuid() != 0 {
		return errors.New("installing a boot service needs root. Run: sudo canopy-hub --as-srv (with the same flags)")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("this system doesn’t run systemd. Run canopy-hub --foreground from your own service manager instead")
	}
	ownData := ""
	if name := os.Getenv("SUDO_USER"); name != "" && name != "root" {
		if u, err := user.Lookup(name); err == nil {
			ownData = filepath.Join(u.HomeDir, ".local", "share", "canopy-hub")
			if pid := daemon.PID(lockPath(ownData)); pid > 0 {
				return fmt.Errorf("your own hub is running (pid %d) and would take the service’s ports. Stop it first: canopy-hub stop", pid)
			}
		}
	}
	fmt.Println("Setting up the hub as a boot service…")

	u, created, err := ensureServiceUser(o.data)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("  Created the user %s.\n", serviceUser)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	group := serviceUser
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		group = g.Name
	}

	if err := selfinstall.Copy(serviceBin); err != nil {
		return err
	}
	fmt.Printf("  Installed canopy-hub to %s.\n", serviceBin)

	if _, err := os.Stat(o.data); errors.Is(err, os.ErrNotExist) && hasHubData(ownData) {
		if err := copyTree(ownData, o.data); err != nil {
			return fmt.Errorf("copy your hub’s data from %s: %w", ownData, err)
		}
		fmt.Printf("  Copied your hub’s data from %s, so the service keeps it. The original stays.\n", ownData)
	}
	if err := os.MkdirAll(o.data, 0o700); err != nil {
		return err
	}
	if keys, err := saveSettings(o.data); err != nil {
		return err
	} else if len(keys) > 0 {
		fmt.Printf("  Saved %s to %s.\n", strings.Join(keys, ", "), envFilePath(o.data))
	}
	if err := chownTree(o.data, uid, gid); err != nil {
		return err
	}

	if err := os.WriteFile(unitPath, []byte(unitFile(o, group)), 0o644); err != nil {
		return err
	}
	fmt.Printf("  Wrote %s.\n", unitPath)
	for _, args := range [][]string{{"daemon-reload"}, {"enable", serviceName}, {"restart", serviceName}} {
		if err := systemctl(args...); err != nil {
			return err
		}
	}
	s, err := waitForService(o.data)
	if err != nil {
		return err
	}
	printRunning(s, o.data, "journalctl -u "+serviceName+" -f")
	fmt.Println("It starts at every boot. Stop it with: sudo canopy-hub stop. Remove the service with: sudo canopy-hub remove-srv")
	return nil
}

func unitFile(o options, group string) string {
	args := []string{serviceBin, "serve", "--foreground", "--data", o.data}
	for _, f := range []struct{ name, value string }{
		{"--addr", o.addr}, {"--public-url", o.publicURL}, {"--domain", o.domain}, {"--runner-addr", o.runnerAddr},
	} {
		if f.value != "" {
			args = append(args, f.name, f.value)
		}
	}
	if o.noAutoMigrate {
		args = append(args, "--no-auto-migrate")
	}
	for i, a := range args {
		args[i] = systemdQuote(a)
	}
	return fmt.Sprintf(`# Written by canopy-hub --as-srv. To change it, run sudo canopy-hub --as-srv again with new
# flags, or edit settings in %s and run: sudo systemctl restart %s
[Unit]
Description=Canopy hub
After=network-online.target
Wants=network-online.target

[Service]
User=%s
Group=%s
ExecStart=%s
Restart=on-failure
RestartSec=5
TimeoutStopSec=60
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, envFilePath(o.data), serviceName, serviceUser, group, strings.Join(args, " "))
}

// systemdQuote writes one ExecStart argument so systemd reads it back unchanged.
func systemdQuote(s string) string {
	s = strings.NewReplacer("%", "%%", "$", "$$").Replace(s)
	if strings.ContainsAny(s, " \t\"'\\;") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}

// saveSettings merges the settings given to this command into DATA/hub.env and returns the
// names it saved.
func saveSettings(data string) ([]string, error) {
	if len(startEnv) == 0 {
		return nil, nil
	}
	merged := map[string]string{}
	if b, err := os.ReadFile(envFilePath(data)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(k, "#") {
				merged[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	maps.Copy(merged, startEnv)
	var b strings.Builder
	b.WriteString("# Settings for canopy-hub, read at start. Restart the hub after changing them.\n")
	for _, k := range slices.Sorted(maps.Keys(merged)) {
		fmt.Fprintf(&b, "%s=%s\n", k, merged[k])
	}
	if err := os.WriteFile(envFilePath(data), []byte(b.String()), 0o600); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(startEnv)), nil
}

func ensureServiceUser(home string) (*user.User, bool, error) {
	if u, err := user.Lookup(serviceUser); err == nil {
		return u, false, nil
	}
	shell := "/bin/false"
	for _, p := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/usr/bin/nologin"} {
		if _, err := os.Stat(p); err == nil {
			shell = p
			break
		}
	}
	out, err := exec.Command("useradd", "--system", "--user-group", "--home-dir", home, "--no-create-home", "--shell", shell, serviceUser).CombinedOutput()
	if err != nil {
		return nil, false, fmt.Errorf("create the user %s: %v: %s", serviceUser, err, strings.TrimSpace(string(out)))
	}
	u, err := user.Lookup(serviceUser)
	return u, true, err
}

func removeService() error {
	if runtime.GOOS != "linux" || !serviceInstalled() {
		fmt.Println("There’s no boot service to remove.")
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("removing the boot service needs root. Run: sudo canopy-hub remove-srv")
	}
	data := serviceDataDir()
	if err := systemctl("disable", "--now", serviceName); err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Printf("Removed the boot service. Its data is still in %s, owned by the user %s.\n", data, serviceUser)
	fmt.Printf("To delete it: sudo rm -rf %s && sudo userdel %s\n", data, serviceUser)
	return nil
}

// startServiceInstead starts the boot service when `canopy-hub` is run with sudo and no flags,
// and stops a second hub from starting next to it. It returns false to start the hub normally.
func startServiceInstead(args []string) (bool, error) {
	if !serviceInstalled() {
		return false, nil
	}
	if os.Geteuid() != 0 {
		if os.Getenv("CANOPY_DATA_DIR") != "" || slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(strings.TrimLeft(a, "-"), "data") }) {
			return false, nil // the person's own hub, in a folder they chose
		}
		switch serviceState("is-active") {
		case "active":
			return true, errors.New("the hub already runs as a boot service. See: sudo canopy-hub status")
		case "inactive", "failed":
			return true, errors.New("the hub is installed as a boot service. Start it with: sudo canopy-hub")
		}
		return true, errors.New("the hub is installed as a boot service. See whether it runs with: sudo canopy-hub status")
	}
	if len(args) > 0 {
		return true, errors.New("the boot service keeps its own flags. To change them, run: sudo canopy-hub --as-srv (with the new flags)")
	}
	if err := systemctl("start", serviceName); err != nil {
		return true, err
	}
	s, err := waitForService(serviceDataDir())
	if err != nil {
		return true, err
	}
	printRunning(s, serviceDataDir(), "journalctl -u "+serviceName+" -f")
	return true, nil
}

func waitForService(data string) (hubState, error) {
	for deadline := time.Now().Add(startTimeout); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		pid, _ := strconv.Atoi(serviceProp("MainPID"))
		if s, err := readState(data); err == nil && pid > 0 && s.PID == pid {
			return s, nil
		}
		if serviceProp("ActiveState") == "failed" || serviceProp("SubState") == "auto-restart" {
			out, _ := exec.Command("journalctl", "-u", serviceName, "-n", "15", "--no-pager", "-o", "cat").Output()
			return hubState{}, fmt.Errorf("the hub service stopped while starting. The end of its log:\n%s", strings.TrimSpace(string(out)))
		}
	}
	return hubState{}, fmt.Errorf("the hub is still starting after %s; follow it with: journalctl -u %s -f", startTimeout, serviceName)
}

func serviceInstalled() bool {
	_, err := os.Stat(unitPath)
	return runtime.GOOS == "linux" && err == nil
}

// serviceDataDir reads --data from the service's ExecStart line.
func serviceDataDir() string {
	b, _ := os.ReadFile(unitPath)
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "ExecStart="); ok {
			f := strings.Fields(rest)
			if i := slices.Index(f, "--data"); i >= 0 && i+1 < len(f) {
				return strings.ReplaceAll(strings.Trim(f[i+1], `"`), "%%", "%")
			}
		}
	}
	return "/var/lib/canopy"
}

// serviceState runs `systemctl is-active` or `is-enabled` and returns its answer.
func serviceState(check string) string {
	out, _ := exec.Command("systemctl", check, serviceName).Output()
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return "unknown"
}

func serviceProp(name string) string {
	out, _ := exec.Command("systemctl", "show", "-p", name, "--value", serviceName).Output()
	return strings.TrimSpace(string(out))
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runAsDataOwner reruns this command as the owner of the data folder when root runs it on a
// hub that belongs to another user, such as the boot service’s canopy user. PostgreSQL can’t
// run as root, and files root wrote there would lock the hub out of them.
func runAsDataOwner(data string) (bool, error) {
	if os.Geteuid() != 0 {
		return false, nil
	}
	info, err := os.Stat(data)
	if err != nil {
		return false, nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return false, nil
	}
	bin, err := os.Executable()
	if err != nil {
		return true, err
	}
	cmd := exec.Command(bin, append(os.Args[1:], "--data", data)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: st.Uid, Gid: st.Gid}}
	cmd.Env = os.Environ()
	if u, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil {
		cmd.Env = append(cmd.Env, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
	}
	if err := cmd.Start(); err != nil {
		return true, err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()
	err = cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	return true, err
}

func hasHubData(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "postgres", "data"))
	return err == nil
}

// copyTree copies a stopped hub's data folder, keeping modes and symlinks. The old hub's lock
// and address files stay behind.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "hub.lock" || rel == "hub.json" {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, info.Mode().Perm())
		}
		return nil
	})
}

func chownTree(dir string, uid, gid int) error {
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}
