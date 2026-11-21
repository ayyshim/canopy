package core

import (
	"context"
	"time"

	"github.com/google/uuid"

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

func UserUsage(ctx context.Context, q *db.Queries, org db.Organization, userID uuid.UUID, now time.Time) (PersonUsage, error) {
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

// Reason says why a person can't start new work now, or "" when they can.
func (u PersonUsage) Reason() string {
	switch {
	case u.SessionUsed >= u.SessionLimit:
		return "5-hour limit used"
	case u.WeekUsed >= u.WeekLimit:
		return "weekly limit used"
	}
	return ""
}
