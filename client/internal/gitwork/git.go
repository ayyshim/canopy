// Package gitwork is the git side of agent work on this computer (DECISIONS: Code in git
// repositories). Agents never touch a person's working tree or checked-out branch:
//
//   - each goal has a branch, canopy/<goal>, made from the base the person chose;
//   - each agent session has its own worktree, and each task its own branch off the goal branch;
//   - when a task is done its branch merges into the goal branch, and the goal branch is backed
//     up to the hub so the goal can move to another computer;
//   - shipping opens a pull request, merges into the base, or keeps the branch.
package gitwork

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"canopy/shared/protocol"
)

// Blobs stores files in the hub (diffs and goal branch backups).
type Blobs interface {
	UploadFile(ctx context.Context, p string) (sha string, size int64, err error)
	Download(ctx context.Context, sha, dest string, mode os.FileMode) error
}

type Repos struct {
	DataDir string // clones Canopy makes live in DataDir/clones
	Blobs   Blobs

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New(dataDir string, b Blobs) *Repos {
	return &Repos{DataDir: dataDir, Blobs: b, locks: map[string]*sync.Mutex{}}
}

// lock serializes git work on one clone; branches and worktrees share its refs.
func (r *Repos) lock(clone string) func() {
	r.mu.Lock()
	l := r.locks[clone]
	if l == nil {
		l = &sync.Mutex{}
		r.locks[clone] = l
	}
	r.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// git runs a git command in dir and returns its trimmed output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

func gitOK(ctx context.Context, dir string, args ...string) bool {
	_, err := git(ctx, dir, args...)
	return err == nil
}

func isRepo(ctx context.Context, dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	return gitOK(ctx, dir, "rev-parse", "--git-dir")
}

func branchExists(ctx context.Context, dir, branch string) bool {
	return branch != "" && gitOK(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

func refExists(ctx context.Context, dir, ref string) bool {
	return ref != "" && gitOK(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// dirtyFiles counts files with uncommitted changes.
func dirtyFiles(ctx context.Context, dir string) int {
	out, err := git(ctx, dir, "status", "--porcelain")
	if err != nil || out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

// defaultBranch is the remote's default branch name ("main" when it can't tell).
func defaultBranch(ctx context.Context, dir string) string {
	if out, err := git(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(out, "origin/")
	}
	for _, b := range []string{"main", "master", "trunk"} {
		if branchExists(ctx, dir, b) || refExists(ctx, dir, "origin/"+b) {
			return b
		}
	}
	return "main"
}

// Info describes a folder for the directory picker.
func Info(ctx context.Context, dir string) protocol.RepoInfoResult {
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(top) != filepath.Clean(dir) {
		return protocol.RepoInfoResult{}
	}
	res := protocol.RepoInfoResult{IsRepo: true, DefaultBranch: defaultBranch(ctx, dir), Dirty: dirtyFiles(ctx, dir)}
	if remote, err := git(ctx, dir, "remote", "get-url", "origin"); err == nil {
		res.Remote = remote
	} else if remotes, err := git(ctx, dir, "remote"); err == nil && remotes != "" {
		res.Remote, _ = git(ctx, dir, "remote", "get-url", strings.Fields(remotes)[0])
	}
	res.CurrentBranch, _ = git(ctx, dir, "branch", "--show-current")
	return res
}

// IsRepoRoot is a quick check for the folder browser.
func IsRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// shaOf takes the content hash out of a hub blob key ("content/ab/abcd…").
func shaOf(key string) string {
	return key[strings.LastIndex(key, "/")+1:]
}
