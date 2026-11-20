//go:build embedpg

package pgserver

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// Release builds bundle the PostgreSQL archive so the hub needs no network to start.
//
//go:embed pgbin
var bundled embed.FS

func seedBinaries(cacheDir string) error {
	entries, err := fs.ReadDir(bundled, "pgbin")
	if err != nil {
		return err
	}
	for _, e := range entries {
		dst := filepath.Join(cacheDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := bundled.ReadFile("pgbin/" + e.Name())
		if err != nil {
			return err
		}
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
