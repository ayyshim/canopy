// Package tools defines the Canopy tools agents see, and the wire format between the
// in-sandbox bridge and the runner.
package tools

import (
	"encoding/json"

	"canopy/shared/protocol"
)

type Def struct {
	Name        string
	Description string
	Schema      string
}

var Defs = []Def{
	{protocol.ToolPermission, "Canopy decides whether a gated action may run. Called by Claude Code, not by you.",
		`{"type":"object","properties":{"tool_name":{"type":"string"},"input":{"type":"object"},"tool_use_id":{"type":"string"}},"required":["tool_name","input"]}`},
	{protocol.ToolAskQuestion, "Ask the people steering this workspace to decide something. Waits for the answer. Offer 2–4 short options and suggest one.",
		`{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},"suggested":{"type":"integer","description":"index of the suggested option"}},"required":["question"]}`},
	{protocol.ToolRequestDir, "Ask for access to one of the workspace's directories you can't see (or can only read).",
		`{"type":"object","properties":{"path":{"type":"string"},"mode":{"type":"string","enum":["read","read_write"]},"reason":{"type":"string"}},"required":["path","reason"]}`},
	{protocol.ToolReportProgress, "Report progress on your task after finishing a step (step is the 1-based number of the step you finished).",
		`{"type":"object","properties":{"step":{"type":"integer"},"progress":{"type":"integer","minimum":0,"maximum":100},"note":{"type":"string"}}}`},
	{protocol.ToolSubmitOutput, "Share a file you made (screenshot, recording, document, diff or report). Save it in your outputs folder first. With needs_review, a person approves it or asks for changes; this waits for them.",
		`{"type":"object","properties":{"path":{"type":"string"},"title":{"type":"string"},"kind":{"type":"string","enum":["image","recording","document","diff","report"]},"needs_review":{"type":"boolean"},"note":{"type":"string"}},"required":["path","title"]}`},
	{protocol.ToolCompleteTask, "Mark your current task done, with a short summary of what you did and where the results are.",
		`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`},
	{protocol.ToolSubmitPlan, "Managers: submit the plan for the goal you're planning.",
		`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},"title":{"type":"string"},"skill":{"type":"string","enum":["developer","reviewer","researcher","designer","writer"]},"description":{"type":"string"},"steps":{"type":"array","items":{"type":"string"}},"depends_on":{"type":"array","items":{"type":"string"}},"directories":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"mode":{"type":"string","enum":["read","read_write"]}},"required":["path","mode"]}}},"required":["key","title","skill"]}}},"required":["tasks"]}`},
	{protocol.ToolAnswerQuestion, "Managers: answer a question your staff asked, when people asked you to decide.",
		`{"type":"object","properties":{"signal_id":{"type":"string"},"answer":{"type":"string"}},"required":["signal_id","answer"]}`},
	{protocol.ToolOwnerChange, "Request a change in a directory you can only read; the directory's owner approves it and an agent who can write it makes the change.",
		`{"type":"object","properties":{"directory":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"}},"required":["directory","description"]}`},
	{protocol.ToolReadNotes, "Read the workspace's notes: plans, decisions, goal summaries and notes people wrote. Pass a query to search.",
		`{"type":"object","properties":{"query":{"type":"string"}}}`},
	{protocol.ToolWriteNote, "Save a note for the workspace: a decision, or something the next person or agent should know.",
		`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"kind":{"type":"string","enum":["note","decision"]}},"required":["title","body"]}`},
	{protocol.ToolAssignSkill, "Managers: give one of your staff a skill from the workspace skill library (or take it away with remove). Skills are listed in your planning prompt.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"},"skill":{"type":"string","description":"name of a library skill"},"remove":{"type":"boolean"}},"required":["member","skill"]}`},
	{protocol.ToolProposeStaff, "Managers: draft a new staff member and send it to a workspace admin for approval. Only when your team truly lacks someone. Attach skills you found on the web or wrote yourself (markdown). Returns right away; you are told later if it was approved or rejected.",
		`{"type":"object","properties":{"name":{"type":"string"},"role":{"type":"string","enum":["developer","reviewer","researcher","designer","writer"]},"instructions":{"type":"string","description":"how this person should work"},"reason":{"type":"string","description":"why the team needs them"},"skills":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"content":{"type":"string","description":"the skill as markdown"},"source_url":{"type":"string","description":"where you found it, if you did"}},"required":["name","content"]}}},"required":["name","role","instructions"]}`},
	{protocol.ToolGetStaff, "Managers: read the current instructions of one of your staff. Use it before update_staff_instructions so you keep what still matters.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"}},"required":["member"]}`},
	{protocol.ToolUpdateStaff, "Managers: draft new instructions for one of your staff and send them to a workspace admin for approval. Send the full new text. Returns right away; you are told later if it was approved or rejected.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"},"instructions":{"type":"string"},"reason":{"type":"string"}},"required":["member","instructions"]}`},
}

// Call is the bridge asking the runner to run a tool.
type Call struct {
	CallID string          `json:"call_id"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
}

// Result is the runner's answer.
type Result struct {
	CallID  string `json:"call_id"`
	Text    string `json:"text"`
	IsError bool   `json:"is_error,omitempty"`
}
