package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store/db"
)

// Usage and analytics (SPEC "Usage, analytics and activity log"). Pages read the hourly
// rollups, which already include the current hour.

type period struct {
	from, to time.Time
	loc      *time.Location
	label    string
}

func (s *Server) periodFor(r *http.Request, org db.Organization) period {
	loc, err := time.LoadLocation(org.Timezone)
	if err != nil {
		loc = time.UTC
	}
	start := WeekStart(s.Now(), org.Timezone)
	p := period{from: start, to: start.AddDate(0, 0, 7), loc: loc, label: "this_week"}
	if r.URL.Query().Get("week") == "last" {
		p = period{from: start.AddDate(0, 0, -7), to: start, loc: loc, label: "last_week"}
	}
	return p
}

type usageAgg struct {
	total    int64
	byDayWS  map[string]map[string]int64 // day → workspace → tokens
	byModel  map[string]int64
	byUser   map[uuid.UUID]int64
	byMember map[uuid.UUID]int64
	byWS     map[uuid.UUID]int64
	heatmap  [7][24]int64 // Monday-first × hour, in the organization's time zone
}

func aggregate(rows []db.UsageHourly, loc *time.Location) usageAgg {
	a := usageAgg{byDayWS: map[string]map[string]int64{}, byModel: map[string]int64{}, byUser: map[uuid.UUID]int64{},
		byMember: map[uuid.UUID]int64{}, byWS: map[uuid.UUID]int64{}}
	for _, r := range rows {
		t := r.TokensIn + r.TokensOut
		a.total += t
		local := r.Hour.In(loc)
		day := local.Format("2006-01-02")
		if a.byDayWS[day] == nil {
			a.byDayWS[day] = map[string]int64{}
		}
		a.byDayWS[day][r.WorkspaceID.String()] += t
		a.byModel[r.Model] += t
		a.byUser[r.UserID] += t
		a.byMember[r.MemberID] += t
		a.byWS[r.WorkspaceID] += t
		a.heatmap[(int(local.Weekday())+6)%7][local.Hour()] += t
	}
	return a
}

func days(p period) []string {
	var out []string
	for d := p.from.In(p.loc); d.Before(p.to); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

func (s *Server) orgForUsage(r *http.Request) (db.Organization, db.Membership, *uuid.UUID, error) {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return db.Organization{}, db.Membership{}, nil, err
	}
	m, err := authz.OrgMember(r.Context(), s.Store.Queries, orgID, identity(r).UserID)
	if err != nil {
		return db.Organization{}, m, nil, err
	}
	org, err := s.Store.GetOrganization(r.Context(), orgID)
	if err != nil {
		return org, m, nil, err
	}
	var ws *uuid.UUID
	if v := r.URL.Query().Get("workspace"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return org, m, nil, httpx.ErrNotFound
		}
		w, err := s.Store.GetWorkspace(r.Context(), id)
		if err != nil || w.OrgID != orgID {
			return org, m, nil, httpx.ErrNotFound
		}
		ws = &id
	}
	return org, m, ws, nil
}

// handleOrgUsage is /org/usage: admins see everyone; members are sent to their own page.
func (s *Server) handleOrgUsage(w http.ResponseWriter, r *http.Request) error {
	org, m, ws, err := s.orgForUsage(r)
	if err != nil {
		return err
	}
	if !authz.CanAdministerOrg(m.OrgRole) {
		return httpx.NewError(http.StatusForbidden, "forbidden", "Only admins see everyone’s usage. Open your own usage instead.")
	}
	ctx := r.Context()
	q := s.Store.Queries
	p := s.periodFor(r, org)
	rows, err := q.UsageRows(ctx, db.UsageRowsParams{OrgID: org.ID, Hour: p.from, Hour_2: p.to, WorkspaceID: ws})
	if err != nil {
		return err
	}
	a := aggregate(rows, p.loc)

	workspaces, err := q.ListOrgWorkspaces(ctx, org.ID)
	if err != nil {
		return err
	}
	wsNames := map[string]string{}
	onBoard := 0
	for _, x := range workspaces {
		wsNames[x.ID.String()] = x.Name
		if ws == nil || *ws == x.ID {
			onBoard += len(boardPresence.list(x.ID))
		}
	}
	live, err := q.LiveSessionCount(ctx, db.LiveSessionCountParams{OrgID: org.ID, WorkspaceID: ws})
	if err != nil {
		return err
	}
	goalsDone, err := q.GoalsDoneBetween(ctx, db.GoalsDoneBetweenParams{OrgID: org.ID, FinishedAt: &p.from, FinishedAt_2: &p.to, WorkspaceID: ws})
	if err != nil {
		return err
	}
	sig, err := q.SignalStats(ctx, db.SignalStatsParams{OrgID: org.ID, ResolvedAt: &p.from, ResolvedAt_2: &p.to, WorkspaceID: ws})
	if err != nil {
		return err
	}
	outputs, err := q.OutputsMadeBetween(ctx, db.OutputsMadeBetweenParams{OrgID: org.ID, CreatedAt: p.from, CreatedAt_2: p.to, WorkspaceID: ws})
	if err != nil {
		return err
	}

	perDay := []map[string]any{}
	for _, d := range days(p) {
		perDay = append(perDay, map[string]any{"day": d, "by_workspace": a.byDayWS[d]})
	}
	models := []map[string]any{}
	for mname, t := range a.byModel {
		models = append(models, map[string]any{"model": mname, "tokens": t})
	}
	sort.Slice(models, func(i, j int) bool { return models[i]["tokens"].(int64) > models[j]["tokens"].(int64) })

	people, err := q.ListOrgPeople(ctx, org.ID)
	if err != nil {
		return err
	}
	counts, err := q.PersonCounts(ctx, db.PersonCountsParams{OrgID: org.ID, CreatedAt: p.from, CreatedAt_2: p.to})
	if err != nil {
		return err
	}
	byUser := map[uuid.UUID]db.PersonCountsRow{}
	for _, c := range counts {
		byUser[c.UserID] = c
	}
	peopleOut := []map[string]any{}
	near := []map[string]any{}
	for _, pr := range people {
		u, err := s.personUsage(ctx, q, org, pr.ID)
		if err != nil {
			return err
		}
		c := byUser[pr.ID]
		row := map[string]any{"id": pr.ID, "name": pr.Name, "email": pr.Email, "avatar_color": pr.AvatarColor, "role": pr.OrgRole,
			"usage": u, "tokens": a.byUser[pr.ID], "goals": c.Goals, "decisions": c.Decisions, "last_active_at": c.LastActive}
		peopleOut = append(peopleOut, row)
		if u.SessionLimit > 0 && u.SessionUsed*100 >= u.SessionLimit*80 {
			near = append(near, map[string]any{"id": pr.ID, "name": pr.Name, "which": "session", "percent": u.SessionUsed * 100 / u.SessionLimit})
		} else if u.WeekLimit > 0 && u.WeekUsed*100 >= u.WeekLimit*80 {
			near = append(near, map[string]any{"id": pr.ID, "name": pr.Name, "which": "week", "percent": u.WeekUsed * 100 / u.WeekLimit})
		}
	}

	members, err := q.OrgMembersAll(ctx, org.ID)
	if err != nil {
		return err
	}
	stats, err := q.MemberStats(ctx, db.MemberStatsParams{OrgID: org.ID, StartedAt: p.from, StartedAt_2: p.to})
	if err != nil {
		return err
	}
	byMember := map[uuid.UUID]db.MemberStatsRow{}
	for _, st := range stats {
		byMember[st.MemberID] = st
	}
	latest := map[uuid.UUID]string{}
	for _, x := range workspaces {
		sessions, err := q.ListLatestSessions(ctx, x.ID)
		if err != nil {
			return err
		}
		for _, se := range sessions {
			latest[se.MemberID] = se.Status
		}
	}
	agents := []map[string]any{}
	for _, mm := range members {
		if ws != nil && mm.WorkspaceID != *ws {
			continue
		}
		st := byMember[mm.ID]
		status := latest[mm.ID]
		if status == "" {
			status = "not_started"
		}
		agents = append(agents, map[string]any{"id": mm.ID, "name": mm.Name, "kind": mm.Kind, "workspace_id": mm.WorkspaceID, "workspace": mm.WorkspaceName,
			"model": mm.Model, "status": status, "tokens": a.byMember[mm.ID], "sessions": st.SessionCount, "tasks_done": st.TasksDone,
			"errors": st.Errors, "avg_task_seconds": st.AvgTaskSeconds})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i]["tokens"].(int64) > agents[j]["tokens"].(int64) })
	accounts, err := s.connectionsView(ctx, org.ID)
	if err != nil {
		return err
	}
	recent, err := s.activityRows(ctx, org.ID, "", nil, s.Now().Add(time.Minute), 8)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"period": map[string]any{"label": p.label, "from": p.from, "to": p.to, "timezone": org.Timezone},
		"tiles": map[string]any{"tokens": a.total, "on_board": onBoard, "sessions_running": live, "goals_completed": goalsDone,
			"decisions": sig.Decisions, "waiting_on_people": sig.Open, "avg_answer_seconds": sig.AvgAnswerSeconds, "outputs": outputs},
		"near_limit": near, "tokens_per_day": perDay, "workspaces": wsNames, "today": s.Now().In(p.loc).Format("2006-01-02"),
		"people": peopleOut, "models": models, "heatmap": a.heatmap, "agents": agents, "accounts": accounts,
		"limits": map[string]any{"session": org.DefaultSessionLimit, "week": org.DefaultWeeklyLimit}, "recent": recent,
	})
	return nil
}

// handlePersonUsage is /org/usage/:personId; members may open only their own.
func (s *Server) handlePersonUsage(w http.ResponseWriter, r *http.Request) error {
	org, m, ws, err := s.orgForUsage(r)
	if err != nil {
		return err
	}
	personID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	if personID != identity(r).UserID && !authz.CanAdministerOrg(m.OrgRole) {
		return httpx.ErrForbidden
	}
	ctx := r.Context()
	q := s.Store.Queries
	pm, err := authz.OrgMember(ctx, q, org.ID, personID)
	if err != nil {
		return err
	}
	person, err := q.GetUserByID(ctx, personID)
	if err != nil {
		return err
	}
	p := s.periodFor(r, org)
	usage, err := s.personUsage(ctx, q, org, personID)
	if err != nil {
		return err
	}
	rows, err := q.UsageRows(ctx, db.UsageRowsParams{OrgID: org.ID, Hour: p.from.AddDate(0, 0, -7), Hour_2: p.to, WorkspaceID: ws})
	if err != nil {
		return err
	}
	var mine, prev []db.UsageHourly
	for _, row := range rows {
		if row.UserID != personID {
			continue
		}
		if row.Hour.Before(p.from) {
			prev = append(prev, row)
		} else {
			mine = append(mine, row)
		}
	}
	a, b := aggregate(mine, p.loc), aggregate(prev, p.loc)
	counts, err := q.PersonCounts(ctx, db.PersonCountsParams{OrgID: org.ID, CreatedAt: p.from, CreatedAt_2: p.to})
	if err != nil {
		return err
	}
	var c db.PersonCountsRow
	for _, x := range counts {
		if x.UserID == personID {
			c = x
		}
	}
	workspaces, _ := q.ListOrgWorkspaces(ctx, org.ID)
	wsNames := map[string]string{}
	for _, x := range workspaces {
		wsNames[x.ID.String()] = x.Name
	}
	members, _ := q.OrgMembersAll(ctx, org.ID)
	memberNames := map[uuid.UUID]string{}
	for _, mm := range members {
		memberNames[mm.ID] = mm.Name
	}
	perDay := []map[string]any{}
	for _, d := range days(p) {
		perDay = append(perDay, map[string]any{"day": d, "by_workspace": a.byDayWS[d]})
	}
	split := []map[string]any{}
	for id, t := range a.byWS {
		split = append(split, map[string]any{"workspace_id": id, "name": wsNames[id.String()], "tokens": t})
	}
	agents := []map[string]any{}
	for id, t := range a.byMember {
		agents = append(agents, map[string]any{"member_id": id, "name": memberNames[id], "tokens": t})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i]["tokens"].(int64) > agents[j]["tokens"].(int64) })
	sessions, err := q.PersonSessions(ctx, db.PersonSessionsParams{OrgID: org.ID, AttributedUserID: &personID, At: p.from})
	if err != nil {
		return err
	}
	sessOut := []map[string]any{}
	for _, se := range sessions {
		live := false
		if se.SessionID != nil {
			if x, err := q.GetAgentSession(ctx, *se.SessionID); err == nil {
				live = x.Status == "running" || x.Status == "waiting" || x.Status == "starting"
			}
		}
		name := ""
		if se.WorkspaceID != nil {
			name = wsNames[se.WorkspaceID.String()]
		}
		agent := ""
		if se.MemberID != nil {
			agent = memberNames[*se.MemberID]
		}
		sessOut = append(sessOut, map[string]any{"started": se.Started, "ended": se.Ended, "tokens": se.Tokens, "workspace": name, "agent": agent, "live": live})
	}
	recent, err := s.activityRows(ctx, org.ID, "", &personID, s.Now().Add(time.Minute), 10)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"person":    map[string]any{"id": person.ID, "name": person.Name, "email": person.Email, "avatar_color": person.AvatarColor, "role": pm.OrgRole},
		"period":    map[string]any{"label": p.label, "from": p.from, "to": p.to, "timezone": org.Timezone},
		"usage":     usage,
		"overrides": map[string]any{"session": pm.PersonalSessionLimit, "week": pm.PersonalWeeklyLimit},
		"change":    map[string]any{"this_period": a.total, "previous_period": b.total},
		"tiles": map[string]any{"tokens": a.total, "goals": c.Goals, "messages": c.Messages, "decisions": c.Decisions, "reviews": c.Reviews,
			"avg_answer_seconds": c.AvgAnswerSeconds},
		"tokens_per_day": perDay, "workspaces": wsNames, "sessions": sessOut, "workspace_split": split, "agents": agents,
		"heatmap": a.heatmap, "recent": recent,
	})
	return nil
}

// activityFilters maps the log's filter chips to audit event types.
var activityFilters = map[string]struct {
	prefix string
	types  []string
}{
	"goals":     {prefix: "goals."},
	"approvals": {types: []string{"signals.gate", "signals.dir_access", "signals.owner_change", "signals.usage_limit", "signals.cap", "signals.timed_out"}},
	"answers":   {types: []string{"signals.question", "answers.delegated"}},
	"reviews":   {types: []string{"signals.review"}},
	"access":    {prefix: "access."},
	"people":    {prefix: "people."},
	"chats":     {prefix: "chats."},
}

func (s *Server) activityRows(ctx context.Context, orgID uuid.UUID, filter string, actor *uuid.UUID, before time.Time, limit int32) ([]map[string]any, error) {
	f := activityFilters[filter]
	if f.types == nil {
		f.types = []string{}
	}
	rows, err := s.Store.ListActivity(ctx, db.ListActivityParams{OrgID: &orgID, At: before, Limit: limit, Prefix: f.prefix, Types: f.types, Actor: actor})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{"id": a.ID, "at": a.At, "type": a.Type, "summary": a.Summary, "actor_id": a.ActorUserID,
			"actor": a.ActorName, "workspace_id": a.WorkspaceID, "workspace": a.WorkspaceName})
	}
	return out, nil
}

// handleActivity is /org/activity (admins), newest first, filterable, exportable as CSV.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	filter := r.URL.Query().Get("type")
	if filter == "everything" {
		filter = ""
	}
	before := s.Now().Add(time.Minute)
	if v := r.URL.Query().Get("before"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			before = t
		}
	}
	limit := int32(100)
	csvOut := r.URL.Query().Get("format") == "csv"
	if csvOut {
		limit = 100000
	} else if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = int32(n)
	}
	rows, err := s.activityRows(ctx, orgID, filter, nil, before, limit)
	if err != nil {
		return err
	}
	if csvOut {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="canopy-activity.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"when", "person", "what happened", "where", "type"})
		for _, a := range rows {
			_ = cw.Write([]string{a["at"].(time.Time).Format(time.RFC3339), deref(a["actor"].(*string)), a["summary"].(string), deref(a["workspace"].(*string)), a["type"].(string)})
		}
		cw.Flush()
		return nil
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"activity": rows})
	return nil
}

// hubUsage adds hub-wide analytics to the admin dashboard: tokens per day by organization.
func (s *Server) hubUsage(ctx context.Context) (map[string]any, error) {
	since := s.Now().AddDate(0, 0, -13).Truncate(24 * time.Hour)
	rows, err := s.Store.HubUsageRows(ctx, since)
	if err != nil {
		return nil, err
	}
	perDay := map[string]map[string]int64{}
	byOrg := map[string]int64{}
	var total int64
	for _, r := range rows {
		d := r.Hour.UTC().Format("2006-01-02")
		if perDay[d] == nil {
			perDay[d] = map[string]int64{}
		}
		perDay[d][r.OrgID.String()] += r.Tokens
		byOrg[r.OrgID.String()] += r.Tokens
		total += r.Tokens
	}
	daysOut := []map[string]any{}
	for d := since; !d.After(s.Now()); d = d.AddDate(0, 0, 1) {
		k := d.UTC().Format("2006-01-02")
		daysOut = append(daysOut, map[string]any{"day": k, "by_org": perDay[k]})
	}
	live, err := s.Store.HubLiveSessionCount(ctx)
	if err != nil {
		return nil, err
	}
	orgs, err := s.Store.AdminListOrganizations(ctx, db.AdminListOrganizationsParams{Lim: 1000})
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	top := []map[string]any{}
	for _, o := range orgs {
		names[o.ID.String()] = o.Name
		top = append(top, map[string]any{"id": o.ID, "name": o.Name, "tokens": byOrg[o.ID.String()], "members": o.Members})
	}
	sort.Slice(top, func(i, j int) bool { return top[i]["tokens"].(int64) > top[j]["tokens"].(int64) })
	if len(top) > 10 {
		top = top[:10]
	}
	return map[string]any{"tokens_14d": total, "sessions_running": live, "per_day": daysOut, "org_names": names, "top_orgs": top}, nil
}

var _ = fmt.Sprint
var _ = strings.TrimSpace
