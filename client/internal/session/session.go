// Package session runs agent sessions: one CLI process per member and goal (or person), fed
// one turn at a time, inside the sandbox, with Canopy's tools bridged to the hub.
package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"canopy/client/internal/adapters/claudecode"
	"canopy/client/internal/blobs"
	"canopy/client/internal/config"
	"canopy/client/internal/detect"
	"canopy/client/internal/gitwork"
	"canopy/client/internal/netproxy"
	"canopy/client/internal/outputs"
	"canopy/client/internal/sandbox"
	"canopy/client/internal/tools"
	"canopy/shared/protocol"
)

type Sender interface {
	Send(typ string, data any) error
}

type Manager struct {
	Hub       Sender
	Blobs     *blobs.Client
	Repos     *gitwork.Repos
	DataDir   string
	Sandbox   string // bwrap or none
	CanopyBin string
	Hostname  string

	mu       sync.Mutex
	sessions map[uuid.UUID]*Session
}

func New(hub Sender, b *blobs.Client, dataDir, sandboxKind, canopyBin string) *Manager {
	host, _ := os.Hostname()
	return &Manager{Hub: hub, Blobs: b, Repos: gitwork.New(dataDir, b), DataDir: dataDir, Sandbox: sandboxKind, CanopyBin: canopyBin,
		Hostname: host, sessions: map[uuid.UUID]*Session{}}
}

// WorkRoot holds every session's work folder (goal-<id>/<member>, person-<id>/<member>).
func (m *Manager) WorkRoot() string { return filepath.Join(m.DataDir, "work") }

func (m *Manager) send(typ string, data any) {
	if err := m.Hub.Send(typ, data); err != nil {
		slog.Debug("send to hub", "type", typ, "err", err)
	}
}

func (m *Manager) get(id uuid.UUID) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Live lists running sessions (sent in hello so the hub can tell what survived).
func (m *Manager) Live() []protocol.LiveSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []protocol.LiveSession{}
	for id, s := range m.sessions {
		s.mu.Lock()
		if s.running {
			st := protocol.SessionWaiting
			if s.current != nil {
				st = protocol.SessionRunning
			}
			out = append(out, protocol.LiveSession{SessionID: id, CLISessionID: s.cliSID, Status: st})
		}
		s.mu.Unlock()
	}
	return out
}

// Start launches (or relaunches) a session.
func (m *Manager) Start(msg protocol.StartSession) {
	if old := m.get(msg.SessionID); old != nil {
		old.terminate("replace")
		old.waitExit(10 * time.Second)
	}
	s := &Session{m: m, id: msg.SessionID, spec: msg, pending: map[string]*pendingCall{}, usageSeen: map[string]bool{},
		dir: filepath.Join(m.DataDir, "sessions", msg.SessionID.String())}
	m.mu.Lock()
	m.sessions[msg.SessionID] = s
	m.mu.Unlock()
	if msg.Turn != nil {
		s.queue = append(s.queue, *msg.Turn)
	}
	if err := s.launch(msg.Resume); err != nil {
		m.send(protocol.TSessionEvent, protocol.SessionEvent{SessionID: s.id, Status: protocol.SessionError, Error: err.Error()})
		m.drop(s.id)
	}
}

func (m *Manager) drop(id uuid.UUID) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) SendInput(in protocol.SendInput) {
	s := m.get(in.SessionID)
	if s == nil {
		// The hub thinks this session is alive; it isn't here (e.g. the runner restarted).
		m.send(protocol.TSessionEvent, protocol.SessionEvent{SessionID: in.SessionID, Status: protocol.SessionCrashed, Error: "The session wasn’t running on " + m.Hostname + "."})
		return
	}
	go func() {
		// A new task may need its own branch first; the worktree is switched in place.
		if len(in.Mounts) > 0 {
			if _, err := s.prepareRepos(in.Mounts); err != nil {
				s.log("error", err.Error())
			}
			s.mu.Lock()
			s.spec.Mounts = in.Mounts
			s.mu.Unlock()
		}
		s.mu.Lock()
		s.queue = append(s.queue, in.Turn)
		s.mu.Unlock()
		s.pump()
	}()
}

func (m *Manager) Sleep(id uuid.UUID) {
	if s := m.get(id); s != nil {
		s.terminate("sleep")
	} else {
		m.send(protocol.TSessionEvent, protocol.SessionEvent{SessionID: id, Status: protocol.SessionAsleep})
	}
}

func (m *Manager) Stop(id uuid.UUID) {
	if s := m.get(id); s != nil {
		s.terminate("stop")
	}
}

// ApplyGrants restarts a session with new mounts, resuming its conversation.
func (m *Manager) ApplyGrants(g protocol.ApplyGrants) {
	s := m.get(g.SessionID)
	if s == nil {
		m.send(protocol.TSessionEvent, protocol.SessionEvent{SessionID: g.SessionID, Status: protocol.SessionCrashed, Error: "The session wasn’t running."})
		return
	}
	s.mu.Lock()
	s.spec.Mounts, s.spec.Work = g.Mounts, g.Work
	if g.Turn != nil {
		s.queue = append([]protocol.Turn{*g.Turn}, s.queue...)
	}
	s.mu.Unlock()
	s.terminate("restart")
}

func (m *Manager) ToolResult(r protocol.ToolResult) {
	if s := m.get(r.SessionID); s != nil {
		s.toolResult(r)
	}
}

// StopAll ends every session (runner shutting down).
func (m *Manager) StopAll() {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.terminate("shutdown")
		s.waitExit(10 * time.Second)
	}
}

type pendingCall struct {
	conn   net.Conn
	wmu    *sync.Mutex
	callID string
	tool   string
	input  json.RawMessage // permission: the original tool input
}

type Session struct {
	m       *Manager
	id      uuid.UUID
	dir     string
	sockDir string // short path: Unix socket paths are limited to 108 bytes
	spec    protocol.StartSession

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	running   bool
	exited    chan struct{}
	cliSID    string
	sawInit   bool
	queue     []protocol.Turn
	current   *protocol.Turn
	stopping  string
	pending   map[string]*pendingCall
	usageSeen map[string]bool
	toolLn    net.Listener
	proxyLn   net.Listener
	proxy     *netproxy.Proxy
	outWatch  *outputs.Watcher
	outTask   *uuid.UUID
	stderr    []string
	visible   []protocol.Mount // what the sandbox shows: the work folder, folders, clones' .git
}

// workDir is the session's folder: its cwd, its repositories' worktrees and its outputs.
func (s *Session) workDir() string {
	if s.spec.Work == "" {
		return filepath.Join(s.dir, "work")
	}
	return filepath.Join(s.m.WorkRoot(), filepath.Clean("/"+s.spec.Work))
}

// prepareRepos checks out each repository mount and returns what the sandbox must show.
func (s *Session) prepareRepos(mounts []protocol.Mount) ([]protocol.Mount, error) {
	work := s.workDir()
	visible := []protocol.Mount{{Path: work, Mode: "rw"}}
	seen := map[string]bool{work: true}
	for _, mt := range mounts {
		if mt.Repo == nil {
			visible = append(visible, mt)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		co, err := s.m.Repos.Prepare(ctx, work, *mt.Repo)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("couldn’t check out %s: %w", mt.Repo.Name, err)
		}
		if !seen[co.GitDir] {
			// Commits from the worktree go into the clone's .git folder.
			visible = append(visible, protocol.Mount{Path: co.GitDir, Mode: "rw"})
			seen[co.GitDir] = true
		}
	}
	return visible, nil
}

func (s *Session) report(ev protocol.SessionEvent) {
	ev.SessionID = s.id
	s.m.send(protocol.TSessionEvent, ev)
}

func (s *Session) log(kind, text string) {
	s.mu.Lock()
	turn := ""
	if s.current != nil {
		turn = s.current.TurnID
	}
	s.mu.Unlock()
	s.m.send(protocol.TLogLine, protocol.LogLine{SessionID: s.id, TurnID: turn, Kind: kind, Text: text})
}

// homeBinds are the CLI's own config and state, shared with the person's own use of it.
func homeBinds(cli string) (ro, rw []string) {
	home, _ := os.UserHomeDir()
	switch cli {
	case "claude":
		dir := os.Getenv("CLAUDE_CONFIG_DIR")
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		_ = os.MkdirAll(dir, 0o700)
		rw = append(rw, dir, filepath.Join(home, ".claude.json"), filepath.Join(home, ".cache", "claude"), filepath.Join(home, ".cache", "claude-cli-nodejs"))
	}
	return ro, rw
}

func (s *Session) launch(resume string) error {
	spec := s.spec
	if spec.CLI != "claude" {
		return fmt.Errorf("%s isn’t supported by this version of Canopy", spec.CLI)
	}
	bin := detect.ResolveCLI(spec.CLI)
	if bin == "" {
		return fmt.Errorf("Claude Code isn’t installed on %s. Install it with: npm install -g @anthropic-ai/claude-code", s.m.Hostname)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	work := s.workDir()
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	visible, err := s.prepareRepos(spec.Mounts)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.visible = visible
	s.mu.Unlock()
	s.sockDir = filepath.Join(runtimeDir(), strings.ReplaceAll(s.id.String(), "-", "")[:16])
	if err := os.MkdirAll(s.sockDir, 0o700); err != nil {
		return err
	}
	sock := filepath.Join(s.sockDir, "tools.sock")
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	s.toolLn = ln
	go s.acceptBridge(ln)

	var proxySock string
	if s.m.Sandbox == "bwrap" {
		proxySock = filepath.Join(s.sockDir, "proxy.sock")
		_ = os.Remove(proxySock)
		pl, err := net.Listen("unix", proxySock)
		if err != nil {
			return err
		}
		s.proxyLn = pl
		s.proxy = netproxy.New(spec.AllowedHosts...)
		go s.proxy.Serve(pl)
	}
	mcpPath := filepath.Join(s.dir, "mcp.json")
	if err := os.WriteFile(mcpPath, claudecode.MCPConfig(s.m.CanopyBin, sock), 0o600); err != nil {
		return err
	}
	var addDirs []string
	for _, mt := range spec.Mounts {
		if mt.Repo == nil {
			addDirs = append(addDirs, mt.Path)
		}
	}
	argv := append([]string{bin}, claudecode.Args(claudecode.Params{Model: spec.Model, AppendSystemPrompt: spec.AppendSystemPrompt,
		Resume: resume, MCPConfig: mcpPath, AddDirs: addDirs})...)
	env := baseEnv(s.id.String())
	env = append(env, gitIdentity(spec.MemberName)...)
	if spec.UseAPIKey {
		key := config.ReadKey(spec.CLI)
		if key == "" {
			return fmt.Errorf("No API key for Claude Code is stored on %s. Add it in Canopy’s CLI connections.", s.m.Hostname)
		}
		env = append(env, "ANTHROPIC_API_KEY="+key)
	}
	ro, rw := homeBinds(spec.CLI)
	ro = append(ro, filepath.Dir(bin), s.m.CanopyBin)
	rw = append(rw, s.dir, s.sockDir)
	cmd, err := sandbox.Command(context.Background(), sandbox.Spec{Kind: s.m.Sandbox, Mounts: visible, Cwd: work,
		ReadOnly: ro, ReadWrite: rw, Env: env, ProxySocket: proxySock, CanopyBin: s.m.CanopyBin}, argv)
	if err != nil {
		return err
	}
	if s.m.Sandbox == "none" && !spec.UseAPIKey {
		cmd.Env = withoutVar(cmd.Env, "ANTHROPIC_API_KEY")
	}
	// An inherited value would switch Artifact off.
	cmd.Env = withoutVar(cmd.Env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Claude Code: %w", err)
	}
	s.mu.Lock()
	s.cmd, s.stdin, s.running, s.exited, s.sawInit, s.stopping, s.stderr = cmd, stdin, true, make(chan struct{}), false, "", nil
	s.usageSeen = map[string]bool{} // message ids are per process
	s.mu.Unlock()
	slog.Info("session started", "session", s.id, "member", spec.MemberName, "sandbox", s.m.Sandbox, "resume", resume != "")
	go s.readStderr(stderr)
	go s.readStdout(stdout)
	go s.wait()
	s.pump()
	return nil
}

func withoutVar(env []string, name string) []string {
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, name+"=") {
			out = append(out, e)
		}
	}
	return out
}

// pump writes the next queued turn when the CLI is free.
func (s *Session) pump() {
	s.mu.Lock()
	if !s.running || s.current != nil || len(s.queue) == 0 || s.stopping != "" {
		s.mu.Unlock()
		return
	}
	turn := s.queue[0]
	s.queue = s.queue[1:]
	s.current = &turn
	stdin := s.stdin
	s.mu.Unlock()
	if turn.TaskID != nil {
		s.watchOutputs(*turn.TaskID)
	}
	if _, err := stdin.Write(claudecode.UserMessage(turn.Text)); err != nil {
		slog.Warn("write turn", "session", s.id, "err", err)
		return
	}
	s.report(protocol.SessionEvent{Status: protocol.SessionRunning, TurnID: turn.TurnID, CLISessionID: s.cliSessionID()})
}

func (s *Session) cliSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cliSID
}

// outputName is p's slash path under dir, or just its file name when it is elsewhere.
func outputName(dir, p string) string {
	if n, ok := outputs.Name(dir, p); ok {
		return n
	}
	return filepath.Base(p)
}

// submitName keeps the folder path for files inside the current task's outputs folder.
func (s *Session) submitName(p string) string {
	s.mu.Lock()
	t := s.outTask
	s.mu.Unlock()
	if t == nil {
		return filepath.Base(p)
	}
	return outputName(filepath.Join(s.workDir(), "outputs", t.String()), p)
}

// watchOutputs reports files saved to the task's outputs folder.
func (s *Session) watchOutputs(taskID uuid.UUID) {
	s.mu.Lock()
	if s.outTask != nil && *s.outTask == taskID {
		s.mu.Unlock()
		return
	}
	if s.outWatch != nil {
		s.outWatch.Close()
	}
	dir := filepath.Join(s.workDir(), "outputs", taskID.String())
	s.outTask = &taskID
	s.mu.Unlock()
	w, err := outputs.Watch(dir, func(p string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sha, size, err := s.m.Blobs.UploadFile(ctx, p)
		if err != nil {
			slog.Warn("upload output", "file", p, "err", err)
			return
		}
		s.m.send(protocol.TFileWritten, protocol.FileWritten{SessionID: s.id, TaskID: &taskID, Path: p, Name: outputName(dir, p), Size: size,
			SHA256: sha, Mime: blobs.MimeOf(p), BlobKey: blobs.Key(sha)})
	})
	if err != nil {
		slog.Warn("watch outputs", "dir", dir, "err", err)
		return
	}
	s.mu.Lock()
	s.outWatch = w
	s.mu.Unlock()
}

func (s *Session) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		s.mu.Lock()
		s.stderr = append(s.stderr, line)
		if len(s.stderr) > 20 {
			s.stderr = s.stderr[len(s.stderr)-20:]
		}
		s.mu.Unlock()
	}
}

func (s *Session) readStdout(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		evs, err := claudecode.Parse(sc.Bytes())
		if err != nil {
			continue
		}
		for _, e := range evs {
			s.onEvent(e)
		}
	}
}

func (s *Session) onEvent(e claudecode.Event) {
	switch e.Kind {
	case "init":
		s.mu.Lock()
		s.cliSID, s.sawInit = e.SessionID, true
		s.mu.Unlock()
		s.report(protocol.SessionEvent{Status: protocol.SessionRunning, CLISessionID: e.SessionID})
	case "text":
		s.log("agent", e.Text)
	case "tool":
		s.log("tool", e.Text)
	case "usage":
		s.mu.Lock()
		dup := e.MessageID != "" && s.usageSeen[e.MessageID]
		s.usageSeen[e.MessageID] = true
		var turn protocol.Turn
		if s.current != nil {
			turn = *s.current
		}
		s.mu.Unlock()
		if dup {
			return
		}
		taskID := turn.TaskID
		if taskID == nil {
			taskID = turn.CommentTaskID // a reply in a task's thread is that task's work
		}
		s.m.send(protocol.TUsage, protocol.Usage{SessionID: s.id, TurnID: turn.TurnID, AttributedUserID: turn.AttributedUserID,
			CLI: s.spec.CLI, Model: e.Model, TokensIn: e.TokensIn, TokensOut: e.TokensOut,
			CacheRead: e.CacheRead, CacheWrite: e.CacheWrite, TaskID: taskID})
	case "rate_limit":
		var windows map[string]protocol.RateWindow
		if len(e.Windows) > 0 {
			windows = make(map[string]protocol.RateWindow, len(e.Windows))
			for k, w := range e.Windows {
				windows[k] = protocol.RateWindow{Utilization: w.Utilization, ResetsAt: w.ResetsAt, Status: w.Status}
			}
		}
		s.m.send(protocol.TRateLimit, protocol.RateLimit{SessionID: s.id, CLI: s.spec.CLI, Limited: e.Limited, ResetsAt: e.ResetsAt,
			Utilization: e.Utilization, Windows: windows, Message: e.Text})
	case "result":
		s.mu.Lock()
		turn := s.current
		s.current = nil
		if e.SessionID != "" {
			s.cliSID = e.SessionID
		}
		s.mu.Unlock()
		ev := protocol.SessionEvent{Status: protocol.SessionWaiting, ResultText: e.Text, IsError: e.IsError, CLISessionID: s.cliSessionID()}
		if turn != nil {
			ev.TurnID, ev.TurnKind, ev.TaskID, ev.CommentTaskID = turn.TurnID, turn.Kind, turn.TaskID, turn.CommentTaskID
		}
		s.report(ev)
		s.pump()
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.running = false
	stopping, sawInit, tail, cliSID := s.stopping, s.sawInit, strings.Join(s.stderr, "\n"), s.cliSID
	interrupted := s.current
	s.current = nil
	if interrupted != nil && stopping == "restart" {
		// The turn in flight is redelivered by the continue turn the hub queued.
		_ = interrupted
	}
	pending := s.pending
	s.pending = map[string]*pendingCall{}
	if s.outWatch != nil {
		s.outWatch.Close()
		s.outWatch, s.outTask = nil, nil
	}
	if s.toolLn != nil {
		s.toolLn.Close()
	}
	if s.proxyLn != nil {
		s.proxyLn.Close()
	}
	close(s.exited)
	s.mu.Unlock()
	for _, p := range pending {
		writeResult(p, tools.Result{CallID: p.callID, Text: "The session stopped.", IsError: true})
	}
	switch stopping {
	case "restart":
		if err := s.launch(cliSID); err != nil {
			s.report(protocol.SessionEvent{Status: protocol.SessionError, Error: err.Error()})
			s.m.drop(s.id)
		}
		return
	case "replace":
		return
	}
	s.m.drop(s.id)
	switch stopping {
	case "sleep":
		s.report(protocol.SessionEvent{Status: protocol.SessionAsleep, CLISessionID: cliSID})
	case "stop", "shutdown":
		if stopping == "stop" {
			s.report(protocol.SessionEvent{Status: protocol.SessionEnded, CLISessionID: cliSID})
		}
	default:
		msg := lastLines(tail, 6)
		if !sawInit {
			if msg == "" && err != nil {
				msg = err.Error()
			}
			s.report(protocol.SessionEvent{Status: protocol.SessionError, Error: "Claude Code didn’t start: " + msg})
			return
		}
		s.report(protocol.SessionEvent{Status: protocol.SessionCrashed, CLISessionID: cliSID, Error: msg})
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// terminate stops the process: close stdin, then signal the process group.
func (s *Session) terminate(reason string) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		if reason == "sleep" {
			s.report(protocol.SessionEvent{Status: protocol.SessionAsleep, CLISessionID: s.cliSID})
		}
		return
	}
	s.stopping = reason
	cmd, stdin := s.cmd, s.stdin
	s.mu.Unlock()
	_ = stdin.Close()
	go func() {
		select {
		case <-s.exited:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-s.exited:
			case <-time.After(5 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}()
}

func (s *Session) waitExit(d time.Duration) {
	s.mu.Lock()
	ch := s.exited
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-time.After(d):
	}
}

// ---- tool bridge ----

func (s *Session) acceptBridge(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveBridge(c)
	}
}

func (s *Session) serveBridge(c net.Conn) {
	defer c.Close()
	wmu := &sync.Mutex{}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var call tools.Call
		if json.Unmarshal(sc.Bytes(), &call) != nil {
			continue
		}
		go s.forward(c, wmu, call)
	}
}

func writeResult(p *pendingCall, r tools.Result) {
	b, _ := json.Marshal(r)
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, _ = p.conn.Write(append(b, '\n'))
}

// forward passes a tool call to the hub, preparing what only this machine can (file uploads).
func (s *Session) forward(c net.Conn, wmu *sync.Mutex, call tools.Call) {
	hubCallID := uuid.NewString()
	p := &pendingCall{conn: c, wmu: wmu, callID: call.CallID, tool: call.Tool}
	args := call.Args
	switch call.Tool {
	case protocol.ToolPermission:
		var in struct {
			Input json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(args, &in)
		p.input = in.Input
	case protocol.ToolSubmitOutput, protocol.ToolAttachFile:
		args = s.prepareOutput(args)
	case protocol.ToolCompleteTask:
		args = s.integrate(args)
	}
	s.mu.Lock()
	s.pending[hubCallID] = p
	turn := ""
	if s.current != nil {
		turn = s.current.TurnID
	}
	s.mu.Unlock()
	s.m.send(protocol.TToolCall, protocol.ToolCall{SessionID: s.id, TurnID: turn, CallID: hubCallID, Tool: call.Tool, Args: args})
}

// prepareOutput uploads the file an agent submitted and adds its hash to the call.
func (s *Session) prepareOutput(raw json.RawMessage) json.RawMessage {
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	path, _ := in["path"].(string)
	fail := func(msg string) json.RawMessage {
		in["upload_error"] = msg
		b, _ := json.Marshal(in)
		return b
	}
	if path == "" {
		return fail("Give the file’s path.")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.workDir(), path)
	}
	s.mu.Lock()
	visible := s.visible
	s.mu.Unlock()
	_, real, ok := sandbox.Visible(visible, path)
	if !ok {
		return fail("That file isn’t in a folder you can see. Save it in your work folder.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sha, size, err := s.m.Blobs.UploadFile(ctx, real)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fail("No file at " + path + ".")
		}
		return fail("Couldn’t upload the file: " + err.Error())
	}
	in["name"], in["sha256"], in["size"], in["mime"], in["blob_key"] = s.submitName(path), sha, size, blobs.MimeOf(path), blobs.Key(sha)
	b, _ := json.Marshal(in)
	return b
}

// integrate merges the task's branches into the goal branches before the hub hears the task
// is done. A conflict goes straight back to the agent to resolve.
func (s *Session) integrate(raw json.RawMessage) json.RawMessage {
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	summary, _ := in["summary"].(string)
	title := strings.TrimSpace(strings.SplitN(summary, "\n", 2)[0])
	if title == "" {
		title = "Task work"
	}
	s.mu.Lock()
	mounts := s.spec.Mounts
	s.mu.Unlock()
	var results []protocol.RepoResult
	for _, mt := range mounts {
		if mt.Repo == nil || mt.Repo.TaskBranch == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		res, err := s.m.Repos.Integrate(ctx, s.workDir(), *mt.Repo, title)
		cancel()
		if err != nil {
			in["git_error"] = err.Error()
			b, _ := json.Marshal(in)
			return b
		}
		results = append(results, res)
	}
	if len(results) > 0 {
		in["git"] = results
	}
	b, _ := json.Marshal(in)
	return b
}

// baseEnv is the environment every Claude Code session starts with.
// Artifact needs CLAUDE_CODE_ARTIFACT=1 and is off under CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC.
// DISABLE_TELEMETRY is left unset: it hides ArtifactComments and ArtifactData.
func baseEnv(sessionID string) []string {
	return []string{"CANOPY_SESSION=" + sessionID, "MCP_TOOL_TIMEOUT=86400000", "DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_ARTIFACT=1", "DISABLE_ERROR_REPORTING=1", "DISABLE_BUG_COMMAND=1"}
}

// gitIdentity names the agent on its commits, under the person's own git email if set.
func gitIdentity(member string) []string {
	email := "canopy@localhost"
	if out, err := exec.Command("git", "config", "--global", "user.email").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		email = strings.TrimSpace(string(out))
	}
	name := member + " (Canopy)"
	return []string{"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email}
}

func (s *Session) toolResult(r protocol.ToolResult) {
	s.mu.Lock()
	p := s.pending[r.CallID]
	delete(s.pending, r.CallID)
	proxy := s.proxy
	s.mu.Unlock()
	if p == nil {
		return
	}
	res := tools.Result{CallID: p.callID, Text: r.Text, IsError: r.IsError}
	if len(r.Files) > 0 {
		go func() {
			res.Text = s.fetchFiles(r.Files, r.Text)
			writeResult(p, res)
		}()
		return
	}
	if p.tool == protocol.ToolPermission {
		allow := r.Allow != nil && *r.Allow
		res = tools.Result{CallID: p.callID, Text: claudecode.PermissionReply(allow, p.input, r.Text)}
		if allow && proxy != nil {
			// An approved fetch opens that host for this session.
			var in map[string]any
			_ = json.Unmarshal(p.input, &in)
			if raw, ok := in["url"].(string); ok {
				if u, err := url.Parse(raw); err == nil {
					proxy.Allow(u.Hostname())
				}
			}
		}
	}
	writeResult(p, res)
}

// fetchFiles downloads attachments into ./attachments and lists where they are.
func (s *Session) fetchFiles(files []protocol.FileRef, text string) string {
	dir := filepath.Join(s.workDir(), "attachments")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var b strings.Builder
	b.WriteString(text)
	for _, f := range files {
		name := filepath.Base(filepath.Clean("/" + f.Name))
		if name == "/" || name == "." || strings.HasPrefix(name, ".") {
			continue
		}
		if err := s.m.Blobs.Download(ctx, f.SHA256, filepath.Join(dir, name), 0o644); err != nil {
			fmt.Fprintf(&b, "\n- %s: couldn’t download (%v)", name, err)
			continue
		}
		fmt.Fprintf(&b, "\n- ./attachments/%s", name)
	}
	return b.String()
}

// runtimeDir is a short, private directory for sockets.
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && len(d) < 40 {
		return filepath.Join(d, "canopy")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("canopy-%d", os.Getuid()))
}
