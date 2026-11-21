package orch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// toolCtx is one agent tool call with what it belongs to.
type toolCtx struct {
	runner db.Runner
	call   protocol.ToolCall
	sess   db.Session
	ws     db.Workspace
	member db.Member
	task   *db.Task
}

func (o *Orch) reply(tc *toolCtx, text string, isErr bool) {
	o.send(tc.runner.ID, protocol.TToolResult, protocol.ToolResult{SessionID: tc.sess.ID, CallID: tc.call.CallID, Text: text, IsError: isErr})
}

func (o *Orch) onToolCall(ctx context.Context, rn db.Runner, c protocol.ToolCall) {
	tc := &toolCtx{runner: rn, call: c}
	sess, err := o.Store.GetAgentSession(ctx, c.SessionID)
	if err != nil || sess.RunnerID == nil || *sess.RunnerID != rn.ID {
		o.send(rn.ID, protocol.TToolResult, protocol.ToolResult{SessionID: c.SessionID, CallID: c.CallID, Text: "Unknown session.", IsError: true})
		return
	}
	tc.sess = sess
	if tc.ws, err = o.Store.GetWorkspace(ctx, sess.WorkspaceID); err != nil {
		o.reply(tc, "Workspace not found.", true)
		return
	}
	if tc.member, err = o.Store.GetMember(ctx, sess.MemberID); err != nil {
		o.reply(tc, "Member not found.", true)
		return
	}
	if sess.TaskID != nil {
		if t, err := o.Store.GetTask(ctx, *sess.TaskID); err == nil {
			tc.task = &t
		}
	}
	var handle func(context.Context, *toolCtx) error
	switch c.Tool {
	case protocol.ToolPermission:
		handle = o.toolPermission
	case protocol.ToolAskQuestion:
		handle = o.toolAskQuestion
	case protocol.ToolRequestDir:
		handle = o.toolRequestDirectory
	case protocol.ToolReportProgress:
		handle = o.toolReportProgress
	case protocol.ToolSubmitOutput:
		handle = o.toolSubmitOutput
	case protocol.ToolCompleteTask:
		handle = o.toolCompleteTask
	case protocol.ToolSubmitPlan:
		handle = o.toolSubmitPlan
	case protocol.ToolAssignTasks:
		handle = o.toolAssignTasks
	case protocol.ToolAnswerQuestion:
		handle = o.toolAnswerQuestion
	case protocol.ToolOwnerChange:
		handle = o.toolOwnerChange
	case protocol.ToolReadNotes:
		handle = o.toolReadNotes
	case protocol.ToolReadItem:
		handle = o.toolReadItem
	case protocol.ToolUpdateNote:
		handle = o.toolUpdateNote
	case protocol.ToolWriteNote:
		handle = o.toolWriteNote
	case protocol.ToolAssignSkill:
		handle = o.toolAssignSkill
	case protocol.ToolProposeStaff:
		handle = o.toolProposeStaff
	case protocol.ToolUpdateStaff:
		handle = o.toolUpdateStaffInstructions
	case protocol.ToolGetStaff:
		handle = o.toolGetStaffInstructions
	case protocol.ToolReadBoard:
		handle = o.toolReadBoard
	case protocol.ToolGetAttachments:
		handle = o.toolGetAttachments
	case protocol.ToolAttachFile:
		handle = o.toolAttachFile
	default:
		o.reply(tc, "Unknown tool "+c.Tool, true)
		return
	}
	if err := handle(ctx, tc); err != nil {
		var te toolError
		if errorsAs(err, &te) {
			o.reply(tc, string(te), true)
			return
		}
		slog.ErrorContext(ctx, "tool call", "tool", c.Tool, "err", err)
		o.reply(tc, "Canopy couldn’t handle that call. Try again.", true)
	}
}

// toolError is a message for the agent, not a server failure.
type toolError string

func (e toolError) Error() string { return string(e) }

func errorsAs(err error, te *toolError) bool {
	if e, ok := err.(toolError); ok {
		*te = e
		return true
	}
	return false
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return toolError("Invalid arguments: " + err.Error())
	}
	return nil
}

// ---- permission (Claude Code's --permission-prompt-tool) ----

// actionKind groups calls for "Always" rules: the tool, and for shell commands the program.
func actionKind(tool string, input map[string]any) (kind, summary string) {
	switch tool {
	case "Bash":
		cmd, _ := input["command"].(string)
		fields := strings.Fields(cmd)
		prog := ""
		if len(fields) > 0 {
			prog = path.Base(fields[0])
		}
		if prog == "git" && len(fields) > 1 {
			prog = "git " + fields[1]
		}
		return "Bash(" + prog + ")", cmd
	case "WebFetch":
		u, _ := input["url"].(string)
		return tool, u
	case "WebSearch":
		q, _ := input["query"].(string)
		return tool, q
	default:
		for _, k := range []string{"file_path", "path", "command", "url"} {
			if v, ok := input[k].(string); ok {
				return tool, v
			}
		}
		return tool, tool
	}
}

// autoAllowed lets everything through except destructive shell commands.
// The sandbox already limits what other tools can reach.
func autoAllowed(tool string, input map[string]any) bool {
	if tool != "Bash" {
		return true
	}
	cmd, _ := input["command"].(string)
	return !isDestructive(cmd)
}

func (o *Orch) toolPermission(ctx context.Context, tc *toolCtx) error {
	var in struct {
		ToolName string         `json:"tool_name"`
		Input    map[string]any `json:"input"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	allow := func(ok bool, text string) {
		o.send(tc.runner.ID, protocol.TToolResult, protocol.ToolResult{SessionID: tc.sess.ID, CallID: tc.call.CallID, Text: text, Allow: &ok})
	}
	kind, summary := actionKind(in.ToolName, in.Input)
	if autoAllowed(in.ToolName, in.Input) {
		allow(true, "")
		return nil
	}
	if _, err := o.Store.GetGateRule(ctx, db.GetGateRuleParams{MemberID: tc.member.ID, ActionKind: kind}); err == nil {
		allow(true, "")
		return nil
	} else if !store.IsNotFound(err) {
		return err
	}
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: tc.ws.ID, Type: "gate", MemberID: &tc.member.ID, TaskID: taskIDOf(tc.task),
			SessionID: &tc.sess.ID, GoalID: tc.goalID(),
			Payload: mustJSON(map[string]any{"tool": in.ToolName, "action_kind": kind, "summary": summary}), RequestID: tc.call.CallID,
			ExpiresAt: ptr(o.now().Add(o.GateTimeout))})
		if err != nil {
			return err
		}
		s, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: tc.sess.ID, Status: "gate"})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, tc.ws, s); err != nil {
			return err
		}
		if err := o.chat(ctx, t, tc.ws, tc.member.ID, "event", "Needs approval: "+summary, map[string]any{"event": "signal", "signal_id": sig.ID}); err != nil {
			return err
		}
		return emitSignal(ctx, t, tc.ws, sig, "signal.opened")
	})
}

func (tc *toolCtx) goalID() *uuid.UUID {
	if tc.task == nil {
		return nil
	}
	return tc.task.GoalID
}

func taskIDOf(t *db.Task) *uuid.UUID {
	if t == nil {
		return nil
	}
	return &t.ID
}

// ---- ask_question ----

func (o *Orch) toolAskQuestion(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Question  string   `json:"question"`
		Options   []string `json:"options"`
		Suggested *int     `json:"suggested"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Question) == "" {
		return toolError("Ask a question.")
	}
	if len(in.Options) > 6 {
		in.Options = in.Options[:6]
	}
	if in.Suggested != nil && (*in.Suggested < 0 || *in.Suggested >= len(in.Options)) {
		in.Suggested = nil
	}
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: tc.ws.ID, Type: "question", MemberID: &tc.member.ID, TaskID: taskIDOf(tc.task),
			SessionID: &tc.sess.ID, GoalID: tc.goalID(),
			Payload: mustJSON(map[string]any{"question": in.Question, "options": nonNil(in.Options), "suggested": in.Suggested,
				"can_delegate": tc.member.ReportsTo != nil}), RequestID: tc.call.CallID})
		if err != nil {
			return err
		}
		if err := o.chat(ctx, t, tc.ws, tc.member.ID, "event", "Question: "+in.Question, map[string]any{"event": "signal", "signal_id": sig.ID}); err != nil {
			return err
		}
		if err := o.log(ctx, t, tc.ws, &tc.sess.ID, tc.member.ID, taskIDOf(tc.task), "ask", in.Question); err != nil {
			return err
		}
		return emitSignal(ctx, t, tc.ws, sig, "signal.opened")
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- request_directory ----

// findDirectory matches a path (the directory itself or anything inside it) or a name.
func findDirectory(dirs []db.Directory, p string) *db.Directory {
	p = strings.TrimRight(strings.TrimSpace(p), "/")
	for i := range dirs {
		d := strings.TrimRight(dirs[i].Path, "/")
		if p == d || strings.HasPrefix(p, d+"/") || strings.EqualFold(p, dirs[i].Name) {
			return &dirs[i]
		}
	}
	return nil
}

func dirList(dirs []db.Directory) string {
	var names []string
	for _, d := range dirs {
		names = append(names, d.Path)
	}
	return strings.Join(names, ", ")
}

func (o *Orch) toolRequestDirectory(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Path   string `json:"path"`
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if in.Mode != "read_write" {
		in.Mode = "read"
	}
	dirs, err := o.Store.ListDirectories(ctx, tc.ws.ID)
	if err != nil {
		return err
	}
	dir := findDirectory(dirs, in.Path)
	if dir == nil {
		return toolError("That isn’t one of this workspace’s directories. Directories: " + dirList(dirs))
	}
	grants, err := o.Store.ListMemberGrants(ctx, tc.member.ID)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.DirectoryID == dir.ID && rank(g.Mode) >= rank(in.Mode) {
			// The grant exists; the person this work is for may still lack access.
			if tc.task != nil && tc.task.AttributedUserID != nil && in.Mode == "read_write" {
				explicit := map[uuid.UUID]string{}
				if acc, err := o.Store.ListDirectoryAccess(ctx, tc.ws.ID); err == nil {
					for _, a := range acc {
						if a.DirectoryID == dir.ID {
							explicit[a.UserID] = a.Mode
						}
					}
				}
				if authz.PersonDirMode(*dir, *tc.task.AttributedUserID, explicit) != "read_write" {
					return toolError("The person this work is for can’t change " + dir.Path + ". Call request_owner_change so its owner can approve the change.")
				}
			}
			o.reply(tc, "You already have "+strings.ReplaceAll(g.Mode, "_", " ")+" access to "+dir.Path+".", false)
			return nil
		}
	}
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: tc.ws.ID, Type: "dir_access", MemberID: &tc.member.ID, TaskID: taskIDOf(tc.task),
			SessionID: &tc.sess.ID, GoalID: tc.goalID(),
			Payload:   mustJSON(map[string]any{"directory_id": dir.ID, "path": dir.Path, "mode": in.Mode, "reason": in.Reason}),
			RequestID: tc.call.CallID, ExpiresAt: ptr(o.now().Add(o.GateTimeout))})
		if err != nil {
			return err
		}
		s, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: tc.sess.ID, Status: "gate"})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, tc.ws, s); err != nil {
			return err
		}
		if err := o.chat(ctx, t, tc.ws, tc.member.ID, "event", "Asked for access to "+dir.Path, map[string]any{"event": "signal", "signal_id": sig.ID}); err != nil {
			return err
		}
		return emitSignal(ctx, t, tc.ws, sig, "signal.opened")
	})
}

// ---- report_progress ----

func (o *Orch) toolReportProgress(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Step     int    `json:"step"`
		Progress *int   `json:"progress"`
		Note     string `json:"note"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if tc.task == nil {
		o.reply(tc, "Noted.", false)
		return nil
	}
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		task, err := t.Q.GetTaskForUpdate(ctx, tc.task.ID)
		if err != nil {
			return err
		}
		steps := core.TaskSteps(task)
		cur := task.CurrentStep
		if in.Step > 0 {
			for i := range steps {
				if i < in.Step {
					steps[i].Done = true
				}
			}
			cur = int32(min(in.Step, len(steps)))
		}
		progress := task.Progress
		if in.Progress != nil {
			progress = int32(max(0, min(99, *in.Progress)))
		} else if len(steps) > 0 {
			done := 0
			for _, s := range steps {
				if s.Done {
					done++
				}
			}
			progress = int32(done * 95 / len(steps))
		}
		nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Steps: mustJSON(steps), CurrentStep: &cur, Progress: &progress})
		if err != nil {
			return err
		}
		if err := emitTask(ctx, t, tc.ws, nt); err != nil {
			return err
		}
		if in.Note != "" {
			if err := o.log(ctx, t, tc.ws, &tc.sess.ID, tc.member.ID, &task.ID, "agent", in.Note); err != nil {
				return err
			}
		}
		t.AfterCommit(func() { o.reply(tc, "Noted.", false) })
		return nil
	})
}

// ---- complete_task ----

func (o *Orch) toolCompleteTask(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Summary string `json:"summary"`
		// For a triage task that needs no work: why.
		NoWorkReason string `json:"no_work_reason"`
		// Filled in by the runner after it merged the task's branch into the goal's branch.
		Git      []protocol.RepoResult `json:"git"`
		GitError string                `json:"git_error"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if tc.task == nil {
		return toolError("You have no task right now.")
	}
	if in.GitError != "" {
		return toolError(in.GitError)
	}
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.LockWorkspace(ctx, tc.ws.ID); err != nil {
			return err
		}
		task, err := t.Q.GetTaskForUpdate(ctx, tc.task.ID)
		if err != nil {
			return err
		}
		if task.Status == "done" {
			t.AfterCommit(func() { o.reply(tc, "This task is already done.", false) })
			return nil
		}
		open, err := t.Q.ListTaskOpenSignals(ctx, &task.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(open, func(s db.Signal) bool { return s.Type == "review" }) {
			return toolError("An output is still waiting for review.")
		}
		if task.Kind == "plan" && task.GoalID != nil {
			g, err := t.Q.GetGoal(ctx, *task.GoalID)
			if err != nil {
				return err
			}
			if g.Status == "planning" {
				return toolError("Call submit_plan with your plan first.")
			}
		}
		if task.Kind == "triage" && task.GoalID != nil && strings.TrimSpace(in.NoWorkReason) == "" {
			goalTasks, err := t.Q.ListGoalTasks(ctx, task.GoalID)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(goalTasks, func(x db.Task) bool { return x.ParentTaskID != nil && *x.ParentTaskID == task.ID }) {
				return toolError("Call assign_tasks to hand this to your staff first. If no work is needed, call complete_task again with no_work_reason.")
			}
		}
		steps := core.TaskSteps(task)
		for i := range steps {
			steps[i].Done = true
		}
		nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: ptr("done"), Progress: ptr(int32(100)), Steps: mustJSON(steps),
			CurrentStep: ptr(int32(len(steps))), Summary: &in.Summary, SetFinished: true})
		if err != nil {
			return err
		}
		if err := t.Q.ResetTaskNudges(ctx, task.ID); err != nil {
			return err
		}
		if err := o.recordRepoResults(ctx, t, tc.ws, nt, tc.member, &tc.sess, in.Git); err != nil {
			return err
		}
		if err := emitTask(ctx, t, tc.ws, nt); err != nil {
			return err
		}
		s, err := t.Q.UpdateAgentSession(ctx, db.UpdateAgentSessionParams{ID: tc.sess.ID, SetTask: true})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, tc.ws, s); err != nil {
			return err
		}
		if err := o.chat(ctx, t, tc.ws, tc.member.ID, "event", "Finished “"+nt.Title+"”", map[string]any{"event": "finished", "task_id": nt.ID}); err != nil {
			return err
		}
		if nt.GoalID != nil && nt.Kind != "plan" {
			g, err := t.Q.GetGoal(ctx, *nt.GoalID)
			if err != nil {
				return err
			}
			if err := core.GoalEvent(ctx, t, tc.ws, g, "task_finished", "Finished “"+nt.Title+"”", map[string]any{"task_id": nt.ID}); err != nil {
				return err
			}
		}
		if nt.Kind == "owner_change" && nt.ParentTaskID != nil {
			// Tell the requester's thread that the owner's change landed.
			if parent, err := t.Q.GetTask(ctx, *nt.ParentTaskID); err == nil && parent.MemberID != nil {
				if err := o.chat(ctx, t, tc.ws, *parent.MemberID, "event", "Approved change done: "+nt.Title, map[string]any{"event": "finished", "task_id": nt.ID}); err != nil {
					return err
				}
			}
		}
		t.AfterCommit(func() {
			o.reply(tc, "Task marked done.", false)
			o.Kick(ctx, tc.ws.ID)
		})
		return nil
	})
}

// ---- answer_question (a manager deciding for its staff) ----

func (o *Orch) toolAnswerQuestion(ctx context.Context, tc *toolCtx) error {
	var in struct {
		SignalID uuid.UUID `json:"signal_id"`
		Answer   string    `json:"answer"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	sig, err := o.Store.GetSignal(ctx, in.SignalID)
	if err != nil || sig.WorkspaceID != tc.ws.ID || sig.Type != "question" || sig.Status != "open" || sig.MemberID == nil {
		return toolError("No open question with that id.")
	}
	asker, err := o.Store.GetMember(ctx, *sig.MemberID)
	if err != nil || asker.ReportsTo == nil || *asker.ReportsTo != tc.member.ID {
		return toolError("That question isn’t from your staff.")
	}
	if _, err := o.Resolve(ctx, sig.ID, nil, Resolution{Action: "answer", Text: in.Answer, By: tc.member.Name}); err != nil {
		return toolError(err.Error())
	}
	o.reply(tc, "Answer sent to "+asker.Name+".", false)
	return nil
}

// ---- request_owner_change (DECISIONS: cross-owner writes) ----

func (o *Orch) toolOwnerChange(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Directory   string `json:"directory"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Description) == "" {
		return toolError("Describe the change.")
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = firstLine(in.Description, 80)
	}
	dirs, err := o.Store.ListDirectories(ctx, tc.ws.ID)
	if err != nil {
		return err
	}
	dir := findDirectory(dirs, in.Directory)
	if dir == nil {
		return toolError("That isn’t one of this workspace’s directories. Directories: " + dirList(dirs))
	}
	var requester *uuid.UUID
	if tc.task != nil {
		requester = tc.task.AttributedUserID
	}
	var created db.Task
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		st, err := o.load(ctx, t.Q, tc.ws.ID)
		if err != nil {
			return err
		}
		created, err = o.createOwnerChange(ctx, t, st, *dir, in.Title, in.Description, requester, &tc.member.ID, taskIDOf(tc.task))
		return err
	})
	if err != nil {
		return err
	}
	owner, _ := o.Store.GetUserByID(ctx, dir.OwnerUserID)
	o.reply(tc, fmt.Sprintf("Requested %s. %s owns %s and must approve it; you can continue with other work meanwhile.", taskLabel(created), owner.Name, dir.Path), false)
	return nil
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len([]rune(s)) > n {
		return string([]rune(s)[:n-1]) + "…"
	}
	return s
}

// createOwnerChange makes a task, for an agent that can write the directory, that waits for
// the directory's owner to approve it. Work approved by the owner is attributed to the owner.
func (o *Orch) createOwnerChange(ctx context.Context, t *core.Tx, st *wsState, dir db.Directory, title, desc string, requester, byMember, parent *uuid.UUID) (db.Task, error) {
	assignee := pickWriter(st, dir.ID, byMember)
	var goalID *uuid.UUID
	if parent != nil {
		if pt, ok := st.task[*parent]; ok {
			goalID = pt.GoalID // stays with the requester's goal on the tree
		}
	}
	task, err := t.Q.CreateTask(ctx, db.CreateTaskParams{ID: newID(), WorkspaceID: st.ws.ID, GoalID: goalID, MemberID: assignee, Kind: "owner_change",
		Title: title, Description: desc, Status: "waiting", DependsOn: []uuid.UUID{}, Steps: []byte("[]"),
		RequiredDirs: mustJSON([]core.DirNeed{{DirectoryID: dir.ID, Mode: "read_write"}}), AttributedUserID: &dir.OwnerUserID,
		CreatedBy: requester, CreatedByMember: byMember, ParentTaskID: parent})
	if err != nil {
		return task, err
	}
	sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: st.ws.ID, Type: "owner_change", MemberID: assignee, TaskID: &task.ID,
		Payload: mustJSON(map[string]any{"directory_id": dir.ID, "path": dir.Path, "owner_user_id": dir.OwnerUserID, "requested_by_user_id": requester,
			"requested_by_member_id": byMember, "title": title, "description": desc})})
	if err != nil {
		return task, err
	}
	if err := emitTask(ctx, t, st.ws, task); err != nil {
		return task, err
	}
	e := core.WS(st.ws.OrgID, st.ws.ID, "signal.opened")
	e.MemberID, e.ForUserID = sig.MemberID, &dir.OwnerUserID
	return task, t.Emit(ctx, e, core.ToSignal(sig))
}

// pickWriter chooses who carries out an owner-approved change: someone already able to write
// the directory (not busy first), else the requester's manager.
func pickWriter(st *wsState, dirID uuid.UUID, requester *uuid.UUID) *uuid.UUID {
	var best *uuid.UUID
	for _, m := range st.members {
		if m.Isolated || st.grantMode(m.ID, dirID) != "read_write" {
			continue
		}
		id := m.ID
		if !st.busyAnywhere(m.ID) {
			return &id
		}
		if best == nil {
			best = &id
		}
	}
	if best != nil {
		return best
	}
	if requester != nil {
		if m, ok := st.member[*requester]; ok && m.ReportsTo != nil {
			return m.ReportsTo
		}
		return requester
	}
	return nil
}
