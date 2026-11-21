package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

type stickyFixture struct {
	*refsFixture
	ownerID uuid.UUID
	base    string
}

func newStickyFixture(t *testing.T) *stickyFixture {
	f := newRefsFixture(t)
	me := f.owner.do("GET", "/api/v1/me", nil)
	return &stickyFixture{refsFixture: f, ownerID: uuid.MustParse(userID(me.Body)), base: "/api/v1/workspaces/" + f.ws + "/notes"}
}

func (f *stickyFixture) create(c *client, body map[string]any) map[string]any {
	f.e.t.Helper()
	r := c.do("POST", f.base, body)
	if r.Status != 201 {
		f.e.t.Fatalf("create note: %d %v", r.Status, r.Body)
	}
	return r.Body["note"].(map[string]any)
}

func (f *stickyFixture) listIDs(c *client) []string {
	r := c.do("GET", f.base, nil)
	var out []string
	for _, n := range r.Body["notes"].([]any) {
		out = append(out, str(n.(map[string]any)["id"]))
	}
	return out
}

func has(ids []string, id string) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// socket is a board websocket that collects what it receives.
type socket struct {
	conn *websocket.Conn
	t    *testing.T
}

type wsMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (f *stickyFixture) dial(c *client) (*socket, wsMsg) {
	f.e.t.Helper()
	url := "ws" + strings.TrimPrefix(f.e.ts.URL, "http") + "/ws/workspaces/" + f.ws
	conn, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: http.Header{"X-Forwarded-For": {c.ip}}})
	if err != nil {
		f.e.t.Fatalf("dial: %v", err)
	}
	f.e.t.Cleanup(func() { conn.CloseNow() })
	s := &socket{conn: conn, t: f.e.t}
	snap := s.next(5 * time.Second)
	if snap.Type != "snapshot" {
		f.e.t.Fatalf("first message %q", snap.Type)
	}
	return s, snap
}

// next returns the next note or snapshot message, skipping presence noise; Type is empty on timeout.
func (s *socket) next(wait time.Duration) wsMsg {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			return wsMsg{}
		}
		var m wsMsg
		_ = json.Unmarshal(data, &m)
		if m.Type == "snapshot" || strings.HasPrefix(m.Type, "note.") {
			return m
		}
	}
}

func snapshotNoteIDs(t *testing.T, snap wsMsg) []string {
	var d struct {
		Board struct {
			Notes []struct {
				ID string `json:"id"`
			} `json:"notes"`
		} `json:"board"`
	}
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range d.Board.Notes {
		out = append(out, n.ID)
	}
	return out
}

func TestPrivateNotesAreAuthorOnly(t *testing.T) {
	f := newStickyFixture(t)
	mine := f.create(f.owner, map[string]any{"title": "Owner secret", "body": "x", "visibility": "private"})
	theirs := f.create(f.guest, map[string]any{"title": "Guest secret", "body": "y", "visibility": "private"})
	shared := f.create(f.owner, map[string]any{"title": "Everyone", "body": "z"})
	mineID, theirsID, sharedID := str(mine["id"]), str(theirs["id"]), str(shared["id"])
	if shared["visibility"] != "shared" || shared["color"] != "yellow" || shared["board_x"] != nil {
		t.Errorf("defaults: %v", shared)
	}

	if ids := f.listIDs(f.owner); !has(ids, mineID) || has(ids, theirsID) || !has(ids, sharedID) {
		t.Errorf("owner list: %v", ids)
	}
	if ids := f.listIDs(f.guest); has(ids, mineID) || !has(ids, theirsID) || !has(ids, sharedID) {
		t.Errorf("guest list: %v", ids)
	}
	_, ownerSnap := f.dial(f.owner)
	_, guestSnap := f.dial(f.guest)
	if ids := snapshotNoteIDs(t, ownerSnap); !has(ids, mineID) || has(ids, theirsID) {
		t.Errorf("owner snapshot: %v", ids)
	}
	if ids := snapshotNoteIDs(t, guestSnap); has(ids, mineID) || !has(ids, theirsID) {
		t.Errorf("guest snapshot: %v", ids)
	}

	// Admins and other members get 404 on everything about someone else's private note.
	hiddenRef := "N-" + itoa(int32(theirs["number"].(float64)))
	for name, req := range map[string]func() resp{
		"patch":     func() resp { return f.owner.do("PATCH", "/api/v1/notes/"+theirsID, map[string]any{"body": "hacked"}) },
		"delete":    func() resp { return f.owner.do("DELETE", "/api/v1/notes/"+theirsID, nil) },
		"revisions": func() resp { return f.owner.do("GET", "/api/v1/notes/"+theirsID+"/revisions", nil) },
		"restore":   func() resp { return f.owner.do("POST", "/api/v1/notes/"+theirsID+"/revisions/1/restore", nil) },
		"ref":       func() resp { return f.owner.do("GET", "/api/v1/workspaces/"+f.ws+"/refs/"+hiddenRef, nil) },
		"guest-patch": func() resp {
			return f.guest.do("PATCH", "/api/v1/notes/"+mineID, map[string]any{"body": "hacked"})
		},
	} {
		if r := req(); r.Status != 404 {
			t.Errorf("%s: %d, want 404", name, r.Status)
		}
	}
	for _, ref := range f.refsOf(f.owner, "secret") {
		if ref == hiddenRef {
			t.Errorf("owner picker shows the guest's private note")
		}
	}
	if got := f.refsOf(f.guest, "secret"); len(got) != 1 || got[0] != hiddenRef {
		t.Errorf("guest picker: %v", got)
	}

	// Only the author changes visibility; an admin still edits a shared note.
	guestShared := f.create(f.guest, map[string]any{"title": "Guest shared", "body": "a"})
	gid := str(guestShared["id"])
	if r := f.owner.do("PATCH", "/api/v1/notes/"+gid, map[string]any{"visibility": "private"}); r.Status != 403 {
		t.Errorf("admin changing visibility: %d", r.Status)
	}
	if r := f.owner.do("PATCH", "/api/v1/notes/"+gid, map[string]any{"body": "edited by admin"}); r.Status != 200 {
		t.Errorf("admin editing shared: %d", r.Status)
	}
	if r := f.guest.do("PATCH", "/api/v1/notes/"+gid, map[string]any{"visibility": "private"}); r.Status != 200 {
		t.Errorf("author making private: %d", r.Status)
	}
	if ids := f.listIDs(f.owner); has(ids, gid) {
		t.Errorf("note turned private is still listed for the admin")
	}
}

func TestAgentsSeePrivateNotesOfTheirPerson(t *testing.T) {
	f := newStickyFixture(t)
	ctx := context.Background()
	me := f.owner.do("GET", "/api/v1/me", nil)
	runnerID := uuid.MustParse(f.e.addRunner(currentOrg(me.Body), userID(me.Body), "Laptop", "computer"))
	rn, err := f.e.store.GetRunner(ctx, runnerID)
	if err != nil {
		t.Fatal(err)
	}
	goal := func(by uuid.UUID) uuid.UUID {
		g, err := f.e.store.CreateGoal(ctx, db.CreateGoalParams{ID: uuid.New(), WorkspaceID: f.wsID, ManagerID: &f.riley, Title: "g", Color: "red", CreatedBy: &by, Status: "active"})
		if err != nil {
			t.Fatal(err)
		}
		return g.ID
	}
	session := func(goalID, person *uuid.UUID) uuid.UUID {
		s, err := f.e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: f.wsID, MemberID: f.dev,
			Status: "waiting", RunnerID: &runnerID, GoalID: goalID, PersonID: person})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	ownerGoal, guestGoal := goal(f.ownerID), goal(f.guestID)
	onOwnersGoal, onGuestsGoal := session(&ownerGoal, nil), session(&guestGoal, nil)
	chatWithOwner, chatWithGuest := session(nil, &f.ownerID), session(nil, &f.guestID)
	noOne := session(nil, nil)

	rec := &recorder{}
	o := orch.New(f.e.srv.Core, rec, nil)
	f.e.srv.Orch = o
	secret := f.create(f.owner, map[string]any{"title": "Owner plan", "body": "owner-only words", "visibility": "private"})
	ref := "N-" + itoa(int32(secret["number"].(float64)))

	for name, c := range map[string]struct {
		sess uuid.UUID
		sees bool
	}{"goal of the author": {onOwnersGoal, true}, "chat with the author": {chatWithOwner, true},
		"goal of someone else": {onGuestsGoal, false}, "chat with someone else": {chatWithGuest, false}, "no person": {noOne, false}} {
		notes := call(t, o, rn, rec, c.sess, protocol.ToolReadNotes, map[string]any{})
		item := call(t, o, rn, rec, c.sess, protocol.ToolReadItem, map[string]any{"ref": ref})
		upd := call(t, o, rn, rec, c.sess, protocol.ToolUpdateNote, map[string]any{"ref": ref, "body": "agent line"})
		if got := strings.Contains(notes.Text, "owner-only words"); got != c.sees {
			t.Errorf("%s: read_notes shows it = %v", name, got)
		}
		if got := !item.IsError && strings.Contains(item.Text, "owner-only words"); got != c.sees {
			t.Errorf("%s: read_item shows it = %v (%s)", name, got, item.Text)
		}
		if got := !upd.IsError; got != c.sees {
			t.Errorf("%s: update_note worked = %v (%s)", name, got, upd.Text)
		}
	}

	// A private note an agent writes belongs to the person it works for.
	if res := call(t, o, rn, rec, noOne, protocol.ToolWriteNote, map[string]any{"title": "t", "body": "b", "visibility": "private"}); !res.IsError {
		t.Errorf("private note with no person should fail: %+v", res)
	}
	if res := call(t, o, rn, rec, onGuestsGoal, protocol.ToolWriteNote, map[string]any{"title": "Guest idea", "body": "b", "visibility": "private"}); res.IsError {
		t.Fatalf("write_note: %+v", res)
	}
	var found *db.Note
	all, _ := f.e.store.ListNotes(ctx, db.ListNotesParams{WorkspaceID: f.wsID, ViewerID: &f.guestID})
	for i := range all {
		if all[i].Title == "Guest idea" {
			found = &all[i]
		}
	}
	if found == nil || found.Visibility != "private" || *found.AuthorUserID != f.guestID || *found.AuthorMemberID != f.dev {
		t.Fatalf("agent's private note: %+v", found)
	}
	if ids := f.listIDs(f.owner); has(ids, found.ID.String()) {
		t.Errorf("owner sees the guest's agent-written private note")
	}
}

func TestPrivateNoteEventsReachOnlyTheAuthor(t *testing.T) {
	f := newStickyFixture(t)
	ownerSock, _ := f.dial(f.owner)
	guestSock, _ := f.dial(f.guest)

	n := f.create(f.guest, map[string]any{"title": "Private one", "body": "x", "visibility": "private"})
	id := str(n["id"])
	if m := guestSock.next(3 * time.Second); m.Type != "note.saved" || !strings.Contains(string(m.Data), id) {
		t.Fatalf("author did not get note.saved: %+v", m)
	}
	f.guest.do("PATCH", "/api/v1/notes/"+id, map[string]any{"body": "changed"})
	guestSock.next(3 * time.Second)

	// A shared note made now is the first thing the admin's socket may see.
	marker := f.create(f.guest, map[string]any{"title": "Marker", "body": "m"})
	m := ownerSock.next(3 * time.Second)
	if m.Type != "note.saved" || !strings.Contains(string(m.Data), str(marker["id"])) {
		t.Fatalf("admin's socket got %s %s before the marker", m.Type, m.Data)
	}

	// Turning it shared tells everyone; turning it private again removes it from other boards.
	f.guest.do("PATCH", "/api/v1/notes/"+id, map[string]any{"visibility": "shared"})
	if m := ownerSock.next(3 * time.Second); m.Type != "note.saved" || !strings.Contains(string(m.Data), id) {
		t.Fatalf("shared note not announced: %+v", m)
	}
	f.guest.do("PATCH", "/api/v1/notes/"+id, map[string]any{"visibility": "private"})
	if m := ownerSock.next(3 * time.Second); m.Type != "note.deleted" || !strings.Contains(string(m.Data), id) {
		t.Fatalf("note going private not removed from others: %+v", m)
	}
	// Deleting a private note is silent for others.
	f.guest.do("DELETE", "/api/v1/notes/"+id, nil)
	marker2 := f.create(f.guest, map[string]any{"title": "Marker 2", "body": "m"})
	if m := ownerSock.next(3 * time.Second); !strings.Contains(string(m.Data), str(marker2["id"])) {
		t.Fatalf("private delete leaked: %s %s", m.Type, m.Data)
	}
}

func TestUpdateNoteKeepsRevisionsAndRestores(t *testing.T) {
	f := newStickyFixture(t)
	ctx := context.Background()
	me := f.owner.do("GET", "/api/v1/me", nil)
	runnerID := uuid.MustParse(f.e.addRunner(currentOrg(me.Body), userID(me.Body), "Laptop", "computer"))
	rn, _ := f.e.store.GetRunner(ctx, runnerID)
	s, err := f.e.store.CreateAgentSession(ctx, db.CreateAgentSessionParams{ID: uuid.New(), WorkspaceID: f.wsID, MemberID: f.dev, Status: "waiting", RunnerID: &runnerID, PersonID: &f.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	o := orch.New(f.e.srv.Core, rec, nil)
	f.e.srv.Orch = o

	n := f.create(f.owner, map[string]any{"title": "Todo list", "body": "one"})
	id, ref := str(n["id"]), "N-"+itoa(int32(n["number"].(float64)))
	revs := func() []any {
		return f.owner.do("GET", "/api/v1/notes/"+id+"/revisions", nil).Body["revisions"].([]any)
	}
	if len(revs()) != 0 {
		t.Fatalf("new note has revisions")
	}

	if res := call(t, o, rn, rec, s.ID, protocol.ToolUpdateNote, map[string]any{"ref": ref, "body": "two"}); res.IsError {
		t.Fatalf("append: %+v", res)
	}
	got := f.owner.do("GET", f.base, nil)
	var cur map[string]any
	for _, x := range got.Body["notes"].([]any) {
		if str(x.(map[string]any)["id"]) == id {
			cur = x.(map[string]any)
		}
	}
	if cur["body"] != "one\n\ntwo" || cur["edited_by_member_id"] != f.dev.String() || cur["edited_by_user_id"] != nil {
		t.Errorf("after append: %v", cur)
	}
	if r := revs(); len(r) != 1 || r[0].(map[string]any)["body"] != "one" {
		t.Errorf("revision after append: %v", r)
	}
	if res := call(t, o, rn, rec, s.ID, protocol.ToolUpdateNote, map[string]any{"ref": ref, "title": "Done list", "body": "fresh", "mode": "replace"}); res.IsError {
		t.Fatalf("replace: %+v", res)
	}
	if res := call(t, o, rn, rec, s.ID, protocol.ToolUpdateNote, map[string]any{"ref": ref}); !res.IsError {
		t.Errorf("update with nothing to change should fail")
	}
	if res := call(t, o, rn, rec, s.ID, protocol.ToolUpdateNote, map[string]any{"ref": "T-1", "body": "x"}); !res.IsError {
		t.Errorf("update_note on a task should fail")
	}

	// Restore revision 1 ("one"): the replaced version is kept too, so restore is undoable.
	r := f.owner.do("POST", "/api/v1/notes/"+id+"/revisions/1/restore", nil)
	note := r.Body["note"].(map[string]any)
	if r.Status != 200 || note["body"] != "one" || note["title"] != "Todo list" || note["edited_by_user_id"] != userID(me.Body) {
		t.Errorf("restore: %d %v", r.Status, r.Body)
	}
	if rv := revs(); len(rv) != 3 || rv[0].(map[string]any)["body"] != "fresh" {
		t.Errorf("revisions after restore: %v", rv)
	}
	if r := f.owner.do("POST", "/api/v1/notes/"+id+"/revisions/99/restore", nil); r.Status != 404 {
		t.Errorf("missing revision: %d", r.Status)
	}

	// Only the last 20 revisions are kept.
	for i := range 25 {
		f.owner.do("PATCH", "/api/v1/notes/"+id, map[string]any{"body": "edit " + itoa(int32(i))})
	}
	rv := revs()
	if len(rv) != 20 {
		t.Fatalf("kept %d revisions, want 20", len(rv))
	}
	if first := rv[0].(map[string]any)["body"]; first != "edit 23" {
		t.Errorf("newest kept revision %v", first)
	}
}

func TestMovingAStickySavesNoRevision(t *testing.T) {
	f := newStickyFixture(t)
	n := f.create(f.owner, map[string]any{"title": "Sticky", "body": "text", "color": "pink", "board": map[string]any{"x": 10.5, "y": 20}})
	id := str(n["id"])
	if n["board_x"] != 10.5 || n["board_y"] != float64(20) || n["color"] != "pink" {
		t.Fatalf("created sticky: %v", n)
	}
	revs := func() int {
		return len(f.owner.do("GET", "/api/v1/notes/"+id+"/revisions", nil).Body["revisions"].([]any))
	}
	patch := func(body map[string]any) map[string]any {
		r := f.owner.do("PATCH", "/api/v1/notes/"+id, body)
		if r.Status != 200 {
			t.Fatalf("patch %v: %d %v", body, r.Status, r.Body)
		}
		return r.Body["note"].(map[string]any)
	}
	m := patch(map[string]any{"board": map[string]any{"x": 300, "y": -40}})
	if m["board_x"] != float64(300) || m["board_y"] != float64(-40) || m["body"] != "text" || m["edited_by_user_id"] != nil {
		t.Errorf("moved: %v", m)
	}
	patch(map[string]any{"color": "blue"})
	patch(map[string]any{"title": "Sticky", "body": "text"}) // same text: nothing changed
	if revs() != 0 {
		t.Errorf("moving, recoloring or saving the same text made %d revisions", revs())
	}
	if u := patch(map[string]any{"board": nil}); u["board_x"] != nil || u["board_y"] != nil {
		t.Errorf("unpin: %v", u)
	}
	patch(map[string]any{"body": "new text"})
	if revs() != 1 {
		t.Errorf("a text change should make one revision, got %d", revs())
	}
	for name, body := range map[string]map[string]any{
		"colour":     {"color": "chartreuse"},
		"visibility": {"visibility": "secret"},
		"half pin":   {"board": map[string]any{"x": 1}},
	} {
		if r := f.owner.do("PATCH", "/api/v1/notes/"+id, body); r.Status != 400 && r.Status != 422 {
			t.Errorf("%s: %d", name, r.Status)
		}
	}
}

func TestDeleteNote(t *testing.T) {
	f := newStickyFixture(t)
	ctx := context.Background()
	revCount := func(id string) int {
		revs, err := f.e.store.ListNoteRevisions(ctx, uuid.MustParse(id))
		if err != nil {
			t.Fatal(err)
		}
		return len(revs)
	}

	// The author deletes their own note, and its revisions go with it.
	mine := f.create(f.guest, map[string]any{"title": "Mine", "body": "a"})
	mid := str(mine["id"])
	f.guest.do("PATCH", "/api/v1/notes/"+mid, map[string]any{"body": "b"})
	if revCount(mid) != 1 {
		t.Fatalf("setup: want 1 revision")
	}
	if r := f.guest.do("DELETE", "/api/v1/notes/"+mid, nil); r.Status != 204 {
		t.Fatalf("author delete: %d", r.Status)
	}
	if revCount(mid) != 0 || has(f.listIDs(f.guest), mid) {
		t.Errorf("note or revisions left after delete")
	}
	if r := f.guest.do("DELETE", "/api/v1/notes/"+mid, nil); r.Status != 404 {
		t.Errorf("second delete: %d", r.Status)
	}

	// An admin deletes a shared note written by an agent, and others hear about it.
	agent, err := f.e.store.CreateNote(ctx, db.CreateNoteParams{ID: uuid.New(), WorkspaceID: f.wsID, GoalID: nil, Kind: "decision",
		Title: "Agent note", Body: "x", AuthorMemberID: &f.dev, Visibility: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	aid := agent.ID.String()
	guestSock, _ := f.dial(f.guest)

	// Other staff may not delete it.
	if r := f.guest.do("DELETE", "/api/v1/notes/"+aid, nil); r.Status != 403 {
		t.Errorf("staff delete of agent note: %d, want 403", r.Status)
	}
	if r := f.owner.do("DELETE", "/api/v1/notes/"+aid, nil); r.Status != 204 {
		t.Fatalf("admin delete: %d", r.Status)
	}
	if m := guestSock.next(3 * time.Second); m.Type != "note.deleted" || !strings.Contains(string(m.Data), aid) {
		t.Errorf("note.deleted not sent: %+v", m)
	}

	// Someone else's private note is 404, even for an admin.
	priv := f.create(f.guest, map[string]any{"title": "Secret", "body": "s", "visibility": "private"})
	if r := f.owner.do("DELETE", "/api/v1/notes/"+str(priv["id"]), nil); r.Status != 404 {
		t.Errorf("admin delete of private note: %d, want 404", r.Status)
	}
}
