// Package orch is the orchestrator: deterministic code around agent calls that turns goals
// into plan tasks, validates plans, assigns and starts tasks, raises signals and meters usage
// (SPEC "Orchestrator"). It runs in the hub; runners only run the CLI sessions it asks for.
package orch

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Sender reaches connected runners (runners.Hub).
type Sender interface {
	Online(id uuid.UUID) bool
	Send(id uuid.UUID, typ string, data any) error
	Request(ctx context.Context, id uuid.UUID, typ string, data any, out any) error
}

type Orch struct {
	Core  *core.Service
	Store *store.Store
	R     Sender
	Blobs blob.Store

	GateTimeout time.Duration // approval gates and directory access count as Deny after this
	IdleSleep   time.Duration // sessions sleep after this long idle
	NudgeLimit  int32         // reminders before a stalled task becomes an error
	Tick        time.Duration

	// Mirror handles directory sync traffic (set by main).
	Mirror MirrorHandler

	kicks chan uuid.UUID
	once  sync.Once
}

// MirrorHandler is the directory mirror (package mirror).
type MirrorHandler interface {
	RunnerConnected(ctx context.Context, r db.Runner)
	Message(ctx context.Context, r db.Runner, env protocol.Envelope)
	MountsFor(ctx context.Context, runnerID uuid.UUID, dirs []db.Directory) map[uuid.UUID]string
}

func New(c *core.Service, r Sender, blobs blob.Store) *Orch {
	return &Orch{Core: c, Store: c.Store, R: r, Blobs: blobs, GateTimeout: 30 * time.Minute, IdleSleep: 3 * time.Minute,
		NudgeLimit: 2, Tick: 5 * time.Second, kicks: make(chan uuid.UUID, 1024)}
}

func (o *Orch) now() time.Time { return o.Core.Now() }

// Kick asks for a scheduling pass over a workspace (uuid.Nil: every active workspace).
func (o *Orch) Kick(_ context.Context, wsID uuid.UUID) {
	select {
	case o.kicks <- wsID:
	default: // a full queue already has work; the tick catches up
	}
}

func (o *Orch) MemberChanged(ctx context.Context, memberID uuid.UUID) {
	m, err := o.Store.GetMember(ctx, memberID)
	if err != nil {
		return
	}
	if m.Sleeping {
		o.sleepMember(ctx, m)
	}
	o.Kick(ctx, m.WorkspaceID)
}

func (o *Orch) MemberRemoved(ctx context.Context, wsID, memberID uuid.UUID) {
	o.Kick(ctx, wsID)
}

func (o *Orch) DirectoriesChanged(ctx context.Context, wsID uuid.UUID) { o.Kick(ctx, wsID) }

// Run processes kicks and periodic work until ctx ends.
func (o *Orch) Run(ctx context.Context) {
	tick := time.NewTicker(o.Tick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ws := <-o.kicks:
			pending := map[uuid.UUID]bool{ws: true}
		drain:
			for {
				select {
				case w := <-o.kicks:
					pending[w] = true
				default:
					break drain
				}
			}
			if pending[uuid.Nil] {
				o.scheduleAll(ctx)
				continue
			}
			for w := range pending {
				o.scheduleLogged(ctx, w)
			}
		case <-tick.C:
			o.tick(ctx)
		}
	}
}

func (o *Orch) scheduleAll(ctx context.Context) {
	ids, err := o.Store.ListActiveWorkspaces(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list active workspaces", "err", err)
		return
	}
	for _, id := range ids {
		o.scheduleLogged(ctx, id)
	}
}

func (o *Orch) scheduleLogged(ctx context.Context, wsID uuid.UUID) {
	if err := o.schedule(ctx, wsID); err != nil {
		slog.ErrorContext(ctx, "schedule", "workspace", wsID, "err", err)
	}
}

// tick handles time: expired gates, idle sessions, account resets, then a scheduling pass.
func (o *Orch) tick(ctx context.Context) {
	o.expireSignals(ctx)
	o.sleepIdle(ctx)
	o.refreshAccounts(ctx)
	o.scheduleAll(ctx)
}

// send delivers a message to a runner, logging failures (the runner resyncs on reconnect).
func (o *Orch) send(runnerID uuid.UUID, typ string, data any) {
	if err := o.R.Send(runnerID, typ, data); err != nil {
		slog.Warn("send to runner", "runner", runnerID, "type", typ, "err", err)
	}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func ptr[T any](v T) *T { return &v }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// emitTask publishes a task under the member it belongs to.
func emitTask(ctx context.Context, t *core.Tx, ws db.Workspace, task db.Task) error {
	e := core.WS(ws.OrgID, ws.ID, "task.updated")
	e.MemberID = task.MemberID
	return t.Emit(ctx, e, core.ToTask(task))
}

func emitSession(ctx context.Context, t *core.Tx, ws db.Workspace, s db.Session) error {
	return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, s.MemberID, "session.updated"), core.ToSession(s))
}

func emitSignal(ctx context.Context, t *core.Tx, ws db.Workspace, s db.Signal, typ string) error {
	e := core.WS(ws.OrgID, ws.ID, typ)
	e.MemberID = s.MemberID
	if s.ForUserID != nil {
		e.ForUserID = s.ForUserID
	} else if core.PersonalSignal(s.Type) {
		e.AdminOnly = true // a shared computer's limits are for admins
	}
	return t.Emit(ctx, e, core.ToSignal(s))
}

// log adds a line to a member's activity.
func (o *Orch) log(ctx context.Context, t *core.Tx, ws db.Workspace, sessionID *uuid.UUID, memberID uuid.UUID, taskID *uuid.UUID, kind, text string) error {
	if len(text) > 8000 {
		text = text[:8000] + "…"
	}
	l, err := t.Q.InsertLogLine(ctx, db.InsertLogLineParams{WorkspaceID: ws.ID, SessionID: sessionID, MemberID: &memberID, TaskID: taskID, Kind: kind, Text: text})
	if err != nil {
		return err
	}
	return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, memberID, "session.log"), core.ToLog(l))
}

// chat posts a message from a member (or an event line) to the member's chat thread.
func (o *Orch) chat(ctx context.Context, t *core.Tx, ws db.Workspace, memberID uuid.UUID, kind, text string, refs map[string]any) error {
	if refs == nil {
		refs = map[string]any{}
	}
	m, err := t.Q.CreateChatMessage(ctx, db.CreateChatMessageParams{ID: newID(), WorkspaceID: ws.ID, MemberID: memberID, Kind: kind, Text: text, Refs: mustJSON(refs)})
	if err != nil {
		return err
	}
	return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, memberID, "chat.message"), core.ToChat(m))
}
