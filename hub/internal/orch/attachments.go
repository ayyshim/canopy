package orch

import (
	"context"
	"fmt"
	"path"
	"strings"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// taskAttachments: files on the task's goal and on every task of that goal, so agents share them.
func taskAttachments(st *wsState, t db.Task) []db.Attachment {
	var out []db.Attachment
	for _, a := range st.attachments {
		switch {
		case a.TaskID != nil && *a.TaskID == t.ID:
		case t.GoalID != nil && a.GoalID != nil && *a.GoalID == *t.GoalID:
		case t.GoalID != nil && a.TaskID != nil && st.task[*a.TaskID].GoalID != nil && *st.task[*a.TaskID].GoalID == *t.GoalID:
		default:
			continue
		}
		out = append(out, a)
	}
	return out
}

// fileNames gives each attachment a unique file name for ./attachments.
func fileNames(as []db.Attachment) []string {
	taken := map[string]bool{}
	names := make([]string, len(as))
	for i, a := range as {
		name := a.Name
		ext := path.Ext(name)
		for n := 2; taken[name]; n++ {
			name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(a.Name, ext), n, ext)
		}
		taken[name] = true
		names[i] = name
	}
	return names
}

// attachmentsLine tells an agent which files it can download.
func attachmentsLine(as []db.Attachment) string {
	if len(as) == 0 {
		return ""
	}
	return fmt.Sprintf("\nAttached files: %s. Call get_attachments to download them into ./attachments.\n", strings.Join(fileNames(as), ", "))
}

func (o *Orch) toolGetAttachments(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if tc.task == nil {
		return toolError("Attachments belong to a goal or task; you have no task right now.")
	}
	st, err := o.load(ctx, o.Store.Queries, tc.ws.ID)
	if err != nil {
		return err
	}
	as := taskAttachments(st, *tc.task)
	names := fileNames(as)
	var files []protocol.FileRef
	for i, a := range as {
		if in.Name == "" || in.Name == names[i] || in.Name == a.Name {
			files = append(files, protocol.FileRef{Name: names[i], SHA256: a.Sha256})
		}
	}
	if len(files) == 0 {
		if in.Name != "" {
			return toolError("No attached file named “" + in.Name + "”.")
		}
		o.reply(tc, "No files are attached to your goal or task.", false)
		return nil
	}
	o.send(tc.runner.ID, protocol.TToolResult, protocol.ToolResult{SessionID: tc.sess.ID, CallID: tc.call.CallID,
		Text: fmt.Sprintf("Downloaded %d file(s):", len(files)), Files: files})
	return nil
}

func (o *Orch) toolAttachFile(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Path string `json:"path"`
		To   string `json:"to"`
		// Filled in by the runner after it uploaded the file.
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
		Mime   string `json:"mime"`
		Error  string `json:"upload_error"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if in.Error != "" {
		return toolError(in.Error)
	}
	if tc.task == nil {
		return toolError("Attachments belong to a goal or task; you have no task right now.")
	}
	if in.SHA256 == "" {
		return toolError("The file couldn’t be read. Save it in your work folder and try again.")
	}
	if in.Size > core.MaxAttachmentBytes {
		return toolError("Files must be 25 MB or smaller.")
	}
	p := db.CreateAttachmentParams{Name: in.Name, Mime: in.Mime, Size: in.Size, Sha256: in.SHA256, MemberID: &tc.member.ID}
	seenVia := tc.task.MemberID
	where := "your task"
	if in.To != "task" && tc.task.GoalID != nil {
		g, err := o.Store.GetGoal(ctx, *tc.task.GoalID)
		if err != nil {
			return err
		}
		p.GoalID, seenVia, where = &g.ID, g.ManagerID, "the goal"
	} else {
		p.TaskID = &tc.task.ID
	}
	var a db.Attachment
	err := o.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		a, err = core.AddAttachment(ctx, t, tc.ws, seenVia, p)
		return err
	})
	if err != nil {
		return err
	}
	o.reply(tc, "Attached “"+a.Name+"” to "+where+".", false)
	return nil
}
