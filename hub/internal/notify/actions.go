package notify

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"canopy/hub/internal/store/db"
)

// Button is one action on a push notification. "open" is handled by the browser alone.
type Button struct {
	Action string `json:"action"`
	Title  string `json:"title"`
}

// Quick describes what can be done from a notification: the buttons to show and the
// actions a token may carry out.
type Quick struct {
	Buttons []Button
	Allowed []string
}

// QuickActions picks the buttons for a signal. Nothing here is risky enough to need a note:
// "always allow", "ask for changes" and the like stay in the app.
func QuickActions(sig db.Signal) Quick {
	open := Button{Action: "open", Title: "Open"}
	switch sig.Type {
	case "gate":
		return Quick{[]Button{{"allow", "Allow"}, {"deny", "Deny"}}, []string{"allow", "deny"}}
	case "dir_access":
		return Quick{[]Button{{"grant", "Grant"}, {"deny", "Deny"}}, []string{"grant", "deny"}}
	case "review":
		return Quick{[]Button{{"approve", "Approve"}, open}, []string{"approve"}}
	case "owner_change":
		return Quick{[]Button{{"approve", "Approve"}, {"deny", "Deny"}}, []string{"approve", "deny"}}
	case "question":
		if text, ok := SuggestedAnswer(sig); ok {
			title := text
			if r := []rune(title); len(r) > 24 {
				title = string(r[:23]) + "…"
			}
			return Quick{[]Button{{"answer", title}, open}, []string{"answer"}}
		}
	}
	return Quick{}
}

// SuggestedAnswer is the option the agent suggested for a question, if it has one.
func SuggestedAnswer(sig db.Signal) (string, bool) {
	var p struct {
		Options   []string `json:"options"`
		Suggested *int     `json:"suggested"`
	}
	if json.Unmarshal(sig.Payload, &p) != nil || p.Suggested == nil || *p.Suggested < 0 || *p.Suggested >= len(p.Options) {
		return "", false
	}
	text := strings.TrimSpace(p.Options[*p.Suggested])
	return text, text != ""
}

// NewToken makes a random action token and the hash that is stored for it.
func NewToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
