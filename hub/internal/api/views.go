package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

type userView struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	AvatarColor string    `json:"avatar_color"`
	HasPassword bool      `json:"has_password"`
	IsHubAdmin  bool      `json:"is_hub_admin"`
	Providers   []string  `json:"providers"`
	// Notifications are the person's choices, with defaults filled in.
	Notifications notify.Prefs `json:"notifications"`
	// SnoozeUntil is when push resumes; null when not paused.
	SnoozeUntil *time.Time `json:"snooze_until"`
}

type orgView struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Slug                string    `json:"slug"`
	Size                string    `json:"size"`
	About               string    `json:"about"`
	DefaultSessionLimit int64     `json:"default_session_limit"`
	DefaultWeeklyLimit  int64     `json:"default_weekly_limit"`
	Timezone            string    `json:"timezone"`
	LogoURL             *string   `json:"logo_url"`
	CreatedAt           time.Time `json:"created_at"`
}

type orgSummary struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Slug    string    `json:"slug"`
	Role    string    `json:"role"`
	LogoURL *string   `json:"logo_url"`
}

// meView is the body of GET/PATCH /me and of successful sign-in responses.
type meView struct {
	User          userView     `json:"user"`
	Organizations []orgSummary `json:"organizations"`
	// CurrentOrgID is the organization to open: the last one used if the user is still a member,
	// otherwise their first; null means they have none and should create one.
	CurrentOrgID *uuid.UUID `json:"current_org_id"`
	// Waiting is how many open things need this person, for the app badge.
	Waiting int `json:"waiting"`
}

func toOrgView(o db.Organization) orgView {
	return orgView{ID: o.ID, Name: o.Name, Slug: o.Slug, Size: o.Size, About: o.About,
		DefaultSessionLimit: o.DefaultSessionLimit, DefaultWeeklyLimit: o.DefaultWeeklyLimit,
		Timezone: o.Timezone, LogoURL: core.LogoURL("orgs", o.ID, o.LogoKey), CreatedAt: o.CreatedAt}
}

func (s *Server) buildMe(ctx context.Context, q *db.Queries, userID uuid.UUID) (meView, error) {
	u, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return meView{}, err
	}
	providers, err := q.ListUserProviders(ctx, userID)
	if err != nil {
		return meView{}, err
	}
	rows, err := q.ListUserOrganizations(ctx, userID)
	if err != nil {
		return meView{}, err
	}
	me := meView{
		User: userView{ID: u.ID, Email: u.Email, Name: u.Name, AvatarColor: u.AvatarColor,
			HasPassword: u.PasswordHash != nil, IsHubAdmin: u.IsHubAdmin, Providers: append([]string{}, providers...),
			Notifications: notify.Parse(u.Notifications), SnoozeUntil: snoozeOf(u.SnoozeUntil, s.now())},
		Organizations: make([]orgSummary, 0, len(rows)),
	}
	for _, r := range rows {
		me.Organizations = append(me.Organizations, orgSummary{ID: r.ID, Name: r.Name, Slug: r.Slug, Role: r.OrgRole, LogoURL: core.LogoURL("orgs", r.ID, r.LogoKey)})
		if u.LastOrgID != nil && *u.LastOrgID == r.ID {
			id := r.ID
			me.CurrentOrgID = &id
		}
	}
	if me.CurrentOrgID == nil && len(me.Organizations) > 0 {
		id := me.Organizations[0].ID
		me.CurrentOrgID = &id
	}
	me.Waiting = s.waitingFor(ctx, q, userID)
	return me, nil
}

func snoozeOf(t *time.Time, now time.Time) *time.Time {
	if t == nil || !t.After(now) {
		return nil
	}
	return t
}

// waitingFor counts the open things in the person's workspaces that notify them: the ones
// they can see, except cards that are only for admins' information.
func (s *Server) waitingFor(ctx context.Context, q *db.Queries, userID uuid.UUID) int {
	orgs, err := q.ListUserOrganizations(ctx, userID)
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range orgs {
		wss, err := q.ListOrgWorkspaces(ctx, o.ID)
		if err != nil {
			continue
		}
		for _, ws := range wss {
			open, err := q.ListOpenSignals(ctx, ws.ID)
			if err != nil || len(open) == 0 {
				continue
			}
			view, err := s.loadWSView(ctx, ws.ID, userID)
			if err != nil {
				continue
			}
			for _, sig := range open {
				if notify.KindOf(sig.Type) != "" && view.seesSignal(sig) {
					n++
				}
			}
		}
	}
	return n
}
