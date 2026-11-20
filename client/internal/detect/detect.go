// Package detect finds installed agent CLIs and the sandbox this machine can use.
package detect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"canopy/client/internal/config"
	"canopy/shared/protocol"
)

var binaries = map[string]string{"claude": "claude", "codex": "codex", "gemini": "gemini", "pi": "pi"}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// ResolveCLI finds a CLI's real executable (through version-manager shims like mise).
func ResolveCLI(cli string) string {
	if v := os.Getenv("CANOPY_" + strings.ToUpper(cli) + "_BIN"); v != "" {
		return v
	}
	bin, ok := binaries[cli]
	if !ok {
		return ""
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	if filepath.Base(p) == "mise" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "mise", "which", bin).Output(); err == nil {
			if real, err := filepath.EvalSymlinks(strings.TrimSpace(string(out))); err == nil {
				return real
			}
		}
	}
	return p
}

// CLIs reports which CLIs are installed and signed in.
func CLIs() []protocol.CLIStatus {
	var out []protocol.CLIStatus
	for _, cli := range []string{"claude", "codex", "gemini", "pi"} {
		st := protocol.CLIStatus{CLI: cli, HasAPIKey: config.ReadKey(cli) != ""}
		if p := ResolveCLI(cli); p != "" {
			st.Installed, st.Path = true, p
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if b, err := exec.CommandContext(ctx, p, "--version").Output(); err == nil {
				st.Version = versionRe.FindString(string(b))
			}
			cancel()
			st.LoggedIn = loggedIn(cli)
		}
		out = append(out, st)
	}
	return out
}

func loggedIn(cli string) bool {
	home, _ := os.UserHomeDir()
	switch cli {
	case "claude":
		if os.Getenv("ANTHROPIC_API_KEY") != "" || config.ReadKey("claude") != "" {
			return true
		}
		dir := os.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		_, err := os.Stat(filepath.Join(dir, ".credentials.json"))
		return err == nil || runtime.GOOS == "darwin" // macOS keeps it in the keychain
	}
	return false
}

// Sandbox picks the strongest sandbox available.
func Sandbox() string {
	if runtime.GOOS == "linux" {
		if p, err := exec.LookPath("bwrap"); err == nil && p != "" {
			return "bwrap"
		}
	}
	return "none"
}
