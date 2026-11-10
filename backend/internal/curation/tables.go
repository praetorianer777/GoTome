// Package curation keeps what was changed to books other than by hand in
// an editor: runs, each of actions, each with the state of the books it
// touched before and after, so that a run or one action can be undone.
package curation

import (
	"strings"
)

// Table is one table of what an action snapshots about the books it
// touches.
type Table struct {
	Name string
	// Where selects the table's rows of the books in $1, a uuid[], as t.
	Where string
	// PK are the columns that identify a row.
	PK []string
	// Columns, when set, are the only columns snapshotted and restored: the
	// rest is the business of other jobs, such as a file's hash, which a
	// write-back changes after any action. The rows must exist at undo.
	Columns []string
	// Edit is a table an edit of one book changes; a merge changes all.
	Edit bool
	// Keep means rows that are in the table now but were not in the
	// snapshot stay at an undo.
	Keep bool
	// Quiet means other jobs change the table after an action as a matter
	// of course, so that it never counts as a conflict.
	Quiet bool
	// Settled, when set, says which rows count as a conflict: those that
	// a person or an action made, not those a pass of suggestions did.
	Settled func(row map[string]any) bool
	// Touch sets updated_at when the rows are restored.
	Touch bool
}

func byBook(name string, pk ...string) Table {
	return Table{Name: name, Where: "t.book_id = ANY($1)", PK: pk}
}

// Tables is every table a merge changes (the writes of backend/queries/
// merge.sql and the queries its steps call; TestTablesCoverWhatAMergeWrites
// holds the two to one list), in the order they are restored in.
var Tables = []Table{
	{
		Name: "books", Where: "t.id = ANY($1) OR t.merged_into_id = ANY($1)", PK: []string{"id"}, Edit: true, Keep: true, Touch: true,
		Columns: []string{
			"id", "title", "sort_title", "title_key", "subtitle", "description", "language", "published_on",
			"published_precision", "publisher_id", "series_id", "series_index", "page_count", "cover_key",
			"locked_fields", "field_sources", "merged_into_id", "deleted_at", "placeholder", "author_sort",
			"primary_text_file_id",
		},
	},
	{
		Name: "book_files", Where: "t.book_id = ANY($1)", PK: []string{"id"}, Keep: true,
		Columns: []string{"id", "book_id", "trashed_at", "trashed_by"},
	},
	func() Table {
		t := byBook("book_contributors", "book_id", "author_id", "role")
		t.Edit = true
		return t
	}(),
	func() Table { t := byBook("book_tags", "book_id", "tag_id"); t.Edit = true; return t }(),
	func() Table { t := byBook("book_identifiers", "id"); t.Edit = true; return t }(),
	byBook("user_books", "user_id", "book_id"),
	byBook("reading_progress", "user_id", "book_id", "medium"),
	byBook("book_notes", "user_id", "book_id"),
	byBook("recommendation_dismissals", "user_id", "book_id"),
	func() Table { t := byBook("reading_sessions", "id"); t.Keep = true; return t }(),
	func() Table { t := byBook("reading_finishes", "id"); t.Keep = true; return t }(),
	byBook("collection_items", "collection_id", "book_id"),
	byBook("deliveries", "id"),
	byBook("play_queue", "user_id", "book_id"),
	{Name: "book_relations", Where: "t.book_a = ANY($1) OR t.book_b = ANY($1)", PK: []string{"book_a", "book_b"}},
	{
		Name: "notifications", Where: "t.book_id = ANY($1)", PK: []string{"id"}, Keep: true, Quiet: true,
		Columns: []string{"id", "book_id"},
	},
	{
		Name: "book_genres", Where: "t.book_id = ANY($1)", PK: []string{"book_id", "genre"},
		Settled: func(row map[string]any) bool { return row["state"] != "suggested" },
	},
	func() Table { t := byBook("book_genre_checks", "book_id"); t.Quiet = true; return t }(),
	byBook("metadata_matches", "id"),
	{
		Name: "duplicate_pairs", Where: "t.book_a = ANY($1) OR t.book_b = ANY($1)", PK: []string{"id"}, Keep: true, Quiet: true,
	},
	{
		Name:  "duplicate_evidence",
		Where: "t.pair_id IN (SELECT p.id FROM duplicate_pairs p WHERE p.book_a = ANY($1) OR p.book_b = ANY($1))",
		PK:    []string{"pair_id", "kind", "detail"}, Keep: true, Quiet: true,
	},
}

// Rebuilt are the tables a merge or a replace writes that hold nothing
// but what is made again from the files; an undo queues the making and
// does not snapshot them.
var Rebuilt = []string{"book_chunks", "file_signatures"}

// Names lists the names of tables.
func Names(tables []Table) []string {
	out := make([]string, len(tables))
	for i, t := range tables {
		out[i] = t.Name
	}
	return out
}

// Set is which of the tables an action snapshots.
type Set int

const (
	// SetAll is what a merge or a replace changes.
	SetAll Set = iota
	// SetEdit is what an edit of one book changes.
	SetEdit
)

func (s Set) tables() []Table {
	if s == SetAll {
		return Tables
	}
	var out []Table
	for _, t := range Tables {
		if t.Edit {
			out = append(out, t)
		}
	}
	return out
}

// tablesIn returns the tables a snapshot holds rows of, in restore order.
func tablesIn(s Snapshot) []Table {
	var out []Table
	for _, t := range Tables {
		if _, ok := s[t.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

func quoteList(cols []string) string { return strings.Join(cols, ", ") }
