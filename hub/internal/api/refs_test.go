package api_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
)

type refsFixture struct {
	e                *env
	owner, guest     *client
	ws               string
	g1, g2           string // g1 is Riley's, g2 belongs to a manager the guest can't see
	guestID          uuid.UUID
	wsID             uuid.UUID
	riley, max, dev  uuid.UUID
	hiddenTask, task db.Task
}

func newRefsFixture(t *testing.T) *refsFixture {
	t.Helper()
	e := newEnv(t)
	owner, me := e.signup("Refs Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	ms := members(d)
	f := &refsFixture{e: e, owner: owner, ws: ws, wsID: uuid.MustParse(ws),
		riley: uuid.MustParse(str(ms["Riley"]["id"])), dev: uuid.MustParse(str(ms["Dev"]["id"]))}
	nm := owner.do("POST", "/api/v1/workspaces/"+ws+"/members", map[string]any{"name": "Max", "kind": "manager", "cli": "claude"})
	f.max = uuid.MustParse(str(nm.Body["member"].(map[string]any)["id"]))

	guestEmail := uniqueEmail("guest")
	r := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": guestEmail, "role": "member",
		"workspace_grants": []any{map[string]any{"workspace_id": ws, "role": "staff", "manager_ids": []string{f.riley.String()}}}})
	token := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	f.guest = e.client()
	acc := f.guest.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "Guest", "password": "a-long-password-1"})
	if acc.Status != 200 {
		t.Fatalf("accept: %d %v", acc.Status, acc.Body)
	}
	f.guestID = uuid.MustParse(userID(e.finishSignIn(f.guest, guestEmail, acc.Body)))

	goal := func(mgr uuid.UUID, title string) string {
		r := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": mgr.String(), "title": title})
		if r.Status != 201 && r.Status != 200 {
			t.Fatalf("goal: %d %v", r.Status, r.Body)
		}
		return str(r.Body["goal"].(map[string]any)["id"])
	}
	f.g1, f.g2 = goal(f.riley, "Speed up checkout"), goal(f.max, "Secret roadmap")
	mk := func(member uuid.UUID, goal, title string) db.Task {
		g := uuid.MustParse(goal)
		tk, err := e.store.CreateTask(context.Background(), db.CreateTaskParams{ID: uuid.New(), WorkspaceID: f.wsID, GoalID: &g, MemberID: &member,
			Kind: "work", Title: title, Status: "todo", DependsOn: []uuid.UUID{}, Steps: []byte("[]"), RequiredDirs: []byte("[]")})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	f.task, f.hiddenTask = mk(f.dev, f.g1, "Cache prices"), mk(f.max, f.g2, "Draft launch plan")
	return f
}

func (f *refsFixture) refsOf(c *client, q string) []string {
	r := c.do("GET", "/api/v1/workspaces/"+f.ws+"/refs?q="+q, nil)
	if r.Status != 200 {
		f.e.t.Fatalf("refs?q=%s: %d %v", q, r.Status, r.Body)
	}
	var out []string
	for _, it := range r.Body["items"].([]any) {
		out = append(out, str(it.(map[string]any)["ref"]))
	}
	sort.Strings(out)
	return out
}

func TestNumbersPerWorkspaceAndType(t *testing.T) {
	f := newRefsFixture(t)
	if f.task.Number != 1 || f.hiddenTask.Number != 2 {
		t.Errorf("task numbers %d, %d", f.task.Number, f.hiddenTask.Number)
	}
	r := f.owner.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/g-2", nil)
	if r.Status != 200 || str(r.Body["type"]) != "goal" || str(r.Body["id"]) != f.g2 {
		t.Errorf("G-2: %d %v", r.Status, r.Body)
	}
	// Notes count on their own.
	n := f.owner.do("POST", "/api/v1/workspaces/"+f.ws+"/notes", map[string]any{"title": "Ideas", "body": "x"})
	if got := n.Body["note"].(map[string]any)["number"]; got != float64(1) {
		t.Errorf("first note number %v", got)
	}
	// A second workspace starts again at 1.
	me := f.owner.do("GET", "/api/v1/me", nil)
	ws2, d2 := f.e.createWorkspace(f.owner, currentOrg(me.Body), setupPayload())
	g := f.owner.do("POST", "/api/v1/workspaces/"+ws2+"/goals", map[string]any{"manager_id": str(members(d2)["Riley"]["id"]), "title": "Other"})
	if got := g.Body["goal"].(map[string]any)["number"]; got != float64(1) {
		t.Errorf("goal number in second workspace %v", got)
	}
	// The same ref in the first workspace is unaffected.
	if r := f.owner.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/G-1", nil); str(r.Body["id"]) != f.g1 {
		t.Errorf("G-1 in first workspace: %v", r.Body)
	}
}

func TestConcurrentInsertsNeverClash(t *testing.T) {
	f := newRefsFixture(t)
	const n = 25
	nums := make([]int32, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			note, err := f.e.store.CreateNote(context.Background(), db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.wsID, Kind: "note", Title: "n"})
			if err != nil {
				t.Error(err)
				return
			}
			nums[i] = note.Number
		})
	}
	wg.Wait()
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	for i, v := range nums {
		if v != int32(i+1) {
			t.Fatalf("numbers %v, want 1..%d", nums, n)
		}
	}
}

func TestPickerAndLookupAreScoped(t *testing.T) {
	f := newRefsFixture(t)
	hiddenNote, err := f.e.store.CreateNote(context.Background(), db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.wsID,
		GoalID: ptr(uuid.MustParse(f.g2)), Kind: "note", Title: "Roadmap secrets"})
	if err != nil {
		t.Fatal(err)
	}
	f.owner.do("POST", "/api/v1/workspaces/"+f.ws+"/notes", map[string]any{"title": "Shared tips", "body": "x"})

	all := f.refsOf(f.owner, "")
	if strings.Join(all, " ") != "G-1 G-2 N-1 N-2 T-1 T-2" {
		t.Errorf("owner picker: %v", all)
	}
	if got := strings.Join(f.refsOf(f.guest, ""), " "); got != "G-1 N-2 T-1" {
		t.Errorf("guest picker: %s", got)
	}
	if got := f.refsOf(f.guest, "secret"); len(got) != 0 {
		t.Errorf("guest found hidden items by title: %v", got)
	}
	if got := f.refsOf(f.guest, "%232"); len(got) != 1 || got[0] != "N-2" {
		t.Errorf("guest #2 should only match the visible note: %v", got)
	}
	if got := f.refsOf(f.owner, "checkout"); len(got) != 1 || got[0] != "G-1" {
		t.Errorf("title search: %v", got)
	}
	if got := f.refsOf(f.owner, "T-2"); len(got) != 1 || got[0] != "T-2" {
		t.Errorf("ref search: %v", got)
	}

	hidden := []string{"G-2", "T-2", "N-" + itoa(hiddenNote.Number), "G-99", "nonsense"}
	for _, ref := range hidden {
		if r := f.guest.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/"+ref, nil); r.Status != 404 {
			t.Errorf("guest GET %s: %d", ref, r.Status)
		}
	}
	if r := f.guest.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/T-1", nil); r.Status != 200 || str(r.Body["type"]) != "task" {
		t.Errorf("guest T-1: %d %v", r.Status, r.Body)
	}

	// Someone from another organization sees nothing.
	stranger, _ := f.e.signup("Elsewhere")
	if r := stranger.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/G-1", nil); r.Status != 404 {
		t.Errorf("stranger lookup: %d", r.Status)
	}
	if r := stranger.do("GET", "/api/v1/workspaces/"+f.ws+"/refs?q=", nil); r.Status != 404 {
		t.Errorf("stranger picker: %d", r.Status)
	}
}

func TestPickerCapsAt20(t *testing.T) {
	f := newRefsFixture(t)
	for range 25 {
		if _, err := f.e.store.CreateNote(context.Background(), db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.wsID, Kind: "note", Title: "bulk"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.refsOf(f.owner, "bulk"); len(got) != 20 {
		t.Errorf("picker returned %d items", len(got))
	}
}

func TestReferencedItemsSkipHiddenOnes(t *testing.T) {
	f := newRefsFixture(t)
	o := orch.New(f.e.srv.Core, &recorder{}, nil)
	ws, err := f.e.store.GetWorkspace(context.Background(), f.wsID)
	if err != nil {
		t.Fatal(err)
	}
	text := "Follow up on G-1, T-2 and `T-1`. Also G-2 and T-1."
	guest := o.ReferencedItems(context.Background(), ws, &f.guestID, text)
	if !strings.Contains(guest, "G-1: Speed up checkout") || !strings.Contains(guest, "T-1: Cache prices") {
		t.Errorf("visible items missing:\n%s", guest)
	}
	if strings.Contains(guest, "Secret roadmap") || strings.Contains(guest, "Draft launch plan") {
		t.Errorf("hidden items expanded for the guest:\n%s", guest)
	}
	ownerID := uuid.MustParse(userID(f.owner.do("GET", "/api/v1/me", nil).Body))
	if out := o.ReferencedItems(context.Background(), ws, &ownerID, text); !strings.Contains(out, "Secret roadmap") || !strings.Contains(out, "Draft launch plan") {
		t.Errorf("owner should see all:\n%s", out)
	}
	if o.ReferencedItems(context.Background(), ws, &f.guestID, "no refs here") != "" || o.ReferencedItems(context.Background(), ws, nil, text) != "" {
		t.Errorf("expected an empty block")
	}
	// At most 10 items.
	var many []string
	for i := 1; i <= 15; i++ {
		many = append(many, "N-"+itoa(int32(i)))
	}
	for range 15 {
		_, _ = f.e.store.CreateNote(context.Background(), db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.wsID, Kind: "note", Title: "n", Body: strings.Repeat("é", 3000)})
	}
	out := o.ReferencedItems(context.Background(), ws, &ownerID, strings.Join(many, " "))
	if n := strings.Count(out, "\n- "); n != 10 {
		t.Errorf("expanded %d items, want 10", n)
	}
	if !strings.Contains(out, "…") || strings.Contains(out, strings.Repeat("é", 1600)) {
		t.Errorf("long bodies should be trimmed to about 1500 chars")
	}
}

// The migration numbers old rows per workspace by created_at, then id.
func TestBackfillOrder(t *testing.T) {
	ctx := context.Background()
	dbURL, drop, err := createDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer drop()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	raw, err := os.ReadFile(filepath.Join("..", "store", "migrations", "00024_item_numbers.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up, down, _ := strings.Cut(string(raw), "-- +goose Down")
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(down)
	exec(`INSERT INTO users (id, name, email, password_hash, avatar_color) VALUES ('00000000-0000-0000-0000-0000000000a1', 'U', 'u@x.test', 'x', 'red')`)
	exec(`INSERT INTO organizations (id, name, slug, size, owner_user_id, default_session_limit, default_weekly_limit)
	      VALUES ('00000000-0000-0000-0000-0000000000b1', 'O', 'o', 'just_me', '00000000-0000-0000-0000-0000000000a1', 1, 1)`)
	for _, w := range []string{"c1", "c2"} {
		exec(`INSERT INTO workspaces (id, org_id, name) VALUES ('00000000-0000-0000-0000-0000000000` + w + `', '00000000-0000-0000-0000-0000000000b1', 'W')`)
	}
	// Created in the opposite order of their ids, plus a tie on created_at.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insert := func(ws, id string, at time.Time) {
		exec(`INSERT INTO goals (id, workspace_id, title, color, created_at) VALUES ('00000000-0000-0000-0000-00000000` + id + `', '00000000-0000-0000-0000-0000000000` + ws + `', 'g` + id + `', 'red', '` + at.Format(time.RFC3339) + `')`)
	}
	insert("c1", "0003", base)
	insert("c1", "0002", base.Add(time.Hour))
	insert("c1", "0001", base.Add(time.Hour)) // tie with 0002: id decides
	insert("c2", "0009", base.Add(-time.Hour))
	exec(up)

	got := map[string]int32{}
	rows, err := conn.Query(ctx, `SELECT title, number FROM goals`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var title string
		var n int32
		_ = rows.Scan(&title, &n)
		got[title] = n
	}
	rows.Close()
	want := map[string]int32{"g0003": 1, "g0001": 2, "g0002": 3, "g0009": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s numbered %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	// New rows continue after the backfill.
	var next int32
	if err := conn.QueryRow(ctx, `SELECT next_item_number('00000000-0000-0000-0000-0000000000c1', 'goal')`).Scan(&next); err != nil || next != 4 {
		t.Errorf("next number %d, %v", next, err)
	}
}

func itoa(n int32) string { return strconv.Itoa(int(n)) }
