// Package outputs watches a task's outputs folder and reports new or changed files
// (SPEC "Outputs → Capture").
package outputs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	w      *fsnotify.Watcher
	cancel context.CancelFunc
}

// Watch calls found(path) for files written to dir or its subfolders, after writes settle.
func Watch(dir string, found func(path string)) (*Watcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := addTree(fw, dir); err != nil {
		fw.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{w: fw, cancel: cancel}
	var mu sync.Mutex
	timers := map[string]*time.Timer{}
	schedule := func(p string) {
		mu.Lock()
		defer mu.Unlock()
		if t := timers[p]; t != nil {
			t.Stop()
		}
		timers[p] = time.AfterFunc(1500*time.Millisecond, func() {
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				found(p)
			}
		})
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				if skip(filepath.Base(ev.Name)) {
					continue
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
					continue
				}
				p := ev.Name
				if fi, err := os.Stat(p); err == nil && fi.IsDir() {
					// New folder: watch it and pick up files already inside.
					_ = addTree(fw, p)
					_ = filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
						if err == nil && d.Type().IsRegular() && !skip(filepath.Base(q)) {
							schedule(q)
						}
						return nil
					})
					continue
				}
				schedule(p)
			case <-fw.Errors:
			}
		}
	}()
	return w, nil
}

func (w *Watcher) Close() {
	if w == nil {
		return
	}
	w.cancel()
	w.w.Close()
}

func skip(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".tmp")
}

// addTree watches dir and every folder under it, except hidden ones.
func addTree(fw *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		return fw.Add(p)
	})
}

// Name is the slash path of p relative to dir. It is false when p is outside dir.
func Name(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
