package system

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Riley", "GIT_AUTHOR_EMAIL=riley@example.test",
		"GIT_COMMITTER_NAME=Riley", "GIT_COMMITTER_EMAIL=riley@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// DECISIONS "Code in git repositories": a goal works on its own branch from the base the person
// chose, each task in its own worktree; tasks that conflict resolve it before they count as
// done; the person's working tree is never touched; shipping opens a pull request, and merging
// is refused while the base has uncommitted changes.
func TestGoalBranchShipsAsPullRequest(t *testing.T) {
	h := startHub(t)
	owner, org := h.setupOrg("Git Co", "Riley Owner", "riley@git.test")
	mc := h.pair(owner, org, "computer", "Riley’s laptop", sandboxKind())
	mc.start()
	h.waitOnline(owner, org, mc.id, true)

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitRun(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	clone := filepath.Join(root, "payments-api")
	gitRun(t, root, "clone", "--quiet", remote, clone)
	writeFile(t, filepath.Join(clone, "shared.txt"), "start\n")
	gitRun(t, clone, "add", ".")
	gitRun(t, clone, "commit", "--quiet", "-m", "first")
	gitRun(t, clone, "push", "--quiet", "origin", "HEAD:main")
	gitRun(t, clone, "switch", "--quiet", "-c", "feat/export-ui")
	writeFile(t, filepath.Join(clone, "wip.txt"), "Riley’s unfinished work\n")

	payload := workspacePayload(mc.id, clone)
	payload["workspace"] = map[string]any{"name": "Payments"} // goals wait for their person to ship
	// Wren can write the repository too, so both tasks run side by side and conflict.
	members := list(payload["members"])
	m(members[2])["grants"] = []any{map[string]any{"directory_key": "code", "mode": "read_write", "source": "human"}}
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", payload, 201)["workspace"])["id"])
	b := owner.board(ws)
	dir := m(list(b["directories"])[0])
	if dir["is_repo"] != true || !strings.HasSuffix(str(dir["remote"]), "remote.git") || str(dir["default_branch"]) != "main" {
		t.Fatalf("directory not seen as a repository: %v", dir)
	}

	riley := memberID(b, "Riley")
	title := "Add exports [tasks:2] [parallel] [everydir] [dirs:" + clone + ":rw] [code:payments-api] [conflict:payments-api] [slow:1]"
	goal := str(m(owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": title, "base": "feat/export-ui"}, 201)["goal"])["id"])
	var branch string
	waitFor(t, 90*time.Second, "goal ready to ship", func() bool {
		b = owner.board(ws)
		for _, sig := range list(b["signals"]) {
			if str(m(sig)["type"]) == "dir_access" { // Wren has no grant on the repository yet
				owner.do("POST", "/api/v1/signals/"+str(m(sig)["id"])+"/resolve", map[string]any{"action": "grant"}, 200)
			}
		}
		owner.approveHandovers(b)
		for _, g := range list(b["goals"]) {
			if str(m(g)["id"]) == goal {
				branch = str(m(g)["branch"])
				return str(m(g)["status"]) == "ready"
			}
		}
		return false
	})
	if !strings.HasPrefix(branch, "canopy/add-exports") {
		t.Fatalf("goal branch: %q", branch)
	}
	repo := m(list(b["goal_repos"])[0])
	if repo["backed_up"] != true || repo["commits"].(float64) < 2 {
		t.Fatalf("goal repo: %v", repo)
	}
	if !strings.Contains(taskSummaries(b), "resolved a conflict") {
		t.Errorf("the second task should have resolved a conflict: %s", taskSummaries(b))
	}
	diffs := 0
	for _, o := range list(b["outputs"]) {
		if str(m(o)["kind"]) == "diff" {
			diffs++
		}
	}
	if diffs != 2 {
		t.Errorf("diff outputs: %d", diffs)
	}
	// Riley's working tree and branch are untouched.
	if cur := gitRun(t, clone, "branch", "--show-current"); cur != "feat/export-ui" {
		t.Fatalf("Riley’s branch changed to %s", cur)
	}
	if got := gitRun(t, clone, "status", "--porcelain"); got != "?? wip.txt" {
		t.Fatalf("Riley’s working tree changed: %q", got)
	}
	if !strings.HasPrefix(gitRun(t, clone, "show", branch+":shared.txt"), "resolved by") {
		t.Fatal("goal branch doesn’t have the resolved file")
	}

	// Merging into feat/export-ui is refused while Riley has uncommitted work there.
	// The approved brief is the goal's summary and its "Summary:" note, and is what ships as the PR body.
	var summary, noteBody string
	for _, g := range list(b["goals"]) {
		if str(m(g)["id"]) == goal {
			summary = str(m(g)["summary"])
		}
	}
	for _, n := range list(b["notes"]) {
		if str(m(n)["kind"]) == "summary" {
			noteBody = str(m(n)["body"])
		}
	}
	if !strings.HasPrefix(summary, "# Handover") || summary != noteBody {
		t.Errorf("summary should be the approved brief: %q (note %q)", summary, noteBody)
	}
	res := owner.do("POST", "/api/v1/goals/"+goal+"/ship", map[string]any{"action": "merge"}, 200)
	if str(res["status"]) != "dirty" {
		t.Fatalf("merge with uncommitted work: %v", res)
	}
	res = owner.do("POST", "/api/v1/goals/"+goal+"/ship", map[string]any{"action": "pr"}, 200)
	if str(res["status"]) != "done" {
		t.Fatalf("pull request: %v", res)
	}
	gitRun(t, remote, "rev-parse", "--verify", "refs/heads/"+branch)
	owner.waitGoalDone(ws, goal, 10*time.Second)

	// Moving the goal's work to another computer restores the branch from the hub backup.
	var bundles int
	_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM goal_repos WHERE bundle_key <> ''`).Scan(&bundles)
	if bundles != 1 {
		t.Errorf("backups: %d", bundles)
	}
}
