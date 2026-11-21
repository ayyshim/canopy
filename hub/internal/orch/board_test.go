package orch

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

func TestBoardText(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { x := now.Add(-time.Duration(m) * time.Minute); return &x }
	mgr, dev := uuid.New(), uuid.New()
	g1, g2 := uuid.New(), uuid.New()
	dev1 := "developer"
	t1 := db.Task{ID: uuid.New(), GoalID: &g1, MemberID: &dev, Number: 1, Title: "Build API", Status: "done", StartedAt: at(90), FinishedAt: at(25), CreatedAt: *at(95)}
	t2 := db.Task{ID: uuid.New(), GoalID: &g1, MemberID: &dev, Number: 2, Title: "Build UI", Status: "doing", Progress: 40, StartedAt: at(20), CreatedAt: *at(94)}
	t3 := db.Task{ID: uuid.New(), GoalID: &g1, MemberID: &dev, Number: 3, Title: "Docs", Status: "todo", DependsOn: []uuid.UUID{t2.ID}, CreatedAt: *at(93)}
	st := &wsState{ws: db.Workspace{Name: "Shop"},
		members: []db.Member{{ID: mgr, Name: "Maya", Kind: "manager"}, {ID: dev, Name: "Dev", Kind: "staff", Skill: &dev1, ReportsTo: &mgr}},
		goals: []db.Goal{{ID: g1, Number: 1, Title: "Export", Status: "active", ManagerID: &mgr, CreatedAt: *at(100)},
			{ID: g2, Number: 2, Title: "Old one", Status: "done", ManagerID: &mgr, CreatedAt: *at(500)}},
		tasks: []db.Task{t1, t2, t3}}
	st.member = map[uuid.UUID]db.Member{mgr: st.members[0], dev: st.members[1]}
	st.task = map[uuid.UUID]db.Task{t1.ID: t1, t2.ID: t2, t3.ID: t3}

	out := boardText(st, now, false)
	for _, want := range []string{
		"- Dev (developer, reports to Maya): on T-2 Build UI",
		"## G-1 Export [active], manager Maya, 1 of 3 tasks done, started 1h 40m ago",
		"- [done] T-1 Build API, Dev, took 1h 5m",
		"- [doing] T-2 Build UI, Dev, 40% done, started 20m ago",
		"- [todo] T-3 Docs, Dev, waits on T-2 Build UI",
		"(1 finished goals left out",
		"Pace: 1 finished tasks took 1h 5m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Old one") {
		t.Errorf("done goal shown without include_done")
	}
	if !strings.Contains(boardText(st, now, true), "## G-2 Old one [done]") {
		t.Errorf("include_done hides done goals")
	}
}
