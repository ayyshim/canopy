package tools

import (
	"encoding/json"
	"testing"

	"canopy/shared/protocol"
)

func TestNotifyToolDef(t *testing.T) {
	for _, d := range Defs {
		if d.Name != protocol.ToolNotify {
			continue
		}
		var s struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal([]byte(d.Schema), &s); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"message", "level", "files", "links"} {
			if _, ok := s.Properties[k]; !ok {
				t.Errorf("notify has no %q", k)
			}
		}
		if len(s.Required) != 1 || s.Required[0] != "message" {
			t.Errorf("required: %v", s.Required)
		}
		return
	}
	t.Fatal("no notify tool")
}
