// Package conn keeps one mutual-TLS WebSocket to a hub open, reconnecting with backoff.
package conn

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"canopy/shared/protocol"
)

// Handler receives the hub's messages. Requests (env.ID set) must be answered with Reply.
type Handler interface {
	Hello() protocol.Hello
	Handle(ctx context.Context, c *Conn, env protocol.Envelope)
	Connected(ctx context.Context, c *Conn)
}

type Conn struct {
	URL string
	TLS *tls.Config

	mu      sync.Mutex
	ws      *websocket.Conn
	up      atomic.Bool
	revoked atomic.Bool
}

var ErrNotConnected = errors.New("not connected to the hub")

// Send writes a message; it fails while disconnected (callers resend state on reconnect).
func (c *Conn) Send(typ string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.write(protocol.Envelope{Type: typ, Data: b})
}

func (c *Conn) Reply(req protocol.Envelope, data any, err error) error {
	env := protocol.Envelope{Type: protocol.TReply, ReplyTo: req.ID}
	if err != nil {
		env.Error = err.Error()
	} else {
		b, merr := json.Marshal(data)
		if merr != nil {
			return merr
		}
		env.Data = b
	}
	return c.write(env)
}

func (c *Conn) write(env protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil {
		return ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

func (c *Conn) Up() bool      { return c.up.Load() }
func (c *Conn) Revoked() bool { return c.revoked.Load() }
func (c *Conn) MarkRevoked()  { c.revoked.Store(true) }

// HTTPClient is an HTTPS client that presents this machine's certificate (blobs).
func (c *Conn) HTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{TLSClientConfig: c.TLS}}
}

// Run connects and reconnects until ctx ends or the machine is disconnected in Canopy.
func (c *Conn) Run(ctx context.Context, h Handler) {
	backoff := time.Second
	for ctx.Err() == nil && !c.revoked.Load() {
		start := time.Now()
		err := c.session(ctx, h)
		if ctx.Err() != nil || c.revoked.Load() {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		slog.Warn("disconnected from hub; reconnecting", "url", c.URL, "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *Conn) session(ctx context.Context, h Handler) error {
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(dctx, c.URL, &websocket.DialOptions{HTTPClient: c.HTTPClient()})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("the hub doesn’t recognize this machine (disconnected in Canopy?)")
		}
		return err
	}
	ws.SetReadLimit(64 << 20)
	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	defer func() {
		c.up.Store(false)
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
		ws.CloseNow()
	}()
	if err := c.Send(protocol.THello, h.Hello()); err != nil {
		return err
	}
	c.up.Store(true)
	slog.Info("connected to hub", "url", c.URL)
	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				_ = c.Send(protocol.THeartbeat, struct{}{})
			}
		}
	}()
	h.Connected(sctx, c)
	for {
		_, data, err := ws.Read(sctx)
		if err != nil {
			return err
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if env.Type == protocol.TRevoked {
			c.revoked.Store(true)
			slog.Error("this machine was disconnected in Canopy; pair it again to reconnect")
			return nil
		}
		go h.Handle(sctx, c, env)
	}
}
