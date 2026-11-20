package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// Hub admin dashboard: every organization and user on this hub, hub settings and totals.

type page struct {
	q             string
	limit, offset int32
}

func readPage(r *http.Request) page {
	p := page{q: strings.TrimSpace(r.URL.Query().Get("q")), limit: 50}
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		p.limit = int32(n)
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
		p.offset = int32(n)
	}
	return p
}

type auditView struct {
	Type    string    `json:"type"`
	Summary string    `json:"summary"`
	At      time.Time `json:"at"`
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	totals, err := s.Store.HubTotals(ctx)
	if err != nil {
		return err
	}
	hub, err := s.Store.GetHubSettings(ctx)
	if err != nil {
		return err
	}
	events, err := s.Store.ListHubAuditEvents(ctx, 10)
	if err != nil {
		return err
	}
	recent := make([]auditView, 0, len(events))
	for _, e := range events {
		recent = append(recent, auditView{Type: e.Type, Summary: e.Summary, At: e.At})
	}
	upgrades, err := s.Store.ListUpgrades(ctx)
	if err != nil {
		return err
	}
	ups := make([]map[string]any, 0, len(upgrades))
	for _, u := range upgrades {
		ups = append(ups, map[string]any{"from_version": u.FromVersion, "to_version": u.ToVersion, "from_schema": u.FromSchema,
			"to_schema": u.ToSchema, "changes": json.RawMessage(u.Changes), "backup_path": u.BackupPath, "at": u.At})
	}
	usage, err := s.hubUsage(ctx)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"usage":    usage,
		"hub":      map[string]any{"name": hub.Name, "signup_open": hub.SignupOpen, "set_up_at": hub.SetupCompletedAt, "version": s.Version},
		"upgrades": ups,
		"totals": map[string]int64{
			"organizations": totals.Organizations, "users": totals.Users, "hub_admins": totals.HubAdmins,
			"active_users_24h": totals.ActiveUsers24h, "users_7d": totals.Users7d, "organizations_7d": totals.Organizations7d,
		},
		"recent_events": recent,
	})
	return nil
}

type adminOrgRow struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Size      string    `json:"size"`
	Members   int64     `json:"members"`
	CreatedAt time.Time `json:"created_at"`
	Owner     personRef `json:"owner"`
}

type personRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

func (s *Server) handleAdminListOrgs(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p := readPage(r)
	rows, err := s.Store.AdminListOrganizations(ctx, db.AdminListOrganizationsParams{Q: p.q, Lim: p.limit, Off: p.offset})
	if err != nil {
		return err
	}
	total, err := s.Store.AdminCountOrganizations(ctx, p.q)
	if err != nil {
		return err
	}
	items := make([]adminOrgRow, 0, len(rows))
	for _, o := range rows {
		items = append(items, adminOrgRow{ID: o.ID, Name: o.Name, Slug: o.Slug, Size: o.Size, Members: o.Members,
			CreatedAt: o.CreatedAt, Owner: personRef{ID: o.OwnerID, Name: o.OwnerName, Email: o.OwnerEmail}})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	return nil
}

type memberRow struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	JoinedAt time.Time `json:"joined_at"`
}

func (s *Server) handleAdminGetOrg(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	org, err := s.Store.GetOrganization(ctx, orgID)
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	rows, err := s.Store.AdminListOrgMembers(ctx, orgID)
	if err != nil {
		return err
	}
	members := make([]memberRow, 0, len(rows))
	for _, m := range rows {
		members = append(members, memberRow{ID: m.ID, Name: m.Name, Email: m.Email, Role: m.OrgRole, Status: m.Status, JoinedAt: m.JoinedAt})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"organization": toOrgView(org), "members": members})
	return nil
}

type adminUserRow struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	IsHubAdmin    bool       `json:"is_hub_admin"`
	Organizations int64      `json:"organizations"`
	CreatedAt     time.Time  `json:"created_at"`
	LastActiveAt  *time.Time `json:"last_active_at"`
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p := readPage(r)
	rows, err := s.Store.AdminListUsers(ctx, db.AdminListUsersParams{Q: p.q, Lim: p.limit, Off: p.offset})
	if err != nil {
		return err
	}
	total, err := s.Store.AdminCountUsers(ctx, p.q)
	if err != nil {
		return err
	}
	items := make([]adminUserRow, 0, len(rows))
	for _, u := range rows {
		row := adminUserRow{ID: u.ID, Name: u.Name, Email: u.Email, IsHubAdmin: u.IsHubAdmin,
			Organizations: u.Organizations, CreatedAt: u.CreatedAt}
		if u.LastActiveAt.Unix() > 0 { // epoch means never
			t := u.LastActiveAt
			row.LastActiveAt = &t
		}
		items = append(items, row)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	return nil
}

func (s *Server) handleAdminGetUser(w http.ResponseWriter, r *http.Request) error {
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.Store.GetUserByID(ctx, userID)
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	orgs, err := s.Store.AdminListUserOrgs(ctx, userID)
	if err != nil {
		return err
	}
	list := make([]orgSummary, 0, len(orgs))
	for _, o := range orgs {
		list = append(list, orgSummary{ID: o.ID, Name: o.Name, Role: o.OrgRole})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user":          map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "is_hub_admin": u.IsHubAdmin, "created_at": u.CreatedAt},
		"organizations": list,
	})
	return nil
}

var errLastHubAdmin = httpx.NewError(http.StatusConflict, "last_hub_admin", "A hub needs at least one hub admin.")

// handleAdminPatchUser grants or removes hub admin; the last hub admin can't be removed.
func (s *Server) handleAdminPatchUser(w http.ResponseWriter, r *http.Request) error {
	userID, err := pathID(r, "userID")
	if err != nil {
		return err
	}
	var req struct {
		IsHubAdmin *bool `json:"is_hub_admin"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.IsHubAdmin == nil {
		return httpx.Validation{"is_hub_admin": "Say whether this person is a hub admin."}.Err()
	}
	ctx := r.Context()
	actor := identity(r).UserID
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		if err := q.LockHubAdmins(ctx); err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, userID)
		if store.IsNotFound(err) {
			return httpx.ErrNotFound
		} else if err != nil {
			return err
		}
		if u.IsHubAdmin == *req.IsHubAdmin {
			return nil
		}
		if err := q.SetHubAdmin(ctx, db.SetHubAdminParams{ID: userID, IsHubAdmin: *req.IsHubAdmin}); err != nil {
			return err
		}
		if n, err := q.CountHubAdmins(ctx); err != nil {
			return err
		} else if n == 0 {
			return errLastHubAdmin
		}
		verb := "made %s a hub admin"
		if !*req.IsHubAdmin {
			verb = "removed %s as a hub admin"
		}
		return s.audit(ctx, q, nil, &actor, "hub.admin_changed", fmt.Sprintf(verb, u.Email))
	})
	if err != nil {
		return err
	}
	return s.handleAdminGetUser(w, r)
}

type providerView struct {
	Enabled     bool   `json:"enabled"`
	ClientID    string `json:"client_id"`
	HasSecret   bool   `json:"has_secret"`
	CallbackURL string `json:"callback_url"`
}

func (s *Server) settingsView(r *http.Request) (map[string]any, error) {
	ctx := r.Context()
	hub, err := s.Store.GetHubSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.ListAuthProviders(ctx)
	if err != nil {
		return nil, err
	}
	providers := map[string]providerView{}
	for _, name := range providerNames {
		providers[name] = providerView{CallbackURL: s.oauthCallback(name)}
	}
	for _, p := range rows {
		providers[p.Provider] = providerView{Enabled: p.Enabled, ClientID: p.ClientID, HasSecret: p.ClientSecret != "",
			CallbackURL: s.oauthCallback(p.Provider)}
	}
	return map[string]any{"name": hub.Name, "signup_open": hub.SignupOpen, "providers": providers}, nil
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) error {
	view, err := s.settingsView(r)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, view)
	return nil
}

func (s *Server) handleAdminPatchSettings(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name       *string                  `json:"name"`
		SignupOpen *bool                    `json:"signup_open"`
		Providers  map[string]providerInput `json:"providers"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	stored := map[string]bool{}
	rows, err := s.Store.ListAuthProviders(ctx)
	if err != nil {
		return err
	}
	for _, p := range rows {
		stored[p.Provider] = p.ClientSecret != ""
	}
	v := httpx.Validation{}
	if req.Name != nil {
		validateHubName(v, "name", req.Name)
	}
	validateProviders(v, req.Providers, func(name string) bool { return stored[name] })
	if err := v.Err(); err != nil {
		return err
	}
	actor := identity(r).UserID
	err = s.Store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.UpdateHubSettings(ctx, db.UpdateHubSettingsParams{Name: req.Name, SignupOpen: req.SignupOpen}); err != nil {
			return err
		}
		if err := upsertProviders(r, q, req.Providers); err != nil {
			return err
		}
		return s.audit(ctx, q, nil, &actor, "hub.settings_changed", "Hub settings changed")
	})
	if err != nil {
		return err
	}
	return s.handleAdminGetSettings(w, r)
}
