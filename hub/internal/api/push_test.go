package api_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"canopy/hub/internal/core"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
)

// browserKeys makes the keys a browser hands over when it subscribes.
func browserKeys(t *testing.T) (p256dh, authSecret string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	_, _ = rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

func subBody(t *testing.T, endpoint string) map[string]any {
	p, a := browserKeys(t)
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": p, "auth": a}}
}

// fakePush is a push service that records what the hub sends and answers with a set status.
type fakePush struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	got    []http.Header
}

func newFakePush(t *testing.T) *fakePush {
	f := &fakePush{status: http.StatusCreated}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.got = append(f.got, r.Header.Clone())
		w.WriteHeader(f.status)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePush) setStatus(s int) { f.mu.Lock(); f.status = s; f.mu.Unlock() }
func (f *fakePush) count() int      { f.mu.Lock(); defer f.mu.Unlock(); return len(f.got) }

func (e *env) enablePush(t *testing.T) {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	e.srv.VAPID = notify.VAPID{Public: pub, Private: priv}
	e.srv.Push = notify.NewPushChannel(e.store, e.srv.VAPID, "http://canopy.test")
}

func TestPushKeyAndSubscriptions(t *testing.T) {
	e := newEnv(t)
	e.enablePush(t)
	alice, _ := e.signup("Push Co")
	bob, me := e.signup("Push Two Co")
	bobID := uuid.MustParse(userID(me))
	ctx := context.Background()

	if r := alice.do("GET", "/api/v1/push/key", nil); r.Status != 200 || r.Body["key"] != e.srv.VAPID.Public {
		t.Errorf("key: %d %v", r.Status, r.Body)
	}
	if r := e.client().do("GET", "/api/v1/push/key", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("signed out key: %d", r.Status)
	}
	ep := "https://push.example/send/abc123"
	if r := alice.do("POST", "/api/v1/push/subscriptions", subBody(t, ep)); r.Status != 200 {
		t.Fatalf("subscribe: %d %v", r.Status, r.Body)
	}
	if r := alice.do("POST", "/api/v1/push/subscriptions", subBody(t, ep)); r.Status != 200 {
		t.Fatalf("subscribe again: %d", r.Status)
	}
	var n int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, ep).Scan(&n)
	if n != 1 {
		t.Errorf("upsert made %d rows", n)
	}
	for name, body := range map[string]map[string]any{
		"no endpoint": {"keys": map[string]string{"p256dh": "a", "auth": "b"}},
		"no keys":     {"endpoint": "https://push.example/x"},
		"not a url":   {"endpoint": "nonsense", "keys": map[string]string{"p256dh": "a", "auth": "b"}},
	} {
		if r := alice.do("POST", "/api/v1/push/subscriptions", body); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, r.Status)
		}
	}

	// Another person can't remove it, and signing in on the same browser moves it to them.
	if r := bob.do("DELETE", "/api/v1/push/subscriptions", map[string]any{"endpoint": ep}); r.Status != 200 {
		t.Errorf("delete other's: %d", r.Status)
	}
	if subs, _ := e.store.ListUserPushSubscriptions(ctx, bobID); len(subs) != 0 {
		t.Errorf("bob has subscriptions: %v", subs)
	}
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, ep).Scan(&n)
	if n != 1 {
		t.Fatalf("bob removed alice's subscription")
	}
	if r := bob.do("POST", "/api/v1/push/subscriptions", subBody(t, ep)); r.Status != 200 {
		t.Fatalf("bob subscribes: %d", r.Status)
	}
	if subs, _ := e.store.ListUserPushSubscriptions(ctx, bobID); len(subs) != 1 {
		t.Errorf("endpoint did not move: %v", subs)
	}
	if r := bob.do("DELETE", "/api/v1/push/subscriptions", map[string]any{"endpoint": ep}); r.Status != 200 {
		t.Errorf("delete own: %d", r.Status)
	}
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, ep).Scan(&n)
	if n != 0 {
		t.Errorf("not deleted")
	}
	if r := alice.do("POST", "/api/v1/push/test", nil); r.Status != http.StatusConflict {
		t.Errorf("test without device: %d", r.Status)
	}
}

func TestVAPIDKeysAreKept(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, err := notify.LoadVAPID(ctx, e.store, "", "")
	if err != nil || a.Public == "" || a.Private == "" {
		t.Fatalf("load: %v %+v", err, a)
	}
	if b, _ := notify.LoadVAPID(ctx, e.store, "", ""); b != a {
		t.Error("keys changed between loads")
	}
	if c, _ := notify.LoadVAPID(ctx, e.store, "pub", "priv"); c.Public != "pub" || c.Private != "priv" {
		t.Errorf("env override: %+v", c)
	}
}

func TestPushSendsAndDropsGoneSubscriptions(t *testing.T) {
	e := newEnv(t)
	e.enablePush(t)
	owner, me := e.signup("Gone Co")
	uid := uuid.MustParse(userID(me))
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	wsID := uuid.MustParse(ws)
	dev := uuid.MustParse(str(members(d)["Dev"]["id"]))
	ctx := context.Background()
	fake := newFakePush(t)
	if r := owner.do("POST", "/api/v1/push/subscriptions", subBody(t, fake.URL+"/s/1")); r.Status != 200 {
		t.Fatalf("subscribe: %d %v", r.Status, r.Body)
	}
	w, _ := e.store.GetWorkspace(ctx, wsID)
	row := func(kind string) db.NotificationOutbox {
		var out db.NotificationOutbox
		err := e.srv.Core.Tx(ctx, func(tx *core.Tx) error {
			sig, err := tx.Q.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.New(), WorkspaceID: wsID, Type: "gate", MemberID: &dev, Payload: []byte(`{"summary":"make build"}`)})
			if err != nil {
				return err
			}
			if err := notify.EnqueueSignal(ctx, tx, w, sig); err != nil {
				return err
			}
			rows, err := tx.Q.ListNotifications(ctx, wsID)
			out = rows[len(rows)-1]
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		out.Kind = kind
		return out
	}

	if err := e.srv.Push.Send(ctx, row("questions")); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 {
		t.Fatalf("requests: %d", fake.count())
	}
	h := fake.got[0]
	if h.Get("Urgency") != "high" || h.Get("Ttl") != "86400" || h.Get("Content-Encoding") != "aes128gcm" {
		t.Errorf("headers: %v", h)
	}
	var tokens int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM push_action_tokens WHERE user_id = $1`, uid).Scan(&tokens)
	if tokens != 1 {
		t.Errorf("tokens: %d", tokens)
	}

	// A kind the person turned push off for is not sent.
	owner.do("PATCH", "/api/v1/me", map[string]any{"notifications": map[string]any{"errors": map[string]any{"desktop": true, "email": true, "push": false}}})
	_ = e.srv.Push.Send(ctx, row("errors"))
	if fake.count() != 1 {
		t.Errorf("sent with push off: %d", fake.count())
	}
	// Snooze stops push; clearing it brings push back.
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if r := owner.do("PATCH", "/api/v1/me", map[string]any{"snooze_until": until}); r.Status != 200 || r.Body["user"].(map[string]any)["snooze_until"] == nil {
		t.Fatalf("snooze: %d %v", r.Status, r.Body)
	}
	_ = e.srv.Push.Send(ctx, row("questions"))
	if fake.count() != 1 {
		t.Errorf("sent while snoozed: %d", fake.count())
	}
	if r := owner.do("PATCH", "/api/v1/me", map[string]any{"snooze_until": nil}); r.Body["user"].(map[string]any)["snooze_until"] != nil {
		t.Errorf("snooze not cleared: %v", r.Body)
	}
	if r := owner.do("PATCH", "/api/v1/me", map[string]any{"name": "Riley P"}); r.Status != 200 {
		t.Errorf("patch without snooze: %d", r.Status)
	}

	// A service error is retried by the dispatcher; 410 removes the subscription.
	fake.setStatus(http.StatusInternalServerError)
	if err := e.srv.Push.Send(ctx, row("questions")); err == nil {
		t.Error("500 should fail the send")
	}
	if subs, _ := e.store.ListUserPushSubscriptions(ctx, uid); len(subs) != 1 {
		t.Fatalf("subscription dropped on 500")
	}
	fake.setStatus(http.StatusGone)
	if err := e.srv.Push.Send(ctx, row("questions")); err != nil {
		t.Errorf("410: %v", err)
	}
	if subs, _ := e.store.ListUserPushSubscriptions(ctx, uid); len(subs) != 0 {
		t.Errorf("gone subscription kept: %v", subs)
	}
}

func TestNotificationAct(t *testing.T) {
	e := newEnv(t)
	e.enablePush(t)
	owner, me := e.signup("Act Co")
	org := currentOrg(me)
	uid := uuid.MustParse(userID(me))
	ws, d := e.createWorkspace(owner, org, setupPayload())
	wsID := uuid.MustParse(ws)
	dev := uuid.MustParse(str(members(d)["Dev"]["id"]))
	outsider, outsiderID := e.joinOrg(owner, org, "Olly") // in the org, not in the workspace
	ctx := context.Background()
	e.srv.Resolver = orch.New(e.srv.Core, &recorder{}, nil)

	signal := func(typ, payload string) uuid.UUID {
		sig, err := e.store.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.New(), WorkspaceID: wsID, Type: typ, MemberID: &dev, Payload: []byte(payload)})
		if err != nil {
			t.Fatal(err)
		}
		return sig.ID
	}
	mint := func(user, sig uuid.UUID, ttl time.Duration, actions ...string) string {
		tok, hash := notify.NewToken()
		if err := e.store.CreatePushActionToken(ctx, db.CreatePushActionTokenParams{TokenHash: hash, UserID: user, SignalID: sig, Actions: actions, ExpiresAt: time.Now().Add(ttl)}); err != nil {
			t.Fatal(err)
		}
		return tok
	}
	act := func(c *client, token, action string) resp {
		return c.raw("POST", "/api/v1/notifications/act", map[string]any{"token": token, "action": action}, false) // no CSRF header, like a service worker
	}
	status := func(id uuid.UUID) string {
		s, _ := e.store.GetSignal(ctx, id)
		return s.Status
	}

	gate := signal("gate", `{"summary":"make build"}`)
	tok := mint(uid, gate, time.Hour, "allow", "deny")

	if r := act(e.client(), tok, "allow"); r.Status != http.StatusUnauthorized {
		t.Errorf("no session: %d", r.Status)
	}
	if r := act(outsider, tok, "allow"); r.Status != http.StatusForbidden || r.errCode() != "bad_token" {
		t.Errorf("someone else's token: %d %v", r.Status, r.Body)
	}
	if r := act(owner, tok, "always"); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("action not on the token: %d", r.Status)
	}
	if r := act(owner, "made-up", "allow"); r.Status != http.StatusForbidden {
		t.Errorf("unknown token: %d", r.Status)
	}
	if status(gate) != "open" {
		t.Fatal("signal closed by a refused attempt")
	}
	if r := act(owner, mint(uid, gate, -time.Minute, "allow"), "allow"); r.Status != http.StatusForbidden {
		t.Errorf("expired token: %d", r.Status)
	}
	// Refused attempts did not use the token up; the real one works once.
	if r := act(owner, tok, "allow"); r.Status != 200 || r.Body["status"] != "resolved" {
		t.Fatalf("allow: %d %v", r.Status, r.Body)
	}
	if status(gate) != "resolved" {
		t.Error("gate not resolved")
	}
	if r := act(owner, tok, "allow"); r.Status != http.StatusForbidden {
		t.Errorf("token reused: %d", r.Status)
	}
	// A new token for a signal that is already closed gives 409.
	if r := act(owner, mint(uid, gate, time.Hour, "allow"), "allow"); r.Status != http.StatusConflict {
		t.Errorf("closed signal: %d %v", r.Status, r.Body)
	}
	// The board hears about it with the signal's id.
	var n int
	_ = e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type = 'signal.resolved' AND data->>'id' = $1`, gate.String()).Scan(&n)
	if n != 1 {
		t.Errorf("signal.resolved events: %d", n)
	}

	// A token can't reach a signal its person can't see: resolving goes through the same checks as the board.
	g2 := signal("gate", `{"summary":"ls"}`)
	if r := act(outsider, mint(uuid.MustParse(outsiderID), g2, time.Hour, "allow"), "allow"); r.Status != http.StatusNotFound {
		t.Errorf("signal not visible: %d %v", r.Status, r.Body)
	}
	if status(g2) != "open" {
		t.Error("hidden signal was resolved")
	}

	// Owner rule: only the directory's owner can approve an owner change, token or not.
	oc := signal("owner_change", `{"path":"/api","owner_user_id":"`+outsiderID+`"}`)
	if r := act(owner, mint(uid, oc, time.Hour, "approve"), "approve"); r.Status == 200 || status(oc) != "open" {
		t.Errorf("admin approved someone else's folder: %d", r.Status)
	}

	// A question's button answers with the suggested option, chosen on the hub.
	q := signal("question", `{"question":"Which table?","options":["orders","payments"],"suggested":1}`)
	if r := act(owner, mint(uid, q, time.Hour, "answer"), "answer"); r.Status != 200 {
		t.Fatalf("answer: %d %v", r.Status, r.Body)
	}
	sig, _ := e.store.GetSignal(ctx, q)
	var res map[string]any
	_ = json.Unmarshal(sig.Resolution, &res)
	if res["text"] != "payments" {
		t.Errorf("resolution: %s", sig.Resolution)
	}
}

func TestQuickActions(t *testing.T) {
	cases := map[string][]string{"gate": {"allow", "deny"}, "dir_access": {"grant", "deny"}, "review": {"approve"}, "owner_change": {"approve", "deny"}}
	for typ, want := range cases {
		q := notify.QuickActions(db.Signal{Type: typ})
		if len(q.Allowed) != len(want) || len(q.Buttons) != 2 {
			t.Errorf("%s: %+v", typ, q)
		}
	}
	if q := notify.QuickActions(db.Signal{Type: "question", Payload: []byte(`{"options":["a","b"]}`)}); len(q.Buttons) != 0 {
		t.Errorf("question without suggestion: %+v", q)
	}
	if q := notify.QuickActions(db.Signal{Type: "question", Payload: []byte(`{"options":["a","b"],"suggested":0}`)}); len(q.Buttons) != 2 || q.Buttons[0].Title != "a" {
		t.Errorf("question: %+v", q)
	}
	if q := notify.QuickActions(db.Signal{Type: "error"}); len(q.Buttons) != 0 {
		t.Errorf("error: %+v", q)
	}
}

func TestWaitingCount(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Badge Co")
	ws, d := e.createWorkspace(owner, currentOrg(me), setupPayload())
	dev := uuid.MustParse(str(members(d)["Dev"]["id"]))
	ctx := context.Background()
	if got := owner.do("GET", "/api/v1/me", nil).Body["waiting"]; got != float64(0) {
		t.Fatalf("waiting at start: %v", got)
	}
	for _, typ := range []string{"gate", "question"} {
		if _, err := e.store.CreateSignal(ctx, db.CreateSignalParams{ID: uuid.New(), WorkspaceID: uuid.MustParse(ws), Type: typ, MemberID: &dev, Payload: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if got := owner.do("GET", "/api/v1/me", nil).Body["waiting"]; got != float64(2) {
		t.Errorf("waiting: %v", got)
	}
}
