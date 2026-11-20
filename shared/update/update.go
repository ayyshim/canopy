// Package update replaces a running Canopy binary with a newer build, from a GitHub release or
// from a URL such as the hub's client download. A release build is checked against the
// release's SHA256SUMS before anything is replaced.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases come from; CANOPY_REPO changes it (for forks).
func Repo() string {
	if r := os.Getenv("CANOPY_REPO"); r != "" {
		return r
	}
	return "ayyshim/canopy"
}

// Asset is the release file for this system, such as canopy-hub-linux-amd64.
func Asset(name string) string { return name + "-" + runtime.GOOS + "-" + runtime.GOARCH }

// githubURL is github.com; tests point it at a local server.
var githubURL = "https://github.com"

var client = &http.Client{Timeout: 10 * time.Minute}

// noRedirect answers with the redirect itself instead of following it.
var noRedirect = &http.Client{
	Timeout:       time.Minute,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// retry runs once up to three times while it fails with an error worth retrying: GitHub’s
// servers sometimes answer 500 for a moment.
func retry[T any](ctx context.Context, once func() (T, bool, error)) (T, error) {
	var v T
	var err error
	for try := range 3 {
		if try > 0 {
			select {
			case <-ctx.Done():
				return v, ctx.Err()
			case <-time.After(time.Duration(try*2) * time.Second):
			}
		}
		var again bool
		if v, again, err = once(); err == nil || !again {
			return v, err
		}
	}
	return v, err
}

// get downloads url.
func get(ctx context.Context, url string) ([]byte, error) {
	return retry(ctx, func() ([]byte, bool, error) { return getOnce(ctx, url) })
}

func getOnce(ctx context.Context, url string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "canopy-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return b, err != nil, err
}

// LatestTag returns the newest release's tag, such as v0.2.0. Pre-releases are skipped.
// It reads the tag from where github.com/OWNER/REPO/releases/latest redirects rather than
// asking GitHub’s API, which answers only 60 requests an hour from an address that isn’t
// signed in.
func LatestTag(ctx context.Context) (string, error) {
	url := githubURL + "/" + Repo() + "/releases/latest"
	tag, err := retry(ctx, func() (string, bool, error) { return latestOnce(ctx, url) })
	if err != nil {
		return "", fmt.Errorf("find the latest release: %w", err)
	}
	return tag, nil
}

func latestOnce(ctx context.Context, url string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "canopy-updater")
	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", true, err
	}
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", resp.StatusCode >= 500, fmt.Errorf("%s: %s", url, resp.Status)
	}
	// A repository without releases redirects to its releases page instead of a tag.
	_, tag, ok := strings.Cut(resp.Header.Get("Location"), "/releases/tag/")
	if !ok || tag == "" || strings.Contains(tag, "/") {
		return "", false, fmt.Errorf("%s has no releases", Repo())
	}
	return tag, false, nil
}

func releaseURL(tag, file string) string {
	return githubURL + "/" + Repo() + "/releases/download/" + tag + "/" + file
}

// ReleaseSum returns the checksum the release lists for asset.
func ReleaseSum(ctx context.Context, tag, asset string) (string, error) {
	b, err := get(ctx, releaseURL(tag, "SHA256SUMS"))
	if err != nil {
		return "", fmt.Errorf("download the checksums for %s: %w", tag, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("release %s has no build for this system (%s)", tag, asset)
}

// Release downloads asset from release tag and checks it against want.
func Release(ctx context.Context, tag, asset, want string) ([]byte, error) {
	b, err := get(ctx, releaseURL(tag, asset))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	if Sum(b) != want {
		return nil, fmt.Errorf("the downloaded %s doesn’t match its checksum in %s’s SHA256SUMS; nothing was changed", asset, tag)
	}
	return b, nil
}

// Download fetches a build from url, such as a hub's /download/canopy-linux-amd64.
func Download(ctx context.Context, url string) ([]byte, error) { return get(ctx, url) }

func Sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// FileSum returns the checksum of the file at path.
func FileSum(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Sum(b), nil
}

// Self is the path of the running binary, with symlinks resolved.
func Self() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p, nil
}

// Replace writes b over target: a temporary file next to it, then a rename, so a running copy
// keeps working until it restarts.
func Replace(target string, b []byte) error {
	if !bytes.HasPrefix(b, []byte("\x7fELF")) && !isMachO(b) {
		return errors.New("the download isn’t a program for this system; nothing was changed")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".update.*")
	if err != nil {
		return permission(target, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return permission(target, os.Rename(tmp.Name(), target))
}

func isMachO(b []byte) bool {
	return len(b) > 4 && (bytes.HasPrefix(b, []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.HasPrefix(b, []byte{0xfe, 0xed, 0xfa, 0xcf}))
}

func permission(target string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("can’t replace %s. Run the update with sudo", target)
	}
	return err
}

// Newer reports whether release version a is newer than b. Versions look like v1.2.3 or 1.2.3.
// A pre-release (1.2.3-rc.1) is older than 1.2.3; a build from git describe (1.2.3-4-gabc123,
// four commits after 1.2.3) is newer. It returns false when either isn't a version.
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// Valid reports whether v is a release version such as 1.2.3, not a development build.
func Valid(v string) bool {
	_, ok := parse(v)
	return ok
}

// parse turns v1.2.3 into {1, 2, 3, 1}. The last number is 0 for a pre-release and 2 for a
// build after the release.
func parse(v string) ([4]int, bool) {
	var out [4]int
	v = strings.TrimPrefix(v, "v")
	core, suffix, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	switch {
	case suffix == "":
		out[3] = 1
	case afterRelease.MatchString(suffix):
		out[3] = 2
	}
	return out, true
}

var afterRelease = regexp.MustCompile(`^[0-9]+-g[0-9a-f]+$`)
