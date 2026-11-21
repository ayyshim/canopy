package api

import (
	"strings"
	"testing"
)

func TestInjectPreviewScript(t *testing.T) {
	for name, in := range map[string]string{
		"head":     "<!doctype html><html><HEAD lang=x><title>a</title></HEAD></html>",
		"no head":  "<!DOCTYPE html><html><body>hi</body></html>",
		"no html":  "<!doctype html><p>hi</p>",
		"fragment": "<p>hi</p>",
		"twice":    "<html><head></head></html>",
	} {
		out := injectPreviewScript([]byte(in))
		if name == "twice" {
			out = injectPreviewScript(out)
		}
		if n := strings.Count(string(out), "<script>"); n != 1 {
			t.Errorf("%s: %d scripts: %s", name, n, out)
		}
		if strings.HasPrefix(strings.ToLower(in), "<!doctype") && !strings.HasPrefix(strings.ToLower(string(out)), "<!doctype") {
			t.Errorf("%s: doctype moved: %s", name, out)
		}
	}
}

func TestPreviewScriptKeepsStorageInWindowName(t *testing.T) {
	if !strings.Contains(previewScript, "window.name") {
		t.Fatal("storage helper must persist in window.name")
	}
}
