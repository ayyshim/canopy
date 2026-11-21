package orch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/domain"
	"canopy/hub/internal/store/db"
)

func TestPlanPromptListsStaffSkills(t *testing.T) {
	dev := "developer"
	mgr := db.Member{ID: uuid.New(), Name: "Riley", Kind: "manager"}
	staff := db.Member{ID: uuid.New(), Name: "Dev", Kind: "staff", Skill: &dev, ReportsTo: &mgr.ID}
	plain := db.Member{ID: uuid.New(), Name: "Sam", Kind: "staff", Skill: &dev, ReportsTo: &mgr.ID}
	indep := db.Member{ID: uuid.New(), Name: "Ivy", Kind: "staff", Skill: &dev}
	goSkill := db.WorkspaceSkill{ID: uuid.New(), Name: "Go style", Content: "x"}
	revSkill := db.WorkspaceSkill{ID: uuid.New(), Name: "Review", Content: "y"}
	st := &wsState{members: []db.Member{mgr, staff, plain, indep}, skills: []db.WorkspaceSkill{goSkill, revSkill},
		memberSkills: map[uuid.UUID][]db.WorkspaceSkill{staff.ID: {goSkill, revSkill}, indep.ID: {revSkill}}}

	p := planPrompt(st, mgr, db.Task{}, db.Goal{Title: "Ship it"}, nil)
	for _, want := range []string{"- Dev (developer) — skills: Go style, Review\n", "- Sam (developer)\n", "- Ivy (developer) — skills: Review\n",
		"Skill library", "- Go style\n", "- Review\n"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan prompt missing %q:\n%s", want, p)
		}
	}
	// An empty library adds no section.
	st.skills = nil
	if p := planPrompt(st, mgr, db.Task{}, db.Goal{Title: "Ship it"}, nil); strings.Contains(p, "Skill library") {
		t.Errorf("empty library listed:\n%s", p)
	}
}

func TestSystemPromptInjectsSkills(t *testing.T) {
	dev := "developer"
	mgr := db.Member{ID: uuid.New(), Name: "Riley", Kind: "manager"}
	staff := db.Member{ID: uuid.New(), Name: "Dev", Kind: "staff", Skill: &dev, ReportsTo: &mgr.ID, Instructions: "Be kind."}
	st := &wsState{ws: db.Workspace{Name: "Payments"}, member: map[uuid.UUID]db.Member{mgr.ID: mgr, staff.ID: staff},
		memberSkills: map[uuid.UUID][]db.WorkspaceSkill{staff.ID: {{Name: "Go style", Content: "\nUse errors.Is.\n"}}}}

	p := systemPrompt(st, staff, nil, nil)
	if !strings.Contains(p, "Your skill: developer.") || !strings.Contains(p, "Instructions from your admins:\nBe kind.") ||
		!strings.Contains(p, "\nSkill: Go style\nUse errors.Is.\n") {
		t.Errorf("staff prompt:\n%s", p)
	}
	if strings.Contains(p, "assign_skill") {
		t.Error("staff were offered assign_skill")
	}
	if mp := systemPrompt(st, mgr, nil, nil); !strings.Contains(mp, "assign_skill") || strings.Contains(mp, "Skill: Go style") {
		t.Errorf("manager prompt:\n%s", mp)
	}
}

func TestSystemPromptIncludesGuide(t *testing.T) {
	skill := "technology.backend"
	staff := db.Member{ID: uuid.New(), Name: "Dev", Kind: "staff", Skill: &skill, Instructions: "Be kind."}
	st := &wsState{ws: db.Workspace{Name: "Payments"}, member: map[uuid.UUID]db.Member{staff.ID: staff}}
	p := systemPrompt(st, staff, nil, nil)
	guide := domain.SkillGuide("developer")
	i, j := strings.Index(p, "How you work as Developer:\n"+guide), strings.Index(p, "Instructions from your admins")
	if i < 0 || j < i {
		t.Errorf("guide missing or after admin instructions:\n%s", p)
	}
	none := db.Member{ID: uuid.New(), Name: "Pat", Kind: "staff"}
	if p := systemPrompt(st, none, nil, nil); strings.Contains(p, "How you work as") {
		t.Error("member without a skill got a guide")
	}
}
