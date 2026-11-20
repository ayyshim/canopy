package gitwork

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"canopy/shared/protocol"
)

// Checkout is a repository ready for an agent session.
type Checkout struct {
	Worktree string // where the agent works: <work folder>/<name>
	GitDir   string // the clone's .git folder, which the worktree's commits go into
}

// Prepare puts a repository in a session's work folder: the clone (made if needed), the goal
// branch (made from its base, or restored from the hub's backup), and a worktree on the
// task's branch. Without a task branch the worktree is a read-only view of the goal branch.
func (r *Repos) Prepare(ctx context.Context, workDir string, m protocol.RepoMount) (Checkout, error) {
	clone, err := r.clone(ctx, m)
	if err != nil {
		return Checkout{}, err
	}
	defer r.lock(clone)()
	gitDir, err := git(ctx, clone, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Checkout{}, err
	}
	if err := r.ensureGoalBranch(ctx, clone, m); err != nil {
		return Checkout{}, err
	}
	wt := filepath.Join(workDir, m.Name)
	fresh := !isRepo(ctx, wt)
	if m.TaskBranch == "" {
		ref := m.GoalBranch
		if !branchExists(ctx, clone, ref) {
			ref = baseRef(ctx, clone, m.Base)
		}
		if fresh {
			_, err = git(ctx, clone, "worktree", "add", "--detach", wt, ref)
		} else {
			_, err = git(ctx, wt, "checkout", "--detach", ref)
		}
		return Checkout{Worktree: wt, GitDir: gitDir}, err
	}
	if fresh {
		_ = os.RemoveAll(wt)
		_, _ = git(ctx, clone, "worktree", "prune")
		if branchExists(ctx, clone, m.TaskBranch) {
			_, err = git(ctx, clone, "worktree", "add", wt, m.TaskBranch)
		} else {
			_, err = git(ctx, clone, "worktree", "add", "-b", m.TaskBranch, wt, m.GoalBranch)
		}
		if err != nil {
			return Checkout{}, err
		}
		r.firstSetup(ctx, wt, m)
		return Checkout{Worktree: wt, GitDir: gitDir}, nil
	}
	current, _ := git(ctx, wt, "branch", "--show-current")
	if current != m.TaskBranch {
		// A new task in the same session: keep anything left over, then branch off the goal.
		if dirtyFiles(ctx, wt) > 0 {
			_, _ = git(ctx, wt, "stash", "push", "--include-untracked", "-m", "Left over before "+m.TaskBranch)
		}
		if branchExists(ctx, clone, m.TaskBranch) {
			_, err = git(ctx, wt, "switch", m.TaskBranch)
		} else {
			_, err = git(ctx, wt, "switch", "-c", m.TaskBranch, m.GoalBranch)
		}
	}
	return Checkout{Worktree: wt, GitDir: gitDir}, err
}

// clone is the clone to work from: the person's own when given, else one Canopy keeps.
func (r *Repos) clone(ctx context.Context, m protocol.RepoMount) (string, error) {
	if m.ClonePath != "" && isRepo(ctx, m.ClonePath) {
		return m.ClonePath, nil
	}
	p := filepath.Join(r.DataDir, "clones", m.DirectoryID.String())
	if isRepo(ctx, p) {
		return p, nil
	}
	if m.Remote == "" {
		return "", fmt.Errorf("%s has no remote to clone from on this computer", m.Name)
	}
	defer r.lock(p)()
	if isRepo(ctx, p) {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if _, err := git(ctx, filepath.Dir(p), "clone", "--quiet", m.Remote, p); err != nil {
		return "", fmt.Errorf("couldn’t clone %s with this computer’s git sign-in: %w", m.Name, err)
	}
	return p, nil
}

// baseRef finds the branch a goal starts from: a local branch, else the remote's, else the
// remote's default branch.
func baseRef(ctx context.Context, clone, base string) string {
	if base != "" {
		if branchExists(ctx, clone, base) {
			return base
		}
		if refExists(ctx, clone, "origin/"+base) {
			return "origin/" + base
		}
	}
	def := defaultBranch(ctx, clone)
	if refExists(ctx, clone, "origin/"+def) {
		return "origin/" + def
	}
	return def
}

func (r *Repos) ensureGoalBranch(ctx context.Context, clone string, m protocol.RepoMount) error {
	if branchExists(ctx, clone, m.GoalBranch) {
		return nil
	}
	if m.BundleKey != "" {
		if err := r.restore(ctx, clone, m); err == nil {
			return nil
		} else {
			slog.Warn("restore goal branch from the hub", "branch", m.GoalBranch, "err", err)
		}
	}
	start := baseRef(ctx, clone, m.Base)
	if m.Snapshot {
		if snap := snapshot(ctx, clone, m.Base); snap != "" {
			start = snap
		}
	}
	_, err := git(ctx, clone, "branch", m.GoalBranch, start)
	return err
}

// snapshot turns the person's uncommitted changes into a commit on top of their current
// branch, without touching their working tree (git stash create only reads it).
func snapshot(ctx context.Context, clone, base string) string {
	current, _ := git(ctx, clone, "branch", "--show-current")
	if base != "" && current != base {
		return ""
	}
	stash, err := git(ctx, clone, "stash", "create")
	if err != nil || stash == "" {
		return ""
	}
	sha, err := git(ctx, clone, "-c", "user.name=Canopy", "-c", "user.email=canopy@localhost",
		"commit-tree", stash+"^{tree}", "-p", "HEAD", "-m", "Uncommitted changes when the goal started")
	if err != nil {
		return ""
	}
	return sha
}

// restore brings a goal branch back from the hub's backup (a git bundle of its own commits).
func (r *Repos) restore(ctx context.Context, clone string, m protocol.RepoMount) error {
	_, _ = git(ctx, clone, "fetch", "--quiet", "origin") // the bundle's base commits come from the remote
	tmp, err := os.CreateTemp("", "canopy-bundle-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := r.Blobs.Download(ctx, shaOf(m.BundleKey), tmp.Name(), 0o600); err != nil {
		return err
	}
	_, err = git(ctx, clone, "fetch", "--quiet", tmp.Name(), "refs/heads/"+m.GoalBranch+":refs/heads/"+m.GoalBranch)
	return err
}

// firstSetup copies files the repository needs but doesn't commit (from the person's own
// clone only) and runs the repository's setup command once.
func (r *Repos) firstSetup(ctx context.Context, wt string, m protocol.RepoMount) {
	if m.ClonePath != "" {
		for _, f := range m.CopyFiles {
			f = filepath.Clean(f)
			if filepath.IsAbs(f) || strings.HasPrefix(f, "..") {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(m.ClonePath, f)); err == nil {
				_ = os.MkdirAll(filepath.Dir(filepath.Join(wt, f)), 0o755)
				_ = os.WriteFile(filepath.Join(wt, f), data, 0o600)
			}
		}
	}
	if strings.TrimSpace(m.Setup) == "" {
		return
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Setup)
	cmd.Dir = wt
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("repository setup command failed", "repo", m.Name, "err", err, "output", lastLine(string(out)))
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
