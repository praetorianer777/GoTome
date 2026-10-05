package ingest

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MergeFields are the fields a merge may take from the merged book. Its
// identifiers are always kept, beside the surviving book's.
var MergeFields = []string{
	catalog.FieldTitle, catalog.FieldSubtitle, catalog.FieldDescription, catalog.FieldLanguage,
	catalog.FieldPublished, catalog.FieldPublisher, catalog.FieldSeries, catalog.FieldPageCount,
	catalog.FieldContributors, catalog.FieldTags, catalog.FieldCover,
}

var (
	// ErrMergeSelf is a book merged into itself.
	ErrMergeSelf = errors.New("a book is merged into another")
	// ErrMergeLibraries is two books of different libraries: a file stays
	// in its library's folder, so it cannot join a book of another.
	ErrMergeLibraries = errors.New("the books are in different libraries")
	// ErrMergeField is a field a merge does not take.
	ErrMergeField = errors.New("no such field to take")
)

// empty lists the fields the book has no value for, which a merge fills
// from the other book without being asked.
func empty(b catalog.Book) []string {
	var out []string
	add := func(field string, none bool) {
		if none {
			out = append(out, field)
		}
	}
	add(catalog.FieldSubtitle, b.Subtitle == "")
	add(catalog.FieldDescription, b.Description == "")
	add(catalog.FieldLanguage, b.Language == "")
	add(catalog.FieldPublished, b.PublishedOn == nil)
	add(catalog.FieldPublisher, b.Publisher == "")
	add(catalog.FieldSeries, b.Series == "")
	add(catalog.FieldPageCount, b.PageCount == nil)
	add(catalog.FieldContributors, len(b.Contributors) == 0)
	add(catalog.FieldTags, len(b.Tags) == 0)
	add(catalog.FieldCover, b.CoverKey == "")
	return out
}

// Merge makes two books of one library that the scope sees one: the book
// from is merged into the book into, which survives. Of from it takes the
// fields named in take, and those into has no value for; its files and
// identifiers, its places on shelves, and everyone's status, rating,
// progress, sessions and finishes, keeping of two the further. from is
// deleted and points to into, so that links to it lead there. All of it
// happens in one transaction, or none of it.
func (s *Service) Merge(ctx context.Context, scope library.Scope, into, from uuid.UUID, take []string) error {
	into, from, fields, err := s.prepareMerge(ctx, scope, into, from, take)
	if err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.mergeTx(ctx, tx, scope, into, from, fields)
	})
}

// prepareMerge checks a merge before its transaction, and says which books
// it is between, as merges before may have moved them, and which fields it
// takes.
func (s *Service) prepareMerge(ctx context.Context, scope library.Scope, into, from uuid.UUID, take []string) (uuid.UUID, uuid.UUID, []string, error) {
	if into == from {
		return into, from, nil, ErrMergeSelf
	}
	for _, f := range take {
		if !slices.Contains(MergeFields, f) {
			return into, from, nil, ErrMergeField
		}
	}
	books := catalog.NewService(s.pool)
	survivor, err := books.Get(ctx, scope, into)
	if err != nil {
		return into, from, nil, err
	}
	merged, err := books.Get(ctx, scope, from)
	if err != nil {
		return into, from, nil, err
	}
	// An ID of a book merged before is the book it lives on as.
	into, from = survivor.ID, merged.ID
	if into == from {
		return into, from, nil, ErrMergeSelf
	}
	if survivor.LibraryID != merged.LibraryID {
		return into, from, nil, ErrMergeLibraries
	}
	fields := slices.Clone(take)
	lacking := empty(merged)
	for _, f := range empty(survivor) {
		if !slices.Contains(lacking, f) && !slices.Contains(fields, f) {
			fields = append(fields, f)
		}
	}
	return into, from, fields, nil
}

// mergeTx is the merge, in the transaction given.
func (s *Service) mergeTx(ctx context.Context, tx pgx.Tx, scope library.Scope, into, from uuid.UUID, fields []string) error {
	q := sqlc.New(tx)
	locked, err := q.LockBooksForMerge(ctx, sqlc.LockBooksForMergeParams{
		Ids: []uuid.UUID{into, from}, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return err
	}
	if len(locked) != 2 {
		return catalog.ErrNotFound
	}
	if locked[0].LibraryID != locked[1].LibraryID {
		return ErrMergeLibraries
	}
	if err := q.CopyBookFields(ctx, sqlc.CopyBookFieldsParams{Fields: fields, IntoBook: into, FromBook: from}); err != nil {
		return err
	}
	if slices.Contains(fields, catalog.FieldContributors) {
		if err := q.DropContributors(ctx, into); err != nil {
			return err
		}
		if err := q.CopyContributors(ctx, sqlc.CopyContributorsParams{IntoBook: into, FromBook: from}); err != nil {
			return err
		}
		if err := q.RefreshAuthorSort(ctx, into); err != nil {
			return err
		}
	}
	if slices.Contains(fields, catalog.FieldTags) {
		if err := q.DropTags(ctx, into); err != nil {
			return err
		}
		if err := q.CopyTags(ctx, sqlc.CopyTagsParams{IntoBook: into, FromBook: from}); err != nil {
			return err
		}
	}
	steps := []func() error{
		func() error {
			return q.MoveFileIdentifiers(ctx, sqlc.MoveFileIdentifiersParams{ToBook: into, FromBook: from})
		},
		func() error { return q.MoveBookFiles(ctx, sqlc.MoveBookFilesParams{ToBook: into, FromBook: from}) },
		func() error {
			return q.MergeBookIdentifiers(ctx, sqlc.MergeBookIdentifiersParams{IntoBook: into, FromBook: from})
		},
		func() error { return q.MergeUserBooks(ctx, sqlc.MergeUserBooksParams{IntoBook: into, FromBook: from}) },
		func() error {
			return q.MergeReadingProgress(ctx, sqlc.MergeReadingProgressParams{IntoBook: into, FromBook: from})
		},
		func() error {
			return q.MoveReadingHistory(ctx, sqlc.MoveReadingHistoryParams{IntoBook: into, FromBook: from})
		},
		func() error {
			return q.MergeCollectionItems(ctx, sqlc.MergeCollectionItemsParams{IntoBook: into, FromBook: from})
		},
		func() error { return q.MergeRelations(ctx, sqlc.MergeRelationsParams{IntoBook: into, FromBook: from}) },
		func() error {
			return q.MoveNotifications(ctx, sqlc.MoveNotificationsParams{IntoBook: &into, FromBook: &from})
		},
		func() error { return q.DropBookMatches(ctx, from) },
		func() error {
			return q.SettleMergedPairs(ctx, sqlc.SettleMergedPairsParams{IntoBook: into, FromBook: from})
		},
		func() error { return q.DropBookChunks(ctx, from) },
		func() error {
			return q.ForwardMergedBooks(ctx, sqlc.ForwardMergedBooksParams{IntoBook: &into, FromBook: &from})
		},
		func() error { return q.MergeBookInto(ctx, sqlc.MergeBookIntoParams{ID: from, IntoBook: &into}) },
		func() error { return q.SetHeldFiles(ctx, into) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	// The book's text may now be read better from one of the files it
	// gained; its chunks are made again if so.
	primary, changed, err := choosePrimaryTx(ctx, q, into)
	if err != nil {
		return err
	}
	if changed && primary != uuid.Nil {
		if err := q.DeleteBookChunks(ctx, sqlc.DeleteBookChunksParams{BookID: into, FileID: primary}); err != nil {
			return err
		}
		if _, err := s.Queue.InsertTx(ctx, tx, ChunkArgs{FileID: primary}, jobs.InsertOpts{
			Queue: jobs.QueueExtract, Unique: true, MaxAttempts: extractAttempts,
		}); err != nil {
			return err
		}
	}
	return s.filesChangedTx(ctx, tx, into)
}
