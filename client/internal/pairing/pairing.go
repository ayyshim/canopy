// Package pairing trades a one-time code from Canopy for this machine's certificate.
package pairing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"canopy/client/internal/config"
)

type Options struct {
	HubURL  string
	Code    string
	Name    string
	Address string // for servers: the address people know it by
	Version string
}

func Pair(ctx context.Context, o Options) (config.Hub, error) {
	o.HubURL = strings.TrimRight(o.HubURL, "/")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return config.Hub{}, err
	}
	host, _ := os.Hostname()
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: host}}, key)
	if err != nil {
		return config.Hub{}, err
	}
	body, _ := json.Marshal(map[string]string{
		"code": o.Code, "csr": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})),
		"name": o.Name, "hostname": host, "os": runtime.GOOS, "arch": runtime.GOARCH, "version": o.Version, "address": o.Address,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.HubURL+"/api/v1/runners/pair", bytes.NewReader(body))
	if err != nil {
		return config.Hub{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return config.Hub{}, fmt.Errorf("couldn’t reach the hub at %s: %w", o.HubURL, err)
	}
	defer resp.Body.Close()
	var out struct {
		RunnerID   string `json:"runner_id"`
		RunnerName string `json:"runner_name"`
		OrgID      string `json:"org_id"`
		OrgName    string `json:"org_name"`
		HubName    string `json:"hub_name"`
		CertPEM    string `json:"cert_pem"`
		CAPEM      string `json:"ca_pem"`
		RunnerURL  string `json:"runner_url"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return config.Hub{}, fmt.Errorf("unexpected reply from the hub (%s)", resp.Status)
	}
	if resp.StatusCode != http.StatusCreated {
		if out.Error != nil {
			return config.Hub{}, fmt.Errorf("%s", out.Error.Message)
		}
		return config.Hub{}, fmt.Errorf("pairing failed: %s", resp.Status)
	}
	dir, err := config.OrgDir(o.HubURL, out.OrgID)
	if err != nil {
		return config.Hub{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return config.Hub{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return config.Hub{}, err
	}
	files := map[string][]byte{
		"key.pem":  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		"cert.pem": []byte(out.CertPEM),
		"ca.pem":   []byte(out.CAPEM),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return config.Hub{}, err
		}
	}
	h := config.Hub{URL: o.HubURL, RunnerURL: out.RunnerURL, RunnerID: out.RunnerID, RunnerName: out.RunnerName, OrgID: out.OrgID,
		OrgName: out.OrgName, HubName: out.HubName, Dir: dir}
	return h, h.Save()
}
