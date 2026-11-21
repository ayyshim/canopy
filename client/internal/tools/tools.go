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
	{protocol.ToolSubmitOutput, "Share a file you made (screenshot, recording, document, diff or report). Save it in your outputs folder first (keep subfolders, such as assets/app.css, as they are). For an HTML prototype, submit every page and its css, js and images from the outputs folder, and the entry page (index.html) last with needs_review. With needs_review, a person approves it or asks for changes; this waits for them.",
		`{"type":"object","properties":{"path":{"type":"string"},"title":{"type":"string"},"kind":{"type":"string","enum":["image","recording","document","diff","report"]},"needs_review":{"type":"boolean"},"note":{"type":"string"}},"required":["path","title"]}`},
	{protocol.ToolCompleteTask, "Mark your current task done, with a short summary of what you did and where the results are.",
		`{"type":"object","properties":{"summary":{"type":"string"},"no_work_reason":{"type":"string","description":"triage tasks only: why no work is needed, when you assigned nothing"}},"required":["summary"]}`},
	{protocol.ToolSubmitPlan, "Managers: submit the plan for the goal you're planning.",
		`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},"title":{"type":"string"},"skill":{"type":"string","description":"a skill key: a base role (developer, reviewer, researcher, designer, writer) or a subcategory key like marketing.seo. Staff match by exact key first, then by base role."},"description":{"type":"string"},"steps":{"type":"array","items":{"type":"string"}},"depends_on":{"type":"array","items":{"type":"string"}},"directories":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"mode":{"type":"string","enum":["read","read_write"]}},"required":["path","mode"]}}},"required":["key","title","skill"]}}},"required":["tasks"]}`},
	{protocol.ToolAssignTasks, "Managers: on a triage task, hand the work to staff. Adds these tasks to the goal and assigns them by skill. Call it before complete_task.",
		`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},"title":{"type":"string"},"skill":{"type":"string","description":"a skill key: a base role (developer, reviewer, researcher, designer, writer) or a subcategory key like marketing.seo. Staff match by exact key first, then by base role."},"description":{"type":"string"},"steps":{"type":"array","items":{"type":"string"}},"depends_on":{"type":"array","items":{"type":"string"}},"directories":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"mode":{"type":"string","enum":["read","read_write"]}},"required":["path","mode"]}}},"required":["key","title","skill"]}}},"required":["tasks"]}`},
	{protocol.ToolAnswerQuestion, "Managers: answer a question your staff asked, when people asked you to decide.",
		`{"type":"object","properties":{"signal_id":{"type":"string"},"answer":{"type":"string"}},"required":["signal_id","answer"]}`},
	{protocol.ToolOwnerChange, "Request a change in a directory you can only read; the directory's owner approves it and an agent who can write it makes the change.",
		`{"type":"object","properties":{"directory":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"}},"required":["directory","description"]}`},
	{protocol.ToolReadNotes, "Read the workspace's notes: plans, decisions, goal summaries and notes people wrote. Pass a query to search.",
		`{"type":"object","properties":{"query":{"type":"string"}}}`},
	{protocol.ToolReadItem, "Read one goal, task or note by its ID, such as G-12, T-34 or N-5: title, status, owner, description or steps, summary, note body and outputs.",
		`{"type":"object","properties":{"ref":{"type":"string","description":"G-12, T-34 or N-5"}},"required":["ref"]}`},
	{protocol.ToolWriteNote, "Save a note for the workspace: a decision, or something the next person or agent should know. Use visibility private only for a note the person you work for asked to keep to themselves.",
		`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"kind":{"type":"string","enum":["note","decision"]},"visibility":{"type":"string","enum":["shared","private"],"description":"default shared"}},"required":["title","body"]}`},
	{protocol.ToolUpdateNote, "Change an existing note by its ID (N-5): a shared note, or a private note of the person you work for. People's notes can be edited when they ask or when it clearly helps. Prefer mode append, which adds to the end and keeps what people wrote. Replace overwrites the body. The old version is kept.",
		`{"type":"object","properties":{"ref":{"type":"string","description":"N-5"},"title":{"type":"string"},"body":{"type":"string"},"mode":{"type":"string","enum":["append","replace"],"description":"how body is applied; default append"}},"required":["ref"]}`},
	{protocol.ToolAssignSkill, "Managers: give one of your staff a skill from the workspace skill library (or take it away with remove). Skills are listed in your planning prompt.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"},"skill":{"type":"string","description":"name of a library skill"},"remove":{"type":"boolean"}},"required":["member","skill"]}`},
	{protocol.ToolProposeStaff, "Managers: draft a new staff member and send it to a workspace admin for approval. Only when your team truly lacks someone. Attach skills you found on the web or wrote yourself (markdown). Returns right away; you are told later if it was approved or rejected.",
		`{"type":"object","properties":{"name":{"type":"string"},"role":{"type":"string","description":"a skill key: a base role (developer, reviewer, researcher, designer, writer) or a subcategory key like marketing.seo"},"instructions":{"type":"string","description":"how this person should work"},"reason":{"type":"string","description":"why the team needs them"},"skills":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"content":{"type":"string","description":"the skill as markdown"},"source_url":{"type":"string","description":"where you found it, if you did"}},"required":["name","content"]}}},"required":["name","role","instructions"]}`},
	{protocol.ToolReadBoard, "Managers: see the live workspace board: every goal and task with who has it, status, progress and how long things took, the team, and what waits on people. Use it to answer questions about progress or how long work will take.",
		`{"type":"object","properties":{"include_done":{"type":"boolean","description":"also list finished goals"}}}`},
	{protocol.ToolGetStaff, "Managers: read the current instructions of one of your staff. Use it before update_staff_instructions so you keep what still matters.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"}},"required":["member"]}`},
	{protocol.ToolUpdateStaff, "Managers: draft new instructions for one of your staff and send them to a workspace admin for approval. Send the full new text. Returns right away; you are told later if it was approved or rejected.",
		`{"type":"object","properties":{"member":{"type":"string","description":"name of a staff member who reports to you"},"instructions":{"type":"string"},"reason":{"type":"string"}},"required":["member","instructions"]}`},
	{protocol.ToolGetAttachments, "Download the files attached to your goal and its tasks (images, documents, data) into ./attachments. Returns their paths.",
		`{"type":"object","properties":{"name":{"type":"string","description":"only this file; leave out for all"}}}`},
	{protocol.ToolNotify, "Send a short message to the people on this goal, on their phone and in Discord. Only for real milestones (a first working build, a screenshot worth seeing, an artifact you published) or when you are blocked by something outside Canopy. Not for routine steps, and not for what ask_question, submit_output and complete_task already notify about. Limits: 5 per task per hour, 20 per day.",
		`{"type":"object","properties":{"message":{"type":"string","description":"one or two plain sentences"},"level":{"type":"string","enum":["info","milestone","needs_attention"],"description":"needs_attention is for being blocked; it counts as an error notice"},"files":{"type":"array","items":{"type":"string"},"description":"names of outputs from this task or goal to attach"},"links":{"type":"array","items":{"type":"string"},"description":"https links, such as a published artifact"}},"required":["message"]}`},
	{protocol.ToolAttachFile, "Attach a file to your goal (default) or your task, so people and other agents on the goal can use it.",
		`{"type":"object","properties":{"path":{"type":"string"},"to":{"type":"string","enum":["goal","task"]}},"required":["path"]}`},
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
