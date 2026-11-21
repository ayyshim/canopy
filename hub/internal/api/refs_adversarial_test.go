package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/refs"
)

// A ref names an item in the workspace being asked about, never another one.
func TestRefsStayInsideTheirWorkspace(t *testing.T) {
	f := newStickyFixture(t)
	me := f.owner.do("GET", "/api/v1/me", nil)
	ws2, d2 := f.e.createWorkspace(f.owner, currentOrg(me.Body), setupPayload())
	riley2 := str(members(d2)["Riley"]["id"])
	for range 5 {
		f.owner.do("POST", "/api/v1/workspaces/"+ws2+"/goals", map[string]any{"manager_id": riley2, "title": "Other workspace secret"})
	}
	if r := f.owner.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/G-5", nil); r.Status != 404 {
		t.Errorf("G-5 exists only in the second workspace, got %d", r.Status)
	}
	if got := f.refsOf(f.owner, "Other"); len(got) != 0 {
		t.Errorf("picker leaked the second workspace: %v", got)
	}
	r := f.owner.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/G-1", nil)
	if str(r.Body["id"]) != f.g1 {
		t.Errorf("G-1 resolved to %v, want the first workspace's goal %s", r.Body["id"], f.g1)
	}

	// Expansion is scoped the same way, and a person outside the organization gets nothing.
	o := orch.New(f.e.srv.Core, &recorder{}, nil)
	ws, _ := f.e.store.GetWorkspace(context.Background(), f.wsID)
	if out := o.ReferencedItems(context.Background(), ws, &f.ownerID, "see G-5"); strings.Contains(out, "Other workspace secret") {
		t.Errorf("expansion leaked the second workspace: %s", out)
	}
	stranger, sme := f.e.signup("Elsewhere Co")
	_ = stranger
	sid := uuid.MustParse(userID(sme))
	if out := o.ReferencedItems(context.Background(), ws, &sid, "see G-1 and T-1 and N-1"); out != "" {
		t.Errorf("a stranger got an expansion: %s", out)
	}
}

// Private notes are left out of expansion for everyone but the author.
func TestExpansionSkipsOtherPeoplesPrivateNotes(t *testing.T) {
	f := newStickyFixture(t)
	n := f.create(f.guest, map[string]any{"title": "Guest plan", "body": "guest-only words", "visibility": "private"})
	ref := "N-" + itoa(int32(n["number"].(float64)))
	o := orch.New(f.e.srv.Core, &recorder{}, nil)
	ws, _ := f.e.store.GetWorkspace(context.Background(), f.wsID)
	ctx := context.Background()
	if out := o.ReferencedItems(ctx, ws, &f.ownerID, "read "+ref); out != "" {
		t.Errorf("admin got the guest's private note: %s", out)
	}
	if out := o.ReferencedItems(ctx, ws, &f.guestID, "read "+ref); !strings.Contains(out, "guest-only words") {
		t.Errorf("author should get it: %q", out)
	}
	sc, _ := refs.ViewerScope(ctx, f.e.store.Queries, ws, f.ownerID)
	if _, ok, _ := (refs.Resolver{Q: f.e.store.Queries}).One(ctx, f.wsID, sc, refs.Ref{Kind: refs.Note, Number: int32(n["number"].(float64))}); ok {
		t.Errorf("admin scope resolved the private note")
	}
}

// Huge inputs stay cheap and bounded.
func TestHugeInputsStayBounded(t *testing.T) {
	f := newStickyFixture(t)
	o := orch.New(f.e.srv.Core, &recorder{}, nil)
	ws, _ := f.e.store.GetWorkspace(context.Background(), f.wsID)

	var b strings.Builder
	for i := 0; i < 200000; i++ {
		b.WriteString("T-1 G-1 N-1 ")
	}
	start := time.Now()
	out := o.ReferencedItems(context.Background(), ws, &f.ownerID, b.String())
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("expanding 2.4 MB of refs took %v", d)
	}
	if n := strings.Count(out, "\n- "); n > 10 {
		t.Errorf("expanded %d items", n)
	}

	body := strings.Repeat("x", 6<<20)
	r := f.owner.do("POST", f.base, map[string]any{"title": "Big", "body": body})
	t.Logf("6 MB note body: status %d", r.Status)
	if r.Status != 201 && r.Status != 400 && r.Status != 413 {
		t.Errorf("unexpected status %d for a 6 MB body", r.Status)
	}
	for name, board := range map[string]any{"nan-ish": map[string]any{"x": 1e12, "y": 0}, "string": map[string]any{"x": "a", "y": 1}} {
		if r := f.owner.do("POST", f.base, map[string]any{"title": "Pin " + name, "board": board}); r.Status != 400 && r.Status != 422 {
			t.Errorf("board %s accepted: %d", name, r.Status)
		}
	}
}
