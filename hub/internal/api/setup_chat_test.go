package api

import "testing"

func TestParseSetupChat(t *testing.T) {
	skills := map[string]bool{"s1": true}
	out := parseSetupChat("Sure!\n{\"reply\":\"Here you go\",\"team\":{\"members\":["+
		`{"key":"m1","name":"Atlas","kind":"manager","skill":"developer","reports_to_key":"x","instructions":"Lead","skill_keys":[]},`+
		`{"key":"m2","name":"Pike","kind":"staff","skill":"wizard","reports_to_key":"m1","instructions":"Build","skill_keys":["s1","nope"]},`+
		`{"key":"m3","name":"","kind":"staff"},`+
		`{"key":"m4","name":"Ivy","kind":"staff","skill":"writer","reports_to_key":"ghost"}]}}`, skills)
	if out.Reply != "Here you go" || out.Team == nil || len(out.Team.Members) != 3 {
		t.Fatalf("unexpected: %+v", out)
	}
	m := out.Team.Members
	if m[0].Skill != "" || m[0].ReportsToKey != "" {
		t.Errorf("manager not cleaned: %+v", m[0])
	}
	if m[1].Skill != "developer" || m[1].ReportsToKey != "m1" || len(m[1].SkillKeys) != 1 {
		t.Errorf("staff not cleaned: %+v", m[1])
	}
	if m[0].SkillKeys == nil {
		t.Error("skill_keys must be [] not null")
	}
	if m[2].ReportsToKey != "" {
		t.Errorf("unknown manager kept: %+v", m[2])
	}
}

func TestParseSetupChatPlainText(t *testing.T) {
	if out := parseSetupChat("What kind of work do you do?", nil); out.Team != nil || out.Reply == "" {
		t.Fatalf("unexpected: %+v", out)
	}
	if out := parseSetupChat(`{"reply":"x","team":{"members":[{"key":"a","name":"Bo","kind":"staff"}]}}`, nil); out.Team != nil {
		t.Fatalf("team without manager should be dropped: %+v", out)
	}
}
