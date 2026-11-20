// Package config stores what `canopy pair` learned about each hub: the machine's key and
// certificate, the hub's CA and where to connect. One binary can pair with many hubs.
package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Hub struct {
	URL        string `json:"url"`        // the hub's web address
	RunnerURL  string `json:"runner_url"` // wss://…/ws/runners
	RunnerID   string `json:"runner_id"`
	RunnerName string `json:"runner_name"`
	OrgID      string `json:"org_id"`
	OrgName    string `json:"org_name"`
	HubName    string `json:"hub_name"`

	Dir string `json:"-"`
}

// Root is where pairings and keys live ($CANOPY_HOME or ~/.config/canopy).
func Root() string {
	if v := os.Getenv("CANOPY_HOME"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "canopy")
}

// DataDir holds mirrored directories, sockets and sandbox state.
func DataDir() string {
	if v := os.Getenv("CANOPY_DATA"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "canopy")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", "canopy")
}

func hubKey(hubURL string) (string, error) {
	u, err := url.Parse(hubURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q isn’t a hub address (try https://canopy.example.com)", hubURL)
	}
	return strings.NewReplacer(":", "_", "/", "_").Replace(u.Host), nil
}

// OrgDir is where one pairing (a hub and an organization) is stored.
func OrgDir(hubURL, orgID string) (string, error) {
	k, err := hubKey(hubURL)
	if err != nil {
		return "", err
	}
	return filepath.Join(Root(), "hubs", k+"--"+orgID), nil
}

func (h Hub) Save() error {
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(h, "", "  ")
	return os.WriteFile(filepath.Join(h.Dir, "hub.json"), b, 0o600)
}

func Load(dir string) (Hub, error) {
	b, err := os.ReadFile(filepath.Join(dir, "hub.json"))
	if err != nil {
		return Hub{}, err
	}
	var h Hub
	if err := json.Unmarshal(b, &h); err != nil {
		return Hub{}, err
	}
	h.Dir = dir
	return h, nil
}

// List returns every pairing on this machine.
func List() ([]Hub, error) {
	entries, err := os.ReadDir(filepath.Join(Root(), "hubs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []Hub
	for _, e := range entries {
		if h, err := Load(filepath.Join(Root(), "hubs", e.Name())); err == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// TLS returns the client configuration for the runner listener: this machine's certificate,
// trusting only the hub's CA.
func (h Hub) TLS() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(h.Dir, "cert.pem"), filepath.Join(h.Dir, "key.pem"))
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(filepath.Join(h.Dir, "ca.pem"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("hub CA is malformed")
	}
	u, err := url.Parse(h.RunnerURL)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: u.Hostname(), MinVersion: tls.VersionTLS13}, nil
}

// KeyPath is where an API key for a CLI is kept (never sent anywhere but the CLI).
func KeyPath(cli string) string { return filepath.Join(Root(), "keys", cli+".key") }

func SaveKey(cli, key string) error {
	if err := os.MkdirAll(filepath.Dir(KeyPath(cli)), 0o700); err != nil {
		return err
	}
	return os.WriteFile(KeyPath(cli), []byte(strings.TrimSpace(key)), 0o600)
}

func ReadKey(cli string) string {
	b, err := os.ReadFile(KeyPath(cli))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
