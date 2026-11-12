//go:build integration

package test

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

func TestTheSimilarCheckMeasuresAuthorsSeriesAndCopies(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "private"))
	books := catalog.NewService(a.pool)
	// Five books of one author, under two spellings, one a copy of another
	// with a shop's addition, two in a series; and one of another author.
	for i, b := range []struct{ title, author, series string }{
		{"Das stille Archiv", "Ulrike Weidner", "Archivreihe"},
		{"Die Schreiberin", "Ulrike Weidner", "Archivreihe"},
		{"Die Gräfin", "Weidner, Ulrike", ""},
		{"Der verschollene Abt", "Ulrike Weidner", ""},
		{"Das stille Archiv: Historischer Roman (German Edition)", "Weidner, Ulrike", ""},
		{"Die Silberweberin", "Greta Berndt", ""},
	} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: lib, Title: b.title, Series: b.series, Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		a.putVector(id, similar.KindContent, direction(map[int]float32{0: 1, i + 1: 0.1}))
	}

	r, err := a.server.Similar.Check(context.Background(), 50, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Every book is as near as any other, so the nearest come in the order
	// they were made. Each book's list takes two of its author's other
	// books, no copy, and the other author's one: a series book has the
	// other in its series among them.
	if r.Books != 5 || r.InSeries != 2 || r.Empty != 0 || r.Neighbours != 15 {
		t.Errorf("books %d, in a series %d, empty %d, neighbours %d", r.Books, r.InSeries, r.Empty, r.Neighbours)
	}
	if math.Abs(r.SameAuthor-2.0/3) > 1e-9 || r.Copies != 0 || math.Abs(r.SameSeries-1.0/3) > 1e-9 || r.Authors != 2 {
		t.Errorf("same author %v, copies %v, same series %v, authors %v", r.SameAuthor, r.Copies, r.SameSeries, r.Authors)
	}

	again, err := a.server.Similar.Check(context.Background(), 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	var first, second []string
	for _, s := range r.Samples[:3] {
		first = append(first, s.Title)
	}
	for _, s := range again.Samples {
		second = append(second, s.Title)
	}
	if !slices.Equal(first, second) {
		t.Errorf("a smaller sample is not the start of the larger: %v, %v", first, second)
	}
}

func TestSimilarBooksTakeTwoOfAnAuthorOrSeriesAndNoCopies(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ids := map[string]uuid.UUID{}
	// Nearest first: the further from the book, the larger the second axis.
	for i, b := range []struct{ title, author, series string }{
		{"Brennholds Schwert", "Marta Sieber", "Brennhold"},
		{"Brennholds Sohn: Brennhold Trilogie (German Edition)", "Sieber, Marta", "Brennhold"},
		{"Brennholds Sohn", "Marta Sieber", ""},
		{"Brennholds Blut", "Marta Sieber", "Brennhold"},
		{"Brennhold: Der Anfang", "Marta Sieber", ""},
		{"Im Namen der Nacht", "Colin Buckley", "Erbe der Lichter"},
		{"Die Stadt der Lichter", "Colin Buckley", "Erbe der Lichter"},
		{"Das Feuer erwacht", "Anna Andere", "Erbe der Lichter"},
		{"Die Silberweberin", "Greta Berndt", ""},
	} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{
			LibraryID: lib, Title: b.title, Series: b.series, Contributors: []catalog.NewContributor{{Name: b.author}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[b.title] = id
		a.putVector(id, similar.KindContent, direction(map[int]float32{0: 1, 1: float32(i) / 10}))
	}
	got := a.similarTitles(admin, ids["Brennholds Schwert"])
	want := []string{
		"Brennholds Sohn: Brennhold Trilogie (German Edition)", "Brennholds Blut",
		"Im Namen der Nacht", "Die Stadt der Lichter", "Die Silberweberin",
	}
	if !slices.Equal(got, want) {
		t.Errorf("similar books %q,\nwant %q", got, want)
	}
}
