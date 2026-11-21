package notify

import (
	"context"
	"log/slog"
	"time"

	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Channel delivers one outbox row (Discord, web push). Return an error to retry later.
type Channel interface {
	Send(ctx context.Context, row db.NotificationOutbox) error
}

// Noop accepts every row; it stands in until a real channel is plugged in.
type Noop struct{}

func (Noop) Send(context.Context, db.NotificationOutbox) error { return nil }

const (
	maxAttempts = 5
	batchSize   = 20
	keepFor     = 30 * 24 * time.Hour
)

// Dispatcher sends queued notifications through each channel and removes old ones.
type Dispatcher struct {
	Store   *store.Store
	Discord Channel
	Push    Channel
	Now     func() time.Time
	// Backoff is the wait after the first failure; it doubles for each further one.
	Backoff time.Duration
}

func NewDispatcher(s *store.Store) *Dispatcher {
	return &Dispatcher{Store: s, Discord: Noop{}, Push: Noop{}, Now: time.Now, Backoff: 30 * time.Second}
}

// Run sends what is due every few seconds until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	cleaned := time.Time{}
	for {
		d.Pass(ctx)
		if d.Now().Sub(cleaned) > time.Hour {
			cleaned = d.Now()
			_ = d.Store.Queries.DeleteExpiredPushActionTokens(ctx, d.Now())
			if _, err := d.Store.Queries.DeleteOldNotifications(ctx, d.Now().Add(-keepFor)); err != nil {
				slog.WarnContext(ctx, "delete old notifications", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass does one round for both channels.
func (d *Dispatcher) Pass(ctx context.Context) {
	d.pass(ctx, d.Discord, "discord")
	d.pass(ctx, d.Push, "push")
}

func (d *Dispatcher) pass(ctx context.Context, ch Channel, name string) {
	if ch == nil {
		return
	}
	// Rows stay locked while we send, so a second hub skips them.
	err := d.Store.Tx(ctx, func(q *db.Queries) error {
		now := d.Now()
		var rows []db.NotificationOutbox
		var err error
		if name == "discord" {
			rows, err = q.ClaimDiscordNotifications(ctx, db.ClaimDiscordNotificationsParams{DiscordAttempts: maxAttempts, DiscordNextAt: now, Limit: batchSize})
		} else {
			rows, err = q.ClaimPushNotifications(ctx, db.ClaimPushNotificationsParams{PushAttempts: maxAttempts, PushNextAt: now, Limit: batchSize})
		}
		if err != nil {
			return err
		}
		for _, row := range rows {
			sendErr := ch.Send(ctx, row)
			if err := d.record(ctx, q, name, row, sendErr, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.WarnContext(ctx, "dispatch notifications", "channel", name, "err", err)
	}
}

func (d *Dispatcher) record(ctx context.Context, q *db.Queries, name string, row db.NotificationOutbox, sendErr error, now time.Time) error {
	attempts := row.DiscordAttempts
	if name == "push" {
		attempts = row.PushAttempts
	}
	if sendErr == nil {
		if name == "discord" {
			return q.MarkDiscordSent(ctx, row.ID)
		}
		return q.MarkPushSent(ctx, row.ID)
	}
	msg := sendErr.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	next := now.Add(d.Backoff << attempts)
	if name == "discord" {
		return q.MarkDiscordFailed(ctx, db.MarkDiscordFailedParams{ID: row.ID, LastError: msg, NextAt: next})
	}
	return q.MarkPushFailed(ctx, db.MarkPushFailedParams{ID: row.ID, LastError: msg, NextAt: next})
}
