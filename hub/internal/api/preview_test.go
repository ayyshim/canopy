package api_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/store/db"
)

type previewFixture struct {
	e                       *env
	owner, stranger         *client
	indexOut, otherGoalOut  string
	taskA, taskB, taskOther uuid.UUID
}

func (f *previewFixture) addOutput(t *testing.T, task, member uuid.UUID, ws, name, mime, body string) string {
	t.Helper()
	ctx := context.Background()
	o, err := f.e.store.UpsertOutput(ctx, db.UpsertOutputParams{ID: uuid.Must(uuid.NewV7()), WorkspaceID: uuid.MustParse(ws), TaskID: &task, MemberID: &member, Kind: "document", Name: name, Title: name})
	if err != nil {
		t.Fatal(err)
	}
	key := "preview-test/" + uuid.NewString()
	if err := f.e.srv.Blobs.Put(ctx, key, strings.NewReader(body), int64(len(body)), mime); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.store.CreateOutputVersion(ctx, db.CreateOutputVersionParams{ID: uuid.Must(uuid.NewV7()), OutputID: o.ID, BlobKey: key, Mime: mime, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	return o.ID.String()
}

func newPreviewFixture(t *testing.T) *previewFixture {
	e := newEnv(t)
	e.srv.Blobs = blob.Dir{Root: t.TempDir()}
	owner, me := e.signup("Preview Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	rileyID := uuid.MustParse(riley)
	stranger, _ := e.signup("Strangers")

	mkGoal := func(title string) string {
		g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": title, "draft": true})
		return str(g.Body["goal"].(map[string]any)["id"])
	}
	mkTask := func(goal string) uuid.UUID {
		r := owner.do("POST", "/api/v1/goals/"+goal+"/tasks", map[string]any{"title": "t", "member_id": riley})
		if r.Status != http.StatusCreated {
			t.Fatalf("task: %d %v", r.Status, r.Body)
		}
		return uuid.MustParse(str(r.Body["task"].(map[string]any)["id"]))
	}
	goal1, goal2 := mkGoal("Prototype"), mkGoal("Other")
	f := &previewFixture{e: e, owner: owner, stranger: stranger}
	f.taskA, f.taskB, f.taskOther = mkTask(goal1), mkTask(goal1), mkTask(goal2)

	f.indexOut = f.addOutput(t, f.taskA, rileyID, ws, "index.html", "text/html; charset=utf-8", `<!doctype html><html><head><title>x</title></head><body><a href="about.html">About</a></body></html>`)
	f.addOutput(t, f.taskA, rileyID, ws, "assets/app.css", "text/css", "body{color:red}")
	f.addOutput(t, f.taskB, rileyID, ws, "about.html", "text/html", "<html><head></head><body>about</body></html>")
	f.addOutput(t, f.taskA, rileyID, ws, "shared.js", "text/javascript", "// A")
	f.addOutput(t, f.taskB, rileyID, ws, "shared.js", "text/javascript", "// B")
	f.otherGoalOut = f.addOutput(t, f.taskOther, rileyID, ws, "secret.html", "text/html", "<html><head></head>secret</html>")
	return f
}

func (f *previewFixture) token(t *testing.T, output string) (token, url string) {
	t.Helper()
	r := f.owner.do("POST", "/api/v1/outputs/"+output+"/preview", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("preview: %d %v", r.Status, r.Body)
	}
	url = str(r.Body["url"])
	if _, err := time.Parse(time.RFC3339, str(r.Body["expires_at"])); err != nil {
		t.Errorf("expires_at: %v", r.Body["expires_at"])
	}
	prefix := "/api/v1/preview/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("url: %s", url)
	}
	return strings.Split(strings.TrimPrefix(url, prefix), "/")[0], url
}

func get(t *testing.T, base, path string) (int, http.Header, string) {
	t.Helper()
	res, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(b)
}

func TestPreviewTokenScope(t *testing.T) {
	f := newPreviewFixture(t)
	base := f.e.ts.URL
	token, url := f.token(t, f.indexOut)
	if !strings.HasSuffix(url, "/"+token+"/index.html") {
		t.Errorf("url: %s", url)
	}

	// Other users and orgs cannot ask for a token.
	if r := f.stranger.do("POST", "/api/v1/outputs/"+f.indexOut+"/preview", nil); r.Status != http.StatusNotFound {
		t.Errorf("stranger token: %d", r.Status)
	}
	if r := f.owner.do("POST", "/api/v1/outputs/"+uuid.NewString()+"/preview", nil); r.Status != http.StatusNotFound {
		t.Errorf("unknown output: %d", r.Status)
	}

	// The token reads its task and goal, and nothing from another goal.
	if st, _, _ := get(t, base, "/api/v1/preview/"+token+"/index.html"); st != 200 {
		t.Errorf("index: %d", st)
	}
	if st, _, _ := get(t, base, "/api/v1/preview/"+token+"/secret.html"); st != http.StatusNotFound {
		t.Errorf("other goal: %d", st)
	}
	if st, _, _ := get(t, base, "/api/v1/preview/nope/index.html"); st != http.StatusNotFound {
		t.Errorf("unknown token: %d", st)
	}
	// A token for the other goal cannot read this goal's pages.
	otherToken, _ := f.token(t, f.otherGoalOut)
	if st, _, _ := get(t, base, "/api/v1/preview/"+otherToken+"/index.html"); st != http.StatusNotFound {
		t.Errorf("token from another goal: %d", st)
	}

	// An expired token gives 404.
	f.e.srv.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if st, _, _ := get(t, base, "/api/v1/preview/"+token+"/index.html"); st != http.StatusNotFound {
		t.Errorf("expired: %d", st)
	}
}

func TestPreviewNames(t *testing.T) {
	f := newPreviewFixture(t)
	base := f.e.ts.URL
	token, _ := f.token(t, f.indexOut)
	p := "/api/v1/preview/" + token + "/"

	st, h, body := get(t, base, p+"assets/app.css")
	if st != 200 || body != "body{color:red}" || h.Get("Content-Type") != "text/css" {
		t.Errorf("subfolder css: %d %q %q", st, h.Get("Content-Type"), body)
	}
	if st, _, body := get(t, base, p+"about.html"); st != 200 || !strings.Contains(body, "about") {
		t.Errorf("page from another task: %d", st)
	}
	// The same name in the token's own task wins over another task.
	if _, _, body := get(t, base, p+"shared.js"); body != "// A" {
		t.Errorf("same task first: %q", body)
	}
	for _, bad := range []string{"missing.html", "assets/", "assets/nope.css", "../index.html", "assets/../index.html", "%2e%2e/index.html", "a%5Cb"} {
		if st, _, _ := get(t, base, p+bad); st != http.StatusNotFound {
			t.Errorf("%q: %d", bad, st)
		}
	}
}

func TestPreviewHeadersAndScript(t *testing.T) {
	f := newPreviewFixture(t)
	token, _ := f.token(t, f.indexOut)
	p := "/api/v1/preview/" + token + "/"

	st, h, body := get(t, f.e.ts.URL, p+"index.html")
	if st != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Fatalf("html: %d %q", st, h.Get("Content-Type"))
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads;", "script-src 'self' 'unsafe-inline' https:", "connect-src https:"} {
		if !strings.Contains(csp, want) {
			t.Errorf("csp missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("csp allows same origin: %s", csp)
	}
	for k, v := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store"} {
		if h.Get(k) != v {
			t.Errorf("%s = %q", k, h.Get(k))
		}
	}
	if n := strings.Count(body, "canopy-preview"); n != 2 { // marker comment + message type
		t.Errorf("script count: %d", n)
	}
	if strings.Count(body, "<script>") != 1 || strings.Index(body, "<script>") < strings.Index(body, "<head>") || strings.Index(body, "<script>") > strings.Index(body, "<title>") {
		t.Errorf("script not at top of head: %s", body)
	}
	if !strings.Contains(body, `<a href="about.html">`) {
		t.Errorf("page body changed: %s", body)
	}

	// Other types get the same headers and no script.
	_, h, css := get(t, f.e.ts.URL, p+"assets/app.css")
	if h.Get("Content-Security-Policy") != csp || strings.Contains(css, "script") {
		t.Errorf("css: %q %v", css, h)
	}
}
