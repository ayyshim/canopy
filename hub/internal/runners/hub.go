// Package runners serves the runner endpoint: a mutual-TLS WebSocket per paired machine,
// plus blob upload/download for outputs and mirrored directories.
package runners

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"canopy/hub/internal/blob"
	"canopy/hub/internal/pki"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

// Handler receives runner traffic; the orchestrator implements it.
type Handler interface {
	RunnerConnected(ctx context.Context, r db.Runner, hello protocol.Hello)
	RunnerDisconnected(ctx context.Context, r db.Runner)
	RunnerMessage(ctx context.Context, r db.Runner, env protocol.Envelope)
}

type Hub struct {
	Store   *store.Store
	CA      *pki.CA
	Blobs   blob.Store
	Handler Handler

	mu    sync.RWMutex
	conns map[uuid.UUID]*conn
	seq   atomic.Uint64
}

func New(st *store.Store, ca *pki.CA, blobs blob.Store) *Hub {
	return &Hub{Store: st, CA: ca, Blobs: blobs, conns: map[uuid.UUID]*conn{}}
}

type conn struct {
	runnerID uuid.UUID
	ws       *websocket.Conn
	wmu      sync.Mutex
	pmu      sync.Mutex
	pending  map[string]chan protocol.Envelope
	cancel   context.CancelFunc
}

var ErrOffline = errors.New("runner is offline")

func (h *Hub) Online(id uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[id]
	return ok
}

func (h *Hub) get(id uuid.UUID) *conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

func (c *conn) write(ctx context.Context, env protocol.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, b)
}

// Send delivers a message without waiting for a reply.
func (h *Hub) Send(id uuid.UUID, typ string, data any) error {
	c := h.get(id)
	if c == nil {
		return ErrOffline
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.write(context.Background(), protocol.Envelope{Type: typ, Data: b})
}

// Request sends a message and waits for the runner's reply.
func (h *Hub) Request(ctx context.Context, id uuid.UUID, typ string, data any, out any) error {
	c := h.get(id)
	if c == nil {
		return ErrOffline
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	reqID := fmt.Sprintf("h%d", h.seq.Add(1))
	ch := make(chan protocol.Envelope, 1)
	c.pmu.Lock()
	c.pending[reqID] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, reqID)
		c.pmu.Unlock()
	}()
	if err := c.write(ctx, protocol.Envelope{Type: typ, ID: reqID, Data: b}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case env := <-ch:
		if env.Error != "" {
			return errors.New(env.Error)
		}
		if out != nil {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runnerFromTLS identifies the machine by its client certificate.
func (h *Hub) runnerFromTLS(r *http.Request) (db.Runner, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return db.Runner{}, errors.New("no client certificate")
	}
	fp := pki.Fingerprint(r.TLS.PeerCertificates[0].Raw)
	return h.Store.GetRunnerByFingerprint(r.Context(), &fp)
}

// Routes is the runner listener's handler.
func (h *Hub) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/runners", h.serveWS)
	mux.HandleFunc("PUT /runner/v1/blobs/{sha}", h.putBlob)
	mux.HandleFunc("GET /runner/v1/blobs/{sha}", h.getBlob)
	mux.HandleFunc("POST /runner/v1/blobs/missing", h.missingBlobs)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

// TLSConfig requires a client certificate signed by the hub CA.
func (h *Hub) TLSConfig(hosts []string) (*tls.Config, error) {
	cert, err := h.CA.ServerCert(hosts)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    h.CA.Pool(),
		MinVersion:   tls.VersionTLS13,
	}, nil
}

const heartbeatTimeout = 35 * time.Second

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	rn, err := h.runnerFromTLS(r)
	if err != nil {
		http.Error(w, "unknown machine", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(32 << 20)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	c := &conn{runnerID: rn.ID, ws: ws, pending: map[string]chan protocol.Envelope{}, cancel: cancel}

	// The first message must be hello.
	hctx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	_, data, err := ws.Read(hctx)
	hcancel()
	if err != nil {
		ws.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	var env protocol.Envelope
	var hello protocol.Hello
	if json.Unmarshal(data, &env) != nil || env.Type != protocol.THello || json.Unmarshal(env.Data, &hello) != nil {
		ws.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	if hello.Version != protocol.Version {
		ws.Close(websocket.StatusPolicyViolation, fmt.Sprintf("hub speaks protocol %d; update canopy", protocol.Version))
		return
	}

	h.mu.Lock()
	if old := h.conns[rn.ID]; old != nil {
		old.cancel()
		old.ws.Close(websocket.StatusNormalClosure, "replaced by a newer connection")
	}
	h.conns[rn.ID] = c
	h.mu.Unlock()
	slog.Info("runner connected", "runner", rn.ID, "name", rn.Name, "host", hello.Hostname)
	if h.Handler != nil {
		h.Handler.RunnerConnected(ctx, rn, hello)
	}
	defer func() {
		h.mu.Lock()
		current := h.conns[rn.ID] == c
		if current {
			delete(h.conns, rn.ID)
		}
		h.mu.Unlock()
		if current {
			slog.Info("runner disconnected", "runner", rn.ID)
			if h.Handler != nil {
				h.Handler.RunnerDisconnected(context.Background(), rn)
			}
		}
	}()

	for {
		rctx, rcancel := context.WithTimeout(ctx, heartbeatTimeout)
		_, data, err := ws.Read(rctx)
		rcancel()
		if err != nil {
			ws.Close(websocket.StatusGoingAway, "")
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if env.Type == protocol.TReply {
			c.pmu.Lock()
			ch := c.pending[env.ReplyTo]
			c.pmu.Unlock()
			if ch != nil {
				ch <- env
			}
			continue
		}
		if env.Type == protocol.THeartbeat {
			_ = h.Store.TouchRunner(ctx, rn.ID)
			continue
		}
		if h.Handler != nil {
			h.Handler.RunnerMessage(ctx, rn, env)
		}
	}
}

func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// putBlob stores content by its SHA-256; the hash is checked.
func (h *Hub) putBlob(w http.ResponseWriter, r *http.Request) {
	if _, err := h.runnerFromTLS(r); err != nil {
		http.Error(w, "unknown machine", http.StatusUnauthorized)
		return
	}
	sha := r.PathValue("sha")
	if !validSHA(sha) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	key := blob.ContentKey(sha)
	if ok, _ := h.Blobs.Exists(r.Context(), key); ok {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	const max = 512 << 20
	tmp, err := os.CreateTemp("", "canopy-blob-*")
	if err != nil {
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(hasher, tmp), http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		http.Error(w, "upload failed", http.StatusBadRequest)
		return
	}
	if hex.EncodeToString(hasher.Sum(nil)) != sha {
		http.Error(w, "hash mismatch", http.StatusBadRequest)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	if err := h.Blobs.Put(r.Context(), key, tmp, size, "application/octet-stream"); err != nil {
		slog.Error("store blob", "err", err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Hub) getBlob(w http.ResponseWriter, r *http.Request) {
	if _, err := h.runnerFromTLS(r); err != nil {
		http.Error(w, "unknown machine", http.StatusUnauthorized)
		return
	}
	sha := r.PathValue("sha")
	if !validSHA(sha) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	rc, size, err := h.Blobs.Get(r.Context(), blob.ContentKey(sha))
	if errors.Is(err, blob.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, rc)
}

// missingBlobs tells a runner which of its files the hub doesn't have yet.
func (h *Hub) missingBlobs(w http.ResponseWriter, r *http.Request) {
	if _, err := h.runnerFromTLS(r); err != nil {
		http.Error(w, "unknown machine", http.StatusUnauthorized)
		return
	}
	var in protocol.BlobsMissing
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out := protocol.BlobsMissing{SHA256: []string{}}
	for _, sha := range in.SHA256 {
		if !validSHA(sha) {
			continue
		}
		if ok, err := h.Blobs.Exists(r.Context(), blob.ContentKey(sha)); err == nil && !ok {
			out.SHA256 = append(out.SHA256, sha)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
