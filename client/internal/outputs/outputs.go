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

// Watch calls found(path) for files written to dir, after writes settle.
func Watch(dir string, found func(path string)) (*Watcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(dir); err != nil {
		fw.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{w: fw, cancel: cancel}
	var mu sync.Mutex
	timers := map[string]*time.Timer{}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				name := filepath.Base(ev.Name)
				if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".tmp") {
					continue
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
					continue
				}
				p := ev.Name
				mu.Lock()
				if t := timers[p]; t != nil {
					t.Stop()
				}
				timers[p] = time.AfterFunc(1500*time.Millisecond, func() {
					if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
						found(p)
					}
				})
				mu.Unlock()
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
