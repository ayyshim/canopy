package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// workspacePayload sets up a workspace whose goals finish on their own when every task is done.
func workspacePayload(runnerID, codeDir string) map[string]any {
	return map[string]any{
		"workspace":   map[string]any{"name": "Payments", "goal_completion": "auto"},
		"runner_id":   runnerID,
		"directories": []any{map[string]any{"key": "code", "path": codeDir}},
		"clis":        []any{map[string]any{"cli": "claude", "enabled": true, "mode": "subscription"}},
		"members": []any{
			map[string]any{"key": "m", "name": "Riley", "kind": "manager", "cli": "claude", "all_directories": true},
			map[string]any{"key": "d", "name": "Dev", "kind": "staff", "skill": "developer", "reports_to_key": "m", "cli": "claude",
				"grants": []any{map[string]any{"directory_key": "code", "mode": "read_write", "source": "suggested"}}},
			map[string]any{"key": "w", "name": "Wren", "kind": "staff", "skill": "writer", "reports_to_key": "m", "cli": "claude"},
		},
	}
}

// M3: a runner pairs by one-time code; a goal yields a validated plan, tasks with
// dependencies and assignment; outputs land; the plan and summary are workspace notes; usage
// is attributed.
func TestGoalRunsEndToEnd(t *testing.T) {
	h := startHub(t)
	owner, org := h.setupOrg("Acme", "Riley Owner", "owner@acme.test")
	mc := h.pair(owner, org, "computer", "Riley’s laptop", sandboxKind())
	mc.start()
	h.waitOnline(owner, org, mc.id, true)

	work := t.TempDir()
	code := filepath.Join(work, "payments-api")
	writeFile(t, filepath.Join(code, "main.go"), "package main\n")
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(mc.id, code), 201)["workspace"])["id"])
	b := owner.board(ws)
	riley := memberID(b, "Riley")

	goal := owner.goal(ws, riley, "Ship the export [tasks:2] [badplan] [output]")
	b = owner.waitGoalDone(ws, goal, 60*time.Second)

	var plan, work1, work2 map[string]any
	for _, x := range list(b["tasks"]) {
		task := m(x)
		switch {
		case str(task["kind"]) == "plan":
			plan = task
		case strings.HasPrefix(str(task["title"]), "Part 1"):
			work1 = task
		case strings.HasPrefix(str(task["title"]), "Part 2"):
			work2 = task
		}
	}
	if plan == nil || work1 == nil || work2 == nil {
		t.Fatalf("tasks: %v", b["tasks"])
	}
	// Assignment prefers own staff with the skill: Part 1 (developer) → Dev, Part 2 (writer) → Wren.
	if str(work1["member_id"]) != memberID(b, "Dev") || str(work2["member_id"]) != memberID(b, "Wren") {
		t.Errorf("assignment: part1→%v part2→%v", work1["member_id"], work2["member_id"])
	}
	if deps := list(work2["depends_on"]); len(deps) != 1 || str(deps[0]) != str(work1["id"]) {
		t.Errorf("dependencies: %v", work2["depends_on"])
	}
	for _, task := range []map[string]any{plan, work1, work2} {
		if str(task["status"]) != "done" || task["progress"].(float64) != 100 {
			t.Errorf("task not done: %v", task)
		}
	}
	// Outputs: the plan (from the manager) and each work task's report.
	outs := list(b["outputs"])
	if len(outs) < 3 {
		t.Errorf("outputs: %v", outs)
	}
	kinds := map[string]bool{}
	for _, n := range list(b["notes"]) {
		kinds[str(m(n)["kind"])] = true
	}
	if !kinds["plan"] || !kinds["summary"] {
		t.Errorf("plan and summary notes: %v", b["notes"])
	}
	// Every token is attributed to the goal giver.
	var total, attributed int64
	ownerID := str(m(owner.me["user"])["id"])
	if err := h.db.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE attributed_user_id::text = $1) FROM usage_events`, ownerID).Scan(&total, &attributed); err != nil {
		t.Fatal(err)
	}
	if total == 0 || attributed != total {
		t.Errorf("usage attribution: %d of %d events attributed to the goal giver", attributed, total)
	}
	// M6: rollups match raw events (within 0.1%; here exactly, since they're written together).
	var raw, rolled int64
	_ = h.db.QueryRow(context.Background(), `SELECT coalesce(sum(tokens_in + tokens_out), 0) FROM usage_events`).Scan(&raw)
	_ = h.db.QueryRow(context.Background(), `SELECT coalesce(sum(tokens_in + tokens_out), 0) FROM usage_hourly`).Scan(&rolled)
	if raw == 0 || float64(abs(raw-rolled)) > float64(raw)*0.001 {
		t.Errorf("rollups %d vs raw %d", rolled, raw)
	}
}

// M3: questions, gates, reviews raise and resolve end to end.
func TestSignalsRaiseAndResolve(t *testing.T) {
	h := startHub(t)
	owner, org := h.setupOrg("Signals Co", "Sam Owner", "sam@signals.test")
	mc := h.pair(owner, org, "computer", "Sam’s laptop", "none")
	mc.start()
	h.waitOnline(owner, org, mc.id, true)
	work := t.TempDir()
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(mc.id, filepath.Join(work, "api")), 201)["workspace"])["id"])
	riley := memberID(owner.board(ws), "Riley")
	goal := owner.goal(ws, riley, "Decide things [tasks:1] [ask] [gate] [review]")

	q := owner.waitSignal(ws, "question", 30*time.Second)
	if str(m(q["payload"])["question"]) == "" || len(list(m(q["payload"])["options"])) != 2 {
		t.Errorf("question payload: %v", q["payload"])
	}
	owner.do("POST", "/api/v1/signals/"+str(q["id"])+"/resolve", map[string]any{"action": "answer", "text": "Option B"}, 200)
	// Answering twice is refused: resolving anywhere resolves everywhere.
	owner.do("POST", "/api/v1/signals/"+str(q["id"])+"/resolve", map[string]any{"action": "answer", "text": "Option A"}, 409)

	g := owner.waitSignal(ws, "gate", 30*time.Second)
	if !strings.Contains(str(m(g["payload"])["summary"]), "rm -rf build") || g["expires_at"] == nil {
		t.Errorf("gate: %v", g)
	}
	owner.do("POST", "/api/v1/signals/"+str(g["id"])+"/resolve", map[string]any{"action": "always"}, 200)

	r := owner.waitSignal(ws, "review", 30*time.Second)
	owner.do("POST", "/api/v1/signals/"+str(r["id"])+"/resolve", map[string]any{"action": "changes"}, 422) // note required
	owner.do("POST", "/api/v1/signals/"+str(r["id"])+"/resolve", map[string]any{"action": "changes", "note": "Add numbers"}, 200)
	waitFor(t, 30*time.Second, "second review", func() bool {
		for _, s := range list(owner.board(ws)["signals"]) {
			if str(m(s)["type"]) == "review" && str(m(s)["id"]) != str(r["id"]) {
				owner.do("POST", "/api/v1/signals/"+str(m(s)["id"])+"/resolve", map[string]any{"action": "approve"}, 200)
				return true
			}
		}
		return false
	})
	b := owner.waitGoalDone(ws, goal, 30*time.Second)
	var report map[string]any
	for _, o := range list(b["outputs"]) {
		if str(m(o)["name"]) == "report.md" {
			report = m(o)
		}
	}
	if report == nil || len(list(report["versions"])) != 2 || str(report["status"]) != "approved" {
		t.Errorf("reviewed output: %v", report)
	}
	var summary string
	for _, x := range list(b["tasks"]) {
		if str(m(x)["kind"]) == "work" {
			summary = str(m(x)["summary"])
		}
	}
	if !strings.Contains(summary, "Option B") || !strings.Contains(summary, "ran rm -rf build") {
		t.Errorf("the agent didn’t get the answers: %q", summary)
	}
	var audits int
	_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE type LIKE 'signals.%'`).Scan(&audits)
	if audits < 4 {
		t.Errorf("signal decisions audited: %d", audits)
	}
}

// Crashes restart once into a Recovered session; stalls get a reminder; runners reconnect.
func TestCrashStallAndReconnect(t *testing.T) {
	h := startHub(t)
	owner, org := h.setupOrg("Crash Co", "Cy Owner", "cy@crash.test")
	mc := h.pair(owner, org, "computer", "Cy’s laptop", "none")
	mc.start()
	h.waitOnline(owner, org, mc.id, true)
	work := t.TempDir()
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(mc.id, filepath.Join(work, "api")), 201)["workspace"])["id"])
	riley := memberID(owner.board(ws), "Riley")
	goal := owner.goal(ws, riley, "Bumpy road [tasks:1] [crash] [stall]")
	owner.waitGoalDone(ws, goal, 60*time.Second)
	var recovered int
	_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE recovered_from IS NOT NULL`).Scan(&recovered)
	if recovered != 1 {
		t.Errorf("recovered sessions: %d", recovered)
	}
	var nudged int
	_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM tasks WHERE summary = 'Finished after a reminder.'`).Scan(&nudged)
	if nudged != 1 {
		t.Errorf("stalled task not finished after a reminder")
	}
	// The runner restarts and reconnects.
	mc.stop()
	h.waitOnline(owner, org, mc.id, false)
	mc.start()
	h.waitOnline(owner, org, mc.id, true)
	goal2 := owner.goal(ws, riley, "After restart [tasks:1]")
	owner.waitGoalDone(ws, goal2, 60*time.Second)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
