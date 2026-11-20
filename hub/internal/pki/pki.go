// Package pki is the hub's small certificate authority: it signs a client certificate for each
// paired machine and a server certificate for the runner listener (SPEC "Runner-to-server
// traffic over mutual TLS with per-machine keys").
package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/google/uuid"

	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

type CA struct {
	Cert    *x509.Certificate
	Key     *ecdsa.PrivateKey
	CertPEM []byte
}

// Load returns the hub's CA, creating it on first use.
func Load(ctx context.Context, st *store.Store) (*CA, error) {
	row, err := st.GetHubPKI(ctx)
	if store.IsNotFound(err) {
		certPEM, keyPEM, gerr := generateCA()
		if gerr != nil {
			return nil, gerr
		}
		if _, err := st.CreateHubPKI(ctx, db.CreateHubPKIParams{CaCertPem: string(certPEM), CaKeyPem: string(keyPEM)}); err != nil && !store.IsNotFound(err) {
			return nil, err
		}
		row, err = st.GetHubPKI(ctx)
	}
	if err != nil {
		return nil, err
	}
	return parse([]byte(row.CaCertPem), []byte(row.CaKeyPem))
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	return n
}

func generateCA() ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: "Canopy hub CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kd, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), nil
}

func parse(certPEM, keyPEM []byte) (*CA, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, errors.New("malformed hub CA")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key, CertPEM: certPEM}, nil
}

// Pool is the trust pool containing only this CA.
func (c *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(c.Cert)
	return p
}

// ServerCert issues a TLS certificate for the runner listener, valid for the given hosts.
func (c *CA) ServerCert(hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: "Canopy hub"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if h != "" {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, c.Cert, &key.PublicKey, c.Key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der, c.Cert.Raw}, PrivateKey: key}, nil
}

// SignCSR issues a machine's client certificate. Returns the PEM and its fingerprint.
func (c *CA) SignCSR(csrPEM []byte, runnerID uuid.UUID) ([]byte, string, error) {
	b, _ := pem.Decode(csrPEM)
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		return nil, "", errors.New("malformed certificate request")
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		return nil, "", err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, "", fmt.Errorf("certificate request signature: %w", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: "runner:" + runnerID.String()},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, c.Cert, csr.PublicKey, c.Key)
	if err != nil {
		return nil, "", err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), Fingerprint(der), nil
}

// Fingerprint identifies a certificate: hex SHA-256 of its DER bytes.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
