package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

const (
	pushTTL     = 24 * 3600 // seconds a push service keeps an undelivered message
	tokenLife   = 24 * time.Hour
	pushTimeout = 15 * time.Second
)

// PushMessage is the JSON the service worker receives.
type PushMessage struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Tag      string   `json:"tag"`
	URL      string   `json:"url"`
	Icon     string   `json:"icon"`
	Image    string   `json:"image,omitempty"`
	Actions  []Button `json:"actions"`
	Token    string   `json:"token,omitempty"`
	SignalID string   `json:"signal_id,omitempty"`
	Kind     string   `json:"kind,omitempty"`
}

// PushChannel sends outbox rows as web push to the recipients' browsers.
type PushChannel struct {
	Store     *store.Store
	VAPID     VAPID
	PublicURL string
	Client    *http.Client
	Now       func() time.Time
}

func NewPushChannel(s *store.Store, v VAPID, publicURL string) *PushChannel {
	return &PushChannel{Store: s, VAPID: v, PublicURL: strings.TrimRight(publicURL, "/"), Client: &http.Client{Timeout: pushTimeout}, Now: time.Now}
}

// Send pushes one row to every recipient who has push on for its kind and isn't snoozed.
// It fails (so the dispatcher retries) only when every attempt failed for a temporary reason.
func (c *PushChannel) Send(ctx context.Context, row db.NotificationOutbox) error {
	q := c.Store.Queries
	var sig *db.Signal
	if row.SignalID != nil {
		if s, err := q.GetSignal(ctx, *row.SignalID); err == nil {
			if s.Status != "open" {
				return nil // answered before we got to it
			}
			sig = &s
		}
	}
	var ok, transient int
	var lastErr error
	for _, uid := range row.Recipients {
		u, err := q.GetUserByID(ctx, uid)
		if err != nil || !Parse(u.Notifications)[row.Kind].Push || (u.SnoozeUntil != nil && u.SnoozeUntil.After(c.Now())) {
			continue
		}
		subs, err := q.ListUserPushSubscriptions(ctx, uid)
		if err != nil {
			return err
		}
		if len(subs) == 0 {
			continue
		}
		msg := c.message(row)
		if sig != nil {
			msg.SignalID = sig.ID.String()
			if quick := QuickActions(*sig); len(quick.Buttons) > 0 {
				token, hash := NewToken()
				if err := q.CreatePushActionToken(ctx, db.CreatePushActionTokenParams{TokenHash: hash, UserID: uid, SignalID: sig.ID, Actions: quick.Allowed, ExpiresAt: c.Now().Add(tokenLife)}); err != nil {
					return err
				}
				msg.Token, msg.Actions = token, quick.Buttons
			}
		}
		urgency := webpush.UrgencyNormal
		if row.Kind == "questions" {
			urgency = webpush.UrgencyHigh
		}
		for _, sub := range subs {
			err := c.deliver(ctx, sub, msg, urgency)
			switch {
			case err == nil:
				ok++
			case errors.Is(err, errGone):
			default:
				transient++
				lastErr = err
			}
		}
	}
	if ok == 0 && transient > 0 {
		return lastErr
	}
	return nil
}

var errGone = errors.New("subscription is gone")

func (c *PushChannel) message(row db.NotificationOutbox) PushMessage {
	tag := row.ID.String()
	if row.SignalID != nil {
		tag = row.SignalID.String()
	}
	return PushMessage{Title: row.Title, Body: row.Body, Tag: tag, URL: row.Link, Icon: c.PublicURL + "/icons/icon-192.png", Actions: []Button{}, Kind: row.Kind}
}

// deliver sends one message to one subscription and drops the subscription when the push
// service says it no longer exists. Errors never include the endpoint (it is a secret).
func (c *PushChannel) deliver(ctx context.Context, sub db.PushSubscription, msg PushMessage, urgency webpush.Urgency) error {
	body, _ := json.Marshal(msg)
	res, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}}, &webpush.Options{
		HTTPClient: c.Client, Subscriber: c.PublicURL, VAPIDPublicKey: c.VAPID.Public, VAPIDPrivateKey: c.VAPID.Private,
		TTL: pushTTL, Urgency: urgency, Topic: "",
	})
	if err != nil {
		return errors.New("push request failed")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		if err := c.Store.Queries.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint); err != nil {
			slog.WarnContext(ctx, "drop push subscription", "err", err)
		}
		return errGone
	case res.StatusCode >= 200 && res.StatusCode < 300:
		_ = c.Store.Queries.TouchPushSubscription(ctx, sub.Endpoint)
		return nil
	}
	return fmt.Errorf("push service answered %d", res.StatusCode)
}

// SendTest pushes a sample message to all of one person's browsers and returns how many took it.
func (c *PushChannel) SendTest(ctx context.Context, userID uuid.UUID) (int, error) {
	subs, err := c.Store.Queries.ListUserPushSubscriptions(ctx, userID)
	if err != nil {
		return 0, err
	}
	msg := PushMessage{Title: "Canopy - Notifications", Body: "Notifications are on for this device.", Tag: "canopy-test", URL: "/", Icon: c.PublicURL + "/icons/icon-192.png", Actions: []Button{}}
	n := 0
	for _, sub := range subs {
		if c.deliver(ctx, sub, msg, webpush.UrgencyNormal) == nil {
			n++
		}
	}
	return n, nil
}
