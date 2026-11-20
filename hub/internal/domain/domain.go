// Package domain holds pure workspace rules: directory kinds, grant suggestions and the
// staff ⊆ manager grant invariant. It has no I/O so the rules are easy to test.
package domain

import (
	"path"
	"strings"

	"github.com/google/uuid"
)

const (
	Read      = "read"
	ReadWrite = "read_write"
)

var Skills = []string{"developer", "reviewer", "researcher", "designer", "writer"}

func ValidSkill(s string) bool {
	for _, k := range Skills {
		if k == s {
			return true
		}
	}
	return false
}

// CLIs Canopy can drive, with their models (most capable first). v1 drives Claude Code only.
type CLIModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CLIInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Supported  bool       `json:"supported"`
	InstallCmd string     `json:"install_cmd"`
	Models     []CLIModel `json:"models"`
	Manager    string     `json:"manager_model"` // most capable
	Staff      string     `json:"staff_model"`   // balanced
}

var CLIs = []CLIInfo{
	{ID: "claude", Name: "Claude Code", Supported: true, InstallCmd: "npm install -g @anthropic-ai/claude-code",
		Models:  []CLIModel{{"opus", "Opus (most capable)"}, {"sonnet", "Sonnet (balanced)"}, {"haiku", "Haiku (fastest)"}},
		Manager: "opus", Staff: "sonnet"},
	{ID: "codex", Name: "Codex", InstallCmd: "npm install -g @openai/codex",
		Models: []CLIModel{{"gpt-5-codex", "GPT-5 Codex"}}, Manager: "gpt-5-codex", Staff: "gpt-5-codex"},
	{ID: "gemini", Name: "Gemini CLI", InstallCmd: "npm install -g @google/gemini-cli",
		Models: []CLIModel{{"gemini-2.5-pro", "Gemini 2.5 Pro"}}, Manager: "gemini-2.5-pro", Staff: "gemini-2.5-pro"},
	{ID: "pi", Name: "Pi", InstallCmd: "npm install -g @mariozechner/pi-coding-agent",
		Models: []CLIModel{{"default", "Default"}}, Manager: "default", Staff: "default"},
}

func CLI(id string) (CLIInfo, bool) {
	for _, c := range CLIs {
		if c.ID == id {
			return c, true
		}
	}
	return CLIInfo{}, false
}

// DefaultModel is the most capable model for managers and the balanced one for staff.
func DefaultModel(cli, kind string) string {
	c, ok := CLI(cli)
	if !ok {
		return ""
	}
	if kind == "manager" {
		return c.Manager
	}
	return c.Staff
}

func ValidModel(cli, model string) bool {
	c, ok := CLI(cli)
	if !ok {
		return false
	}
	for _, m := range c.Models {
		if m.ID == model {
			return true
		}
	}
	return false
}

// ClassifyDir guesses a directory's kind from its path.
func ClassifyDir(p string) string {
	base := strings.ToLower(path.Base(strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")))
	switch {
	case containsAny(base, "doc", "wiki", "handbook", "notes", "guide", "spec", "content", "blog"):
		return "docs"
	case containsAny(base, "design", "figma", "sketch", "mock", "brand", "asset"):
		return "design"
	default:
		return "code"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func DirName(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if b := path.Base(p); b != "." && b != "/" && b != "" {
		return b
	}
	return p
}

type Dir struct {
	ID   uuid.UUID
	Name string
	Kind string
}

type Grant struct {
	DirectoryID uuid.UUID `json:"directory_id"`
	Mode        string    `json:"mode"`
	Reason      string    `json:"reason,omitempty"`
}

// MainCodebase is the code directory whose name matches the workspace, else the first one.
func MainCodebase(dirs []Dir, workspaceName string) (Dir, bool) {
	want := normalize(workspaceName)
	var first *Dir
	for i := range dirs {
		if dirs[i].Kind != "code" {
			continue
		}
		if first == nil {
			first = &dirs[i]
		}
		if want != "" && (normalize(dirs[i].Name) == want || strings.Contains(normalize(dirs[i].Name), want) || strings.Contains(want, normalize(dirs[i].Name))) {
			return dirs[i], true
		}
	}
	if first != nil {
		return *first, true
	}
	return Dir{}, false
}

func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return -1
	}, s)
}

// Suggest returns the fallback directory suggestions for a skill (SPEC "Suggestion rules").
// limit, when non-nil, is the manager's access: suggestions are clamped to it.
func Suggest(skill string, dirs []Dir, workspaceName string, limit []Grant) []Grant {
	var out []Grant
	add := func(d Dir, mode, reason string) {
		out = append(out, Grant{DirectoryID: d.ID, Mode: mode, Reason: reason})
	}
	switch skill {
	case "developer", "reviewer":
		if d, ok := MainCodebase(dirs, workspaceName); ok {
			if skill == "developer" {
				add(d, ReadWrite, "Writes code in the main codebase")
			} else {
				add(d, Read, "Reviews changes in the main codebase")
			}
		}
	case "designer":
		for _, d := range dirs {
			switch d.Kind {
			case "design":
				add(d, ReadWrite, "Design files")
			case "docs":
				add(d, ReadWrite, "Documents design decisions")
			}
		}
	case "writer":
		for _, d := range dirs {
			if d.Kind == "docs" {
				add(d, ReadWrite, "Writes the docs")
			}
		}
	case "researcher":
		for _, d := range dirs {
			if d.Kind == "docs" || d.Kind == "code" {
				add(d, Read, "Reads docs and code for research")
			}
		}
	}
	if limit != nil {
		out = Clamp(out, limit)
	}
	return out
}

func rank(mode string) int {
	switch mode {
	case ReadWrite:
		return 2
	case Read:
		return 1
	}
	return 0
}

// Clamp keeps only grants the limit allows, lowering read_write to read where the limit is read.
func Clamp(grants, limit []Grant) []Grant {
	allowed := map[uuid.UUID]string{}
	for _, g := range limit {
		allowed[g.DirectoryID] = g.Mode
	}
	out := make([]Grant, 0, len(grants))
	for _, g := range grants {
		max, ok := allowed[g.DirectoryID]
		if !ok {
			continue
		}
		if rank(g.Mode) > rank(max) {
			g.Mode = max
		}
		out = append(out, g)
	}
	return out
}

// WithinLimit reports the first grant that exceeds the limit (staff ⊆ manager).
func WithinLimit(grants, limit []Grant) (Grant, bool) {
	allowed := map[uuid.UUID]string{}
	for _, g := range limit {
		allowed[g.DirectoryID] = g.Mode
	}
	for _, g := range grants {
		max, ok := allowed[g.DirectoryID]
		if !ok || rank(g.Mode) > rank(max) {
			return g, false
		}
	}
	return Grant{}, true
}

// AllDirectories grants read_write on every directory (managers with "all directories").
func AllDirectories(dirs []Dir) []Grant {
	out := make([]Grant, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, Grant{DirectoryID: d.ID, Mode: ReadWrite})
	}
	return out
}

// AvatarColors cycle through the spec's goal colors.
var AvatarColors = []string{"#8E5BD9", "#0E9AA7", "#D6457A", "#E8743B", "#4C5FD5", "#16794F"}

// GoalColors are assigned to goals in order.
var GoalColors = []string{"#8E5BD9", "#0E9AA7", "#D6457A", "#E8743B", "#4C5FD5"}

// Slug turns a title into lowercase words joined by dashes, for branch and file names.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() > 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// SameRemote reports whether two git remote URLs name the same repository, so
// git@github.com:acme/api.git and https://github.com/acme/api match.
func SameRemote(a, b string) bool {
	na, nb := normalizeRemote(a), normalizeRemote(b)
	return na != "" && na == nb
}

func normalizeRemote(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	u = strings.Replace(u, ":", "/", 1)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	return u
}
