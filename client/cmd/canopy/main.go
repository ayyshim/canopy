// Command canopy is the client that runs on people's machines: it pairs with a Canopy hub,
// runs agent CLIs in sandboxes for workspaces, and syncs shared directories.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"canopy/client/internal/config"
	"canopy/client/internal/detect"
	"canopy/client/internal/mcpbridge"
	"canopy/client/internal/netproxy"
	"canopy/client/internal/pairing"
	"canopy/client/internal/runner"
	"canopy/client/internal/sandbox"
	"canopy/shared/selfinstall"
)

var version = "0.1.0-dev"

const usage = `canopy connects this machine to a Canopy hub.

Usage:
  canopy install [--dir DIR] [--no-path]
      copy this binary to ~/.local/bin/canopy (/usr/local/bin as root) and add that folder
      to your PATH, so canopy runs from any folder
  canopy pair --hub URL --code CODE [--name NAME] [--address HOST] [--run]
      pair with a hub using a code from Canopy (--address: for servers, the name people
      reach it by; --run: start running right after pairing)
  canopy run [--hub URL] [--sandbox auto|bwrap|none] [--foreground] [-v]
      connect and run agents in the background (--hub: only this hub; --foreground: stay in
      this terminal, for systemd or debugging; -v: verbose logs)
  canopy stop        stop canopy running in the background
  canopy update [--check] [--version vX.Y.Z] [--hub URL]
      replace this binary with the build your hub serves (or the latest GitHub release when
      not paired, or the release --version names), then restart canopy if it was running
  canopy status      show whether canopy is running, pairings and installed CLIs
  canopy version

Pairing codes come from Canopy: workspace setup, or Organization → Computers → Connect a computer.

Environment:
  CANOPY_HOME          pairings and keys (default ~/.config/canopy)
  CANOPY_DATA          work folders, mirrors, sockets and canopy.log (default ~/.local/share/canopy)
  CANOPY_CLAUDE_BIN    the claude binary to run (default: found on PATH)
  CANOPY_BIN_DIR       where canopy install puts the binary
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = selfinstall.Run("canopy", os.Args[2:])
	case "pair":
		err = cmdPair(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "stop":
		err = cmdStop()
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "status":
		err = cmdStatus()
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "sandbox-init":
		err = cmdSandboxInit(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("canopy", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "canopy:", err)
		os.Exit(1)
	}
}

func cmdPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	hub := fs.String("hub", "", "the hub’s address, e.g. https://canopy.example.com")
	code := fs.String("code", "", "pairing code from Canopy")
	name := fs.String("name", "", "a name for this machine")
	address := fs.String("address", "", "for servers: the address people know it by")
	run := fs.Bool("run", false, "start running right after pairing")
	_ = fs.Parse(args)
	if *hub == "" || *code == "" {
		return fmt.Errorf("pair needs --hub and --code (copy the command from Canopy)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h, err := pairing.Pair(ctx, pairing.Options{HubURL: *hub, Code: *code, Name: *name, Address: *address, Version: version})
	if err != nil {
		return err
	}
	fmt.Printf("Paired %s with %s (%s).\n", h.RunnerName, h.OrgName, h.URL)
	if *run {
		return cmdRun([]string{"--hub", h.URL})
	}
	fmt.Println("Start it with: canopy run")
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	hub := fs.String("hub", "", "only connect to this hub")
	sb := fs.String("sandbox", "auto", "auto, bwrap or none")
	unsafe := fs.Bool("unsafe-no-sandbox", false, "run agents without a sandbox (development only)")
	verbose := fs.Bool("v", false, "verbose logging")
	foreground := fs.Bool("foreground", false, "stay in this terminal instead of running in the background")
	_ = fs.Parse(args)
	if pid := runningPID(); pid > 0 {
		return fmt.Errorf("canopy is already running (pid %d). Stop it with: canopy stop", pid)
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	kind := *sb
	if kind == "auto" {
		kind = detect.Sandbox()
	}
	if kind == "none" && !*unsafe {
		return fmt.Errorf("no sandbox is available on this machine (Linux needs bubblewrap: install the bubblewrap package). Agents must not run without one; pass --unsafe-no-sandbox only for development")
	}
	if kind == "none" {
		slog.Warn("running agents WITHOUT a sandbox: they can see everything this user can")
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	hubs, err := config.List()
	if err != nil {
		return err
	}
	var selected []config.Hub
	for _, h := range hubs {
		if *hub == "" || strings.TrimRight(h.URL, "/") == strings.TrimRight(*hub, "/") {
			selected = append(selected, h)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("this machine isn’t paired yet. Run: canopy pair --hub URL --code CODE")
	}
	if !*foreground {
		return startBackground(args)
	}
	release, err := holdPIDFile()
	if err != nil {
		return err
	}
	defer release()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var wg sync.WaitGroup
	for _, h := range selected {
		r, err := runner.New(h, version, kind, bin)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("connecting", "hub", h.URL, "organization", h.OrgName, "machine", h.RunnerName, "sandbox", kind)
			r.Run(ctx)
		}()
	}
	wg.Wait()
	return nil
}

func cmdStatus() error {
	hubs, err := config.List()
	if err != nil {
		return err
	}
	if pid := runningPID(); pid > 0 {
		fmt.Printf("Running in the background (pid %d). Logs: %s\n\n", pid, logPath())
	} else {
		fmt.Print("Not running. Start it with: canopy run\n\n")
	}
	if len(hubs) == 0 {
		fmt.Println("Not paired with any hub yet.")
	}
	for _, h := range hubs {
		fmt.Printf("%s — %s (machine: %s)\n", h.URL, h.OrgName, h.RunnerName)
	}
	fmt.Println("\nCLIs:")
	for _, c := range detect.CLIs() {
		state := "not installed"
		if c.Installed {
			state = "installed " + c.Version
			if c.LoggedIn {
				state += ", signed in"
			}
		}
		fmt.Printf("  %-8s %s\n", c.CLI, state)
	}
	fmt.Println("\nSandbox:", detect.Sandbox())
	return nil
}

// cmdMCP is started by the CLI inside the sandbox; it serves Canopy's tools.
func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	socket := fs.String("socket", "", "runner socket")
	_ = fs.Parse(args)
	return mcpbridge.Run(context.Background(), *socket, version)
}

// cmdSandboxInit runs inside the sandbox: it opens the proxy on loopback, then runs the CLI.
func cmdSandboxInit(args []string) error {
	fs := flag.NewFlagSet("sandbox-init", flag.ExitOnError)
	proxy := fs.String("proxy", "", "proxy socket")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("nothing to run")
	}
	if *proxy != "" {
		if err := netproxy.Forward("127.0.0.1:"+sandbox.ProxyPort, *proxy); err != nil {
			return err
		}
	}
	cmd := exec.Command(rest[0], rest[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, os.Interrupt)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()
	err := cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	return err
}
