package gitwork

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"canopy/shared/protocol"
)

// ConflictError means the goal branch changed in ways that conflict with the task's work.
// The merge is left in progress so the agent can resolve it.
type ConflictError struct {
	GoalBranch string
	Files      []string
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("Merging %s into your branch conflicts in: %s. Resolve the conflicts, commit, then call complete_task again.",
		e.GoalBranch, strings.Join(e.Files, ", "))
}

// Integrate finishes a task's branch: it commits anything left uncommitted, merges in the
// goal branch's newest work, moves the goal branch forward to the result, uploads the task's
// diff and backs the goal branch up to the hub.
func (r *Repos) Integrate(ctx context.Context, workDir string, m protocol.RepoMount, taskTitle string) (protocol.RepoResult, error) {
	res := protocol.RepoResult{DirectoryID: m.DirectoryID, Base: m.Base}
	wt := filepath.Join(workDir, m.Name)
	if !isRepo(ctx, wt) || m.TaskBranch == "" {
		return res, nil
	}
	clone, err := r.clone(ctx, m)
	if err != nil {
		return res, err
	}
	defer r.lock(clone)()

	if gitOK(ctx, wt, "rev-parse", "--verify", "--quiet", "MERGE_HEAD") {
		files := conflictedFiles(ctx, wt)
		if len(files) > 0 {
			return res, ConflictError{GoalBranch: m.GoalBranch, Files: files}
		}
	}
	if dirtyFiles(ctx, wt) > 0 {
		if _, err := git(ctx, wt, "add", "--all"); err != nil {
			return res, err
		}
		if _, err := git(ctx, wt, withIdentity("commit", "--quiet", "--no-verify", "-m", taskTitle)...); err != nil {
			return res, err
		}
	}
	goalTip, err := git(ctx, clone, "rev-parse", "refs/heads/"+m.GoalBranch)
	if err != nil {
		return res, err
	}
	if !gitOK(ctx, wt, "merge-base", "--is-ancestor", goalTip, "HEAD") {
		if _, err := git(ctx, wt, withIdentity("merge", "--no-edit", "--quiet", m.GoalBranch)...); err != nil {
			if files := conflictedFiles(ctx, wt); len(files) > 0 {
				return res, ConflictError{GoalBranch: m.GoalBranch, Files: files}
			}
			return res, err
		}
	}
	head, err := git(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return res, err
	}
	// Compare-and-swap: fails if another task moved the goal branch meanwhile.
	if _, err := git(ctx, clone, "update-ref", "refs/heads/"+m.GoalBranch, head, goalTip); err != nil {
		return res, fmt.Errorf("the goal branch moved while merging; call complete_task again")
	}
	if err := r.describe(ctx, clone, m, goalTip, head, &res); err != nil {
		return res, err
	}
	return res, nil
}

// describe fills in the goal branch's state, the task's diff and the hub backup.
func (r *Repos) describe(ctx context.Context, clone string, m protocol.RepoMount, from, to string, res *protocol.RepoResult) error {
	res.TipSHA = to
	base := baseRef(ctx, clone, m.Base)
	if mb, err := git(ctx, clone, "merge-base", base, to); err == nil {
		res.BaseSHA = mb
		if n, err := git(ctx, clone, "rev-list", "--count", mb+".."+to); err == nil {
			res.Commits, _ = strconv.Atoi(n)
		}
	}
	if files, err := git(ctx, clone, "diff", "--name-only", from, to); err == nil && files != "" {
		res.Files = len(strings.Split(files, "\n"))
		patch, _ := git(ctx, clone, "diff", from, to)
		if key, size, err := r.upload(ctx, []byte(patch+"\n"), "diff"); err == nil {
			res.DiffKey, res.DiffSize = key, size
		}
	}
	key, err := r.backup(ctx, clone, m.GoalBranch)
	if err == nil {
		res.BundleKey = key
	}
	return nil
}

// backup bundles the goal branch's commits that aren't on the remote and uploads them.
func (r *Repos) backup(ctx context.Context, clone, branch string) (string, error) {
	tmp, err := os.CreateTemp("", "canopy-bundle-*")
	if err != nil {
		return "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	args := []string{"bundle", "create", "--quiet", tmp.Name(), "refs/heads/" + branch}
	if remotes, _ := git(ctx, clone, "for-each-ref", "--count=1", "refs/remotes"); remotes != "" {
		args = append(args, "--not", "--remotes")
	}
	if _, err := git(ctx, clone, args...); err != nil {
		return "", err
	}
	sha, _, err := r.Blobs.UploadFile(ctx, tmp.Name())
	if err != nil {
		return "", err
	}
	return blobKey(sha), nil
}

func (r *Repos) upload(ctx context.Context, data []byte, kind string) (string, int64, error) {
	tmp, err := os.CreateTemp("", "canopy-"+kind+"-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", 0, err
	}
	tmp.Close()
	sha, size, err := r.Blobs.UploadFile(ctx, tmp.Name())
	return blobKey(sha), size, err
}

func blobKey(sha string) string { return "content/" + sha[:2] + "/" + sha }

func conflictedFiles(ctx context.Context, dir string) []string {
	out, err := git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// withIdentity lets git commit when the computer has no git name set up.
func withIdentity(args ...string) []string {
	if out, err := git(context.Background(), ".", "config", "--global", "user.email"); err == nil && out != "" {
		return args
	}
	return append([]string{"-c", "user.name=Canopy", "-c", "user.email=canopy@localhost"}, args...)
}
