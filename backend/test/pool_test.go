//go:build integration

package test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// What extraction does inside the transaction that holds the book's lock
// must not ask the pool for another connection: once every connection is
// taken by requests waiting for that lock, nothing moves until one gives
// up (#189). A pool of one connection, held by the transaction, proves it.
func TestQueueingALookupNeedsNoSecondConnection(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	_, adminID := a.signedIn("admin", "admin")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := a.pool.Config()
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	store, err := settings.Open(ctx, one, a.box)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, uuid.MustParse(adminID), map[string]*string{settings.AutoMatch: ptr("on")}); err != nil {
		t.Fatal(err)
	}
	m := enrich.NewService(one, nil, a.scans, a.covers, store, slog.New(slog.DiscardHandler))
	m.Queue = a.scans.Queue
	err = db.InTx(ctx, one, func(tx pgx.Tx) error {
		return m.EnqueueTx(ctx, tx, uuid.New())
	})
	if err != nil {
		t.Errorf("queueing a lookup in a transaction on a pool of one: %v", err)
	}
}
