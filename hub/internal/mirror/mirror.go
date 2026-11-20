// Package mirror keeps the hub's copy of every shared directory (DECISIONS: Directories).
// The owner's machine syncs its directory up; runners materialize copies from the hub so
// agents can read them while the owner is offline; changes agents make to a copy queue as
// pending writes and land on the owner's disk when it is online, checked against the version
// they started from.
package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

type Sender interface {
	Online(id uuid.UUID) bool
	Send(id uuid.UUID, typ string, data any) error
	Request(ctx context.Context, id uuid.UUID, typ string, data any, out any) error
}

type Kicker interface {
	Kick(ctx context.Context, wsID uuid.UUID)
}

type Service struct {
	Core   *core.Service
	Store  *store.Store
	R      Sender
	Blobs  blob.Store
	Kicker Kicker

	mu sync.Mutex
	// copies[runner][directory] = where the runner keeps its copy
	copies   map[uuid.UUID]map[uuid.UUID]string
	fetching map[string]bool
}

func New(c *core.Service, r Sender, blobs blob.Store, k Kicker) *Service {
	return &Service{Core: c, Store: c.Store, R: r, Blobs: blobs, Kicker: k,
		copies: map[uuid.UUID]map[uuid.UUID]string{}, fetching: map[string]bool{}}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// RunnerConnected tells an owner machine what to sync, and forgets stale copies.
func (s *Service) RunnerConnected(ctx context.Context, r db.Runner) {
	s.mu.Lock()
	delete(s.copies, r.ID)
	s.mu.Unlock()
	s.Rewatch(ctx, r.ID)
}

// Rewatch sends a machine the current list of directories it shares.
func (s *Service) Rewatch(ctx context.Context, machineID uuid.UUID) {
	if !s.R.Online(machineID) {
		return
	}
	dirs, err := s.Store.ListDirectoriesOnMachine(ctx, &machineID)
	if err != nil {
		return
	}
	watch := protocol.MirrorWatch{Directories: []protocol.MirrorDir{}}
	var folders []db.Directory
	for _, d := range dirs {
		if d.RepoRemote != "" {
			continue // repositories are shared through git, not the hub copy
		}
		folders = append(folders, d)
		watch.Directories = append(watch.Directories, protocol.MirrorDir{DirectoryID: d.ID, Path: d.Path})
		if _, err := s.Store.EnsureMirrorState(ctx, d.ID); err != nil {
			return
		}
	}
	_ = s.R.Send(machineID, protocol.TMirrorWatch, watch)
	for _, d := range folders {
		s.pushQueued(ctx, d)
	}
}

// MountsFor returns, per directory, where a runner keeps an up-to-date copy. Directories on
// another machine whose hub copy is complete but not yet on the runner start fetching.
func (s *Service) MountsFor(ctx context.Context, runnerID uuid.UUID, dirs []db.Directory) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	s.mu.Lock()
	for id, p := range s.copies[runnerID] {
		out[id] = p
	}
	s.mu.Unlock()
	for _, d := range dirs {
		if d.MachineID != nil && *d.MachineID == runnerID {
			continue
		}
		if _, ok := out[d.ID]; ok {
			continue
		}
		state, err := s.Store.GetMirrorState(ctx, d.ID)
		if err != nil || !state.Complete || !s.R.Online(runnerID) {
			continue
		}
		key := runnerID.String() + d.ID.String()
		s.mu.Lock()
		busy := s.fetching[key]
		s.fetching[key] = true
		s.mu.Unlock()
		if !busy {
			go s.fetch(context.WithoutCancel(ctx), runnerID, d)
		}
	}
	return out
}

func toEntries(files []db.MirrorFile) []protocol.FileEntry {
	out := make([]protocol.FileEntry, 0, len(files))
	for _, f := range files {
		out = append(out, protocol.FileEntry{Path: f.Path, SHA256: f.Sha256, Size: f.Size, Mode: uint32(f.Mode)})
	}
	return out
}

// fetch materializes a directory's hub copy on a runner.
func (s *Service) fetch(ctx context.Context, runnerID uuid.UUID, d db.Directory) {
	defer func() {
		s.mu.Lock()
		delete(s.fetching, runnerID.String()+d.ID.String())
		s.mu.Unlock()
	}()
	files, err := s.Store.ListMirrorFiles(ctx, d.ID)
	if err != nil {
		return
	}
	var res protocol.MirrorFetched
	if err := s.R.Request(ctx, runnerID, protocol.TMirrorFetch, protocol.MirrorFetch{DirectoryID: d.ID, Path: d.Path, Full: true, Files: toEntries(files)}, &res); err != nil {
		slog.WarnContext(ctx, "materialize mirror", "directory", d.ID, "err", err)
		return
	}
	s.mu.Lock()
	if s.copies[runnerID] == nil {
		s.copies[runnerID] = map[uuid.UUID]string{}
	}
	s.copies[runnerID][d.ID] = res.LocalPath
	s.mu.Unlock()
	s.Kicker.Kick(ctx, d.WorkspaceID)
}

// Message handles mirror traffic from runners.
func (s *Service) Message(ctx context.Context, r db.Runner, env protocol.Envelope) {
	switch env.Type {
	case protocol.TMirrorChange:
		var ch protocol.MirrorChange
		if json.Unmarshal(env.Data, &ch) != nil {
			return
		}
		d, err := s.Store.GetDirectory(ctx, ch.DirectoryID)
		if err != nil {
			return
		}
		ws, err := s.Store.GetWorkspace(ctx, d.WorkspaceID)
		if err != nil || ws.OrgID != r.OrgID {
			return
		}
		if d.MachineID != nil && *d.MachineID == r.ID {
			s.ownerChanged(ctx, ws, d, ch)
		} else {
			s.copyChanged(ctx, ws, d, r, ch)
		}
	case protocol.TMirrorResult:
		var res protocol.MirrorResult
		if json.Unmarshal(env.Data, &res) == nil {
			s.applied(ctx, r, res)
		}
	}
}

// ownerChanged: the source of truth changed; update the hub copy and every runner's copy.
func (s *Service) ownerChanged(ctx context.Context, ws db.Workspace, d db.Directory, ch protocol.MirrorChange) {
	var rev db.MirrorState
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if _, err := q.EnsureMirrorState(ctx, d.ID); err != nil {
			return err
		}
		var err error
		rev, err = q.BumpMirrorRevision(ctx, db.BumpMirrorRevisionParams{DirectoryID: d.ID, IsFull: ch.Full && !ch.Missing, IsMissing: ch.Missing})
		if err != nil {
			return err
		}
		keep := make([]string, 0, len(ch.Files))
		for _, f := range ch.Files {
			if f.Deleted {
				if err := q.DeleteMirrorFile(ctx, db.DeleteMirrorFileParams{DirectoryID: d.ID, Path: f.Path}); err != nil {
					return err
				}
				continue
			}
			keep = append(keep, f.Path)
			if err := q.UpsertMirrorFile(ctx, db.UpsertMirrorFileParams{DirectoryID: d.ID, Path: f.Path, Sha256: f.SHA256, Size: f.Size,
				Mode: int32(f.Mode), Revision: rev.Revision}); err != nil {
				return err
			}
		}
		if ch.Full {
			if err := q.DeleteMirrorFilesNotIn(ctx, db.DeleteMirrorFilesNotInParams{DirectoryID: d.ID, Keep: keep}); err != nil {
				return err
			}
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.synced"), map[string]any{"directory_id": d.ID, "revision": rev.Revision,
			"complete": rev.Complete, "missing": rev.Missing, "at": rev.LastSyncedAt})
	})
	if err != nil {
		slog.ErrorContext(ctx, "mirror owner change", "err", err)
		return
	}
	// Runners holding a copy get the change (a full refresh after a full sync).
	s.mu.Lock()
	var holders []uuid.UUID
	for runnerID, copies := range s.copies {
		if _, ok := copies[d.ID]; ok {
			holders = append(holders, runnerID)
		}
	}
	s.mu.Unlock()
	for _, runnerID := range holders {
		if ch.Full {
			go s.fetch(context.WithoutCancel(ctx), runnerID, d)
		} else {
			_ = s.R.Send(runnerID, protocol.TMirrorFetch, protocol.MirrorFetch{DirectoryID: d.ID, Path: d.Path, Files: ch.Files})
		}
	}
	if rev.Complete {
		s.Kicker.Kick(ctx, ws.ID)
	}
	s.pushQueued(ctx, d)
}

// copyChanged: an agent changed a runner's copy. Record writes for the owner's disk.
func (s *Service) copyChanged(ctx context.Context, ws db.Workspace, d db.Directory, r db.Runner, ch protocol.MirrorChange) {
	s.mu.Lock()
	_, holds := s.copies[r.ID][d.ID]
	s.mu.Unlock()
	if !holds || ch.Full {
		return
	}
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		rev, err := q.BumpMirrorRevision(ctx, db.BumpMirrorRevisionParams{DirectoryID: d.ID})
		if err != nil {
			return err
		}
		for _, f := range ch.Files {
			base := ""
			if cur, err := q.GetMirrorFile(ctx, db.GetMirrorFileParams{DirectoryID: d.ID, Path: f.Path}); err == nil {
				base = cur.Sha256
			} else if !store.IsNotFound(err) {
				return err
			}
			newSHA := f.SHA256
			if f.Deleted {
				newSHA = ""
				if base == "" {
					continue
				}
				if err := q.DeleteMirrorFile(ctx, db.DeleteMirrorFileParams{DirectoryID: d.ID, Path: f.Path}); err != nil {
					return err
				}
			} else {
				if base == newSHA {
					continue
				}
				if err := q.UpsertMirrorFile(ctx, db.UpsertMirrorFileParams{DirectoryID: d.ID, Path: f.Path, Sha256: newSHA, Size: f.Size,
					Mode: int32(f.Mode), Revision: rev.Revision}); err != nil {
					return err
				}
			}
			if _, err := q.CreatePendingWrite(ctx, db.CreatePendingWriteParams{ID: uuid.Must(uuid.NewV7()), DirectoryID: d.ID, Path: f.Path,
				BaseSha: base, NewSha: newSHA, Mode: int32(f.Mode)}); err != nil {
				return err
			}
		}
		n, err := q.CountQueuedWrites(ctx, d.ID)
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.synced"), map[string]any{"directory_id": d.ID, "revision": rev.Revision, "queued_writes": n})
	})
	if err != nil {
		slog.ErrorContext(ctx, "mirror copy change", "err", err)
		return
	}
	s.pushQueued(ctx, d)
}

// pushQueued sends queued writes to the owner's machine when it is online.
func (s *Service) pushQueued(ctx context.Context, d db.Directory) {
	if d.MachineID == nil || !s.R.Online(*d.MachineID) {
		return
	}
	writes, err := s.Store.ListQueuedWrites(ctx, d.ID)
	if err != nil || len(writes) == 0 {
		return
	}
	msg := protocol.MirrorApply{DirectoryID: d.ID}
	for _, w := range writes {
		msg.Writes = append(msg.Writes, protocol.PendingWrite{ID: w.ID, Path: w.Path, BaseSHA: w.BaseSha, NewSHA: w.NewSha, Mode: uint32(w.Mode)})
	}
	_ = s.R.Send(*d.MachineID, protocol.TMirrorApply, msg)
}

// applied records the owner's outcome; conflicts go to the owner to decide.
func (s *Service) applied(ctx context.Context, r db.Runner, res protocol.MirrorResult) {
	d, err := s.Store.GetDirectory(ctx, res.DirectoryID)
	if err != nil || d.MachineID == nil || *d.MachineID != r.ID {
		return
	}
	ws, err := s.Store.GetWorkspace(ctx, d.WorkspaceID)
	if err != nil {
		return
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		for _, o := range res.Outcomes {
			w, err := q.GetPendingWrite(ctx, o.ID)
			if err != nil || w.DirectoryID != d.ID || w.Status != "queued" {
				continue
			}
			status := "applied"
			if o.Status == "conflict" {
				status = "conflict"
			}
			if _, err := q.SetPendingWriteStatus(ctx, db.SetPendingWriteStatusParams{ID: w.ID, Status: status, CurrentSha: o.CurrentSHA}); err != nil {
				return err
			}
			if status != "conflict" {
				continue
			}
			sig, err := q.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: ws.ID, Type: "sync_conflict",
				Payload: mustJSON(map[string]any{"directory_id": d.ID, "path": w.Path, "dir_path": d.Path, "write_id": w.ID, "owner_user_id": d.OwnerUserID,
					"owner_sha": o.CurrentSHA, "agent_sha": w.NewSha})})
			if err != nil {
				return err
			}
			e := core.WS(ws.OrgID, ws.ID, "signal.opened")
			e.ForUserID = &d.OwnerUserID
			if err := t.Emit(ctx, e, core.ToSignal(sig)); err != nil {
				return err
			}
		}
		n, err := q.CountQueuedWrites(ctx, d.ID)
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.WS(ws.OrgID, ws.ID, "directory.synced"), map[string]any{"directory_id": d.ID, "queued_writes": n})
	})
	if err != nil {
		slog.ErrorContext(ctx, "mirror result", "err", err)
	}
}

// ResolveConflict: keep the owner's version (drop the agent's write) or apply the agent's
// version over the owner's current file.
func (s *Service) ResolveConflict(ctx context.Context, t *core.Tx, sig db.Signal, keepAgent bool) error {
	var p map[string]any
	_ = json.Unmarshal(sig.Payload, &p)
	writeID, err := uuid.Parse(fmt.Sprint(p["write_id"]))
	if err != nil {
		return err
	}
	w, err := t.Q.GetPendingWrite(ctx, writeID)
	if err != nil {
		return err
	}
	if !keepAgent {
		_, err := t.Q.SetPendingWriteStatus(ctx, db.SetPendingWriteStatusParams{ID: w.ID, Status: "discarded", CurrentSha: w.CurrentSha})
		return err
	}
	// Re-queue on top of the owner's current version.
	if _, err := t.Q.CreatePendingWrite(ctx, db.CreatePendingWriteParams{ID: uuid.Must(uuid.NewV7()), DirectoryID: w.DirectoryID, Path: w.Path,
		BaseSha: w.CurrentSha, NewSha: w.NewSha, Mode: w.Mode}); err != nil {
		return err
	}
	if _, err := t.Q.SetPendingWriteStatus(ctx, db.SetPendingWriteStatusParams{ID: w.ID, Status: "discarded", CurrentSha: w.CurrentSha}); err != nil {
		return err
	}
	d, err := t.Q.GetDirectory(ctx, w.DirectoryID)
	if err != nil {
		return err
	}
	t.AfterCommit(func() { s.pushQueued(context.WithoutCancel(ctx), d) })
	return nil
}

var _ = strings.TrimSpace
