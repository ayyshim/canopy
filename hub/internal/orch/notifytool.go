package orch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

const (
	notifyPerTaskHour  = 5
	notifyPerMemberDay = 20
	notifyMaxLinks     = 5
	notifyMaxFiles     = 10
	notifyMaxMessage   = 1000
)

// toolNotify lets an agent send its own short message to the goal's people.
func (o *Orch) toolNotify(ctx context.Context, tc *toolCtx) error {
	var in struct {
		Message string   `json:"message"`
		Level   string   `json:"level"`
		Files   []string `json:"files"`
		Links   []string `json:"links"`
	}
	if err := decode(tc.call.Args, &in); err != nil {
		return err
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		return toolError("Write a message.")
	}
	if utf8.RuneCountInString(msg) > notifyMaxMessage {
		return toolError(fmt.Sprintf("Keep the message under %d characters.", notifyMaxMessage))
	}
	level := in.Level
	switch level {
	case "":
		level = "info"
	case "info", "milestone", "needs_attention":
	default:
		return toolError("level must be info, milestone or needs_attention.")
	}
	links, err := notifyLinks(in.Links)
	if err != nil {
		return err
	}
	if len(in.Files) > notifyMaxFiles {
		return toolError(fmt.Sprintf("Attach at most %d files.", notifyMaxFiles))
	}
	kind := "progress"
	if level == "needs_attention" {
		kind = "errors"
	}
	var goalID, taskID *uuid.UUID
	if tc.task != nil {
		goalID, taskID = tc.task.GoalID, &tc.task.ID
	}
	err = o.Core.Tx(ctx, func(t *core.Tx) error {
		now := time.Now()
		if taskID != nil {
			n, err := t.Q.CountAgentNotificationsForTask(ctx, db.CountAgentNotificationsForTaskParams{TaskID: taskID, CreatedAt: now.Add(-time.Hour)})
			if err != nil {
				return err
			}
			if n >= notifyPerTaskHour {
				return toolError(fmt.Sprintf("You have already sent %d notifications for this task in the last hour. Keep going and mention the rest in complete_task.", notifyPerTaskHour))
			}
		}
		n, err := t.Q.CountAgentNotificationsForMember(ctx, db.CountAgentNotificationsForMemberParams{MemberID: &tc.member.ID, CreatedAt: now.Add(-24 * time.Hour)})
		if err != nil {
			return err
		}
		if n >= notifyPerMemberDay {
			return toolError(fmt.Sprintf("You have already sent %d notifications in the last day. Use complete_task or ask_question instead.", notifyPerMemberDay))
		}
		ids, err := o.notifyOutputs(ctx, t, tc, in.Files)
		if err != nil {
			return err
		}
		note := notify.Note{Kind: kind, MemberID: &tc.member.ID, GoalID: goalID, TaskID: taskID, Body: msg,
			// Goal-level news goes to the goal's person, like other progress.
			For:   &db.Signal{WorkspaceID: tc.ws.ID, MemberID: &tc.member.ID, GoalID: goalID},
			Media: notify.Media{OutputIDs: ids, Links: append(notify.ArtifactLinks(msg), links...), Agent: true, Level: level}}
		return notify.Enqueue(ctx, t, tc.ws, note)
	})
	if err != nil {
		return err
	}
	o.reply(tc, "Notification sent.", false)
	return nil
}

// notifyOutputs turns output names into ids. Only outputs of this task, or of another task on
// the same goal, can be attached.
func (o *Orch) notifyOutputs(ctx context.Context, t *core.Tx, tc *toolCtx, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	outs, err := t.Q.ListOutputs(ctx, tc.ws.ID)
	if err != nil {
		return nil, err
	}
	sameGoal := map[uuid.UUID]bool{}
	if tc.task != nil {
		sameGoal[tc.task.ID] = true
		if tc.task.GoalID != nil {
			tasks, err := t.Q.ListGoalTasks(ctx, tc.task.GoalID)
			if err != nil {
				return nil, err
			}
			for _, x := range tasks {
				sameGoal[x.ID] = true
			}
		}
	}
	var ids []uuid.UUID
	var missing []string
	for _, name := range names {
		found := false
		// Newest first: a task's own file wins over a same-named one on the goal.
		for _, pass := range []bool{true, false} {
			for i := len(outs) - 1; i >= 0 && !found; i-- {
				x := outs[i]
				own := tc.task != nil && x.TaskID != nil && *x.TaskID == tc.task.ID
				if x.Name == name && x.TaskID != nil && sameGoal[*x.TaskID] && own == pass {
					ids, found = append(ids, x.ID), true
				}
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, toolError("No output named " + strings.Join(missing, ", ") + " on this task or goal. Save it with submit_output first.")
	}
	return ids, nil
}

// notifyLinks keeps https links only, labelled by host.
func notifyLinks(raw []string) ([]notify.Link, error) {
	if len(raw) > notifyMaxLinks {
		return nil, toolError(fmt.Sprintf("Add at most %d links.", notifyMaxLinks))
	}
	var out []notify.Link
	for _, s := range raw {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(s) > 2000 {
			return nil, toolError("Links must be full https addresses. This one isn’t: " + s)
		}
		out = append(out, notify.Link{Label: "Open " + u.Hostname(), URL: u.String()})
	}
	return out, nil
}
