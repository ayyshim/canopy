// Package mirror syncs shared directories with the hub's copy (DECISIONS: Directories): owner
// machines sync their directories up and apply queued writes; runners keep copies of
// directories that live on other machines and report the changes agents make to them.
package mirror

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"

	"canopy/client/internal/blobs"
	"canopy/shared/protocol"
)

type Sender interface {
	Send(typ string, data any) error
}

type Mirror struct {
	Hub     Sender
	Blobs   *blobs.Client
	DataDir string

	mu     sync.Mutex
	owned  map[uuid.UUID]*tree
	copies map[uuid.UUID]*tree
	h      hasher
}

func New(hub Sender, b *blobs.Client, dataDir string) *Mirror {
	return &Mirror{Hub: hub, Blobs: b, DataDir: dataDir, owned: map[uuid.UUID]*tree{}, copies: map[uuid.UUID]*tree{}}
}

// tree is one synced directory on this machine.
type tree struct {
	id       uuid.UUID
	root     string
	owner    bool
	mu       sync.Mutex
	manifest map[string]protocol.FileEntry
	cancel   context.CancelFunc
}

// Watch sets the directories this machine owns and syncs up.
func (m *Mirror) Watch(dirs []protocol.MirrorDir) {
	m.mu.Lock()
	want := map[uuid.UUID]string{}
	for _, d := range dirs {
		want[d.DirectoryID] = d.Path
	}
	for id, t := range m.owned {
		if p, ok := want[id]; !ok || p != t.root {
			t.cancel()
			delete(m.owned, id)
		}
	}
	var start []*tree
	for id, p := range want {
		if _, ok := m.owned[id]; !ok {
			t := &tree{id: id, root: p, owner: true}
			m.owned[id] = t
			start = append(start, t)
		}
	}
	m.mu.Unlock()
	for _, t := range start {
		go m.syncOwned(t)
	}
}

// Resync sends every owned directory's full tree again (after reconnecting).
func (m *Mirror) Resync() {
	m.mu.Lock()
	var list []*tree
	for _, t := range m.owned {
		list = append(list, t)
	}
	m.mu.Unlock()
	for _, t := range list {
		go m.push(t, true)
	}
}

func (m *Mirror) syncOwned(t *tree) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	if _, err := os.Stat(t.root); err != nil {
		_ = m.Hub.Send(protocol.TMirrorChange, protocol.MirrorChange{DirectoryID: t.id, Full: true, Missing: true})
		return
	}
	m.push(t, true)
	m.watch(ctx, t, func() { m.push(t, false) })
}

// push uploads what changed and tells the hub (the whole tree when full).
func (m *Mirror) push(t *tree, full bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur, _, err := scan(t.root, &m.h)
	if err != nil {
		slog.Warn("scan directory", "path", t.root, "err", err)
		return
	}
	var changed []protocol.FileEntry
	if full || t.manifest == nil {
		for _, f := range cur {
			changed = append(changed, f)
		}
		full = true
	} else {
		changed = diff(t.manifest, cur)
	}
	if !full && len(changed) == 0 {
		return
	}
	if err := m.upload(t.root, changed); err != nil {
		slog.Warn("upload directory changes", "path", t.root, "err", err)
		return
	}
	if err := m.Hub.Send(protocol.TMirrorChange, protocol.MirrorChange{DirectoryID: t.id, Full: full, Files: nonNil(changed)}); err != nil {
		return // resent in full after reconnecting
	}
	t.manifest = cur
}

func nonNil(f []protocol.FileEntry) []protocol.FileEntry {
	if f == nil {
		return []protocol.FileEntry{}
	}
	return f
}

func (m *Mirror) upload(root string, files []protocol.FileEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	byHash := map[string]string{}
	var shas []string
	for _, f := range files {
		if f.Deleted {
			continue
		}
		if _, ok := byHash[f.SHA256]; !ok {
			shas = append(shas, f.SHA256)
		}
		p, _ := safeJoin(root, f.Path)
		byHash[f.SHA256] = p
	}
	for i := 0; i < len(shas); i += 500 {
		batch := shas[i:min(i+500, len(shas))]
		missing, err := m.Blobs.Missing(ctx, batch)
		if err != nil {
			return err
		}
		for _, sha := range missing {
			if _, _, err := m.Blobs.UploadFile(ctx, byHash[sha]); err != nil {
				return err
			}
		}
	}
	return nil
}

// watch calls changed (debounced) whenever something under t.root changes.
func (m *Mirror) watch(ctx context.Context, t *tree, changed func()) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer fw.Close()
	addAll := func() {
		_, dirs, err := scan(t.root, &m.h)
		if err != nil {
			return
		}
		for _, d := range dirs {
			_ = fw.Add(d)
		}
	}
	addAll()
	var timer *time.Timer
	fire := make(chan struct{}, 1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.Events:
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(time.Second, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		case <-fw.Errors:
		case <-fire:
			addAll()
			changed()
		}
	}
}

// Apply lands writes made elsewhere on the owner's disk, refusing any whose file changed
// since the agent's version was based on it.
func (m *Mirror) Apply(a protocol.MirrorApply) protocol.MirrorResult {
	res := protocol.MirrorResult{DirectoryID: a.DirectoryID}
	m.mu.Lock()
	t := m.owned[a.DirectoryID]
	m.mu.Unlock()
	if t == nil {
		return res
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	for _, w := range a.Writes {
		target, ok := safeJoin(t.root, w.Path)
		if !ok {
			res.Outcomes = append(res.Outcomes, protocol.WriteOutcome{ID: w.ID, Status: "conflict"})
			continue
		}
		cur := ""
		if fi, err := os.Stat(target); err == nil && fi.Mode().IsRegular() {
			cur, _ = m.h.hash(target, fi)
		}
		if cur != w.BaseSHA {
			res.Outcomes = append(res.Outcomes, protocol.WriteOutcome{ID: w.ID, Status: "conflict", CurrentSHA: cur})
			continue
		}
		var err error
		if w.NewSHA == "" {
			err = os.Remove(target)
			if os.IsNotExist(err) {
				err = nil
			}
		} else {
			err = m.Blobs.Download(ctx, w.NewSHA, target, os.FileMode(w.Mode))
		}
		if err != nil {
			slog.Warn("apply write", "path", target, "err", err)
			res.Outcomes = append(res.Outcomes, protocol.WriteOutcome{ID: w.ID, Status: "conflict", CurrentSHA: cur})
			continue
		}
		if t.manifest != nil {
			if w.NewSHA == "" {
				delete(t.manifest, w.Path)
			} else {
				t.manifest[w.Path] = protocol.FileEntry{Path: w.Path, SHA256: w.NewSHA, Mode: w.Mode}
			}
		}
		res.Outcomes = append(res.Outcomes, protocol.WriteOutcome{ID: w.ID, Status: "applied", CurrentSHA: w.NewSHA})
	}
	return res
}

// Fetch materializes (Full) or updates this runner's copy of a directory on another machine.
func (m *Mirror) Fetch(f protocol.MirrorFetch) (protocol.MirrorFetched, error) {
	m.mu.Lock()
	t := m.copies[f.DirectoryID]
	if t == nil {
		t = &tree{id: f.DirectoryID, root: filepath.Join(m.DataDir, "mirror", f.DirectoryID.String())}
		m.copies[f.DirectoryID] = t
	}
	m.mu.Unlock()
	t.mu.Lock()
	if err := os.MkdirAll(t.root, 0o755); err != nil {
		t.mu.Unlock()
		return protocol.MirrorFetched{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	local, _, err := scan(t.root, &m.h)
	if err != nil {
		t.mu.Unlock()
		return protocol.MirrorFetched{}, err
	}
	if t.manifest == nil {
		t.manifest = map[string]protocol.FileEntry{}
	}
	want := map[string]bool{}
	for _, e := range f.Files {
		want[e.Path] = true
		target, ok := safeJoin(t.root, e.Path)
		if !ok {
			continue
		}
		if e.Deleted {
			_ = os.Remove(target)
			delete(t.manifest, e.Path)
			continue
		}
		if l, ok := local[e.Path]; !ok || l.SHA256 != e.SHA256 {
			if err := m.Blobs.Download(ctx, e.SHA256, target, os.FileMode(e.Mode)); err != nil {
				t.mu.Unlock()
				return protocol.MirrorFetched{}, err
			}
		}
		t.manifest[e.Path] = e
	}
	if f.Full {
		for p := range local {
			if !want[p] {
				if target, ok := safeJoin(t.root, p); ok {
					_ = os.Remove(target)
				}
				delete(t.manifest, p)
			}
		}
	}
	startWatch := t.cancel == nil
	if startWatch {
		ctx, cancel := context.WithCancel(context.Background())
		t.cancel = cancel
		go m.watch(ctx, t, func() { m.copyChanged(t) })
	}
	t.mu.Unlock()
	return protocol.MirrorFetched{LocalPath: t.root}, nil
}

// copyChanged reports agents' edits to a copy so they queue for the owner's disk.
func (m *Mirror) copyChanged(t *tree) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur, _, err := scan(t.root, &m.h)
	if err != nil {
		return
	}
	changed := diff(t.manifest, cur)
	if len(changed) == 0 {
		return
	}
	if err := m.upload(t.root, changed); err != nil {
		slog.Warn("upload agent changes", "err", err)
		return
	}
	if err := m.Hub.Send(protocol.TMirrorChange, protocol.MirrorChange{DirectoryID: t.id, Files: changed}); err != nil {
		return
	}
	t.manifest = cur
}

// Close stops every watcher.
func (m *Mirror) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.owned {
		if t.cancel != nil {
			t.cancel()
		}
	}
	for _, t := range m.copies {
		if t.cancel != nil {
			t.cancel()
		}
	}
}
