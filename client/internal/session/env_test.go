package session

import (
	"slices"
	"strings"
	"testing"
)

func TestBaseEnv(t *testing.T) {
	env := baseEnv("abc")
	for _, want := range []string{"CANOPY_SESSION=abc", "CLAUDE_CODE_ARTIFACT=1"} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=") || strings.HasPrefix(e, "DISABLE_TELEMETRY=") {
			t.Errorf("must not set %s", e)
		}
	}
}
