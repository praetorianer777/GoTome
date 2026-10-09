//go:build integration

package test

import (
	"context"
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
		{"Das letzte Evangelium", "Barbara Goldstein", "Evangelien"},
		{"Die Evangelistin", "Barbara Goldstein", "Evangelien"},
		{"Die Kardinälin", "Goldstein, Barbara", ""},
		{"Der vergessene Papst", "Barbara Goldstein", ""},
		{"Das letzte Evangelium: Historischer Roman (German Edition)", "Goldstein, Barbara", ""},
		{"Die Goldspinnerin", "Gerit Bertram", ""},
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
	if r.Books != 5 || r.InSeries != 2 || r.Empty != 0 || r.Neighbours != 25 {
		t.Errorf("books %d, in a series %d, empty %d, neighbours %d", r.Books, r.InSeries, r.Empty, r.Neighbours)
	}
	if r.SameAuthor != 0.8 || r.Copies != 0.08 || r.SameSeries != 0.2 || r.Authors != 2 {
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
