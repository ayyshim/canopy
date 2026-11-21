package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Usage reported with a task lands on that task and its goal, with cost from the price table.
func TestUsagePerTaskAndGoal(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Usage Cost Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := uuid.MustParse(str(members(d)["Riley"]["id"]))
	wsID, uid := uuid.MustParse(ws), uuid.MustParse(userID(me))
	ctx := context.Background()
	runnerID := uuid.MustParse(e.addRunner(org, userID(me), "Laptop", "computer"))
	rn, err := e.store.GetRunner(ctx, runnerID)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: wsID, MemberID: riley,
		Status: "waiting", RunnerID: &runnerID, PersonID: &uid})
	if err != nil {
		t.Fatal(err)
	}
	o := orch.New(e.srv.Core, &recorder{}, nil)
	e.srv.Orch = o

	r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley.String(), "title": "Price me", "draft": true})
	if r.Status != 201 {
		t.Fatalf("goal: %d %v", r.Status, r.Body)
	}
	goalID := str(r.Body["goal"].(map[string]any)["id"])
	tk := owner.do("POST", "/api/v1/goals/"+goalID+"/tasks", map[string]any{"title": "Do it", "member_id": members(d)["Dev"]["id"]})
	if tk.Status != 201 {
		t.Fatalf("task: %d %v", tk.Status, tk.Body)
	}
	taskID := str(tk.Body["task"].(map[string]any)["id"])
	b := map[string]any{}
	tid := uuid.MustParse(taskID)

	send := func(u protocol.Usage) {
		u.SessionID = sess.ID
		data, _ := json.Marshal(u)
		o.RunnerMessage(ctx, rn, protocol.Envelope{Type: protocol.TUsage, Data: data})
	}
	// 1M fresh input at $2, 100k output at $10, 1M cache read at $0.20 = $3.20 on Sonnet 5.5.
	send(protocol.Usage{TaskID: &tid, Model: "claude-sonnet-5-5", TokensIn: 1_000_000, TokensOut: 100_000, CacheRead: 1_000_000})
	// A second call on another model adds to the same task.
	send(protocol.Usage{TaskID: &tid, Model: "claude-haiku-5-5", TokensIn: 1_000_000})
	// A chat call with no task counts for nobody.
	send(protocol.Usage{Model: "claude-sonnet-5-5", TokensIn: 5_000_000})

	check := func(name string, u map[string]any) {
		t.Helper()
		if u["tokens_in"].(float64) != 2_000_000 || u["tokens_out"].(float64) != 100_000 || u["cache_read"].(float64) != 1_000_000 {
			t.Errorf("%s tokens: %v", name, u)
		}
		// $3.20 + $0.10 for 1M haiku input.
		if u["cost_micros"].(float64) != 3_300_000 {
			t.Errorf("%s cost: %v", name, u["cost_micros"])
		}
		if len(u["by_model"].([]any)) != 2 {
			t.Errorf("%s by model: %v", name, u["by_model"])
		}
	}
	b = owner.do("GET", "/api/v1/workspaces/"+ws+"/board", nil).Body
	for _, x := range b["goals"].([]any) {
		if g := x.(map[string]any); str(g["id"]) == goalID {
			check("board goal", g["usage"].(map[string]any))
		}
	}
	for _, x := range b["tasks"].([]any) {
		if tk := x.(map[string]any); str(tk["id"]) == taskID {
			check("board task", tk["usage"].(map[string]any))
		}
	}
	gd := owner.do("GET", "/api/v1/goals/"+goalID, nil)
	if gd.Status != 200 {
		t.Fatalf("goal detail: %d %v", gd.Status, gd.Body)
	}
	check("goal detail", gd.Body["goal"].(map[string]any)["usage"].(map[string]any))
	td := owner.do("GET", "/api/v1/tasks/"+taskID, nil)
	if td.Status != 200 {
		t.Fatalf("task detail: %d %v", td.Status, td.Body)
	}
	check("task detail", td.Body["task"].(map[string]any)["usage"].(map[string]any))

	// The event row keeps the same cost and the task, goal and cache numbers.
	var cost, cacheRead int64
	var gotTask, gotGoal uuid.UUID
	if err := e.store.Pool.QueryRow(ctx, `SELECT cost_micros, cache_read, task_id, goal_id FROM usage_events WHERE model = 'claude-sonnet-5-5' AND task_id IS NOT NULL`).
		Scan(&cost, &cacheRead, &gotTask, &gotGoal); err != nil {
		t.Fatal(err)
	}
	if cost != 3_200_000 || cacheRead != 1_000_000 || gotTask != tid || gotGoal.String() != goalID {
		t.Errorf("event: cost %d cache %d task %s goal %s", cost, cacheRead, gotTask, gotGoal)
	}
	var untasked int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM usage_events WHERE task_id IS NULL AND session_id = $1`, sess.ID).Scan(&untasked)
	if untasked != 1 {
		t.Errorf("events without a task: %d", untasked)
	}
}
