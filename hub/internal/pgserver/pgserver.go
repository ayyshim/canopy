// Package pgserver runs the hub's own PostgreSQL when no DATABASE_URL is given, keeping its
// data in the hub's data directory. Upgrading the hub (a new binary) never wipes data: the
// Postgres major version is pinned, and the data directory is copied to a backup before any
// schema upgrade.
package pgserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// Major is the PostgreSQL major version this hub runs. Changing it needs a pg_upgrade step,
// which the hub refuses to skip.
const Major = "16"

type Server struct {
	DataDir string // the hub's data directory
	pg      *embeddedpostgres.EmbeddedPostgres
	URL     string
}

// State is what the hub remembers about its data between versions.
type State struct {
	HubVersion    string    `json:"hub_version"`
	SchemaVersion int64     `json:"schema_version"`
	DBPassword    string    `json:"db_password"`
	DBPort        uint32    `json:"db_port"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func StatePath(dataDir string) string { return filepath.Join(dataDir, "hub-state.json") }

func LoadState(dataDir string) (State, error) {
	var s State
	b, err := os.ReadFile(StatePath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

func SaveState(dataDir string, s State) error {
	s.UpdatedAt = time.Now()
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := StatePath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath(dataDir))
}

func pgData(dataDir string) string { return filepath.Join(dataDir, "postgres", "data") }

// CheckMajor refuses data from another PostgreSQL major version instead of re-initializing it.
func CheckMajor(dataDir string) error {
	b, err := os.ReadFile(filepath.Join(pgData(dataDir), "PG_VERSION"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if v := strings.TrimSpace(string(b)); v != Major {
		return fmt.Errorf("the database in %s is PostgreSQL %s, but this hub runs PostgreSQL %s. Nothing was changed. "+
			"Use a hub version built for PostgreSQL %s, or upgrade the data with pg_upgrade first", pgData(dataDir), v, Major, v)
	}
	return nil
}

func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

func portFree(p uint32) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// Start runs PostgreSQL on a loopback port with the data in dataDir/postgres/data.
func Start(dataDir string, st *State, log io.Writer) (*Server, error) {
	if err := CheckMajor(dataDir); err != nil {
		return nil, err
	}
	if st.DBPassword == "" {
		b := make([]byte, 18)
		_, _ = rand.Read(b)
		st.DBPassword = hex.EncodeToString(b)
	}
	if st.DBPort == 0 || !portFree(st.DBPort) {
		p, err := freePort()
		if err != nil {
			return nil, err
		}
		st.DBPort = p
	}
	if err := seedBinaries(filepath.Join(dataDir, "postgres", "cache")); err != nil {
		slog.Warn("couldn’t unpack the bundled PostgreSQL; it will be downloaded", "err", err)
	}
	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.PostgresVersion(versionFor(Major))).
		Port(st.DBPort).Username("canopy").Password(st.DBPassword).Database("canopy").
		DataPath(pgData(dataDir)).
		RuntimePath(filepath.Join(dataDir, "postgres", "runtime")).
		CachePath(filepath.Join(dataDir, "postgres", "cache")).
		StartTimeout(2 * time.Minute).
		Logger(log)
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start PostgreSQL: %w", err)
	}
	return &Server{DataDir: dataDir, pg: pg, URL: fmt.Sprintf("postgres://canopy:%s@127.0.0.1:%d/canopy?sslmode=disable", st.DBPassword, st.DBPort)}, nil
}

func versionFor(major string) string {
	switch major {
	case "16":
		return "16.9.0"
	case "17":
		return "17.5.0"
	}
	return "18.3.0"
}

func (s *Server) Stop() error { return s.pg.Stop() }

// Backup copies the (stopped) PostgreSQL data directory, so an upgrade can be undone by
// putting it back. Returns the backup's path.
func Backup(dataDir, label string) (string, error) {
	src := pgData(dataDir)
	if _, err := os.Stat(src); err != nil {
		return "", nil // nothing to back up yet
	}
	dst := filepath.Join(dataDir, "backups", time.Now().Format("20060102-150405")+"-"+label)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, "postgres-data", rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		return "", err
	}
	_ = os.WriteFile(filepath.Join(dst, "RESTORE.txt"), []byte(
		"To undo the upgrade: stop the hub, replace "+src+" with postgres-data from this folder,\n"+
			"and start the previous hub binary.\n"), 0o644)
	return dst, nil
}
