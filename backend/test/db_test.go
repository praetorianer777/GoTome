//go:build integration

// Package test is the integration suite: it runs against the Postgres of the
// checkout's stack, through make test-integration.
package test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

func TestMigrateFreshThenNoOp(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewEmpty(t)

	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if applied == 0 {
		t.Fatal("a fresh database had nothing to apply")
	}
	version, err := db.SchemaVersion(ctx, pool)
	if err != nil || version < 1 {
		t.Fatalf("schema version = %d, %v", version, err)
	}

	again, err := db.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if again != 0 {
		t.Errorf("the second run applied %d migrations, want none", again)
	}

	var hasCitext bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext')").Scan(&hasCitext); err != nil || !hasCitext {
		t.Errorf("citext installed = %v, %v", hasCitext, err)
	}
}

// Two instances starting together must each end up with the schema, with
// every migration applied once.
func TestMigrateConcurrently(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewEmpty(t)

	const starters = 4
	applied := make([]int, starters)
	errs := make([]error, starters)
	var wg sync.WaitGroup
	for i := range starters {
		wg.Go(func() { applied[i], errs[i] = db.Migrate(ctx, pool) })
	}
	wg.Wait()

	total := 0
	for i := range starters {
		if errs[i] != nil {
			t.Errorf("starter %d: %v", i, errs[i])
		}
		total += applied[i]
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if total != rows {
		t.Errorf("the starters applied %d migrations between them, the database records %d", total, rows)
	}
}

func TestGeneratedQueryRuns(t *testing.T) {
	pool := dbtest.New(t)
	version, err := sqlc.New(pool).ServerVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "18") {
		t.Errorf("server version = %q, want PostgreSQL 18", version)
	}
}

func TestInTx(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	if _, err := pool.Exec(ctx, "CREATE TABLE notes (body text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	insert := func(body string) func(pgx.Tx) error {
		return func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO notes (body) VALUES ($1)", body)
			return err
		}
	}
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM notes").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := db.InTx(ctx, pool, insert("kept")); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if count() != 1 {
		t.Fatal("a committed row is missing")
	}

	boom := errors.New("boom")
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := insert("undone")(tx); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count() != 1 {
		t.Errorf("after a failed transaction: err = %v, rows = %d", err, count())
	}

	func() {
		defer func() { _ = recover() }()
		_ = db.InTx(ctx, pool, func(tx pgx.Tx) error {
			_ = insert("undone by panic")(tx)
			panic("boom")
		})
	}()
	if count() != 1 {
		t.Errorf("a panic left %d rows, want 1", count())
	}
}

// Each test gets its own database: what one writes, the other never sees.
func TestDatabasesAreIsolated(t *testing.T) {
	ctx := context.Background()
	a, b := dbtest.New(t), dbtest.New(t)
	if _, err := a.Exec(ctx, "CREATE TABLE only_in_a (id int)"); err != nil {
		t.Fatal(err)
	}
	if exists(t, b, "only_in_a") {
		t.Error("a table made in one test database shows in another")
	}
	if !exists(t, a, "only_in_a") {
		t.Error("the table is missing where it was made")
	}
}

func exists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}
