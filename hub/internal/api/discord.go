package api

import (
	"encoding/json"

	"github.com/google/uuid"

	"net/http"
	"strings"
	"time"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/store/db"
)

// DiscordView is a workspace's Discord settings. The webhook token is never included.
type DiscordView struct {
	Configured bool            `json:"configured"`
	Webhook    string          `json:"webhook"` // masked
	Kinds      map[string]bool `json:"kinds"`
	RoleID     string          `json:"role_id"`
	Forum      bool            `json:"forum"`
	LastOKAt   *time.Time      `json:"last_ok_at"`
	LastError  string          `json:"last_error"`
}

func toDiscordView(ws db.Workspace) DiscordView {
	return DiscordView{Configured: ws.DiscordWebhook != "", Webhook: notify.MaskWebhook(ws.DiscordWebhook),
		Kinds: notify.ParseDiscordKinds(ws.DiscordKinds), RoleID: ws.DiscordRoleID, Forum: ws.DiscordForum,
		LastOKAt: ws.DiscordLastOkAt, LastError: ws.DiscordLastError}
}

// discordAdmin loads the workspace for its admins; everyone else gets 404.
func (s *Server) discordAdmin(r *http.Request) (db.Workspace, error) {
	wsID, err := pathID(r, "wsID")
	if err != nil {
		return db.Workspace{}, err
	}
	ws, v, err := authz.LoadWorkspace(r.Context(), s.Store.Queries, wsID, identity(r).UserID)
	if err != nil {
		return ws, err
	}
	if !v.Admin() {
		return ws, httpx.ErrNotFound
	}
	return ws, nil
}

func (s *Server) handleGetDiscord(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.discordAdmin(r)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"discord": toDiscordView(ws)})
	return nil
}

func (s *Server) handlePutDiscord(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.discordAdmin(r)
	if err != nil {
		return err
	}
	var req struct {
		Webhook *string         `json:"webhook"` // omitted or empty keeps the saved one
		Kinds   map[string]bool `json:"kinds"`
		RoleID  *string         `json:"role_id"`
		Forum   *bool           `json:"forum"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx, uid := r.Context(), identity(r).UserID
	val := httpx.Validation{}
	hook := ws.DiscordWebhook
	if req.Webhook != nil && strings.TrimSpace(*req.Webhook) != "" {
		hook = strings.TrimSpace(*req.Webhook)
		if notify.ValidateWebhook(hook) != nil {
			val.Add("webhook", "Paste a Discord webhook URL. It starts with https://discord.com/api/webhooks/.")
		}
	}
	if hook == "" {
		val.Add("webhook", "Paste a Discord webhook URL.")
	}
	kinds := notify.ParseDiscordKinds(ws.DiscordKinds)
	if req.Kinds != nil {
		var ok bool
		if kinds, ok = notify.CleanKinds(req.Kinds); !ok {
			val.Add("kinds", "Choose from questions, progress, goal_ready and errors.")
		}
	}
	role := ws.DiscordRoleID
	if req.RoleID != nil {
		role = strings.TrimSpace(*req.RoleID)
		if !notify.ValidRoleID(role) {
			val.Add("role_id", "A role id is a number. Copy it from Discord with developer mode on.")
		}
	}
	forum := ws.DiscordForum
	if req.Forum != nil {
		forum = *req.Forum
	}
	if err := val.Err(); err != nil {
		return err
	}
	raw, _ := json.Marshal(kinds)
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		ws, err = t.Q.SetWorkspaceDiscord(ctx, db.SetWorkspaceDiscordParams{ID: ws.ID, Webhook: hook, Kinds: raw, RoleID: role, Forum: forum})
		if err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &uid, "workspace.discord_set", "Set the Discord webhook for "+ws.Name)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"discord": toDiscordView(ws)})
	return nil
}

func (s *Server) handleDeleteDiscord(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.discordAdmin(r)
	if err != nil {
		return err
	}
	ctx, uid := r.Context(), identity(r).UserID
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		ws, err = t.Q.ClearWorkspaceDiscord(ctx, ws.ID)
		if err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &ws.OrgID, &uid, "workspace.discord_removed", "Removed the Discord webhook from "+ws.Name)
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"discord": toDiscordView(ws)})
	return nil
}

// handleTestDiscord queues a sample message; the dispatcher sends it like any other.
func (s *Server) handleTestDiscord(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.discordAdmin(r)
	if err != nil {
		return err
	}
	if ws.DiscordWebhook == "" {
		return httpx.FieldError(http.StatusBadRequest, "no_webhook", "webhook", "Save a Discord webhook first.")
	}
	ctx := r.Context()
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		return notify.Enqueue(ctx, t, ws, notify.Note{Kind: "progress", Recipients: []uuid.UUID{},
			Body: "This is a test message from Canopy. Notifications for this workspace will appear here.", Media: notify.Media{Test: true}})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true})
	return nil
}
