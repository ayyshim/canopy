// Command canopy-hub is the self-hosted Canopy hub in one binary: the API, the web app, the
// runner endpoint and (unless DATABASE_URL points elsewhere) its own PostgreSQL. Upgrading is
// replacing the binary: on start it backs up and upgrades the data, and says what changed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/acme/autocert"

	"canopy/hub/internal/api"
	"canopy/hub/internal/auth"
	"canopy/hub/internal/blob"
	"canopy/hub/internal/config"
	"canopy/hub/internal/mail"
	"canopy/hub/internal/mirror"
	"canopy/hub/internal/notify"
	"canopy/hub/internal/orch"
	"canopy/hub/internal/pgserver"
	"canopy/hub/internal/pki"
	"canopy/hub/internal/realtime"
	"canopy/hub/internal/runners"
	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
	"canopy/hub/internal/web"
	"canopy/shared/selfinstall"
)

var version = "0.1.0-dev"

const usage = `canopy-hub runs a self-hosted Canopy hub.

Usage:
  canopy-hub [serve] [flags]     run the hub (the default)
  canopy-hub install [--dir DIR] [--no-path]
                                 copy this binary to ~/.local/bin/canopy-hub (/usr/local/bin as
                                 root) and add that folder to your PATH
  canopy-hub migrate status      show the data's version and what an upgrade would change
  canopy-hub migrate up          upgrade the data now (the hub also does this on start)
  canopy-hub backup              copy the database (stop the hub first)
  canopy-hub version

Serve flags (each also readable from the environment, shown in brackets):
  --data DIR           where the hub keeps everything [CANOPY_DATA_DIR] (default
                       ~/.local/share/canopy-hub, or /var/lib/canopy as root)
  --addr ADDR          web and API listen address [HUB_ADDR] (default :8080)
  --public-url URL     the address people open [PUBLIC_URL] (default http://localhost:8080)
  --domain NAME        serve HTTPS for NAME on :443 with automatic certificates
  --runner-addr ADDR   where machines connect, mutual TLS [RUNNER_ADDR] (default :8443)
  --no-auto-migrate    refuse to start if the data needs an upgrade

Without DATABASE_URL the hub runs its own PostgreSQL ` + pgserver.Major + ` inside the data directory.

Other settings (environment):
  DATABASE_URL         use your own PostgreSQL instead of the built-in one
  SMTP_ADDR, SMTP_FROM send email through this SMTP server (without it, emails go to the log)
  HUB_SETUP_TOKEN      the code in the first-run setup link (default: generated and logged)
  RUNNER_URL           the address machines dial, if not PUBLIC_URL's host on the runner port
  RUNNER_HOSTS         extra names for the runner certificate, comma separated
  REDIS_URL            share live updates and rate limits between several hubs
  S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET, S3_SECURE
                       keep files in S3-compatible storage instead of the data directory
  BLOB_DIR             where files go without S3 (default DATA/blobs)
  COOKIE_SECURE        set false only for plain http that isn't localhost
  TRUST_PROXY          true behind a reverse proxy that sets X-Forwarded-For
  AUTH_RATE_LIMIT      sign-in attempts per minute per address (default 10)
  DEFAULT_SESSION_LIMIT, DEFAULT_WEEKLY_LIMIT
                       token limits for new organizations (default 2000000 and 20000000)
`

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || args[0] == "-h" || args[0] == "--help" || args[0] == "--version") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "install":
		err = selfinstall.Run("canopy-hub", args)
	case "migrate":
		err = migrateCmd(args)
	case "backup":
		err = backupCmd(args)
	case "version", "--version":
		fmt.Printf("canopy-hub %s (schema %d, PostgreSQL %s)\n", version, store.LatestSchema(), pgserver.Major)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

type options struct {
	data, addr, publicURL, domain, runnerAddr string
	noAutoMigrate                             bool
}

func parseFlags(name string, args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&o.data, "data", envOr("CANOPY_DATA_DIR", defaultDataDir()), "data directory")
	fs.StringVar(&o.addr, "addr", "", "listen address")
	fs.StringVar(&o.publicURL, "public-url", "", "public URL")
	fs.StringVar(&o.domain, "domain", "", "domain for automatic HTTPS")
	fs.StringVar(&o.runnerAddr, "runner-addr", "", "runner listen address")
	fs.BoolVar(&o.noAutoMigrate, "no-auto-migrate", false, "refuse to upgrade data on start")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	abs, err := filepath.Abs(o.data)
	if err != nil {
		return o, err
	}
	o.data = abs
	// Flags win over the environment; config reads the environment.
	set := func(k, v string) {
		if v != "" {
			os.Setenv(k, v)
		}
	}
	set("HUB_ADDR", o.addr)
	set("PUBLIC_URL", o.publicURL)
	set("RUNNER_ADDR", o.runnerAddr)
	if o.domain != "" {
		set("PUBLIC_URL", "https://"+o.domain)
		os.Setenv("COOKIE_SECURE", "true")
	}
	if os.Getenv("PUBLIC_URL") == "" && os.Getenv("HUB_ADDR") == "" {
		os.Setenv("PUBLIC_URL", "http://localhost:8080")
		os.Setenv("COOKIE_SECURE", envOr("COOKIE_SECURE", "false"))
	}
	if os.Getenv("BLOB_DIR") == "" {
		os.Setenv("BLOB_DIR", filepath.Join(o.data, "blobs"))
	}
	return o, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// defaultDataDir is the same folder whichever folder the hub starts in.
func defaultDataDir() string {
	if os.Geteuid() == 0 {
		return "/var/lib/canopy"
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "canopy-hub")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "canopy-hub")
}

// warnOldDataDir points at ./canopy-data, where earlier hubs kept their data.
func warnOldDataDir(using string) {
	old, err := filepath.Abs("canopy-data")
	if err != nil || old == using {
		return
	}
	if _, err := os.Stat(filepath.Join(old, "postgres")); err == nil {
		slog.Warn("found a hub’s data in ./canopy-data, but this hub uses another folder. To keep using that data, start the hub with: --data "+old, "using", using)
	}
}

// lockData keeps two hubs from using one data directory.
func lockData(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "hub.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another hub is using %s; stop it first", dir)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// database opens the store: the hub's own PostgreSQL or DATABASE_URL. Before an upgrade it
// backs up the data, and it refuses data from a newer hub.
type database struct {
	st       *store.Store
	pg       *pgserver.Server
	state    pgserver.State
	backup   string
	external bool
}

func openDatabase(ctx context.Context, o options) (*database, error) {
	d := &database{}
	var err error
	if d.state, err = pgserver.LoadState(o.data); err != nil {
		return nil, err
	}
	latest := store.LatestSchema()
	url := os.Getenv("DATABASE_URL")
	d.external = url != ""
	if !d.external {
		if d.state.SchemaVersion > latest {
			return nil, fmt.Errorf("this data was upgraded by a newer hub (%s, schema %d); this binary is %s (schema %d). "+
				"Run the newer hub, or restore a backup from %s", d.state.HubVersion, d.state.SchemaVersion, version, latest, filepath.Join(o.data, "backups"))
		}
		if d.state.SchemaVersion > 0 && d.state.SchemaVersion < latest {
			if o.noAutoMigrate {
				return nil, upgradeNeeded(d.state.SchemaVersion, latest)
			}
			// PostgreSQL isn't running yet, so a plain copy is a consistent backup.
			if d.backup, err = pgserver.Backup(o.data, fmt.Sprintf("before-%s", version)); err != nil {
				return nil, fmt.Errorf("back up before upgrading: %w", err)
			}
		}
		slog.Info("starting the hub’s PostgreSQL", "data", filepath.Join(o.data, "postgres", "data"))
		if d.pg, err = pgserver.Start(o.data, &d.state, newPGLog(o.data)); err != nil {
			return nil, err
		}
		url = d.pg.URL
	}
	if d.st, err = store.Open(ctx, url); err != nil {
		d.close()
		return nil, err
	}
	return d, nil
}

func upgradeNeeded(from, to int64) error {
	return fmt.Errorf("the data needs an upgrade (schema %d → %d):\n  - %s\nRun `canopy-hub migrate up` (it backs up first), or start without --no-auto-migrate",
		from, to, strings.Join(store.MigrationNames(from), "\n  - "))
}

func (d *database) close() {
	if d.st != nil {
		d.st.Close()
	}
	if d.pg != nil {
		if err := d.pg.Stop(); err != nil {
			slog.Warn("stop PostgreSQL", "err", err)
		}
	}
}

func newPGLog(dataDir string) *os.File {
	_ = os.MkdirAll(filepath.Join(dataDir, "logs"), 0o700)
	f, err := os.OpenFile(filepath.Join(dataDir, "logs", "postgres.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return os.Stderr
	}
	return f
}

// upgrade applies pending migrations, says what changed, and records it.
func (d *database) upgrade(ctx context.Context, o options) error {
	status, err := d.st.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	if len(status.Pending) == 0 {
		return d.remember(status.Latest)
	}
	if status.Current > 0 && o.noAutoMigrate {
		return upgradeNeeded(status.Current, status.Latest)
	}
	if status.Current > 0 && d.external {
		if p, err := exec.LookPath("pg_dump"); err == nil {
			dst := filepath.Join(o.data, "backups", time.Now().Format("20060102-150405")+"-before-"+version+".sql")
			_ = os.MkdirAll(filepath.Dir(dst), 0o700)
			if out, err := exec.CommandContext(ctx, p, "--no-owner", "-f", dst, os.Getenv("DATABASE_URL")).CombinedOutput(); err != nil {
				return fmt.Errorf("back up before upgrading (pg_dump): %v: %s", err, out)
			}
			d.backup = dst
		} else {
			slog.Warn("pg_dump isn’t installed, so no backup was taken before upgrading; back up your database yourself")
		}
	}
	if status.Current > 0 {
		slog.Warn("upgrading the hub’s data", "from_hub", firstNonEmpty(d.state.HubVersion, "unknown"), "to_hub", version,
			"from_schema", status.Current, "to_schema", status.Latest, "changes", strings.Join(status.Pending, ", "), "backup", firstNonEmpty(d.backup, "none"))
	} else {
		slog.Info("setting up a new database", "schema", status.Latest)
	}
	if err := d.st.Migrate(ctx); err != nil {
		return fmt.Errorf("upgrade failed; your data is unchanged in the backup at %s: %w", firstNonEmpty(d.backup, "(none)"), err)
	}
	if status.Current > 0 {
		changes, _ := jsonMarshal(status.Pending)
		if err := d.st.RecordUpgrade(ctx, db.RecordUpgradeParams{ID: uuid.Must(uuid.NewV7()), FromVersion: firstNonEmpty(d.state.HubVersion, "unknown"),
			ToVersion: version, FromSchema: status.Current, ToSchema: status.Latest, Changes: changes, BackupPath: d.backup}); err != nil {
			return err
		}
		slog.Info("upgrade finished", "schema", status.Latest)
	}
	return d.remember(status.Latest)
}

func (d *database) remember(schema int64) error {
	if d.external {
		return nil
	}
	d.state.SchemaVersion, d.state.HubVersion = schema, version
	return pgserver.SaveState(d.pg.DataDir, d.state)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func serve(args []string) error {
	o, err := parseFlags("serve", args)
	if err != nil {
		return err
	}
	warnOldDataDir(o.data)
	unlock, err := lockData(o.data)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbs, err := openDatabase(ctx, o)
	if err != nil {
		return err
	}
	defer dbs.close()
	if err := dbs.upgrade(ctx, o); err != nil {
		return err
	}
	st := dbs.st

	var mailer mail.Sender = mail.Log{}
	if cfg.SMTPAddr != "" {
		mailer = mail.SMTP{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom}
	}
	var limiter auth.Limiter = &auth.MemoryLimiter{}
	var bus realtime.Bus = realtime.NewMemory()
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return err
		}
		rc := redis.NewClient(opt)
		limiter = auth.RedisLimiter{Client: rc}
		bus = realtime.NewRedis(ctx, rc)
	}
	ca, err := pki.Load(ctx, st)
	if err != nil {
		return err
	}
	var blobs blob.Store = blob.Dir{Root: cfg.BlobDir}
	if cfg.S3Endpoint != "" {
		if blobs, err = blob.NewS3(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Secure); err != nil {
			return err
		}
	}
	if err := st.MarkAllRunnersOffline(ctx); err != nil {
		return err
	}

	app := api.New(cfg, st, mailer, limiter, bus)
	app.Version = version
	rh := runners.New(st, ca, blobs)
	orchestrator := orch.New(app.Core, rh, blobs)
	mirrorSvc := mirror.New(app.Core, rh, blobs, orchestrator)
	orchestrator.Mirror = mirrorSvc
	rh.Handler = orchestrator
	app.Runners, app.Orch, app.PKI, app.Blobs, app.Resolver, app.Mirror = rh, orchestrator, ca, blobs, orchestrator, mirrorSvc
	if web.HasApp() || os.Getenv("SERVE_WEB") != "" {
		app.WebApp = web.App()
	}
	app.Download, app.Install = web.Download(), web.InstallScript(cfg.PublicURL)
	go orchestrator.Run(ctx)
	go notify.New(st, mailer, cfg.PublicURL).Run(ctx)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				app.DrainQueue(ctx)
				if time.Now().Minute() == 0 && time.Now().Second() < 30 {
					// Retention: the activity log keeps 400 days, session logs 90.
					_ = st.PruneAudit(ctx)
					_ = st.PruneLogs(ctx)
				}
			}
		}
	}()

	tlsCfg, err := rh.TLSConfig(cfg.RunnerHosts)
	if err != nil {
		return err
	}
	runnerSrv := &http.Server{Addr: cfg.RunnerAddr, Handler: rh.Routes(), TLSConfig: tlsCfg, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := runnerSrv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("runner listener stopped", "err", err)
			stop()
		}
	}()
	if _, err := st.GetHubSettings(ctx); store.IsNotFound(err) {
		if app.SetupToken == "" {
			app.SetupToken, _ = auth.NewToken()
		}
		slog.Warn("this hub isn't set up yet; open the setup link to name it and create the first hub admin",
			"setup_url", cfg.PublicURL+"/hub-setup?token="+app.SetupToken)
	} else if err != nil {
		return err
	}

	handler := app.Routes()
	servers := []*http.Server{runnerSrv}
	if o.domain != "" {
		m := &autocert.Manager{Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist(o.domain), Cache: autocert.DirCache(filepath.Join(o.data, "certs"))}
		https := &http.Server{Addr: ":443", Handler: handler, TLSConfig: m.TLSConfig(), ReadHeaderTimeout: 10 * time.Second}
		redirect := &http.Server{Addr: ":80", Handler: m.HTTPHandler(nil), ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, https, redirect)
		go func() { _ = redirect.ListenAndServe() }()
		go func() {
			if err := https.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("https stopped", "err", err)
				stop()
			}
		}()
	} else {
		srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, srv)
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("hub stopped", "err", err)
				stop()
			}
		}()
	}
	u, _ := url.Parse(cfg.PublicURL)
	slog.Info("canopy-hub is running", "version", version, "open", cfg.PublicURL, "web_app", app.WebApp != nil,
		"machines_connect_to", cfg.RunnerURL, "data", o.data, "host", u.Host)
	<-ctx.Done()
	slog.Info("stopping")
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	return nil
}

func migrateCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: canopy-hub migrate status|up")
	}
	sub := args[0]
	o, err := parseFlags("migrate", args[1:])
	if err != nil {
		return err
	}
	unlock, err := lockData(o.data)
	if err != nil {
		return err
	}
	defer unlock()
	ctx := context.Background()
	if sub == "status" {
		o.noAutoMigrate = true // just look
	}
	d, err := openDatabase(ctx, o)
	if err != nil {
		return err
	}
	defer d.close()
	switch sub {
	case "status":
		s, err := d.st.MigrationStatus(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Hub binary: %s (schema %d)\nData: schema %d (last run by hub %s)\n", version, s.Latest, s.Current, firstNonEmpty(d.state.HubVersion, "unknown"))
		if len(s.Pending) == 0 {
			fmt.Println("Up to date.")
			return nil
		}
		fmt.Printf("Starting this hub will back up the data and apply %d change(s):\n", len(s.Pending))
		for _, p := range s.Pending {
			fmt.Println("  -", p)
		}
		return nil
	case "up":
		o.noAutoMigrate = false
		return d.upgrade(ctx, o)
	}
	return fmt.Errorf("usage: canopy-hub migrate status|up")
}

func backupCmd(args []string) error {
	o, err := parseFlags("backup", args)
	if err != nil {
		return err
	}
	if os.Getenv("DATABASE_URL") != "" {
		return fmt.Errorf("with DATABASE_URL, back up with your database’s own tools (pg_dump)")
	}
	unlock, err := lockData(o.data)
	if err != nil {
		return err
	}
	defer unlock()
	p, err := pgserver.Backup(o.data, "manual")
	if err != nil {
		return err
	}
	fmt.Println("Backed up to", p)
	return nil
}
