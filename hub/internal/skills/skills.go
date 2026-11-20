// Package skills turns a skill source (pasted markdown or a GitHub link) into stored markdown.
package skills

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxBytes caps one skill's markdown, uploaded or fetched.
const MaxBytes = 100 << 10

const rawHost = "raw.githubusercontent.com"

// Error is a message safe to show people.
type Error string

func (e Error) Error() string { return string(e) }

// RawURL turns a GitHub file link into the URL of its raw markdown:
//
//	https://github.com/o/r/blob/main/skills/go/SKILL.md → https://raw.githubusercontent.com/o/r/main/skills/go/SKILL.md
//
// raw.githubusercontent.com links pass through. Anything else is an error.
func RawURL(link string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", Error("Paste a full https:// link to a markdown file on GitHub.")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch host {
	case rawHost:
		if len(parts) < 4 {
			return "", Error("That link doesn’t point to a file. Open the file on GitHub and copy its address.")
		}
		return u.String(), nil
	case "github.com":
		// owner/repo/blob|raw/ref/path...
		if len(parts) >= 5 && (parts[2] == "blob" || parts[2] == "raw") {
			return "https://" + rawHost + "/" + strings.Join(append(parts[:2:2], parts[3:]...), "/"), nil
		}
		return "", Error("That GitHub link doesn’t point to a file. Open the markdown file on GitHub and copy its address.")
	}
	return "", Error("Only GitHub links work for now. Paste a link to a markdown file on github.com.")
}

// Fetcher downloads skills from GitHub. The zero value works.
type Fetcher struct {
	Client *http.Client // defaults to a client with a 10 second timeout
}

// Fetch returns the markdown a GitHub link points to.
func (f *Fetcher) Fetch(ctx context.Context, link string) (string, error) {
	raw, err := RawURL(link)
	if err != nil {
		return "", err
	}
	c := f.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// GitHub may redirect (renamed repos); never follow one to another host or off https.
			if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != rawHost {
				return http.ErrUseLastResponse
			}
			return nil
		}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", Error("That link isn’t valid.")
	}
	res, err := c.Do(req)
	if err != nil {
		return "", Error("Canopy couldn’t reach GitHub. Check the link and try again.")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return "", Error("GitHub says that file doesn’t exist. Private repositories aren’t supported; copy the file’s contents instead.")
	case res.StatusCode != http.StatusOK:
		return "", Error(fmt.Sprintf("GitHub answered %d. Try again in a moment.", res.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return "", Error("Canopy couldn’t finish reading that file. Try again.")
	}
	return Check(string(body))
}

// Check validates skill markdown: not empty, not too big, text.
func Check(content string) (string, error) {
	if len(content) > MaxBytes {
		return "", Error(fmt.Sprintf("That skill is too long. Keep it under %d KB.", MaxBytes>>10))
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return "", Error("That file isn’t text. Skills are markdown (.md) files.")
	}
	if strings.TrimSpace(content) == "" {
		return "", Error("That skill is empty.")
	}
	return content, nil
}

// SuggestName is a skill name from a GitHub link: the file name without .md, or the folder
// for SKILL.md files. Empty when nothing sensible is there.
func SuggestName(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	file := strings.TrimSuffix(parts[len(parts)-1], ".md")
	if strings.EqualFold(file, "skill") && len(parts) >= 2 {
		file = parts[len(parts)-2]
	}
	file = strings.NewReplacer("-", " ", "_", " ").Replace(file)
	if file == "" {
		return ""
	}
	return strings.ToUpper(file[:1]) + file[1:]
}
