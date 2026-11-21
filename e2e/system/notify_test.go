package system

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type discordMsg struct {
	Payload map[string]any
	Files   []string
}

// TestNotificationsReachDiscord runs a goal through the real hub, runner and fakeclaude and
// checks what a workspace's Discord receives. Set NOTIFY_DUMP to save the messages as JSON.
func TestNotificationsReachDiscord(t *testing.T) {
	var mu sync.Mutex
	var msgs []discordMsg
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg discordMsg
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				if p.FormName() == "payload_json" {
					_ = json.Unmarshal(b, &msg.Payload)
				} else {
					msg.Files = append(msg.Files, p.FileName())
				}
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&msg.Payload)
		}
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"1","channel_id":"2"}`))
	}))
	defer fake.Close()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(msgs) }

	h := startHub(t)
	owner, org := h.setupOrg("Notify Co", "Sam Owner", "sam@notify.test")
	mc := h.pair(owner, org, "computer", "Sam’s laptop", "none")
	mc.start()
	h.waitOnline(owner, org, mc.id, true)
	ws := str(m(owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", workspacePayload(mc.id, filepath.Join(t.TempDir(), "api")), 201)["workspace"])["id"])

	// The settings API refuses other hosts and never returns the token.
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/discord", map[string]any{"webhook": "https://example.com/api/webhooks/123456/abcdefghijklmnopqrstuvwx"}, 422)
	secret := "SECRETtokenSECRETtokenSECRET"
	put := owner.do("PUT", "/api/v1/workspaces/"+ws+"/discord", map[string]any{"webhook": "https://discord.com/api/webhooks/123456789/" + secret}, 200)
	got := owner.do("GET", "/api/v1/workspaces/"+ws+"/discord", nil, 200)
	for _, v := range []map[string]any{put, got} {
		if b, _ := json.Marshal(v); strings.Contains(string(b), secret) {
			t.Errorf("webhook token leaked: %s", b)
		}
	}
	// Point the saved webhook at the fake server (the API only takes discord.com).
	if _, err := h.db.Exec(context.Background(), `UPDATE workspaces SET discord_webhook=$2 WHERE id=$1`, ws, fake.URL+"/api/webhooks/123456789/"+secret); err != nil {
		t.Fatal(err)
	}

	riley := memberID(owner.board(ws), "Riley")
	goal := owner.goal(ws, riley, "Notify things [tasks:1] [ask] [gate] [review] [notify]")
	q := owner.waitSignal(ws, "question", 30*time.Second)
	owner.do("POST", "/api/v1/signals/"+str(q["id"])+"/resolve", map[string]any{"action": "answer", "text": "Option B"}, 200)
	g := owner.waitSignal(ws, "gate", 30*time.Second)
	owner.do("POST", "/api/v1/signals/"+str(g["id"])+"/resolve", map[string]any{"action": "allow"}, 200)
	r := owner.waitSignal(ws, "review", 30*time.Second)
	owner.do("POST", "/api/v1/signals/"+str(r["id"])+"/resolve", map[string]any{"action": "approve"}, 200)
	owner.waitGoalDone(ws, goal, 30*time.Second)
	owner.do("POST", "/api/v1/workspaces/"+ws+"/discord/test", nil, 202)

	// A manual workspace raises goal ready; a usage limit raises an error message.
	owner.do("PATCH", "/api/v1/workspaces/"+ws, map[string]any{"goal_completion": "manual"}, 200)
	owner.goal(ws, riley, "Wrap up [tasks:1]")
	waitFor(t, 30*time.Second, "a goal_ready signal", func() bool {
		b := owner.board(ws)
		owner.approveHandovers(b)
		for _, s := range list(b["signals"]) {
			if str(m(s)["type"]) == "goal_ready" {
				return true
			}
		}
		return false
	})
	owner.goal(ws, riley, "Hit the limit [tasks:1] [limit]")
	owner.waitSignal(ws, "usage_limit", 30*time.Second)

	waitFor(t, 40*time.Second, "discord messages", func() bool {
		var pending int
		_ = h.db.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox WHERE discord_sent_at IS NULL`).Scan(&pending)
		return pending == 0 && count() >= 10
	})

	mu.Lock()
	defer mu.Unlock()
	title := regexp.MustCompile(`^Payments - \S`)
	var withFile, withLink int
	for i, msg := range msgs {
		name, _ := msg.Payload["username"].(string)
		if !title.MatchString(name) {
			t.Errorf("message %d username %q", i, name)
		}
		raw, _ := json.Marshal(msg.Payload)
		if strings.Contains(string(raw), "@everyone") || strings.Contains(string(raw), "@here") {
			t.Errorf("message %d can ping everyone: %s", i, raw)
		}
		if am, _ := msg.Payload["allowed_mentions"].(map[string]any); am == nil || len(list(am["parse"])) != 0 {
			t.Errorf("message %d allowed_mentions: %v", i, msg.Payload["allowed_mentions"])
		}
		if len(msg.Files) > 0 {
			withFile++
		}
		if strings.Contains(string(raw), "?goal=") {
			withLink++
		}
		for _, e := range list(msg.Payload["embeds"]) {
			if et := str(m(e)["title"]); !title.MatchString(et) {
				t.Errorf("message %d embed title %q", i, et)
			}
		}
	}
	if withFile == 0 {
		t.Error("no message carried the screenshot")
	}
	if withLink == 0 {
		t.Error("no message linked back to the board")
	}
	if p := os.Getenv("NOTIFY_DUMP"); p != "" {
		b, _ := json.MarshalIndent(msgs, "", "  ")
		_ = os.WriteFile(p, b, 0o644)
	}
}
