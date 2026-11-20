package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"canopy/hub/internal/blob"
)

// People attach files to goals and tasks; only people who see the goal or task get them.
func TestAttachments(t *testing.T) {
	e := newEnv(t)
	e.srv.Blobs = blob.Dir{Root: t.TempDir()}
	owner, me := e.signup("Files Co")
	org := currentOrg(me)
	ws, d := e.createWorkspace(owner, org, setupPayload())
	riley := str(members(d)["Riley"]["id"])
	bob, bobID := e.joinOrg(owner, org, "Bob")
	owner.do("PUT", "/api/v1/workspaces/"+ws+"/access/"+bobID, map[string]any{"role": "staff", "manager_ids": []string{riley}})
	stranger, _ := e.signup("Strangers")

	g := owner.do("POST", "/api/v1/workspaces/"+ws+"/goals", map[string]any{"manager_id": riley, "title": "CSV export", "draft": true})
	goal := str(g.Body["goal"].(map[string]any)["id"])

	if st, _, _ := stranger.putRaw("POST", "/api/v1/goals/"+goal+"/attachments?name=x.txt", []byte("hi")); st != http.StatusNotFound {
		t.Errorf("stranger attached: %d", st)
	}
	if st, _, _ := owner.putRaw("POST", "/api/v1/goals/"+goal+"/attachments?name=empty.txt", nil); st != http.StatusUnprocessableEntity {
		t.Errorf("empty file: %d", st)
	}
	st, _, body := owner.putRaw("POST", "/api/v1/goals/"+goal+"/attachments?name=../../data.csv", []byte("a,b\n1,2\n"))
	if st != http.StatusCreated {
		t.Fatalf("attach: %d %s", st, body)
	}
	var out struct {
		Attachment map[string]any `json:"attachment"`
	}
	_ = json.Unmarshal(body, &out)
	att := str(out.Attachment["id"])
	if str(out.Attachment["name"]) != "data.csv" || str(out.Attachment["mime"]) != "text/csv; charset=utf-8" || out.Attachment["size"] != float64(8) {
		t.Errorf("attachment: %v", out.Attachment)
	}

	tk := owner.do("POST", "/api/v1/workspaces/"+ws+"/tasks", map[string]any{"title": "Look at it", "member_id": riley})
	task := str(tk.Body["task"].(map[string]any)["id"])
	if st, _, b := bob.putRaw("POST", "/api/v1/tasks/"+task+"/attachments?name=shot.png", pngBytes); st != http.StatusCreated {
		t.Fatalf("Bob attached to task: %d %s", st, b)
	}

	snap := bob.do("GET", "/api/v1/workspaces/"+ws+"/board", nil)
	if n := len(snap.Body["attachments"].([]any)); n != 2 {
		t.Errorf("board attachments: %d", n)
	}
	st, h, file := bob.putRaw("GET", "/api/v1/attachments/"+att+"/file?download=1", nil)
	if st != http.StatusOK || string(file) != "a,b\n1,2\n" || h.Get("Content-Disposition") != `attachment; filename="data.csv"` {
		t.Errorf("download: %d %q %v", st, file, h)
	}
	if st, _, _ := stranger.putRaw("GET", "/api/v1/attachments/"+att+"/file", nil); st != http.StatusNotFound {
		t.Errorf("stranger downloaded: %d", st)
	}
	if r := bob.do("DELETE", "/api/v1/attachments/"+att, nil); r.Status != http.StatusForbidden {
		t.Errorf("Bob deleted the owner’s file: %d", r.Status)
	}
	if r := owner.do("DELETE", "/api/v1/attachments/"+att, nil); r.Status != http.StatusNoContent {
		t.Errorf("delete: %d", r.Status)
	}
	if st, _, _ := owner.putRaw("GET", "/api/v1/attachments/"+att+"/file", nil); st != http.StatusNotFound {
		t.Errorf("deleted file still served: %d", st)
	}
}
