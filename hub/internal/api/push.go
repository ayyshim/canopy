package api

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) error {
	if s.VAPID.Public == "" {
		return httpx.ErrNotFound
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"key": s.VAPID.Public})
	return nil
}

type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handlePushSubscribe saves this browser for the current person. The same endpoint sent again
// updates its keys; an endpoint that belonged to someone else (a shared browser) moves to the
// person who signed in now.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) error {
	var in pushSubscription
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	u, err := url.Parse(in.Endpoint)
	v := httpx.Validation{}
	if err != nil || !s.pushEndpointOK(u) || len(in.Endpoint) > 2048 {
		v.Add("endpoint", "That isn’t a push address.")
	}
	if in.Keys.P256dh == "" || in.Keys.Auth == "" || len(in.Keys.P256dh) > 200 || len(in.Keys.Auth) > 100 {
		v.Add("keys", "The browser’s keys are missing.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if _, err := s.Store.UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{ID: newID(), UserID: identity(r).UserID,
		Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth, UserAgent: ua}); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"subscribed": true})
	return nil
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	// Someone else's endpoint matches nothing, so it looks the same as one that isn't saved.
	if _, err := s.Store.DeletePushSubscription(r.Context(), db.DeletePushSubscriptionParams{UserID: identity(r).UserID, Endpoint: in.Endpoint}); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"subscribed": false})
	return nil
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) error {
	if s.Push == nil {
		return httpx.NewError(http.StatusServiceUnavailable, "unavailable", "Push isn’t set up on this hub.")
	}
	n, err := s.Push.SendTest(r.Context(), identity(r).UserID)
	if err != nil {
		return err
	}
	if n == 0 {
		return httpx.NewError(http.StatusConflict, "no_device", "No device took the test. Turn on push for this device first.")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sent": n})
	return nil
}

// handleNotificationAct carries out a button pressed on a push notification. It is exempt from
// the CSRF header (a service worker has none) but needs the person's session and the one-use
// token that was sent to them for this signal.
func (s *Server) handleNotificationAct(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token  string `json:"token"`
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx, uid := r.Context(), identity(r).UserID
	badToken := httpx.NewError(http.StatusForbidden, "bad_token", "This button has expired. Open Canopy to answer.")
	var tok db.PushActionToken
	// A refused attempt rolls back, so a wrong person or action doesn't use the token up.
	err := s.Store.Tx(ctx, func(q *db.Queries) error {
		var err error
		if tok, err = q.TakePushActionToken(ctx, db.TakePushActionTokenParams{TokenHash: notify.HashToken(strings.TrimSpace(in.Token)), ExpiresAt: s.now()}); err != nil {
			if store.IsNotFound(err) {
				return badToken
			}
			return err
		}
		if tok.UserID != uid {
			return badToken
		}
		if !slices.Contains(tok.Actions, in.Action) {
			return httpx.Validation{"action": "That button isn’t allowed here."}.Err()
		}
		return nil
	})
	if err != nil {
		return err
	}
	sig, err := s.Store.GetSignal(ctx, tok.SignalID)
	if err != nil {
		return err
	}
	res := orch.Resolution{Action: in.Action}
	if in.Action == "answer" {
		text, ok := notify.SuggestedAnswer(sig)
		if !ok {
			return httpx.Validation{"action": "That question has no suggested answer."}.Err()
		}
		res.Text = text
	}
	out, err := s.resolveSignalAs(ctx, uid, sig.ID, res)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"signal_id": out.ID, "status": out.Status})
	return nil
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// pushServices are the hosts browsers use for web push. The hub posts to the endpoint a browser
// gives it, so on a real (https) hub only these are accepted; local development takes any.
var pushServices = []string{"fcm.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com", "push.microsoft.com"}

func (s *Server) pushEndpointOK(u *url.URL) bool {
	if u == nil || u.Host == "" || u.User != nil {
		return false
	}
	if !s.Cfg.CookieSecure {
		return u.Scheme == "https" || u.Scheme == "http"
	}
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && slices.ContainsFunc(pushServices, func(d string) bool { return h == d || strings.HasSuffix(h, "."+d) })
}
