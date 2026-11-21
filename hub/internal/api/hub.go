package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

var (
	errSetupRequired = httpx.NewError(http.StatusConflict, "setup_required", "This hub isn’t set up yet.")
	errAlreadySetUp  = httpx.NewError(http.StatusConflict, "already_set_up", "This hub is already set up.")
	errBadSetupToken = httpx.FieldError(http.StatusForbidden, "bad_setup_token", "token", "That setup code isn’t right. Find it in the hub’s log.")
	errNotHubAdmin   = httpx.NewError(http.StatusForbidden, "forbidden", "Only hub admins can open the hub dashboard.")
	providerNames    = []string{"github", "google"}
)

// SetupComplete reports whether first-run setup has happened, caching a positive answer.
func (s *Server) SetupComplete(r *http.Request) (bool, error) {
	if s.setupDone.Load() {
		return true, nil
	}
	_, err := s.Store.GetHubSettings(r.Context())
	if store.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.setupDone.Store(true)
	return true, nil
}

func (s *Server) requireSetup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, err := s.SetupComplete(r)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if !done {
			httpx.Fail(w, r, errSetupRequired)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireHubAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Store.GetUserByID(r.Context(), identity(r).UserID)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if !u.IsHubAdmin {
			httpx.Fail(w, r, errNotHubAdmin)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type hubView struct {
	Name          string          `json:"name"`
	SetupRequired bool            `json:"setup_required"`
	SignupOpen    bool            `json:"signup_open"`
	Providers     map[string]bool `json:"providers"` // enabled sign-in providers
}

// handleGetHub is public: the sign-in pages use it to pick what to show.
func (s *Server) handleGetHub(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	view := hubView{Providers: map[string]bool{"github": false, "google": false}}
	hub, err := s.Store.GetHubSettings(ctx)
	if store.IsNotFound(err) {
		view.SetupRequired = true
		httpx.JSON(w, http.StatusOK, view)
		return nil
	} else if err != nil {
		return err
	}
	view.Name, view.SignupOpen = hub.Name, hub.SignupOpen
	for _, name := range providerNames {
		_, view.Providers[name] = s.oauthProvider(ctx, name)
	}
	httpx.JSON(w, http.StatusOK, view)
	return nil
}

type providerInput struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // empty keeps the stored secret
}

type hubSetupRequest struct {
	Token string `json:"token"`
	Hub   struct {
		Name       string `json:"name"`
		SignupOpen *bool  `json:"signup_open"`
	} `json:"hub"`
	Admin struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	} `json:"admin"`
	Providers map[string]providerInput `json:"providers"`
}

func validateHubName(v httpx.Validation, field string, name *string) {
	*name = strings.TrimSpace(*name)
	if *name == "" {
		v.Add(field, "Enter a name for this hub.")
	} else if utf8.RuneCountInString(*name) > 80 {
		v.Add(field, "Use at most 80 characters.")
	}
}

// validateProviders checks provider input; storedSecret reports whether a secret is already saved.
func validateProviders(v httpx.Validation, in map[string]providerInput, storedSecret func(string) bool) {
	for name, p := range in {
		if name != "github" && name != "google" {
			v.Add("providers."+name, "Unknown sign-in provider.")
			continue
		}
		if !p.Enabled {
			continue
		}
		if strings.TrimSpace(p.ClientID) == "" {
			v.Add("providers."+name+".client_id", "Enter the client ID.")
		}
		if p.ClientSecret == "" && !storedSecret(name) {
			v.Add("providers."+name+".client_secret", "Enter the client secret.")
		}
	}
}

func upsertProviders(r *http.Request, q *db.Queries, in map[string]providerInput) error {
	for name, p := range in {
		if _, err := q.UpsertAuthProvider(r.Context(), db.UpsertAuthProviderParams{
			Provider: name, Enabled: p.Enabled, ClientID: strings.TrimSpace(p.ClientID), ClientSecret: p.ClientSecret,
		}); err != nil {
			return err
		}
	}
	return nil
}

// handleHubSetup runs once: it names the hub, stores sign-in providers and creates the first
// hub admin, then signs them in. Only after this can organizations register.
func (s *Server) handleHubSetup(w http.ResponseWriter, r *http.Request) error {
	var req hubSetupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if s.SetupToken == "" || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Token)), []byte(s.SetupToken)) != 1 {
		return errBadSetupToken
	}
	v := httpx.Validation{}
	validateHubName(v, "hub.name", &req.Hub.Name)
	req.Admin.Name = strings.TrimSpace(req.Admin.Name)
	req.Admin.Email = normalizeEmail(req.Admin.Email)
	if req.Admin.Name == "" {
		v.Add("admin.name", "Enter your name.")
	}
	if !validEmail(req.Admin.Email) {
		v.Add("admin.email", "Enter a valid email.")
	}
	checkPassword(v, "admin.password", req.Admin.Password)
	validateProviders(v, req.Providers, func(string) bool { return false })
	if err := v.Err(); err != nil {
		return err
	}
	hash, err := auth.HashPassword(s.Argon2, req.Admin.Password)
	if err != nil {
		return err
	}
	signupOpen := true
	if req.Hub.SignupOpen != nil {
		signupOpen = *req.Hub.SignupOpen
	}

	ctx := r.Context()
	userID := newID()
	var next string
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.CreateHubSettings(ctx, db.CreateHubSettingsParams{Name: req.Hub.Name, SignupOpen: signupOpen}); err != nil {
			if store.IsNotFound(err) {
				return errAlreadySetUp
			}
			return err
		}
		if _, err := q.CreateUser(ctx, db.CreateUserParams{
			ID: userID, Email: req.Admin.Email, Name: req.Admin.Name, PasswordHash: &hash,
			AvatarColor: avatarColorFor(req.Admin.Email),
		}); err != nil {
			return err
		}
		if err := q.SetHubAdmin(ctx, db.SetHubAdminParams{ID: userID, IsHubAdmin: true}); err != nil {
			return err
		}
		if err := upsertProviders(r, q, req.Providers); err != nil {
			return err
		}
		if err := s.audit(ctx, q, nil, &userID, "hub.setup", fmt.Sprintf("%s set up the hub", req.Admin.Name)); err != nil {
			return err
		}
		id, err := s.Sessions.Start(ctx, w, r, q, userID)
		next = id.Next()
		return err
	})
	if err != nil {
		return err
	}
	s.setupDone.Store(true)
	httpx.JSON(w, http.StatusCreated, twoFactorNext(next))
	return nil
}
