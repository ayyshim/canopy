//go:build !embedpg

package pgserver

// seedBinaries is a no-op in builds without bundled PostgreSQL (it's downloaded on first start).
func seedBinaries(string) error { return nil }
