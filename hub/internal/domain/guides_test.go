package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestParseGuides(t *testing.T) {
	got := parseGuides("intro\n## a\n- one\n- two\n\n## b.c\n- three\n")
	if len(got) != 2 || got["a"] != "- one\n- two" || got["b.c"] != "- three" {
		t.Errorf("parsed %q", got)
	}
}

func TestSkillGuide(t *testing.T) {
	old := guides
	defer func() { guides = old }()
	guides = map[string]string{"developer": "base guide", "technology.backend": "backend guide"}
	if SkillGuide("developer") != "base guide" || SkillGuide("technology.backend") != "backend guide" {
		t.Error("known keys lost their guide")
	}
	if SkillGuide("nope") != "" {
		t.Error("unknown key returned a guide")
	}
}

func TestBaseGuidesEmbedded(t *testing.T) {
	for _, k := range BaseRoles {
		if SkillGuide(k) == "" {
			t.Errorf("no guide for base role %s", k)
		}
	}
}

func TestGuidesMatchCatalog(t *testing.T) {
	var missing, unknown []string
	for _, k := range SkillKeys() {
		if SkillGuide(k) == "" {
			missing = append(missing, k)
		}
	}
	for _, k := range GuideKeys() {
		if !ValidSkill(k) {
			unknown = append(unknown, k)
		}
	}
	slices.Sort(missing)
	slices.Sort(unknown)
	if len(missing) > 0 {
		t.Errorf("catalog keys without a guide: %s", strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		t.Errorf("guides with an unknown key: %s", strings.Join(unknown, ", "))
	}
}
