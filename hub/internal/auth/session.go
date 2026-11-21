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
	// TwoFactor is true once this session has passed the code check.
	TwoFactor bool
	// Enrolled is true when the person already has an authenticator set up.
	Enrolled bool
}

// Next is what a pending session must do: "enroll", "verify", or "" when it is done.
func (i Identity) Next() string {
	switch {
	case i.TwoFactor:
		return ""
	case i.Enrolled:
		return "verify"
	}
	return "enroll"
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
	return s.pending(ctx, q, Identity{UserID: userID, SessionID: sess.ID})
}

// pending fills in whether the person has an authenticator, which decides enroll or verify.
func (s *Sessions) pending(ctx context.Context, q *db.Queries, id Identity) (Identity, error) {
	u, err := q.GetUserByID(ctx, id.UserID)
	if err != nil {
		return Identity{}, err
	}
	id.Enrolled = u.TotpEnabledAt != nil
	return id, nil
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
		id := Identity{UserID: sess.UserID, SessionID: sess.ID, TwoFactor: sess.TwoFactorAt != nil}
		if !id.TwoFactor {
			if id, err = s.pending(r.Context(), s.Store.Queries, id); err != nil {
				slog.ErrorContext(r.Context(), "load user for session", "err", err)
				next.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}

// RequireSession rejects requests without a session, but lets one that still needs its
// two-factor code through. Only the two-factor endpoints use it.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			httpx.Fail(w, r, httpx.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireUser rejects requests without a signed-in user whose session passed the code check.
// A pending session gets 401 two_factor_required with next=enroll or next=verify.
func RequireUser(next http.Handler) http.Handler {
	return RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := FromContext(r.Context())
		if n := id.Next(); n != "" {
			e := httpx.NewError(http.StatusUnauthorized, "two_factor_required", "Enter your two-factor code to continue.")
			e.Next = n
			httpx.Fail(w, r, e)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
