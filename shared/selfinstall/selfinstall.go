// Package selfinstall copies a downloaded binary into a folder on PATH under its short name
// (canopy-linux-amd64 becomes canopy), so it runs from any folder. If that folder isn't on
// PATH yet, it adds it to the shell's startup file.
package selfinstall

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Run handles `<name> install [--dir DIR] [--no-path]`.
func Run(name string, args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dir := fs.String("dir", DefaultDir(), "folder to install into")
	noPath := fs.Bool("no-path", false, "don’t change shell startup files")
	_ = fs.Parse(args)

	target, err := filepath.Abs(filepath.Join(*dir, name))
	if err != nil {
		return err
	}
	if err := Copy(target); err != nil {
		return err
	}
	fmt.Printf("Installed %s to %s\n", name, target)

	if onPath(filepath.Dir(target)) {
		fmt.Printf("Run it from any folder: %s help\n", name)
		return nil
	}
	if *noPath {
		fmt.Printf("%s isn’t on your PATH. Add it, or run %s by its full path.\n", filepath.Dir(target), target)
		return nil
	}
	file, added, err := addToPath(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("installed, but couldn’t add %s to your PATH: %w", filepath.Dir(target), err)
	}
	if added {
		fmt.Printf("Added %s to your PATH in %s.\n", filepath.Dir(target), file)
	}
	fmt.Printf("Open a new terminal, then run: %s help\n", name)
	return nil
}

// DefaultDir is $CANOPY_BIN_DIR, /usr/local/bin for root, or ~/.local/bin.
func DefaultDir() string {
	if d := os.Getenv("CANOPY_BIN_DIR"); d != "" {
		return d
	}
	if os.Geteuid() == 0 {
		return "/usr/local/bin"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

// Copy writes this binary to target. It writes a temporary file and renames it, so it can
// replace a copy that is running.
func Copy(target string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	if real, err := filepath.EvalSymlinks(target); err == nil && real == self {
		return nil // already running the installed copy
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return writeErr(target, err)
	}
	src, err := os.Open(self)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*")
	if err != nil {
		return writeErr(target, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return writeErr(target, os.Rename(tmp.Name(), target))
}

func writeErr(target string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("can’t write %s. Run it with sudo, or pick a folder you own with --dir", target)
	}
	return err
}

func onPath(dir string) bool {
	return slices.ContainsFunc(filepath.SplitList(os.Getenv("PATH")), func(p string) bool {
		return filepath.Clean(p) == dir
	})
}

// addToPath adds dir to PATH in the startup file of the user's shell. It returns that file
// and whether it added the line (false when the line was already there).
func addToPath(dir string) (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	shown := dir
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		shown = "$HOME/" + rel
	}
	line := fmt.Sprintf("export PATH=\"%s:$PATH\"", shown)
	var file string
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		file = filepath.Join(home, ".zshrc")
	case "bash":
		file = filepath.Join(home, ".bashrc")
		if runtime.GOOS == "darwin" {
			file = filepath.Join(home, ".bash_profile") // Terminal on macOS opens login shells
		}
	case "fish":
		file = filepath.Join(home, ".config", "fish", "conf.d", "canopy.fish")
		line = fmt.Sprintf("fish_add_path %q", dir)
	default:
		file = filepath.Join(home, ".profile")
	}
	if b, err := os.ReadFile(file); err == nil && strings.Contains(string(b), line) {
		return file, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", false, err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# Added by canopy install\n%s\n", line)
	return file, true, err
}
