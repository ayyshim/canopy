package notify

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/store/db"
)

// Link is a labelled URL that goes with a notification.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Media is stored on each outbox row: outputs to attach and links to show as buttons.
type Media struct {
	OutputIDs []uuid.UUID `json:"output_ids"`
	Links     []Link      `json:"links"`
	// Test marks the sample message from the settings page; it skips the kind filter.
	Test bool `json:"test,omitempty"`
}

// Note is one notification to queue.
type Note struct {
	Kind     string // one of Kinds (not "daily")
	MemberID *uuid.UUID
	Body     string
	SignalID *uuid.UUID
	TaskID   *uuid.UUID
	GoalID   *uuid.UUID
	// For picks who gets the push; Recipients is filled from it when empty.
	For        *db.Signal
	Recipients []uuid.UUID
	Media      Media
}

// Title is always "{Workspace} - {Manager/Staff name}", or "{Workspace} - Canopy" without a member.
func Title(workspace, member string) string {
	if strings.TrimSpace(member) == "" {
		member = "Canopy"
	}
	return workspace + " - " + member
}

// DeepLink opens the board on the goal, task or signal (path only; add the public URL to share it).
func DeepLink(wsID uuid.UUID, goal, task, signal *uuid.UUID) string {
	q := url.Values{}
	if goal != nil {
		q.Set("goal", goal.String())
	}
	if task != nil {
		q.Set("task", task.String())
	}
	if signal != nil {
		q.Set("signal", signal.String())
	}
	l := "/w/" + wsID.String()
	if len(q) > 0 {
		l += "?" + q.Encode()
	}
	return l
}

var artifactRe = regexp.MustCompile(`https://claude\.ai/(?:code/)?artifact/[A-Za-z0-9_-]+`)

// ArtifactLinks pulls claude.ai artifact links out of text.
func ArtifactLinks(texts ...string) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, t := range texts {
		for _, u := range artifactRe.FindAllString(t, -1) {
			if !seen[u] {
				seen[u] = true
				out = append(out, Link{Label: "Open artifact", URL: u})
			}
		}
	}
	return out
}

// Enqueue queues a notification in the caller's transaction, so it exists only if the change does.
func Enqueue(ctx context.Context, t *core.Tx, ws db.Workspace, n Note) error {
	if n.Kind == "" {
		return nil
	}
	who := ""
	if n.MemberID != nil {
		if m, err := t.Q.GetMember(ctx, *n.MemberID); err == nil {
			who = m.Name
		}
	}
	if n.Recipients == nil {
		sig := db.Signal{WorkspaceID: ws.ID, MemberID: n.MemberID, TaskID: n.TaskID, GoalID: n.GoalID}
		if n.For != nil {
			sig = *n.For
		}
		n.Recipients = Recipients(ctx, t.Q, ws, sig)
	}
	if n.Media.OutputIDs == nil {
		n.Media.OutputIDs = []uuid.UUID{}
	}
	if n.Media.Links == nil {
		n.Media.Links = []Link{}
	}
	media, _ := json.Marshal(n.Media)
	body := strings.TrimSpace(n.Body)
	if len(body) > 3000 {
		body = body[:3000] + "…"
	}
	_, err := t.Q.InsertNotification(ctx, db.InsertNotificationParams{
		ID: uuid.Must(uuid.NewV7()), OrgID: ws.OrgID, WorkspaceID: ws.ID, MemberID: n.MemberID, Kind: n.Kind,
		Title: Title(ws.Name, who), Body: body, Link: DeepLink(ws.ID, n.GoalID, n.TaskID, n.SignalID),
		SignalID: n.SignalID, TaskID: n.TaskID, GoalID: n.GoalID, Recipients: n.Recipients, Media: media,
	})
	return err
}

// EnqueueSignal queues a notification for a signal that needs a person.
func EnqueueSignal(ctx context.Context, t *core.Tx, ws db.Workspace, sig db.Signal) error {
	kind := KindOf(sig.Type)
	if kind == "" {
		return nil
	}
	_, body := SignalText(ctx, t.Q, ws, sig)
	n := Note{Kind: kind, MemberID: sig.MemberID, Body: body, SignalID: &sig.ID, TaskID: sig.TaskID, GoalID: sig.GoalID, For: &sig}
	var p map[string]any
	_ = json.Unmarshal(sig.Payload, &p)
	if sig.Type == "review" {
		if id, err := uuid.Parse(strOf(p["output_id"])); err == nil {
			n.Media.OutputIDs = append(n.Media.OutputIDs, id)
		}
	}
	n.Media.Links = ArtifactLinks(body)
	return Enqueue(ctx, t, ws, n)
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
