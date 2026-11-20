package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/client/internal/blobs"
	"canopy/shared/protocol"
)

// fakeHub records what the runner sends and answers tool calls.
type fakeHub struct {
	mu   sync.Mutex
	msgs []protocol.Envelope
	m    *Manager
}

func (f *fakeHub) Send(typ string, data any) error {
	b, _ := json.Marshal(data)
	f.mu.Lock()
	f.msgs = append(f.msgs, protocol.Envelope{Type: typ, Data: b})
	f.mu.Unlock()
	if typ == protocol.TToolCall {
		var c protocol.ToolCall
		_ = json.Unmarshal(b, &c)
		allow := true
		go f.m.ToolResult(protocol.ToolResult{SessionID: c.SessionID, CallID: c.CallID, Text: "ok", Allow: &allow})
	}
	return nil
}

func (f *fakeHub) find(typ string, match func(json.RawMessage) bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs {
		if m.Type == typ && match(m.Data) {
			return true
		}
	}
	return false
}

// TestLiveClaudeInSandbox drives real Claude Code (costs a few cents): CANOPY_LIVE=1.
func TestLiveClaudeInSandbox(t *testing.T) {
	if os.Getenv("CANOPY_LIVE") == "" {
		t.Skip("set CANOPY_LIVE=1 to run against real Claude Code")
	}
	bin := filepath.Join(t.TempDir(), "canopy")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/canopy").CombinedOutput(); err != nil {
		t.Fatalf("build canopy: %v %s", err, out)
	}
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "hello.txt"), []byte("hello from the folder"), 0o644)
	hub := &fakeHub{}
	m := New(hub, &blobs.Client{}, t.TempDir(), "bwrap", bin)
	hub.m = m
	id := uuid.New()
	m.Start(protocol.StartSession{SessionID: id, MemberName: "Spike", CLI: "claude", Model: "haiku", Work: "spike",
		Mounts:             []protocol.Mount{{Path: folder, Mode: "rw"}},
		AppendSystemPrompt: "You are a test agent. Follow instructions exactly and be brief.",
		Turn:               &protocol.Turn{TurnID: "t1", Kind: "task", Text: "Run the shell command `wc -c " + folder + "/hello.txt` with Bash, then call the canopy tool report_progress with step 1 and note 'counted', then reply with just the byte count."}})
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		if hub.find(protocol.TSessionEvent, func(d json.RawMessage) bool {
			return strings.Contains(string(d), `"status":"waiting"`) && strings.Contains(string(d), `"turn_id":"t1"`)
		}) {
			break
		}
		if hub.find(protocol.TSessionEvent, func(d json.RawMessage) bool {
			return strings.Contains(string(d), `"status":"error"`) || strings.Contains(string(d), `"status":"crashed"`)
		}) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	m.StopAll()
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var sawInit, sawTool, sawUsage, sawResult bool
	for _, msg := range hub.msgs {
		s := string(msg.Data)
		switch msg.Type {
		case protocol.TSessionEvent:
			if strings.Contains(s, "cli_session_id") {
				sawInit = true
			}
			if strings.Contains(s, `"status":"waiting"`) && strings.Contains(s, "21") {
				sawResult = true
			}
			if strings.Contains(s, `"status":"error"`) || strings.Contains(s, `"status":"crashed"`) {
				t.Errorf("session failed: %s", s)
			}
		case protocol.TToolCall:
			if strings.Contains(s, "report_progress") {
				sawTool = true
			}
		case protocol.TUsage:
			sawUsage = true
		}
		t.Logf("%s %s", msg.Type, truncate(s, 300))
	}
	if !sawInit || !sawTool || !sawUsage || !sawResult {
		t.Errorf("init=%v tool=%v usage=%v result=%v", sawInit, sawTool, sawUsage, sawResult)
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
