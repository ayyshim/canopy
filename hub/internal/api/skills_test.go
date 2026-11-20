package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/skills"
)

// redirectTransport sends every request to the stub, whatever host it asked for.
type redirectTransport struct{ to *url.URL }

func (r redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

// stubGitHub serves raw markdown and counts requests.
func stubGitHub(t *testing.T, e *env) *int {
	t.Helper()
	hits := new(int)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch r.URL.Path {
		case "/acme/skills/main/review.md":
			io.WriteString(w, "# Reviewing\nCheck error handling first.")
		case "/acme/skills/main/huge.md":
			io.WriteString(w, strings.Repeat("a", skills.MaxBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stub.Close)
	u, _ := url.Parse(stub.URL)
	e.srv.Skills = &skills.Fetcher{Client: &http.Client{Transport: redirectTransport{u}}}
	return hits
}

func TestSkillLibraryCRUD(t *testing.T) {
	e := newEnv(t)
	hits := stubGitHub(t, e)
	owner, me := e.signup("Skills Co")
	org := currentOrg(me)
	ws, _ := e.createWorkspace(owner, org, setupPayload())
	base := "/api/v1/workspaces/" + ws + "/skills"

	// Markdown skill.
	r := owner.do("POST", base, map[string]any{"name": " House style ", "source_kind": "md", "content": "Use sentence case."})
	if r.Status != http.StatusCreated {
		t.Fatalf("create md skill: %d %v", r.Status, r.Body)
	}
	md := r.Body["skill"].(map[string]any)
	if md["name"] != "House style" || md["source_kind"] != "md" || md["content"] != "Use sentence case." {
		t.Errorf("md skill: %v", md)
	}

	// GitHub skill: a blob link is fetched from raw and stored.
	r = owner.do("POST", base, map[string]any{"name": "Review", "source_kind": "github", "source_url": "https://github.com/acme/skills/blob/main/review.md"})
	if r.Status != http.StatusCreated {
		t.Fatalf("create github skill: %d %v", r.Status, r.Body)
	}
	gh := r.Body["skill"].(map[string]any)
	if gh["content"] != "# Reviewing\nCheck error handling first." || *hits != 1 {
		t.Errorf("github skill: %v (hits %d)", gh, *hits)
	}

	// Clear errors.
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
		want  string
	}{
		"no name":      {map[string]any{"source_kind": "md", "content": "x"}, "name", "name"},
		"no content":   {map[string]any{"name": "A", "source_kind": "md"}, "content", "markdown"},
		"bad kind":     {map[string]any{"name": "A", "source_kind": "zip"}, "source_kind", "GitHub"},
		"not github":   {map[string]any{"name": "A", "source_kind": "github", "source_url": "https://example.com/a.md"}, "source_url", "GitHub"},
		"missing file": {map[string]any{"name": "A", "source_kind": "github", "source_url": "https://github.com/acme/skills/blob/main/nope.md"}, "source_url", "doesn’t exist"},
		"too big":      {map[string]any{"name": "A", "source_kind": "github", "source_url": "https://github.com/acme/skills/blob/main/huge.md"}, "source_url", "too long"},
	} {
		r := owner.do("POST", base, tc.body)
		if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.field(tc.field), tc.want) {
			t.Errorf("%s: %d field %q = %q (%v)", name, r.Status, tc.field, r.field(tc.field), r.Body)
		}
	}
	if r := owner.do("POST", base, map[string]any{"name": "Review", "source_kind": "md", "content": "dup"}); r.Status != http.StatusConflict {
		t.Errorf("duplicate name: %d %v", r.Status, r.Body)
	}

	// List, edit, delete.
	list := owner.do("GET", base, nil).Body["skills"].([]any)
	if len(list) != 2 {
		t.Fatalf("list: %v", list)
	}
	mdID, ghID := str(md["id"]), str(gh["id"])
	r = owner.do("PATCH", "/api/v1/skills/"+mdID, map[string]any{"content": "Use sentence case, always.", "name": "Style"})
	if r.Status != 200 || r.Body["skill"].(map[string]any)["content"] != "Use sentence case, always." || r.Body["skill"].(map[string]any)["name"] != "Style" {
		t.Errorf("patch md: %d %v", r.Status, r.Body)
	}
	before := *hits
	owner.do("PATCH", "/api/v1/skills/"+ghID, map[string]any{"name": "Code review"})
	if *hits != before {
		t.Error("renaming a github skill fetched it again")
	}
	owner.do("PATCH", "/api/v1/skills/"+ghID, map[string]any{"refresh": true})
	if *hits != before+1 {
		t.Error("refresh didn’t fetch")
	}
	// Switching a skill to markdown needs content.
	if r := owner.do("PATCH", "/api/v1/skills/"+ghID, map[string]any{"source_kind": "md"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("github → md without content: %d", r.Status)
	}
	r = owner.do("PATCH", "/api/v1/skills/"+ghID, map[string]any{"source_kind": "md", "content": "Own words."})
	if sk := r.Body["skill"].(map[string]any); r.Status != 200 || sk["source_url"] != "" || sk["content"] != "Own words." {
		t.Errorf("github → md: %d %v", r.Status, r.Body)
	}
	if r := owner.do("DELETE", "/api/v1/skills/"+mdID, nil); r.Status != http.StatusNoContent {
		t.Errorf("delete: %d", r.Status)
	}
	if r := owner.do("DELETE", "/api/v1/skills/"+mdID, nil); r.Status != http.StatusNotFound {
		t.Errorf("delete twice: %d", r.Status)
	}
}

func TestSkillPermissions(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Skill Perms Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff"})
	stranger, _ := e.signup("Outsiders")
	dev := str(members(d)["Dev"]["id"])
	base := "/api/v1/workspaces/" + ws + "/skills"

	r := owner.do("POST", base, map[string]any{"name": "Style", "source_kind": "md", "content": "Be brief."})
	sk := str(r.Body["skill"].(map[string]any)["id"])
	attach := "/api/v1/workspaces/" + ws + "/members/" + dev + "/skills/" + sk

	// Anyone in the workspace reads the library; only admins change it.
	if r := bob.do("GET", base, nil); r.Status != 200 || len(r.Body["skills"].([]any)) != 1 {
		t.Errorf("member lists skills: %d %v", r.Status, r.Body)
	}
	if r := bob.do("POST", base, map[string]any{"name": "Mine", "source_kind": "md", "content": "x"}); r.Status != http.StatusForbidden {
		t.Errorf("member created a skill: %d", r.Status)
	}
	if r := bob.do("PATCH", "/api/v1/skills/"+sk, map[string]any{"content": "x"}); r.Status != http.StatusForbidden {
		t.Errorf("member edited a skill: %d", r.Status)
	}
	if r := bob.do("DELETE", "/api/v1/skills/"+sk, nil); r.Status != http.StatusForbidden {
		t.Errorf("member deleted a skill: %d", r.Status)
	}
	// Bob can’t even see Dev (not on his manager’s team), so he gets a 404 rather than a 403.
	if r := bob.do("PUT", attach, nil); r.Status != http.StatusNotFound && r.Status != http.StatusForbidden {
		t.Errorf("member attached a skill: %d", r.Status)
	}
	// Outsiders see nothing.
	if r := stranger.do("GET", base, nil); r.Status != http.StatusNotFound {
		t.Errorf("stranger listed skills: %d", r.Status)
	}
	if r := stranger.do("PATCH", "/api/v1/skills/"+sk, map[string]any{"content": "x"}); r.Status != http.StatusNotFound {
		t.Errorf("stranger edited a skill: %d", r.Status)
	}
	if r := stranger.do("PUT", attach, nil); r.Status != http.StatusNotFound {
		t.Errorf("stranger attached a skill: %d", r.Status)
	}

	// Admin attaches and detaches; a skill from another workspace is refused.
	if r := owner.do("PUT", attach, nil); r.Status != 200 || len(r.Body["skills"].([]any)) != 1 {
		t.Fatalf("attach: %d %v", r.Status, r.Body)
	}
	if r := owner.do("PUT", attach, nil); r.Status != 200 || len(r.Body["skills"].([]any)) != 1 {
		t.Errorf("attach twice: %d %v", r.Status, r.Body)
	}
	if got := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+dev+"/skills", nil).Body["skills"].([]any); len(got) != 1 {
		t.Errorf("member skills: %v", got)
	}
	other, _ := e.createWorkspace(owner, org, setupPayload())
	r = owner.do("POST", "/api/v1/workspaces/"+other+"/skills", map[string]any{"name": "Elsewhere", "source_kind": "md", "content": "x"})
	foreign := str(r.Body["skill"].(map[string]any)["id"])
	if r := owner.do("PUT", "/api/v1/workspaces/"+ws+"/members/"+dev+"/skills/"+foreign, nil); r.Status != http.StatusNotFound {
		t.Errorf("attached another workspace’s skill: %d", r.Status)
	}
	if r := owner.do("DELETE", attach, nil); r.Status != 200 || len(r.Body["skills"].([]any)) != 0 {
		t.Errorf("detach: %d %v", r.Status, r.Body)
	}
	// Deleting a skill removes it from members.
	owner.do("PUT", attach, nil)
	owner.do("DELETE", "/api/v1/skills/"+sk, nil)
	if got := owner.do("GET", "/api/v1/workspaces/"+ws+"/members/"+dev+"/skills", nil).Body["skills"].([]any); len(got) != 0 {
		t.Errorf("deleted skill still on member: %v", got)
	}
}

// The manager's assign_skill tool, and library skills reaching the staff member's prompt.
func TestAssignSkillAndPrompt(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Assign Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	m := members(d)
	riley, dev := uuid.MustParse(str(m["Riley"]["id"])), uuid.MustParse(str(m["Dev"]["id"]))
	o := orch.New(e.srv.Core, offline{}, nil)
	ctx := context.Background()

	owner.do("POST", "/api/v1/workspaces/"+ws+"/skills", map[string]any{"name": "Go style", "source_kind": "md", "content": "  Prefer the standard library.\n"})

	before, err := o.MemberPrompt(ctx, dev)
	if err != nil || strings.Contains(before, "standard library") {
		t.Fatalf("prompt before: %v %q", err, before)
	}
	msg, err := o.AssignSkill(ctx, riley, "dev", "GO STYLE", false)
	if err != nil || !strings.Contains(msg, "Go style") {
		t.Fatalf("assign: %v %q", err, msg)
	}
	prompt, err := o.MemberPrompt(ctx, dev)
	if err != nil || !strings.Contains(prompt, "Skill: Go style\nPrefer the standard library.\n") {
		t.Errorf("prompt after: %v %q", err, prompt)
	}
	// The role is kept alongside library skills.
	if !strings.Contains(prompt, "Your skill: developer.") {
		t.Errorf("role missing from prompt: %q", prompt)
	}

	// Refusals read as messages for the agent.
	if _, err := o.AssignSkill(ctx, riley, "Ivy", "Go style", false); err == nil || !strings.Contains(err.Error(), "isn’t one of your staff") {
		t.Errorf("independent staff: %v", err)
	}
	if _, err := o.AssignSkill(ctx, riley, "Dev", "Nope", false); err == nil || !strings.Contains(err.Error(), "Go style") {
		t.Errorf("unknown skill should list the library: %v", err)
	}
	if _, err := o.AssignSkill(ctx, dev, "Dev", "Go style", false); err == nil || !strings.Contains(err.Error(), "Only managers") {
		t.Errorf("staff assigning: %v", err)
	}

	// Detaching removes it from the prompt.
	if _, err := o.AssignSkill(ctx, riley, "Dev", "Go style", true); err != nil {
		t.Fatal(err)
	}
	if after, _ := o.MemberPrompt(ctx, dev); strings.Contains(after, "standard library") {
		t.Errorf("prompt after detach: %q", after)
	}
}

// Setup can create the library and give members skills in one go; GitHub links are previewed first.
func TestSetupWithSkills(t *testing.T) {
	e := newEnv(t)
	stubGitHub(t, e)
	owner, me := e.signup("Setup Skills Co")
	org := currentOrg(me)

	r := owner.do("POST", "/api/v1/orgs/"+org+"/skills/preview", map[string]any{"source_url": "https://github.com/acme/skills/blob/main/review.md"})
	if r.Status != 200 || r.Body["name"] != "Review" || !strings.Contains(str(r.Body["content"]), "error handling") {
		t.Fatalf("preview: %d %v", r.Status, r.Body)
	}
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/skills/preview", map[string]any{"source_url": "https://example.com/x.md"}); r.Status != http.StatusUnprocessableEntity || r.field("source_url") == "" {
		t.Errorf("bad preview link: %d %v", r.Status, r.Body)
	}

	p := setupPayload()
	p["skills"] = []any{
		map[string]any{"key": "k1", "name": "Review", "source_kind": "github", "source_url": "https://github.com/acme/skills/blob/main/review.md", "content": r.Body["content"]},
		map[string]any{"key": "k2", "name": "Style", "source_kind": "md", "content": "Be brief."},
	}
	p["members"].([]any)[1].(map[string]any)["skill_keys"] = []any{"k1", "k2"}
	bad := setupPayload()
	bad["skills"] = []any{map[string]any{"key": "k1", "name": "A", "source_kind": "md", "content": " "}}
	if r := owner.do("POST", "/api/v1/orgs/"+org+"/workspaces", bad); r.Status != http.StatusUnprocessableEntity || r.field("skills.0.content") == "" {
		t.Errorf("empty skill: %d %v", r.Status, r.Body)
	}
	ws, _ := e.createWorkspace(owner, org, p)
	board := owner.do("GET", "/api/v1/workspaces/"+ws+"/board", nil).Body
	if len(board["skills"].([]any)) != 2 {
		t.Fatalf("board skills: %v", board["skills"])
	}
	ms := board["member_skills"].(map[string]any)
	if len(ms) != 1 {
		t.Errorf("member_skills: %v", ms)
	}
	for _, ids := range ms {
		if len(ids.([]any)) != 2 {
			t.Errorf("Dev's skills: %v", ids)
		}
	}
}
