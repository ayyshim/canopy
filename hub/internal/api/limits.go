package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

func (s *Server) personUsage(ctx context.Context, q *db.Queries, org db.Organization, userID uuid.UUID) (core.PersonUsage, error) {
	return core.UserUsage(ctx, q, org, userID, s.Now())
}

// queueIfOverLimit holds new goals and chats from a person at 100% of a personal limit;
// running work finishes its current step.
func (s *Server) queueIfOverLimit(ctx context.Context, ws db.Workspace, userID uuid.UUID, kind string, payload map[string]any) (bool, string, error) {
	reason, err := s.limitReason(ctx, ws, userID)
	if err != nil || reason == "" {
		return false, "", err
	}
	payload["workspace_id"] = ws.ID
	b, _ := json.Marshal(payload)
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		qw, err := t.Q.CreateQueuedWork(ctx, db.CreateQueuedWorkParams{ID: newID(), OrgID: ws.OrgID, WorkspaceID: ws.ID, UserID: userID, Kind: kind, Payload: b, Reason: reason})
		if err != nil {
			return err
		}
		e := core.WS(ws.OrgID, ws.ID, "queued.updated")
		e.ForUserID = &userID
		e.AdminOnly = true
		return t.Emit(ctx, e, map[string]any{"id": qw.ID, "kind": kind, "reason": reason, "user_id": userID})
	})
	return true, reason, err
}

// limitReason says why a person can't start new work now, or "" when they can.
func (s *Server) limitReason(ctx context.Context, ws db.Workspace, userID uuid.UUID) (string, error) {
	org, err := s.Store.GetOrganization(ctx, ws.OrgID)
	if err != nil {
		return "", err
	}
	u, err := s.personUsage(ctx, s.Store.Queries, org, userID)
	if err != nil {
		return "", err
	}
	switch {
	case u.SessionUsed >= u.SessionLimit:
		return fmt.Sprintf("You’ve used your 5-hour limit (%s of %s tokens). This starts at %s, when the limit frees up.",
			compact(u.SessionUsed), compact(u.SessionLimit), u.SessionResetAt.Format("15:04")), nil
	case u.WeekUsed >= u.WeekLimit:
		return fmt.Sprintf("You’ve used your weekly limit (%s of %s tokens). This starts after the reset on Monday.", compact(u.WeekUsed), compact(u.WeekLimit)), nil
	}
	return "", nil
}

func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// DrainQueue starts queued goals and chats from people who are back under their limits.
func (s *Server) DrainQueue(ctx context.Context) {
	items, err := s.Store.ListQueuedWork(ctx)
	if err != nil {
		return
	}
	for _, it := range items {
		ws, err := s.Store.GetWorkspace(ctx, it.WorkspaceID)
		if err != nil {
			_ = s.Store.DeleteQueuedWork(ctx, it.ID)
			continue
		}
		org, err := s.Store.GetOrganization(ctx, ws.OrgID)
		if err != nil {
			continue
		}
		u, err := s.personUsage(ctx, s.Store.Queries, org, it.UserID)
		if err != nil || u.SessionUsed >= u.SessionLimit || u.WeekUsed >= u.WeekLimit {
			continue
		}
		var p struct {
			ManagerID   uuid.UUID `json:"manager_id"`
			MemberID    uuid.UUID `json:"member_id"`
			Title       string    `json:"title"`
			DoneWhen    string    `json:"done_when"`
			Description string    `json:"description"`
			Text        string    `json:"text"`
		}
		_ = json.Unmarshal(it.Payload, &p)
		err = s.Core.Tx(ctx, func(t *core.Tx) error {
			if err := t.Q.DeleteQueuedWork(ctx, it.ID); err != nil {
				return err
			}
			switch it.Kind {
			case "goal":
				if _, err := core.CreateGoal(ctx, t, ws, core.NewGoal{ManagerID: p.ManagerID, Title: p.Title, DoneWhen: p.DoneWhen, Description: p.Description, By: it.UserID}); err != nil {
					return err
				}
			case "chat":
				if err := s.deliverChat(ctx, t, ws, p.MemberID, it.UserID, p.Text, false); err != nil {
					return err
				}
			}
			e := core.WS(ws.OrgID, ws.ID, "queued.updated")
			e.ForUserID, e.AdminOnly = &it.UserID, true
			return t.Emit(ctx, e, map[string]any{"id": it.ID, "started": true, "user_id": it.UserID})
		})
		if err != nil {
			slog.ErrorContext(ctx, "drain queued work", "err", err)
			continue
		}
		if s.Orch != nil {
			s.Orch.Kick(ctx, ws.ID)
		}
	}
}
