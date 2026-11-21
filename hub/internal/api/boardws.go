package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/store/db"
)

// presence tracks who has each workspace's board open on this hub instance.
type presence struct {
	mu    sync.Mutex
	users map[uuid.UUID]map[uuid.UUID]*presenceUser // workspace → user
}

type presenceUser struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	AvatarColor string    `json:"avatar_color"`
	conns       int
}

var boardPresence = &presence{users: map[uuid.UUID]map[uuid.UUID]*presenceUser{}}

func (p *presence) join(ws uuid.UUID, u presenceUser) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.users[ws] == nil {
		p.users[ws] = map[uuid.UUID]*presenceUser{}
	}
	if cur := p.users[ws][u.ID]; cur != nil {
		cur.conns++
		return false
	}
	u.conns = 1
	p.users[ws][u.ID] = &u
	return true
}

func (p *presence) leave(ws, user uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.users[ws][user]
	if cur == nil {
		return false
	}
	cur.conns--
	if cur.conns > 0 {
		return false
	}
	delete(p.users[ws], user)
	return true
}

func (p *presence) list(ws uuid.UUID) []presenceUser {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []presenceUser{}
	for _, u := range p.users[ws] {
		out = append(out, *u)
	}
	return out
}

// boardConn is one open board: it forwards what this viewer may see.
type boardConn struct {
	s       *Server
	ws      db.Workspace
	user    db.User
	view    *wsView
	conn    *websocket.Conn
	wmu     sync.Mutex
	members map[uuid.UUID]db.Member
	mmu     sync.Mutex
}

type outMsg struct {
	Type string `json:"type"`
	Seq  int64  `json:"seq,omitempty"`
	Data any    `json:"data"`
}

func (b *boardConn) send(ctx context.Context, m outMsg) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b.wmu.Lock()
	defer b.wmu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.conn.Write(wctx, websocket.MessageText, data)
}

// handleBoardWS serves /ws/workspaces/{id}: a snapshot filtered to the viewer's access, then
// deltas (SPEC "Board WebSocket"). The client sends only presence.cursor and chat.typing.
func (s *Server) handleBoardWS(w http.ResponseWriter, r *http.Request) {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	uid := identity(r).UserID
	ctx := context.WithoutCancel(r.Context())
	view, err := s.loadWSView(ctx, wsID, uid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user, err := s.Store.GetUserByID(ctx, uid)
	if err != nil {
		http.Error(w, "unknown user", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b := &boardConn{s: s, ws: view.ws, user: user, view: view, conn: conn, members: view.members}

	// Subscribe before the snapshot so nothing between them is lost.
	events, unsubscribe := s.Bus.Subscribe(realtime.WorkspaceChannel(wsID), realtime.OrgChannel(view.ws.OrgID))
	defer unsubscribe()
	// Join first, so the snapshot's presence list includes this viewer.
	me := presenceUser{ID: user.ID, Name: user.Name, AvatarColor: user.AvatarColor}
	if boardPresence.join(wsID, me) {
		s.Bus.Publish(ctx, realtime.Event{OrgID: view.ws.OrgID, WorkspaceID: &wsID, Type: "presence.joined", Ephemeral: true, FromUser: &user.ID, Data: mustJSON(me)})
	}
	seq, err := b.sendSnapshot(ctx)
	if err != nil {
		return
	}
	defer func() {
		if boardPresence.leave(wsID, user.ID) {
			s.Bus.Publish(context.Background(), realtime.Event{OrgID: view.ws.OrgID, WorkspaceID: &wsID, Type: "presence.left", Ephemeral: true,
				FromUser: &user.ID, Data: mustJSON(map[string]any{"id": user.ID})})
		}
	}()

	go b.readLoop(ctx, cancel)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case e := <-events:
			if !e.Ephemeral && e.Seq <= seq {
				continue
			}
			if e.Type == "access.updated" || e.Type == "workspace.deleted" {
				// The viewer's access may have changed: rebuild their view.
				if e.Type == "workspace.deleted" {
					var d struct {
						ID uuid.UUID `json:"id"`
					}
					_ = json.Unmarshal(e.Data, &d)
					if d.ID == wsID {
						_ = b.send(ctx, outMsg{Type: "workspace.deleted"})
						return
					}
					continue
				}
				nv, err := s.loadWSView(ctx, wsID, uid)
				if err != nil {
					_ = b.send(ctx, outMsg{Type: "access.lost"})
					return
				}
				b.view = nv
				b.setMembers(nv.members)
				if seq, err = b.sendSnapshot(ctx); err != nil {
					return
				}
				continue
			}
			msg, ok := b.filter(ctx, e)
			if !ok {
				continue
			}
			if err := b.send(ctx, msg); err != nil {
				return
			}
		}
	}
}

func (b *boardConn) setMembers(m map[uuid.UUID]db.Member) {
	b.mmu.Lock()
	b.members = m
	b.mmu.Unlock()
}

func (b *boardConn) sendSnapshot(ctx context.Context) (int64, error) {
	snap, err := b.s.BoardSnapshot(ctx, b.ws.ID, b.user.ID)
	if err != nil {
		return 0, err
	}
	return snap.Seq, b.send(ctx, outMsg{Type: "snapshot", Seq: snap.Seq, Data: map[string]any{"board": snap, "presence": boardPresence.list(b.ws.ID)}})
}

// member returns a member, loading ones created after the snapshot.
func (b *boardConn) member(ctx context.Context, id uuid.UUID) (db.Member, bool) {
	b.mmu.Lock()
	m, ok := b.members[id]
	b.mmu.Unlock()
	if ok {
		return m, true
	}
	m, err := b.s.Store.GetMember(ctx, id)
	if err != nil || m.WorkspaceID != b.ws.ID {
		return m, false
	}
	b.mmu.Lock()
	b.members[id] = m
	b.mmu.Unlock()
	return m, true
}

func (b *boardConn) seesMemberID(ctx context.Context, id *uuid.UUID) bool {
	if id == nil {
		return true
	}
	m, ok := b.member(ctx, *id)
	return ok && b.view.v.CanSeeMember(m)
}

// canChatWith is false for an isolated staff member's chat unless the viewer created it.
func (b *boardConn) canChatWith(ctx context.Context, id uuid.UUID) bool {
	m, ok := b.member(ctx, id)
	return ok && (!m.Isolated || (m.CreatedBy != nil && *m.CreatedBy == b.user.ID))
}

// filter decides whether this viewer gets an event (SPEC: "Staff viewers receive only permitted
// nodes and cursors over the socket").
func (b *boardConn) filter(ctx context.Context, e realtime.Event) (outMsg, bool) {
	v := b.view.v
	if e.FromUser != nil && *e.FromUser == b.user.ID && e.Ephemeral {
		return outMsg{}, false // your own cursor and typing
	}
	if e.ToUserID != nil && *e.ToUserID != b.user.ID {
		return outMsg{}, false
	}
	forMe := e.ForUserID != nil && *e.ForUserID == b.user.ID
	if e.AdminOnly && !v.Admin() && !forMe {
		return outMsg{}, false
	}
	switch e.Type {
	case "usage.added":
		var u struct {
			UserID *uuid.UUID `json:"user_id"`
		}
		_ = json.Unmarshal(e.Data, &u)
		if !v.Admin() && (u.UserID == nil || *u.UserID != b.user.ID) {
			return outMsg{}, false
		}
	case "account.updated":
		// A computer's sign-in is its owner's business (admins see all; shared ones are everyone's).
		var a struct {
			RunnerID uuid.UUID `json:"runner_id"`
		}
		_ = json.Unmarshal(e.Data, &a)
		if rn, err := b.s.Store.GetRunner(ctx, a.RunnerID); !v.Admin() && (err != nil || (rn.OwnerUserID != b.user.ID && !rn.Shared)) {
			return outMsg{}, false
		}
	case "member.updated":
		var d struct {
			Member struct {
				ID uuid.UUID `json:"id"`
			} `json:"member"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if m, err := b.s.Store.GetMember(ctx, d.Member.ID); err == nil {
			b.mmu.Lock()
			b.members[m.ID] = m
			b.mmu.Unlock()
			if !v.CanSeeMember(m) {
				return outMsg{Type: "member.updated", Seq: e.Seq, Data: map[string]any{"member": core.ToHiddenMember(m)}}, true
			}
		}
	case "presence.cursor":
		var c struct {
			Over *uuid.UUID `json:"over_member_id"`
		}
		_ = json.Unmarshal(e.Data, &c)
		if !b.seesMemberID(ctx, c.Over) {
			return outMsg{}, false
		}
	}
	if e.MemberID != nil && !forMe && !b.seesMemberID(ctx, e.MemberID) {
		return outMsg{}, false
	}
	if e.MemberID != nil && strings.HasPrefix(e.Type, "chat.") && !b.canChatWith(ctx, *e.MemberID) {
		return outMsg{}, false
	}
	return outMsg{Type: e.Type, Seq: e.Seq, Data: json.RawMessage(e.Data)}, true
}

// readLoop takes the client's cursor and typing messages and shares them (never stored).
func (b *boardConn) readLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	b.conn.SetReadLimit(64 << 10)
	var last time.Time
	for {
		_, data, err := b.conn.Read(ctx)
		if err != nil {
			return
		}
		var in struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.Type {
		case "presence.cursor":
			if time.Since(last) < 60*time.Millisecond { // cursors at up to ~15 Hz
				continue
			}
			last = time.Now()
			var c struct {
				X    float64    `json:"x"`
				Y    float64    `json:"y"`
				Over *uuid.UUID `json:"over_member_id"`
			}
			if json.Unmarshal(in.Data, &c) != nil {
				continue
			}
			out := map[string]any{"user_id": b.user.ID, "name": b.user.Name, "avatar_color": b.user.AvatarColor, "x": c.X, "y": c.Y, "over_member_id": c.Over}
			b.s.Bus.Publish(ctx, realtime.Event{OrgID: b.ws.OrgID, WorkspaceID: &b.ws.ID, Type: "presence.cursor", Ephemeral: true, FromUser: &b.user.ID, Data: mustJSON(out)})
		case "chat.typing":
			var t struct {
				MemberID uuid.UUID `json:"member_id"`
			}
			if json.Unmarshal(in.Data, &t) != nil || !b.seesMemberID(ctx, &t.MemberID) || !b.canChatWith(ctx, t.MemberID) {
				continue
			}
			b.s.Bus.Publish(ctx, realtime.Event{OrgID: b.ws.OrgID, WorkspaceID: &b.ws.ID, Type: "chat.typing", Ephemeral: true, FromUser: &b.user.ID,
				MemberID: &t.MemberID, Data: mustJSON(map[string]any{"user_id": b.user.ID, "name": b.user.Name, "member_id": t.MemberID})})
		}
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode", "err", err)
	}
	return b
}

var _ = authz.Viewer{}
