package orch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

func TestTriagePrompt(t *testing.T) {
	dev := "developer"
	mgr := db.Member{ID: uuid.New(), Name: "Riley", Kind: "manager"}
	staff := db.Member{ID: uuid.New(), Name: "Dev", Kind: "staff", Skill: &dev, ReportsTo: &mgr.ID}
	g := db.Goal{ID: uuid.New(), Title: "CSV export"}
	done := db.Task{ID: uuid.New(), GoalID: &g.ID, MemberID: &staff.ID, Title: "Write exporter", Status: "done", Summary: "Added export.go"}
	req := db.Task{ID: uuid.New(), GoalID: &g.ID, MemberID: &mgr.ID, Kind: "triage", Title: "Also add JSON", Description: "Please support JSON too"}
	st := &wsState{members: []db.Member{mgr, staff}, member: map[uuid.UUID]db.Member{mgr.ID: mgr, staff.ID: staff}, tasks: []db.Task{done, req}}
	notes := []db.Note{{GoalID: &g.ID, Kind: "plan", Title: "Plan: CSV export"}}

	p := triagePrompt(st, mgr, req, g, notes)
	for _, want := range []string{"change request or note from a person", "Please support JSON too", "Goal: CSV export",
		"Plan: CSV export", "- Write exporter (done, Dev): Added export.go", "assign_tasks", "Do NOT edit code", "- Dev (developer)", "call complete_task"} {
		if !strings.Contains(p, want) {
			t.Errorf("triage prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "Also add JSON (") {
		t.Errorf("triage task listed itself:\n%s", p)
	}
	sys := systemPrompt(&wsState{member: st.member}, mgr, nil, nil)
	if !strings.Contains(sys, "Never do their work yourself") {
		t.Errorf("manager role text missing triage rule")
	}
}
