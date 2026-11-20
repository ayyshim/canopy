package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"canopy/hub/internal/auth"
	"canopy/hub/internal/authz"
	"canopy/hub/internal/core"
	"github.com/google/uuid"

	"canopy/hub/internal/httpx"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/shared/protocol"
)

const pairingTTL = 15 * time.Minute

func toRunnerView(r db.Runner) core.RunnerView {
	v := core.RunnerView{ID: r.ID, Name: r.Name, Kind: r.Kind, OwnerUserID: r.OwnerUserID, Hostname: r.Hostname, OS: r.Os,
		Version: r.Version, Sandbox: r.Sandbox, Online: r.Online, Shared: r.Shared, LastSeenAt: r.LastSeenAt}
	var clis []core.CLIStatus
	_ = json.Unmarshal(r.Clis, &clis)
	if clis == nil {
		clis = []core.CLIStatus{}
	}
	v.Detected = clis
	return v
}

func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	m, err := authz.OrgMember(ctx, s.Store.Queries, orgID, uid)
	if err != nil {
		return err
	}
	rows, err := s.Store.ListOrgRunners(ctx, orgID)
	if err != nil {
		return err
	}
	out := make([]core.RunnerView, 0, len(rows))
	for _, rn := range rows {
		v := toRunnerView(rn)
		if s.Runners != nil {
			v.Online = s.Runners.Online(rn.ID)
		}
		out = append(out, v)
	}
	defaults := map[string]*uuid.UUID{}
	if list, err := s.Store.ListDefaultRunners(ctx, orgID); err == nil {
		for _, d := range list {
			defaults[d.UserID.String()] = d.DefaultRunnerID
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"runners": out, "my_computer": m.DefaultRunnerID, "computers_by_person": defaults})
	return nil
}

// pairingCode is 8 characters from an alphabet without look-alikes, shown as XXXX-XXXX.
func pairingCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

// NormalizePairingCode accepts codes typed with or without the dash, in any case.
func NormalizePairingCode(code string) string {
	c := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

// handleCreatePairing issues a one-time code a person types into `canopy pair`.
// Any organization member may pair their own computer (to share directories); servers that
// run agents for everyone are paired by admins.
func (s *Server) handleCreatePairing(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	m, err := authz.OrgMember(ctx, s.Store.Queries, orgID, uid)
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
	}
	if in.Kind == "" {
		in.Kind = "computer"
	}
	if in.Kind != "computer" && in.Kind != "server" {
		return httpx.Validation{"kind": "Choose this computer or a server."}.Err()
	}
	if in.Kind == "server" && !authz.CanAdministerOrg(m.OrgRole) {
		return httpx.ErrForbidden
	}
	code := pairingCode()
	p, err := s.Store.CreatePairing(ctx, db.CreatePairingParams{ID: newID(), OrgID: orgID, UserID: uid, CodeHash: auth.HashToken(code),
		Name: strings.TrimSpace(in.Name), Kind: in.Kind, ExpiresAt: s.Now().Add(pairingTTL)})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"pairing_id": p.ID, "code": code, "expires_at": p.ExpiresAt,
		"command": "canopy pair --hub " + s.Cfg.PublicURL + " --code " + code,
	})
	return nil
}

func (s *Server) handleRevokeRunner(w http.ResponseWriter, r *http.Request) error {
	runnerID, err := pathID(r, "runnerID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	rn, err := s.Store.GetRunner(ctx, runnerID)
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	m, err := authz.OrgMember(ctx, s.Store.Queries, rn.OrgID, uid)
	if err != nil {
		return err
	}
	if rn.OwnerUserID != uid && !authz.CanAdministerOrg(m.OrgRole) {
		return httpx.ErrForbidden
	}
	err = s.Core.Tx(ctx, func(t *core.Tx) error {
		if err := t.Q.RevokeRunner(ctx, runnerID); err != nil {
			return err
		}
		if err := t.Emit(ctx, core.Org(rn.OrgID, "runner.updated"), map[string]any{"id": rn.ID, "online": false, "revoked": true}); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &rn.OrgID, &uid, "runners.revoked", "Disconnected the machine "+rn.Name)
	})
	if err != nil {
		return err
	}
	if s.Runners != nil {
		_ = s.Runners.Send(runnerID, "revoked", map[string]any{})
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type pairRequest struct {
	Code     string `json:"code"`
	CSR      string `json:"csr"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Address  string `json:"address"`
}

var errBadPairingCode = httpx.FieldError(http.StatusBadRequest, "bad_pairing_code", "code", "That pairing code is wrong or has expired. Get a new one from Canopy.")

// handlePair is called by `canopy pair`: it trades a one-time code and a certificate request
// for a client certificate, the hub CA and the runner URL.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) error {
	if s.PKI == nil {
		return httpx.NewError(http.StatusServiceUnavailable, "unavailable", "This hub isn’t accepting machines right now.")
	}
	var in pairRequest
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	var rn db.Runner
	var certPEM []byte
	var org db.Organization
	err := s.Core.Tx(ctx, func(t *core.Tx) error {
		p, err := t.Q.ClaimPairing(ctx, auth.HashToken(NormalizePairingCode(in.Code)))
		if store.IsNotFound(err) {
			return errBadPairingCode
		} else if err != nil {
			return err
		}
		org, err = t.Q.GetOrganization(ctx, p.OrgID)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = strings.TrimSpace(in.Name)
		}
		if name == "" {
			name = strings.TrimSpace(in.Hostname)
		}
		if name == "" {
			name = "My computer"
		}
		id := newID()
		var fp string
		certPEM, fp, err = s.PKI.SignCSR([]byte(in.CSR), id)
		if err != nil {
			return httpx.FieldError(http.StatusBadRequest, "bad_csr", "csr", "The certificate request is malformed.")
		}
		rn, err = t.Q.CreateRunner(ctx, db.CreateRunnerParams{ID: id, OrgID: p.OrgID, OwnerUserID: p.UserID, Name: name, Kind: p.Kind,
			CertFingerprint: &fp, Hostname: in.Hostname, Os: in.OS, Arch: in.Arch, Version: in.Version})
		if err != nil {
			return err
		}
		if in.Address != "" {
			if err := t.Q.SetRunnerAddress(ctx, db.SetRunnerAddressParams{ID: id, Address: in.Address}); err != nil {
				return err
			}
		}
		if err := t.Q.SetPairingRunner(ctx, db.SetPairingRunnerParams{ID: p.ID, RunnerID: &id}); err != nil {
			return err
		}
		if p.Kind == "computer" {
			if err := bindOwnerDirectories(ctx, t.Q, rn); err != nil {
				return err
			}
			// The first computer a person pairs runs their goals.
			if err := t.Q.SetDefaultRunnerIfEmpty(ctx, db.SetDefaultRunnerIfEmptyParams{OrgID: p.OrgID, UserID: p.UserID, DefaultRunnerID: &id}); err != nil {
				return err
			}
		}
		if err := t.Emit(ctx, core.Org(p.OrgID, "runner.updated"), toRunnerView(rn)); err != nil {
			return err
		}
		return s.audit(ctx, t.Q, &p.OrgID, &p.UserID, "runners.paired", "Connected the machine "+name)
	})
	if err != nil {
		return err
	}
	hub, _ := s.Store.GetHubSettings(ctx)
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"runner_id": rn.ID, "runner_name": rn.Name, "org_id": org.ID, "org_name": org.Name, "hub_name": hub.Name,
		"cert_pem": string(certPEM), "ca_pem": string(s.PKI.CertPEM), "runner_url": s.Cfg.RunnerURL,
	})
	return nil
}

// handleGetPairing lets the setup page wait for `canopy pair` to finish.
func (s *Server) handleGetPairing(w http.ResponseWriter, r *http.Request) error {
	orgID, err := pathID(r, "orgID")
	if err != nil {
		return err
	}
	pairingID, err := pathID(r, "pairingID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	if _, err := authz.OrgMember(ctx, s.Store.Queries, orgID, uid); err != nil {
		return err
	}
	p, err := s.Store.GetPairing(ctx, pairingID)
	if err != nil || p.OrgID != orgID || p.UserID != uid {
		return httpx.ErrNotFound
	}
	out := map[string]any{"paired": p.RunnerID != nil, "expires_at": p.ExpiresAt}
	if p.RunnerID != nil {
		rn, err := s.Store.GetRunner(ctx, *p.RunnerID)
		if err != nil {
			return err
		}
		v := toRunnerView(rn)
		if s.Runners != nil {
			v.Online = s.Runners.Online(rn.ID)
		}
		out["runner"] = v
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// handleBrowse lists folders on a person's own connected machine for the directory picker.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) error {
	runnerID, err := pathID(r, "runnerID")
	if err != nil {
		return err
	}
	ctx := r.Context()
	uid := identity(r).UserID
	rn, err := s.Store.GetRunner(ctx, runnerID)
	if err != nil || rn.RevokedAt != nil {
		return httpx.ErrNotFound
	}
	m, err := authz.OrgMember(ctx, s.Store.Queries, rn.OrgID, uid)
	if err != nil {
		return err
	}
	// Browsing a machine is for its owner (their own files), or admins on a shared computer.
	if rn.OwnerUserID != uid && !((rn.Kind == "server" || rn.Shared) && authz.CanAdministerOrg(m.OrgRole)) {
		return httpx.ErrNotFound
	}
	if s.Runners == nil || !s.Runners.Online(runnerID) {
		return httpx.NewError(http.StatusConflict, "runner_offline", "That machine isn’t connected right now.")
	}
	var out protocol.BrowseResult
	if err := s.Runners.Request(ctx, runnerID, protocol.TBrowse, protocol.Browse{Path: r.URL.Query().Get("path")}, &out); err != nil {
		return httpx.NewError(http.StatusBadGateway, "browse_failed", "Couldn’t list that folder: "+err.Error())
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}
