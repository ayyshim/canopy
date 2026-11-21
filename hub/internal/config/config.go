// Package config reads hub settings from the environment.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr        string // listen address, e.g. ":8080"
	DatabaseURL string
	RedisURL    string // optional; enables shared rate limiting
	// VAPIDPublic and VAPIDPrivate override the web push keys kept in the database.
	VAPIDPublic, VAPIDPrivate string
	PublicURL                 string // browser-facing origin, used in email links and OAuth redirects

	CookieSecure bool // false only for plain-http local development
	TrustProxy   bool // read client IP from X-Forwarded-For (behind Caddy)

	SMTPAddr string // empty logs mail instead of sending
	SMTPFrom string

	DefaultSessionLimit int64
	DefaultWeeklyLimit  int64

	AuthRateLimit int // requests per minute per IP on auth endpoints

	// SetupToken guards first-run hub setup. Empty: the hub generates one and logs the setup URL.
	SetupToken string

	// Runner listener (mutual TLS): where it listens, the URL runners dial, extra cert hosts.
	RunnerAddr  string
	RunnerURL   string
	RunnerHosts []string

	// Blob storage: S3-compatible (RustFS) when S3Endpoint is set, else a local directory.
	BlobDir                                        string
	S3Endpoint, S3AccessKey, S3SecretKey, S3Bucket string
	S3Secure                                       bool

	SessionTTL time.Duration
	ResetTTL   time.Duration
}

func FromEnv() (Config, error) {
	c := Config{
		Addr:        env("HUB_ADDR", ":8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://canopy:canopy@localhost:5432/canopy?sslmode=disable"),
		RedisURL:    os.Getenv("REDIS_URL"),
		PublicURL:   env("PUBLIC_URL", "http://localhost:8000"),
		SMTPAddr:    os.Getenv("SMTP_ADDR"),
		VAPIDPublic: os.Getenv("VAPID_PUBLIC_KEY"), VAPIDPrivate: os.Getenv("VAPID_PRIVATE_KEY"),
		SMTPFrom:   env("SMTP_FROM", "Canopy <no-reply@canopy.local>"),
		SetupToken: os.Getenv("HUB_SETUP_TOKEN"),
		SessionTTL: 30 * 24 * time.Hour,
		ResetTTL:   30 * time.Minute,
	}
	c.RunnerAddr = env("RUNNER_ADDR", ":8443")
	c.RunnerURL = os.Getenv("RUNNER_URL")
	c.BlobDir = env("BLOB_DIR", "data/blobs")
	c.S3Endpoint, c.S3AccessKey, c.S3SecretKey = os.Getenv("S3_ENDPOINT"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
	c.S3Bucket = env("S3_BUCKET", "canopy")
	var err error
	if c.CookieSecure, err = envBool("COOKIE_SECURE", true); err != nil {
		return c, err
	}
	if c.TrustProxy, err = envBool("TRUST_PROXY", false); err != nil {
		return c, err
	}
	if c.DefaultSessionLimit, err = envInt("DEFAULT_SESSION_LIMIT", 2_000_000); err != nil {
		return c, err
	}
	if c.DefaultWeeklyLimit, err = envInt("DEFAULT_WEEKLY_LIMIT", 20_000_000); err != nil {
		return c, err
	}
	if c.S3Secure, err = envBool("S3_SECURE", false); err != nil {
		return c, err
	}
	pub, err := url.Parse(c.PublicURL)
	if err != nil || pub.Host == "" {
		return c, fmt.Errorf("PUBLIC_URL: %q is not a URL", c.PublicURL)
	}
	if c.RunnerURL == "" {
		_, port, err := net.SplitHostPort(c.RunnerAddr)
		if err != nil || port == "" {
			return c, fmt.Errorf("RUNNER_ADDR: %q needs a port, like :8443", c.RunnerAddr)
		}
		c.RunnerURL = "wss://" + net.JoinHostPort(pub.Hostname(), port) + "/ws/runners"
	}
	c.RunnerHosts = []string{pub.Hostname(), "localhost", "127.0.0.1", "::1"}
	if ru, err := url.Parse(c.RunnerURL); err == nil && ru.Hostname() != "" {
		c.RunnerHosts = append(c.RunnerHosts, ru.Hostname())
	}
	for _, h := range strings.Split(os.Getenv("RUNNER_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.RunnerHosts = append(c.RunnerHosts, h)
		}
	}
	rl, err := envInt("AUTH_RATE_LIMIT", 10)
	if err != nil {
		return c, err
	}
	c.AuthRateLimit = int(rl)
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func envInt(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}
