package discord

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanUsername(t *testing.T) {
	if got := cleanUsername("My Discord - Clyde Bot"); strings.Contains(strings.ToLower(got), "discord") || strings.Contains(strings.ToLower(got), "clyde") {
		t.Fatalf("banned word kept: %q", got)
	}
	if got := cleanUsername(strings.Repeat("a", 200)); utf8.RuneCountInString(got) != maxUsername {
		t.Fatalf("len %d", utf8.RuneCountInString(got))
	}
	if got := cleanUsername("discord"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestFitEmbedsLimits(t *testing.T) {
	var fields []Field
	for i := 0; i < 40; i++ {
		fields = append(fields, Field{Name: "n", Value: strings.Repeat("v", 2000)})
	}
	out := fitEmbeds([]Embed{{
		Title:       strings.Repeat("t", 500),
		Description: strings.Repeat("d", 9000),
		Fields:      fields,
	}})
	e := out[0]
	if utf8.RuneCountInString(e.Title) != maxTitle || utf8.RuneCountInString(e.Description) > maxDescription {
		t.Fatal("title or description not trimmed")
	}
	total := 0
	for _, e := range out {
		total += embedSize(e)
	}
	if total > maxEmbedTotal {
		t.Fatalf("total %d", total)
	}
	for _, f := range e.Fields {
		if utf8.RuneCountInString(f.Value) > maxFieldValue {
			t.Fatal("field not trimmed")
		}
	}
	if len(e.Fields) > maxFields {
		t.Fatal("too many fields")
	}
}

func TestFitEmbedsCountAndEmptyFields(t *testing.T) {
	in := make([]Embed, 12)
	for i := range in {
		in[i] = Embed{Title: "x", Fields: []Field{{Name: "", Value: "v"}}}
	}
	out := fitEmbeds(in)
	if len(out) != maxEmbeds || len(out[0].Fields) != 0 {
		t.Fatalf("got %d embeds, %d fields", len(out), len(out[0].Fields))
	}
}
