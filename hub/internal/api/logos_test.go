package api_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/blob"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

const svgText = `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8"/></svg>`

// putRaw sends a raw body and returns the status, headers and body bytes.
func (c *client) putRaw(method, path string, body []byte) (int, http.Header, []byte) {
	c.e.t.Helper()
	req, _ := http.NewRequest(method, c.e.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Forwarded-For", c.ip)
	if method != http.MethodGet {
		req.Header.Set(auth.CSRFHeader, c.csrf())
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, data
}

func TestLogosOrgAndWorkspace(t *testing.T) {
	e := newEnv(t)
	e.srv.Blobs = blob.Dir{Root: t.TempDir()}
	owner, me := e.signup("Logo Co")
	org := currentOrg(me)
	ws, _ := e.createWorkspace(owner, org, setupPayload())
	outsider, _ := e.signup("Other Co")

	// A staff member can see the logo but not change it.
	r := owner.do("POST", "/api/v1/orgs/"+org+"/invites", map[string]any{"email": uniqueEmail("staff"), "role": "member",
		"workspace_grants": []any{map[string]any{"workspace_id": ws, "role": "staff", "manager_ids": []string{}}}})
	if r.Status != http.StatusCreated {
		t.Fatalf("invite: %d %v", r.Status, r.Body)
	}
	token := strings.TrimPrefix(str(r.Body["link"]), "http://canopy.test/invite/")
	staff := e.client()
	acc := staff.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"name": "Sam", "password": "staff password"})
	if acc.Status != 200 {
		t.Fatalf("accept: %d %v", acc.Status, acc.Body)
	}
	e.finishSignIn(staff, "", acc.Body)

	for _, tc := range []struct{ name, path string }{{"org", "/api/v1/orgs/" + org + "/logo"}, {"workspace", "/api/v1/workspaces/" + ws + "/logo"}} {
		t.Run(tc.name, func(t *testing.T) {
			if st, _, _ := owner.putRaw("GET", tc.path, nil); st != http.StatusNotFound {
				t.Errorf("no logo yet: %d", st)
			}
			if st, _, _ := owner.putRaw("PUT", tc.path, pngBytes); st != http.StatusOK {
				t.Fatalf("upload png: %d", st)
			}
			st, h, body := owner.putRaw("GET", tc.path, nil)
			if st != 200 || h.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngBytes) || !strings.Contains(h.Get("Cache-Control"), "max-age") {
				t.Errorf("serve png: %d %q %q", st, h.Get("Content-Type"), h.Get("Cache-Control"))
			}
			if st, _, _ := owner.putRaw("PUT", tc.path, []byte(svgText)); st != http.StatusOK {
				t.Errorf("upload svg: %d", st)
			}
			if _, h, _ := owner.putRaw("GET", tc.path, nil); h.Get("Content-Type") != "image/svg+xml" {
				t.Errorf("svg type: %q", h.Get("Content-Type"))
			}
			if st, _, _ := owner.putRaw("PUT", tc.path, make([]byte, 1<<20+1)); st != http.StatusRequestEntityTooLarge {
				t.Errorf("too large: %d", st)
			}
			if st, _, _ := owner.putRaw("PUT", tc.path, []byte("just some text")); st != http.StatusUnsupportedMediaType {
				t.Errorf("wrong type: %d", st)
			}
			if st, _, _ := owner.putRaw("PUT", tc.path, []byte(`<svg><script>alert(1)</script></svg>`)); st != http.StatusUnsupportedMediaType {
				t.Errorf("scripted svg: %d", st)
			}
			if st, _, _ := staff.putRaw("PUT", tc.path, pngBytes); st != http.StatusForbidden {
				t.Errorf("staff upload: %d", st)
			}
			if st, _, _ := staff.putRaw("DELETE", tc.path, nil); st != http.StatusForbidden {
				t.Errorf("staff delete: %d", st)
			}
			if st, _, _ := staff.putRaw("GET", tc.path, nil); st != 200 {
				t.Errorf("staff view: %d", st)
			}
			if st, _, _ := outsider.putRaw("PUT", tc.path, pngBytes); st != http.StatusNotFound {
				t.Errorf("outsider upload: %d", st)
			}
			if st, _, _ := outsider.putRaw("GET", tc.path, nil); st != http.StatusNotFound {
				t.Errorf("outsider view: %d", st)
			}
		})
	}

	// logo_url shows up on the views.
	m := owner.do("GET", "/api/v1/me", nil)
	if str(m.Body["organizations"].([]any)[0].(map[string]any)["logo_url"]) == "" {
		t.Errorf("me has no logo_url: %v", m.Body)
	}
	if str(owner.do("GET", "/api/v1/workspaces/"+ws, nil).Body["workspace"].(map[string]any)["logo_url"]) == "" {
		t.Error("workspace has no logo_url")
	}
	list := owner.do("GET", "/api/v1/orgs/"+org+"/workspaces", nil)
	if str(list.Body["workspaces"].([]any)[0].(map[string]any)["logo_url"]) == "" {
		t.Errorf("list has no logo_url: %v", list.Body)
	}

	for _, p := range []string{"/api/v1/orgs/" + org + "/logo", "/api/v1/workspaces/" + ws + "/logo"} {
		if st, _, _ := owner.putRaw("DELETE", p, nil); st != http.StatusNoContent {
			t.Errorf("delete: %d", st)
		}
		if st, _, _ := owner.putRaw("GET", p, nil); st != http.StatusNotFound {
			t.Errorf("after delete: %d", st)
		}
	}
	if owner.do("GET", "/api/v1/orgs/"+org, nil).Body["organization"].(map[string]any)["logo_url"] != nil {
		t.Error("logo_url still set after delete")
	}
}
