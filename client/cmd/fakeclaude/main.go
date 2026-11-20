// Command fakeclaude imitates Claude Code's print mode (stream-json in and out) and calls
// Canopy's tools through the MCP config it's given. Tests and demos use it so the whole system
// runs without a model. Markers in the goal or task text pick a scenario:
//
//	[tasks:N] plan N tasks     [badplan] first plan has a cycle   [ask] ask a question
//	[gate] run a gated command  [output] save an output             [review] output needs review
//	[dir:/p] request /p          [owner:/p] request an owner change  [crash] crash once
//	[limit] hit a usage limit once  [stall] stop without finishing once  [slow:N] N seconds per step
//	[read:/p] try to read /p (sandbox checks)  [write:/p] write to /p
//	[dirs:/p:rw] the plan's first task needs /p  [everydir] every task needs it  [parallel] no dependencies
//	[code:repo] write a file in the repository's worktree  [conflict:repo] every task edits the same file
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	sessionID string
	model     = "fake-sonnet"
	tools     *mcp.ClientSession
	msgSeq    int
	stateDir  = stateLocation()
)

// stateLocation survives sandbox restarts: ~/.claude is shared with the sandbox.
func stateLocation() string {
	if v := os.Getenv("FAKECLAUDE_STATE"); v != "" {
		return v
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".claude", "fakeclaude-state")
	}
	return filepath.Join(os.TempDir(), "fakeclaude")
}

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func assistant(text string) {
	msgSeq++
	content := []any{}
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	emit(map[string]any{"type": "assistant", "session_id": sessionID, "message": map[string]any{
		"id": fmt.Sprintf("msg_%s_%d", uuid.NewString()[:13], msgSeq), "model": model, "role": "assistant", "content": content,
		"usage": map[string]any{"input_tokens": 120, "output_tokens": 40, "cache_creation_input_tokens": 0}}})
}

func toolUse(name string, input any) {
	msgSeq++
	b, _ := json.Marshal(input)
	emit(map[string]any{"type": "assistant", "session_id": sessionID, "message": map[string]any{
		"id": fmt.Sprintf("msg_%s_%d", uuid.NewString()[:13], msgSeq), "model": model, "role": "assistant",
		"content": []any{map[string]any{"type": "tool_use", "name": name, "input": json.RawMessage(b)}},
		"usage":   map[string]any{"input_tokens": 80, "output_tokens": 20}}})
}

func result(text string, isErr bool) {
	emit(map[string]any{"type": "result", "subtype": "success", "is_error": isErr, "result": text, "session_id": sessionID,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0}})
}

func call(name string, args map[string]any) (string, bool) {
	toolUse("mcp__canopy__"+name, args)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	res, err := tools.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	text := ""
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text += t.Text
		}
	}
	return text, res.IsError
}

func marker(text, name string) bool { return strings.Contains(text, "["+name+"]") }

func markerArg(text, name string) string {
	m := regexp.MustCompile(`\[` + name + `:([^\]]+)\]`).FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// once reports true the first time a key is seen (across restarts).
func once(key string) bool {
	_ = os.MkdirAll(stateDir, 0o755)
	p := filepath.Join(stateDir, "once-"+regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(key, "_"))
	if _, err := os.Stat(p); err == nil {
		return false
	}
	_ = os.WriteFile(p, nil, 0o644)
	return true
}

func remember(text string) {
	_ = os.WriteFile(filepath.Join(stateDir, "task-"+sessionID), []byte(text), 0o644)
}

func recall() string {
	b, _ := os.ReadFile(filepath.Join(stateDir, "task-"+sessionID))
	return string(b)
}

func line(text, prefix string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(l, prefix))
		}
	}
	return ""
}

func plan(text string) {
	goal := line(text, "New goal:")
	n := 2
	if v, err := strconv.Atoi(markerArg(goal, "tasks")); err == nil && v > 0 {
		n = v
	}
	skills := []string{"developer", "writer", "reviewer", "researcher", "designer"}
	build := func(cycle bool) []any {
		var tasks []any
		for i := 1; i <= n; i++ {
			t := map[string]any{"key": fmt.Sprintf("t%d", i), "title": fmt.Sprintf("Part %d of %s", i, stripMarkers(goal)),
				"skill": skills[(i-1)%len(skills)], "steps": []string{"Look around", "Do the work", "Check it"}, "depends_on": []string{}}
			if i > 1 && !marker(goal, "parallel") {
				t["depends_on"] = []string{fmt.Sprintf("t%d", i-1)}
			}
			if cycle && i == 1 && n > 1 {
				t["depends_on"] = []string{fmt.Sprintf("t%d", n)}
			}
			if d := markerArg(goal, "dirs"); d != "" && (i == 1 || marker(goal, "everydir")) {
				parts := strings.Split(d, ":")
				mode := "read"
				if len(parts) > 1 && parts[1] == "rw" {
					mode = "read_write"
				}
				t["directories"] = []any{map[string]any{"path": parts[0], "mode": mode}}
			}
			tasks = append(tasks, t)
		}
		return tasks
	}
	assistant("I’ll plan this goal.")
	res, isErr := call("submit_plan", map[string]any{"tasks": build(marker(goal, "badplan") && once("badplan-"+goal))})
	if isErr {
		assistant("The plan needs fixing: " + firstLine(res))
		res, isErr = call("submit_plan", map[string]any{"tasks": build(false)})
	}
	if isErr {
		result("Couldn’t submit the plan: "+res, false)
		return
	}
	call("complete_task", map[string]any{"summary": fmt.Sprintf("Planned %d tasks.", n)})
	result("Plan submitted.", false)
}

func stripMarkers(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s*\[[^\]]+\]`).ReplaceAllString(s, ""))
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func work(text string, resumed bool) {
	title := line(text, "Task:")
	outDir := ""
	if m := regexp.MustCompile(`Save outputs people should see in (\S+) and`).FindStringSubmatch(text); m != nil {
		outDir = m[1]
	}
	key := title + "|" + outDir
	slow := 0
	if v, err := strconv.Atoi(markerArg(text, "slow")); err == nil {
		slow = v
	}
	notes := []string{}
	if marker(text, "crash") && once("crash-"+key) {
		assistant("Starting…")
		os.Exit(3)
	}
	if marker(text, "limit") && once("limit-"+key) {
		emit(map[string]any{"type": "rate_limit_event", "session_id": sessionID, "rate_limit_info": map[string]any{
			"status": "rejected", "resetsAt": time.Now().Add(time.Hour).Unix(), "unifiedWindows": map[string]any{"five_hour": map[string]any{"utilization": 1.0}}}})
		result(fmt.Sprintf("Claude AI usage limit reached|%d", time.Now().Add(time.Hour).Unix()), true)
		return
	}
	if marker(text, "stall") && once("stall-"+key) {
		assistant("Let me think about this.")
		result("Thinking about it.", false)
		return
	}
	if p := markerArg(text, "read"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			notes = append(notes, "couldn’t read "+p)
		} else {
			notes = append(notes, "read "+p+": "+strings.TrimSpace(string(b)))
		}
	}
	if p := markerArg(text, "write"); p != "" {
		if err := os.WriteFile(p, []byte("changed by "+stripMarkers(title)+"\n"), 0o644); err != nil {
			notes = append(notes, "couldn’t write "+p)
		} else {
			notes = append(notes, "wrote "+p)
		}
	}
	if repo := markerArg(text, "code"); repo != "" {
		name := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(stripMarkers(title)), "-") + ".txt"
		if err := os.WriteFile(filepath.Join(repo, name), []byte(title+"\n"), 0o644); err != nil {
			notes = append(notes, "couldn’t write code: "+err.Error())
		}
	}
	if repo := markerArg(text, "conflict"); repo != "" {
		_ = os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("from "+title+"\n"), 0o644)
	}
	if p := markerArg(text, "dir"); p != "" && once("dir-"+key) {
		res, _ := call("request_directory", map[string]any{"path": p, "mode": "read", "reason": "Need to read it for this task"})
		notes = append(notes, "directory: "+firstLine(res))
		if strings.Contains(res, "restarts") {
			result("Waiting for the restart.", false)
			return
		}
	}
	if marker(text, "ask") && once("ask-"+key) {
		res, _ := call("ask_question", map[string]any{"question": "Which approach should I take?", "options": []string{"Option A", "Option B"}, "suggested": 0})
		notes = append(notes, firstLine(res))
	}
	if p := markerArg(text, "owner"); p != "" && once("owner-"+key) {
		res, _ := call("request_owner_change", map[string]any{"directory": p, "title": "Update the shared config", "description": "Change the timeout from 5s to 10s."})
		notes = append(notes, firstLine(res))
	}
	for step := 1; step <= 3; step++ {
		if slow > 0 {
			time.Sleep(time.Duration(slow) * time.Second)
		}
		if step == 2 && marker(text, "gate") && once("gate-"+key) {
			toolUse("Bash", map[string]any{"command": "rm -rf build"})
			res, _ := call("permission", map[string]any{"tool_name": "Bash", "input": map[string]any{"command": "rm -rf build"}})
			if strings.Contains(res, `"allow"`) {
				notes = append(notes, "ran rm -rf build")
			} else {
				notes = append(notes, "wasn’t allowed to run rm -rf build")
			}
		}
		call("report_progress", map[string]any{"step": step, "note": fmt.Sprintf("Finished step %d", step)})
	}
	if outDir != "" && (marker(text, "output") || marker(text, "review")) {
		_ = os.MkdirAll(outDir, 0o755)
		name := filepath.Join(outDir, "report.md")
		_ = os.WriteFile(name, []byte("# "+title+"\n\nVersion 1.\n"), 0o644)
		res, _ := call("submit_output", map[string]any{"path": name, "title": "Report", "kind": "document", "needs_review": marker(text, "review")})
		if strings.Contains(res, "Changes requested") {
			_ = os.WriteFile(name, []byte("# "+title+"\n\nVersion 2, with the changes.\n"), 0o644)
			res, _ = call("submit_output", map[string]any{"path": name, "title": "Report", "kind": "document", "needs_review": true})
		}
		notes = append(notes, "output: "+firstLine(res))
	}
	summary := "Did " + stripMarkers(title)
	if len(notes) > 0 {
		summary += " (" + strings.Join(notes, "; ") + ")"
	}
	res, isErr := call("complete_task", map[string]any{"summary": summary})
	if isErr && strings.Contains(res, "conflicts in") {
		// Resolve like a person would: keep both changes, commit, try again.
		repo := markerArg(text, "conflict")
		_ = os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("resolved by "+title+"\n"), 0o644)
		_ = exec.Command("git", "-C", repo, "add", "--all").Run()
		_ = exec.Command("git", "-C", repo, "-c", "user.name=Fake", "-c", "user.email=fake@localhost", "commit", "--quiet", "--no-edit").Run()
		summary += " (resolved a conflict)"
		res, isErr = call("complete_task", map[string]any{"summary": summary})
	}
	if isErr {
		result("Couldn’t finish: "+res, false)
		return
	}
	result(summary, false)
}

func handle(text string) {
	switch {
	case strings.HasPrefix(text, "New goal:"):
		remember(text)
		plan(text)
	case strings.HasPrefix(text, "Task:"):
		remember(text)
		work(text, false)
	case strings.HasPrefix(text, "Message from"):
		assistant("On it.")
		result("On it — thanks for the note.", false)
	case strings.HasPrefix(text, "Comment from"):
		result("Thanks, noted.", false)
	case strings.Contains(text, "(your staff) asks"):
		id := regexp.MustCompile(`signal_id ([0-9a-f-]{36})`).FindStringSubmatch(text)
		if id != nil {
			call("answer_question", map[string]any{"signal_id": id[1], "answer": "Go with Option A."})
		}
		result("Answered.", false)
	case strings.HasPrefix(text, "You stopped"):
		call("complete_task", map[string]any{"summary": "Finished after a reminder."})
		result("Done.", false)
	default:
		// Continue after a restart, a grant or a limit: pick the task up again.
		if prev := recall(); prev != "" {
			if strings.HasPrefix(prev, "New goal:") {
				plan(prev)
			} else {
				work(prev, true)
			}
			return
		}
		result("Okay.", false)
	}
}

func main() {
	args := os.Args[1:]
	var mcpConfig, resume string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--version":
			fmt.Println("2.1.0 (Fake Claude Code)")
			return
		case "--mcp-config":
			i++
			mcpConfig = args[i]
		case "--resume":
			i++
			resume = args[i]
		case "--model":
			i++
			model = "fake-" + args[i]
		}
	}
	sessionID = resume
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	_ = os.MkdirAll(stateDir, 0o755)
	if mcpConfig != "" {
		var cfg struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		b, err := os.ReadFile(mcpConfig)
		if err == nil {
			_ = json.Unmarshal(b, &cfg)
		}
		if srv, ok := cfg.MCPServers["canopy"]; ok {
			client := mcp.NewClient(&mcp.Implementation{Name: "fakeclaude", Version: "1"}, nil)
			cs, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: exec.Command(srv.Command, srv.Args...)}, nil)
			if err != nil {
				fmt.Fprintln(os.Stderr, "fakeclaude: connect to canopy tools:", err)
				os.Exit(1)
			}
			tools = cs
		}
	}
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": sessionID, "model": model, "tools": []string{}, "mcp_servers": []any{}})
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var in struct {
			Type    string `json:"type"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &in) != nil || in.Type != "user" {
			continue
		}
		handle(in.Message.Content)
	}
}
