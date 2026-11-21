package discord

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxUsername    = 80
	maxContent     = 2000
	maxTitle       = 256
	maxDescription = 4096
	maxFieldName   = 256
	maxFieldValue  = 1024
	maxFooter      = 2048
	maxFields      = 25
	maxEmbeds      = 10
	maxEmbedTotal  = 6000
	maxFiles       = 10
	maxFileSize    = 8 << 20
	maxButtons     = 25
)

var bannedName = regexp.MustCompile(`(?i)discord|clyde`)

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func cleanUsername(s string) string {
	s = strings.TrimSpace(bannedName.ReplaceAllString(s, ""))
	return strings.TrimSpace(truncate(s, maxUsername))
}

func embedSize(e Embed) int {
	n := utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Description) + utf8.RuneCountInString(e.Footer)
	for _, f := range e.Fields {
		n += utf8.RuneCountInString(f.Name) + utf8.RuneCountInString(f.Value)
	}
	return n
}

// fitEmbeds applies per-field limits, then drops content from the end
// until the whole message is within the combined limit.
func fitEmbeds(in []Embed) []Embed {
	if len(in) > maxEmbeds {
		in = in[:maxEmbeds]
	}
	out := make([]Embed, 0, len(in))
	for _, e := range in {
		e.Title = truncate(e.Title, maxTitle)
		e.Description = truncate(e.Description, maxDescription)
		e.Footer = truncate(e.Footer, maxFooter)
		if len(e.Fields) > maxFields {
			e.Fields = e.Fields[:maxFields]
		}
		fs := make([]Field, 0, len(e.Fields))
		for _, f := range e.Fields {
			f.Name = truncate(f.Name, maxFieldName)
			f.Value = truncate(f.Value, maxFieldValue)
			if f.Name == "" || f.Value == "" {
				continue
			}
			fs = append(fs, f)
		}
		e.Fields = fs
		out = append(out, e)
	}
	for {
		total := 0
		for _, e := range out {
			total += embedSize(e)
		}
		if total <= maxEmbedTotal || len(out) == 0 {
			return out
		}
		excess := total - maxEmbedTotal
		last := &out[len(out)-1]
		switch {
		case len(last.Fields) > 0:
			last.Fields = last.Fields[:len(last.Fields)-1]
		case last.Description != "":
			last.Description = truncate(last.Description, utf8.RuneCountInString(last.Description)-excess)
		case last.Footer != "":
			last.Footer = ""
		case last.Title != "":
			last.Title = truncate(last.Title, utf8.RuneCountInString(last.Title)-excess)
		default:
			out = out[:len(out)-1]
		}
	}
}
