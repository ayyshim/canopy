package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

// SessionWindow is the length of a person's current usage session (SPEC "Per-person limits").
const SessionWindow = 5 * time.Hour

// PersonUsage is a person's Canopy-level usage against their limits.
type PersonUsage struct {
	SessionUsed    int64     `json:"session_used"`
	SessionLimit   int64     `json:"session_limit"`
	SessionResetAt time.Time `json:"session_reset_at"`
	WeekUsed       int64     `json:"week_used"`
	WeekLimit      int64     `json:"week_limit"`
	WeekResetAt    time.Time `json:"week_reset_at"`
}

// WeekStart is Monday 00:00 in the organization's time zone.
func WeekStart(now time.Time, tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	t := now.In(loc)
	days := (int(t.Weekday()) + 6) % 7 // Monday = 0
	return time.Date(t.Year(), t.Month(), t.Day()-days, 0, 0, 0, 0, loc)
}

func (s *Server) personUsage(ctx context.Context, q *db.Queries, org db.Organization, userID uuid.UUID) (PersonUsage, error) {
	now := s.Now()
	m, err := q.GetMembership(ctx, db.GetMembershipParams{OrgID: org.ID, UserID: userID})
	if err != nil {
		return PersonUsage{}, err
	}
	u := PersonUsage{SessionLimit: org.DefaultSessionLimit, WeekLimit: org.DefaultWeeklyLimit}
	if m.PersonalSessionLimit != nil {
		u.SessionLimit = *m.PersonalSessionLimit
	}
	if m.PersonalWeeklyLimit != nil {
		u.WeekLimit = *m.PersonalWeeklyLimit
	}
	since := now.Add(-SessionWindow)
	if u.SessionUsed, err = q.UserTokensSince(ctx, db.UserTokensSinceParams{OrgID: org.ID, AttributedUserID: &userID, At: since}); err != nil {
		return u, err
	}
	oldest, err := q.OldestUserUsageSince(ctx, db.OldestUserUsageSinceParams{OrgID: org.ID, AttributedUserID: &userID, At: since})
	if err != nil {
		return u, err
	}
	u.SessionResetAt = oldest.Add(SessionWindow)
	week := WeekStart(now, org.Timezone)
	u.WeekResetAt = week.AddDate(0, 0, 7)
	if u.WeekUsed, err = q.UserTokensSince(ctx, db.UserTokensSinceParams{OrgID: org.ID, AttributedUserID: &userID, At: week}); err != nil {
		return u, err
	}
	return u, nil
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
