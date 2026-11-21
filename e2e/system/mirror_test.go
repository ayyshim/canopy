package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DECISIONS: the hub copies folders so agents can read them while the owner's computer is
// off; a change by someone without write access waits for the owner's approval and runs on the
// owner's computer; with write access, changes made while the owner is off land on their disk
// when it reconnects; usage of approved work is the owner's.
func TestMirrorOfflineReadsAndOwnerApproval(t *testing.T) {
	if sandboxKind() != "bwrap" {
		t.Skip("mirrored directories are mounted by the sandbox; needs bubblewrap")
	}
	h := startHub(t)
	alice, org := h.setupOrg("Mirror Co", "Alice Owner", "alice@mirror.test")
	runner := h.pair(alice, org, "computer", "Alice’s runner", "bwrap")
	runner.start()
	h.waitOnline(alice, org, runner.id, true)

	work := t.TempDir()
	ws := str(m(alice.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(runner.id, filepath.Join(work, "api")), 201)["workspace"])["id"])

	// Bob joins and becomes a workspace admin so he can share a directory from his machine.
	inv := alice.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": "bob@mirror.test", "role": "member"}, 201)
	token := str(inv["link"])[strings.LastIndex(str(inv["link"]), "/")+1:]
	bob := h.client()
	bob.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "Bob", "password": "bob-pass-12"}, 200)
	bob.me = bob.enroll()
	bobID := str(m(bob.me["user"])["id"])
	alice.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "admin"}, 204)
	bobPC := h.pair(bob, org, "computer", "Bob’s laptop", "bwrap")
	bobPC.start()
	h.waitOnline(bob, org, bobPC.id, true)

	shared := filepath.Join(work, "bob-config")
	writeFile(t, filepath.Join(shared, "config.txt"), "timeout=5s\n")
	writeFile(t, filepath.Join(shared, ".env"), "SECRET=hunter2\n")
	writeFile(t, filepath.Join(shared, "node_modules", "x", "index.js"), "junk\n")
	dir := m(bob.do("POST", "/api/v1/workspaces/"+ws+"/directories", map[string]any{"path": shared}, 201)["directory"])
	dirID := str(dir["id"])
	if str(dir["machine_id"]) != bobPC.id {
		t.Fatalf("directory not on Bob’s machine: %v", dir)
	}
	waitFor(t, 20*time.Second, "hub copy of Bob’s directory", func() bool {
		st := m(m(alice.board(ws)["mirrors"])[dirID])
		return st["complete"] == true
	})
	var mirrored []string
	rows, _ := h.db.Query(context.Background(), `SELECT path FROM mirror_files WHERE directory_id::text = $1 ORDER BY path`, dirID)
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		mirrored = append(mirrored, p)
	}
	rows.Close()
	if strings.Join(mirrored, ",") != "config.txt" {
		t.Fatalf("mirrored files: %v (secrets and dependencies must stay home)", mirrored)
	}

	// Bob goes offline. Agents on Alice's runner still read his directory from the hub copy.
	bobPC.stop()
	h.waitOnline(alice, org, bobPC.id, false)
	b := alice.board(ws)
	riley := memberID(b, "Riley")
	read := alice.goal(ws, riley, "Check Bob’s config [tasks:1] [dirs:"+shared+":read] [read:"+filepath.Join(shared, "config.txt")+"]")
	// Dev has no grant on Bob's directory yet: the task asks first.
	access := alice.waitSignal(ws, "dir_access", 30*time.Second)
	alice.do("POST", "/api/v1/signals/"+str(access["id"])+"/resolve", map[string]any{"action": "grant"}, 200)
	b = alice.waitGoalDone(ws, read, 60*time.Second)
	if !strings.Contains(taskSummaries(b), "timeout=5s") {
		t.Errorf("agent couldn’t read the mirrored directory: %s", taskSummaries(b))
	}

	// Alice can only read Bob's directory, so changing it needs Bob's approval, and the change
	// runs on Bob's computer: it waits while that computer is off.
	change := alice.goal(ws, riley, "Tune Bob’s config [tasks:1] [dirs:"+shared+":rw] [write:"+filepath.Join(shared, "config.txt")+"]")
	sig := alice.waitSignal(ws, "owner_change", 30*time.Second)
	if str(m(sig["payload"])["owner_user_id"]) != bobID {
		t.Fatalf("owner approval not addressed to Bob: %v", sig)
	}
	alice.do("POST", "/api/v1/signals/"+str(sig["id"])+"/resolve", map[string]any{"action": "approve"}, 403)
	bob.do("POST", "/api/v1/signals/"+str(sig["id"])+"/resolve", map[string]any{"action": "approve"}, 200)
	time.Sleep(2 * time.Second)
	if b, _ := os.ReadFile(filepath.Join(shared, "config.txt")); string(b) != "timeout=5s\n" {
		t.Fatalf("Bob’s disk changed while he was offline: %q", b)
	}
	bobPC.start()
	alice.waitGoalDone(ws, change, 60*time.Second)
	if b, _ := os.ReadFile(filepath.Join(shared, "config.txt")); !strings.HasPrefix(string(b), "changed by") {
		t.Fatalf("the approved change didn’t run on Bob’s computer: %q", b)
	}

	// With write access, Alice's agents edit the hub copy while Bob is off; the change lands
	// on his disk when he's back.
	bob.do("PUT", "/api/v1/workspaces/"+ws+"/directories/"+dirID+"/access/"+str(m(alice.me["user"])["id"]), map[string]any{"mode": "read_write"}, 200)
	bobPC.stop()
	h.waitOnline(alice, org, bobPC.id, false)
	writeTo := filepath.Join(shared, "notes.txt")
	edit := alice.goal(ws, riley, "Add a note to Bob’s config [tasks:1] [dirs:"+shared+":rw] [write:"+writeTo+"]")
	access = alice.waitSignal(ws, "dir_access", 30*time.Second) // Dev could only read it so far
	alice.do("POST", "/api/v1/signals/"+str(access["id"])+"/resolve", map[string]any{"action": "grant"}, 200)
	alice.waitGoalDone(ws, edit, 60*time.Second)
	if _, err := os.Stat(writeTo); err == nil {
		t.Fatal("Bob’s disk changed while he was offline")
	}
	waitFor(t, 20*time.Second, "queued write", func() bool {
		var n int
		_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM pending_writes WHERE status = 'queued'`).Scan(&n)
		return n == 1
	})
	bobPC.start()
	waitFor(t, 30*time.Second, "write applied on Bob’s disk", func() bool {
		b, _ := os.ReadFile(writeTo)
		return strings.HasPrefix(string(b), "changed by")
	})
	// The approved change's tokens are Bob's.
	var bobTokens int64
	_ = h.db.QueryRow(context.Background(), `SELECT coalesce(sum(tokens_in + tokens_out), 0) FROM usage_events WHERE attributed_user_id::text = $1`, bobID).Scan(&bobTokens)
	if bobTokens == 0 {
		rows, _ := h.db.Query(context.Background(), `SELECT coalesce(u.attributed_user_id::text, 'none'), coalesce(t.title, '?'), count(*) FROM usage_events u
			LEFT JOIN sessions s ON s.id = u.session_id LEFT JOIN tasks t ON t.id = s.task_id GROUP BY 1, 2`)
		for rows.Next() {
			var who, task string
			var n int
			_ = rows.Scan(&who, &task, &n)
			t.Logf("usage: %s %s %d (bob %s)", who, task, n, bobID)
		}
		rows.Close()
		t.Error("approved change wasn’t attributed to the directory owner")
	}
}

func taskSummaries(b map[string]any) string {
	var s []string
	for _, x := range list(b["tasks"]) {
		s = append(s, str(m(x)["summary"]))
	}
	return strings.Join(s, " | ")
}

// Usage limits pause work until reset or until an API key takes over; API key caps pause at
// 100% until raised.
func TestUsageLimitAndCap(t *testing.T) {
	h := startHub(t)
	owner, org := h.setupOrg("Limits Co", "Lee Owner", "lee@limits.test")
	mc := h.pair(owner, org, "computer", "Lee’s laptop", "none")
	mc.start()
	h.waitOnline(owner, org, mc.id, true)
	work := t.TempDir()
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(mc.id, filepath.Join(work, "api")), 201)["workspace"])["id"])
	riley := memberID(owner.board(ws), "Riley")

	goal := owner.goal(ws, riley, "Busy work [tasks:1] [limit]")
	sig := owner.waitSignal(ws, "usage_limit", 30*time.Second)
	if str(m(sig["payload"])["cli"]) != "claude" {
		t.Errorf("limit payload: %v", sig["payload"])
	}
	// The header's usage is Claude Code's own report for this computer, not Canopy's limits.
	cu := owner.do("GET", "/api/v1/orgs/"+org+"/usage/claude", nil, 200)
	comps := list(cu["computers"])
	if len(comps) != 1 || m(comps[0])["reported"] != true {
		t.Fatalf("claude usage: %v", cu)
	}
	if ws := list(m(comps[0])["windows"]); len(ws) != 2 || m(ws[0])["key"] != "five_hour" || m(ws[0])["used_percent"].(float64) != 100 ||
		m(ws[1])["key"] != "seven_day" || m(ws[1])["used_percent"].(float64) < 41.9 || m(ws[1])["used_percent"].(float64) > 42.1 || m(ws[1])["resets_at"] == nil {
		t.Errorf("windows: %v", m(comps[0])["windows"])
	}
	// Use an API key until the reset: the key goes to the runner, the hub keeps a fingerprint.
	conns := owner.do("PUT", "/api/v1/runners/"+mc.id+"/connections/claude", map[string]any{"enabled": true, "mode": "subscription", "api_key": "sk-ant-test-9876"}, 200)
	if conns["key_delivered"] != true || str(m(list(conns["connections"])[0])["key_fingerprint"]) != "…9876" {
		t.Fatalf("key relay: %v", conns)
	}
	waitFor(t, 5*time.Second, "key stored on the runner", func() bool {
		b, err := os.ReadFile(filepath.Join(mc.home, ".config", "canopy", "keys", "claude.key"))
		return err == nil && string(b) == "sk-ant-test-9876"
	})
	var keyInDB int
	_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM cli_connections WHERE key_fingerprint LIKE '%sk-ant%'`).Scan(&keyInDB)
	if keyInDB != 0 {
		t.Fatal("the API key reached the database")
	}
	owner.do("POST", "/api/v1/signals/"+str(sig["id"])+"/resolve", map[string]any{"action": "use_key"}, 200)
	owner.waitGoalDone(ws, goal, 60*time.Second)

	// An API key with a tiny daily cap pauses at 100% until raised.
	owner.do("PUT", "/api/v1/runners/"+mc.id+"/connections/claude", map[string]any{"enabled": true, "mode": "api_key", "cap_tokens_per_day": 300}, 200)
	_, _ = h.db.Exec(context.Background(), `UPDATE accounts SET used_tokens = 0, limited_until = NULL, use_key_until = NULL`)
	goal2 := owner.goal(ws, riley, "Capped work [tasks:2]")
	var capSig map[string]any
	waitFor(t, 30*time.Second, "cap at 100%", func() bool {
		for _, s := range list(owner.board(ws)["signals"]) {
			if str(m(s)["type"]) == "cap" && m(m(s)["payload"])["level"] == float64(100) {
				capSig = m(s)
				return true
			}
		}
		return false
	})
	owner.do("POST", "/api/v1/signals/"+str(capSig["id"])+"/resolve", map[string]any{"action": "raise_cap", "cap": 10_000_000}, 200)
	owner.waitGoalDone(ws, goal2, 60*time.Second)
}
