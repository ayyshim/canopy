package gitwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/shared/protocol"
)

// memBlobs keeps uploads in memory, standing in for the hub.
type memBlobs map[string][]byte

func (b memBlobs) UploadFile(_ context.Context, p string) (string, int64, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	b[sha] = data
	return sha, int64(len(data)), nil
}

func (b memBlobs) Download(_ context.Context, sha, dest string, mode os.FileMode) error {
	data, ok := b[sha]
	if !ok {
		return errors.New("no such blob")
	}
	return os.WriteFile(dest, data, mode)
}

func must(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup makes a remote with one commit on main and Riley's clone of it on feat/x with an
// uncommitted change.
func setup(t *testing.T) (remote, clone string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	must(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	seed := filepath.Join(root, "seed")
	must(t, root, "clone", "--quiet", remote, seed)
	write(t, filepath.Join(seed, "app.txt"), "one\n")
	must(t, seed, "add", ".")
	must(t, seed, "commit", "--quiet", "-m", "first")
	must(t, seed, "push", "--quiet", "origin", "HEAD:main")
	clone = filepath.Join(root, "riley-api")
	must(t, root, "clone", "--quiet", remote, clone)
	must(t, clone, "switch", "--quiet", "-c", "feat/x")
	write(t, filepath.Join(clone, "app.txt"), "one\nriley's work in progress\n")
	return remote, clone
}

func TestGoalBranchFlow(t *testing.T) {
	ctx := context.Background()
	remote, clone := setup(t)
	blobs := memBlobs{}
	r := New(t.TempDir(), blobs)
	work := t.TempDir()
	m := protocol.RepoMount{DirectoryID: uuid.New(), Name: "api", Remote: remote, ClonePath: clone, Base: "feat/x",
		GoalBranch: "canopy/csv-export", TaskBranch: "canopy/csv-export--t1", Snapshot: true}

	co, err := r.Prepare(ctx, work, m)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(co.Worktree, "app.txt")); !strings.Contains(string(got), "riley's work") {
		t.Fatalf("snapshot missing from the goal branch: %q", got)
	}
	if must(t, clone, "branch", "--show-current") != "feat/x" || dirtyFiles(ctx, clone) != 1 {
		t.Fatal("Riley's working tree or branch changed")
	}

	write(t, filepath.Join(co.Worktree, "export.txt"), "csv\n")
	res, err := r.Integrate(ctx, work, m, "Add the export")
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.DiffKey == "" || res.BundleKey == "" || res.Commits != 2 {
		t.Fatalf("result: %+v", res)
	}

	// A second task in the same session branches off the goal branch's new tip.
	m.TaskBranch = "canopy/csv-export--t2"
	if _, err := r.Prepare(ctx, work, m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(co.Worktree, "export.txt")); err != nil {
		t.Fatal("second task doesn't start from the first task's work")
	}

	// Move: another computer restores the goal branch from the hub's backup.
	other := New(t.TempDir(), blobs)
	moved := m
	moved.ClonePath, moved.Snapshot, moved.BundleKey, moved.BundleTip = "", false, res.BundleKey, res.TipSHA
	co2, err := other.Prepare(ctx, t.TempDir(), moved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(co2.Worktree, "export.txt")); err != nil {
		t.Fatal("restored branch is missing the work")
	}

	// Merge refuses while Riley has uncommitted changes on the base.
	ship := protocol.GitShip{Action: "merge", Title: "CSV export", Repos: []protocol.RepoMount{m}}
	if out := r.Ship(ctx, ship); !out.Repos[0].Dirty {
		t.Fatalf("merge should be refused: %+v", out.Repos[0])
	}
	// Opening a pull request pushes the branch.
	ship.Action = "pr"
	if out := r.Ship(ctx, ship); out.Repos[0].Error != "" || !out.Repos[0].Pushed {
		t.Fatalf("pr: %+v", out.Repos[0])
	}
	if !gitOK(ctx, remote, "rev-parse", "--verify", "refs/heads/canopy/csv-export") {
		t.Fatal("branch not on the remote")
	}
}

func TestConflictGoesBackToTheAgent(t *testing.T) {
	ctx := context.Background()
	remote, clone := setup(t)
	r := New(t.TempDir(), memBlobs{})
	a, b := t.TempDir(), t.TempDir()
	base := protocol.RepoMount{DirectoryID: uuid.New(), Name: "api", Remote: remote, ClonePath: clone, GoalBranch: "canopy/g"}
	ma, mb := base, base
	ma.TaskBranch, mb.TaskBranch = "canopy/g--a", "canopy/g--b"
	coA, err := r.Prepare(ctx, a, ma)
	if err != nil {
		t.Fatal(err)
	}
	coB, err := r.Prepare(ctx, b, mb)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(coA.Worktree, "app.txt"), "from a\n")
	write(t, filepath.Join(coB.Worktree, "app.txt"), "from b\n")
	if _, err := r.Integrate(ctx, a, ma, "A"); err != nil {
		t.Fatal(err)
	}
	_, err = r.Integrate(ctx, b, mb, "B")
	var ce ConflictError
	if !errors.As(err, &ce) || len(ce.Files) != 1 || ce.Files[0] != "app.txt" {
		t.Fatalf("want a conflict in app.txt, got %v", err)
	}
	// The agent resolves it and completes again.
	write(t, filepath.Join(coB.Worktree, "app.txt"), "from a and b\n")
	must(t, coB.Worktree, "add", ".")
	must(t, coB.Worktree, "commit", "--quiet", "--no-edit")
	if _, err := r.Integrate(ctx, b, mb, "B"); err != nil {
		t.Fatal(err)
	}
	if got := must(t, clone, "show", "canopy/g:app.txt"); got != "from a and b" {
		t.Fatalf("goal branch has %q", got)
	}
}

func TestInfo(t *testing.T) {
	_, clone := setup(t)
	info := Info(context.Background(), clone)
	if !info.IsRepo || info.CurrentBranch != "feat/x" || info.DefaultBranch != "main" || info.Dirty != 1 || info.Remote == "" {
		t.Fatalf("info: %+v", info)
	}
	if Info(context.Background(), t.TempDir()).IsRepo {
		t.Fatal("plain folder seen as a repository")
	}
}

// A chat outside a goal has no goal or task branch: it gets a read-only view of the base.
func TestChatWithoutAGoalReadsTheBase(t *testing.T) {
	ctx := context.Background()
	remote, clone := setup(t)
	r := New(t.TempDir(), memBlobs{})
	m := protocol.RepoMount{DirectoryID: uuid.New(), Name: "api", Remote: remote, ClonePath: clone}

	co, err := r.Prepare(ctx, t.TempDir(), m)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(co.Worktree, "app.txt")); string(got) != "one\n" {
		t.Errorf("worktree has %q, want the base branch’s app.txt", got)
	}
	if branches := must(t, clone, "branch", "--list", "canopy/*"); branches != "" {
		t.Errorf("made branches for a chat: %q", branches)
	}
}
