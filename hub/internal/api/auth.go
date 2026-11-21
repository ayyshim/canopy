package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

var (
	errUnknownEmail = httpx.FieldError(http.StatusUnauthorized, "unknown_email", "email", "We couldn’t find an account with that email.")
	errWrongPass    = httpx.FieldError(http.StatusUnauthorized, "wrong_password", "password", "That password isn’t right.")
	errNoPassword   = httpx.FieldError(http.StatusUnauthorized, "no_password", "password", "This account signs in with GitHub or Google.")
	errEmailTaken   = httpx.FieldError(http.StatusConflict, "email_taken", "user.email", "An account already uses this email.")
	errInvalidReset = httpx.NewError(http.StatusBadRequest, "invalid_token", "This reset link has expired or was already used.")
	errSignupClosed = httpx.NewError(http.StatusForbidden, "signups_closed", "This hub isn’t taking new organizations. Ask the hub admin for an invitation.")
)

func (s *Server) handleCSRF(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"token": s.CSRF.Token(w, r)})
}

// oauthProvider returns the enabled, configured provider with that name.
func (s *Server) oauthProvider(ctx context.Context, name string) (auth.OAuthProvider, bool) {
	p, err := s.Store.GetAuthProvider(ctx, name)
	if err != nil || !p.Enabled || p.ClientID == "" || p.ClientSecret == "" {
		return nil, false
	}
	return s.NewOAuth(name, p.ClientID, p.ClientSecret, s.oauthCallback(name)), true
}

type signupRequest struct {
	Org  orgInput `json:"org"`
	User struct {
		Name        string `json:"name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		AcceptTerms bool   `json:"accept_terms"`
	} `json:"user"`
}

// handleSignup creates the organization, then its owner, the owner membership and default
// limits in one transaction, and signs the owner in.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) error {
	var req signupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	req.Org.validate(v, "org.")
	req.User.Name = strings.TrimSpace(req.User.Name)
	req.User.Email = normalizeEmail(req.User.Email)
	if req.User.Name == "" {
		v.Add("user.name", "Enter your name.")
	}
	if !validEmail(req.User.Email) {
		v.Add("user.email", "Enter a valid work email.")
	}
	checkPassword(v, "user.password", req.User.Password)
	if !req.User.AcceptTerms {
		v.Add("user.accept_terms", "Accept the terms to continue.")
	}
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	hub, err := s.Store.GetHubSettings(ctx)
	if err != nil {
		return err
	}
	if !hub.SignupOpen {
		return errSignupClosed
	}
	if _, err := s.Store.GetUserByEmail(ctx, req.User.Email); err == nil {
		return errEmailTaken
	} else if !store.IsNotFound(err) {
		return err
	}
	hash, err := auth.HashPassword(s.Argon2, req.User.Password)
	if err != nil {
		return err
	}

	userID := newID()
	var next string
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		org, err := s.createOrg(ctx, q, req.Org, userID)
		if err != nil {
			return err
		}
		if _, err := q.CreateUser(ctx, db.CreateUserParams{
			ID: userID, Email: req.User.Email, Name: req.User.Name, PasswordHash: &hash,
			AvatarColor: avatarColorFor(req.User.Email), LastOrgID: &org.ID,
		}); err != nil {
			return err
		}
		if _, err := q.CreateMembership(ctx, db.CreateMembershipParams{
			ID: newID(), OrgID: org.ID, UserID: userID, OrgRole: "owner", Status: "active",
		}); err != nil {
			return err
		}
		if err := s.audit(ctx, q, &org.ID, &userID, "organization.created",
			fmt.Sprintf("%s created the organization", req.User.Name)); err != nil {
			return err
		}
		id, err := s.Sessions.Start(ctx, w, r, q, userID)
		next = id.Next()
		return err
	})
	if store.IsUniqueViolation(err, "users_email_key") {
		return errEmailTaken
	}
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, twoFactorNext(next))
	return nil
}

// createOrg inserts an organization owned by ownerID with the configured default limits.
// A taken slug (including one claimed by a concurrent sign-up) gets a random suffix.
func (s *Server) createOrg(ctx context.Context, q *db.Queries, in orgInput, ownerID uuid.UUID) (db.Organization, error) {
	base := slugify(in.Name)
	slug := base
	for range 8 {
		org, err := q.CreateOrganization(ctx, db.CreateOrganizationParams{
			ID: newID(), Name: in.Name, Slug: slug, Size: in.Size, About: in.About, OwnerUserID: ownerID,
			DefaultSessionLimit: s.Cfg.DefaultSessionLimit, DefaultWeeklyLimit: s.Cfg.DefaultWeeklyLimit,
		})
		if !store.IsNotFound(err) { // no row means the slug was taken
			return org, err
		}
		slug = base + "-" + randomSuffix()
	}
	return db.Organization{}, fmt.Errorf("could not find a free slug for %q", base)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Email = normalizeEmail(req.Email)
	v := httpx.Validation{}
	if !validEmail(req.Email) {
		v.Add("email", "Enter a valid email.")
	}
	if req.Password == "" {
		v.Add("password", "Enter your password.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.Store.GetUserByEmail(ctx, req.Email)
	if store.IsNotFound(err) {
		return errUnknownEmail
	} else if err != nil {
		return err
	}
	if u.PasswordHash == nil {
		return errNoPassword
	}
	ok, err := auth.VerifyPassword(req.Password, *u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return errWrongPass
	}
	id, err := s.Sessions.Start(ctx, w, r, s.Store.Queries, u.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, twoFactorNext(id.Next()))
	return nil
}

// handleLogout ends this session, or with {"everywhere": true} every session of the person.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Everywhere bool `json:"everywhere"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
	}
	if id, ok := auth.FromContext(r.Context()); ok {
		var err error
		if req.Everywhere {
			err = s.Store.DeleteUserSessions(r.Context(), id.UserID)
		} else {
			err = s.Store.DeleteSession(r.Context(), id.SessionID)
		}
		if err != nil {
			return err
		}
	}
	s.Sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleForgot always answers 202 so the response doesn't reveal which emails have accounts.
func (s *Server) handleForgot(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Email = normalizeEmail(req.Email)
	if !validEmail(req.Email) {
		return httpx.Validation{"email": "Enter a valid email."}.Err()
	}
	ctx := r.Context()
	u, err := s.Store.GetUserByEmail(ctx, req.Email)
	if err == nil {
		raw, hash := auth.NewToken()
		if err := s.Store.CreatePasswordReset(ctx, db.CreatePasswordResetParams{
			ID: newID(), UserID: u.ID, TokenHash: hash, ExpiresAt: s.Now().Add(s.Cfg.ResetTTL),
		}); err != nil {
			return err
		}
		link := s.Cfg.PublicURL + "/reset/" + raw
		if err := s.Mail.Send(ctx, mail.Message{
			To:      u.Email,
			Subject: "Reset your Canopy password",
			Text: "Someone asked to reset the password for your Canopy account.\n\n" +
				"Choose a new password: " + link + "\n\n" +
				"This link works once and expires in 30 minutes. If you didn’t ask, you can ignore this email.\n",
		}); err != nil {
			slog.ErrorContext(ctx, "send reset email", "err", err)
		}
	} else if !store.IsNotFound(err) {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{})
	return nil
}

func (s *Server) handleResetCheck(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.Store.GetUsablePasswordReset(r.Context(), auth.HashToken(pathParam(r, "token"))); err != nil {
		if store.IsNotFound(err) {
			return errInvalidReset
		}
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"valid": true})
	return nil
}

// handleReset sets a new password from a single-use link, revokes every session and signs in.
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	checkPassword(v, "password", req.Password)
	if err := v.Err(); err != nil {
		return err
	}
	hash, err := auth.HashPassword(s.Argon2, req.Password)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var next string
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		reset, err := q.ClaimPasswordReset(ctx, auth.HashToken(req.Token))
		if store.IsNotFound(err) {
			return errInvalidReset
		} else if err != nil {
			return err
		}
		if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ID: reset.UserID, PasswordHash: &hash}); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, reset.UserID); err != nil {
			return err
		}
		id, err := s.Sessions.Start(ctx, w, r, q, reset.UserID)
		next = id.Next()
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, twoFactorNext(next))
	return nil
}

const oauthStateCookie = "canopy_oauth_state"

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "provider")
	p, ok := s.oauthProvider(r.Context(), name)
	if !ok {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	state, _ := auth.NewToken()
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: name + ":" + state, Path: "/api/v1/auth/oauth", MaxAge: 600,
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, p.AuthCodeURL(state), http.StatusFound)
}

// handleOAuthCallback signs in an existing account by linked identity or verified email.
// Registering always starts with an organization, so unknown emails are sent to sign up.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "provider")
	fail := func(code string, extra url.Values) {
		q := url.Values{"error": {code}}
		for k, vs := range extra {
			q[k] = vs
		}
		http.Redirect(w, r, "/login?"+q.Encode(), http.StatusFound)
	}
	p, ok := s.oauthProvider(r.Context(), name)
	ck, err := r.Cookie(oauthStateCookie)
	if !ok || err != nil || ck.Value != name+":"+r.URL.Query().Get("state") || r.URL.Query().Get("state") == "" {
		fail("oauth_failed", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/api/v1/auth/oauth", MaxAge: -1,
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})

	ctx := r.Context()
	prof, err := p.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		slog.WarnContext(ctx, "oauth exchange", "provider", name, "err", err)
		fail("oauth_failed", nil)
		return
	}
	if !prof.EmailVerified {
		fail("oauth_unverified", nil)
		return
	}
	var userID uuid.UUID
	if ident, err := s.Store.GetIdentity(ctx, db.GetIdentityParams{Provider: name, Subject: prof.Subject}); err == nil {
		userID = ident.UserID
	} else if !store.IsNotFound(err) {
		httpx.Fail(w, r, err)
		return
	} else {
		u, err := s.Store.GetUserByEmail(ctx, prof.Email)
		if store.IsNotFound(err) {
			fail("oauth_no_account", url.Values{"email": {prof.Email}})
			return
		} else if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := s.Store.CreateIdentity(ctx, db.CreateIdentityParams{
			ID: newID(), UserID: u.ID, Provider: name, Subject: prof.Subject, Email: prof.Email,
		}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		userID = u.ID
	}
	if _, err := s.Sessions.Start(ctx, w, r, s.Store.Queries, userID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
