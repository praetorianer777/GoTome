//go:build integration

package test

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTheImprintFillsWhatTheFileDoesNotSay(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	path := filepath.Join(books, "pride.epub")
	imprint := "Pride and Prejudice. © 1813 Beispielverlag GmbH, Musterstadt. ISBN 978-0-306-40615-7 (E-Book)"
	writeEPUB(t, path, `<dc:title>Pride</dc:title><dc:creator>Austen</dc:creator>`, nil, imprint, "Chapter one.")

	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["pride.epub"]
	a.extract(file.ID)

	book := a.book(file.BookID)
	if len(book.Identifiers) != 1 || book.Identifiers[0].Type != "isbn" || book.Identifiers[0].Value != "9780306406157" {
		t.Fatalf("identifiers = %+v, want the ISBN of the imprint", book.Identifiers)
	}
	if book.Publisher != "Beispielverlag GmbH" || book.Sources["identifiers"] != "text" || book.Sources["publisher"] != "text" {
		t.Errorf("publisher %q, sources %+v, want the imprint's, from the text", book.Publisher, book.Sources)
	}

	writeEPUB(t, path, `<dc:title>Pride</dc:title><dc:creator>Austen</dc:creator>
		<dc:identifier>urn:isbn:978-1-86197-271-2</dc:identifier><dc:publisher>Other House</dc:publisher>`, nil, imprint, "Chapter one, longer.")
	a.scanNow(id)
	a.extract(file.ID)
	book = a.book(file.BookID)
	if len(book.Identifiers) != 1 || book.Identifiers[0].Value != "9781861972712" {
		t.Errorf("identifiers = %+v, want only the one the file's metadata names", book.Identifiers)
	}
	if book.Publisher != "Other House" || book.Sources["identifiers"] == "text" {
		t.Errorf("publisher %q, sources %+v, want the file's metadata to replace the text's", book.Publisher, book.Sources)
	}

	if _, err := a.pool.Exec(ctx, `DELETE FROM book_identifiers WHERE book_id = $1`, file.BookID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE books SET locked_fields = '{identifiers}' WHERE id = $1`, file.BookID); err != nil {
		t.Fatal(err)
	}
	writeEPUB(t, path, `<dc:title>Pride</dc:title><dc:creator>Austen</dc:creator>`, nil, imprint, "Chapter one, longer still.")
	a.scanNow(id)
	a.extract(file.ID)
	if book = a.book(file.BookID); len(book.Identifiers) != 0 {
		t.Errorf("identifiers = %+v, want none added to a locked field", book.Identifiers)
	}
}

func TestAPrintISBNOfTheImprintIsNotStored(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	books := t.TempDir()
	writeEPUB(t, filepath.Join(books, "pride.epub"), `<dc:title>Pride</dc:title><dc:creator>Austen</dc:creator>`, nil,
		"Taschenbuch ISBN 978-0-306-40615-7", "Chapter one.")
	id := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(id)
	file := a.shelfFiles()["pride.epub"]
	a.extract(file.ID)

	book := a.book(file.BookID)
	if len(book.Identifiers) != 0 {
		t.Errorf("identifiers = %+v, want none: the print edition's ISBN is not the book's", book.Identifiers)
	}
	if len(book.PrintISBNs) != 1 || book.PrintISBNs[0] != "9780306406157" {
		t.Errorf("print ISBNs = %v, want the one the text names, for the lookup", book.PrintISBNs)
	}

	writeEPUB(t, filepath.Join(books, "pride.epub"), `<dc:title>Pride</dc:title><dc:creator>Austen</dc:creator>`, nil,
		"ISBN 978-1-86197-271-2 (E-Book) ISBN 978-0-306-40615-7 (Print)", "Chapter one, longer.")
	a.scanNow(id)
	a.extract(file.ID)
	book = a.book(file.BookID)
	if len(book.Identifiers) != 1 || book.Identifiers[0].Value != "9781861972712" || len(book.PrintISBNs) != 0 {
		t.Errorf("identifiers %+v, print ISBNs %v; want the e-book's stored and no print one kept", book.Identifiers, book.PrintISBNs)
	}
	_ = ctx
}
