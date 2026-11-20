package orch

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// sleepMember stops a member's processes but keeps their conversations.
func (o *Orch) sleepMember(ctx context.Context, m db.Member) {
	live, err := o.Store.ListMemberLiveSessions(ctx, m.ID)
	if err != nil {
		return
	}
	for _, s := range live {
		if s.RunnerID != nil && (s.Status == "waiting" || s.Status == "running" || s.Status == "starting") {
			o.putToSleep(ctx, s)
		}
	}
}

func (o *Orch) putToSleep(ctx context.Context, s db.Session) {
	ws, err := o.Store.GetWorkspace(ctx, s.WorkspaceID)
	if err != nil {
		return
	}
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		ns, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "asleep"})
		if err != nil {
			return err
		}
		t.AfterCommit(func() { o.send(*s.RunnerID, protocol.TSleep, protocol.SessionRef{SessionID: s.ID}) })
		return emitSession(ctx, t, ws, ns)
	})
}

// sleepIdle puts sessions idle for IdleSleep to sleep (SPEC "Session lifecycle").
func (o *Orch) sleepIdle(ctx context.Context) {
	idle, err := o.Store.IdleSessions(ctx, o.now().Add(-o.IdleSleep))
	if err != nil {
		return
	}
	for _, s := range idle {
		if s.RunnerID == nil {
			continue
		}
		tasks, err := o.Store.ListMemberOpenTasks(ctx, &s.MemberID)
		if err != nil {
			continue
		}
		k := keyOfSession(s)
		if slices.ContainsFunc(tasks, func(t db.Task) bool { return (t.Status == "doing" || t.Status == "review") && taskKey(t) == k }) {
			continue
		}
		o.putToSleep(ctx, s)
	}
}

// RestartSessions ends every agent conversation in a workspace and starts the ones with work
// in progress again in fresh conversations. A session waiting on a person's answer keeps
// waiting, because its answer goes back into that conversation. It returns how many sessions
// were restarted.
func (o *Orch) RestartSessions(ctx context.Context, wsID uuid.UUID) (int, error) {
	ws, err := o.Store.GetWorkspace(ctx, wsID)
	if err != nil {
		return 0, err
	}
	var again []sessKey
	n := 0
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.LockWorkspace(ctx, wsID); err != nil {
			return err
		}
		latest, err := t.Q.ListLatestSessions(ctx, wsID)
		if err != nil {
			return err
		}
		for _, s := range latest {
			if s.Status == "ended" || s.Status == "crashed" {
				continue
			}
			open, err := t.Q.OpenSignalsForSession(ctx, &s.ID)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(open, func(sig db.Signal) bool { return sig.RequestID != "" }) {
				continue
			}
			ns, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "ended"})
			if err != nil {
				return err
			}
			if err := emitSession(ctx, t, ws, ns); err != nil {
				return err
			}
			if s.RunnerID != nil && s.Status != "asleep" {
				runner, id := *s.RunnerID, s.ID
				t.AfterCommit(func() { o.send(runner, protocol.TStop, protocol.SessionRef{SessionID: id}) })
			}
			n++
			if s.TaskID != nil {
				if task, err := t.Q.GetTask(ctx, *s.TaskID); err == nil && task.Status == "doing" {
					again = append(again, keyOfSession(s))
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, k := range again {
		o.sendTurn(ctx, wsID, k, protocol.Turn{TurnID: newID().String(), Kind: "system",
			Text: "Your session was restarted with a fresh conversation. Continue your task where it left off."})
	}
	o.Kick(ctx, wsID)
	return n, nil
}
