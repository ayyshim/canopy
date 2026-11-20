package core

import (
	"context"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store/db"
)

// MaxAttachmentBytes caps one attached file.
const MaxAttachmentBytes = 25 << 20

// AttachmentView is a file attached to a goal or a task.
type AttachmentView struct {
	ID        uuid.UUID  `json:"id"`
	GoalID    *uuid.UUID `json:"goal_id,omitempty"`
	TaskID    *uuid.UUID `json:"task_id,omitempty"`
	Name      string     `json:"name"`
	Mime      string     `json:"mime"`
	Size      int64      `json:"size"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`   // a person attached it
	MemberID  *uuid.UUID `json:"member_id,omitempty"` // an agent attached it
	CreatedAt time.Time  `json:"created_at"`
}

func ToAttachment(a db.Attachment) AttachmentView {
	return AttachmentView{ID: a.ID, GoalID: a.GoalID, TaskID: a.TaskID, Name: a.Name, Mime: a.Mime, Size: a.Size,
		UserID: a.UserID, MemberID: a.MemberID, CreatedAt: a.CreatedAt}
}

// AttachmentName makes a safe file name: no folders, no leading dots, at most 200 characters.
func AttachmentName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.TrimLeft(name, ".")
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == ':' {
			return '_'
		}
		return r
	}, name)
	for utf8.RuneCountInString(name) > 200 {
		_, n := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-n]
	}
	if name == "" {
		return "file"
	}
	return name
}

// attachmentEvent reaches the people who see the goal's manager or the task's member.
func attachmentEvent(ws db.Workspace, seenVia *uuid.UUID, typ string) realtime.Event {
	if seenVia != nil {
		return WSMember(ws.OrgID, ws.ID, *seenVia, typ)
	}
	return WS(ws.OrgID, ws.ID, typ)
}

// AddAttachment records a stored file on a goal or task and tells the board.
func AddAttachment(ctx context.Context, t *Tx, ws db.Workspace, seenVia *uuid.UUID, p db.CreateAttachmentParams) (db.Attachment, error) {
	p.ID, p.WorkspaceID, p.Name = uuid.Must(uuid.NewV7()), ws.ID, AttachmentName(p.Name)
	a, err := t.Q.CreateAttachment(ctx, p)
	if err != nil {
		return a, err
	}
	return a, t.Emit(ctx, attachmentEvent(ws, seenVia, "attachment.created"), ToAttachment(a))
}

// RemoveAttachment deletes an attachment; its stored file stays (it may be shared by hash).
func RemoveAttachment(ctx context.Context, t *Tx, ws db.Workspace, seenVia *uuid.UUID, a db.Attachment) error {
	if err := t.Q.DeleteAttachment(ctx, a.ID); err != nil {
		return err
	}
	return t.Emit(ctx, attachmentEvent(ws, seenVia, "attachment.deleted"), map[string]any{"id": a.ID})
}
