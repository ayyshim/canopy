package api_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

type discordHit struct {
	Query   map[string]string
	Payload map[string]any
	Files   map[string]string
}

// fakeDiscord records what the hub posts and answers like a webhook with ?wait=true.
type fakeDiscord struct {
	srv    *httptest.Server
	mu     sync.Mutex
	hits   []discordHit
	status int
	chanID string
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	f := &fakeDiscord{status: 200, chanID: "555"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := discordHit{Query: map[string]string{}, Files: map[string]string{}}
		for k, v := range r.URL.Query() {
			h.Query[k] = v[0]
		}
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				if p.FormName() == "payload_json" {
					json.Unmarshal(b, &h.Payload)
				} else {
					h.Files[p.FileName()] = string(b)
				}
			}
		} else {
			json.NewDecoder(r.Body).Decode(&h.Payload)
		}
		f.mu.Lock()
		f.hits = append(f.hits, h)
		status := f.status
		f.mu.Unlock()
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte(`{"id":"m1","channel_id":"` + f.chanID + `"}`))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDiscord) got() []discordHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]discordHit(nil), f.hits...)
}

type dcFixture struct {
	e       *env
	fake    *fakeDiscord
	ch      *notify.DiscordChannel
	ws      db.Workspace
	goal    uuid.UUID
	task    uuid.UUID
	dev     uuid.UUID
	blobs   blob.Dir
	ctx     context.Context
	allKind map[string]bool
}

func newDCFixture(t *testing.T) *dcFixture {
	e := newEnv(t)
	owner, me := e.signup("Disco Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	riley, dev := str(members(d)["Riley"]["id"]), str(members(d)["Dev"]["id"])
	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "draft": true})
	goal := uuid.MustParse(str(g.Body["goal"].(map[string]any)["id"]))
	tk := owner.do("POST", "/api/v1/goals/"+goal.String()+"/tasks", map[string]any{"title": "Add column", "member_id": dev})
	f := &dcFixture{e: e, fake: newFakeDiscord(t), goal: goal, dev: uuid.MustParse(dev), ctx: context.Background(),
		task: uuid.MustParse(str(tk.Body["task"].(map[string]any)["id"])), blobs: blob.Dir{Root: t.TempDir()}}
	f.ch = notify.NewDiscordChannel(e.store, e.srv.Core, f.blobs, "https://canopy.example.com")
	f.ws, _ = e.store.GetWorkspace(f.ctx, uuid.MustParse(ws))
	f.configure(t, map[string]bool{"questions": true, "progress": true, "goal_ready": true, "errors": true}, "", false)
	return f
}

func (f *dcFixture) configure(t *testing.T, kinds map[string]bool, role string, forum bool) {
	t.Helper()
	raw, _ := json.Marshal(kinds)
	ws, err := f.e.store.SetWorkspaceDiscord(f.ctx, db.SetWorkspaceDiscordParams{ID: f.ws.ID, Webhook: f.fake.srv.URL + "/api/webhooks/1/tok", Kinds: raw, RoleID: role, Forum: forum})
	if err != nil {
		t.Fatal(err)
	}
	f.ws = ws
}

// send queues one note and delivers the newest outbox row.
func (f *dcFixture) send(t *testing.T, n notify.Note) error {
	t.Helper()
	if n.Recipients == nil {
		n.Recipients = []uuid.UUID{}
	}
	if err := f.e.srv.Core.Tx(f.ctx, func(tx *core.Tx) error { return notify.Enqueue(f.ctx, tx, f.ws, n) }); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.e.store.ListNotifications(f.ctx, f.ws.ID)
	row := rows[len(rows)-1]
	err := f.ch.Send(f.ctx, row)
	// Keep the shared test database clear for the dispatcher test.
	_ = f.e.store.MarkDiscordSent(f.ctx, row.ID)
	_ = f.e.store.MarkPushSent(f.ctx, row.ID)
	return err
}

func (f *dcFixture) addOutput(t *testing.T, name, mimeType, body string) uuid.UUID {
	t.Helper()
	o, err := f.e.store.UpsertOutput(f.ctx, db.UpsertOutputParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: f.ws.ID, TaskID: &f.task, MemberID: &f.dev, Kind: "document", Name: name, Title: name})
	if err != nil {
		t.Fatal(err)
	}
	key := "dc-test/" + uuid.NewString()
	if err := f.blobs.Put(f.ctx, key, strings.NewReader(body), int64(len(body)), mimeType); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.store.CreateOutputVersion(f.ctx, db.CreateOutputVersionParams{ID: uuid.Must(uuid.NewV7()), OutputID: o.ID, BlobKey: key, Mime: mimeType, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func firstEmbed(h discordHit) map[string]any { return h.Payload["embeds"].([]any)[0].(map[string]any) }

func fieldValue(emb map[string]any, name string) string {
	fs, _ := emb["fields"].([]any)
	for _, x := range fs {
		f := x.(map[string]any)
		if f["name"] == name {
			return f["value"].(string)
		}
	}
	return ""
}

func TestDiscordMessageShape(t *testing.T) {
	f := newDCFixture(t)
	f.configure(t, map[string]bool{"questions": true, "progress": true, "goal_ready": true, "errors": true}, "123456789", false)
	if err := f.send(t, notify.Note{Kind: "questions", MemberID: &f.dev, Body: "Which table?", GoalID: &f.goal, TaskID: &f.task}); err != nil {
		t.Fatal(err)
	}
	h := f.fake.got()[0]
	if h.Query["wait"] != "true" || h.Query["with_components"] != "true" {
		t.Fatalf("query %v", h.Query)
	}
	if h.Payload["username"] != f.ws.Name+" - Dev" {
		t.Fatalf("username %v", h.Payload["username"])
	}
	e := firstEmbed(h)
	if e["title"] != f.ws.Name+" - Dev" || e["description"] != "Which table?" || e["timestamp"] == nil {
		t.Fatalf("embed %v", e)
	}
	if !strings.HasPrefix(e["url"].(string), "https://canopy.example.com/w/"+f.ws.ID.String()+"?goal=") {
		t.Fatalf("url %v", e["url"])
	}
	if fieldValue(e, "Goal") != "CSV export" || fieldValue(e, "Task") != "Add column" {
		t.Fatalf("fields %v", e["fields"])
	}
	if int(e["color"].(float64)) != 0xF5A524 {
		t.Fatalf("color %v", e["color"])
	}
	if !strings.HasPrefix(h.Payload["content"].(string), "<@&123456789>") {
		t.Fatalf("no role ping: %v", h.Payload["content"])
	}
	btn := h.Payload["components"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	if btn["label"] != "Open in Canopy" {
		t.Fatalf("button %v", btn)
	}
	st, _ := f.e.store.GetWorkspace(f.ctx, f.ws.ID)
	if st.DiscordLastOkAt == nil || st.DiscordLastError != "" {
		t.Fatalf("status %v %q", st.DiscordLastOkAt, st.DiscordLastError)
	}
}

func TestDiscordFiltersAndRolePing(t *testing.T) {
	f := newDCFixture(t)
	f.configure(t, map[string]bool{"questions": true, "progress": false, "goal_ready": true, "errors": true}, "123456789", false)
	if err := f.send(t, notify.Note{Kind: "progress", MemberID: &f.dev, Body: "Step done"}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.fake.got()); n != 0 {
		t.Fatalf("progress was sent although off: %d", n)
	}
	if err := f.send(t, notify.Note{Kind: "goal_ready", MemberID: &f.dev, Body: "Ready for review"}); err != nil {
		t.Fatal(err)
	}
	h := f.fake.got()[0]
	if c, _ := h.Payload["content"].(string); strings.Contains(c, "<@&") {
		t.Fatalf("role pinged on goal_ready: %q", c)
	}
	am := h.Payload["allowed_mentions"].(map[string]any)
	if _, has := am["roles"]; has {
		t.Fatalf("roles allowed: %v", am)
	}
}

func TestDiscordImageAndLinks(t *testing.T) {
	f := newDCFixture(t)
	img := f.addOutput(t, "screenshot", "image/png", "PNGDATA")
	page := f.addOutput(t, "prototype.html", "text/html", "<h1>hi</h1>")
	err := f.send(t, notify.Note{Kind: "goal_ready", MemberID: &f.dev, Body: "Done", GoalID: &f.goal, TaskID: &f.task,
		Media: notify.Media{OutputIDs: []uuid.UUID{img, page}, Links: []notify.Link{
			{Label: "Open artifact", URL: "https://claude.ai/artifact/abc"}, {Label: "Open PR", URL: "https://github.com/o/r/pull/1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	h := f.fake.got()[0]
	if h.Files["screenshot.png"] != "PNGDATA" || len(h.Files) != 1 {
		t.Fatalf("files %v", h.Files)
	}
	e := firstEmbed(h)
	if e["image"].(map[string]any)["url"] != "attachment://screenshot.png" {
		t.Fatalf("image %v", e["image"])
	}
	outs := fieldValue(e, "Outputs")
	if !strings.Contains(outs, "[prototype.html](https://canopy.example.com/w/"+f.ws.ID.String()) || !strings.Contains(outs, "output="+page.String()) {
		t.Fatalf("outputs %q", outs)
	}
	var labels []string
	for _, c := range h.Payload["components"].([]any)[0].(map[string]any)["components"].([]any) {
		labels = append(labels, c.(map[string]any)["label"].(string))
	}
	if strings.Join(labels, ",") != "Open in Canopy,Open artifact,Open PR" {
		t.Fatalf("buttons %v", labels)
	}
}

func TestDiscordForumThreadReuse(t *testing.T) {
	f := newDCFixture(t)
	f.configure(t, map[string]bool{"questions": true, "progress": true, "goal_ready": true, "errors": true}, "", true)
	f.fake.chanID = "777"
	for i := 0; i < 2; i++ {
		if err := f.send(t, notify.Note{Kind: "progress", MemberID: &f.dev, Body: "step", GoalID: &f.goal}); err != nil {
			t.Fatal(err)
		}
	}
	hits := f.fake.got()
	if hits[0].Payload["thread_name"] != "CSV export" || hits[0].Query["thread_id"] != "" {
		t.Fatalf("first: %v %v", hits[0].Payload["thread_name"], hits[0].Query)
	}
	if hits[1].Query["thread_id"] != "777" || hits[1].Payload["thread_name"] != nil {
		t.Fatalf("second: %v %v", hits[1].Payload["thread_name"], hits[1].Query)
	}
	g, _ := f.e.store.GetGoal(f.ctx, f.goal)
	if g.DiscordThreadID != "777" {
		t.Fatalf("stored thread %q", g.DiscordThreadID)
	}
	// A message without a goal is not put in a thread.
	if err := f.send(t, notify.Note{Kind: "progress", Body: "general"}); err != nil {
		t.Fatal(err)
	}
	if last := f.fake.got()[2]; last.Query["thread_id"] != "" || last.Payload["thread_name"] != nil {
		t.Fatalf("general message in thread: %v", last.Query)
	}
}

func TestDiscordWebhookGone(t *testing.T) {
	f := newDCFixture(t)
	f.fake.status = 404
	for i := 0; i < 2; i++ {
		if err := f.send(t, notify.Note{Kind: "questions", MemberID: &f.dev, Body: "q"}); err != nil {
			t.Fatalf("a gone webhook must not be retried: %v", err)
		}
		if i == 0 {
			f.configure(t, map[string]bool{"questions": true, "progress": true, "goal_ready": true, "errors": true}, "", false)
		}
	}
	ws, _ := f.e.store.GetWorkspace(f.ctx, f.ws.ID)
	if ws.DiscordWebhook != "" || ws.DiscordLastError == "" {
		t.Fatalf("not switched off: %q %q", ws.DiscordWebhook, ws.DiscordLastError)
	}
	sigs, _ := f.e.store.ListOpenSignals(f.ctx, f.ws.ID)
	n := 0
	for _, s := range sigs {
		if s.Type == "error" && strings.Contains(string(s.Payload), `"discord"`) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one error signal, got %d", n)
	}
	// With Discord off nothing more is sent.
	before := len(f.fake.got())
	if err := f.send(t, notify.Note{Kind: "questions", Body: "q"}); err != nil || len(f.fake.got()) != before {
		t.Fatalf("sent while off: %v", err)
	}
}

func TestDiscordTestMessageHasSampleImage(t *testing.T) {
	f := newDCFixture(t)
	f.configure(t, map[string]bool{"questions": false, "progress": false, "goal_ready": false, "errors": false}, "", false)
	if err := f.send(t, notify.Note{Kind: "progress", Body: "test", Media: notify.Media{Test: true}}); err != nil {
		t.Fatal(err)
	}
	h := f.fake.got()[0]
	if !strings.HasPrefix(h.Files["sample.png"], "\x89PNG") {
		t.Fatalf("no sample image: %v", h.Files)
	}
	if firstEmbed(h)["image"].(map[string]any)["url"] != "attachment://sample.png" {
		t.Fatal("sample image not in embed")
	}
}
