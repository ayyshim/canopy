package orch

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

func isolatedState() (*wsState, db.Member, db.Member, db.Member) {
	skill := "developer"
	mgr := db.Member{ID: uuid.New(), Name: "Maya", Kind: "manager"}
	open := db.Member{ID: uuid.New(), Name: "Open", Kind: "staff", Skill: &skill}
	solo := db.Member{ID: uuid.New(), Name: "Solo", Kind: "staff", Skill: &skill, Isolated: true}
	st := &wsState{members: []db.Member{mgr, solo, open}, member: map[uuid.UUID]db.Member{mgr.ID: mgr, solo.ID: solo, open.ID: open}}
	return st, mgr, open, solo
}

func TestAssignSkipsIsolated(t *testing.T) {
	st, mgr, open, solo := isolatedState()
	load := map[uuid.UUID]int{open.ID: 5}
	id, borrowed := assign(st, mgr, "developer", load)
	if id != open.ID || !borrowed {
		t.Errorf("picked %v borrowed=%v, want the open staff", id, borrowed)
	}
	st.members = []db.Member{mgr, solo}
	if id, _ := assign(st, mgr, "developer", load); id != mgr.ID {
		t.Errorf("isolated staff was assigned")
	}
}

func TestPromptsHideIsolated(t *testing.T) {
	st, mgr, _, _ := isolatedState()
	prompt := planPrompt(st, mgr, db.Task{}, db.Goal{Title: "G"}, nil)
	if strings.Contains(prompt, "Solo") || !strings.Contains(prompt, "Open") {
		t.Errorf("borrow list wrong:\n%s", prompt)
	}
	if strings.Contains(boardText(st, time.Now(), false), "Solo") {
		t.Errorf("board lists isolated staff")
	}
}

func TestAssignMatchesSubcategoryAndBaseRole(t *testing.T) {
	seo, content, dev := "marketing.seo", "marketing.content", "developer"
	mgr := db.Member{ID: uuid.New(), Name: "Maya", Kind: "manager"}
	mk := func(name string, skill *string) db.Member {
		return db.Member{ID: uuid.New(), Name: name, Kind: "staff", Skill: skill, ReportsTo: &mgr.ID}
	}
	seoer, writer, coder := mk("Seo", &seo), mk("Writer", &content), mk("Coder", &dev)
	st := &wsState{members: []db.Member{mgr, coder, writer, seoer}}
	load := map[uuid.UUID]int{seoer.ID: 9}
	if id, _ := assign(st, mgr, "marketing.seo", load); id != seoer.ID {
		t.Errorf("exact key should win even when busier")
	}
	if id, _ := assign(st, mgr, "writer", load); id != writer.ID {
		t.Errorf("base role writer should reach the content marketer")
	}
	if id, _ := assign(st, mgr, "marketing.email", load); id != coder.ID {
		// no match for an unmatched key: falls back to any own staff (least busy, first listed)
		t.Errorf("unmatched key should fall back to own staff, got %v", id)
	}
	if errs := validatePlan([]planTask{{Key: "a", Title: "A", Skill: "marketing.seo"}}, nil); len(errs) != 0 {
		t.Errorf("subcategory key rejected: %v", errs)
	}
	if errs := validatePlan([]planTask{{Key: "a", Title: "A", Skill: "wizard"}}, nil); len(errs) == 0 {
		t.Error("unknown skill accepted")
	}
}
