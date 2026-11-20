// Package api serves the hub's REST API under /api/v1.
package api

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/blob"
	"canopy/hub/internal/config"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/pki"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/skills"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

type Server struct {
	Cfg      config.Config
	Store    *store.Store
	Core     *core.Service
	Bus      realtime.Bus
	Mail     mail.Sender
	Sessions *auth.Sessions
	CSRF     auth.CSRF
	Limiter  auth.Limiter
	Argon2   auth.Argon2Params
	Now      func() time.Time

	// NewOAuth builds a sign-in provider from the hub's stored settings; tests replace it.
	NewOAuth func(provider, clientID, secret, callback string) auth.OAuthProvider
	// SetupToken must accompany first-run hub setup.
	SetupToken string
	// Runners and Orch are wired by main once the runner listener and orchestrator exist.
	Runners  RunnerHub
	Orch     Orchestrator
	PKI      *pki.CA
	Blobs    blob.Store
	Resolver Resolver
	Mirror   any
	// Skills fetches library skills from GitHub; nil uses the defaults. Tests point it at a stub.
	Skills  *skills.Fetcher
	Version string

	// Single-binary mode: the embedded web app, client downloads and install script.
	WebApp, Download, Install http.Handler
	setupDone                 atomic.Bool
}

func New(cfg config.Config, st *store.Store, mailer mail.Sender, limiter auth.Limiter, bus realtime.Bus) *Server {
	s := &Server{
		Cfg:      cfg,
		Store:    st,
		Core:     &core.Service{Store: st, Bus: bus, Now: time.Now},
		Bus:      bus,
		Mail:     mailer,
		Sessions: &auth.Sessions{Store: st, TTL: cfg.SessionTTL, Secure: cfg.CookieSecure, TrustProxy: cfg.TrustProxy},
		CSRF:     auth.CSRF{Secure: cfg.CookieSecure},
		Limiter:  limiter,
		Argon2:   auth.DefaultArgon2,
		Now:      time.Now,
		NewOAuth: func(provider, clientID, secret, callback string) auth.OAuthProvider {
			if provider == "google" {
				return auth.NewGoogle(clientID, secret, callback)
			}
			return auth.NewGitHub(clientID, secret, callback)
		},
		SetupToken: cfg.SetupToken,
	}
	return s
}

// oauthCallback is the redirect URL to register with a provider.
func (s *Server) oauthCallback(provider string) string {
	return s.Cfg.PublicURL + "/api/v1/auth/oauth/" + provider + "/callback"
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if s.Install != nil {
		r.Handle("/install.sh", s.Install)
	}
	if s.Download != nil {
		r.Handle("/download/*", s.Download)
	}
	if s.WebApp != nil {
		r.NotFound(s.WebApp.ServeHTTP)
	}

	r.With(s.Sessions.Middleware, auth.RequireUser).Get("/ws/workspaces/{wsID}", s.handleBoardWS)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.Sessions.Middleware, s.CSRF.Middleware)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) { httpx.Fail(w, r, httpx.ErrNotFound) })

		// Available before the hub is set up.
		r.Get("/auth/csrf", s.handleCSRF)
		r.Get("/hub", httpx.Handler(s.handleGetHub))
		r.With(auth.RateLimit(s.Limiter, "setup", s.Cfg.AuthRateLimit, s.Cfg.TrustProxy)).
			Post("/hub/setup", httpx.Handler(s.handleHubSetup))
		r.With(auth.RateLimit(s.Limiter, "pair", s.Cfg.AuthRateLimit, s.Cfg.TrustProxy)).
			Post("/runners/pair", httpx.Handler(s.handlePair))

		// Everything else waits for first-run setup.
		r.Group(s.setupRoutes)
	})
	return r
}

func (s *Server) setupRoutes(r chi.Router) {
	r.Use(s.requireSetup)
	{
		r.Get("/auth/reset/{token}", httpx.Handler(s.handleResetCheck))
		r.Get("/invites/{token}", httpx.Handler(s.handleGetInvite))
		r.Post("/invites/{token}/decline", httpx.Handler(s.handleDeclineInvite))
		r.With(auth.RateLimit(s.Limiter, "invite", s.Cfg.AuthRateLimit, s.Cfg.TrustProxy)).
			Post("/invites/{token}/accept", httpx.Handler(s.handleAcceptInvite))
		r.Get("/auth/oauth/{provider}/callback", s.handleOAuthCallback)
		r.Post("/auth/logout", httpx.Handler(s.handleLogout))

		r.Group(func(r chi.Router) {
			r.Use(auth.RateLimit(s.Limiter, "auth", s.Cfg.AuthRateLimit, s.Cfg.TrustProxy))
			r.Post("/auth/signup", httpx.Handler(s.handleSignup))
			r.Post("/auth/login", httpx.Handler(s.handleLogin))
			r.Post("/auth/forgot", httpx.Handler(s.handleForgot))
			r.Post("/auth/reset", httpx.Handler(s.handleReset))
			r.Get("/auth/oauth/{provider}/start", s.handleOAuthStart)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireUser)
			r.Get("/me", httpx.Handler(s.handleGetMe))
			r.Patch("/me", httpx.Handler(s.handlePatchMe))
			r.Post("/orgs", httpx.Handler(s.handleCreateOrg))
			r.Get("/orgs/{orgID}", httpx.Handler(s.handleGetOrg))
			r.Patch("/orgs/{orgID}", httpx.Handler(s.handlePatchOrg))
			r.Delete("/orgs/{orgID}", httpx.Handler(s.handleDeleteOrg))
			r.Post("/orgs/{orgID}/transfer", httpx.Handler(s.handleTransferOrg))
			s.workspaceRoutes(r)
		})

		// Hub admin dashboard.
		r.Route("/admin", func(r chi.Router) {
			r.Use(auth.RequireUser, s.requireHubAdmin)
			r.Get("/overview", httpx.Handler(s.handleAdminOverview))
			r.Get("/organizations", httpx.Handler(s.handleAdminListOrgs))
			r.Get("/organizations/{orgID}", httpx.Handler(s.handleAdminGetOrg))
			r.Get("/users", httpx.Handler(s.handleAdminListUsers))
			r.Get("/users/{userID}", httpx.Handler(s.handleAdminGetUser))
			r.Patch("/users/{userID}", httpx.Handler(s.handleAdminPatchUser))
			r.Get("/settings", httpx.Handler(s.handleAdminGetSettings))
			r.Patch("/settings", httpx.Handler(s.handleAdminPatchSettings))
		})
	}
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// identity returns the signed-in user; routes behind RequireUser always have one.
func identity(r *http.Request) auth.Identity {
	id, _ := auth.FromContext(r.Context())
	return id
}

// pathParam reads a route parameter; the only place handlers touch the router.
func pathParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

// pathID parses a UUID path parameter; malformed IDs are reported as not found.
func pathID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(pathParam(r, name))
	if err != nil {
		return uuid.Nil, httpx.ErrNotFound
	}
	return id, nil
}

// audit records an event; orgID nil means a hub-level event.
func (s *Server) audit(ctx context.Context, q *db.Queries, orgID *uuid.UUID, actor *uuid.UUID, typ, summary string) error {
	return q.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		ID: newID(), OrgID: orgID, ActorUserID: actor, Type: typ, Summary: summary,
	})
}
