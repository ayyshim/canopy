package orch

import (
	"context"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

type outputFile struct {
	Name, Title, Kind, BlobKey, Mime, Note string
	Size                                   int64
}

// KindFor guesses an output kind from its name and type (SPEC "Outputs").
func KindFor(name, mime string) string {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case strings.HasPrefix(mime, "image/") || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".svg":
		return "image"
	case strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/") || ext == ".mp4" || ext == ".webm" || ext == ".mov":
		return "recording"
	case ext == ".diff" || ext == ".patch":
		return "diff"
	case ext == ".csv" || ext == ".json" || strings.Contains(strings.ToLower(name), "report"):
		return "report"
	default:
		return "document"
	}
}

func validKind(k string) bool {
	switch k {
	case "image", "recording", "document", "diff", "report":
		return true
	}
	return false
}

// sniffMime fixes the type of a file with no extension, which the runner can only call
// octet-stream. HTML and images are then stored with their real type so the viewer shows them.
func sniffMime(name, mime string, head []byte) string {
	if path.Ext(name) != "" || len(head) == 0 {
		return mime
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])) {
	case "", "application/octet-stream", "text/plain":
		if got := http.DetectContentType(head); got != "application/octet-stream" {
			return got
		}
	}
	return mime
}

func (o *Orch) sniffOutput(ctx context.Context, f outputFile) string {
	if o.Blobs == nil || path.Ext(f.Name) != "" {
		return f.Mime
	}
	r, _, err := o.Blobs.Get(ctx, f.BlobKey)
	if err != nil {
		return f.Mime
	}
	defer r.Close()
	head, _ := io.ReadAll(io.LimitReader(r, 512))
	return sniffMime(f.Name, f.Mime, head)
}

// addOutputVersion records a new version of an output (same name on the same task = same
// fruit). With review, the task waits in Review until a person decides.
func (o *Orch) addOutputVersion(ctx context.Context, t *core.Tx, ws db.Workspace, task db.Task, m db.Member, sess *db.Session, f outputFile, review bool) error {
	q := t.Q
	f.Mime = o.sniffOutput(ctx, f)
	if f.Kind == "" || !validKind(f.Kind) {
		f.Kind = KindFor(f.Name, f.Mime)
	}
	if f.Title == "" {
		f.Title = f.Name
	}
	out, err := q.UpsertOutput(ctx, db.UpsertOutputParams{ID: newID(), WorkspaceID: ws.ID, TaskID: &task.ID, MemberID: &m.ID, Kind: f.Kind, Name: f.Name, Title: f.Title})
	if err != nil {
		return err
	}
	versions, err := q.ListOutputVersions(ctx, out.ID)
	if err != nil {
		return err
	}
	if n := len(versions); n > 0 && versions[n-1].BlobKey == f.BlobKey {
		return nil // the same content again (the folder watcher and the tool both saw it)
	}
	model := m.Model
	params := db.CreateOutputVersionParams{ID: newID(), OutputID: out.ID, BlobKey: f.BlobKey, Mime: f.Mime, Size: f.Size, Note: f.Note, Model: model}
	if sess != nil {
		params.SessionID = &sess.ID
	}
	ver, err := q.CreateOutputVersion(ctx, params)
	if err != nil {
		return err
	}
	status := "new"
	if review {
		status = "review"
	}
	if out, err = q.SetOutputStatus(ctx, db.SetOutputStatusParams{ID: out.ID, Status: status}); err != nil {
		return err
	}
	versions = append(versions, ver)
	typ := "output.created"
	if ver.Number > 1 {
		typ = "output.version"
	}
	if err := t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, m.ID, typ), core.ToOutput(out, versions)); err != nil {
		return err
	}
	if err := o.chat(ctx, t, ws, m.ID, "event", "Made output “"+out.Title+"”", map[string]any{"event": "output", "output_id": out.ID, "version": ver.Number}); err != nil {
		return err
	}
	if task.GoalID != nil {
		if g, err := q.GetGoal(ctx, *task.GoalID); err == nil {
			if err := core.GoalEvent(ctx, t, ws, g, "output", "Output “"+out.Title+"”", map[string]any{"output_id": out.ID, "version": ver.Number}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *Orch) toolSubmitOutput(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Path        string `json:"path"`
		Title       string `json:"title"`
		Kind        string `json:"kind"`
		NeedsReview bool   `json:"needs_review"`
		Note        string `json:"note"`
		// Filled in by the runner after it uploaded the file.
		Name    string `json:"name"`
		SHA256  string `json:"sha256"`
		Size    int64  `json:"size"`
		Mime    string `json:"mime"`
		BlobKey string `json:"blob_key"`
		Error   string `json:"upload_error"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	if in.Error != "" {
		return toolError(in.Error)
	}
	if tc.task == nil {
		return toolError("Outputs belong to a task; you have no task right now.")
	}
	if in.BlobKey == "" {
		return toolError("The file couldn’t be read. Save it inside your outputs folder and try again.")
	}
	if in.Name == "" {
		in.Name = path.Base(in.Path)
	}
	in.Name = strings.TrimPrefix(strings.ReplaceAll(in.Name, "\\", "/"), "/")
	if in.Name == "" || slices.Contains(strings.Split(in.Name, "/"), "..") {
		return toolError("That file name isn’t allowed. Use a path inside your outputs folder.")
	}
	in.Name = path.Clean(in.Name)
	return o.Core.Tx(ctx, func(t *core.Tx) error {
		task, err := t.Q.GetTaskForUpdate(ctx, tc.task.ID)
		if err != nil {
			return err
		}
		if err := o.addOutputVersion(ctx, t, tc.ws, task, tc.member, &tc.sess, outputFile{Name: in.Name, Title: in.Title, Kind: in.Kind,
			BlobKey: in.BlobKey, Mime: in.Mime, Size: in.Size, Note: in.Note}, in.NeedsReview); err != nil {
			return err
		}
		if !in.NeedsReview {
			t.AfterCommit(func() { o.reply(tc, "Saved “"+firstNonEmpty(in.Title, in.Name)+"”.", false) })
			return nil
		}
		outs, err := t.Q.ListOutputs(ctx, tc.ws.ID)
		if err != nil {
			return err
		}
		var outID string
		for _, x := range outs {
			if x.TaskID != nil && *x.TaskID == task.ID && x.Name == in.Name {
				outID = x.ID.String()
			}
		}
		sig, err := t.Q.CreateSignal(ctx, db.CreateSignalParams{ID: newID(), WorkspaceID: tc.ws.ID, Type: "review", MemberID: &tc.member.ID, TaskID: &task.ID,
			SessionID: &tc.sess.ID, GoalID: task.GoalID,
			Payload: mustJSON(map[string]any{"output_id": outID, "title": firstNonEmpty(in.Title, in.Name)}), RequestID: tc.call.CallID})
		if err != nil {
			return err
		}
		nt, err := t.Q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, Status: ptr("review")})
		if err != nil {
			return err
		}
		if err := emitTask(ctx, t, tc.ws, nt); err != nil {
			return err
		}
		s, err := t.Q.SetSessionStatus(ctx, db.SetSessionStatusParams{ID: tc.sess.ID, Status: "review"})
		if err != nil {
			return err
		}
		if err := emitSession(ctx, t, tc.ws, s); err != nil {
			return err
		}
		return emitSignal(ctx, t, tc.ws, sig, "signal.opened")
	})
}

// onFileWritten: the runner saw a new file in a task's outputs folder.
func (o *Orch) onFileWritten(ctx context.Context, rn db.Runner, f protocol.FileWritten) {
	sess, err := o.Store.GetAgentSession(ctx, f.SessionID)
	if err != nil || sess.RunnerID == nil || *sess.RunnerID != rn.ID || f.TaskID == nil || f.BlobKey == "" {
		return
	}
	task, err := o.Store.GetTask(ctx, *f.TaskID)
	if err != nil || task.WorkspaceID != sess.WorkspaceID {
		return
	}
	ws, err := o.Store.GetWorkspace(ctx, sess.WorkspaceID)
	if err != nil {
		return
	}
	m, err := o.Store.GetMember(ctx, sess.MemberID)
	if err != nil {
		return
	}
	_ = o.Core.Tx(ctx, func(t *core.Tx) error {
		return o.addOutputVersion(ctx, t, ws, task, m, &sess, outputFile{Name: f.Name, BlobKey: f.BlobKey, Mime: f.Mime, Size: f.Size}, false)
	})
}
