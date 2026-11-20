package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// OAuthProfile is what a provider tells us about the person signing in.
type OAuthProfile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// OAuthProvider is one sign-in provider (GitHub, Google).
type OAuthProvider interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (OAuthProfile, error)
}

type oauthProvider struct {
	cfg     *oauth2.Config
	profile func(ctx context.Context, c *http.Client) (OAuthProfile, error)
}

func (p oauthProvider) AuthCodeURL(state string) string { return p.cfg.AuthCodeURL(state) }

func (p oauthProvider) Exchange(ctx context.Context, code string) (OAuthProfile, error) {
	tok, err := p.cfg.Exchange(ctx, code)
	if err != nil {
		return OAuthProfile{}, err
	}
	return p.profile(ctx, p.cfg.Client(ctx, tok))
}

// NewGitHub returns a GitHub provider; callback is the full redirect URL.
func NewGitHub(clientID, secret, callback string) OAuthProvider {
	return oauthProvider{
		cfg: &oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: callback,
			Endpoint: github.Endpoint, Scopes: []string{"read:user", "user:email"}},
		profile: githubProfile,
	}
}

// NewGoogle returns a Google provider; callback is the full redirect URL.
func NewGoogle(clientID, secret, callback string) OAuthProvider {
	return oauthProvider{
		cfg: &oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: callback,
			Endpoint: google.Endpoint, Scopes: []string{"openid", "email", "profile"}},
		profile: googleProfile,
	}
}

func getJSON(ctx context.Context, c *http.Client, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func githubProfile(ctx context.Context, c *http.Client) (OAuthProfile, error) {
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := getJSON(ctx, c, "https://api.github.com/user", &u); err != nil {
		return OAuthProfile{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, c, "https://api.github.com/user/emails", &emails); err != nil {
		return OAuthProfile{}, err
	}
	p := OAuthProfile{Subject: strconv.FormatInt(u.ID, 10), Name: u.Name}
	if p.Name == "" {
		p.Name = u.Login
	}
	for _, e := range emails {
		if e.Primary {
			p.Email, p.EmailVerified = strings.ToLower(e.Email), e.Verified
		}
	}
	if p.Email == "" {
		return p, errors.New("github account has no primary email")
	}
	return p, nil
}

func googleProfile(ctx context.Context, c *http.Client) (OAuthProfile, error) {
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := getJSON(ctx, c, "https://openidconnect.googleapis.com/v1/userinfo", &u); err != nil {
		return OAuthProfile{}, err
	}
	return OAuthProfile{Subject: u.Sub, Email: strings.ToLower(u.Email), EmailVerified: u.EmailVerified, Name: u.Name}, nil
}
