package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func setupPayload() map[string]any {
	return map[string]any{
		"workspace": map[string]any{"name": "Payments", "about": "Card payments"},
		"directories": []any{
			map[string]any{"key": "d1", "path": "/home/riley/payments-api"},
			map[string]any{"key": "d2", "path": "/home/riley/docs"},
			map[string]any{"key": "d3", "path": "/home/riley/web"},
		},
		"clis": []any{map[string]any{"cli": "claude", "enabled": true, "mode": "subscription"}},
		"members": []any{
			map[string]any{"key": "m1", "name": "Riley", "kind": "manager", "cli": "claude", "grants": []any{
				map[string]any{"directory_key": "d1", "mode": "read_write", "source": "human"},
				map[string]any{"directory_key": "d2", "mode": "read", "source": "human"},
			}},
			map[string]any{"key": "s1", "name": "Dev", "kind": "staff", "skill": "developer", "reports_to_key": "m1", "cli": "claude", "grants": []any{
				map[string]any{"directory_key": "d1", "mode": "read_write", "source": "suggested", "reason": "Writes code in the main codebase"},
			}},
			map[string]any{"key": "s2", "name": "Scribe", "kind": "staff", "skill": "writer", "reports_to_key": "m1", "cli": "claude", "grants": []any{
				map[string]any{"directory_key": "d2", "mode": "read", "source": "human"},
			}},
			map[string]any{"key": "i1", "name": "Ivy", "kind": "staff", "skill": "researcher", "cli": "claude", "grants": []any{
				map[string]any{"directory_key": "d3", "mode": "read", "source": "suggested"},
			}},
		},
		"first_goal": nil,
	}
}

func members(d map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, m := range d["members"].([]any) {
		mm := m.(map[string]any)
		if name, ok := mm["name"].(string); ok {
			out[name] = mm
		}
	}
	return out
}

func grantsFor(d map[string]any, memberID string) map[string]string {
	dirs := map[string]string{}
	for _, dd := range d["directories"].([]any) {
		x := dd.(map[string]any)
		dirs[str(x["id"])] = str(x["path"])
	}
	out := map[string]string{}
	for _, g := range d["grants"].([]any) {
		gg := g.(map[string]any)
		if str(gg["member_id"]) == memberID {
			out[dirs[str(gg["directory_id"])]] = str(gg["mode"])
		}
	}
	return out
}

func (e *env) createWorkspace(c *client, org string, payload map[string]any) (string, map[string]any) {
	e.t.Helper()
	r := c.do("POST", "/api/v1/orgs/"+org+"/workspaces", payload)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("create workspace: %d %v", r.Status, r.Body)
	}
	id := str(r.Body["workspace"].(map[string]any)["id"])
	d := c.do("GET", "/api/v1/workspaces/"+id, nil)
	if d.Status != 200 {
		e.t.Fatalf("get workspace: %d %v", d.Status, d.Body)
	}
	return id, d.Body
}

// M2: Nothing is persisted until Create workspace; what's created matches the payload exactly.
func TestSetupCreatesExactlyThePayload(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Setup Co")
	org := currentOrg(me)
	ctx := context.Background()

	// An invalid payload persists nothing (one transaction).
	bad := setupPayload()
	bad["members"] = append(bad["members"].([]any), map[string]any{"key": "x", "name": "", "kind": "staff", "skill": "developer", "cli": "claude"})
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", bad); r.Status != http.StatusUnprocessableEntity || r.field("members.4.name") == "" {
		t.Fatalf("bad payload: %d %v", r.Status, r.Body)
	}
	var n int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE org_id = $1`, org).Scan(&n)
	if n != 0 {
		t.Fatalf("invalid setup left %d workspaces behind", n)
	}

	id, d := e.createWorkspace(owner, org, setupPayload())
	ws := d["workspace"].(map[string]any)
	if str(ws["name"]) != "Payments" || str(ws["goal_completion"]) != "manual" {
		t.Errorf("workspace: %v", ws)
	}
	if len(d["directories"].([]any)) != 3 || len(d["members"].([]any)) != 4 {
		t.Fatalf("counts: %v", d)
	}
	ms := members(d)
	riley, dev, ivy := ms["Riley"], ms["Dev"], ms["Ivy"]
	if str(riley["kind"]) != "manager" || str(riley["model"]) != "opus" || str(dev["model"]) != "sonnet" {
		t.Errorf("default models: manager %v staff %v", riley["model"], dev["model"])
	}
	if str(dev["reports_to"]) != str(riley["id"]) || ivy["independent"] != true {
		t.Errorf("hierarchy: dev→%v ivy independent=%v", dev["reports_to"], ivy["independent"])
	}
	if g := grantsFor(d, str(riley["id"])); len(g) != 2 || g["/home/riley/payments-api"] != "read_write" || g["/home/riley/docs"] != "read" {
		t.Errorf("manager grants: %v", g)
	}
	if g := grantsFor(d, str(dev["id"])); len(g) != 1 || g["/home/riley/payments-api"] != "read_write" {
		t.Errorf("staff grants: %v", g)
	}
	for _, gg := range d["grants"].([]any) {
		g := gg.(map[string]any)
		if str(g["member_id"]) == str(dev["id"]) && (str(g["source"]) != "suggested" || str(g["reason"]) == "") {
			t.Errorf("suggested grant lost its label: %v", g)
		}
	}
	// Sign-ins belong to a computer; with none connected yet, nothing is set up.
	conns := owner.do("GET", "/api/v1/orgs/"+org+"/connections", nil)
	if c := conns.Body["connections"].([]any); len(c) != 0 {
		t.Errorf("connections without a computer: %v", conns.Body)
	}
	_ = id
}

// M2: At least one manager is enforced in the API.
func TestSetupNeedsAManagerAndLastManagerStays(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Manager Co")
	org := currentOrg(me)
	p := setupPayload()
	p["members"] = []any{map[string]any{"key": "s", "name": "Dev", "kind": "staff", "skill": "developer", "cli": "claude"}}
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", p); r.field("members") == "" {
		t.Fatalf("no manager accepted: %d %v", r.Status, r.Body)
	}
	id, d := e.createWorkspace(owner, org, setupPayload())
	riley := members(d)["Riley"]
	if r := owner.do("DELETE", "/api/v1/workspaces/"+id+"/members/"+str(riley["id"]), map[string]any{"open_tasks": "delete"}); r.Status != http.StatusConflict || r.errCode() != "last_manager" {
		t.Fatalf("removed the last manager: %d %v", r.Status, r.Body)
	}
	// With a second manager, removing Riley moves its staff to them.
	r := owner.do("POST", "/api/v1/workspaces/"+id+"/members", map[string]any{"name": "Morgan", "kind": "manager", "cli": "claude"})
	if r.Status != http.StatusCreated {
		t.Fatalf("add manager: %d %v", r.Status, r.Body)
	}
	morgan := str(r.Body["member"].(map[string]any)["id"])
	if r := owner.do("DELETE", "/api/v1/workspaces/"+id+"/members/"+str(riley["id"]), map[string]any{"open_tasks": "delete", "staff_to": morgan}); r.Status != http.StatusNoContent {
		t.Fatalf("remove manager: %d %v", r.Status, r.Body)
	}
	after := owner.do("GET", "/api/v1/workspaces/"+id, nil).Body
	if str(members(after)["Dev"]["reports_to"]) != morgan {
		t.Errorf("staff not moved to Morgan: %v", members(after)["Dev"])
	}
}

// M2: Staff grants are always a subset of their manager's; narrowing a manager prunes staff.
func TestStaffGrantsStayWithinManager(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Grants Co")
	org := currentOrg(me)

	p := setupPayload()
	staff := p["members"].([]any)[1].(map[string]any)
	staff["grants"] = []any{map[string]any{"directory_key": "d3", "mode": "read", "source": "human"}} // manager can't reach d3
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", p); r.field("members.1.grants") == "" {
		t.Fatalf("setup accepted staff outside manager: %d %v", r.Status, r.Body)
	}
	p = setupPayload()
	p["members"].([]any)[2].(map[string]any)["grants"] = []any{map[string]any{"directory_key": "d2", "mode": "read_write", "source": "human"}} // manager has read
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", p); r.field("members.2.grants") == "" {
		t.Fatalf("setup accepted wider mode than manager: %d %v", r.Status, r.Body)
	}

	id, d := e.createWorkspace(owner, org, setupPayload())
	ms := members(d)
	dirID := map[string]string{}
	for _, dd := range d["directories"].([]any) {
		x := dd.(map[string]any)
		dirID[str(x["path"])] = str(x["id"])
	}
	devID, rileyID := str(ms["Dev"]["id"]), str(ms["Riley"]["id"])

	// Editing staff beyond the manager is refused.
	r := owner.do("PUT", "/api/v1/workspaces/"+id+"/members/"+devID+"/grants", map[string]any{"grants": []any{
		map[string]any{"directory_id": dirID["/home/riley/web"], "mode": "read"}}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("staff beyond manager: %d %v", r.Status, r.Body)
	}
	// Narrow the manager to read-only on the code directory: Dev drops to read, Scribe loses docs.
	r = owner.do("PUT", "/api/v1/workspaces/"+id+"/members/"+rileyID+"/grants", map[string]any{"grants": []any{
		map[string]any{"directory_id": dirID["/home/riley/payments-api"], "mode": "read"}}})
	if r.Status != 200 {
		t.Fatalf("narrow manager: %d %v", r.Status, r.Body)
	}
	after := owner.do("GET", "/api/v1/workspaces/"+id, nil).Body
	if g := grantsFor(after, devID); len(g) != 1 || g["/home/riley/payments-api"] != "read" {
		t.Errorf("Dev not narrowed: %v", g)
	}
	if g := grantsFor(after, str(ms["Scribe"]["id"])); len(g) != 0 {
		t.Errorf("Scribe kept docs the manager lost: %v", g)
	}
	// Independent staff aren't limited by any manager.
	if g := grantsFor(after, str(ms["Ivy"]["id"])); g["/home/riley/web"] != "read" {
		t.Errorf("independent staff changed: %v", g)
	}
	// A new manager with all directories gets every directory, including ones added later.
	nm := owner.do("POST", "/api/v1/workspaces/"+id+"/members", map[string]any{"name": "Max", "kind": "manager", "cli": "claude"})
	maxID := str(nm.Body["member"].(map[string]any)["id"])
	owner.do("POST", "/api/v1/workspaces/"+id+"/directories", map[string]any{"path": "/home/riley/design-system"})
	after = owner.do("GET", "/api/v1/workspaces/"+id, nil).Body
	if g := grantsFor(after, maxID); len(g) != 4 || g["/home/riley/design-system"] != "read_write" {
		t.Errorf("all-directories manager: %v", g)
	}
}

// M2: Invite accept creates only a user for new emails and only a membership for existing accounts.
func TestInviteAcceptNewAndExistingAccounts(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Invite Co")
	org := currentOrg(me)
	id, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	ctx := context.Background()

	newEmail := uniqueEmail("newbie")
	r := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": newEmail, "role": "member",
		"workspace_grants": []any{map[string]any{"workspace_id": id, "role": "staff", "manager_ids": []string{riley}}}})
	if r.Status != http.StatusCreated {
		t.Fatalf("invite: %d %v", r.Status, r.Body)
	}
	token := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	if msg, ok := e.mail.Last(); !ok || msg.To != newEmail || !strings.Contains(msg.Text, token) {
		t.Errorf("invite email: %+v", msg)
	}
	if dup := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": newEmail}); dup.errCode() != "already_invited" {
		t.Errorf("duplicate invite: %d %v", dup.Status, dup.Body)
	}

	guest := e.client()
	info := guest.do("GET", "/api/v1/invites/"+token, nil)
	if info.Status != 200 || info.Body["account_exists"] != false || str(info.Body["organization"]) != "Invite Co" {
		t.Fatalf("invite page: %d %v", info.Status, info.Body)
	}
	ws0 := info.Body["workspaces"].([]any)[0].(map[string]any)
	if str(ws0["role"]) != "staff" || !strings.Contains(str(ws0["sees"]), "Riley") {
		t.Errorf("what they'll see: %v", ws0)
	}
	var orgsBefore, usersBefore int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgsBefore)
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersBefore)
	acc := guest.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "New Person", "password": "new person pw"})
	if acc.Status != 200 || acc.Body["two_factor"] != "enroll" {
		t.Fatalf("accept new: %d %v", acc.Status, acc.Body)
	}
	acc.Body = e.finishSignIn(guest, newEmail, acc.Body)
	if currentOrg(acc.Body) != org {
		t.Fatalf("accept new: current org %v", acc.Body)
	}
	var orgsAfter, usersAfter int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgsAfter)
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersAfter)
	if orgsAfter != orgsBefore || usersAfter != usersBefore+1 {
		t.Errorf("new email: orgs %d→%d users %d→%d (want only a user)", orgsBefore, orgsAfter, usersBefore, usersAfter)
	}
	if len(acc.Body["organizations"].([]any)) != 1 {
		t.Errorf("new user orgs: %v", acc.Body["organizations"])
	}
	// The staff invitee sees Riley's team only.
	view := guest.do("GET", "/api/v1/workspaces/"+id, nil).Body
	if str(view["viewer"].(map[string]any)["role"]) != "staff" {
		t.Errorf("viewer: %v", view["viewer"])
	}
	for _, m := range view["members"].([]any) {
		mm := m.(map[string]any)
		if mm["name"] == "Ivy" {
			t.Error("staff viewer sees independent staff they weren't granted")
		}
	}
	if again := e.client().do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "x", "password": "whatever12"}); again.errCode() != "invalid_invite" {
		t.Errorf("token reused: %d %v", again.Status, again.Body)
	}

	// Existing account: password only, membership added, no new user.
	_, otherMe := e.signup("Other Home")
	existing := userEmail(otherMe)
	r = owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": existing, "role": "admin"})
	token2 := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	c2 := e.client()
	if info := c2.do("GET", "/api/v1/invites/"+token2, nil); info.Body["account_exists"] != true {
		t.Errorf("existing account not detected: %v", info.Body)
	}
	if bad := c2.do("POST", "/api/v1/invites/"+token2+"/accept", map[string]any{"password": "nope nope"}); bad.errCode() != "wrong_password" {
		t.Errorf("wrong password: %d %v", bad.Status, bad.Body)
	}
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersBefore)
	acc = c2.do("POST", "/api/v1/invites/"+token2+"/accept", map[string]any{"password": "correct horse"})
	if acc.Status != 200 || acc.Body["two_factor"] != "verify" {
		t.Fatalf("accept existing: %d %v", acc.Status, acc.Body)
	}
	acc.Body = e.finishSignIn(c2, existing, acc.Body)
	if len(acc.Body["organizations"].([]any)) != 2 {
		t.Fatalf("accept existing: %v", acc.Body)
	}
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersAfter)
	if usersAfter != usersBefore {
		t.Errorf("existing account created a user: %d→%d", usersBefore, usersAfter)
	}

	// Declining notifies the inviter.
	r = owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": uniqueEmail("nah")})
	token3 := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	if dec := e.client().do("POST", "/api/v1/invites/"+token3+"/decline", nil); dec.Status != http.StatusNoContent {
		t.Fatalf("decline: %d", dec.Status)
	}
	if msg, _ := e.mail.Last(); msg.To != userEmail(me) || !strings.Contains(msg.Subject, "declined") {
		t.Errorf("inviter not told: %+v", msg)
	}
	if r := e.client().do("GET", "/api/v1/invites/"+token3, nil); r.errCode() != "invalid_invite" {
		t.Errorf("declined invite still valid: %v", r.Body)
	}
}

func TestStaffCantAdministerAndPersonDirectoryAccess(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Access Co")
	org := currentOrg(me)
	id, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	staffC, staffMe := e.signup("Staff Home")
	staffID := str(staffMe["user"].(map[string]any)["id"])
	if _, err := e.store.Pool.Exec(context.Background(), `INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'member')`, org, staffID); err != nil {
		t.Fatal(err)
	}
	if r := staffC.do("GET", "/api/v1/workspaces/"+id, nil); r.Status != http.StatusNotFound {
		t.Fatalf("member without access sees workspace: %d", r.Status)
	}
	if r := owner.do("PUT", "/api/v1/workspaces/"+id+"/access/"+staffID, map[string]any{"role": "staff", "manager_ids": []string{riley}}); r.Status != http.StatusNoContent {
		t.Fatalf("grant access: %d %v", r.Status, r.Body)
	}
	view := staffC.do("GET", "/api/v1/workspaces/"+id, nil).Body
	hidden := 0
	for _, m := range view["members"].([]any) {
		if m.(map[string]any)["hidden"] == true {
			hidden++
			if _, leaked := m.(map[string]any)["name"]; leaked {
				t.Error("hidden member leaks its name")
			}
		}
	}
	if hidden != 1 {
		t.Errorf("want Ivy hidden, got %d hidden", hidden)
	}
	if r := staffC.do("PATCH", "/api/v1/workspaces/"+id, map[string]any{"name": "Mine"}); r.Status != http.StatusForbidden {
		t.Errorf("staff renamed workspace: %d", r.Status)
	}
	if r := staffC.do("POST", "/api/v1/workspaces/"+id+"/members", map[string]any{"name": "X", "kind": "manager"}); r.Status != http.StatusForbidden {
		t.Errorf("staff added member: %d", r.Status)
	}
	// Person-level directory access: everyone reads by default; the owner can give write.
	dir := view["directories"].([]any)[0].(map[string]any)
	if str(dir["your_mode"]) != "read" {
		t.Errorf("default person access: %v", dir["your_mode"])
	}
	r := staffC.do("PUT", "/api/v1/workspaces/"+id+"/directories/"+str(dir["id"])+"/access/"+staffID, map[string]any{"mode": "read_write"})
	if r.Status != http.StatusForbidden {
		t.Errorf("staff granted themselves write: %d", r.Status)
	}
	if r := owner.do("PUT", "/api/v1/workspaces/"+id+"/directories/"+str(dir["id"])+"/access/"+staffID, map[string]any{"mode": "read_write"}); r.Status != 200 {
		t.Fatalf("owner grants write: %d %v", r.Status, r.Body)
	}
	view = staffC.do("GET", "/api/v1/workspaces/"+id, nil).Body
	if str(view["directories"].([]any)[0].(map[string]any)["your_mode"]) != "read_write" {
		t.Errorf("write not applied: %v", view["directories"])
	}
}
