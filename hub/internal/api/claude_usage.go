package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/httpx"
	"canopy/shared/protocol"
)

// ClaudeWindow is one of Claude Code's own limit windows (five_hour, seven_day, ...).
type ClaudeWindow struct {
	Key         string     `json:"key"`
	UsedPercent float64    `json:"used_percent"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
	Status      string     `json:"status,omitempty"`
}

// ClaudeComputerUsage is what Claude Code reported for one of the caller's computers.
// Reported is false until Claude Code has sent its first limit report; show "no data yet", not 0%.
type ClaudeComputerUsage struct {
	RunnerID   uuid.UUID      `json:"runner_id"`
	Name       string         `json:"name"`
	Online     bool           `json:"online"`
	SignIn     string         `json:"sign_in"` // "subscription", "api_key", or "" when Claude isn't set up
	Reported   bool           `json:"reported"`
	ReportedAt *time.Time     `json:"reported_at,omitempty"`
	Windows    []ClaudeWindow `json:"windows"`
}

// parseClaudeWindows reads the account's utilization json: the "windows" object, or for older
// rows the bare per-window fractions next to resets_at.
func parseClaudeWindows(raw []byte) (ws []ClaudeWindow, reportedAt *time.Time) {
	ws = []ClaudeWindow{}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ws, nil
	}
	if b, ok := m["reported_at"]; ok {
		_ = json.Unmarshal(b, &reportedAt)
	}
	if b, ok := m["windows"]; ok {
		var w map[string]protocol.RateWindow
		if json.Unmarshal(b, &w) == nil {
			for k, v := range w {
				ws = append(ws, ClaudeWindow{Key: k, UsedPercent: v.Utilization * 100, ResetsAt: v.ResetsAt, Status: v.Status})
			}
		}
	} else {
		for k, b := range m {
			var f float64
			if k != "resets_at" && k != "reported_at" && json.Unmarshal(b, &f) == nil {
				ws = append(ws, ClaudeWindow{Key: k, UsedPercent: f * 100})
			}
		}
	}
	order := func(k string) int {
		switch k {
		case "five_hour":
			return 0
		case "seven_day":
			return 1
		}
		return 2
	}
	slices.SortFunc(ws, func(a, b ClaudeWindow) int {
		if d := order(a.Key) - order(b.Key); d != 0 {
			return d
		}
		switch {
		case a.Key < b.Key:
			return -1
		case a.Key > b.Key:
			return 1
		}
		return 0
	})
	return ws, reportedAt
}

// handleClaudeUsage is GET /orgs/{orgID}/usage/claude: Claude Code's own limit reports for the
// computers the caller owns.
func (s *Server) handleClaudeUsage(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	me := identity(r).UserID
	if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, me); err != nil {
		return err
	}
	runners, err := s.Store.ListOrgRunners(ctx, orgID)
	if err != nil {
		return err
	}
	conns, err := s.Store.ListOrgConnections(ctx, orgID)
	if err != nil {
		return err
	}
	accounts, err := s.Store.ListAccounts(ctx, orgID)
	if err != nil {
		return err
	}
	out := []ClaudeComputerUsage{}
	for _, rn := range runners {
		if rn.OwnerUserID != me {
			continue
		}
		c := ClaudeComputerUsage{RunnerID: rn.ID, Name: rn.Name, Online: rn.Online, Windows: []ClaudeWindow{}}
		for _, cc := range conns {
			if cc.RunnerID == rn.ID && cc.Cli == "claude" && cc.Enabled {
				c.SignIn = cc.Mode
			}
		}
		for _, a := range accounts {
			if a.RunnerID != rn.ID || a.Cli != "claude" {
				continue
			}
			ws, at := parseClaudeWindows(a.Utilization)
			if len(ws) == 0 {
				continue
			}
			if at == nil {
				at = &a.UpdatedAt
			}
			c.Windows, c.Reported, c.ReportedAt = ws, true, at
		}
		out = append(out, c)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"computers": out})
	return nil
}
