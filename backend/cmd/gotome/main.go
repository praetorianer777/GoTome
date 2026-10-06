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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/metadata/hardcover"
	"github.com/praetorianer777/gotome/backend/internal/metadata/openlibrary"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/reading"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
	"github.com/praetorianer777/gotome/backend/internal/similar"
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
	trashPurgeInterval   = 6 * time.Hour
	embedInterval        = time.Hour
)

const usage = `Usage: gotome <command>

Commands:
  serve        Bring the database schema up to date, then run the server
  init         Write the database password for a new installation, once
  healthcheck  Probe a running server; exits non-zero unless it is ready
  embed-check  Load ONNX Runtime; with -reference, check the embedding model against it
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
	case "embed-check":
		return embedCheck(args[1:])
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
	providers := []metadata.Provider{
		openlibrary.New(),
		hardcover.New(func(ctx context.Context) string {
			token, _, err := settingStore.Secret(ctx, settings.HardcoverToken)
			if err != nil {
				log.Warn("the Hardcover token cannot be read", "error", err)
			}
			return token
		}),
	}
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
	books := catalog.NewService(pool)
	duplicates := dedupe.NewService(pool, books, log)
	events := &notify.Events{Pool: pool}
	scans.Events = events
	duplicates.Events = events
	matches.Events = events
	scans.OnExtracted = func(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
		if err := matches.EnqueueTx(ctx, tx, bookID); err != nil {
			return err
		}
		return duplicates.EnqueueTx(ctx, tx, bookID)
	}
	vectors := similar.NewService(pool, settingStore.Embedding, similar.OnnxOpener(embed.OnnxOptions{
		Runtime:  cfg.OnnxRuntime,
		ModelDir: cfg.ModelDir,
		Threads:  cfg.EmbedThreads,
		Fetch:    embed.FetchOptions{Offline: cfg.Offline, Log: log},
	}), log)
	scans.OnChunked = func(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID) error {
		if err := duplicates.ChunkedTx(ctx, tx, bookID, fileID); err != nil {
			return err
		}
		return vectors.EnqueueTx(ctx, tx)
	}
	settingStore.OnChange = func(ctx context.Context, keys []string) {
		if !slices.ContainsFunc(keys, func(k string) bool { return strings.HasPrefix(k, "embedding.") }) {
			return
		}
		// Switched on, or another model: a pass goes on with, or redoes,
		// the books. Switched off, the pass finds nothing to do.
		if err := vectors.Enqueue(ctx); err != nil {
			log.Warn("could not queue embedding", "error", err)
		}
	}
	scans.OnFilesChanged = duplicates.EnqueueTx
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
	river.AddWorker(workers, &dedupe.CheckWorker{Service: duplicates})
	river.AddWorker(workers, &dedupe.SignWorker{Service: duplicates})
	river.AddWorker(workers, &similar.EmbedWorker{Service: vectors})
	river.AddWorker(workers, &notify.FlushWorker{Events: events})
	river.AddWorker(workers, &ingest.PurgeTrashWorker{Service: scans, Retention: func(ctx context.Context) time.Duration {
		retention, err := settingStore.TrashRetention(ctx)
		if err != nil {
			log.Warn("trash retention unreadable; keeping files", "error", err)
			// Nothing is purged until the setting can be read.
			return 100 * 365 * 24 * time.Hour
		}
		return retention
	}})
	periodic := []*river.PeriodicJob{
		jobs.Every(sessionSweepInterval, true, auth.SweepSessionsArgs{}, jobs.QueueDefault),
		jobs.Every(trashPurgeInterval, true, ingest.PurgeTrashArgs{}, jobs.QueueDefault),
		similar.Periodic(embedInterval),
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
	duplicates.Queue = runner
	vectors.Queue = runner
	events.Queue = runner
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
	// Texts chunked before overlap was looked for, or signed another way.
	if err := duplicates.EnqueueUnsigned(ctx); err != nil {
		log.Warn("could not queue signing", "error", err)
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

	hub := notify.NewHub(pool, log)
	go hub.Run(ctx)
	server := &httpapi.Server{
		Log:        log,
		DB:         pool,
		Auth:       accounts,
		Logins:     httpapi.NewLoginLimits(time.Now),
		Libraries:  libraries,
		Settings:   settingStore,
		Scans:      scans,
		Books:      books,
		Covers:     coverStore,
		Metadata:   meta,
		Matches:    matches,
		Bulk:       changes,
		Reading:    reading.NewService(pool),
		Shelves:    shelves.NewService(pool, books),
		Search:     search.NewPGSearch(pool, books),
		Duplicates: duplicates,
		Similar:    vectors,
		Index:      searchIndex,

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
