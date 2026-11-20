package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Managers can read the live board; staff can't.
func TestReadBoardTool(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Board Tool Co")
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
	sess := func(member uuid.UUID) uuid.UUID {
		s, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: member,
			Status: "waiting", RunnerID: &runnerID, PersonID: &uid})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	mgrSess, staffSess := sess(riley), sess(dev)
	rec := &recorder{}
	o := orch.New(e.srv.Core, rec, nil)
	e.srv.Orch = o

	if r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley.String(), "title": "Ship the export"}); r.Status != 201 {
		t.Fatalf("goal: %d %v", r.Status, r.Body)
	}

	if res := call(t, o, rn, rec, staffSess, protocol.ToolReadBoard, map[string]any{}); !res.IsError {
		t.Errorf("staff read_board: %+v", res)
	}
	res := call(t, o, rn, rec, mgrSess, protocol.ToolReadBoard, map[string]any{})
	if res.IsError {
		t.Fatalf("read_board: %+v", res)
	}
	for _, want := range []string{"Team:", "Riley (manager)", "Dev (developer, reports to Riley)", "## Ship the export", "Waiting on people:"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("board misses %q:\n%s", want, res.Text)
		}
	}
	mgrPrompt, _ := o.MemberPrompt(ctx, riley)
	staffPrompt, _ := o.MemberPrompt(ctx, dev)
	if !strings.Contains(mgrPrompt, "read_board") || strings.Contains(staffPrompt, "read_board") {
		t.Errorf("read_board should be offered to managers only")
	}
}
