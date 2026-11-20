package orch

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

func rank(mode string) int {
	switch mode {
	case "read_write", "rw":
		return 2
	case "read", "ro":
		return 1
	}
	return 0
}

func minMode(a, b string) string {
	if rank(a) <= rank(b) {
		return a
	}
	return b
}

// OutputsDir is where a member saves a task's outputs, relative to its work folder.
func OutputsDir(taskID uuid.UUID) string { return "outputs/" + taskID.String() }

// mirrorsOn is where the hub copies of folders live on a computer (fetched once per pass).
func (o *Orch) mirrorsOn(ctx context.Context, st *wsState, runner uuid.UUID) map[uuid.UUID]string {
	if o.Mirror == nil {
		return nil
	}
	if st.mirrorCache == nil {
		st.mirrorCache = map[uuid.UUID]map[uuid.UUID]string{}
	}
	if m, ok := st.mirrorCache[runner]; ok {
		return m
	}
	var folders []db.Directory
	for _, d := range st.dirs {
		if d.RepoRemote == "" {
			folders = append(folders, d)
		}
	}
	m := o.Mirror.MountsFor(ctx, runner, folders)
	st.mirrorCache[runner] = m
	return m
}

// mounts computes what a member's sandbox sees on a computer: its granted directories,
// narrowed to what the person the work is for may reach (DECISIONS: Directories).
// Repositories become a worktree of the goal's branch; folders are the real directory on the
// owner's computer, else the hub copy. unavailable names a needed folder not synced yet.
func (o *Orch) mounts(ctx context.Context, st *wsState, runner uuid.UUID, k sessKey, m db.Member, task *db.Task, forUser *uuid.UUID) (mounts []protocol.Mount, unavailable string) {
	attributed := forUser
	if task != nil {
		attributed = task.AttributedUserID
	}
	needed := map[uuid.UUID]bool{}
	if task != nil {
		for _, d := range taskDirs(*task) {
			needed[d.DirectoryID] = true
		}
	}
	names := map[string]bool{}
	for _, g := range st.grants[m.ID] {
		dir, ok := st.dir[g.DirectoryID]
		if !ok {
			continue
		}
		mode := g.Mode
		if attributed != nil {
			mode = minMode(mode, authz.PersonDirMode(dir, *attributed, st.dirAccess[dir.ID]))
		}
		if rank(mode) == 0 {
			continue
		}
		short := map[string]string{"read": "ro", "read_write": "rw"}[mode]
		if dir.RepoRemote != "" {
			mounts = append(mounts, st.repoMount(runner, k, dir, short, task, names))
			continue
		}
		mt := protocol.Mount{Path: dir.Path, Mode: short, DirectoryID: &dir.ID}
		switch mirrors := o.mirrorsOn(ctx, st, runner); {
		case dir.MachineID != nil && *dir.MachineID == runner:
			// The owner's own computer: use the real directory.
		case mirrors[dir.ID] != "":
			mt.Mirror, mt.Source = true, mirrors[dir.ID]
		default:
			if needed[dir.ID] {
				return nil, dir.Name
			}
			continue
		}
		mounts = append(mounts, mt)
	}
	sort.SliceStable(mounts, func(i, j int) bool { return mounts[i].Path < mounts[j].Path })
	return mounts, ""
}

// repoMount checks out a repository for a session. Work for a task gets its own branch off
// the goal's branch; anything else (chat) gets a read-only copy of the goal's branch or base.
func (st *wsState) repoMount(runner uuid.UUID, k sessKey, dir db.Directory, mode string, task *db.Task, names map[string]bool) protocol.Mount {
	base := slug(dir.Name)
	if base == "" {
		base = "repo"
	}
	name := base
	for i := 2; names[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	names[name] = true

	rm := &protocol.RepoMount{DirectoryID: dir.ID, Name: name, Remote: dir.RepoRemote, Setup: dir.SetupCommand, CopyFiles: dir.CopyFiles}
	var owner *uuid.UUID
	if g, ok := st.goal[k.goal]; ok {
		rm.Base, rm.GoalBranch, owner = g.Base, g.Branch, g.CreatedBy
		for _, r := range st.goalRepos[g.ID] {
			if r.DirectoryID == dir.ID {
				rm.BundleKey, rm.BundleTip = r.BundleKey, r.TipSha
				if r.Base != "" {
					rm.Base = r.Base
				}
			}
		}
		rm.Snapshot = g.Snapshot && rm.BundleKey == ""
	} else if task != nil {
		rm.GoalBranch = "canopy/task-" + short(task.ID)
		owner = task.AttributedUserID
	}
	if task != nil && mode == "rw" && rm.GoalBranch != "" {
		rm.TaskBranch = rm.GoalBranch + "--" + short(task.ID)
	}
	// Prefer the clone of the person the work is for; any clone on this computer will do.
	for _, c := range st.clones {
		if c.DirectoryID != dir.ID || c.RunnerID != runner {
			continue
		}
		if rm.ClonePath == "" || (owner != nil && c.UserID == *owner) {
			rm.ClonePath = c.Path
		}
	}
	if rm.Snapshot {
		rm.Snapshot = false
		for _, c := range st.clones {
			if c.DirectoryID == dir.ID && c.RunnerID == runner && owner != nil && c.UserID == *owner && c.Path == rm.ClonePath {
				rm.Snapshot = true
			}
		}
	}
	return protocol.Mount{Path: name, Mode: mode, DirectoryID: &dir.ID, Repo: rm}
}

func mountsKey(ms []protocol.Mount) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		key := m.Path + "|" + m.Mode
		if m.Repo != nil {
			key += "|" + m.Repo.ClonePath
		}
		parts = append(parts, key)
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// isolationNeeded: a directory the old conversation could see isn't in the new mounts, so the
// new work must not inherit that conversation (DECISIONS #9).
func isolationNeeded(old, next []protocol.Mount) bool {
	keep := map[string]bool{}
	for _, m := range next {
		keep[path.Clean(m.Path)] = true
	}
	for _, m := range old {
		if m.DirectoryID != nil && !keep[path.Clean(m.Path)] {
			return true
		}
	}
	return false
}
