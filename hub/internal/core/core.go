// Package core is the hub's domain layer: transactions that change state and emit events.
// REST handlers, the runner protocol and the orchestrator all go through it.
package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

type Service struct {
	Store *store.Store
	Bus   realtime.Bus
	Now   func() time.Time
}

// Tx is a transaction that can emit events; events are published only after commit.
type Tx struct {
	Q      *db.Queries
	events []realtime.Event
	after  []func()
}

// Emit stores an event in the transaction.
func (t *Tx) Emit(ctx context.Context, e realtime.Event, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e.Data = b
	row, err := t.Q.InsertEvent(ctx, db.InsertEventParams{
		OrgID: e.OrgID, WorkspaceID: e.WorkspaceID, Type: e.Type, MemberID: e.MemberID, AdminOnly: e.AdminOnly, Data: b,
	})
	if err != nil {
		return err
	}
	e.Seq, e.At = row.Seq, row.At
	t.events = append(t.events, e)
	return nil
}

// AfterCommit runs fn once the transaction has committed (e.g. telling a runner).
func (t *Tx) AfterCommit(fn func()) { t.after = append(t.after, fn) }

func (s *Service) Tx(ctx context.Context, fn func(t *Tx) error) error {
	var t *Tx
	err := s.Store.Tx(ctx, func(q *db.Queries) error {
		t = &Tx{Q: q}
		return fn(t)
	})
	if err != nil {
		return err
	}
	for _, e := range t.events {
		s.Bus.Publish(ctx, e)
	}
	for _, f := range t.after {
		f()
	}
	return nil
}

// WS builds a workspace event.
func WS(orgID, wsID uuid.UUID, typ string) realtime.Event {
	return realtime.Event{OrgID: orgID, WorkspaceID: &wsID, Type: typ}
}

// WSMember builds a workspace event that belongs to one agent.
func WSMember(orgID, wsID, memberID uuid.UUID, typ string) realtime.Event {
	return realtime.Event{OrgID: orgID, WorkspaceID: &wsID, Type: typ, MemberID: &memberID}
}

// Org builds an organization-wide event.
func Org(orgID uuid.UUID, typ string) realtime.Event {
	return realtime.Event{OrgID: orgID, Type: typ}
}
