package api_test

import (
	"context"
	"net/http"
	"testing"
)

// Isolated staff are independent staff; the creator is recorded.
func TestIsolatedStaff(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Isolated Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	base := "/api/v1/workspaces/" + ws + "/members"
	riley := str(members(d)["Riley"]["id"])

	r := owner.do("POST", base, map[string]any{"name": "Sol", "kind": "staff", "skill": "developer", "isolated": true})
	if r.Status != http.StatusCreated {
		t.Fatalf("create isolated: %d %v", r.Status, r.Body)
	}
	m := r.Body["member"].(map[string]any)
	if m["isolated"] != true || str(m["created_by"]) == "" {
		t.Errorf("isolated or creator missing: %v", m)
	}
	id := str(m["id"])

	if r := owner.do("POST", base, map[string]any{"name": "Mgr", "kind": "manager", "isolated": true}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("isolated manager: %d", r.Status)
	}
	if r := owner.do("POST", base, map[string]any{"name": "Dee", "kind": "staff", "skill": "developer", "reports_to": riley, "isolated": true}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("isolated staff with manager: %d", r.Status)
	}
	if r := owner.do("PATCH", base+"/"+id, map[string]any{"set_reports_to": true, "reports_to": riley}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("gave isolated staff a manager: %d", r.Status)
	}
	if r := owner.do("PATCH", base+"/"+riley, map[string]any{"isolated": true}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("isolated manager by update: %d", r.Status)
	}
	r = owner.do("PATCH", base+"/"+id, map[string]any{"isolated": false})
	if r.Status != http.StatusOK || r.Body["member"].(map[string]any)["isolated"] != false {
		t.Errorf("clear isolated: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PATCH", base+"/"+id, map[string]any{"set_reports_to": true, "reports_to": riley}); r.Status != http.StatusOK {
		t.Errorf("manager for non-isolated: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PATCH", base+"/"+id, map[string]any{"isolated": true}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("isolate staff with manager: %d", r.Status)
	}
}

// Only the creator can chat with isolated staff; other admins get 403.
func TestIsolatedStaffChat(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Isolated Chat Co")
	org := currentOrg(me)
	ws, _ := e.createWorkspace(owner, org, setupPayload())
	other, otherMe := e.signup("Other Home")
	otherID := str(otherMe["user"].(map[string]any)["id"])
	if _, err := e.store.Pool.Exec(context.Background(), `INSERT INTO memberships (id, org_id, user_id, org_role) VALUES (gen_random_uuid(), $1, $2, 'admin')`, org, otherID); err != nil {
		t.Fatal(err)
	}
	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/members", map[string]any{"name": "Sol", "kind": "staff", "skill": "developer", "isolated": true})
	if r.Status != http.StatusCreated {
		t.Fatalf("create: %d %v", r.Status, r.Body)
	}
	chat := "/api/v1/workspaces/" + ws + "/members/" + str(r.Body["member"].(map[string]any)["id"]) + "/chat"

	if r := owner.do("POST", chat, map[string]any{"text": "hi"}); r.Status != http.StatusCreated {
		t.Errorf("creator send: %d %v", r.Status, r.Body)
	}
	if r := owner.do("GET", chat, nil); r.Status != http.StatusOK {
		t.Errorf("creator read: %d", r.Status)
	}
	if r := other.do("GET", chat, nil); r.Status != http.StatusForbidden {
		t.Errorf("other read: %d", r.Status)
	}
	if r := other.do("POST", chat, map[string]any{"text": "hi"}); r.Status != http.StatusForbidden {
		t.Errorf("other send: %d", r.Status)
	}
	if r := other.do("POST", chat+"/read", nil); r.Status != http.StatusForbidden {
		t.Errorf("other mark read: %d", r.Status)
	}
}

// Staff made before creators were recorded belong to whoever isolates them.
func TestIsolateOldStaffClaimsCreator(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Old Staff Co")
	ws, _ := e.createWorkspace(owner, currentOrg(me), setupPayload())
	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/members", map[string]any{"name": "Updater", "kind": "staff", "skill": "developer"})
	if r.Status != http.StatusCreated {
		t.Fatalf("create: %d %v", r.Status, r.Body)
	}
	id := str(r.Body["member"].(map[string]any)["id"])
	if _, err := e.store.Pool.Exec(context.Background(), `UPDATE members SET created_by = NULL WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	r = owner.do("PATCH", "/api/v1/workspaces/"+ws+"/members/"+id, map[string]any{"isolated": true})
	if r.Status != http.StatusOK || str(r.Body["member"].(map[string]any)["created_by"]) != str(me["user"].(map[string]any)["id"]) {
		t.Fatalf("isolate old staff: %d %v", r.Status, r.Body)
	}
	if r := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+id+"/chat", nil); r.Status != http.StatusOK {
		t.Errorf("chat after isolating: %d", r.Status)
	}
}
