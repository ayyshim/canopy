package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"canopy/hub/internal/domain"
	"canopy/hub/internal/httpx"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/store/db"
)

// RunnerHub reaches connected runners. Set by main; nil in tests that don't need runners.
type RunnerHub interface {
	Online(runnerID uuid.UUID) bool
	Send(runnerID uuid.UUID, typ string, data any) error
	Request(ctx context.Context, runnerID uuid.UUID, typ string, data any, out any) error
}

// Orchestrator is told about changes that can unblock or reshape agent work.
type Orchestrator interface {
	Kick(ctx context.Context, workspaceID uuid.UUID)
	MemberChanged(ctx context.Context, memberID uuid.UUID)
	StaffProposalDecided(ctx context.Context, p db.StaffProposal)
	MemberRemoved(ctx context.Context, workspaceID, memberID uuid.UUID)
	DirectoriesChanged(ctx context.Context, workspaceID uuid.UUID)

	ReopenGoal(ctx context.Context, t *core.Tx, ws db.Workspace, g db.Goal) error
	MarkGoalDone(ctx context.Context, goalID, by uuid.UUID) error
	ShowGoalTasks(ctx context.Context, goalID uuid.UUID, show bool) error
	MoveGoal(ctx context.Context, goalID, runnerID, by uuid.UUID, admin bool) error
	ShipGoal(ctx context.Context, goalID uuid.UUID, action string, by uuid.UUID) (orch.ShipResult, error)
	RestartSessions(ctx context.Context, workspaceID uuid.UUID) (int, error)
}

func accountView(c db.CliConnection, a *db.Account) core.AccountView {
	v := core.AccountView{RunnerID: c.RunnerID, CLI: c.Cli, Mode: c.Mode, CapTokensPerDay: c.CapTokensPerDay, KeyFingerprint: c.KeyFingerprint,
		Enabled: c.Enabled, Utilization: []byte("{}")}
	if a != nil {
		v.UsedTokens, v.LimitedUntil, v.Paused, v.Utilization = a.UsedTokens, a.LimitedUntil, a.Paused, a.Utilization
	}
	return v
}

// connectionsView is every computer's CLI sign-ins in an organization.
func (s *Server) connectionsView(ctx context.Context, orgID uuid.UUID) ([]core.AccountView, error) {
	conns, err := s.Store.ListOrgConnections(ctx, orgID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.Store.ListAccounts(ctx, orgID)
	if err != nil {
		return nil, err
	}
	type key struct {
		runner uuid.UUID
		cli    string
	}
	byKey := map[key]*db.Account{}
	for i := range accounts {
		byKey[key{accounts[i].RunnerID, accounts[i].Cli}] = &accounts[i]
	}
	out := make([]core.AccountView, 0, len(conns))
	for _, c := range conns {
		out = append(out, accountView(c, byKey[key{c.RunnerID, c.Cli}]))
	}
	return out, nil
}

// handleListConnections lists how each computer's CLIs sign in.
func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, identity(r).UserID); err != nil {
		return err
	}
	out, err := s.connectionsView(ctx, orgID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"connections": out, "clis": domain.CLIs})
	return nil
}

// canManageRunner: a computer's owner sets up its sign-ins; admins do it for shared computers.
func (s *Server) canManageRunner(ctx context.Context, rn db.Runner, userID uuid.UUID) error {
	m, err := authz.OrgMember(ctx, s.Store.Queries, rn.OrgID, userID)
	if err != nil || rn.RevokedAt != nil {
		return httpx.ErrNotFound
	}
	if rn.OwnerUserID == userID || authz.CanAdministerOrg(m.OrgRole) {
		return nil
	}
	return httpx.ErrNotFound
}

func (s *Server) handlePutConnection(w http.ResponseWriter, r *http.Request) error {
	runnerID, err := pathID(r, "runnerID")
	if err != nil {
		return err
	}
	cli := pathParam(r, "cli")
	ctx := r.Context()
	actor := identity(r).UserID
	rn, err := s.Store.GetRunner(ctx, runnerID)
	if err != nil {
		return httpx.ErrNotFound
	}
	if err := s.canManageRunner(ctx, rn, actor); err != nil {
		return err
	}
	info, ok := domain.CLI(cli)
	if !ok {
		return httpx.ErrNotFound
	}
	var in struct {
		Enabled         bool   `json:"enabled"`
		Mode            string `json:"mode"`
		CapTokensPerDay *int64 `json:"cap_tokens_per_day"`
		APIKey          string `json:"api_key"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	val := httpx.Validation{}
	if in.Enabled && !info.Supported {
		val.Add("enabled", info.Name+" isn’t supported yet.")
	}
	if in.Mode != "subscription" && in.Mode != "api_key" {
		val.Add("mode", "Choose subscription or API key.")
	}
	if in.CapTokensPerDay != nil && *in.CapTokensPerDay <= 0 {
		val.Add("cap_tokens_per_day", "Use a number above zero, or leave it empty.")
	}
	if rn.Shared && in.Mode != "api_key" {
		val.Add("mode", "A shared computer must sign in with an API key; a subscription is personal.")
	}
	if err := val.Err(); err != nil {
		return err
	}
	if err := s.saveConnection(ctx, rn, actor, cli, in.Enabled, in.Mode, in.CapTokensPerDay, in.APIKey); err != nil {
		return err
	}
	delivered := s.relayKey(ctx, rn.ID, cli, in.APIKey)
	out, err := s.connectionsView(ctx, rn.OrgID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"connections": out, "key_delivered": delivered})
	if s.Orch != nil {
		go s.Orch.Kick(context.WithoutCancel(ctx), uuid.Nil)
	}
	return nil
}

func (s *Server) saveConnection(ctx context.Context, rn db.Runner, actor uuid.UUID, cli string, enabled bool, mode string, capTokens *int64, apiKey string) error {
	info, _ := domain.CLI(cli)
	fp := ""
	if strings.TrimSpace(apiKey) != "" {
		fp = keyFingerprint(apiKey)
	}
	return s.Core.Tx(ctx, func(t *core.Tx) error {
		c, err := t.Q.UpsertCliConnection(ctx, db.UpsertCliConnectionParams{ID: newID(), OrgID: rn.OrgID, RunnerID: rn.ID, Cli: cli, Mode: mode,
			CapTokensPerDay: capTokens, KeyFingerprint: fp, Enabled: enabled})
		if err != nil {
			return err
		}
		a, err := t.Q.EnsureAccount(ctx, db.EnsureAccountParams{ID: newID(), OrgID: rn.OrgID, RunnerID: rn.ID, Cli: cli, Mode: mode})
		if err != nil {
			return err
		}
		if err := t.Emit(ctx, core.Org(rn.OrgID, "account.updated"), accountView(c, &a)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &rn.OrgID, &actor, "connections.changed", "Changed how "+info.Name+" signs in on "+rn.Name)
	})
}

// relayKey hands an API key to the computer, which stores it in the CLI's config. The hub
// keeps only the fingerprint. Reports whether the computer took it.
func (s *Server) relayKey(ctx context.Context, runnerID uuid.UUID, cli, key string) bool {
	if s.Runners == nil || strings.TrimSpace(key) == "" || !s.Runners.Online(runnerID) {
		return false
	}
	if err := s.Runners.Send(runnerID, "set_api_key", map[string]string{"cli": cli, "key": strings.TrimSpace(key)}); err != nil {
		slog.WarnContext(ctx, "relay api key", "runner", runnerID, "err", err)
		return false
	}
	return true
}
