// Package claudecode drives Claude Code in print mode with streaming JSON in and out
// (SPEC "CLI adapters"), and turns its output into Canopy events.
package claudecode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Params struct {
	Model              string
	AppendSystemPrompt string
	Resume             string
	MCPConfig          string // path to the MCP config file
	AddDirs            []string
}

// PermissionTool is how Claude Code asks Canopy before gated actions.
const PermissionTool = "mcp__canopy__permission"

// Args are Claude Code's flags for a Canopy session.
func Args(p Params) []string {
	a := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--mcp-config", p.MCPConfig,
		"--permission-prompt-tool", PermissionTool, "--permission-mode", "acceptEdits",
		"--allowedTools", "mcp__canopy", // Canopy's own tools never need a gate
		"--setting-sources", "project"}
	if p.Model != "" {
		a = append(a, "--model", p.Model)
	}
	if p.AppendSystemPrompt != "" {
		a = append(a, "--append-system-prompt", p.AppendSystemPrompt)
	}
	if p.Resume != "" {
		a = append(a, "--resume", p.Resume)
	}
	for _, d := range p.AddDirs {
		a = append(a, "--add-dir", d)
	}
	return a
}

// UserMessage is one input line.
func UserMessage(text string) []byte {
	b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	return append(b, '\n')
}

// MCPConfig points Claude Code at Canopy's tool bridge.
func MCPConfig(canopyBin, socket string) []byte {
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"canopy": map[string]any{"type": "stdio", "command": canopyBin, "args": []string{"mcp", "--socket", socket}},
	}})
	return b
}

// Event is something worth reporting from one output line.
type Event struct {
	Kind string // init, text, tool, usage, rate_limit, result

	SessionID string
	Model     string

	Text string // text, tool summary, result text

	MessageID string
	TokensIn  int64
	TokensOut int64

	Limited     bool
	ResetsAt    *time.Time
	Utilization map[string]float64

	IsError bool
}

type line struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Model     string          `json:"model"`
	Message   json.RawMessage `json:"message"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	RateLimit *struct {
		Status         string `json:"status"`
		ResetsAt       int64  `json:"resetsAt"`
		RateLimitType  string `json:"rateLimitType"`
		UnifiedWindows map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
			Status      string  `json:"status"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
}

type message struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// Parse reads one output line. Unknown lines yield nothing.
func Parse(b []byte) ([]Event, error) {
	var l line
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" {
			return []Event{{Kind: "init", SessionID: l.SessionID, Model: l.Model}}, nil
		}
	case "assistant":
		var m message
		if err := json.Unmarshal(l.Message, &m); err != nil {
			return nil, err
		}
		var out []Event
		for _, c := range m.Content {
			switch c.Type {
			case "text":
				if strings.TrimSpace(c.Text) != "" {
					out = append(out, Event{Kind: "text", Text: c.Text})
				}
			case "tool_use":
				out = append(out, Event{Kind: "tool", Text: ToolSummary(c.Name, c.Input)})
			}
		}
		if m.Usage != nil {
			// New input processed (fresh plus cache writes) and output; cache reads are left out.
			out = append(out, Event{Kind: "usage", MessageID: m.ID, Model: m.Model,
				TokensIn: m.Usage.InputTokens + m.Usage.CacheCreationInputTokens, TokensOut: m.Usage.OutputTokens})
		}
		return out, nil
	case "rate_limit_event":
		if l.RateLimit == nil {
			return nil, nil
		}
		e := Event{Kind: "rate_limit", Limited: l.RateLimit.Status != "" && l.RateLimit.Status != "allowed" && l.RateLimit.Status != "allowed_warning",
			Utilization: map[string]float64{}}
		if l.RateLimit.ResetsAt > 0 {
			t := time.Unix(l.RateLimit.ResetsAt, 0)
			e.ResetsAt = &t
		}
		for k, w := range l.RateLimit.UnifiedWindows {
			e.Utilization[k] = w.Utilization
		}
		return []Event{e}, nil
	case "result":
		e := Event{Kind: "result", SessionID: l.SessionID, Text: l.Result, IsError: l.IsError}
		if l.IsError {
			if limited, at := LimitFromText(l.Result); limited {
				return []Event{e, {Kind: "rate_limit", Limited: true, ResetsAt: at, Text: l.Result}}, nil
			}
		}
		return []Event{e}, nil
	}
	return nil, nil
}

// ToolSummary is a short line for the activity log.
func ToolSummary(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description"} {
		if v, ok := in[k].(string); ok && v != "" {
			if len(v) > 200 {
				v = v[:200] + "…"
			}
			return name + ": " + v
		}
	}
	return name
}

var (
	pipeEpoch = regexp.MustCompile(`(?i)usage limit reached\|(\d{9,})`)
	resetsAt  = regexp.MustCompile(`(?i)resets? (?:at )?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	limitText = regexp.MustCompile(`(?i)(usage limit|rate limit|hit your limit|limit reached|out of extra usage)`)
)

// LimitFromText recognizes Claude Code's limit messages, in the formats different versions
// print, and returns the reset time when it's given.
func LimitFromText(s string) (bool, *time.Time) {
	if m := pipeEpoch.FindStringSubmatch(s); m != nil {
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		t := time.Unix(sec, 0)
		return true, &t
	}
	if !limitText.MatchString(s) {
		return false, nil
	}
	if m := resetsAt.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mins := 0
		if m[2] != "" {
			mins, _ = strconv.Atoi(m[2])
		}
		switch strings.ToLower(m[3]) {
		case "pm":
			if h < 12 {
				h += 12
			}
		case "am":
			if h == 12 {
				h = 0
			}
		}
		now := time.Now()
		t := time.Date(now.Year(), now.Month(), now.Day(), h, mins, 0, 0, now.Location())
		if t.Before(now) {
			t = t.Add(24 * time.Hour)
		}
		return true, &t
	}
	return true, nil
}

// PermissionReply is what Claude Code expects back from the permission prompt tool.
func PermissionReply(allow bool, input json.RawMessage, message string) string {
	if allow {
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		b, _ := json.Marshal(map[string]any{"behavior": "allow", "updatedInput": input})
		return string(b)
	}
	if message == "" {
		message = "A person denied this."
	}
	b, _ := json.Marshal(map[string]any{"behavior": "deny", "message": message})
	return string(b)
}

var _ = fmt.Sprint
