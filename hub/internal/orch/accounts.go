package orch

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Sign-ins belong to a computer: a usage limit or a cap stops only the work running on that
// computer, and only its owner (admins, for a shared computer) decides what to do.

// onUsage records tokens for the person the work is for (SPEC "Attribution rule"), meters the
// computer's sign-in and enforces API key caps.
func (o *Orch) onUsage(ctx context.Context, rn db.Runner, u protocol.Usage) {
	sess, ws, ok := o.sessionOn(ctx, rn, u.SessionID)
	if !ok || (u.TokensIn == 0 && u.TokensOut == 0) {
		return
	}
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		attributed := u.AttributedUserID
		if attributed != nil {
			if _, err := q.GetActiveMembership(ctx, db.GetActiveMembershipParams{OrgID: ws.OrgID, UserID: *attributed}); err != nil {
				attributed = nil
			}
		}
		if attributed == nil && sess.TaskID != nil {
			if task, err := q.GetTask(ctx, *sess.TaskID); err == nil {
				attributed = task.AttributedUserID
			}
		}
		m, err := q.GetMember(ctx, sess.MemberID)
		if err != nil {
			return err
		}
		model := firstNonEmpty(u.Model, m.Model)
		ev, err := q.InsertUsageEvent(ctx, db.InsertUsageEventParams{OrgID: ws.OrgID, WorkspaceID: &ws.ID, MemberID: &m.ID, AttributedUserID: attributed,
			SessionID: &sess.ID, Cli: m.Cli, Model: model, TokensIn: u.TokensIn, TokensOut: u.TokensOut})
		if err != nil {
			return err
		}
		if err := q.AddUsageHourly(ctx, db.AddUsageHourlyParams{OrgID: ws.OrgID, At: ev.At, UserID: deref2(attributed), MemberID: m.ID, WorkspaceID: ws.ID, Model: model,
			TokensIn: u.TokensIn, TokensOut: u.TokensOut}); err != nil {
			return err
		}
		s, err := q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: sess.ID, AddIn: u.TokensIn, AddOut: u.TokensOut})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, ws, s); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.Org(ws.OrgID, "usage.added"), map[string]any{"workspace_id": ws.ID, "member_id": m.ID, "user_id": attributed,
			"model": model, "tokens": u.TokensIn + u.TokensOut, "at": ev.At, "runner_id": rn.ID}); err != nil {
			return err
		}
		return o.meter(ctx, t, rn, m.Cli, u.TokensIn+u.TokensOut)
	})
	if err != nil {
		slog.ErrorContext(ctx, "usage", "err", err)
	}
}

// meter adds tokens to a computer's sign-in and warns at 80% and pauses at 100% of its cap.
func (o *Orch) meter(ctx context.Context, t *core.Tx, rn db.Runner, cli string, tokens int64) error {
	q := t.Q
	acc, err := q.AddAccountTokens(ctx, db.AddAccountTokensParams{RunnerID: rn.ID, Cli: cli, Tokens: tokens})
	if err != nil {
		return nil // this computer has no sign-in set up for the CLI; nothing to meter
	}
	conn, err := q.GetCliConnection(ctx, db.GetCliConnectionParams{RunnerID: rn.ID, Cli: cli})
	if err == nil && conn.Mode == "api_key" && conn.CapTokensPerDay != nil {
		used, capT := acc.UsedTokens, *conn.CapTokensPerDay
		today := time.Now().UTC().Truncate(24 * time.Hour)
		switch {
		case used >= capT && !acc.Paused:
			if acc, err = q.SetAccountPaused(ctx, db.SetAccountPausedParams{RunnerID: rn.ID, Cli: cli, Paused: true}); err != nil {
				return err
			}
			if err := o.accountSignal(ctx, t, rn, cli, "cap", map[string]any{"level": 100, "used": used, "cap": capT}); err != nil {
				return err
			}
			t.AfterCommit(func() { o.pauseAccount(context.WithoutCancel(ctx), rn.ID, cli) })
		case used*100 >= capT*80 && (!acc.CapWarnedDay.Valid || acc.CapWarnedDay.Time.Before(today)):
			if err := q.SetAccountCapWarned(ctx, db.SetAccountCapWarnedParams{RunnerID: rn.ID, Cli: cli}); err != nil {
				return err
			}
			if err := o.accountSignal(ctx, t, rn, cli, "cap", map[string]any{"level": 80, "used": used, "cap": capT}); err != nil {
				return err
			}
		}
	}
	return o.emitAccount(ctx, t, rn.OrgID, acc)
}

// accountSignal opens (or refreshes) a limit or cap card in every workspace using the CLI.
// It is for the computer's owner, or for admins when the computer is shared.
func (o *Orch) accountSignal(ctx context.Context, t *core.Tx, rn db.Runner, cli, typ string, payload map[string]any) error {
	payload["cli"], payload["runner_id"], payload["runner_name"], payload["shared"] = cli, rn.ID.String(), rn.Name, rn.Shared
	var forUser *uuid.UUID
	if !rn.Shared {
		forUser = &rn.OwnerUserID
	}
	wss, err := t.Q.ListOrgWorkspacesUsingCLI(ctx, db.ListOrgWorkspacesUsingCLIParams{OrgID: rn.OrgID, Cli: cli})
	if err != nil {
		return err
	}
	for _, ws := range wss {
		existing, err := t.Q.OpenAccountSignal(ctx, db.OpenAccountSignalParams{WorkspaceID: ws.ID, Type: typ, Cli: cli, RunnerID: rn.ID.String()})
		if err == nil {
			ns, err := t.Q.UpdateSignalPayload(ctx, db.UpdateSignalPayloadParams{ID: existing.ID, Payload: mustJSON(payload)})
			if err != nil {
				return err
			}
			if err := emitSignal(ctx, t, ws, ns, "signal.updated"); err != nil {
				return err
			}
			continue
		} else if !store.IsNotFound(err) {
			return err
		}
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: ws.ID, Type: typ, Payload: mustJSON(payload), ForUserID: forUser})
		if err != nil {
			return err
		}
		if err := emitSignal(ctx, t, ws, sig, "signal.opened"); err != nil {
			return err
		}
	}
	return nil
}

// pauseAccount puts the sessions on a computer's sign-in to sleep; their tasks resume later.
func (o *Orch) pauseAccount(ctx context.Context, runner uuid.UUID, cli string) {
	live, err := o.Store.ListRunnerLiveSessions(ctx, &runner)
	if err != nil {
		return
	}
	for _, s := range live {
		if m, err := o.Store.GetMember(ctx, s.MemberID); err == nil && m.Cli == cli {
			o.putToSleep(ctx, s)
		}
	}
}

// onRateLimit: the CLI reported its sign-in's limits (Claude Code's rate_limit_event).
func (o *Orch) onRateLimit(ctx context.Context, rn db.Runner, r protocol.RateLimit) {
	if _, _, ok := o.sessionOn(ctx, rn, r.SessionID); !ok {
		return
	}
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		util := map[string]any{}
		for k, v := range r.Utilization {
			util[k] = v
		}
		if r.ResetsAt != nil {
			util["resets_at"] = r.ResetsAt
		}
		params := db.UpdateAccountStateParams{RunnerID: rn.ID, Cli: r.CLI, Utilization: mustJSON(util)}
		if r.Limited {
			until := o.now().Add(time.Hour)
			if r.ResetsAt != nil {
				until = *r.ResetsAt
			}
			params.SetLimited, params.LimitedUntil = true, &until
		}
		acc, err := t.Q.UpdateAccountState(ctx, params)
		if err != nil {
			return nil // no sign-in set up for this CLI on this computer
		}
		if r.Limited {
			if err := o.accountSignal(ctx, t, rn, r.CLI, "usage_limit", map[string]any{"resets_at": acc.LimitedUntil, "message": r.Message}); err != nil {
				return err
			}
		}
		return o.emitAccount(ctx, t, rn.OrgID, acc)
	})
	if err != nil {
		slog.ErrorContext(ctx, "rate limit", "err", err)
	}
}

// resumeAccount continues work stopped by a computer's limit or cap.
func (o *Orch) resumeAccount(ctx context.Context, runner uuid.UUID, cli string) {
	sessions, err := o.Store.ListRunnerSessionsForResume(ctx, &runner)
	if err != nil {
		return
	}
	wsIDs := map[uuid.UUID]bool{}
	for _, s := range sessions {
		m, err := o.Store.GetMember(ctx, s.MemberID)
		if err != nil || m.Cli != cli || s.TaskID == nil {
			continue
		}
		task, err := o.Store.GetTask(ctx, *s.TaskID)
		if err != nil || task.Status != "doing" {
			continue
		}
		// Restart (resuming the conversation) so the new sign-in takes effect.
		if s.Status != "asleep" {
			_, _ = o.Store.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: s.ID, Status: "asleep"})
		}
		o.sendTurn(ctx, s.WorkspaceID, keyOfSession(s), protocol.Turn{TurnID: newID().String(), Kind: "system",
			Text: "You were paused by a usage limit. Continue your task where you left off."})
		wsIDs[s.WorkspaceID] = true
	}
	for id := range wsIDs {
		o.Kick(ctx, id)
	}
	o.Kick(ctx, uuid.Nil)
}

func (o *Orch) emitAccount(ctx context.Context, t *core.Tx, orgID uuid.UUID, a db.Account) error {
	conn, err := t.Q.GetCliConnection(ctx, db.GetCliConnectionParams{RunnerID: a.RunnerID, Cli: a.Cli})
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	v := core.AccountView{RunnerID: a.RunnerID, CLI: a.Cli, Mode: a.Mode, UsedTokens: a.UsedTokens, LimitedUntil: a.LimitedUntil, Utilization: a.Utilization,
		Paused: a.Paused, CapTokensPerDay: conn.CapTokensPerDay, KeyFingerprint: conn.KeyFingerprint, Enabled: conn.Enabled}
	return t.Emit(ctx, core.Org(orgID, "account.updated"), v)
}

// refreshAccounts clears limits whose reset time passed and caps paused on an earlier day.
func (o *Orch) refreshAccounts(ctx context.Context) {
	accs, err := o.Store.AccountsToRefresh(ctx)
	if err != nil {
		return
	}
	for _, a := range accs {
		_ = o.Core.Tx(ctx, func(t *core.Tx) error {
			na, err := t.Q.ClearAccountLimit(ctx, a.ID)
			if err != nil {
				return err
			}
			if err := o.emitAccount(ctx, t, a.OrgID, na); err != nil {
				return err
			}
			// Close the account's open limit and cap cards.
			wss, err := t.Q.ListOrgWorkspacesUsingCLI(ctx, db.ListOrgWorkspacesUsingCLIParams{OrgID: a.OrgID, Cli: a.Cli})
			if err != nil {
				return err
			}
			for _, ws := range wss {
				for _, typ := range []string{"usage_limit", "cap"} {
					if (typ == "cap" && na.Paused) || (typ == "usage_limit" && na.LimitedUntil != nil) {
						continue
					}
					sig, err := t.Q.OpenAccountSignal(ctx, db.OpenAccountSignalParams{WorkspaceID: ws.ID, Type: typ, Cli: a.Cli, RunnerID: a.RunnerID.String()})
					if err != nil {
						continue
					}
					ns, err := t.Q.ResolveSignal(ctx, db.ResolveSignalParams{ID: sig.ID, Status: "resolved", Resolution: mustJSON(map[string]any{"action": "reset", "by": "reset"})})
					if err != nil {
						return err
					}
					if err := emitSignal(ctx, t, ws, ns, "signal.resolved"); err != nil {
						return err
					}
				}
			}
			runner, cli := a.RunnerID, a.Cli
			t.AfterCommit(func() { o.resumeAccount(context.WithoutCancel(ctx), runner, cli) })
			return nil
		})
	}
}
