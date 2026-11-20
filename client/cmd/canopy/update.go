package main

// `canopy update` replaces this binary with the build the paired hub serves, so a computer
// always runs the client its hub was built with. Without a pairing, or with --version, it
// uses a GitHub release instead. Then it restarts canopy if it was running.

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"canopy/client/internal/config"
	"canopy/shared/daemon"
	"canopy/shared/update"
)

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only say whether an update is available")
	tag := fs.String("version", "", "install this GitHub release, e.g. v0.2.0")
	hubURL := fs.String("hub", "", "update from this paired hub")
	_ = fs.Parse(args)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	self, err := update.Self()
	if err != nil {
		return err
	}
	have, err := update.FileSum(self)
	if err != nil {
		return err
	}
	asset := update.Asset("canopy")

	var data []byte
	from := ""
	if hub := pairedHub(*hubURL); *tag == "" && hub != "" {
		fmt.Printf("Checking %s for its canopy build…\n", hub)
		data, err = update.Download(ctx, hub+"/download/"+asset)
		if err != nil {
			return fmt.Errorf("download canopy from your hub: %w", err)
		}
		if update.Sum(data) == have {
			fmt.Printf("canopy %s matches your hub. Nothing to update.\n", version)
			return nil
		}
		from = "your hub"
	} else {
		if *hubURL != "" {
			return fmt.Errorf("this computer isn’t paired with %s", *hubURL)
		}
		t := *tag
		if t == "" {
			if t, err = update.LatestTag(ctx); err != nil {
				return err
			}
		}
		want, err := update.ReleaseSum(ctx, t, asset)
		if err != nil {
			return err
		}
		if want == have {
			fmt.Printf("canopy is up to date (%s).\n", t)
			return nil
		}
		if *tag == "" && update.Valid(version) && !update.Newer(t, version) {
			fmt.Printf("This canopy (%s) is newer than the latest release (%s). Nothing to update.\n", version, t)
			return nil
		}
		if *check {
			fmt.Printf("canopy %s is available (this is %s). Update with: canopy update\n", t, version)
			return nil
		}
		if data, err = update.Release(ctx, t, asset, want); err != nil {
			return err
		}
		from = "release " + t
	}
	if *check {
		fmt.Printf("Your hub has a different canopy build than this one (%s). Update with: canopy update\n", version)
		return nil
	}

	if err := update.Replace(self, data); err != nil {
		return err
	}
	out, _ := exec.Command(self, "version").Output()
	fmt.Printf("Updated %s from %s: %s\n", self, from, strings.TrimSpace(string(out)))
	return restartClient(self)
}

// pairedHub returns the address of the hub to update from: --hub if it is paired, else the
// only paired hub. With several pairings and no --hub it returns the first.
func pairedHub(want string) string {
	hubs, _ := config.List()
	for _, h := range hubs {
		u := strings.TrimRight(h.URL, "/")
		if want == "" || u == strings.TrimRight(want, "/") {
			return u
		}
	}
	return ""
}

// restartClient restarts canopy the way it was running: the systemd user service, or the
// background process with the same flags.
func restartClient(self string) error {
	if runtime.GOOS == "linux" {
		if out, _ := exec.Command("systemctl", "--user", "is-active", "canopy").Output(); strings.TrimSpace(string(out)) == "active" {
			if out, err := exec.Command("systemctl", "--user", "restart", "canopy").CombinedOutput(); err != nil {
				return fmt.Errorf("restart the canopy service: %v: %s", err, strings.TrimSpace(string(out)))
			}
			fmt.Println("Restarted the canopy service.")
			return nil
		}
	}
	pid := runningPID()
	if pid == 0 {
		fmt.Println("Start it with: canopy run")
		return nil
	}
	args := daemon.Args(pidPath())
	if len(args) < 2 {
		args = []string{self, "run", "--foreground"}
	}
	if _, err := daemon.Stop(pidPath(), 15*time.Second); err != nil {
		return err
	}
	_, exited, err := daemon.Start(self, args[1:], logPath())
	if err != nil {
		return err
	}
	select {
	case <-exited:
		return fmt.Errorf("canopy stopped right after restarting. See the log: %s", logPath())
	case <-time.After(2 * time.Second):
	}
	fmt.Printf("Restarted canopy in the background. Logs: %s\n", logPath())
	return nil
}
