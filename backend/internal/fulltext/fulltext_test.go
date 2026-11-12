package fulltext_test

import (
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/fulltext"
)

// open opens an index in a folder of the test's own, with the library
// GOTOME_TEST_FULLTEXT names; make check-go names the one make check-engine
// built, and the tests skip without it.
func open(t *testing.T) *fulltext.Index {
	t.Helper()
	path := os.Getenv("GOTOME_TEST_FULLTEXT")
	if path == "" {
		t.Skip("GOTOME_TEST_FULLTEXT does not name the engine's library; make check-engine builds it")
	}
	if _, err := fulltext.Load(path); err != nil {
		t.Fatal(err)
	}
	ix, err := fulltext.Open(t.TempDir(), 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func ids(hits []fulltext.Hit) []int64 {
	var out []int64
	for _, h := range hits {
		out = append(out, h.ID)
	}
	slices.Sort(out)
	return out
}

func TestTheIndexFindsWhatWasCommittedInTheLibrariesNamed(t *testing.T) {
	ix := open(t)
	libA, libB := uuid.New(), uuid.New()
	book1, book2, book3 := uuid.New(), uuid.New(), uuid.New()
	if err := ix.Apply(fulltext.Batch{Add: []fulltext.Doc{
		{ID: 1, BookID: book1, LibraryID: libA, Lang: "en", Text: "The whales were running before the ship."},
		{ID: 2, BookID: book2, LibraryID: libA, Lang: "de", Text: "In der Nacht brannten die Häuser am Fluss."},
		{ID: 3, BookID: book3, LibraryID: libB, Lang: "en", Text: "A whale runs where the old man stands."},
	}}); err != nil {
		t.Fatal(err)
	}
	both := []uuid.UUID{libA, libB}
	if hits, err := ix.Search(fulltext.Query{Words: "whale", Libraries: both, Limit: 10}); err != nil || len(hits) != 0 {
		t.Fatalf("before the commit: %v, %v", hits, err)
	}
	if err := ix.Commit(); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		q    fulltext.Query
		want []int64
	}{
		{"stemmed words", fulltext.Query{Words: "whale run", Libraries: both}, []int64{1, 3}},
		{"German", fulltext.Query{Words: "Haus Flüsse", Libraries: both}, []int64{2}},
		{"phrase", fulltext.Query{Phrases: []string{"in der nacht"}, Libraries: both}, []int64{2}},
		{"phrase out of order", fulltext.Query{Phrases: []string{"der in nacht"}, Libraries: both}, nil},
		{"one library", fulltext.Query{Words: "whale", Libraries: []uuid.UUID{libB}}, []int64{3}},
		{"no library", fulltext.Query{Words: "whale", Libraries: []uuid.UUID{}}, nil},
		{"books named", fulltext.Query{Words: "whale", Libraries: both, Books: []uuid.UUID{book1}}, []int64{1}},
		{"no book", fulltext.Query{Words: "whale", Libraries: both, Books: []uuid.UUID{}}, nil},
		{"group", fulltext.Query{Groups: [][]string{{"wale", "whale"}}, Libraries: both}, []int64{1, 3}},
		{"variants", fulltext.Query{Variants: [][]string{{"the olt man", "the old man"}}, Libraries: both}, []int64{3}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.q.Limit = 10
			hits, err := ix.Search(c.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(hits); !slices.Equal(got, c.want) {
				t.Errorf("found %v, want %v", got, c.want)
			}
		})
	}

	if err := ix.Apply(fulltext.Batch{DeleteChunks: []int64{1}, DeleteBooks: []uuid.UUID{book2}, DeleteLibraries: []uuid.UUID{libB}}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Commit(); err != nil {
		t.Fatal(err)
	}
	info, err := ix.Info()
	if err != nil || info.Docs != 0 {
		t.Errorf("after the deletes the index holds %+v, %v", info, err)
	}
}

func TestAClosedIndexSaysSo(t *testing.T) {
	ix := open(t)
	ix.Close()
	if _, err := ix.Search(fulltext.Query{Words: "x", Libraries: []uuid.UUID{uuid.New()}}); err == nil {
		t.Error("a closed index searched")
	}
	if err := ix.Close(); err != nil {
		t.Error(err)
	}
}

func TestASecondWriterIsRefused(t *testing.T) {
	path := os.Getenv("GOTOME_TEST_FULLTEXT")
	if path == "" {
		t.Skip("GOTOME_TEST_FULLTEXT does not name the engine's library")
	}
	if _, err := fulltext.Load(path); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	w, err := fulltext.Open(dir, 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := fulltext.Open(dir, 20_000_000); err == nil {
		t.Error("a second writer opened the index")
	}
	r, err := fulltext.Open(dir, 0)
	if err != nil {
		t.Fatalf("a reader beside the writer: %v", err)
	}
	defer r.Close()
	if err := r.Apply(fulltext.Batch{DeleteChunks: []int64{1}}); err == nil {
		t.Error("a reader wrote")
	}
}
