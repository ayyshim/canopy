package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"canopy/hub/internal/httpx"
)

// Limiter counts hits per key in fixed one-minute windows.
type Limiter interface {
	// Allow records a hit and reports whether key is still within limit for this window.
	Allow(ctx context.Context, key string, limit int) (bool, error)
}

// MemoryLimiter is a single-process limiter.
type MemoryLimiter struct {
	mu     sync.Mutex
	window int64
	counts map[string]int
	Now    func() time.Time
}

func (m *MemoryLimiter) Allow(_ context.Context, key string, limit int) (bool, error) {
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	w := now().Unix() / 60
	m.mu.Lock()
	defer m.mu.Unlock()
	if w != m.window || m.counts == nil {
		m.window, m.counts = w, map[string]int{}
	}
	m.counts[key]++
	return m.counts[key] <= limit, nil
}

// RedisLimiter shares counts across hub instances.
type RedisLimiter struct{ Client *redis.Client }

func (l RedisLimiter) Allow(ctx context.Context, key string, limit int) (bool, error) {
	k := fmt.Sprintf("rl:%s:%d", key, time.Now().Unix()/60)
	pipe := l.Client.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, 2*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, err // fail open: an outage shouldn't lock everyone out
	}
	return incr.Val() <= int64(limit), nil
}

var errRateLimited = httpx.NewError(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Wait a minute and try again.")

// RateLimit allows `limit` requests per minute per client IP for the wrapped routes.
func RateLimit(l Limiter, bucket string, limit int, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, err := l.Allow(r.Context(), bucket+":"+ClientIP(r, trustProxy), limit)
			if err == nil && !ok {
				w.Header().Set("Retry-After", strconv.Itoa(60-time.Now().Second()))
				httpx.Fail(w, r, errRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the caller's IP. Behind a trusted proxy (Caddy) it uses the right-most
// X-Forwarded-For entry, which the proxy appended itself.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
