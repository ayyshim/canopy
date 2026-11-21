package api_test

import (
	"net/http"
	"testing"
)

// A person's task for a manager on a goal is a triage request; for staff, or off a goal, it stays work.
func TestTriageTasks(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Triage Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	dev := str(members(d)["Dev"]["id"])
	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "draft": true})
	goal := str(g.Body["goal"].(map[string]any)["id"])

	kind := func(path, member string) string {
		r := owner.do("POST", path, map[string]any{"title": "Change it", "member_id": member})
		if r.Status != http.StatusCreated {
			t.Fatalf("create task: %d %v", r.Status, r.Body)
		}
		return str(r.Body["task"].(map[string]any)["kind"])
	}
	if k := kind("/api/v1/goals/"+goal+"/tasks", riley); k != "triage" {
		t.Errorf("manager task on goal: %s", k)
	}
	if k := kind("/api/v1/goals/"+goal+"/tasks", dev); k != "work" {
		t.Errorf("staff task on goal: %s", k)
	}
	if k := kind("/api/v1/workspaces/"+ws+"/tasks", riley); k != "work" {
		t.Errorf("manager task off goal: %s", k)
	}
}
