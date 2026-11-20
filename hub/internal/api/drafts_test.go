package api_test

import (
	"net/http"
	"testing"

	"canopy/hub/internal/orch"
)

// A draft sits on the board without work starting; its person can edit, give or delete it.
func TestDraftGoals(t *testing.T) {
	e := newEnv(t)
	e.srv.Orch = orch.New(e.srv.Core, offline{}, nil)
	owner, me := e.signup("Drafts Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff", "manager_ids": []string{riley}})

	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "description": "First pass", "draft": true})
	if r.Status != http.StatusCreated {
		t.Fatalf("save draft: %d %v", r.Status, r.Body)
	}
	g := r.Body["goal"].(map[string]any)
	goal := str(g["id"])
	if str(g["status"]) != "draft" {
		t.Errorf("status: %v", g["status"])
	}
	if r := owner.do("POST", "/api/v1/goals/"+goal+"/done", nil); r.Status != http.StatusConflict {
		t.Errorf("done on draft: %d", r.Status)
	}

	if r := bob.do("PUT", "/api/v1/goals/"+goal+"/draft", map[string]any{"manager_id": riley, "title": "Bob’s"}); r.Status != http.StatusForbidden {
		t.Errorf("Bob edited the draft: %d", r.Status)
	}
	if r := owner.do("PUT", "/api/v1/goals/"+goal+"/draft", map[string]any{"manager_id": riley, "title": ""}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("empty title: %d", r.Status)
	}
	r = owner.do("PUT", "/api/v1/goals/"+goal+"/draft", map[string]any{"manager_id": riley, "title": "PDF export", "description": "Second pass"})
	if r.Status != http.StatusOK {
		t.Fatalf("edit draft: %d %v", r.Status, r.Body)
	}
	g = r.Body["goal"].(map[string]any)
	if str(g["title"]) != "PDF export" || str(g["description"]) != "Second pass" || str(g["branch"]) != "canopy/pdf-export" {
		t.Errorf("edited: %v", g)
	}

	r = owner.do("POST", "/api/v1/goals/"+goal+"/give", nil)
	if r.Status != http.StatusOK || str(r.Body["goal"].(map[string]any)["status"]) != "planning" {
		t.Fatalf("give: %d %v", r.Status, r.Body)
	}
	if r := owner.do("POST", "/api/v1/goals/"+goal+"/give", nil); r.Status != http.StatusConflict {
		t.Errorf("gave twice: %d", r.Status)
	}
	if r := owner.do("DELETE", "/api/v1/goals/"+goal, nil); r.Status != http.StatusConflict {
		t.Errorf("deleted a given goal: %d", r.Status)
	}

	r = owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "Throwaway", "draft": true})
	other := str(r.Body["goal"].(map[string]any)["id"])
	if r := owner.do("DELETE", "/api/v1/goals/"+other, nil); r.Status != http.StatusNoContent {
		t.Errorf("delete draft: %d", r.Status)
	}
	if r := owner.do("GET", "/api/v1/goals/"+other, nil); r.Status != http.StatusNotFound {
		t.Errorf("draft still there: %d", r.Status)
	}
}
