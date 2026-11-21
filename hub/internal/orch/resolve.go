package orch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Resolution is a person's answer to a signal (SPEC "Signals that need people" → Actions).
type Resolution struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"` // question answers
	Note   string `json:"note,omitempty"` // "Ask for changes" note
	Cap    *int64 `json:"cap,omitempty"`  // "Raise cap"
	By     string `json:"-"`              // who answered, for messages to the agent
}

var (
	ErrSignalClosed = errors.New("This was already answered.")
	ErrBadAction    = errors.New("That answer doesn’t fit this item.")
)

var actions = map[string][]string{
	"question":      {"answer", "manager"},
	"gate":          {"allow", "always", "deny"},
	"dir_access":    {"grant", "deny"},
	"error":         {"retry", "skip"},
	"review":        {"approve", "changes"},
	"usage_limit":   {"wait", "use_key"},
	"cap":           {"raise_cap", "keep"},
	"owner_change":  {"approve", "deny"},
	"sync_conflict": {"keep_owner", "keep_agent"},
}

func allowed(typ, action string) bool {
	for _, a := range actions[typ] {
		if a == action {
			return true
		}
	}
	return false
}

// Resolve answers an open signal everywhere at once and records an audit event.
func (o *Orch) Resolve(ctx context.Context, signalID uuid.UUID, by *uuid.UUID, r Resolution) (db.Signal, error) {
	var out db.Signal
	var after []func()
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		q := t.Q
		sig, err := q.GetSignalForUpdate(ctx, signalID)
		if err != nil {
			return err
		}
		if sig.Status != "open" {
			return ErrSignalClosed
		}
		if !allowed(sig.Type, r.Action) {
			return ErrBadAction
		}
		ws, err := q.GetWorkspace(ctx, sig.WorkspaceID)
		if err != nil {
			return err
		}
		if err := q.LockWorkspace(ctx, ws.ID); err != nil {
			return err
		}
		var payload map[string]any
		_ = json.Unmarshal(sig.Payload, &payload)
		var member *db.Member
		if sig.MemberID != nil {
			if m, err := q.GetMember(ctx, *sig.MemberID); err == nil {
				member = &m
			}
		}
		var sess *db.Session // the session waiting on this signal, if any
		if sig.SessionID != nil {
			if s, err := q.GetAgentSession(ctx, *sig.SessionID); err == nil {
				sess = &s
			}
		}
		toolReply := func(res protocol.ToolResult) {
			if sig.RequestID == "" || sess == nil || sess.RunnerID == nil {
				return
			}
			res.SessionID, res.CallID = sess.ID, sig.RequestID
			runnerID := *sess.RunnerID
			after = append(after, func() { o.send(runnerID, protocol.TToolResult, res) })
		}
		resumeSession := func() error {
			if sess == nil || (sess.Status != "gate" && sess.Status != "review") {
				return nil
			}
			s, err := q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: sess.ID, Status: "running"})
			if err != nil {
				return err
			}
			return emitSession(ctx, t, ws, s)
		}
		who := r.By
		if by != nil {
			if u, err := q.GetUserByID(ctx, *by); err == nil {
				who = u.Name
			}
		}
		if who == "" {
			who = "a person"
		}
		summary := ""
		switch sig.Type {
		case "question":
			if r.Action == "manager" {
				if member == nil || member.ReportsTo == nil {
					return ErrBadAction
				}
				// Hand the question to the manager; the signal stays open until it answers.
				payload["delegated"] = true
				ns, err := q.UpdateSignalPayload(ctx, db.UpdateSignalPayloadParams{ID: sig.ID, Payload: mustJSON(payload)})
				if err != nil {
					return err
				}
				if err := emitSignal(ctx, t, ws, ns, "signal.updated"); err != nil {
					return err
				}
				managerID, staffName, question := *member.ReportsTo, member.Name, fmt.Sprint(payload["question"])
				opts, _ := json.Marshal(payload["options"])
				var attributed *uuid.UUID
				mk := personKey(managerID, deref2(by))
				if sig.TaskID != nil {
					if task, err := q.GetTask(ctx, *sig.TaskID); err == nil {
						attributed = task.AttributedUserID
						mk = personKey(managerID, deref2(attributed))
						if task.GoalID != nil {
							mk = goalKey(managerID, *task.GoalID)
						}
					}
				}
				wsID := ws.ID
				after = append(after, func() {
					o.sendTurn(context.WithoutCancel(ctx), wsID, mk, protocol.Turn{TurnID: newID().String(), Kind: "system", AttributedUserID: attributed,
						Text: fmt.Sprintf("%s (your staff) asks: %s\nOptions: %s\n%s asked you to decide. Call answer_question with signal_id %s and your answer.", staffName, question, opts, who, sig.ID)})
				})
				_ = o.audit(ctx, q, ws, by, "answers.delegated", "Asked the manager to decide: "+question)
				out = ns
				return nil
			}
			if strings.TrimSpace(r.Text) == "" {
				return ErrBadAction
			}
			toolReply(protocol.ToolResult{Text: "Answer from " + who + ": " + r.Text})
			summary = "Answered “" + fmt.Sprint(payload["question"]) + "”: " + r.Text
		case "gate":
			ok := r.Action != "deny"
			if r.Action == "always" && member != nil {
				if err := q.CreateGateRule(ctx, db.CreateGateRuleParams{ID: newID(), MemberID: member.ID, ActionKind: fmt.Sprint(payload["action_kind"]), CreatedBy: by}); err != nil {
					return err
				}
			}
			toolReply(protocol.ToolResult{Allow: &ok, Text: map[bool]string{true: "", false: "Denied by " + who + "."}[ok]})
			if err := resumeSession(); err != nil {
				return err
			}
			summary = map[string]string{"allow": "Allowed once", "always": "Always allowed", "deny": "Denied"}[r.Action] + ": " + fmt.Sprint(payload["summary"])
		case "dir_access":
			dirID, _ := uuid.Parse(fmt.Sprint(payload["directory_id"]))
			mode := fmt.Sprint(payload["mode"])
			if r.Action == "grant" && member != nil {
				if err := o.grantWithManager(ctx, t, ws, *member, dirID, mode, by); err != nil {
					return err
				}
				summary = "Granted " + strings.ReplaceAll(mode, "_", " ") + " access to " + fmt.Sprint(payload["path"])
			} else {
				summary = "Denied access to " + fmt.Sprint(payload["path"])
				if sig.TaskID != nil && sig.RequestID == "" {
					// The member works around it: drop the directory from the task's needs.
					if err := o.dropTaskDir(ctx, t, ws, *sig.TaskID, dirID); err != nil {
						return err
					}
				}
			}
			if sig.RequestID != "" && sess != nil && sess.RunnerID != nil {
				if r.Action == "grant" {
					toolReply(protocol.ToolResult{Text: "Access granted by " + who + ". Your session restarts with it mounted; then continue."})
					k, wsID := keyOfSession(*sess), ws.ID
					after = append(after, func() {
						o.sendTurn(context.WithoutCancel(ctx), wsID, k, protocol.Turn{TurnID: newID().String(), Kind: "system",
							Text: "Access to " + fmt.Sprint(payload["path"]) + " is now mounted. Continue your task."})
					})
				} else {
					toolReply(protocol.ToolResult{Text: "Access denied by " + who + ". Work around it."})
				}
			}
			if err := resumeSession(); err != nil {
				return err
			}
		case "error":
			if member == nil {
				break
			}
			memberID, wsID := member.ID, ws.ID
			if sess != nil {
				s, err := q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: sess.ID, Status: "ended"})
				if err != nil {
					return err
				}
				if err := emitSession(ctx, t, ws, s); err != nil {
					return err
				}
				if s.RunnerID != nil {
					runnerID, sid := *s.RunnerID, s.ID
					after = append(after, func() { o.send(runnerID, protocol.TStop, protocol.SessionRef{SessionID: sid}) })
				}
			}
			if sig.TaskID != nil {
				task, err := q.GetTask(ctx, *sig.TaskID)
				if err == nil && task.Status == "doing" {
					text := "Try the task again."
					if r.Action == "skip" {
						steps := core.TaskSteps(task)
						cur := int(task.CurrentStep)
						if cur < len(steps) {
							steps[cur].Done = true
							text = "Skip the step “" + steps[cur].Title + "” and continue with the next one."
							cur++
						}
						nt, err := q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Steps: mustJSON(steps), CurrentStep: ptr(int32(cur))})
						if err != nil {
							return err
						}
						if err := emitTask(ctx, t, ws, nt); err != nil {
							return err
						}
					}
					// Put the task back so the scheduler restarts it in a fresh session.
					nt, err := q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: ptr("todo")})
					if err != nil {
						return err
					}
					if err := emitTask(ctx, t, ws, nt); err != nil {
						return err
					}
					_ = text
				}
			}
			summary = map[string]string{"retry": "Retried", "skip": "Skipped the step"}[r.Action]
			after = append(after, func() { o.Kick(ctx, wsID) })
			_ = memberID
		case "review":
			outID, _ := uuid.Parse(fmt.Sprint(payload["output_id"]))
			if r.Action == "changes" && strings.TrimSpace(r.Note) == "" {
				return ErrNoteRequired
			}
			if out, err := q.GetOutput(ctx, outID); err == nil {
				status := map[string]string{"approve": "approved", "changes": "changes"}[r.Action]
				if out, err = q.SetOutputStatus(ctx, db.SetOutputStatusParams{ID: out.ID, Status: status}); err != nil {
					return err
				}
				versions, _ := q.ListOutputVersions(ctx, out.ID)
				if err := t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, deref2(out.MemberID), "output.version"), core.ToOutput(out, versions)); err != nil {
					return err
				}
			}
			if sig.TaskID != nil {
				task, err := q.GetTask(ctx, *sig.TaskID)
				if err != nil {
					return err
				}
				params := db.UpdateTaskParams{ID: task.ID, Status: ptr("doing")}
				if task.Kind == "handover" && r.Action == "approve" && task.GoalID != nil {
					if err := o.approveHandover(ctx, t, ws, task, outID, by); err != nil {
						return err
					}
					params.Status, params.Progress, params.SetFinished = ptr("done"), ptr(int32(100)), true
					if sess != nil {
						ns, err := q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: sess.ID, SetTask: true})
						if err != nil {
							return err
						}
						if err := emitSession(ctx, t, ws, ns); err != nil {
							return err
						}
					}
				}
				if r.Action == "changes" {
					params.Progress = ptr(int32(72)) // SPEC: "rolls the task back to 72%"
				}
				nt, err := q.UpdateTask(ctx, params)
				if err != nil {
					return err
				}
				if err := emitTask(ctx, t, ws, nt); err != nil {
					return err
				}
				if task.GoalID != nil {
					if g, err := q.GetGoal(ctx, *task.GoalID); err == nil {
						label := map[string]string{"approve": "Approved", "changes": "Changes asked"}[r.Action]
						if err := core.GoalEvent(ctx, t, ws, g, "review", label+": “"+fmt.Sprint(payload["title"])+"”", map[string]any{"output_id": outID, "by": who}); err != nil {
							return err
						}
					}
				}
			}
			if r.Action == "approve" {
				text := "Approved by " + who + ". Finish the task."
				if task := sig.TaskID; task != nil && payload["handover"] == true {
					text = "Approved by " + who + ". The handover is done; nothing more to do."
				}
				toolReply(protocol.ToolResult{Text: text})
				summary = "Approved “" + fmt.Sprint(payload["title"]) + "”"
			} else {
				toolReply(protocol.ToolResult{Text: "Changes requested by " + who + ": " + r.Note + "\nMake the changes, save a new version of the same file and call submit_output again."})
				summary = "Asked for changes to “" + fmt.Sprint(payload["title"]) + "”: " + r.Note
			}
			if err := resumeSession(); err != nil {
				return err
			}
		case "usage_limit":
			cli := fmt.Sprint(payload["cli"])
			runner, _ := uuid.Parse(fmt.Sprint(payload["runner_id"]))
			if r.Action == "use_key" {
				acc, err := q.GetAccount(ctx, db.GetAccountParams{RunnerID: runner, Cli: cli})
				if err != nil {
					return err
				}
				until := acc.LimitedUntil
				if until == nil {
					until = ptr(o.now().Add(5 * 60 * 60 * 1e9))
				}
				acc, err = q.SetAccountUseKey(ctx, db.SetAccountUseKeyParams{RunnerID: runner, Cli: cli, UseKeyUntil: until})
				if err != nil {
					return err
				}
				if err := o.emitAccount(ctx, t, ws.OrgID, acc); err != nil {
					return err
				}
				summary = "Using an API key for " + cli + " until the limit resets"
				after = append(after, func() { o.resumeAccount(context.WithoutCancel(ctx), runner, cli) })
			} else {
				summary = "Waiting for the " + cli + " limit to reset"
			}
		case "cap":
			cli := fmt.Sprint(payload["cli"])
			runner, _ := uuid.Parse(fmt.Sprint(payload["runner_id"]))
			if r.Action == "raise_cap" {
				if r.Cap == nil || *r.Cap <= 0 {
					return ErrBadAction
				}
				conn, err := q.GetCliConnection(ctx, db.GetCliConnectionParams{RunnerID: runner, Cli: cli})
				if err != nil {
					return err
				}
				if _, err := q.UpsertCliConnection(ctx, db.UpsertCliConnectionParams{ID: conn.ID, OrgID: ws.OrgID, RunnerID: runner, Cli: cli, Mode: conn.Mode,
					CapTokensPerDay: r.Cap, Enabled: conn.Enabled}); err != nil {
					return err
				}
				acc, err := q.GetAccount(ctx, db.GetAccountParams{RunnerID: runner, Cli: cli})
				if err != nil {
					return err
				}
				if acc.UsedTokens < *r.Cap {
					if acc, err = q.SetAccountPaused(ctx, db.SetAccountPausedParams{RunnerID: runner, Cli: cli, Paused: false}); err != nil {
						return err
					}
				}
				if err := o.emitAccount(ctx, t, ws.OrgID, acc); err != nil {
					return err
				}
				summary = fmt.Sprintf("Raised the %s daily cap to %d tokens", cli, *r.Cap)
				after = append(after, func() { o.resumeAccount(context.WithoutCancel(ctx), runner, cli) })
			} else {
				summary = "Kept the " + cli + " cap"
			}
		case "owner_change":
			if sig.TaskID != nil {
				task, err := q.GetTask(ctx, *sig.TaskID)
				if err != nil {
					return err
				}
				params := db.UpdateTaskParams{ID: task.ID, Status: ptr("todo")}
				if r.Action == "deny" {
					params = db.UpdateTaskParams{ID: task.ID, Status: ptr("done"), Summary: ptr("Declined by " + who + "; work around it."), SetFinished: true, Progress: ptr(int32(100))}
				}
				nt, err := q.UpdateTask(ctx, params)
				if err != nil {
					return err
				}
				if err := emitTask(ctx, t, ws, nt); err != nil {
					return err
				}
				if reqID, err := uuid.Parse(fmt.Sprint(payload["requested_by_member_id"])); err == nil {
					text := map[string]string{"approve": "Approved", "deny": "Declined"}[r.Action] + " by " + who + ": " + task.Title
					if err := o.chat(ctx, t, ws, reqID, "event", text, map[string]any{"event": "owner_change", "task_id": task.ID}); err != nil {
						return err
					}
				}
			}
			summary = map[string]string{"approve": "Approved a change to ", "deny": "Declined a change to "}[r.Action] + fmt.Sprint(payload["path"])
			wsID := ws.ID
			after = append(after, func() { o.Kick(ctx, wsID) })
		case "sync_conflict":
			if o.Mirror == nil {
				return ErrBadAction
			}
			summary = "Resolved a sync conflict in " + fmt.Sprint(payload["path"])
			if rm, ok := o.Mirror.(interface {
				ResolveConflict(ctx context.Context, t *core.Tx, sig db.Signal, keepAgent bool) error
			}); ok {
				if err := rm.ResolveConflict(ctx, t, sig, r.Action == "keep_agent"); err != nil {
					return err
				}
			}
		}
		res := mustJSON(map[string]any{"action": r.Action, "text": r.Text, "note": r.Note, "cap": r.Cap, "by": who})
		ns, err := q.ResolveSignal(ctx, db.ResolveSignalParams{ID: sig.ID, Status: "resolved", ResolvedBy: by, Resolution: res})
		if err != nil {
			return err
		}
		if err := emitSignal(ctx, t, ws, ns, "signal.resolved"); err != nil {
			return err
		}
		if member != nil {
			if err := o.log(ctx, t, ws, nil, member.ID, sig.TaskID, "human", who+": "+summary); err != nil {
				return err
			}
		}
		out = ns
		wsID := ws.ID
		after = append(after, func() { o.Kick(ctx, wsID) })
		return o.audit(ctx, q, ws, by, "signals."+sig.Type, summary)
	})
	if err != nil {
		return out, err
	}
	for _, f := range after {
		f()
	}
	return out, nil
}

var ErrNoteRequired = errors.New("Add a note saying what to change.")

func deref2(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func (o *Orch) audit(ctx context.Context, q *db.Queries, ws db.Workspace, by *uuid.UUID, typ, summary string) error {
	return q.InsertAuditEvent(ctx, db.InsertAuditEventParams{ID: newID(), OrgID: &ws.OrgID, WorkspaceID: &ws.ID, ActorUserID: by, Type: typ, Summary: summary})
}

// grantWithManager adds a grant to a member, and to its manager first if missing, keeping
// staff within their manager (SPEC "Grant adds to the staff member, and to its manager if missing").
func (o *Orch) grantWithManager(ctx context.Context, t *core.Tx, ws db.Workspace, m db.Member, dirID uuid.UUID, mode string, by *uuid.UUID) error {
	q := t.Q
	add := func(target db.Member) error {
		grants, err := q.ListMemberGrants(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, g := range grants {
			if g.DirectoryID == dirID && rank(g.Mode) >= rank(mode) {
				return nil
			}
		}
		if _, err := q.UpsertGrant(ctx, db.UpsertGrantParams{ID: newID(), MemberID: target.ID, DirectoryID: dirID, Mode: mode, Source: "human", GrantedBy: by}); err != nil {
			return err
		}
		grants, err = q.ListMemberGrants(ctx, target.ID)
		if err != nil {
			return err
		}
		gv := make([]core.GrantView, 0, len(grants))
		for _, g := range grants {
			gv = append(gv, core.ToGrant(g))
		}
		return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, target.ID, "member.updated"), map[string]any{"member": core.ToMember(target), "grants": gv})
	}
	if m.Kind == "staff" && m.ReportsTo != nil {
		if mgr, err := q.GetMember(ctx, *m.ReportsTo); err == nil {
			if err := add(mgr); err != nil {
				return err
			}
		}
	}
	return add(m)
}

func (o *Orch) dropTaskDir(ctx context.Context, t *core.Tx, ws db.Workspace, taskID, dirID uuid.UUID) error {
	task, err := t.Q.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	var keep []core.DirNeed
	for _, d := range core.TaskDirs(task) {
		if d.DirectoryID != dirID {
			keep = append(keep, d)
		}
	}
	if _, err := t.Q.SetTaskDirs(ctx, db.SetTaskDirsParams{ID: taskID, RequiredDirs: mustJSON(nonNilNeeds(keep))}); err != nil {
		return err
	}
	nt, err := t.Q.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	return emitTask(ctx, t, ws, nt)
}

// sendTurn gives a session an input outside the task flow (a continue, a delegated question,
// a chat message). It goes to the task the session is working on, if any.
func (o *Orch) sendTurn(ctx context.Context, wsID uuid.UUID, k sessKey, turn protocol.Turn) {
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.LockWorkspace(ctx, wsID); err != nil {
			return err
		}
		st, err := o.load(ctx, t.Q, wsID)
		if err != nil {
			return err
		}
		m, ok := st.member[k.member]
		if !ok {
			return nil
		}
		var task *db.Task
		for i := range st.tasks {
			x := &st.tasks[i]
			if x.MemberID != nil && (x.Status == "doing" || x.Status == "review") && taskKey(*x) == k {
				task = x
			}
		}
		if task != nil && turn.Kind != "chat" && turn.Kind != "comment" {
			turn.TaskID = &task.ID
			if turn.AttributedUserID == nil {
				turn.AttributedUserID = task.AttributedUserID
			}
		}
		runner := st.runnerFor(k, task)
		if why := st.whyWaiting(runner, m.Cli, alwaysTrue); why != "" {
			if turn.Kind == "chat" {
				return o.chat(ctx, t, st.ws, m.ID, "event", "Can’t reply yet: "+why+".", map[string]any{"event": "waiting"})
			}
			return nil
		}
		mounts, unavailable := o.mounts(ctx, st, *runner, k, m, task, turn.AttributedUserID)
		if unavailable != "" {
			return nil
		}
		notes, err := t.Q.ListNotes(ctx, wsID)
		if err != nil {
			return err
		}
		return o.deliver(ctx, t, st, *runner, k, m, task, mounts, turn, notes)
	})
	if err != nil {
		slog.ErrorContext(ctx, "send turn", "member", k.member, "err", err)
	}
}

// SendChat hands a person's chat message to a member. Chat runs on the chatting person's
// computer; the reply comes back as a chat message and the tokens are the chatter's.
func (o *Orch) SendChat(ctx context.Context, wsID, memberID, userID uuid.UUID, who, text string) {
	o.sendTurn(ctx, wsID, personKey(memberID, userID), protocol.Turn{TurnID: newID().String(), Kind: "chat", AttributedUserID: &userID,
		Text: "Message from " + who + " in Canopy chat:\n\n" + text + "\n\nReply briefly to them."})
}

// SendComment forwards a task comment to the session working on the task; the reply is
// posted in the task's thread.
func (o *Orch) SendComment(ctx context.Context, wsID, memberID, taskID, userID uuid.UUID, who, title, text string) {
	task, err := o.Store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	k := taskKey(task)
	k.member = memberID
	o.sendTurn(ctx, wsID, k, protocol.Turn{TurnID: newID().String(), Kind: "comment", AttributedUserID: &userID,
		Text:          "Comment from " + who + " on the task “" + title + "”:\n\n" + text + "\n\nReply briefly; your reply is posted in the task’s comments.",
		CommentTaskID: &taskID})
}

// expireSignals: gates and directory requests count as Deny after 30 minutes.
func (o *Orch) expireSignals(ctx context.Context) {
	expired, err := o.Store.ExpiredSignals(ctx, ptr(o.now()))
	if err != nil {
		return
	}
	for _, sig := range expired {
		err := o.Core.Tx(ctx, func(t *core.Tx) error {
			ns, err := t.Q.ResolveSignal(ctx, db.ResolveSignalParams{ID: sig.ID, Status: "timed_out", Resolution: mustJSON(map[string]any{"action": "deny", "by": "timeout"})})
			if store.IsNotFound(err) {
				return nil
			} else if err != nil {
				return err
			}
			ws, err := t.Q.GetWorkspace(ctx, sig.WorkspaceID)
			if err != nil {
				return err
			}
			if err := emitSignal(ctx, t, ws, ns, "signal.resolved"); err != nil {
				return err
			}
			if sig.SessionID != nil {
				sess, err := t.Q.GetAgentSession(ctx, *sig.SessionID)
				if err == nil && sess.RunnerID != nil && sig.RequestID != "" {
					no := false
					res := protocol.ToolResult{SessionID: sess.ID, CallID: sig.RequestID, Allow: &no, Text: "No one answered within 30 minutes, so this counts as denied. Work around it."}
					runnerID := *sess.RunnerID
					t.AfterCommit(func() { o.send(runnerID, protocol.TToolResult, res) })
					if sess.Status == "gate" {
						if s, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: sess.ID, Status: "running"}); err == nil {
							_ = emitSession(ctx, t, ws, s)
						}
					}
				}
			}
			if sig.Type == "dir_access" && sig.RequestID == "" && sig.TaskID != nil {
				var p map[string]any
				_ = json.Unmarshal(sig.Payload, &p)
				if dirID, err := uuid.Parse(fmt.Sprint(p["directory_id"])); err == nil {
					if err := o.dropTaskDir(ctx, t, ws, *sig.TaskID, dirID); err != nil {
						return err
					}
				}
			}
			t.AfterCommit(func() { o.Kick(ctx, ws.ID) })
			return o.audit(ctx, t.Q, ws, nil, "signals.timed_out", "No answer within 30 minutes; denied")
		})
		if err != nil {
			slog.ErrorContext(ctx, "expire signal", "signal", sig.ID, "err", err)
		}
	}
}

var _ = domain.Read
