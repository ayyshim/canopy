package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// recorder is a connected runner that remembers what the hub sent it.
type recorder struct {
	mu   sync.Mutex
	sent []protocol.Envelope
}

func (r *recorder) Online(uuid.UUID) bool { return true }
func (r *recorder) Send(_ uuid.UUID, typ string, data any) error {
	b, _ := json.Marshal(data)
	r.mu.Lock()
	r.sent = append(r.sent, protocol.Envelope{Type: typ, Data: b})
	r.mu.Unlock()
	return nil
}
func (r *recorder) Request(context.Context, uuid.UUID, string, any, any) error { return nil }

// wait returns the first sent message whose payload contains text.
func (r *recorder) wait(t *testing.T, typ, text string) protocol.Envelope {
	t.Helper()
	for i := 0; i < 100; i++ {
		r.mu.Lock()
		for _, e := range r.sent {
			if e.Type == typ && strings.Contains(string(e.Data), text) {
				r.mu.Unlock()
				return e
			}
		}
		r.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("no %s containing %q; sent: %v", typ, text, r.sent)
	return protocol.Envelope{}
}

// call makes one tool call as a member's session and returns the runner's reply.
func call(t *testing.T, o *orch.Orch, rn db.Runner, rec *recorder, sess uuid.UUID, tool string, args any) protocol.ToolResult {
	t.Helper()
	id := uuid.NewString()
	raw, _ := json.Marshal(args)
	data, _ := json.Marshal(protocol.ToolCall{SessionID: sess, CallID: id, Tool: tool, Args: raw})
	o.RunnerMessage(context.Background(), rn, protocol.Envelope{Type: protocol.TToolCall, Data: data})
	e := rec.wait(t, protocol.TToolResult, id)
	var res protocol.ToolResult
	_ = json.Unmarshal(e.Data, &res)
	return res
}

func TestStaffProposalTools(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Staff Tools Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	m := members(d)
	riley, dev := uuid.MustParse(str(m["Riley"]["id"])), uuid.MustParse(str(m["Dev"]["id"]))
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	ctx := context.Background()

	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	rn, err := e.store.GetRunner(ctx, runnerID)
	if err != nil {
		t.Fatal(err)
	}
	newSession := func(member uuid.UUID) uuid.UUID {
		s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: member,
			Status: "waiting", RunnerID: &runnerID, PersonID: &uid})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	mgrSess, staffSess := newSession(riley), newSession(dev)
	rec := &recorder{}
	o := orch.New(e.srv.Core, rec, nil)
	e.srv.Orch = o
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"

	// Staff are refused and nothing is drafted; the manager prompt offers the tools, the staff prompt doesn't.
	args := map[string]any{"name": "Tess", "role": "reviewer", "instructions": "Review pull requests."}
	if res := call(t, o, rn, rec, staffSess, protocol.ToolProposeStaff, args); !res.IsError || !strings.Contains(res.Text, "Only managers") {
		t.Errorf("staff propose_staff: %+v", res)
	}
	if res := call(t, o, rn, rec, staffSess, protocol.ToolUpdateStaff, map[string]any{"member": "Dev", "instructions": "x"}); !res.IsError {
		t.Errorf("staff update_staff_instructions: %+v", res)
	}
	if list := owner.do("GET", base, nil).Body["proposals"].([]any); len(list) != 0 {
		t.Fatalf("staff created drafts: %v", list)
	}
	staffPrompt, _ := o.MemberPrompt(ctx, dev)
	mgrPrompt, _ := o.MemberPrompt(ctx, riley)
	if strings.Contains(staffPrompt, "propose_staff") || strings.Contains(staffPrompt, "update_staff_instructions") {
		t.Errorf("staff prompt lists manager tools:\n%s", staffPrompt)
	}
	if !strings.Contains(mgrPrompt, "propose_staff") || !strings.Contains(mgrPrompt, "WebSearch") {
		t.Errorf("manager prompt:\n%s", mgrPrompt)
	}

	// Bad drafts read as messages for the manager.
	for name, a := range map[string]map[string]any{
		"role":    {"name": "Tess", "role": "wizard", "instructions": "x"},
		"name":    {"name": "dev", "role": "reviewer", "instructions": "x"},
		"skill":   {"name": "Tess", "role": "reviewer", "instructions": "x", "skills": []map[string]string{{"name": "Empty", "content": "  "}}},
		"nothing": {"name": "Tess", "role": "reviewer", "instructions": " "},
	} {
		if res := call(t, o, rn, rec, mgrSess, protocol.ToolProposeStaff, a); !res.IsError {
			t.Errorf("%s: accepted %v", name, a)
		}
	}

	// A good draft is stored as pending, with the manager's session, and the manager is told it's waiting.
	args["reason"] = "Nobody checks our pull requests."
	args["skills"] = []map[string]string{{"name": "Review", "content": "Check errors.", "source_url": "https://github.com/acme/skills/blob/main/review.md"}}
	res := call(t, o, rn, rec, mgrSess, protocol.ToolProposeStaff, args)
	if res.IsError || !strings.Contains(res.Text, "Sent for approval") {
		t.Fatalf("propose_staff: %+v", res)
	}
	list := owner.do("GET", base+"?status=pending", nil).Body["proposals"].([]any)
	if len(list) != 1 {
		t.Fatalf("drafts: %v", list)
	}
	p := list[0].(map[string]any)
	if p["kind"] != "new" || p["name"] != "Tess" || p["manager_id"] != riley.String() || p["note"] != "Nobody checks our pull requests." || len(p["skills"].([]any)) != 1 {
		t.Errorf("draft: %v", p)
	}
	row, err := e.store.GetStaffProposalForUpdate(ctx, uuid.MustParse(str(p["id"])))
	if err != nil || row.SessionID == nil || *row.SessionID != mgrSess {
		t.Errorf("session not kept: %v %+v", err, row)
	}

	// An update can only target the manager's own staff.
	if res := call(t, o, rn, rec, mgrSess, protocol.ToolUpdateStaff, map[string]any{"member": "Ivy", "instructions": "x"}); !res.IsError {
		t.Errorf("update for someone else's staff: %+v", res)
	}
	res = call(t, o, rn, rec, mgrSess, protocol.ToolUpdateStaff, map[string]any{"member": "dev", "instructions": "Write tests first.", "reason": "Too many bugs."})
	if res.IsError || !strings.Contains(res.Text, "Sent for approval") {
		t.Fatalf("update_staff_instructions: %+v", res)
	}

	// Approval and rejection reach the manager's session.
	if r := owner.do("POST", base+"/"+str(p["id"])+"/approve", map[string]any{}); r.Status != 200 {
		t.Fatalf("approve: %d %v", r.Status, r.Body)
	}
	rec.wait(t, protocol.TSendInput, "approved your draft for Tess")
	var upd map[string]any
	for _, x := range owner.do("GET", base+"?status=pending", nil).Body["proposals"].([]any) {
		upd = x.(map[string]any)
	}
	if upd["kind"] != "update" || upd["member_id"] != dev.String() {
		t.Fatalf("update draft: %v", upd)
	}
	if r := owner.do("POST", base+"/"+str(upd["id"])+"/reject", map[string]any{"reason": "Keep it as it is."}); r.Status != 200 {
		t.Fatalf("reject: %d %v", r.Status, r.Body)
	}
	rec.wait(t, protocol.TSendInput, "rejected your new instructions for Dev")
	rec.wait(t, protocol.TSendInput, "Keep it as it is.")
}

// A manager drafts a new staff member and an instruction change; the admin approves both.
func TestStaffProposalFlow(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Staff Flow Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley, dev := uuid.MustParse(str(members(d)["Riley"]["id"])), str(members(d)["Dev"]["id"])
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	ctx := context.Background()
	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	rn, err := e.store.GetRunner(ctx, runnerID)
	if err != nil {
		t.Fatal(err)
	}
	s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: riley,
		Status: "waiting", RunnerID: &runnerID, PersonID: &uid})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	o := orch.New(e.srv.Core, rec, nil)
	e.srv.Orch = o
	base := "/api/v1/workspaces/" + ws + "/staff-proposals"
	pending := func() map[string]any {
		list := owner.do("GET", base+"?status=pending", nil).Body["proposals"].([]any)
		if len(list) != 1 {
			t.Fatalf("pending drafts: %v", list)
		}
		return list[0].(map[string]any)
	}

	call(t, o, rn, rec, s.ID, protocol.ToolProposeStaff, map[string]any{"name": "Tess", "role": "reviewer", "instructions": "Review pull requests.",
		"skills": []map[string]string{{"name": "Review", "content": "Check errors.", "source_url": "https://example.com/review.md"}}})
	r := owner.do("POST", base+"/"+str(pending()["id"])+"/approve", map[string]any{})
	if r.Status != 200 {
		t.Fatalf("approve new: %d %v", r.Status, r.Body)
	}
	tess := r.Body["member"].(map[string]any)
	if tess["name"] != "Tess" || tess["reports_to"] != riley.String() {
		t.Errorf("member: %v", tess)
	}
	if got := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+str(tess["id"])+"/skills", nil).Body["skills"].([]any); len(got) != 1 {
		t.Errorf("Tess's skills: %v", got)
	}

	call(t, o, rn, rec, s.ID, protocol.ToolUpdateStaff, map[string]any{"member": "Dev", "instructions": "Write tests first."})
	if r := owner.do("POST", base+"/"+str(pending()["id"])+"/approve", map[string]any{}); r.Status != 200 {
		t.Fatalf("approve update: %d %v", r.Status, r.Body)
	}
	got, err := e.store.GetMember(ctx, uuid.MustParse(dev))
	if err != nil || got.Instructions != "Write tests first." {
		t.Errorf("Dev: %v %q", err, got.Instructions)
	}
	rec.wait(t, protocol.TSendInput, "approved your new instructions for Dev")
}
