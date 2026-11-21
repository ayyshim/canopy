// Package refs finds item references such as G-12, T-34 and N-5 in text and resolves them
// inside one workspace, with the same visibility rules as the board snapshot.
package refs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Kind byte

const (
	Goal Kind = 'G'
	Task Kind = 'T'
	Note Kind = 'N'
)

type Ref struct {
	Kind   Kind
	Number int32
}

func (r Ref) String() string { return fmt.Sprintf("%c-%d", r.Kind, r.Number) }

var (
	// A ref is a letter, a dash and digits, not glued to other word characters.
	refRe    = regexp.MustCompile(`(?i)(?:^|[^\w/-])#?([GTN])-(\d{1,9})\b`)
	fenceRe  = regexp.MustCompile("(?s)```.*?(?:```|$)")
	inlineRe = regexp.MustCompile("`[^`\n]*`")
	exactRe  = regexp.MustCompile(`(?i)^#?([GTN])-(\d{1,9})$`)
)

// ParseOne reads a single ref such as "T-34" or "#t-34".
func ParseOne(s string) (Ref, bool) {
	m := exactRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Ref{}, false
	}
	return mk(m[1], m[2])
}

func mk(letter, digits string) (Ref, bool) {
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return Ref{}, false
	}
	return Ref{Kind: Kind(strings.ToUpper(letter)[0]), Number: int32(n)}, true
}

// Parse returns the distinct refs in text, in order of first appearance. Refs inside code
// spans and fenced blocks are ignored.
func Parse(text string) []Ref {
	text = fenceRe.ReplaceAllString(text, " ")
	text = inlineRe.ReplaceAllString(text, " ")
	var out []Ref
	seen := map[Ref]bool{}
	for _, m := range refRe.FindAllStringSubmatch(text, -1) {
		if r, ok := mk(m[1], m[2]); ok && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
