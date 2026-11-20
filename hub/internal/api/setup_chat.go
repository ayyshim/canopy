package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/httpx"
	"canopy/shared/protocol"
)

type chatMessage struct {
	Role string `json:"role"` // "user" or "assistant"
	Text string `json:"text"`
}

type chatMember struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Skill        string   `json:"skill"`
	ReportsToKey string   `json:"reports_to_key"`
	Instructions string   `json:"instructions"`
	SkillKeys    []string `json:"skill_keys"`
}

type chatTeam struct {
	Members []chatMember `json:"members"`
}

type setupChatReply struct {
	Reply string    `json:"reply"`
	Team  *chatTeam `json:"team"`
}

var staffSkills = map[string]bool{"developer": true, "reviewer": true, "designer": true, "writer": true, "researcher": true}

const setupChatSystem = `You help someone set up a team of AI workers in Canopy. A manager takes goals and leads staff. Staff do tasks.
Talk in short, plain sentences. Ask at most one question if you really need to. Do not use jargon.
Always answer with ONE JSON object and nothing else:
{"reply": "what you say to the person", "team": null or {"members": [{"key": "m1", "name": "...", "kind": "manager" or "staff", "skill": "developer|reviewer|designer|writer|researcher (staff only, else empty)", "reports_to_key": "key of a manager, or empty", "instructions": "2 to 5 plain lines on how this person should work", "skill_keys": ["keys from the given skills list that fit"]}]}}
Rules: include at least one manager. Give every member a short first name. Only use skill keys from the list given. Staff with no manager help everyone. Send a team whenever you can suggest one; send the full team each time.`

// parseSetupChat reads Claude's answer. It tolerates text around the JSON and drops anything
// invalid (unknown skills, unknown managers, empty names) instead of failing.
func parseSetupChat(text string, skillKeys map[string]bool) setupChatReply {
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	var raw setupChatReply
	if start < 0 || end <= start || json.Unmarshal([]byte(text[start:end+1]), &raw) != nil {
		return setupChatReply{Reply: text}
	}
	if raw.Team == nil {
		return raw
	}
	keys := map[string]bool{}
	var members []chatMember
	for _, m := range raw.Team.Members {
		m.Name = strings.TrimSpace(m.Name)
		if m.Name == "" || m.Key == "" || keys[m.Key] || (m.Kind != "manager" && m.Kind != "staff") {
			continue
		}
		keys[m.Key] = true
		if m.Kind == "manager" {
			m.Skill = ""
		} else if !staffSkills[m.Skill] {
			m.Skill = "developer"
		}
		sk := []string{}
		for _, k := range m.SkillKeys {
			if skillKeys[k] {
				sk = append(sk, k)
			}
		}
		m.SkillKeys = sk
		members = append(members, m)
	}
	hasManager := false
	for i := range members {
		if members[i].Kind == "manager" {
			hasManager = true
		}
		if members[i].Kind == "staff" && !isManagerKey(members, members[i].ReportsToKey) {
			members[i].ReportsToKey = ""
		}
		if members[i].Kind == "manager" {
			members[i].ReportsToKey = ""
		}
	}
	if !hasManager {
		return setupChatReply{Reply: raw.Reply}
	}
	raw.Team = &chatTeam{Members: members}
	return raw
}

func isManagerKey(ms []chatMember, key string) bool {
	for _, m := range ms {
		if m.Key == key && m.Kind == "manager" {
			return true
		}
	}
	return false
}

// handleSetupChat is POST /orgs/{orgID}/setup/chat: the setup wizard's assistant. It asks Claude
// on the admin's own computer and returns a reply plus an optional team draft. Nothing is stored.
func (s *Server) handleSetupChat(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	if _, err := authz.OrgAdmin(ctx, s.Store.Queries, orgID, uid); err != nil {
		return err
	}
	var in struct {
		RunnerID    uuid.UUID `json:"runner_id"`
		Workspace   struct{ Name, About string }
		Directories []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"directories"`
		Skills []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"skills"`
		Messages []chatMessage `json:"messages"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if len(in.Messages) == 0 || len(in.Messages) > 40 || strings.TrimSpace(in.Messages[len(in.Messages)-1].Text) == "" {
		return httpx.Validation{"messages": "Write a message first."}.Err()
	}
	rn, err := s.Store.GetRunner(ctx, in.RunnerID)
	if err != nil || rn.OrgID != orgID || rn.RevokedAt != nil || rn.OwnerUserID != uid {
		return httpx.ErrNotFound
	}
	if s.Runners == nil || !s.Runners.Online(rn.ID) {
		return httpx.NewError(http.StatusConflict, "runner_offline", "Your computer isn’t connected right now. Connect it and try again.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\nAbout: %s\nFolders:\n", in.Workspace.Name, in.Workspace.About)
	for _, d := range in.Directories {
		fmt.Fprintf(&b, "- %s (%s)\n", d.Name, d.Kind)
	}
	b.WriteString("Skills you can pick (key: name):\n")
	skillKeys := map[string]bool{}
	for _, k := range in.Skills {
		skillKeys[k.Key] = true
		fmt.Fprintf(&b, "- %s: %s\n", k.Key, k.Name)
	}
	b.WriteString("\nConversation so far:\n")
	for _, m := range in.Messages {
		who := "Person"
		if m.Role == "assistant" {
			who = "You"
		}
		fmt.Fprintf(&b, "%s: %s\n", who, strings.TrimSpace(m.Text))
	}
	b.WriteString("\nAnswer now with the JSON object.")

	rctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var res protocol.AskResult
	if err := s.Runners.Request(rctx, rn.ID, protocol.TAsk, protocol.Ask{System: setupChatSystem, Prompt: b.String(), Model: "sonnet"}, &res); err != nil {
		return httpx.NewError(http.StatusBadGateway, "chat_failed", "I couldn’t get an answer from your computer. Please try again.")
	}
	httpx.JSON(w, http.StatusOK, parseSetupChat(res.Text, skillKeys))
	return nil
}
