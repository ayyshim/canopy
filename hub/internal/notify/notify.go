// Package notify emails people about what needs them (questions, approvals, errors, goals
// that are ready) and sends the daily summary, following each person's notification choices.
// Desktop notifications are shown by the web app itself.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Pref is one row of the notification table on the account page.
type Pref struct {
	Desktop bool `json:"desktop"`
	Email   bool `json:"email"`
	Push    bool `json:"push"`
}

type Prefs map[string]Pref

// Kinds are the rows people can turn on and off.
var Kinds = []string{"questions", "progress", "errors", "goal_ready", "daily"}

// Defaults: everything that needs you is on; the daily summary is off until asked for.
func Defaults() Prefs {
	return Prefs{
		"questions":  {Desktop: true, Email: true, Push: true},
		"progress":   {},
		"errors":     {Desktop: true, Email: true, Push: true},
		"goal_ready": {Desktop: true, Email: true, Push: true},
		"daily":      {},
	}
}

// Parse reads stored choices on top of the defaults.
func Parse(raw []byte) Prefs {
	p := Defaults()
	// Push follows the desktop choice until the person sets it.
	var saved map[string]struct {
		Desktop bool  `json:"desktop"`
		Email   bool  `json:"email"`
		Push    *bool `json:"push"`
	}
	if json.Unmarshal(raw, &saved) == nil {
		for k, v := range saved {
			if _, ok := p[k]; !ok {
				continue
			}
			pref := Pref{Desktop: v.Desktop, Email: v.Email, Push: v.Desktop && k != "progress" && k != "daily"}
			if v.Push != nil {
				pref.Push = *v.Push
			}
			p[k] = pref
		}
	}
	return p
}

// KindOf maps a signal type to its notification row ("" when it's never emailed).
func KindOf(signalType string) string {
	switch signalType {
	case "question", "gate", "dir_access", "review", "owner_change", "sync_conflict", "usage_limit", "cap", "staff_proposal":
		return "questions"
	case "error", "git":
		return "errors"
	case "goal_ready":
		return "goal_ready"
	}
	return ""
}

type Notifier struct {
	Store     *store.Store
	Mail      mail.Sender
	PublicURL string
	Now       func() time.Time
	// Delay: a signal still open after this long is emailed, so people working on the board
	// don't get mail for things they answer right away.
	Delay time.Duration
	// DailyHour is the local hour the daily summary goes out.
	DailyHour int
}

func New(s *store.Store, m mail.Sender, publicURL string) *Notifier {
	return &Notifier{Store: s, Mail: m, PublicURL: strings.TrimRight(publicURL, "/"), Now: time.Now, Delay: 2 * time.Minute, DailyHour: 8}
}

// Run checks once a minute until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		n.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass sends whatever is due now.
func (n *Notifier) Pass(ctx context.Context) {
	if err := n.emailSignals(ctx); err != nil {
		slog.WarnContext(ctx, "notification emails", "err", err)
	}
	if err := n.dailySummaries(ctx); err != nil {
		slog.WarnContext(ctx, "daily summaries", "err", err)
	}
}

func (n *Notifier) emailSignals(ctx context.Context) error {
	q := n.Store.Queries
	sigs, err := q.ListUnmailedSignals(ctx, n.Now().Add(-n.Delay))
	if err != nil {
		return err
	}
	sent := map[string]bool{} // one email per person and subject in a pass (limits repeat per workspace)
	for _, sig := range sigs {
		if err := q.MarkSignalEmailed(ctx, sig.ID); err != nil {
			return err
		}
		kind := KindOf(sig.Type)
		if kind == "" {
			continue
		}
		ws, err := q.GetWorkspace(ctx, sig.WorkspaceID)
		if err != nil {
			continue
		}
		subject, body := SignalText(ctx, n.Store.Queries, ws, sig)
		for _, uid := range Recipients(ctx, n.Store.Queries, ws, sig) {
			u, err := q.GetUserByID(ctx, uid)
			if err != nil || !Parse(u.Notifications)[kind].Email || sent[uid.String()+subject] {
				continue
			}
			sent[uid.String()+subject] = true
			msg := mail.Message{To: u.Email, Subject: subject, Text: fmt.Sprintf("Hi %s,\n\n%s\n\nOpen the board: %s/w/%s\n\n%s", firstName(u.Name), body, n.PublicURL, ws.ID, n.footer())}
			if err := n.Mail.Send(ctx, msg); err != nil {
				slog.WarnContext(ctx, "send notification", "err", err)
			}
		}
	}
	return nil
}

// Recipients are the people a signal is for, limited to those who can still see it.
func Recipients(ctx context.Context, q *db.Queries, ws db.Workspace, sig db.Signal) []uuid.UUID {
	var want []uuid.UUID
	var payload map[string]any
	_ = json.Unmarshal(sig.Payload, &payload)
	switch {
	case sig.ForUserID != nil:
		want = append(want, *sig.ForUserID)
	case sig.Type == "owner_change" || sig.Type == "sync_conflict":
		if id, err := uuid.Parse(fmt.Sprint(payload["owner_user_id"])); err == nil {
			want = append(want, id)
		}
	case sig.TaskID != nil:
		if t, err := q.GetTask(ctx, *sig.TaskID); err == nil && t.AttributedUserID != nil {
			want = append(want, *t.AttributedUserID)
		}
	case sig.GoalID != nil:
		if g, err := q.GetGoal(ctx, *sig.GoalID); err == nil && g.CreatedBy != nil {
			want = append(want, *g.CreatedBy)
		}
	}
	if len(want) == 0 {
		// Nobody in particular: the workspace's admins.
		if people, err := q.ListOrgPeople(ctx, ws.OrgID); err == nil {
			for _, p := range people {
				if authz.CanAdministerOrg(p.OrgRole) && p.Status == "active" {
					want = append(want, p.ID)
				}
			}
		}
		if access, err := q.ListWorkspaceAccess(ctx, ws.ID); err == nil {
			for _, a := range access {
				if a.Role == "admin" {
					want = append(want, a.UserID)
				}
			}
		}
	}
	var member *db.Member
	if sig.MemberID != nil {
		if m, err := q.GetMember(ctx, *sig.MemberID); err == nil {
			member = &m
		}
	}
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, uid := range want {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		v, err := authz.WorkspaceViewer(ctx, q, ws, uid)
		if err != nil || (member != nil && !v.CanSeeMember(*member)) {
			continue
		}
		out = append(out, uid)
	}
	return out
}

func SignalText(ctx context.Context, q *db.Queries, ws db.Workspace, sig db.Signal) (subject, body string) {
	var p map[string]any
	_ = json.Unmarshal(sig.Payload, &p)
	get := func(k string) string {
		if v, ok := p[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	who := "An agent"
	if sig.MemberID != nil {
		if m, err := q.GetMember(ctx, *sig.MemberID); err == nil && m.Name != "" {
			who = m.Name
		}
	}
	in := " in " + ws.Name
	switch sig.Type {
	case "question":
		return who + " has a question" + in, who + " asks: " + get("question")
	case "gate":
		return who + " wants your approval" + in, who + " wants to run:\n\n    " + get("summary") + "\n\nIt waits until you allow or deny it."
	case "dir_access":
		verb := "read"
		if get("mode") == "read_write" {
			verb = "change"
		}
		return who + " asks for access" + in, who + " wants to " + verb + " " + get("path") + "."
	case "review":
		return "Review “" + get("title") + "”" + in, who + " made “" + get("title") + "”. The task finishes once you approve it."
	case "owner_change":
		return "Your approval for a change" + in, who + " wants to change " + get("path") + " in a folder you own:\n\n" + get("description")
	case "sync_conflict":
		return "Sync conflict in " + get("path"), "You changed " + get("path") + " while it was offline, and an agent changed it too. Choose which version to keep."
	case "usage_limit":
		return "A usage limit was reached" + in, "A computer’s CLI hit its usage limit. Agents on it wait until it resets, unless you choose otherwise."
	case "cap":
		return "Daily token cap" + in, "A computer used " + get("level") + "% of its daily token cap."
	case "error":
		return who + " stopped on an error" + in, who + " stopped: " + get("message")
	case "git":
		return "A goal’s branch needs attention" + in, get("message")
	case "goal_ready":
		return "“" + get("title") + "” is ready" + in, "The goal “" + get("title") + "” is ready. Review the work and ship it or mark it done."
	}
	return "Something needs you" + in, ""
}

func (n *Notifier) footer() string {
	return "You get this email because of your notification settings: " + n.PublicURL + "/account"
}

// dailySummaries sends each person who asked for it a summary once a day, after DailyHour in
// the time zone of their current organization.
func (n *Notifier) dailySummaries(ctx context.Context) error {
	q := n.Store.Queries
	users, err := q.ListDailySummaryUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		loc, err := time.LoadLocation(u.Timezone)
		if err != nil {
			loc = time.UTC
		}
		now := n.Now().In(loc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if now.Hour() < n.DailyHour || (u.DailySentOn.Valid && !u.DailySentOn.Time.Before(today)) {
			continue
		}
		if err := q.SetUserDailySent(ctx, db.SetUserDailySentParams{ID: u.ID, DailySentOn: pgDate(today)}); err != nil {
			return err
		}
		text, ok := n.summary(ctx, u.ID, n.Now().Add(-24*time.Hour))
		if !ok {
			continue // nothing happened; no email
		}
		msg := mail.Message{To: u.Email, Subject: "Your Canopy summary for " + now.Format("Monday, January 2"),
			Text: fmt.Sprintf("Hi %s,\n\n%s\n%s", firstName(u.Name), text, n.footer())}
		if err := n.Mail.Send(ctx, msg); err != nil {
			slog.WarnContext(ctx, "send daily summary", "err", err)
		}
	}
	return nil
}

// summary lists, for every organization the person belongs to: goals that moved, decisions
// waiting for them and the tokens their work used.
func (n *Notifier) summary(ctx context.Context, userID uuid.UUID, since time.Time) (string, bool) {
	q := n.Store.Queries
	orgs, err := q.ListUserOrganizations(ctx, userID)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	found := false
	for _, o := range orgs {
		var goals, waiting []string
		workspaces, err := q.ListOrgWorkspaces(ctx, o.ID)
		if err != nil {
			continue
		}
		for _, ws := range workspaces {
			if _, err := authz.WorkspaceViewer(ctx, q, ws, userID); err != nil {
				continue
			}
			if moved, err := q.ListGoalsMovedSince(ctx, db.ListGoalsMovedSinceParams{WorkspaceID: ws.ID, UpdatedAt: since}); err == nil {
				for _, g := range moved {
					if g.Status == "draft" {
						continue
					}
					goals = append(goals, fmt.Sprintf("- %s (%s): %s", g.Title, ws.Name, goalStatus[g.Status]))
				}
			}
			if open, err := q.ListOpenSignals(ctx, ws.ID); err == nil {
				for _, sig := range open {
					if KindOf(sig.Type) == "" || !slices.Contains(Recipients(ctx, n.Store.Queries, ws, sig), userID) {
						continue
					}
					subject, body := SignalText(ctx, n.Store.Queries, ws, sig)
					line := "- " + subject
					if sig.Type == "question" || sig.Type == "gate" {
						// The question or command itself says more than the subject.
						line = "- " + strings.Join(strings.Fields(body), " ") + " (" + ws.Name + ")"
					}
					if !slices.Contains(waiting, line) {
						waiting = append(waiting, line)
					}
				}
			}
		}
		tokens, _ := q.UserTokensSince(ctx, db.UserTokensSinceParams{OrgID: o.ID, AttributedUserID: &userID, At: since})
		if len(goals) == 0 && len(waiting) == 0 && tokens == 0 {
			continue
		}
		found = true
		fmt.Fprintf(&b, "%s\n\n", o.Name)
		if len(goals) > 0 {
			fmt.Fprintf(&b, "Goals that moved:\n%s\n\n", strings.Join(goals, "\n"))
		}
		if len(waiting) > 0 {
			fmt.Fprintf(&b, "Waiting for you:\n%s\n\n", strings.Join(waiting, "\n"))
		}
		fmt.Fprintf(&b, "Tokens your work used in the last day: %s\n\n", thousands(tokens))
	}
	return b.String(), found
}

var goalStatus = map[string]string{"planning": "planning", "active": "in progress", "ready": "ready for you", "done": "done", "stopped": "stopped"}

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return "there"
}

func thousands(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func pgDate(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }
