package refs

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	r := func(k Kind, n int32) Ref { return Ref{k, n} }
	cases := []struct {
		name, in string
		want     []Ref
	}{
		{"plain", "see T-34 and G-12", []Ref{r(Task, 34), r(Goal, 12)}},
		{"hash and lowercase", "fix #n-5 then #T-7", []Ref{r(Note, 5), r(Task, 7)}},
		{"end of sentence", "This depends on T-34. Also (G-2), N-9!", []Ref{r(Task, 34), r(Goal, 2), r(Note, 9)}},
		{"comma list", "T-1,T-2, T-1", []Ref{r(Task, 1), r(Task, 2)}},
		{"inline code", "run `T-34` but not this, see N-1", []Ref{r(Note, 1)}},
		{"fenced code", "```\nT-34\n```\nG-3", []Ref{r(Goal, 3)}},
		{"unclosed fence", "G-1\n```\nT-2", []Ref{r(Goal, 1)}},
		{"glued to words", "ABT-34 xT-5 T-34abc T-34_x", nil},
		{"zero and huge", "T-0 T-99999999999", nil},
		{"in a path or url", "see /T-34 and a/G-1", nil},
		{"line start", "T-8\nN-2", []Ref{r(Task, 8), r(Note, 2)}},
	}
	for _, c := range cases {
		if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Parse(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestParseOne(t *testing.T) {
	for in, want := range map[string]Ref{"T-34": {Task, 34}, "#g-1": {Goal, 1}, " N-5 ": {Note, 5}} {
		if got, ok := ParseOne(in); !ok || got != want {
			t.Errorf("ParseOne(%q) = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "T-", "T-0", "X-1", "T-1 T-2", "T-1."} {
		if _, ok := ParseOne(in); ok {
			t.Errorf("ParseOne(%q) should fail", in)
		}
	}
}

func TestRefString(t *testing.T) {
	if s := (Ref{Task, 34}).String(); s != "T-34" {
		t.Errorf("got %s", s)
	}
}
