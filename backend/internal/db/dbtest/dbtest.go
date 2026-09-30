// Package dbtest gives each integration test a database of its own.
//
// Migrating a fresh database for every test would make the suite slow, so the
// migrations are applied once to a template, and each test gets a copy of it,
// which Postgres makes by copying files. The template is named after the
// content of the migrations: a changed migration makes a new template, and
// test packages running side by side share one.
package dbtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/migrations"
)

// EnvURL names the variable with the URL of a Postgres role that may create
// databases. make test-integration sets it to the checkout's own stack.
const EnvURL = "GOTOME_TEST_DATABASE_URL"

// templateLock serialises building the template across test processes.
const templateLock = 0x676f746f6d65

const setupTimeout = 2 * time.Minute

var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// New returns a pool on a fresh database with every migration applied. The
// database is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	admin := adminURL(t)
	templateOnce.Do(func() { templateName, templateErr = ensureTemplate(admin) })
	if templateErr != nil {
		t.Fatalf("dbtest: template database: %v", templateErr)
	}
	return create(t, admin, templateName)
}

// NewEmpty returns a pool on a fresh database with nothing in it, for tests of
// the migrations themselves.
func NewEmpty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	return create(t, adminURL(t), "template0")
}

func adminURL(t testing.TB) string {
	t.Helper()
	admin := os.Getenv(EnvURL)
	if admin == "" {
		t.Fatalf("dbtest: %s is not set. Run the suite with make test-integration, which starts from the checkout's stack.", EnvURL)
	}
	return admin
}

func create(t testing.TB, admin, template string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	var suffix [6]byte
	_, _ = rand.Read(suffix[:])
	name := "gotome_test_" + hex.EncodeToString(suffix[:])

	if err := exec(ctx, admin, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, template)); err != nil {
		t.Fatalf("dbtest: create database: %v", err)
	}
	pool, err := pgxpool.New(ctx, withDatabase(admin, name))
	if err != nil {
		t.Fatalf("dbtest: connect: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
		defer cancel()
		// FORCE: a test that leaked a connection still leaves no database behind.
		if err := exec(ctx, admin, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
			t.Errorf("dbtest: drop database: %v", err)
		}
	})
	return pool
}

// ensureTemplate builds the migrated template unless one for these migrations
// exists already.
func ensureTemplate(admin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	hash, err := migrationsHash()
	if err != nil {
		return "", err
	}
	name := "gotome_tpl_" + hash

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	// Held on this connection until it closes, so a second process waits here
	// and then finds the template the first one built.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(templateLock)); err != nil {
		return "", err
	}

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}

	// Built under another name and renamed when complete, so a run that dies
	// half way leaves nothing that looks like a finished template.
	building := name + "_building"
	if _, err := conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", building)); err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE template0", building)); err != nil {
		return "", err
	}
	pool, err := pgxpool.New(ctx, withDatabase(admin, building))
	if err != nil {
		return "", err
	}
	_, err = db.Migrate(ctx, pool)
	// A database with open connections can be neither renamed nor copied.
	pool.Close()
	if err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s RENAME TO %s", building, name)); err != nil {
		return "", err
	}
	return name, nil
}

func migrationsHash() (string, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func exec(ctx context.Context, admin, sql string) error {
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

// withDatabase is the admin URL pointed at another database.
func withDatabase(admin, name string) string {
	u, err := url.Parse(admin)
	if err != nil {
		return admin
	}
	u.Path = "/" + name
	return u.String()
}
