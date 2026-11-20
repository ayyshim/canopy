// Package blobs moves file contents to and from the hub, addressed by SHA-256.
package blobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"canopy/shared/protocol"
)

type Client struct {
	Base string // https://host:port of the runner listener
	HTTP *http.Client
}

// BaseFromRunnerURL turns wss://host:port/ws/runners into https://host:port.
func BaseFromRunnerURL(runnerURL string) string {
	u, err := url.Parse(runnerURL)
	if err != nil {
		return ""
	}
	scheme := "https"
	if u.Scheme == "ws" {
		scheme = "http"
	}
	return scheme + "://" + u.Host
}

// HashFile returns a file's SHA-256 and size.
func HashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Key is where the hub stores content (matches the hub's blob.ContentKey).
func Key(sha string) string { return "content/" + sha[:2] + "/" + sha }

// MimeOf guesses a file's type from its extension.
func MimeOf(p string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(p))); t != "" {
		return t
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md":
		return "text/markdown"
	case ".diff", ".patch":
		return "text/x-diff"
	}
	return "application/octet-stream"
}

// UploadFile sends a file unless the hub already has it.
func (c *Client) UploadFile(ctx context.Context, p string) (sha string, size int64, err error) {
	sha, size, err = HashFile(p)
	if err != nil {
		return "", 0, err
	}
	missing, err := c.Missing(ctx, []string{sha})
	if err != nil {
		return "", 0, err
	}
	if len(missing) == 0 {
		return sha, size, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.Base+"/runner/v1/blobs/"+sha, f)
	if err != nil {
		return "", 0, err
	}
	req.ContentLength = size
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", 0, fmt.Errorf("upload %s: %s %s", filepath.Base(p), resp.Status, strings.TrimSpace(string(b)))
	}
	return sha, size, nil
}

// Missing returns which hashes the hub doesn't have.
func (c *Client) Missing(ctx context.Context, shas []string) ([]string, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(protocol.BlobsMissing{SHA256: shas})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/runner/v1/blobs/missing", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check blobs: %s", resp.Status)
	}
	var out protocol.BlobsMissing
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.SHA256, nil
}

// Download writes a blob to dest atomically, checking its hash.
func (c *Client) Download(ctx context.Context, sha, dest string, mode os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/runner/v1/blobs/"+sha, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", sha[:12], resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".canopy-*")
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if hex.EncodeToString(h.Sum(nil)) != sha {
		os.Remove(tmp.Name())
		return fmt.Errorf("download %s: content doesn’t match", sha[:12])
	}
	if mode == 0 {
		mode = 0o644
	}
	_ = os.Chmod(tmp.Name(), mode)
	return os.Rename(tmp.Name(), dest)
}
