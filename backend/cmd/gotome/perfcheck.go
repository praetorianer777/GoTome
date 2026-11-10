package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/autodupe"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/config"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/perf"
	"github.com/praetorianer777/gotome/backend/internal/readimport"
	"github.com/praetorianer777/gotome/backend/internal/reading"
	"github.com/praetorianer777/gotome/backend/internal/releases"
	"github.com/praetorianer777/gotome/backend/internal/resources"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
	"github.com/praetorianer777/gotome/backend/internal/stats"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

// perfCheck times the main views on the installation's own library, as the
// API answers them to one person, and prints a table that names nothing of
// the library. The database refuses every write on its connections, and no
// job is queued: it only reads. Errors say which view, never what was asked.
func perfCheck(args []string) error {
	flags := flag.NewFlagSet("perf-check", flag.ContinueOnError)
	user := flags.String("user", "", "the account the views are asked as (default: the first administrator)")
	skip := flags.String("skip", "", "views not to run, separated by commas")
	pause := flags.Duration("pause", 500*time.Millisecond, "how long to wait between views")
	timeout := flags.Duration("timeout", 10*time.Second, "the longest a request may take")
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
	if err := cfg.RequireDatabase(); err != nil {
		return err
	}
	readOnly, err := readOnlyURL(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, readOnly, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := settings.OpenReadOnly(ctx, pool, box)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the server has not run on this database yet; start it once first")
	}
	if err != nil {
		return err
	}
	viewer, err := perfUser(ctx, pool, *user)
	if err != nil {
		return err
	}

	quiet := slog.New(slog.DiscardHandler)
	accounts, err := auth.NewService(pool, auth.DefaultPasswordParams(), auth.DefaultSessionTTL)
	if err != nil {
		return err
	}
	libraries := library.NewService(pool, cfg.DataDir)
	coverStore := covers.NewStore(cfg.DataDir)
	scans := ingest.NewService(pool, libraries, coverStore, quiet)
	books := catalog.NewService(pool)
	meta := metadata.NewService(pool, nil, metadata.Options{})
	matches := enrich.NewService(pool, meta, scans, coverStore, store, quiet)
	changes := bulk.NewService(pool, scans, matches, quiet)
	vectors := checkVectors(cfg, pool, store, quiet)
	engineVersion, err := fulltext.Load(cfg.Fulltext)
	if err != nil {
		return err
	}
	indexDir := fulltext.Dir(cfg.DataDir)
	if _, err := os.Stat(filepath.Join(indexDir, "meta.json")); err != nil {
		return errors.New("there is no full-text index to search; start the server once and let it build one")
	}
	index, err := fulltext.Open(indexDir, 0)
	if err != nil {
		return err
	}
	defer index.Close()
	health := resources.NewService(pool)
	health.Embedding, health.Server = store.Embedding, store.EmbeddingServer
	autoDuplicates := autodupe.NewService(pool, books, quiet)
	autoDuplicates.Settings = store
	server := &httpapi.Server{
		Log:            quiet,
		DB:             pool,
		Auth:           accounts,
		Settings:       store,
		Libraries:      libraries,
		Scans:          scans,
		Books:          books,
		Covers:         coverStore,
		Metadata:       meta,
		Matches:        matches,
		Bulk:           changes,
		Reading:        reading.NewService(pool),
		Imports:        readimport.NewService(pool, books, quiet),
		Shelves:        shelves.NewService(pool, books),
		Search:         search.NewTantivySearch(pool, books, index),
		Index:          search.NewIndex(pool, quiet, engineVersion),
		Duplicates:     dedupe.NewService(pool, books, quiet),
		AutoDuplicates: autoDuplicates,
		Similar:        vectors,
		Genres: genres.NewService(pool, vectors, func(ctx context.Context) (string, error) {
			return store.Text(ctx, settings.GenreSuggestions)
		}, quiet),
		Stats:     stats.NewService(pool, vectors),
		Resources: health,
		Releases:  releases.NewService(pool, meta, quiet),
		Cleanup:   cleanup.NewService(pool, changes),
	}

	spec, embedding, err := store.Embedding(ctx)
	if err != nil {
		return err
	}
	picked, err := perf.Pick(ctx, pool, library.ScopeOf(viewer), spec.Model.Name, scans.FileChunks)
	if err != nil {
		return err
	}
	figures, err := perfContext(ctx, pool, health, viewer, spec.Model.Name)
	if err != nil {
		return err
	}
	figures.EmbeddingOn, figures.EmbeddingModel = embedding, spec.Model.Name
	figures.EmbeddingHere = spec.Runs()
	if address, _ := store.EmbeddingServer(ctx); address != "" && spec.Server != "" {
		figures.EmbeddingHere = false
	}
	figures.TextFiles = picked.TextFiles
	figures.Before = health.Usage(ctx)

	views := perf.Views(picked.Params)
	started := 0
	results, measureErr := perf.Measure(ctx, server.Routes(), viewer, views, perf.Options{
		Skip:    splitList(*skip),
		Timeout: *timeout,
		Pause:   *pause,
		Started: func(name string) {
			started++
			fmt.Fprintf(os.Stderr, "%d of %d: %s\n", started, len(views), name)
		},
	})
	figures.After = health.Usage(ctx)
	perf.Print(os.Stdout, figures, results)
	return measureErr
}

// readOnlyURL is the database URL with every transaction read-only: a
// write the command should never make is refused by the database.
func readOnlyURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("GOTOME_DATABASE_URL is not a valid Postgres URL")
	}
	q := u.Query()
	q.Set("default_transaction_read_only", "on")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// perfUser is the account named, or the first administrator still enabled.
func perfUser(ctx context.Context, pool *pgxpool.Pool, name string) (auth.User, error) {
	q := sqlc.New(pool)
	users, err := q.ListUsers(ctx)
	if err != nil {
		return auth.User{}, err
	}
	var found *sqlc.User
	for i, u := range users {
		switch {
		case u.DisabledAt != nil:
		case name != "" && strings.EqualFold(u.Username, name):
			found = &users[i]
		case name == "" && u.Role == auth.RoleAdmin && (found == nil || u.CreatedAt.Before(found.CreatedAt)):
			found = &users[i]
		}
	}
	if found == nil {
		if name != "" {
			return auth.User{}, errors.New("no enabled account with that name")
		}
		return auth.User{}, errors.New("no enabled administrator; name an account with -user")
	}
	return auth.User{ID: found.ID, Username: found.Username, Email: found.Email, Role: found.Role}, nil
}

func perfContext(ctx context.Context, pool *pgxpool.Pool, health *resources.Service, viewer auth.User, model string) (perf.Context, error) {
	q := sqlc.New(pool)
	scope := library.ScopeOf(viewer)
	figures, err := q.PerfFigures(ctx, sqlc.PerfFiguresParams{Model: model, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return perf.Context{}, err
	}
	postgres, err := q.ServerVersion(ctx)
	if err != nil {
		return perf.Context{}, err
	}
	machine, err := health.Check(ctx)
	if err != nil {
		return perf.Context{}, err
	}
	return perf.Context{
		Version: version.Current().Version,
		Books:   figures.Books, Files: figures.Files, Authors: figures.Authors, Series: figures.Series,
		Chunks: figures.Chunks, Vectors: figures.Vectors,
		CPUs: machine.Memory.CPUs, MachineBytes: machine.Memory.MachineBytes, Limited: machine.Memory.Limited,
		Postgres: postgres, Settings: machine.Postgres,
	}, nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
