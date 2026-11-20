package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

const SessionCookie = "canopy_session"

// Identity is the signed-in user behind a request.
type Identity struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type ctxKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// Sessions issues and validates HTTP-only session cookies backed by auth_sessions rows.
// Sessions roll: each use more than an hour after the last extension pushes expiry out to TTL.
type Sessions struct {
	Store      *store.Store
	TTL        time.Duration
	Secure     bool
	TrustProxy bool
	Now        func() time.Time
}

const extendEvery = time.Hour

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start creates a session for userID and sets the cookie.
func (s *Sessions) Start(ctx context.Context, w http.ResponseWriter, r *http.Request, q *db.Queries, userID uuid.UUID) (Identity, error) {
	raw, hash := NewToken()
	sess, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID:        uuid.Must(uuid.NewV7()),
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: s.now().Add(s.TTL),
		UserAgent: truncate(r.UserAgent(), 300),
		Ip:        ClientIP(r, s.TrustProxy),
	})
	if err != nil {
		return Identity{}, err
	}
	s.setCookie(w, raw)
	return Identity{UserID: userID, SessionID: sess.ID}, nil
}

func (s *Sessions) setCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: raw, Path: "/", MaxAge: int(s.TTL.Seconds()),
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// Clear removes the session cookie.
func (s *Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// Middleware attaches the Identity when the request carries a live session cookie.
func (s *Sessions) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(SessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := s.Store.GetLiveSession(r.Context(), HashToken(c.Value))
		if err != nil {
			if !store.IsNotFound(err) {
				slog.ErrorContext(r.Context(), "load session", "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		if s.now().Sub(sess.LastSeenAt) > extendEvery {
			if err := s.Store.ExtendSession(r.Context(), db.ExtendSessionParams{ID: sess.ID, ExpiresAt: s.now().Add(s.TTL)}); err != nil {
				slog.ErrorContext(r.Context(), "extend session", "err", err)
			} else {
				s.setCookie(w, c.Value)
			}
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), Identity{UserID: sess.UserID, SessionID: sess.ID})))
	})
}

// RequireUser rejects requests without a signed-in user.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
