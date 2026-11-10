package ingest

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// AutoMerge is a merge a rule has decided on: which book survives, which
// fields it takes, and which of the files of either book are copies to
// trash.
type AutoMerge struct {
	Into, From uuid.UUID
	// Take are fields the surviving book takes from the other (MergeFields).
	Take []string
	// Union are fields (tags and contributors) it gains the other's values
	// of, keeping its own.
	Union []string
	// Trash are live files of either book, of formats both hold.
	Trash []uuid.UUID
	// Rule and Reason are recorded with the action.
	Rule, Reason string
}

// MergeAutomatically makes the merge as one action of the run the context
// belongs to (curation.WithRun): the books, the fields, the files trashed
// as Replace does, and the record that undoes it, in one transaction.
func (s *Service) MergeAutomatically(ctx context.Context, scope library.Scope, m AutoMerge) error {
	into, from, fields, err := s.prepareMerge(ctx, scope, m.Into, m.From, m.Take)
	if err != nil {
		return err
	}
	var made []fileMove
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if err := q.HoldLibrariesOfBooks(ctx, []uuid.UUID{into, from}); err != nil {
			return err
		}
		pending, err := s.Curation.Begin(ctx, tx, curation.SetAll, into, from)
		if err != nil {
			return err
		}
		moves, err := s.trashFilesTx(ctx, tx, scope, m.Trash)
		if err != nil {
			return err
		}
		for i, move := range moves {
			if move.bookID == into {
				if err := s.textGoneTx(ctx, tx, into, m.Trash[i]); err != nil {
					return err
				}
			}
		}
		if err := s.mergeTx(ctx, tx, scope, into, from, fields, m.Union); err != nil {
			return err
		}
		kind := curation.ActionMerge
		if len(m.Trash) > 0 {
			kind = curation.ActionReplace
			if err := q.ProgressByFraction(ctx, into); err != nil {
				return err
			}
			if err := q.SetPairReplaced(ctx, sqlc.SetPairReplacedParams{IntoBook: into, FromBook: from}); err != nil {
				return err
			}
		}
		trashed := slices.Clone(m.Trash)
		if err := pending.Done(ctx, tx, curation.Record{
			RunKind: curation.KindDuplicates, By: scope.Viewer, Kind: kind, TrashedFiles: trashed,
			Rule: m.Rule, Reason: m.Reason,
		}); err != nil {
			return err
		}
		return s.moveFiles(moves, &made)
	})
	if err != nil {
		s.moveFilesBack(made)
	}
	return s.noteUnwritable(ctx, err)
}
