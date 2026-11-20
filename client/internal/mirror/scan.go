package mirror

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	"canopy/client/internal/blobs"
	"canopy/shared/protocol"
)

// MaxFileSize: larger files aren't mirrored (build artifacts, media); agents read them only
// on the owner's own machine.
const MaxFileSize = 10 << 20

var defaultIgnores = []string{
	".git/", "node_modules/", ".next/", ".nuxt/", "dist/", "build/", "target/", "out/", ".venv/", "venv/", "__pycache__/",
	".cache/", "coverage/", ".terraform/", ".gradle/", ".idea/", "*.pyc", ".DS_Store", "*.log", "*.swp",
	// Secrets never leave the owner's machine.
	".env", ".env.*", "!.env.example", "!.env.sample", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "id_ecdsa*", "*.p12", "*.pfx",
	"*.keystore", "*.jks", ".npmrc", ".pypirc", ".netrc", ".git-credentials", "credentials.json", "service-account*.json", ".aws/", ".ssh/", ".gnupg/",
}

var ignoreFiles = []string{".gitignore", ".dockerignore", ".canopyignore"}

type cacheEntry struct {
	size  int64
	mtime int64
	sha   string
}

// hasher remembers hashes by size and modification time so rescans are cheap.
type hasher struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
}

func (h *hasher) hash(p string, fi fs.FileInfo) (string, error) {
	h.mu.Lock()
	c, ok := h.cache[p]
	h.mu.Unlock()
	if ok && c.size == fi.Size() && c.mtime == fi.ModTime().UnixNano() {
		return c.sha, nil
	}
	sha, _, err := blobs.HashFile(p)
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	if h.cache == nil {
		h.cache = map[string]cacheEntry{}
	}
	h.cache[p] = cacheEntry{size: fi.Size(), mtime: fi.ModTime().UnixNano(), sha: sha}
	h.mu.Unlock()
	return sha, nil
}

// scan lists the files to mirror under root, honoring ignore files at every level.
func scan(root string, h *hasher) (map[string]protocol.FileEntry, []string, error) {
	files := map[string]protocol.FileEntry{}
	var dirs []string
	var patterns []gitignore.Pattern
	for _, p := range defaultIgnores {
		patterns = append(patterns, gitignore.ParsePattern(p, nil))
	}
	var walk func(dir string, domain []string, inherited []gitignore.Pattern) error
	walk = func(dir string, domain []string, inherited []gitignore.Pattern) error {
		pats := append([]gitignore.Pattern{}, inherited...)
		for _, name := range ignoreFiles {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimRight(line, "\r")
				if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
					continue
				}
				pats = append(pats, gitignore.ParsePattern(line, domain))
			}
		}
		m := gitignore.NewMatcher(pats)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		dirs = append(dirs, dir)
		for _, e := range entries {
			parts := append(append([]string{}, domain...), e.Name())
			if e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			if m.Match(parts, e.IsDir()) {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if err := walk(full, parts, pats); err != nil && !os.IsPermission(err) {
					return err
				}
				continue
			}
			fi, err := e.Info()
			if err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxFileSize {
				continue
			}
			sha, err := h.hash(full, fi)
			if err != nil {
				continue
			}
			rel := strings.Join(parts, "/")
			files[rel] = protocol.FileEntry{Path: rel, SHA256: sha, Size: fi.Size(), Mode: uint32(fi.Mode().Perm())}
		}
		return nil
	}
	if err := walk(root, nil, patterns); err != nil {
		return nil, nil, err
	}
	return files, dirs, nil
}

// diff returns what changed from old to new (deletions marked).
func diff(old, cur map[string]protocol.FileEntry) []protocol.FileEntry {
	var out []protocol.FileEntry
	for p, f := range cur {
		if o, ok := old[p]; !ok || o.SHA256 != f.SHA256 || o.Mode != f.Mode {
			out = append(out, f)
		}
	}
	for p := range old {
		if _, ok := cur[p]; !ok {
			out = append(out, protocol.FileEntry{Path: p, Deleted: true})
		}
	}
	return out
}

// safeJoin resolves a mirrored relative path inside root, refusing escapes.
func safeJoin(root, rel string) (string, bool) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	r := filepath.Clean(root)
	return p, p != r && strings.HasPrefix(p, r+string(filepath.Separator))
}
