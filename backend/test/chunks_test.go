//go:build integration

package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// storedChunk is a row of book_chunks as the tests look at it.
type storedChunk struct {
	FileID           uuid.UUID
	LibraryID        uuid.UUID
	Position         int
	Chapter          string
	PageFrom, PageTo *int32
	Offset           int
	Lang             string
	Text             string
}

func (a *app) chunks(bookID uuid.UUID) []storedChunk {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(), `
		SELECT file_id, library_id, position, chapter, page_from, page_to, char_offset, lang,
		       coalesce(body_en, body_de, body_xx)
		FROM book_chunks WHERE book_id = $1 ORDER BY position`, bookID)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out []storedChunk
	for rows.Next() {
		var c storedChunk
		if err := rows.Scan(&c.FileID, &c.LibraryID, &c.Position, &c.Chapter, &c.PageFrom, &c.PageTo, &c.Offset, &c.Lang, &c.Text); err != nil {
			a.t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func (a *app) countRows(query string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestAnEPUBsTextIsKeptAsChunksInOrder(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	long := strings.Repeat("Emma Woodhouse, handsome, clever, and rich, with a comfortable home and a happy disposition. ", 200)
	german := strings.Repeat("Die Kinder spielten am Ufer des Flusses, und die alten Häuser standen still daneben. ", 150)
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, long, german, "The end.")
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)

	chunks := a.chunks(file.BookID)
	if len(chunks) < 3 {
		t.Fatalf("%d chunks", len(chunks))
	}
	var texts []string
	for i, c := range chunks {
		if c.Position != i || c.FileID != file.ID || c.LibraryID.String() != id {
			t.Errorf("chunk %d: %+v", i, c)
		}
		if c.PageFrom == nil || c.PageTo == nil || *c.PageTo < *c.PageFrom {
			t.Errorf("chunk %d has pages %v–%v", i, c.PageFrom, c.PageTo)
		}
		texts = append(texts, c.Text)
	}
	whole := strings.Join(texts, "\n\n")
	if !strings.HasPrefix(whole, "Emma Woodhouse") || !strings.Contains(whole, "Die Kinder spielten") || !strings.HasSuffix(whole, "The end.") {
		t.Errorf("the chunks do not hold the book's text in order: %.60q … %.60q", whole, whole[len(whole)-60:])
	}
	if chunks[0].Lang != "en" || chunks[len(chunks)-2].Lang != "de" {
		t.Errorf("languages %q and %q", chunks[0].Lang, chunks[len(chunks)-2].Lang)
	}
	if chunks[0].Offset != 0 || chunks[1].Offset <= chunks[0].Offset {
		t.Errorf("offsets %d, %d", chunks[0].Offset, chunks[1].Offset)
	}
	if n := a.countRows("SELECT count(*) FROM search_words WHERE word IN ('woodhouse', 'kinder', 'häuser')"); n != 3 {
		t.Errorf("%d of three words in the vocabulary", n)
	}
	if n := a.countRows("SELECT count(*) FROM book_files WHERE id = $1 AND chunked_at IS NOT NULL", file.ID); n != 1 {
		t.Error("the file is not recorded as chunked")
	}

	// Read again: the same chunks, not twice as many.
	a.extract(file.ID)
	if again := a.chunks(file.BookID); len(again) != len(chunks) {
		t.Errorf("%d chunks after reading again, %d before", len(again), len(chunks))
	}

	// Moved to another library, the chunks go along.
	other := a.externalLibrary(admin, "Other", "shared", t.TempDir())
	if _, err := a.pool.Exec(context.Background(), "UPDATE books SET library_id = $2 WHERE id = $1", file.BookID, other); err != nil {
		t.Fatal(err)
	}
	if n := a.countRows("SELECT count(*) FROM book_chunks WHERE book_id = $1 AND library_id <> $2", file.BookID, other); n != 0 {
		t.Errorf("%d chunks left in the old library", n)
	}
}

func TestTheBooksTextComesFromItsBestFile(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	for _, name := range []string{"Emma.epub", "Emma.pdf"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "books", "Jane Austen", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(books, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	files := a.shelfFiles()
	pdf, epub := files["Emma.pdf"], files["Emma.epub"]
	from := func() uuid.UUID {
		chunks := a.chunks(pdf.BookID)
		if len(chunks) == 0 {
			t.Fatal("no chunks")
		}
		for _, c := range chunks[1:] {
			if c.FileID != chunks[0].FileID {
				t.Fatal("the chunks come from two files")
			}
		}
		return chunks[0].FileID
	}

	a.extract(pdf.ID)
	if from() != pdf.ID {
		t.Fatal("with only the PDF read, its text is not the book's")
	}
	if c := a.chunks(pdf.BookID)[0]; c.PageFrom == nil || *c.PageFrom != 1 || c.Chapter != "" {
		t.Errorf("the PDF's chunk: page %v, chapter %q", c.PageFrom, c.Chapter)
	}
	a.extract(epub.ID)
	if from() != epub.ID {
		t.Error("the EPUB's text did not replace the PDF's")
	}
	a.extract(pdf.ID)
	if from() != epub.ID {
		t.Error("reading the PDF again took the text back from the EPUB")
	}
	if n := a.countRows("SELECT count(*) FROM books WHERE id = $1 AND primary_text_file_id = $2", pdf.BookID, epub.ID); n != 1 {
		t.Error("the EPUB is not the book's primary text file")
	}
}

func TestChunksAreRebuiltFromTheFile(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "Emma.epub"), emmaMetadata, nil, strings.Repeat("Emma Woodhouse, handsome, clever, and rich. ", 300))
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["Emma.epub"]
	a.extract(file.ID)
	want := a.chunks(file.BookID)

	// As a book read before chunks were kept: no chunks, not chunked.
	ctx := context.Background()
	if _, err := a.pool.Exec(ctx, "DELETE FROM book_chunks WHERE book_id = $1", file.BookID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, "UPDATE book_files SET chunked_at = NULL WHERE id = $1", file.ID); err != nil {
		t.Fatal(err)
	}
	if n := a.countRows("SELECT count(*) FROM book_files f JOIN books b ON b.primary_text_file_id = f.id WHERE f.chunked_at IS NULL AND b.id = $1", file.BookID); n != 1 {
		t.Fatal("the file is not waiting to be chunked")
	}
	if err := a.scans.Chunk(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	got := a.chunks(file.BookID)
	if len(got) != len(want) || got[0].Text != want[0].Text {
		t.Errorf("rebuilt %d chunks, %d before", len(got), len(want))
	}
}
