package api_test

import (
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
