package notify

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var (
	webhookHost = regexp.MustCompile(`^(?:ptb\.|canary\.)?discord(?:app)?\.com$`)
	webhookPath = regexp.MustCompile(`^/api/webhooks/(\d{5,25})/([A-Za-z0-9_-]{20,200})$`)
	roleIDRe    = regexp.MustCompile(`^\d{5,25}$`)
)

// DiscordKinds are the events a workspace can send to Discord.
var DiscordKinds = []string{"questions", "progress", "goal_ready", "errors", "jobs"}

// ErrBadWebhook is returned for anything that isn't a Discord webhook URL.
var ErrBadWebhook = errors.New("not a Discord webhook URL")

// ValidateWebhook accepts only https://discord.com/api/webhooks/{id}/{token} (and the ptb,
// canary and discordapp.com variants), so the hub can't be pointed at other hosts.
func ValidateWebhook(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return ErrBadWebhook
	}
	if !webhookHost.MatchString(u.Hostname()) || !webhookPath.MatchString(u.Path) || u.RawPath != "" && u.RawPath != u.Path {
		return ErrBadWebhook
	}
	return nil
}

// MaskWebhook hides the token: https://discord.com/api/webhooks/{id}/••••.
func MaskWebhook(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "••••"
	}
	m := webhookPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "••••"
	}
	return u.Scheme + "://" + u.Host + "/api/webhooks/" + m[1] + "/••••"
}

// ValidRoleID reports whether s is empty or a Discord role id.
func ValidRoleID(s string) bool { return s == "" || roleIDRe.MatchString(s) }

// ParseDiscordKinds reads stored choices; missing kinds count as on.
func ParseDiscordKinds(raw []byte) map[string]bool {
	out := map[string]bool{}
	for _, k := range DiscordKinds {
		out[k] = true
	}
	var saved map[string]bool
	if json.Unmarshal(raw, &saved) == nil {
		for k, v := range saved {
			if _, ok := out[k]; ok {
				out[k] = v
			}
		}
	}
	return out
}

// CleanKinds keeps only known kinds, trimmed; it reports false if a key is unknown.
func CleanKinds(in map[string]bool) (map[string]bool, bool) {
	out := ParseDiscordKinds(nil)
	for k, v := range in {
		if _, ok := out[strings.TrimSpace(k)]; !ok {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}
