package orch

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// ---- runners.Handler ----

func (o *Orch) RunnerConnected(ctx context.Context, rn db.Runner, h protocol.Hello) {
	clis := mustJSON(h.CLIs)
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		r, err := t.Q.SetRunnerOnline(ctx, db.SetRunnerOnlineParams{ID: rn.ID, Online: true, Hostname: &h.Hostname, Os: &h.OS, Arch: &h.Arch,
			Version: &h.ClientVersion, Clis: clis, Sandbox: &h.Sandbox})
		if err != nil {
			return err
		}
		if r.Kind == "computer" {
			dirs, err := t.Q.BindUnboundDirectories(ctx, db.BindUnboundDirectoriesParams{OwnerUserID: r.OwnerUserID, MachineID: &r.ID, OrgID: r.OrgID})
			if err != nil {
				return err
			}
			for _, d := range dirs {
				if d.RepoRemote == "" {
					continue
				}
				if _, err := t.Q.UpsertClone(ctx, db.UpsertCloneParams{ID: newID(), DirectoryID: d.ID, RunnerID: r.ID, UserID: r.OwnerUserID, Path: d.Path}); err != nil {
					return err
				}
			}
		}
		return t.Emit(ctx, core.Org(r.OrgID, "runner.updated"), runnerEvent(r, true))
	})
	if err != nil {
		slog.ErrorContext(ctx, "runner connected", "err", err)
	}
	// Sessions the hub thinks are alive but the runner doesn't have ended with the machine
	// (a restart, not a CLI crash): they sleep with their conversation kept, and work in
	// progress picks up again.
	live, err := o.Store.ListLiveSessionsOnRunner(ctx, &rn.ID)
	if err == nil {
		have := map[uuid.UUID]bool{}
		for _, s := range h.Sessions {
			have[s.SessionID] = true
		}
		for _, s := range live {
			if have[s.ID] {
				continue
			}
			o.machineRestarted(ctx, s)
		}
	}
	if o.Mirror != nil {
		o.Mirror.RunnerConnected(ctx, rn)
	}
	o.Kick(ctx, uuid.Nil)
}

func runnerEvent(r db.Runner, online bool) map[string]any {
	var clis any
	_ = json.Unmarshal(r.Clis, &clis)
	return map[string]any{"id": r.ID, "name": r.Name, "kind": r.Kind, "owner_user_id": r.OwnerUserID, "hostname": r.Hostname,
		"online": online, "clis": clis, "sandbox": r.Sandbox, "shared": r.Shared, "last_seen_at": r.LastSeenAt}
}

func (o *Orch) RunnerDisconnected(ctx context.Context, rn db.Runner) {
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		r, err := t.Q.SetRunnerOnline(ctx, db.SetRunnerOnlineParams{ID: rn.ID, Online: false})
		if err != nil {
			return err
		}
		return t.Emit(ctx, core.Org(r.OrgID, "runner.updated"), runnerEvent(r, false))
	})
}

func (o *Orch) RunnerMessage(ctx context.Context, rn db.Runner, env protocol.Envelope) {
	switch env.Type {
	case protocol.TSessionEvent:
		var ev protocol.SessionEvent
		if json.Unmarshal(env.Data, &ev) == nil {
			o.onSessionEvent(ctx, rn, ev)
		}
	case protocol.TLogLine:
		var l protocol.LogLine
		if json.Unmarshal(env.Data, &l) == nil {
			o.onLog(ctx, rn, l)
		}
	case protocol.TUsage:
		var u protocol.Usage
		if json.Unmarshal(env.Data, &u) == nil {
			o.onUsage(ctx, rn, u)
		}
	case protocol.TRateLimit:
		var r protocol.RateLimit
		if json.Unmarshal(env.Data, &r) == nil {
			o.onRateLimit(ctx, rn, r)
		}
	case protocol.TToolCall:
		var c protocol.ToolCall
		if json.Unmarshal(env.Data, &c) == nil {
			// Tool calls can wait on people; don't hold up the connection's other messages.
			go o.onToolCall(context.WithoutCancel(ctx), rn, c)
		}
	case protocol.TFileWritten:
		var f protocol.FileWritten
		if json.Unmarshal(env.Data, &f) == nil {
			o.onFileWritten(ctx, rn, f)
		}
	case protocol.TMirrorChange, protocol.TMirrorResult:
		if o.Mirror != nil {
			o.Mirror.Message(ctx, rn, env)
		}
	}
}

// sessionOn loads a session the runner may speak for.
func (o *Orch) sessionOn(ctx context.Context, rn db.Runner, id uuid.UUID) (db.Session, db.Workspace, bool) {
	s, err := o.Store.GetAgentSession(ctx, id)
	if err != nil || s.RunnerID == nil || *s.RunnerID != rn.ID {
		return s, db.Workspace{}, false
	}
	ws, err := o.Store.GetWorkspace(ctx, s.WorkspaceID)
	return s, ws, err == nil
}

func (o *Orch) onSessionEvent(ctx context.Context, rn db.Runner, ev protocol.SessionEvent) {
	sess, ws, ok := o.sessionOn(ctx, rn, ev.SessionID)
	if !ok {
		return
	}
	switch ev.Status {
	case protocol.SessionCrashed:
		o.onCrash(ctx, rn, sess, ev.Error)
		return
	case protocol.SessionError:
		o.onSessionError(ctx, ws, sess, ev.Error)
		return
	}
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		status := map[string]string{protocol.SessionRunning: "running", protocol.SessionWaiting: "waiting", protocol.SessionAsleep: "asleep", protocol.SessionEnded: "ended"}[ev.Status]
		if status == "" {
			return nil
		}
		if (sess.Status == "gate" || sess.Status == "review") && status == "running" {
			status = sess.Status
		}
		var cliSession *string
		if ev.CLISessionID != "" {
			cliSession = &ev.CLISessionID
		}
		s, err := q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: sess.ID, Status: &status, CliSessionID: cliSession})
		if err != nil {
			return err
		}
		if status == "ended" || status == "asleep" {
			if s, err = q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: sess.ID, Status: status}); err != nil {
				return err
			}
		}
		if err := emitSession(ctx, t, ws, s); err != nil {
			return err
		}
		if ev.Status != protocol.SessionWaiting || ev.TurnID == "" {
			return nil
		}
		// A turn finished.
		switch ev.TurnKind {
		case "chat":
			if ev.ResultText != "" {
				if err := o.chat(ctx, t, ws, sess.MemberID, "agent", ev.ResultText, nil); err != nil {
					return err
				}
			}
			t.AfterCommit(func() { o.Kick(ctx, ws.ID) })
			return nil
		case "comment":
			if ev.ResultText != "" && ev.CommentTaskID != nil {
				c, err := q.CreateComment(ctx, db.CreateCommentParams{ID: newID(), TaskID: *ev.CommentTaskID, AuthorMemberID: &sess.MemberID, Text: ev.ResultText})
				if err != nil {
					return err
				}
				if err := t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, sess.MemberID, "task.comment"), map[string]any{"id": c.ID, "task_id": c.TaskID,
					"author_member_id": c.AuthorMemberID, "text": c.Text, "created_at": c.CreatedAt}); err != nil {
					return err
				}
			}
			t.AfterCommit(func() { o.Kick(ctx, ws.ID) })
			return nil
		}
		if ev.ResultText != "" {
			if err := o.log(ctx, t, ws, &sess.ID, sess.MemberID, ev.TaskID, "agent", ev.ResultText); err != nil {
				return err
			}
		}
		if ev.TaskID == nil || ev.IsError {
			t.AfterCommit(func() { o.Kick(ctx, ws.ID) })
			return nil
		}
		task, err := q.GetTask(ctx, *ev.TaskID)
		if err != nil || task.Status != "doing" {
			t.AfterCommit(func() { o.Kick(ctx, ws.ID) })
			return nil
		}
		open, err := q.OpenSignalsForMember(ctx, &sess.MemberID)
		if err != nil {
			return err
		}
		if len(open) > 0 {
			return nil // waiting on a person
		}
		// The agent stopped without finishing: remind it, then raise an error.
		n, err := q.BumpTaskNudges(ctx, task.ID)
		if err != nil {
			return err
		}
		if n > o.NudgeLimit {
			return o.raiseError(ctx, t, ws, s, &task, "Stopped without finishing “"+task.Title+"”.")
		}
		turn := protocol.Turn{TurnID: newID().String(), Kind: "nudge", TaskID: &task.ID, AttributedUserID: task.AttributedUserID,
			Text: "You stopped, but “" + task.Title + "” isn’t marked done. If it’s finished, call complete_task with a summary. If you’re blocked, call ask_question. Otherwise continue."}
		runnerID := rn.ID
		t.AfterCommit(func() { o.send(runnerID, protocol.TSendInput, protocol.SendInput{SessionID: s.ID, Turn: turn}) })
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "session event", "err", err)
	}
}

// raiseError puts a red error card on the member's branch.
func (o *Orch) raiseError(ctx context.Context, t *core.Tx, ws db.Workspace, s db.Session, task *db.Task, msg string) error {
	q := t.Q
	if open, err := q.OpenSignalsForMember(ctx, &s.MemberID); err == nil && slices.ContainsFunc(open, func(x db.Signal) bool { return x.Type == "error" }) {
		return nil
	}
	payload := map[string]any{"message": msg}
	if task != nil {
		steps := core.TaskSteps(*task)
		if int(task.CurrentStep) < len(steps) {
			payload["step"] = steps[task.CurrentStep].Title
		}
	}
	sig, err := q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: ws.ID, Type: "error", MemberID: &s.MemberID, TaskID: taskIDOf(task), Payload: mustJSON(payload)})
	if err != nil {
		return err
	}
	ns, err := q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "error"})
	if err != nil {
		return err
	}
	if err := emitSession(ctx, t, ws, ns); err != nil {
		return err
	}
	if err := o.log(ctx, t, ws, &s.ID, s.MemberID, taskIDOf(task), "error", msg); err != nil {
		return err
	}
	return emitSignal(ctx, t, ws, sig, "signal.opened")
}

func (o *Orch) onSessionError(ctx context.Context, ws db.Workspace, sess db.Session, msg string) {
	if msg == "" {
		msg = "The CLI stopped with an error."
	}
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		var task *db.Task
		if sess.TaskID != nil {
			if x, err := t.Q.GetTask(ctx, *sess.TaskID); err == nil {
				task = &x
			}
		}
		return o.raiseError(ctx, t, ws, sess, task, msg)
	})
}

// onCrash restarts a crashed CLI once, resuming its conversation in a new session marked
// Recovered; a second crash becomes an error (SPEC "Session lifecycle").
func (o *Orch) onCrash(ctx context.Context, rn db.Runner, sess db.Session, msg string) {
	ws, err := o.Store.GetWorkspace(ctx, sess.WorkspaceID)
	if err != nil {
		return
	}
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		if err := q.LockWorkspace(ctx, ws.ID); err != nil {
			return err
		}
		cs, err := q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: sess.ID, Status: "crashed"})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, ws, cs); err != nil {
			return err
		}
		var task *db.Task
		if sess.TaskID != nil {
			if x, err := q.GetTask(ctx, *sess.TaskID); err == nil && (x.Status == "doing" || x.Status == "review") {
				task = &x
			}
		}
		if sess.Restarts >= 1 {
			return o.raiseError(ctx, t, ws, cs, task, "Crashed again after restarting. "+msg)
		}
		st, err := o.load(ctx, q, ws.ID)
		if err != nil {
			return err
		}
		m, ok := st.member[sess.MemberID]
		if !ok || !o.R.Online(rn.ID) {
			return nil
		}
		var mounts []protocol.Mount
		_ = json.Unmarshal(sess.Mounts, &mounts)
		ns, err := q.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: newID(), WorkspaceID: ws.ID, MemberID: m.ID, Status: "starting",
			RunnerID: &rn.ID, CliSessionID: sess.CliSessionID, TaskID: taskIDOf(task), RecoveredFrom: &sess.ID, Restarts: 1,
			GoalID: sess.GoalID, PersonID: sess.PersonID})
		if err != nil {
			return err
		}
		if ns, err = q.SetSessionMounts(ctx, db.SetSessionMountsParams{ID: ns.ID, Mounts: sess.Mounts, Cwd: sess.Cwd, RunnerID: &rn.ID}); err != nil {
			return err
		}
		st.session[keyOfSession(ns)] = ns
		if err := emitSession(ctx, t, ws, ns); err != nil {
			return err
		}
		if err := o.log(ctx, t, ws, &ns.ID, m.ID, taskIDOf(task), "system", "Crashed; restarted and resumed the conversation."); err != nil {
			return err
		}
		notes, err := q.ListNotes(ctx, ws.ID)
		if err != nil {
			return err
		}
		var turn *protocol.Turn
		if task != nil {
			turn = &protocol.Turn{TurnID: newID().String(), Kind: "system", TaskID: &task.ID, AttributedUserID: task.AttributedUserID,
				Text: "Your session crashed and restarted. Continue “" + task.Title + "” where you left off."}
		}
		start := protocol.StartSession{SessionID: ns.ID, WorkspaceID: ws.ID, MemberID: m.ID, MemberName: m.Name, CLI: m.Cli, Model: m.Model,
			Resume: sess.CliSessionID, AppendSystemPrompt: systemPrompt(st, m, mounts, notes), Work: sess.Cwd, Mounts: mounts, Turn: turn,
			UseAPIKey: o.useKey(st, rn.ID, m.Cli)}
		runner := rn.ID
		t.AfterCommit(func() { o.send(runner, protocol.TStartSession, start) })
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "recover session", "err", err)
	}
}

func (o *Orch) onLog(ctx context.Context, rn db.Runner, l protocol.LogLine) {
	sess, ws, ok := o.sessionOn(ctx, rn, l.SessionID)
	if !ok {
		return
	}
	kind := l.Kind
	switch kind {
	case "agent", "tool", "error", "system":
	default:
		kind = "agent"
	}
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		return o.log(ctx, t, ws, &sess.ID, sess.MemberID, sess.TaskID, kind, l.Text)
	})
}

// machineRestarted puts a session that vanished with its machine to sleep and resumes its task.
func (o *Orch) machineRestarted(ctx context.Context, s db.Session) {
	ws, err := o.Store.GetWorkspace(ctx, s.WorkspaceID)
	if err != nil {
		return
	}
	resume := false
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		ns, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "asleep"})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, ws, ns); err != nil {
			return err
		}
		if s.TaskID != nil {
			if task, err := t.Q.GetTask(ctx, *s.TaskID); err == nil && task.Status == "doing" {
				resume = true
			}
		}
		return nil
	})
	if resume {
		o.sendTurn(ctx, ws.ID, keyOfSession(s), protocol.Turn{TurnID: newID().String(), Kind: "system",
			Text: "Your machine restarted. Continue your task where you left off."})
	}
}
