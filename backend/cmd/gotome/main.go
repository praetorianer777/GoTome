// Command gotome is the GOtome server and its maintenance subcommands.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/deliveries"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/kobo"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/memlimit"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/metadata/crossref"
	"github.com/praetorianer777/gotome/backend/internal/metadata/hardcover"
	"github.com/praetorianer777/gotome/backend/internal/metadata/openlibrary"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/pairing"
	"github.com/praetorianer777/gotome/backend/internal/passages"
	"github.com/praetorianer777/gotome/backend/internal/reading"
	"github.com/praetorianer777/gotome/backend/internal/releases"
	"github.com/praetorianer777/gotome/backend/internal/resources"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
	"github.com/praetorianer777/gotome/backend/internal/similar"
	"github.com/praetorianer777/gotome/backend/internal/sso"
	"github.com/praetorianer777/gotome/backend/internal/stats"
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
	genreInterval        = time.Hour
	// A subject is asked about once a day; the hourly run picks up those due.
	releasePollInterval = time.Hour
	// feedFetchInterval is how often feeds are looked at for those due.
	feedFetchInterval = 10 * time.Minute
	// New and changed ebook files get the MD5 KOReader names them by.
	koreaderHashInterval = 10 * time.Minute
	// EPUBs read before measuring was, or changed since, are measured.
	measureInterval = 10 * time.Minute
	// Deletes reach the full-text index's outbox through a trigger, which
	// queues no job; they are applied this often.
	outboxInterval = time.Minute
)

const usage = `Usage: gotome <command>

Commands:
  serve          Bring the database schema up to date, then run the server
  init           Write the database password and secret key for a new installation, once
  healthcheck    Probe a running server; exits non-zero unless it is ready
  embed-check    Load ONNX Runtime; with -reference, check the embedding model against it
  fulltext-check Load the full-text engine and print its version
  audio-check    Compare what FFmpeg reads from folders of audio files with their reference
  migrate        Bring the database schema up to date and exit
  reset-password Set a new password for an account, read from the terminal (-h for options)
  similar-check  Measure the similar books of a sample of the library (-h for options)
  genre-check    Measure how well genres are found again for tagged books (-h for options)
  openapi        Write the API's OpenAPI document to the given file, or to stdout
  version        Print the version
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
	case "fulltext-check":
		return fulltextCheck()
	case "audio-check":
		return audioCheck(args[1:])
	case "migrate":
		return migrate()
	case "reset-password":
		return resetPassword(args[1:])
	case "similar-check":
		return similarCheck(args[1:])
	case "genre-check":
		return genreCheck(args[1:])
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
	if soft := memlimit.Apply(); soft > 0 {
		log.Info("holding the heap under the container's memory limit", "softLimitMiB", soft>>20)
	}
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
	if !cfg.ForcePasswords {
		accounts.PasswordsOff = settingStore.PasswordsOff
	}
	// Background work runs in this process, on the same database.
	libraries := library.NewService(pool, cfg.DataDir)
	coverStore := covers.NewStore(cfg.DataDir)
	scans := ingest.NewService(pool, libraries, coverStore, log)
	scans.UploadLimit = cfg.UploadLimit
	providers := []metadata.Provider{
		openlibrary.New(),
		crossref.New(func(ctx context.Context) string {
			contact, _ := settingStore.Text(ctx, settings.ContactEmail)
			return contact
		}),
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
	onnx := embed.OnnxOptions{
		Runtime:  cfg.OnnxRuntime,
		ModelDir: cfg.ModelDir,
		Threads:  cfg.EmbedThreads,
		Fetch:    embed.FetchOptions{Offline: cfg.Offline, Log: log},
	}
	vectors := similar.NewService(pool, settingStore.Embedding, similar.ServerOpener(similar.OnnxOpener(onnx), settingStore.EmbeddingServer), log)
	vectors.Probe = similar.ServerProbe(similar.OnnxProbe(onnx), settingStore.EmbeddingServer)
	vectors.Passages = scans.Passages
	passageStore, err := passages.OpenStore(passages.Dir(cfg.DataDir))
	if err != nil {
		return err
	}
	defer passageStore.Close()
	if passageStore.StartedOver() {
		log.Warn("the passage store's files were not of one set, as after a crash while it was compacted: its passages are embedded again")
	}
	passageVectors := passages.NewService(pool, passageStore, settingStore.Embedding, settingStore.EmbeddingPassages,
		similar.ServerOpener(similar.OnnxOpener(onnx), settingStore.EmbeddingServer), log)
	passageVectors.Chunks = scans.FileChunks
	vectors.NearPassages = passageVectors.Near
	vectors.PassageStatus = passageVectors.Status
	duplicates.Texts = scans.FileTexts
	scans.OnChunked = func(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID, texts []string) error {
		if err := duplicates.ChunkedTx(ctx, tx, bookID, fileID, texts); err != nil {
			return err
		}
		if err := passageVectors.EnqueueTx(ctx, tx); err != nil {
			return err
		}
		return vectors.EnqueueTx(ctx, tx)
	}
	genreService := genres.NewService(pool, vectors, func(ctx context.Context) (string, error) {
		return settingStore.Text(ctx, settings.GenreSuggestions)
	}, log)
	settingStore.OnChange = func(ctx context.Context, keys []string) {
		if slices.Contains(keys, settings.GenreSuggestions) {
			if err := genreService.Enqueue(ctx); err != nil {
				log.Warn("could not queue genres", "error", err)
			}
		}
		if !slices.ContainsFunc(keys, func(k string) bool { return strings.HasPrefix(k, "embedding.") }) {
			return
		}
		// Switched on, or another model: a pass goes on with, or redoes,
		// the books. Switched off, the pass finds nothing to do.
		if err := vectors.Enqueue(ctx); err != nil {
			log.Warn("could not queue embedding", "error", err)
		}
		if err := passageVectors.Enqueue(ctx); err != nil {
			log.Warn("could not queue embedding passages", "error", err)
		}
	}
	scans.OnFilesChanged = duplicates.EnqueueTx
	scans.OnDescribed = duplicates.EnqueueTx
	changes := bulk.NewService(pool, scans, matches, log)
	follows := releases.NewService(pool, meta, log)
	fixes := cleanup.NewService(pool, changes)
	follows.Events = events
	follows.Offline = cfg.Offline
	readingService := reading.NewService(pool)
	progressSync := kosync.NewService(pool, readingService, log)
	engineVersion, err := fulltext.Load(cfg.Fulltext)
	if err != nil {
		return err
	}
	searchIndex := search.NewIndex(pool, log, engineVersion)
	textIndex, freshIndex, err := fulltext.OpenService(ctx, pool, fulltext.Dir(cfg.DataDir), engineVersion, log)
	if err != nil {
		return err
	}
	defer textIndex.Close()
	workers := background{
		accounts: accounts, matches: matches, changes: changes, scans: scans,
		searchIndex: searchIndex, textIndex: textIndex, duplicates: duplicates,
		vectors: vectors, genres: genreService, passages: passageVectors,
		events: events, follows: follows, sync: progressSync,
		trashRetention: settingStore.TrashRetention,
	}.workers()
	runner, err := jobs.New(pool, jobs.Config{Logger: log, Workers: workers, Periodic: schedules(cfg.ScanInterval)})
	if err != nil {
		return err
	}
	scans.Queue = runner
	matches.Queue = runner
	changes.Queue = runner
	searchIndex.Queue = runner
	duplicates.Queue = runner
	vectors.Queue = runner
	genreService.Queue = runner
	passageVectors.Queue = runner
	events.Queue = runner
	follows.Queue = runner
	if err := runner.Start(ctx); err != nil {
		return err
	}
	// A new way of cutting text into chunks is rebuilt for in the
	// background; search keeps answering from what is there.
	if err := scans.CheckChunkVersion(ctx); err != nil {
		log.Warn("could not check the chunks", "error", err)
	}
	// Chunks the full-text index lacks, all of them when it is new: their
	// files are read again, as it holds no text to be built from.
	if freshIndex {
		log.Info("the full-text index is new; reading the text of every book into it", "engine", engineVersion)
	}
	if err := scans.EnqueueIndexing(ctx); err != nil {
		log.Warn("could not queue indexing", "error", err)
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
	health := resources.NewService(pool)
	health.DataDir, health.IndexDir = cfg.DataDir, fulltext.Dir(cfg.DataDir)
	health.Embedding, health.Server = settingStore.Embedding, settingStore.EmbeddingServer
	go health.Watch(ctx, log)
	server := &httpapi.Server{
		Log:        log,
		DB:         pool,
		Auth:       accounts,
		SSO:        sso.NewService(pool, accounts, settingStore),
		Logins:     httpapi.NewLoginLimits(time.Now),
		Libraries:  libraries,
		Settings:   settingStore,
		Scans:      scans,
		Books:      books,
		Covers:     coverStore,
		Metadata:   meta,
		Matches:    matches,
		Bulk:       changes,
		Reading:    readingService,
		Shelves:    shelves.NewService(pool, books),
		Search:     search.NewTantivySearch(pool, books, textIndex.Index()),
		Duplicates: duplicates,
		Similar:    vectors,
		Genres:     genreService,
		Stats:      stats.NewService(pool, vectors),
		Resources:  health,
		Deliveries: deliveries.NewService(pool),
		Pairing:    pairing.NewService(pool),
		Kobo:       kobo.NewService(pool),
		Sync:       progressSync,
		Releases:   follows,
		Cleanup:    fixes,
		Index:      searchIndex,

		Notifications: notify.NewService(pool),
		NotifyHub:     hub,
		Web:           webui.Handler(),

		TrustedProxies: cfg.TrustedProxies,
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

// similarCheck measures the similar books of a sample of the library, as
// they are shown now: how many are by the book's own author, from its
// series, or a copy of it. It reads only.
func similarCheck(args []string) error {
	flags := flag.NewFlagSet("similar-check", flag.ContinueOnError)
	n := flags.Int("n", 300, "how many books to ask about")
	k := flags.Int("k", 10, "how many similar books to take of each")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	verbose := flags.Bool("v", false, "list each book with its similar books")
	describe := flags.String("describe", "", "list the books nearest this description instead, k of them")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
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
	store, err := settings.Open(ctx, pool, box)
	if err != nil {
		return err
	}
	vectors := checkVectors(cfg, pool, store, log)
	if *describe != "" {
		titles, err := vectors.DescribeTitles(ctx, *describe, *k)
		if err != nil {
			return err
		}
		for _, title := range titles {
			fmt.Println(title)
		}
		return nil
	}
	r, err := vectors.Check(ctx, *n, *k)
	if err != nil {
		return err
	}
	if !*verbose {
		r.Samples = nil
	}
	if *asJSON {
		out := json.NewEncoder(os.Stdout)
		out.SetIndent("", "  ")
		return out.Encode(r)
	}
	for _, s := range r.Samples {
		fmt.Printf("%s\n", s.Title)
		for _, t := range s.Neighbours {
			fmt.Printf("    %s\n", t)
		}
	}
	fmt.Printf("Books asked about           %d (%d in a series, %d without similar books)\n", r.Books, r.InSeries, r.Empty)
	fmt.Printf("Similar books               %d, at most %d each\n", r.Neighbours, *k)
	fmt.Printf("By the book's own author    %.1f%%\n", 100*r.SameAuthor)
	fmt.Printf("From the book's series      %.1f%% (of those of books in a series)\n", 100*r.SameSeries)
	fmt.Printf("Copies of the book          %.1f%%\n", 100*r.Copies)
	fmt.Printf("Authors among them          %.1f on average\n", r.Authors)
	return nil
}

// genreCheck measures how well each way of suggesting genres finds again
// the genres the tags of a sample of tagged books give. It reads only.
func genreCheck(args []string) error {
	flags := flag.NewFlagSet("genre-check", flag.ContinueOnError)
	n := flags.Int("n", 300, "how many tagged books to ask about")
	precision := flags.Float64("precision", 0.8, "the precision a genre's threshold must reach")
	neighbours := flags.Bool("neighbours", true, "measure the neighbours too, a look at a library's vectors per book")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
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
	store, err := settings.Open(ctx, pool, box)
	if err != nil {
		return err
	}
	vectors := checkVectors(cfg, pool, store, log)
	r, err := genres.NewService(pool, vectors, nil, log).Check(ctx, genres.CheckOptions{
		Books: *n, Precision: *precision, Neighbours: *neighbours,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		out := json.NewEncoder(os.Stdout)
		out.SetIndent("", "  ")
		return out.Encode(r)
	}
	genres.PrintCheck(os.Stdout, r, *precision)
	return nil
}

// localOpener runs the models the checks embed with. Tests give them one
// that needs no ONNX Runtime.
var localOpener = similar.OnnxOpener

// checkVectors is the similar books' service as the checks use it.
func checkVectors(cfg config.Config, pool *pgxpool.Pool, store *settings.Store, log *slog.Logger) *similar.Service {
	return similar.NewService(pool, store.Embedding, similar.ServerOpener(localOpener(embed.OnnxOptions{
		Runtime: cfg.OnnxRuntime, ModelDir: cfg.ModelDir, Threads: cfg.EmbedThreads,
		Fetch: embed.FetchOptions{Offline: cfg.Offline, Log: log},
	}), store.EmbeddingServer), log)
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
