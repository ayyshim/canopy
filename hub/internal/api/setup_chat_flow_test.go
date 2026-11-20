package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// askHub is a connected runner that answers every ask with a fixed text.
type askHub struct {
	recorder
	online bool
	text   string
}

func (h *askHub) Online(uuid.UUID) bool { return h.online }
func (h *askHub) Request(_ context.Context, _ uuid.UUID, _ string, _ any, out any) error {
	b, _ := json.Marshal(map[string]string{"text": h.text})
	return json.Unmarshal(b, out)
}

func TestSetupChat(t *testing.T) {
	e := newEnv(t)
	owner, me := e.signup("Setup Chat Co")
	org := currentOrg(me)
	runner := e.addRunner(org, userID(me), "Laptop", "computer")
	hub := &askHub{online: true, text: `{"reply":"Try this team","team":{"members":[` +
		`{"key":"m1","name":"Atlas","kind":"manager"},{"key":"s1","name":"Pike","kind":"staff","skill":"developer","reports_to_key":"m1"}]}}`}
	e.srv.Runners = hub
	path := "/api/v1/orgs/" + org + "/setup/chat"
	body := map[string]any{"runner_id": runner, "workspace": map[string]any{"Name": "Shop"},
		"messages": []map[string]string{{"role": "user", "text": "I run a small web shop"}}}

	r := owner.do("POST", path, body)
	team, _ := r.Body["team"].(map[string]any)
	if r.Status != 200 || r.Body["reply"] != "Try this team" || len(team["members"].([]any)) != 2 {
		t.Fatalf("chat: %d %v", r.Status, r.Body)
	}

	hub.online = false
	if r := owner.do("POST", path, body); r.Status != http.StatusConflict || r.errCode() != "runner_offline" {
		t.Errorf("offline: %d %v", r.Status, r.Body)
	}
	hub.online = true

	bob, _ := e.joinOrg(owner, org, "Bob")
	if r := bob.do("POST", path, body); r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
		t.Errorf("member used the chat: %d", r.Status)
	}
	body["messages"] = []map[string]string{}
	if r := owner.do("POST", path, body); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("empty chat: %d", r.Status)
	}
}
