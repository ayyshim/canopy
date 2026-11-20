package gitwork

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"canopy/shared/protocol"
)

// Ship finishes a goal's branch in each repository: first it brings the branch up to date with
// its base (conflicts go back to the hub as a task for the manager), then it opens a pull
// request, merges into the base, or keeps the branch.
func (r *Repos) Ship(ctx context.Context, msg protocol.GitShip) protocol.GitShipResult {
	out := protocol.GitShipResult{Repos: []protocol.ShipResult{}}
	for _, m := range msg.Repos {
		out.Repos = append(out.Repos, r.shipOne(ctx, msg, m))
	}
	return out
}

func (r *Repos) shipOne(ctx context.Context, msg protocol.GitShip, m protocol.RepoMount) protocol.ShipResult {
	res := protocol.ShipResult{DirectoryID: m.DirectoryID}
	fail := func(err error) protocol.ShipResult { res.Error = err.Error(); return res }
	clone, err := r.clone(ctx, m)
	if err != nil {
		return fail(err)
	}
	defer r.lock(clone)()
	if err := r.ensureGoalBranch(ctx, clone, m); err != nil {
		return fail(err)
	}
	if msg.Action == "keep" {
		return res
	}
	_, _ = git(ctx, clone, "fetch", "--quiet", "origin")
	base := baseRef(ctx, clone, m.Base)
	if files, err := r.mergeBase(ctx, clone, m.GoalBranch, base); err != nil {
		return fail(err)
	} else if len(files) > 0 {
		res.Conflicts = files
		return res
	}
	baseName := strings.TrimPrefix(base, "origin/")
	switch msg.Action {
	case "pr":
		if _, err := git(ctx, clone, "push", "--quiet", "origin", m.GoalBranch); err != nil {
			return fail(fmt.Errorf("couldn’t push with this computer’s git sign-in: %w", err))
		}
		res.Pushed = true
		res.PRURL = openPR(ctx, clone, baseName, m.GoalBranch, msg.Title, msg.Body)
		if res.PRURL == "" {
			res.PRURL = compareURL(m.Remote, baseName, m.GoalBranch)
		}
	case "merge":
		dirty, err := mergeInto(ctx, clone, baseName, m.GoalBranch)
		if dirty {
			res.Dirty = true
			return res
		}
		if err != nil {
			return fail(err)
		}
		res.Merged = true
	}
	return res
}

// mergeBase merges the base into the goal branch in a throwaway worktree. It returns the
// conflicting files, if any, leaving the goal branch as it was.
func (r *Repos) mergeBase(ctx context.Context, clone, goalBranch, base string) ([]string, error) {
	if gitOK(ctx, clone, "merge-base", "--is-ancestor", base, goalBranch) {
		return nil, nil
	}
	tmp, err := os.MkdirTemp("", "canopy-ship-*")
	if err != nil {
		return nil, err
	}
	wt := filepath.Join(tmp, "wt")
	defer func() {
		_, _ = git(ctx, clone, "worktree", "remove", "--force", wt)
		_ = os.RemoveAll(tmp)
	}()
	tip, err := git(ctx, clone, "rev-parse", "refs/heads/"+goalBranch)
	if err != nil {
		return nil, err
	}
	if _, err := git(ctx, clone, "worktree", "add", "--quiet", "--detach", wt, tip); err != nil {
		return nil, err
	}
	if _, err := git(ctx, wt, withIdentity("merge", "--no-edit", "--quiet", base)...); err != nil {
		if files := conflictedFiles(ctx, wt); len(files) > 0 {
			return files, nil
		}
		return nil, err
	}
	head, err := git(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	_, err = git(ctx, clone, "update-ref", "refs/heads/"+goalBranch, head, tip)
	return nil, err
}

// mergeInto merges the goal branch into the base in the person's own clone. It never touches
// uncommitted work: if the base is checked out with changes, it reports dirty instead.
func mergeInto(ctx context.Context, clone, base, goalBranch string) (dirty bool, err error) {
	current, _ := git(ctx, clone, "branch", "--show-current")
	if current == base {
		if dirtyFiles(ctx, clone) > 0 {
			return true, nil
		}
		_, err = git(ctx, clone, withIdentity("merge", "--no-edit", "--quiet", goalBranch)...)
		return false, err
	}
	if !branchExists(ctx, clone, base) {
		if _, err := git(ctx, clone, "branch", base, "origin/"+base); err != nil {
			return false, err
		}
	}
	old, err := git(ctx, clone, "rev-parse", "refs/heads/"+base)
	if err != nil {
		return false, err
	}
	// The goal branch already contains the base (mergeBase made sure), so this is a fast-forward.
	if !gitOK(ctx, clone, "merge-base", "--is-ancestor", old, goalBranch) {
		return false, fmt.Errorf("%s moved on; ship again", base)
	}
	_, err = git(ctx, clone, "update-ref", "refs/heads/"+base, "refs/heads/"+goalBranch, old)
	return false, err
}

// openPR uses the GitHub CLI when this computer has it signed in; "" otherwise.
func openPR(ctx context.Context, clone, base, head, title, body string) string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body", body)
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

// compareURL is the code host's "open a pull request" page for a pushed branch.
func compareURL(remote, base, head string) string {
	u := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if strings.HasPrefix(u, "git@") {
		u = "https://" + strings.Replace(strings.TrimPrefix(u, "git@"), ":", "/", 1)
	}
	if !strings.HasPrefix(u, "http") {
		return ""
	}
	switch {
	case strings.Contains(u, "gitlab"):
		return u + "/-/merge_requests/new?merge_request[source_branch]=" + head + "&merge_request[target_branch]=" + base
	default:
		return u + "/compare/" + base + "..." + head + "?expand=1"
	}
}

// Cleanup removes a goal's worktrees from this computer once the goal is done. Its branch
// stays in the clone.
func (r *Repos) Cleanup(ctx context.Context, workRoot, goalID string) {
	dir := filepath.Join(workRoot, "goal-"+goalID)
	sessions, _ := os.ReadDir(dir)
	for _, s := range sessions {
		repos, _ := os.ReadDir(filepath.Join(dir, s.Name()))
		for _, repo := range repos {
			wt := filepath.Join(dir, s.Name(), repo.Name())
			if isRepo(ctx, wt) {
				if common, err := git(ctx, wt, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
					_, _ = git(ctx, filepath.Dir(common), "worktree", "remove", "--force", wt)
				}
			}
		}
	}
	_ = os.RemoveAll(dir)
}
