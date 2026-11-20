package main

// `canopy-hub update` replaces this binary with a GitHub release (the latest, or --version),
// checked against the release's SHA256SUMS, and restarts the hub if it was running. On start
// the hub backs up its data and upgrades it, as with any new binary. The new hub also serves
// the matching client, so computers then run `canopy update`.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"canopy/shared/daemon"
	"canopy/shared/update"
)

func updateCmd(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only say whether an update is available")
	tag := fs.String("version", "", "install this release, e.g. v0.2.0")
	dataFlag := fs.String("data", os.Getenv("CANOPY_DATA_DIR"), "the data folder of the hub to restart")
	if err := fs.Parse(args); err != nil {
		return err
	}
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
	asset := update.Asset("canopy-hub")
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
	service := serviceInstalled()
	if service && os.Geteuid() == 0 {
		// The boot service runs /usr/local/bin/canopy-hub, which may not be the copy running now.
		if sum, err := update.FileSum(serviceBin); err == nil {
			have = sum
		}
	}
	if want == have {
		fmt.Printf("canopy-hub is up to date (%s).\n", t)
		return nil
	}
	if *tag == "" && update.Valid(version) && !update.Newer(t, version) {
		fmt.Printf("This canopy-hub (%s) is newer than the latest release (%s). Nothing to update.\n", version, t)
		return nil
	}
	if *check {
		fmt.Printf("canopy-hub %s is available (this is %s). Update with: canopy-hub update\n", t, version)
		return nil
	}
	if service && os.Geteuid() != 0 && *dataFlag == "" {
		return errors.New("the hub runs as a boot service. Update it with: sudo canopy-hub update")
	}

	fmt.Printf("Downloading canopy-hub %s…\n", t)
	data, err := update.Release(ctx, t, asset, want)
	if err != nil {
		return err
	}
	targets := []string{self}
	if service && os.Geteuid() == 0 && self != serviceBin {
		targets = append(targets, serviceBin)
	}
	for _, p := range targets {
		if err := update.Replace(p, data); err != nil {
			return err
		}
		fmt.Printf("Updated %s to %s.\n", p, t)
	}

	if service && os.Geteuid() == 0 && *dataFlag == "" {
		if serviceState("is-active") != "active" {
			fmt.Println("The boot service isn’t running. Start it with: sudo canopy-hub")
			return nil
		}
		fmt.Println("Restarting the boot service (it backs up and upgrades its data first)…")
		if err := systemctl("restart", serviceName); err != nil {
			return err
		}
		s, err := waitForService(serviceDataDir())
		if err != nil {
			return err
		}
		printRunning(s, serviceDataDir(), "journalctl -u "+serviceName+" -f")
		return nil
	}

	data2 := defaultDataDir()
	if *dataFlag != "" {
		if data2, err = filepath.Abs(*dataFlag); err != nil {
			return err
		}
	}
	if daemon.PID(lockPath(data2)) == 0 {
		fmt.Println("Start it with: canopy-hub")
		return nil
	}
	return restartHub(self, data2)
}

// restartHub stops the hub running in the background and starts the new binary with the same
// flags.
func restartHub(self, data string) error {
	args := daemon.Args(lockPath(data))
	if len(args) < 2 {
		args = []string{self, "serve", "--foreground", "--data", data}
	}
	if s, err := readState(data); err == nil {
		for k, v := range s.Env {
			os.Setenv(k, v)
		}
	}
	fmt.Println("Restarting the hub (it backs up and upgrades its data first)…")
	if _, err := daemon.Stop(lockPath(data), 60*time.Second); err != nil {
		return err
	}
	cmd, exited, err := daemon.Start(self, args[1:], logPath(data))
	if err != nil {
		return err
	}
	s, err := waitForHub(data, cmd.Process.Pid, exited)
	if err != nil {
		return err
	}
	printRunning(s, data, logPath(data))
	return nil
}
