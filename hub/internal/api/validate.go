package api

import (
	"crypto/rand"
	"encoding/base32"
	"hash/fnv"
	netmail "net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"canopy/hub/internal/httpx"
)

const minPasswordLen = 8

var orgSizes = map[string]bool{"just_me": true, "2_10": true, "11_50": true, "51_plus": true}

// avatarColors are the spec's goal colors plus the brand green.
var avatarColors = []string{"#8E5BD9", "#0E9AA7", "#D6457A", "#E8743B", "#4C5FD5", "#16794F"}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	a, err := netmail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s[strings.LastIndex(s, "@")+1:], ".")
}

func avatarColorFor(email string) string {
	h := fnv.New32a()
	h.Write([]byte(email))
	return avatarColors[h.Sum32()%uint32(len(avatarColors))]
}

func checkPassword(v httpx.Validation, field, pw string) {
	if utf8.RuneCountInString(pw) < minPasswordLen {
		v.Add(field, "Use at least 8 characters.")
	} else if len(pw) > 1024 {
		v.Add(field, "Use at most 1,024 characters.")
	}
}

type orgInput struct {
	Name  string `json:"name"`
	Size  string `json:"size"`
	About string `json:"about"`
}

func (in *orgInput) validate(v httpx.Validation, prefix string) {
	in.Name = strings.TrimSpace(in.Name)
	in.About = strings.TrimSpace(in.About)
	if in.Name == "" {
		v.Add(prefix+"name", "Enter your organization’s name.")
	} else if utf8.RuneCountInString(in.Name) > 80 {
		v.Add(prefix+"name", "Use at most 80 characters.")
	}
	if !orgSizes[in.Size] {
		v.Add(prefix+"size", "Choose how big your organization is.")
	}
	if utf8.RuneCountInString(in.About) > 500 {
		v.Add(prefix+"about", "Use at most 500 characters.")
	}
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}
