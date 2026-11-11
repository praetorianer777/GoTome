package enrich

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// TextISBNInterval is how often the books that gained an ISBN from their
// text are looked for.
const TextISBNInterval = time.Hour

// textISBNBatch is how many books one transaction queues.
const textISBNBatch = 500

// TextISBNArgs is the job that queues the lookup of every book whose ISBN
// came from its text (the imprint page) and that was not looked up by it.
// Re-reading a book's text gives it the ISBN without a lookup; this sweep
// catches those, and all of them again once metadata.autoMatch is on.
type TextISBNArgs struct{}

func (TextISBNArgs) Kind() string { return "enrich.text_isbn_lookups" }

// TextISBNWorker runs TextISBNArgs.
type TextISBNWorker struct {
	river.WorkerDefaults[TextISBNArgs]
	Service *Service
}

func (w *TextISBNWorker) Work(ctx context.Context, _ *river.Job[TextISBNArgs]) error {
	_, err := w.Service.QueueTextISBNLookups(ctx)
	return err
}

// QueueTextISBNLookups queues a lookup for each book whose ISBN came from
// its text and was not queued by that ISBN before, and says how many. With
// metadata.autoMatch off it queues and records nothing, so that turning it
// on later still finds every such book.
func (s *Service) QueueTextISBNLookups(ctx context.Context) (int, error) {
	queued := 0
	for {
		n := 0
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			on, err := s.settings.TextTx(ctx, tx, settings.AutoMatch)
			if err != nil || on != "on" {
				return err
			}
			q := sqlc.New(tx)
			rows, err := q.ListTextISBNsToLookUp(ctx, textISBNBatch)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if err := s.EnqueueTx(ctx, tx, r.ID); err != nil {
					return err
				}
				if err := q.RecordTextISBNLookup(ctx, sqlc.RecordTextISBNLookupParams{BookID: r.ID, Isbn: r.Isbn}); err != nil {
					return err
				}
			}
			n = len(rows)
			return nil
		})
		queued += n
		if err != nil || n < textISBNBatch {
			return queued, err
		}
	}
}
