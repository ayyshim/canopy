// Package sandbox runs a member's CLI so it can see only the workspace folder and its
// granted directories (SPEC "Enforcement": paths outside are invisible, not just denied),
// with network only through Canopy's allowlisting proxy.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"canopy/shared/protocol"
)

type Spec struct {
	Kind   string           // "bwrap" or "none"
	Mounts []protocol.Mount // agent-visible path, source on this machine, ro/rw
	Cwd    string
	// Extra paths the CLI itself needs: its install dir and Canopy's binary (ro), its config
	// and Canopy's session socket dir (rw).
	ReadOnly  []string
	ReadWrite []string
	Env       []string
	// ProxySocket, when set, is the only network: the sandbox has no network of its own.
	ProxySocket string
	CanopyBin   string
}

// systemDirs are read-only in every sandbox (programs, libraries, certificates).
var systemDirs = []string{"/usr", "/etc", "/opt", "/bin", "/sbin", "/lib", "/lib64", "/lib32", "/nix/store"}

const ProxyPort = "18080"

// Command builds the process for argv inside the sandbox.
func Command(ctx context.Context, s Spec, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("nothing to run")
	}
	switch s.Kind {
	case "none":
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.Env = s.Cwd, append(os.Environ(), s.Env...)
		return cmd, nil
	case "bwrap":
		args, err := bwrapArgs(s)
		if err != nil {
			return nil, err
		}
		inner := argv
		if s.ProxySocket != "" {
			inner = append([]string{s.CanopyBin, "sandbox-init", "--proxy", s.ProxySocket, "--"}, argv...)
		}
		cmd := exec.CommandContext(ctx, "bwrap", append(append(args, "--"), inner...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "USER=" + os.Getenv("USER"), "LANG=" + firstNonEmpty(os.Getenv("LANG"), "C.UTF-8"), "TERM=dumb"}
		cmd.Env = append(cmd.Env, s.Env...)
		if s.ProxySocket != "" {
			p := "http://127.0.0.1:" + ProxyPort
			cmd.Env = append(cmd.Env, "HTTPS_PROXY="+p, "HTTP_PROXY="+p, "https_proxy="+p, "http_proxy="+p, "NO_PROXY=localhost,127.0.0.1", "no_proxy=localhost,127.0.0.1")
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("unknown sandbox %q", s.Kind)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func bwrapArgs(s Spec) ([]string, error) {
	a := []string{"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try"}
	if s.ProxySocket != "" {
		a = append(a, "--unshare-net")
	}
	for _, d := range systemDirs {
		fi, err := os.Lstat(d)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(d)
			if err != nil {
				return nil, err
			}
			a = append(a, "--symlink", target, d)
			continue
		}
		a = append(a, "--ro-bind", d, d)
	}
	a = append(a, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--tmpfs", "/var")
	home := os.Getenv("HOME")
	if home != "" {
		a = append(a, "--tmpfs", home) // the home directory exists but is empty except for what we bind
	}
	// Order matters: parents before children so a read-write subfolder (outputs) can sit
	// inside a read-only folder.
	type bind struct{ src, dst, mode string }
	var binds []bind
	for _, p := range s.ReadOnly {
		if exists(p) {
			binds = append(binds, bind{p, p, "ro"})
		}
	}
	for _, p := range s.ReadWrite {
		if exists(p) {
			binds = append(binds, bind{p, p, "rw"})
		}
	}
	for _, m := range s.Mounts {
		src := m.Source
		if src == "" {
			src = m.Path
		}
		if m.Mode == "rw" {
			if err := os.MkdirAll(src, 0o755); err != nil {
				return nil, err
			}
		}
		if !exists(src) {
			continue // a granted directory missing on disk stays invisible
		}
		binds = append(binds, bind{src, m.Path, m.Mode})
	}
	sort.SliceStable(binds, func(i, j int) bool { return depth(binds[i].dst) < depth(binds[j].dst) })
	for _, b := range binds {
		flag := "--ro-bind"
		if b.mode == "rw" {
			flag = "--bind"
		}
		a = append(a, flag, b.src, b.dst)
	}
	if s.Cwd != "" {
		a = append(a, "--chdir", s.Cwd)
	}
	return a, nil
}

func depth(p string) int { return strings.Count(filepath.Clean(p), "/") }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Visible reports whether path is inside one of the mounts (for checks outside the sandbox).
func Visible(mounts []protocol.Mount, p string) (protocol.Mount, string, bool) {
	p = filepath.Clean(p)
	var best *protocol.Mount
	for i := range mounts {
		m := &mounts[i]
		mp := filepath.Clean(m.Path)
		if p == mp || strings.HasPrefix(p, mp+"/") {
			if best == nil || len(mp) > len(filepath.Clean(best.Path)) {
				best = m
			}
		}
	}
	if best == nil {
		return protocol.Mount{}, "", false
	}
	rel := strings.TrimPrefix(p, filepath.Clean(best.Path))
	src := best.Source
	if src == "" {
		src = best.Path
	}
	return *best, filepath.Join(src, rel), true
}
