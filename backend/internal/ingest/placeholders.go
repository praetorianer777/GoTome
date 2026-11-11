package ingest

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

var (
	// ErrPlaceholderHolders is a placeholder that someone reads who cannot
	// see the book it would join: it stays, as the join would show that
	// person a book they may not see.
	ErrPlaceholderHolders = errors.New("someone who reads the book cannot see that one")
	// ErrNotPlaceholder is a book that is not one read but not owned, or a
	// target that is.
	ErrNotPlaceholder = errors.New("the books are not a placeholder and a library book")
)

const joinPlaceholderRule = "join-placeholder"

// JoinPlaceholders joins the placeholders that the book is, by the rule of
// an import (an ISBN, else the title and an author), to it, as
// joinPlaceholder does for each; it reports how many joined.
func (s *Service) JoinPlaceholders(ctx context.Context, bookID uuid.UUID) (int, error) {
	ids, err := sqlc.New(s.pool).ListPlaceholdersOfBook(ctx, bookID)
	if err != nil {
		return 0, err
	}
	joined := 0
	for _, id := range ids {
		ok, _, err := s.joinPlaceholder(ctx, bookID, id, uuid.Nil)
		if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, ErrMergeSelf) || errors.Is(err, ErrNotPlaceholder) {
			continue
		}
		if err != nil {
			return joined, err
		}
		if ok {
			joined++
		}
	}
	return joined, nil
}

// JoinPlaceholder is a person's saying that the placeholder they read is
// the library's book. Everyone who reads the placeholder is moved to the
// book, so each of them must see it. It returns the curation action that
// undoes it.
func (s *Service) JoinPlaceholder(ctx context.Context, scope library.Scope, placeholder, book uuid.UUID) (uuid.UUID, error) {
	books := catalog.NewService(s.pool)
	for _, id := range []uuid.UUID{placeholder, book} {
		if _, err := books.Get(ctx, scope, id); err != nil {
			return uuid.Nil, err
		}
	}
	holds, err := sqlc.New(s.pool).IsBookHolder(ctx, sqlc.IsBookHolderParams{BookID: placeholder, UserID: scope.Viewer})
	if err != nil {
		return uuid.Nil, err
	}
	if holds == nil || !*holds {
		return uuid.Nil, catalog.ErrNotFound
	}
	ok, action, err := s.joinPlaceholder(ctx, book, placeholder, scope.Viewer)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, ErrPlaceholderHolders
	}
	return action, nil
}

// joinPlaceholder merges the placeholder into the book, as one action that
// can be undone, if every one who reads the placeholder sees the book. Of
// a rule's own join (by is uuid.Nil) it also asks that for each of them
// the book is the only one that fits the placeholder, by the rule of an
// import: two fitting books are for a person to choose between.
func (s *Service) joinPlaceholder(ctx context.Context, into, from, by uuid.UUID) (bool, uuid.UUID, error) {
	into, from, fields, err := s.prepareMerge(ctx, everyone, into, from, nil)
	if err != nil {
		return false, uuid.Nil, err
	}
	var joined bool
	var action uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{into, from}); err != nil {
			return err
		}
		locked, err := q.LockBooksForMerge(ctx, sqlc.LockBooksForMergeParams{Ids: []uuid.UUID{into, from}, SeesAll: true})
		if err != nil {
			return err
		}
		if len(locked) != 2 {
			return catalog.ErrNotFound
		}
		for _, b := range locked {
			if b.Placeholder != (b.ID == from) {
				return ErrNotPlaceholder
			}
		}
		facts, err := q.PlaceholderFacts(ctx, from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotPlaceholder
		}
		if err != nil {
			return err
		}
		holders, err := q.ListPlaceholderHolders(ctx, from)
		if err != nil || len(holders) == 0 {
			return err
		}
		for _, h := range holders {
			scope := library.Scope{Viewer: h.ID, SeesAll: auth.Allows(h.Role, auth.StorageManage)}
			if by == uuid.Nil {
				pick, err := catalog.MatchBooks(ctx, q, scope, facts.Isbns, facts.TitleKey, facts.PersonKeys, false)
				if err != nil {
					return err
				}
				if len(pick) != 1 || pick[0] != into {
					return nil
				}
				continue
			}
			seen, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: []uuid.UUID{into}, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
			if err != nil {
				return err
			}
			if len(seen) == 0 {
				return nil
			}
		}
		pending, err := s.Curation.Begin(ctx, tx, curation.SetAll, into, from)
		if err != nil {
			return err
		}
		if err := s.mergeTx(ctx, tx, everyone, into, from, fields, nil); err != nil {
			return err
		}
		err = pending.Done(ctx, tx, curation.Record{
			RunKind: curation.KindPlaceholders, By: by, Kind: curation.ActionMerge, Rule: joinPlaceholderRule,
			Reason: "A book read but not owned is a book of the library.",
		})
		if err != nil {
			return err
		}
		joined = true
		if by != uuid.Nil {
			action, err = q.LatestPlaceholderJoinAction(ctx, sqlc.LatestPlaceholderJoinActionParams{UserID: by, FromBook: from})
		}
		return err
	})
	return joined, action, err
}

// UndoPlaceholderJoin takes back a join the person made of a placeholder
// themselves.
func (s *Service) UndoPlaceholderJoin(ctx context.Context, scope library.Scope, action uuid.UUID) (curation.Outcome, error) {
	if _, err := sqlc.New(s.pool).GetPlaceholderJoinAction(ctx, sqlc.GetPlaceholderJoinActionParams{ID: action, UserID: scope.Viewer}); errors.Is(err, pgx.ErrNoRows) {
		return curation.Outcome{}, curation.ErrNotFound
	} else if err != nil {
		return curation.Outcome{}, err
	}
	return s.Curation.UndoAction(ctx, scope, action)
}

// queuePlaceholderJoinTx asks for the placeholders the book may be to be
// joined to it, after the transaction that changed how it is described,
// if there are any.
func (s *Service) queuePlaceholderJoinTx(ctx context.Context, tx pgx.Tx, bookID uuid.UUID) error {
	if s.Queue == nil {
		return nil
	}
	ids, err := sqlc.New(tx).ListPlaceholdersOfBook(ctx, bookID)
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = s.Queue.InsertTx(ctx, tx, JoinPlaceholdersArgs{BookID: bookID}, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: 3,
	})
	return err
}

// JoinPlaceholdersArgs is the job that joins the placeholders a book may be.
type JoinPlaceholdersArgs struct {
	BookID uuid.UUID `json:"bookId"`
}

// Kind names the job in the queue. It is stored with every job, so it stays.
func (JoinPlaceholdersArgs) Kind() string { return "ingest.join_placeholders" }

// JoinPlaceholdersWorker runs JoinPlaceholdersArgs.
type JoinPlaceholdersWorker struct {
	river.WorkerDefaults[JoinPlaceholdersArgs]
	Service *Service
}

func (w *JoinPlaceholdersWorker) Work(ctx context.Context, job *river.Job[JoinPlaceholdersArgs]) error {
	_, err := w.Service.JoinPlaceholders(ctx, job.Args.BookID)
	if errors.Is(err, catalog.ErrNotFound) {
		return nil
	}
	return err
}
