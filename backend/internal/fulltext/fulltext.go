// Package fulltext is GOtome's full-text index (#265,
// docs/decisions/text-store.md): Tantivy in the app's process, in the Rust
// library engine/ builds, which this package loads at run time as embed
// loads ONNX Runtime. The index holds no text, only each chunk's words in
// the field of its language (English and German stemmed, every other
// language as written), its book and its library.
//
// One process holds the writer of an index; others may open it to search.
package fulltext

import (
	"errors"

	"github.com/google/uuid"
)

// ErrUnavailable is an engine that cannot run here: a binary built without
// cgo, or the library missing.
var ErrUnavailable = errors.New("the full-text engine is not available")

// MinHeap is the least memory the writer's buffer may have; the engine
// takes this much when asked for less.
const MinHeap = 15_000_000

// Doc is a chunk's words, under its ID.
type Doc struct {
	ID        int64     `json:"id"`
	BookID    uuid.UUID `json:"book"`
	LibraryID uuid.UUID `json:"library"`
	// Lang is "en" or "de", whose words are stemmed, or anything else.
	Lang string `json:"lang"`
	Text string `json:"text"`
}

// Batch is a change to the index: its deletes first, then its documents.
// A chunk added again under its ID must be deleted in the same batch, or it
// is found twice.
type Batch struct {
	DeleteChunks    []int64     `json:"delete_chunks,omitempty"`
	DeleteBooks     []uuid.UUID `json:"delete_books,omitempty"`
	DeleteLibraries []uuid.UUID `json:"delete_libraries,omitempty"`
	Add             []Doc       `json:"add,omitempty"`
}

// Query is what a chunk must hold, in one of the language fields: every
// free word in any form, one word of each group, each phrase in order and
// one phrase of each set of variants. Only chunks of the libraries named
// and, when Books is not nil, of the books named are found.
type Query struct {
	Words     string      `json:"words,omitempty"`
	Groups    [][]string  `json:"groups,omitempty"`
	Phrases   []string    `json:"phrases,omitempty"`
	Variants  [][]string  `json:"variants,omitempty"`
	Libraries []uuid.UUID `json:"libraries"`
	Books     []uuid.UUID `json:"books"`
	// Limit is how many of the best chunks are returned.
	Limit int `json:"limit"`
}

// Hit is a chunk found.
type Hit struct {
	ID    int64   `json:"id"`
	Score float64 `json:"score"`
}

// Passage is text to make a snippet of, in the language its chunk is in.
type Passage struct {
	Lang string `json:"lang"`
	Text string `json:"text"`
}

// Snippet is a fragment of a passage, and the byte ranges in it that match.
type Snippet struct {
	Fragment    string   `json:"fragment"`
	Highlighted [][2]int `json:"highlighted"`
}

type snippetRequest struct {
	Query
	Passages []Passage `json:"passages"`
	MaxChars int       `json:"max_chars"`
}

// Info is what the index holds, as of its last commit.
type Info struct {
	Docs     int64 `json:"docs"`
	Segments int   `json:"segments"`
}
