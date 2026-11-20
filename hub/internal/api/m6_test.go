package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func addUsage(t *testing.T, e *env, org, ws, user string, tokens int64, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.store.Pool.Exec(ctx, `INSERT INTO usage_events (org_id, workspace_id, attributed_user_id, cli, model, tokens_in, tokens_out, at)
		VALUES ($1, $2, $3, 'claude', 'sonnet', $4, 0, $5)`, org, ws, user, tokens, at); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Pool.Exec(ctx, `INSERT INTO usage_hourly (org_id, hour, user_id, workspace_id, model, tokens_in, tokens_out, calls)
		VALUES ($1, date_trunc('hour', $5::timestamptz), $3, $2, 'sonnet', $4, 0, 1)
		ON CONFLICT (org_id, hour, user_id, member_id, workspace_id, model) DO UPDATE SET tokens_in = usage_hourly.tokens_in + excluded.tokens_in`,
		org, ws, user, tokens, at); err != nil {
		t.Fatal(err)
	}
}

// M6: session (5 hours) and weekly limits with personal overrides; at 100% new work queues.
func TestPersonalLimitsQueueNewWork(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Limit Co")
	org := currentOrg(me)
	ownerID := str(me["user"].(map[string]any)["id"])
	id, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])

	// A personal override of 1,000 tokens per 5-hour session.
	owner.do("PATCH", "/api/v1/orgs/"+org+"/memberships/"+ownerID, map[string]any{"set_session_limit": true, "personal_session_limit": 1000})
	addUsage(t, e, org, id, ownerID, 600, time.Now().Add(-6*time.Hour)) // outside the 5-hour window
	if r := owner.do("POST", "/api/v1/workspaces/"+id+"/goals", map[string]any{"manager_id": riley, "title": "Under the limit"}); r.Status != http.StatusCreated {
		t.Fatalf("goal under limit: %d %v", r.Status, r.Body)
	}
	addUsage(t, e, org, id, ownerID, 1000, time.Now().Add(-time.Hour))
	r := owner.do("POST", "/api/v1/workspaces/"+id+"/goals", map[string]any{"manager_id": riley, "title": "Over the limit"})
	if r.Status != http.StatusAccepted || r.Body["queued"] != true || !strings.Contains(str(r.Body["reason"]), "5-hour limit") {
		t.Fatalf("goal over limit: %d %v", r.Status, r.Body)
	}
	chat := owner.do("POST", "/api/v1/workspaces/"+id+"/members/"+riley+"/chat", map[string]any{"text": "hello"})
	if chat.Status != http.StatusAccepted {
		t.Errorf("chat over limit: %d %v", chat.Status, chat.Body)
	}
	var queued int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM queued_work WHERE user_id = $1`, ownerID).Scan(&queued)
	if queued != 2 {
		t.Errorf("queued work: %d", queued)
	}
	// Raising the override lets the queue drain.
	owner.do("PATCH", "/api/v1/orgs/"+org+"/memberships/"+ownerID, map[string]any{"set_session_limit": true, "personal_session_limit": 1_000_000})
	e.srv.DrainQueue(context.Background())
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM queued_work WHERE user_id = $1`, ownerID).Scan(&queued)
	if queued != 0 {
		t.Errorf("queue didn’t drain: %d", queued)
	}
	var goals int
	_ = e.store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM goals WHERE workspace_id = $1`, id).Scan(&goals)
	if goals != 2 {
		t.Errorf("queued goal not started: %d goals", goals)
	}

	// The weekly window starts Monday 00:00 in the organization's time zone.
	owner.do("PATCH", "/api/v1/orgs/"+org+"/memberships/"+ownerID, map[string]any{"set_weekly_limit": true, "personal_weekly_limit": 1500}) // 1,600 used this week
	r = owner.do("POST", "/api/v1/workspaces/"+id+"/goals", map[string]any{"manager_id": riley, "title": "Weekly"})
	if r.Status != http.StatusAccepted || !strings.Contains(str(r.Body["reason"]), "weekly limit") {
		t.Errorf("weekly limit: %d %v", r.Status, r.Body)
	}
}

// M6: usage and activity are admin-only at the API; members get only their own usage.
func TestUsagePagesAreAdminOnly(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Usage Co")
	org := currentOrg(me)
	ownerID := str(me["user"].(map[string]any)["id"])
	id, _ := e.createWorkspace(owner, org, setupPayload())
	member, memberMe := e.signup("Member Home")
	memberID := str(memberMe["user"].(map[string]any)["id"])
	if _, err := e.store.Pool.Exec(context.Background(), `INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'member')`, org, memberID); err != nil {
		t.Fatal(err)
	}
	addUsage(t, e, org, id, ownerID, 5000, time.Now())
	addUsage(t, e, org, id, memberID, 700, time.Now())

	r := owner.do("GET", "/api/v1/orgs/"+org+"/usage", nil)
	if r.Status != 200 || r.Body["tiles"].(map[string]any)["tokens"].(float64) != 5700 {
		t.Fatalf("admin usage: %d %v", r.Status, r.Body["tiles"])
	}
	if len(r.Body["people"].([]any)) != 2 {
		t.Errorf("people table: %v", r.Body["people"])
	}
	if r := member.do("GET", "/api/v1/orgs/"+org+"/usage", nil); r.Status != http.StatusForbidden {
		t.Errorf("member saw everyone’s usage: %d", r.Status)
	}
	if r := member.do("GET", "/api/v1/orgs/"+org+"/usage/people/"+ownerID, nil); r.Status != http.StatusForbidden {
		t.Errorf("member saw the owner’s usage: %d", r.Status)
	}
	mine := member.do("GET", "/api/v1/orgs/"+org+"/usage/people/"+memberID, nil)
	if mine.Status != 200 || mine.Body["tiles"].(map[string]any)["tokens"].(float64) != 700 {
		t.Errorf("own usage: %d %v", mine.Status, mine.Body["tiles"])
	}
	if r := member.do("GET", "/api/v1/orgs/"+org+"/activity", nil); r.Status != http.StatusForbidden {
		t.Errorf("member read the activity log: %d", r.Status)
	}
	act := owner.do("GET", "/api/v1/orgs/"+org+"/activity", nil)
	if act.Status != 200 || len(act.Body["activity"].([]any)) == 0 {
		t.Errorf("activity: %d %v", act.Status, act.Body)
	}
	csvResp := owner.raw("GET", "/api/v1/orgs/"+org+"/activity?format=csv", nil, false)
	if csvResp.Status != 200 || !strings.HasPrefix(csvResp.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("csv export: %d %v", csvResp.Status, csvResp.Header)
	}
}
