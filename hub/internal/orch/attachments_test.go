package orch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

// Agents on a goal share its files and the files on each of its tasks; other goals stay apart.
func TestTaskAttachmentsAreSharedOnTheGoal(t *testing.T) {
	goal, other := uuid.New(), uuid.New()
	mine := db.Task{ID: uuid.New(), GoalID: &goal}
	sibling := db.Task{ID: uuid.New(), GoalID: &goal}
	elsewhere := db.Task{ID: uuid.New(), GoalID: &other}
	st := &wsState{task: map[uuid.UUID]db.Task{mine.ID: mine, sibling.ID: sibling, elsewhere.ID: elsewhere}, attachments: []db.Attachment{
		{Name: "brief.pdf", GoalID: &goal},
		{Name: "data.csv", TaskID: &sibling.ID},
		{Name: "data.csv", TaskID: &mine.ID},
		{Name: "secret.txt", TaskID: &elsewhere.ID},
		{Name: "other.pdf", GoalID: &other},
	}}
	got := fileNames(taskAttachments(st, mine))
	if strings.Join(got, "|") != "brief.pdf|data.csv|data (2).csv" {
		t.Errorf("files: %v", got)
	}
	if line := attachmentsLine(taskAttachments(st, mine)); !strings.Contains(line, "get_attachments") {
		t.Errorf("line: %q", line)
	}
	if line := attachmentsLine(nil); line != "" {
		t.Errorf("empty line: %q", line)
	}
}
