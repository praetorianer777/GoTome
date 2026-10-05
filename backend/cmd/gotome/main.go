// Command gotome is the GOtome server and its maintenance subcommands.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/metadata/openlibrary"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/reading"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
	"github.com/praetorianer777/gotome/backend/internal/version"
	"github.com/praetorianer777/gotome/backend/internal/webui"
)

// Server timeouts. Downloads and streams set their own deadlines when they
// arrive; these bound an ordinary API request.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownGrace     = 25 * time.Second
	healthcheckWait   = 3 * time.Second
	// jobsShutdownWait is longer than the grace the job runner gives its
	// running jobs, so that its own cancelling of them still happens.
	jobsShutdownWait     = 40 * time.Second
	sessionSweepInterval = time.Hour
)

const usage = `Usage: gotome <command>

Commands:
  serve        Bring the database schema up to date, then run the server
  init         Write the database password for a new installation, once
  healthcheck  Probe a running server; exits non-zero unless it is ready
  migrate      Bring the database schema up to date and exit
  openapi      Write the API's OpenAPI document to the given file, or to stdout
  version      Print the version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("gotome exited", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	switch args[0] {
	case "serve":
		return serve()
	case "init":
		return runInit()
	case "healthcheck":
		return healthcheck()
	case "migrate":
		return migrate()
	case "openapi":
		return writeOpenAPI(args[1:])
	case "version":
		fmt.Println(version.Current())
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cfg.RequireSecretKey(); err != nil {
		return err
	}
	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	pool, err := openDatabase(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	settingStore, err := settings.Open(ctx, pool, box)
	if err != nil {
		return err
	}

	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		return err
	}
	// Background work runs in this process, on the same database.
	libraries := library.NewService(pool, cfg.DataDir)
	coverStore := covers.NewStore(cfg.DataDir)
	scans := ingest.NewService(pool, libraries, coverStore, log)
	scans.UploadLimit = cfg.UploadLimit
	providers := []metadata.Provider{openlibrary.New()}
	if cfg.Offline {
		providers = nil
	}
	meta := metadata.NewService(pool, providers, metadata.Options{
		Language: func(ctx context.Context) string {
			lang, _ := settingStore.Text(ctx, settings.MetadataLanguage)
			return lang
		},
		Enabled: func(ctx context.Context) []string {
			names, _ := settingStore.Text(ctx, settings.Providers)
			return strings.Split(names, ",")
		},
	})
	matches := enrich.NewService(pool, meta, scans, coverStore, settingStore, log)
	scans.OnExtracted = matches.EnqueueTx
	changes := bulk.NewService(pool, scans, matches, log)
	workers := jobs.NewWorkers()
	river.AddWorker(workers, &enrich.MatchWorker{Service: matches})
	river.AddWorker(workers, &bulk.Worker{Service: changes})
	river.AddWorker(workers, &auth.SweepSessionsWorker{Service: accounts})
	river.AddWorker(workers, &ingest.ScanWorker{Service: scans})
	river.AddWorker(workers, &ingest.ScanAllWorker{Service: scans})
	river.AddWorker(workers, &ingest.ExtractWorker{Service: scans})
	river.AddWorker(workers, &ingest.ChunkWorker{Service: scans})
	river.AddWorker(workers, &ingest.WriteBackWorker{Service: scans})
	searchIndex := search.NewIndex(pool, log)
	river.AddWorker(workers, &search.RebuildIndexWorker{Index: searchIndex})
	periodic := []*river.PeriodicJob{
		jobs.Every(sessionSweepInterval, true, auth.SweepSessionsArgs{}, jobs.QueueDefault),
	}
	if cfg.ScanInterval > 0 {
		// Also once at start: what changed on disk while GOtome was down is
		// found now, and an unchanged library costs a walk.
		periodic = append(periodic, jobs.Every(cfg.ScanInterval, true, ingest.ScanAllArgs{}, jobs.QueueDefault))
	}
	runner, err := jobs.New(pool, jobs.Config{Logger: log, Workers: workers, Periodic: periodic})
	if err != nil {
		return err
	}
	scans.Queue = runner
	matches.Queue = runner
	changes.Queue = runner
	searchIndex.Queue = runner
	if err := runner.Start(ctx); err != nil {
		return err
	}
	// A new pg_search, or a new way of cutting text into chunks, is rebuilt
	// for in the background; search keeps answering from what is there.
	if err := searchIndex.CheckEngine(ctx); err != nil {
		log.Warn("could not check the search index", "error", err)
	}
	if err := scans.CheckChunkVersion(ctx); err != nil {
		log.Warn("could not check the chunks", "error", err)
	}
	// Books read before their text was kept, or whose chunks were dropped to
	// be rebuilt. Search works without them meanwhile, only knows less.
	if err := scans.EnqueueUnchunked(ctx); err != nil {
		log.Warn("could not queue chunking", "error", err)
	}
	// Stopped last, after the HTTP server has drained: a request in flight may
	// still enqueue a job.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobsShutdownWait)
		defer cancel()
		if err := runner.Stop(stopCtx); err != nil {
			log.Warn("background jobs did not stop cleanly", "error", err)
		}
	}()

	books := catalog.NewService(pool)
	hub := notify.NewHub(pool, log)
	go hub.Run(ctx)
	server := &httpapi.Server{
		Log:       log,
		DB:        pool,
		Auth:      accounts,
		Logins:    httpapi.NewLoginLimits(time.Now),
		Libraries: libraries,
		Settings:  settingStore,
		Scans:     scans,
		Books:     books,
		Covers:    coverStore,
		Metadata:  meta,
		Matches:   matches,
		Bulk:      changes,
		Reading:   reading.NewService(pool),
		Shelves:   shelves.NewService(pool, books),
		Search:    search.NewPGSearch(pool, books),
		Index:     searchIndex,

		Notifications: notify.NewService(pool),
		NotifyHub:     hub,
		Web:           webui.Handler(),
	}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Exactly one value is ever sent on this channel. Closing it instead would
	// make the select below read a nil and report a clean exit for a failure.
	serveErr := make(chan error, 1)
	go func() {
		build := version.Current()
		log.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", build.Version, "commit", build.Commit)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return errors.New("http server stopped serving without being asked to")
	case <-ctx.Done():
		log.Info("shutdown requested, draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}

// openDatabase connects and brings the schema up to date. The server migrates
// on every start, so an upgrade is a new image and nothing else.
func openDatabase(ctx context.Context, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return nil, err
	}
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := jobs.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	schema, err := db.SchemaVersion(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	postgres, err := sqlc.New(pool).ServerVersion(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	log.Info("database ready", "postgres", postgres, "schemaVersion", schema, "migrationsApplied", applied)
	return pool, nil
}

// migrate brings the schema up to date without serving, for a deployment that
// wants the step on its own.
func migrate() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := openDatabase(ctx, cfg, log)
	if err != nil {
		return err
	}
	pool.Close()
	return nil
}

func writeOpenAPI(args []string) error {
	encoded, err := httpapi.Spec().MarshalIndent()
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if len(args) == 0 {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	return os.WriteFile(args[0], encoded, 0o644)
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// healthcheck probes the local readiness endpoint. The container health check
// runs the binary itself, so the image needs no curl and the probe cannot drift
// from what the server answers.
func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: healthcheckWait}).Get(healthURL(cfg.HTTPAddr))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", resp.Status)
	}
	return nil
}

// healthURL turns the listen address into one a client can dial: a server
// bound to every interface is reached over loopback.
func healthURL(addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port + httpapi.ReadyPath
}
