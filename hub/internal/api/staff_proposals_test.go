package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// draftProposal stores a manager's draft the way the manager's tool will.
func draftProposal(t *testing.T, e *env, ws, manager, kind, member, name, role, instructions string, skills []map[string]string) string {
	t.Helper()
	raw, _ := json.Marshal(skills)
	p := db.CreateStaffProposalParams{ID: uuid.New(), WorkspaceID: uuid.MustParse(ws), ManagerID: uuid.MustParse(manager),
		Kind: kind, Name: name, Instructions: instructions, Skills: raw}
	if role != "" {
		p.Role = &role
	}
	if member != "" {
		id := uuid.MustParse(member)
		p.MemberID = &id
	}
	got, err := e.srv.Store.CreateStaffProposal(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return got.ID.String()
}

func TestApproveNewStaffProposal(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Proposals Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	mgr := str(members(d)["Riley"]["id"])
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"
	owner.do("POST", "/api/v1/workspaces/"+ws+"/skills", map[string]any{"name": "Style", "source_kind": "md", "content": "Be brief."})
	id := draftProposal(t, e, ws, mgr, "new", "", "Tess", "reviewer", "Review pull requests.", []map[string]string{
		{"name": "Style", "content": "Different text."},
		{"name": "Review", "content": "Check errors.", "source_url": "https://github.com/acme/skills/blob/main/review.md"},
	})

	r := owner.do("GET", base+"?status=pending", nil)
	if list := r.Body["proposals"].([]any); r.Status != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", r.Status, r.Body)
	}

	// The admin edits the name before approving.
	r = owner.do("POST", base+"/"+id+"/approve", map[string]any{"name": "Tessa"})
	if r.Status != 200 {
		t.Fatalf("approve: %d %v", r.Status, r.Body)
	}
	m := r.Body["member"].(map[string]any)
	if m["name"] != "Tessa" || m["kind"] != "staff" || m["reports_to"] != mgr || m["skill"] != "reviewer" || m["instructions"] != "Review pull requests." {
		t.Errorf("member: %v", m)
	}
	if p := r.Body["proposal"].(map[string]any); p["status"] != "approved" || p["result_member_id"] != m["id"] {
		t.Errorf("proposal: %v", p)
	}
	got := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+str(m["id"])+"/skills", nil).Body["skills"].([]any)
	if len(got) != 2 {
		t.Fatalf("member skills: %v", got)
	}
	lib := owner.do("GET", "/api/v1/workspaces/"+ws+"/skills", nil).Body["skills"].([]any)
	if len(lib) != 2 {
		t.Errorf("library should gain only the new skill: %v", lib)
	}
	for _, s := range lib {
		if sk := s.(map[string]any); sk["name"] == "Style" && sk["content"] != "Be brief." {
			t.Errorf("existing skill overwritten: %v", sk)
		}
	}
	if r := owner.do("POST", base+"/"+id+"/approve", map[string]any{}); r.Status != http.StatusConflict {
		t.Errorf("approve twice: %d", r.Status)
	}
}

func TestApproveInstructionUpdateProposal(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Updates Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	mgr, dev := str(members(d)["Riley"]["id"]), str(members(d)["Dev"]["id"])
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"
	id := draftProposal(t, e, ws, mgr, "update", dev, "", "", "Write tests first.", []map[string]string{{"name": "TDD", "content": "Red, green, refactor."}})

	r := owner.do("POST", base+"/"+id+"/approve", map[string]any{"instructions": "Write tests first, then code."})
	if r.Status != 200 {
		t.Fatalf("approve: %d %v", r.Status, r.Body)
	}
	if m := r.Body["member"].(map[string]any); m["id"] != dev || m["name"] != "Dev" || m["instructions"] != "Write tests first, then code." {
		t.Errorf("member: %v", m)
	}
	if got := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+dev+"/skills", nil).Body["skills"].([]any); len(got) != 1 {
		t.Errorf("member skills: %v", got)
	}
}

func TestRejectStaffProposal(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Reject Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	mgr := str(members(d)["Riley"]["id"])
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"
	id := draftProposal(t, e, ws, mgr, "new", "", "Tess", "reviewer", "x", nil)

	r := owner.do("POST", base+"/"+id+"/reject", map[string]any{"reason": "We have enough reviewers."})
	if p, _ := r.Body["proposal"].(map[string]any); r.Status != 200 || p["status"] != "rejected" || p["reason"] != "We have enough reviewers." {
		t.Fatalf("reject: %d %v", r.Status, r.Body)
	}
	if n := len(members(owner.do("GET", "/api/v1/workspaces/"+ws+"/board", nil).Body)); n != 4 {
		t.Errorf("rejecting created a member: %d members", n)
	}
	if r := owner.do("POST", base+"/"+id+"/approve", map[string]any{}); r.Status != http.StatusConflict {
		t.Errorf("approve after reject: %d", r.Status)
	}
}

func TestStaffProposalPermissions(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Proposal Perms Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	mgr := str(members(d)["Riley"]["id"])
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff"})
	stranger, _ := e.signup("Outsiders")
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"
	id := draftProposal(t, e, ws, mgr, "new", "", "Tess", "reviewer", "x", nil)

	for name, tc := range map[string]struct {
		c      *client
		method string
		path   string
		want   int
	}{
		"member lists":      {bob, "GET", base, http.StatusForbidden},
		"member approves":   {bob, "POST", base + "/" + id + "/approve", http.StatusForbidden},
		"member rejects":    {bob, "POST", base + "/" + id + "/reject", http.StatusForbidden},
		"stranger lists":    {stranger, "GET", base, http.StatusNotFound},
		"stranger approves": {stranger, "POST", base + "/" + id + "/approve", http.StatusNotFound},
	} {
		if r := tc.c.do(tc.method, tc.path, map[string]any{}); r.Status != tc.want {
			t.Errorf("%s: %d, want %d", name, r.Status, tc.want)
		}
	}
	// A draft from another workspace can't be decided through this one.
	other, _ := e.createWorkspace(owner, org, setupPayload())
	if r := owner.do("POST", "/api/v1/workspaces/"+other+"/staff-proposals/"+id+"/approve", map[string]any{}); r.Status != http.StatusNotFound {
		t.Errorf("cross-workspace approve: %d", r.Status)
	}
	if r := owner.do("GET", base+"?status=pending", nil); len(r.Body["proposals"].([]any)) != 1 {
		t.Errorf("denied calls changed the draft: %v", r.Body)
	}
}
