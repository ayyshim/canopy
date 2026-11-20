// Package realtime fans domain events out to board sockets, across hub instances via Redis.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Event is one state change. WorkspaceID nil means organization-wide (accounts, runners).
type Event struct {
	Seq         int64           `json:"seq"`
	OrgID       uuid.UUID       `json:"org_id"`
	WorkspaceID *uuid.UUID      `json:"workspace_id,omitempty"`
	Type        string          `json:"type"`
	MemberID    *uuid.UUID      `json:"member_id,omitempty"`   // for staff visibility filtering
	ForUserID   *uuid.UUID      `json:"for_user_id,omitempty"` // also visible to this person (owner approvals)
	AdminOnly   bool            `json:"admin_only,omitempty"`
	Data        json.RawMessage `json:"data"`
	At          time.Time       `json:"at"`
	// Ephemeral events (presence, typing) aren't stored and have no Seq.
	Ephemeral bool       `json:"ephemeral,omitempty"`
	FromUser  *uuid.UUID `json:"from_user,omitempty"` // presence: who sent it
}

func (e Event) Channel() string {
	if e.WorkspaceID != nil {
		return "ws:" + e.WorkspaceID.String()
	}
	return "org:" + e.OrgID.String()
}

func WorkspaceChannel(id uuid.UUID) string { return "ws:" + id.String() }
func OrgChannel(id uuid.UUID) string       { return "org:" + id.String() }

type Bus interface {
	Publish(ctx context.Context, e Event)
	// Subscribe delivers events on the given channels until cancel is called.
	Subscribe(channels ...string) (<-chan Event, func())
}

// local fans out to subscribers in this process.
type local struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func newLocal() *local { return &local{subs: map[string]map[chan Event]struct{}{}} }

func (l *local) deliver(e Event) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for ch := range l.subs[e.Channel()] {
		select {
		case ch <- e:
		default: // slow consumer: drop; the socket resyncs from a snapshot when it notices a gap
		}
	}
}

func (l *local) Subscribe(channels ...string) (<-chan Event, func()) {
	ch := make(chan Event, 256)
	l.mu.Lock()
	for _, c := range channels {
		if l.subs[c] == nil {
			l.subs[c] = map[chan Event]struct{}{}
		}
		l.subs[c][ch] = struct{}{}
	}
	l.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			l.mu.Lock()
			for _, c := range channels {
				delete(l.subs[c], ch)
			}
			l.mu.Unlock()
		})
	}
}

// Memory is a single-process bus.
type Memory struct{ *local }

func NewMemory() *Memory { return &Memory{newLocal()} }

func (m *Memory) Publish(_ context.Context, e Event) { m.deliver(e) }

// Redis publishes to Redis and delivers what it receives from Redis to local subscribers,
// so every hub instance sees every event.
type Redis struct {
	*local
	client *redis.Client
}

func NewRedis(ctx context.Context, client *redis.Client) *Redis {
	r := &Redis{local: newLocal(), client: client}
	go r.listen(ctx)
	return r
}

func (r *Redis) Publish(ctx context.Context, e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		slog.ErrorContext(ctx, "encode event", "err", err)
		return
	}
	if err := r.client.Publish(ctx, "canopy:"+e.Channel(), b).Err(); err != nil {
		slog.ErrorContext(ctx, "publish event", "err", err)
		r.deliver(e) // still reach this instance's sockets
	}
}

func (r *Redis) listen(ctx context.Context) {
	for ctx.Err() == nil {
		sub := r.client.PSubscribe(ctx, "canopy:*")
		for msg := range sub.Channel() {
			var e Event
			if err := json.Unmarshal([]byte(msg.Payload), &e); err == nil {
				r.deliver(e)
			}
		}
		_ = sub.Close()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}
