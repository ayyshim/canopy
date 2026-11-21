package claudecode

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRecordedTranscript parses a transcript recorded from Claude Code 2.1.
func TestRecordedTranscript(t *testing.T) {
	f, err := os.Open("testdata/pong.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var kinds []string
	var init, result Event
	var usageIn, usageOut int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		evs, err := Parse(sc.Bytes())
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for _, e := range evs {
			kinds = append(kinds, e.Kind)
			switch e.Kind {
			case "init":
				init = e
			case "result":
				result = e
			case "usage":
				usageIn += e.TokensIn
				usageOut += e.TokensOut
			}
		}
	}
	if init.SessionID == "" || init.Model == "" {
		t.Errorf("init: %+v", init)
	}
	if result.Text != "pong" || result.IsError || result.SessionID != init.SessionID {
		t.Errorf("result: %+v", result)
	}
	if usageIn != 4+8271 || usageOut != 4 {
		t.Errorf("usage in=%d out=%d", usageIn, usageOut)
	}
	if strings.Join(kinds, ",") != "init,rate_limit,text,usage,result" {
		t.Errorf("events: %v", kinds)
	}
}

func TestRateLimitEvent(t *testing.T) {
	ok := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1795080000,"unifiedWindows":{"five_hour":{"utilization":0.37,"resetsAt":1795070000,"status":"allowed"},"seven_day":{"utilization":0.47}}}}`
	evs, _ := Parse([]byte(ok))
	if len(evs) != 1 || evs[0].Limited || evs[0].Utilization["five_hour"] != 0.37 || evs[0].ResetsAt == nil {
		t.Errorf("allowed: %+v", evs)
	}
	fh := evs[0].Windows["five_hour"]
	if fh.Utilization != 0.37 || fh.Status != "allowed" || fh.ResetsAt == nil || fh.ResetsAt.Unix() != 1795070000 {
		t.Errorf("five_hour window: %+v", fh)
	}
	if sd := evs[0].Windows["seven_day"]; sd.Utilization != 0.47 || sd.ResetsAt != nil {
		t.Errorf("seven_day window: %+v", sd)
	}
	bad := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1795080000}}`
	evs, _ = Parse([]byte(bad))
	if !evs[0].Limited {
		t.Errorf("rejected not limited: %+v", evs)
	}
}

func TestLimitMessages(t *testing.T) {
	if ok, at := LimitFromText("Claude AI usage limit reached|1795080000"); !ok || at == nil || at.Unix() != 1795080000 {
		t.Errorf("pipe format: %v %v", ok, at)
	}
	if ok, at := LimitFromText("You've hit your limit · resets 3pm"); !ok || at == nil || at.Hour() != 15 {
		t.Errorf("resets format: %v %v", ok, at)
	}
	if ok, at := LimitFromText("5-hour limit reached ∙ resets 11:30am"); !ok || at == nil || at.Hour() != 11 || at.Minute() != 30 {
		t.Errorf("minutes format: %v %v", ok, at)
	}
	if ok, _ := LimitFromText("API Error: 500 internal error"); ok {
		t.Error("treated a server error as a limit")
	}
	r := `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1795080000","session_id":"s"}`
	evs, _ := Parse([]byte(r))
	if len(evs) != 2 || evs[1].Kind != "rate_limit" || !evs[1].Limited || evs[1].ResetsAt.Before(time.Unix(1795079999, 0)) {
		t.Errorf("result with limit: %+v", evs)
	}
}

func TestArgsAndPermissionReply(t *testing.T) {
	a := strings.Join(Args(Params{Model: "sonnet", MCPConfig: "/x.json", Resume: "abc", AddDirs: []string{"/a", "/b"}}), " ")
	for _, want := range []string{"--input-format stream-json", "--permission-prompt-tool mcp__canopy__permission", "--setting-sources user,project,local", "--resume abc", "--add-dir /a --add-dir /b", "--model sonnet"} {
		if !strings.Contains(a, want) {
			t.Errorf("args missing %q: %s", want, a)
		}
	}
	if strings.Contains(a, "--strict-mcp-config") {
		t.Errorf("--strict-mcp-config hides claude.ai connectors: %s", a)
	}
	if got := PermissionReply(true, []byte(`{"command":"ls"}`), ""); got != `{"behavior":"allow","updatedInput":{"command":"ls"}}` {
		t.Errorf("allow reply: %s", got)
	}
	if got := PermissionReply(false, nil, "No"); got != `{"behavior":"deny","message":"No"}` {
		t.Errorf("deny reply: %s", got)
	}
}
