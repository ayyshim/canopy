// Package runner is one paired hub connection: it answers the hub's requests, runs sessions
// and syncs directories.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"canopy/client/internal/blobs"
	"canopy/client/internal/config"
	"canopy/client/internal/conn"
	"canopy/client/internal/detect"
	"canopy/client/internal/gitwork"
	"canopy/client/internal/mirror"
	"canopy/client/internal/session"
	"canopy/shared/protocol"
)

type Runner struct {
	Hub      config.Hub
	Conn     *conn.Conn
	Sessions *session.Manager
	Mirror   *mirror.Mirror
	Version  string
	Sandbox  string
}

func New(h config.Hub, version, sandboxKind, canopyBin string) (*Runner, error) {
	tlsCfg, err := h.TLS()
	if err != nil {
		return nil, fmt.Errorf("load this machine’s certificate for %s: %w", h.URL, err)
	}
	c := &conn.Conn{URL: h.RunnerURL, TLS: tlsCfg}
	bc := &blobs.Client{Base: blobs.BaseFromRunnerURL(h.RunnerURL), HTTP: c.HTTPClient()}
	data := filepath.Join(config.DataDir(), filepath.Base(h.Dir))
	r := &Runner{Hub: h, Conn: c, Version: version, Sandbox: sandboxKind,
		Sessions: session.New(c, bc, data, sandboxKind, canopyBin), Mirror: mirror.New(c, bc, data)}
	return r, nil
}

func (r *Runner) Run(ctx context.Context) {
	r.Conn.Run(ctx, r)
	r.Sessions.StopAll()
	r.Mirror.Close()
}

func (r *Runner) Hello() protocol.Hello {
	host, _ := os.Hostname()
	return protocol.Hello{Version: protocol.Version, ClientVersion: r.Version, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
		CLIs: detect.CLIs(), Sandbox: r.Sandbox, Sessions: r.Sessions.Live()}
}

func (r *Runner) Connected(ctx context.Context, c *conn.Conn) { r.Mirror.Resync() }

func decode[T any](env protocol.Envelope) (T, bool) {
	var v T
	if err := json.Unmarshal(env.Data, &v); err != nil {
		slog.Warn("bad message from hub", "type", env.Type, "err", err)
		return v, false
	}
	return v, true
}

func (r *Runner) Handle(ctx context.Context, c *conn.Conn, env protocol.Envelope) {
	switch env.Type {
	case protocol.TStartSession:
		if m, ok := decode[protocol.StartSession](env); ok {
			r.Sessions.Start(m)
		}
	case protocol.TSendInput:
		if m, ok := decode[protocol.SendInput](env); ok {
			r.Sessions.SendInput(m)
		}
	case protocol.TSleep:
		if m, ok := decode[protocol.SessionRef](env); ok {
			r.Sessions.Sleep(m.SessionID)
		}
	case protocol.TStop:
		if m, ok := decode[protocol.SessionRef](env); ok {
			r.Sessions.Stop(m.SessionID)
		}
	case protocol.TApplyGrants:
		if m, ok := decode[protocol.ApplyGrants](env); ok {
			r.Sessions.ApplyGrants(m)
		}
	case protocol.TToolResult:
		if m, ok := decode[protocol.ToolResult](env); ok {
			r.Sessions.ToolResult(m)
		}
	case protocol.TSetAPIKey:
		if m, ok := decode[protocol.SetAPIKey](env); ok {
			if err := config.SaveKey(m.CLI, m.Key); err != nil {
				slog.Error("save API key", "cli", m.CLI, "err", err)
			} else {
				slog.Info("saved API key", "cli", m.CLI)
			}
		}
	case protocol.TBrowse:
		m, ok := decode[protocol.Browse](env)
		if !ok {
			return
		}
		res, err := browse(m.Path)
		_ = c.Reply(env, res, err)
	case protocol.TRepoInfo:
		if m, ok := decode[protocol.RepoInfo](env); ok {
			_ = c.Reply(env, gitwork.Info(ctx, expandHome(m.Path)), nil)
		}
	case protocol.TGitShip:
		if m, ok := decode[protocol.GitShip](env); ok {
			// Pushing can take a while; don't hold up other messages.
			go func() { _ = c.Reply(env, r.Sessions.Repos.Ship(context.WithoutCancel(ctx), m), nil) }()
		}
	case protocol.TGoalCleanup:
		if m, ok := decode[protocol.GoalCleanup](env); ok {
			go r.Sessions.Repos.Cleanup(context.WithoutCancel(ctx), r.Sessions.WorkRoot(), m.GoalID.String())
		}
	case protocol.TMirrorWatch:
		if m, ok := decode[protocol.MirrorWatch](env); ok {
			r.Mirror.Watch(m.Directories)
		}
	case protocol.TMirrorApply:
		if m, ok := decode[protocol.MirrorApply](env); ok {
			_ = c.Send(protocol.TMirrorResult, r.Mirror.Apply(m))
		}
	case protocol.TMirrorFetch:
		m, ok := decode[protocol.MirrorFetch](env)
		if !ok {
			return
		}
		res, err := r.Mirror.Fetch(m)
		if env.ID != "" {
			_ = c.Reply(env, res, err)
		} else if err != nil {
			slog.Warn("update mirrored directory", "err", err)
		}
	}
}

func expandHome(p string) string {
	home, _ := os.UserHomeDir()
	if p == "" || p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}

// browse lists folders for the directory picker (hidden folders left out), marking git
// repositories.
func browse(p string) (protocol.BrowseResult, error) {
	home, _ := os.UserHomeDir()
	p = expandHome(p)
	entries, err := os.ReadDir(p)
	if err != nil {
		return protocol.BrowseResult{}, fmt.Errorf("can’t open %s", p)
	}
	res := protocol.BrowseResult{Path: p, Parent: filepath.Dir(p), Home: home, Entries: []protocol.BrowseEntry{}}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(p, e.Name())
		res.Entries = append(res.Entries, protocol.BrowseEntry{Name: e.Name(), Path: full, IsDir: true, IsRepo: gitwork.IsRepoRoot(full)})
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	return res, nil
}
