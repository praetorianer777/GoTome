package ingest

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// everyone is the scope of work GOtome does by itself: the books of the
// library whose option asked for it.
var everyone = library.Scope{SeesAll: true}

// JoinFormats makes one book of the book and the other of its library that
// is its ebook or its audiobook, if the library asks for it (joinFormats)
// and there is exactly one: a pair with the same title and author, of which
// one book has only ebook files and the other only audio files. It is a
// merge like a person's, with the ebook's book surviving and the narrators
// added to its credits, and it records what moved so that Unjoin can take
// it back. It reports whether it joined.
func (s *Service) JoinFormats(ctx context.Context, bookID uuid.UUID) (bool, error) {
	var joined bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{bookID}); err != nil {
			return err
		}
		candidates, err := q.ListJoinCandidates(ctx, bookID)
		if err != nil || len(candidates) != 1 {
			return err
		}
		other := candidates[0].Other
		// The other book must have no second candidate either, or the
		// match is a guess: two recordings of one ebook are for a person.
		back, err := q.ListJoinCandidates(ctx, other)
		if err != nil || len(back) != 1 {
			return err
		}
		into, from := other, bookID
		if candidates[0].MeIsEbook {
			into, from = bookID, other
		}
		_, _, fields, err := s.prepareMerge(ctx, everyone, into, from, nil)
		if err != nil {
			return err
		}
		files, err := q.ListBookFileIDs(ctx, from)
		if err != nil {
			return err
		}
		if _, err := q.RecordJoin(ctx, sqlc.RecordJoinParams{IntoBook: into, FromBook: from, FileIds: files}); err != nil {
			return err
		}
		if err := s.mergeTx(ctx, tx, everyone, into, from, fields, nil); err != nil {
			return err
		}
		if err := q.AddNarrators(ctx, sqlc.AddNarratorsParams{IntoBook: into, FromBook: from}); err != nil {
			return err
		}
		joined = true
		return nil
	})
	return joined, err
}

// ErrNoJoin is a join that does not exist, was taken back, or is in a
// library the viewer does not see.
var ErrNoJoin = errors.New("no such join")

// Unjoin splits what JoinFormats made one: the book merged away is a book
// again, with the files it brought, and the pair is kept as two books. What
// people did with the one book since, its status, rating, progress and
// places on shelves, stays with the surviving book, which is where they
// moved to.
func (s *Service) Unjoin(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		join, err := q.GetJoinForUpdate(ctx, sqlc.GetJoinForUpdateParams{ID: id, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoJoin
		}
		if err != nil {
			return err
		}
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{join.IntoBook, join.FromBook}); err != nil {
			return err
		}
		steps := []func() error{
			func() error { return q.RestoreMergedBook(ctx, join.FromBook) },
			func() error {
				return q.MoveFileIdentifiersBack(ctx, sqlc.MoveFileIdentifiersBackParams{
					ToBook: join.FromBook, FromBook: join.IntoBook, FileIds: join.FileIds})
			},
			func() error {
				return q.MoveFilesBack(ctx, sqlc.MoveFilesBackParams{
					ToBook: join.FromBook, FromBook: join.IntoBook, FileIds: join.FileIds})
			},
			func() error {
				return q.KeepPairBoth(ctx, sqlc.KeepPairBothParams{One: join.IntoBook, Other: join.FromBook})
			},
			func() error { return q.MarkJoinUndone(ctx, join.ID) },
		}
		for _, step := range steps {
			if err := step(); err != nil {
				return err
			}
		}
		for _, book := range []uuid.UUID{join.IntoBook, join.FromBook} {
			primary, changed, err := choosePrimaryTx(ctx, q, book)
			if err != nil {
				return err
			}
			if changed && primary != uuid.Nil {
				if err := q.DeleteBookChunks(ctx, sqlc.DeleteBookChunksParams{BookID: book, FileID: primary}); err != nil {
					return err
				}
				if _, err := s.Queue.InsertTx(ctx, tx, ChunkArgs{FileID: primary}, jobs.InsertOpts{
					Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
				}); err != nil {
					return err
				}
			}
			if err := s.filesChangedTx(ctx, tx, book); err != nil {
				return err
			}
		}
		return nil
	})
}

// QueueJoinTx asks for the book to be joined to its ebook or audiobook,
// once the transaction that found the pair has committed.
func (s *Service) QueueJoinTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
	_, err := s.Queue.InsertTx(ctx, tx, JoinArgs{BookID: bookID}, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: 3,
	})
	return err
}

// JoinArgs is the job that joins a book to its other form.
type JoinArgs struct {
	BookID uuid.UUID `json:"bookId"`
}

// joinKind names the job in the queue. It is stored with every job, so it
// stays.
const joinKind = "ingest.join_formats"

func (JoinArgs) Kind() string { return joinKind }

// JoinWorker runs JoinArgs.
type JoinWorker struct {
	river.WorkerDefaults[JoinArgs]
	Service *Service
}

func (w *JoinWorker) Work(ctx context.Context, job *river.Job[JoinArgs]) error {
	_, err := w.Service.JoinFormats(ctx, job.Args.BookID)
	if errors.Is(err, catalog.ErrNotFound) {
		return nil
	}
	return err
}
