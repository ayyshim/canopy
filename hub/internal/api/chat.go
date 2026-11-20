package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

// Chatter sends chat messages and comments to agents (the orchestrator).
type Chatter interface {
	SendChat(ctx context.Context, wsID, memberID, userID uuid.UUID, who, text string)
	SendComment(ctx context.Context, wsID, memberID, taskID, userID uuid.UUID, who, title, text string)
}

// loadChatMember loads a member and refuses isolated staff to anyone but their creator.
func (s *Server) loadChatMember(r *http.Request) (db.Workspace, authz.Viewer, db.Member, error) {
	ws, v, m, err := s.loadMember(r)
	if err != nil {
		return ws, v, m, err
	}
	if m.Isolated && (m.CreatedBy == nil || *m.CreatedBy != identity(r).UserID) {
		return ws, v, m, httpx.ErrForbidden
	}
	return ws, v, m, nil
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) error {
	_, _, m, err := s.loadChatMember(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	limit := int32(200)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 1000 {
		limit = int32(n)
	}
	rows, err := s.Store.ListChatMessages(ctx, db.ListChatMessagesParams{MemberID: m.ID, Limit: limit})
	if err != nil {
		return err
	}
	out := make([]core.ChatMessageView, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, core.ToChat(rows[i]))
	}
	if err := s.Store.MarkChatRead(ctx, db.MarkChatReadParams{UserID: identity(r).UserID, MemberID: m.ID}); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"messages": out})
	return nil
}

func (s *Server) handleMarkChatRead(w http.ResponseWriter, r *http.Request) error {
	_, _, m, err := s.loadChatMember(r)
	if err != nil {
		return err
	}
	if err := s.Store.MarkChatRead(r.Context(), db.MarkChatReadParams{UserID: identity(r).UserID, MemberID: m.ID}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handlePostChat sends a message to a member, or with as_goal gives its manager a goal.
func (s *Server) handlePostChat(w http.ResponseWriter, r *http.Request) error {
	ws, _, m, err := s.loadChatMember(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	var in struct {
		Text   string `json:"text"`
		AsGoal bool   `json:"as_goal"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		return httpx.Validation{"text": "Write a message."}.Err()
	}
	if utf8.RuneCountInString(in.Text) > 8000 {
		return httpx.Validation{"text": "Keep messages under 8,000 characters."}.Err()
	}
	if in.AsGoal && m.Kind != "manager" {
		return httpx.Validation{"as_goal": "Only managers take goals."}.Err()
	}
	kind := "chat"
	payload := map[string]any{"member_id": m.ID, "text": in.Text}
	if in.AsGoal {
		kind = "goal"
		payload = map[string]any{"manager_id": m.ID, "title": in.Text}
	}
	if queued, reason, err := s.queueIfOverLimit(ctx, ws, uid, kind, payload); err != nil {
		return err
	} else if queued {
		httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true, "reason": reason})
		return nil
	}
	var msg db.ChatMessage
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if in.AsGoal {
			g, err := core.CreateGoal(ctx, t, ws, core.NewGoal{ManagerID: m.ID, Title: in.Text, DoneWhen: "", By: uid})
			if err != nil {
				return err
			}
			refs, _ := json.Marshal(map[string]any{"goal_id": g.ID})
			msg, err = t.Q.CreateChatMessage(ctx, db.CreateChatMessageParams{ID: newID(), WorkspaceID: ws.ID, MemberID: m.ID, AuthorUserID: &uid,
				Kind: "human", Text: in.Text, AsGoal: true, Refs: refs})
			if err != nil {
				return err
			}
			if s.Orch != nil {
				t.AfterCommit(func() { s.Orch.Kick(context.WithoutCancel(ctx), ws.ID) })
			}
			return t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, m.ID, "chat.message"), core.ToChat(msg))
		}
		return s.deliverChatMsg(ctx, t, ws, m.ID, uid, in.Text, &msg)
	})
	if err != nil {
		return err
	}
	_ = s.Store.MarkChatRead(ctx, db.MarkChatReadParams{UserID: uid, MemberID: m.ID})
	httpx.JSON(w, http.StatusCreated, map[string]any{"message": core.ToChat(msg)})
	return nil
}

func (s *Server) deliverChat(ctx context.Context, t *core.Tx, ws db.Workspace, memberID, userID uuid.UUID, text string, _ bool) error {
	var msg db.ChatMessage
	return s.deliverChatMsg(ctx, t, ws, memberID, userID, text, &msg)
}

func (s *Server) deliverChatMsg(ctx context.Context, t *core.Tx, ws db.Workspace, memberID, userID uuid.UUID, text string, out *db.ChatMessage) error {
	msg, err := t.Q.CreateChatMessage(ctx, db.CreateChatMessageParams{ID: newID(), WorkspaceID: ws.ID, MemberID: memberID, AuthorUserID: &userID,
		Kind: "human", Text: text, Refs: []byte("{}")})
	if err != nil {
		return err
	}
	*out = msg
	if err := t.Emit(ctx, core.WSMember(ws.OrgID, ws.ID, memberID, "chat.message"), core.ToChat(msg)); err != nil {
		return err
	}
	if err := t.Q.InsertAuditEvent(ctx, db.InsertAuditEventParams{ID: newID(), OrgID: &ws.OrgID, WorkspaceID: &ws.ID, ActorUserID: &userID,
		Type: "chats.sent", Summary: "Sent a message: " + truncateText(text, 120)}); err != nil {
		return err
	}
	if c, ok := s.Orch.(Chatter); ok {
		who := "a teammate"
		if u, err := t.Q.GetUserByID(ctx, userID); err == nil {
			who = u.Name
		}
		t.AfterCommit(func() { c.SendChat(context.WithoutCancel(ctx), ws.ID, memberID, userID, who, text) })
	}
	return nil
}

// handlePostComment adds a comment to a task and forwards it to the assignee.
func (s *Server) handlePostComment(w http.ResponseWriter, r *http.Request) error {
	view, task, err := s.loadTask(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	var in struct {
		Text string `json:"text"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		return httpx.Validation{"text": "Write a comment."}.Err()
	}
	var c db.Comment
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		var err error
		c, err = t.Q.CreateComment(ctx, db.CreateCommentParams{ID: newID(), TaskID: task.ID, AuthorUserID: &uid, Text: in.Text})
		if err != nil {
			return err
		}
		e := core.WS(view.ws.OrgID, view.ws.ID, "task.comment")
		e.MemberID = task.MemberID
		if err := t.Emit(ctx, e, map[string]any{"id": c.ID, "task_id": c.TaskID, "author_user_id": c.AuthorUserID, "text": c.Text, "created_at": c.CreatedAt}); err != nil {
			return err
		}
		if chatter, ok := s.Orch.(Chatter); ok && task.MemberID != nil && task.Status != "done" {
			who := "a teammate"
			if u, err := t.Q.GetUserByID(ctx, uid); err == nil {
				who = u.Name
			}
			memberID := *task.MemberID
			t.AfterCommit(func() {
				chatter.SendComment(context.WithoutCancel(ctx), view.ws.ID, memberID, task.ID, uid, who, task.Title, in.Text)
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"comment": map[string]any{"id": c.ID, "task_id": c.TaskID, "author_user_id": c.AuthorUserID, "text": c.Text, "created_at": c.CreatedAt}})
	return nil
}

func truncateText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
