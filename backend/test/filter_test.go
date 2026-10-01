//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// filterShelf fills a library with books that differ in every field a filter
// knows, and returns it.
func (a *app) filterShelf(admin *http.Client) string {
	a.t.Helper()
	ctx := context.Background()
	lib := a.library(admin, "Shelf", "shared")
	books := catalog.NewService(a.pool)
	add := func(b catalog.NewBook, formats ...string) {
		a.t.Helper()
		b.LibraryID = uuid.MustParse(lib)
		id, err := books.CreateBook(ctx, b)
		if err != nil {
			a.t.Fatal(err)
		}
		for _, f := range formats {
			_, err := books.AddFile(ctx, id, b.LibraryID, catalog.NewFile{
				RelPath: b.Title + "." + f, Format: f, ModifiedAt: time.Now(),
			})
			if err != nil {
				a.t.Fatal(err)
			}
		}
	}
	author := func(name string) []catalog.NewContributor {
		return []catalog.NewContributor{{Name: name, Role: catalog.RoleAuthor}}
	}
	add(catalog.NewBook{Title: "Emma", Contributors: author("Jane Austen"), Language: "en-GB", Published: "1815",
		Series: "Austen Novels", Tags: []string{"Fiction", "Classics"}}, "epub", "pdf")
	add(catalog.NewBook{Title: "Persuasion", Contributors: author("Jane Austen"), Language: "en", Published: "1817",
		Tags: []string{"Fiction"}}, "epub")
	add(catalog.NewBook{Title: "Jane Eyre", Contributors: author("Charlotte Brontë"), Language: "en", Published: "1847",
		Tags: []string{"Fiction", "Gothic"}}, "mobi")
	add(catalog.NewBook{Title: "Der Process", Contributors: author("Franz Kafka"), Language: "de", Published: "1925"}, "pdf")
	add(catalog.NewBook{Title: "Dune", Contributors: author("Frank Herbert"), Language: "en", Published: "1965-08-01",
		Series: "Dune", Tags: []string{"Science Fiction"}}, "m4b")
	add(catalog.NewBook{Title: "Untitled"})
	return lib
}

func TestFilters(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	a.filterShelf(admin)

	cases := []struct {
		filter string
		want   []string
	}{
		{`{"field": "author", "op": "in", "values": ["Jane Austen"]}`, []string{"Emma", "Persuasion"}},
		// Names compare by their key: no accents, case or commas.
		{`{"field": "author", "op": "in", "values": ["charlotte bronte", "FRANZ KAFKA"]}`, []string{"Der Process", "Jane Eyre"}},
		{`{"field": "author", "op": "empty"}`, []string{"Untitled"}},
		{`{"field": "series", "op": "in", "values": ["Dune"]}`, []string{"Dune"}},
		{`{"field": "series", "op": "empty"}`, []string{"Der Process", "Jane Eyre", "Persuasion", "Untitled"}},
		{`{"field": "tag", "op": "in", "values": ["fiction"]}`, []string{"Emma", "Jane Eyre", "Persuasion"}},
		{`{"field": "tag", "op": "empty"}`, []string{"Der Process", "Untitled"}},
		// A region is part of its language.
		{`{"field": "language", "op": "in", "values": ["EN"]}`, []string{"Dune", "Emma", "Jane Eyre", "Persuasion"}},
		{`{"field": "language", "op": "in", "values": ["de-AT"]}`, []string{"Der Process"}},
		{`{"field": "language", "op": "empty"}`, []string{"Untitled"}},
		{`{"field": "published", "op": "between", "values": ["1815", "1847"]}`, []string{"Emma", "Jane Eyre", "Persuasion"}},
		{`{"field": "published", "op": "between", "values": ["", "1816"]}`, []string{"Emma"}},
		{`{"field": "published", "op": "between", "values": ["1900", ""]}`, []string{"Der Process", "Dune"}},
		{`{"field": "published", "op": "empty"}`, []string{"Untitled"}},
		{`{"field": "format", "op": "in", "values": ["PDF"]}`, []string{"Der Process", "Emma"}},
		{`{"field": "format", "op": "in", "values": ["m4b", "mobi"]}`, []string{"Dune", "Jane Eyre"}},
		{`{"all": [{"field": "author", "op": "in", "values": ["jane austen"]}, {"field": "format", "op": "in", "values": ["pdf"]}]}`,
			[]string{"Emma"}},
		{`{"any": [{"field": "series", "op": "in", "values": ["dune"]}, {"field": "language", "op": "in", "values": ["de"]}]}`,
			[]string{"Der Process", "Dune"}},
		{`{"all": [{"field": "tag", "op": "in", "values": ["fiction"]}, {"not": {"field": "author", "op": "in", "values": ["jane austen"]}}]}`,
			[]string{"Jane Eyre"}},
		{`{"all": []}`, []string{"Der Process", "Dune", "Emma", "Jane Eyre", "Persuasion", "Untitled"}},
	}
	for _, c := range cases {
		// By sort title "Der Process" is "Process, Der"; the order is not
		// what these cases are about.
		got, _ := a.listAll(admin, url.Values{"filter": {c.filter}})
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s\n got %v\nwant %v", c.filter, got, c.want)
		}
	}

	// A filtered list is paged like any other.
	got, pages := a.listAll(admin, url.Values{"filter": {cases[7].filter}, "limit": {"1"}})
	slices.Sort(got)
	if pages != 4 || !slices.Equal(got, cases[7].want) {
		t.Errorf("a page at a time: %v on %d pages", got, pages)
	}

	for filter, want := range map[string]string{
		`{"field": "title", "op": "in", "values": ["Emma"]}`:                          "cannot be filtered by \"title\"",
		`{"field": "author", "op": "in", "values": []}`:                               "at least one value",
		`{"field": "format", "op": "in", "values": ["docx"]}`:                         "not a format",
		`{"field": "language", "op": "in", "values": ["e"]}`:                          "not a language code",
		`{"field": "published", "op": "between", "values": ["x", ""]}`:                "not a year",
		`{"field": "author", "op": "in", "values": ["Emma'); DROP TABLE books; --"]}`: "",
		`not json`: "not a rule tree",
	} {
		for _, path := range []string{"/books?", "/books/facets?"} {
			status, body, _ := a.call(admin, http.MethodGet, path+url.Values{"filter": {filter}}.Encode(), nil)
			if want == "" {
				// A value is only ever a parameter: this one matches no author.
				if status != 200 {
					t.Errorf("%s%s: %d %v", path, filter, status, body)
				}
				continue
			}
			if status != 422 || !strings.Contains(fieldError(body, "filter"), want) {
				t.Errorf("%s%s: %d %v, want 422 saying %q", path, filter, status, body, want)
			}
		}
	}
}

// facetCounts reads a facets answer as field → value → count, and the
// labels as field → value → label.
func facetCounts(t *testing.T, body map[string]any) (map[string]map[string]int, map[string]map[string]string) {
	t.Helper()
	counts := map[string]map[string]int{}
	labels := map[string]map[string]string{}
	for _, f := range body["facets"].([]any) {
		facet := f.(map[string]any)
		field := facet["field"].(string)
		counts[field] = map[string]int{}
		labels[field] = map[string]string{}
		for _, v := range facet["values"].([]any) {
			value := v.(map[string]any)
			counts[field][value["value"].(string)] = int(value["count"].(float64))
			labels[field][value["value"].(string)] = value["label"].(string)
		}
	}
	return counts, labels
}

func TestFacets(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := a.filterShelf(admin)
	private := a.library(admin, "Private", "private")
	if _, err := catalog.NewService(a.pool).CreateBook(context.Background(), catalog.NewBook{
		LibraryID: uuid.MustParse(private), Title: "Secret",
		Contributors: []catalog.NewContributor{{Name: "Hidden Author", Role: catalog.RoleAuthor}},
	}); err != nil {
		t.Fatal(err)
	}

	status, body, _ := a.call(reader, http.MethodGet, "/books/facets", nil)
	if status != 200 {
		t.Fatalf("facets: %d %v", status, body)
	}
	counts, labels := facetCounts(t, body)
	want := map[string]map[string]int{
		"author":    {"jane austen": 2, "charlotte bronte": 1, "franz kafka": 1, "frank herbert": 1},
		"series":    {"austen novels": 1, "dune": 1},
		"tag":       {"fiction": 3, "classics": 1, "gothic": 1, "science fiction": 1},
		"language":  {"en": 4, "de": 1},
		"published": {"1810": 2, "1840": 1, "1920": 1, "1960": 1},
		"format":    {"epub": 2, "pdf": 2, "mobi": 1, "m4b": 1},
	}
	for field, values := range want {
		if !mapsEqualInt(counts[field], values) {
			t.Errorf("for a reader, %s: %v, want %v", field, counts[field], values)
		}
	}
	if labels["author"]["charlotte bronte"] != "Charlotte Brontë" {
		t.Errorf("the label of a name is its spelling: %q", labels["author"]["charlotte bronte"])
	}

	// The administrator sees the private library too.
	_, body, _ = a.call(admin, http.MethodGet, "/books/facets", nil)
	if counts, _ := facetCounts(t, body); counts["author"]["hidden author"] != 1 {
		t.Errorf("the administrator's authors: %v", counts["author"])
	}
	_, body, _ = a.call(admin, http.MethodGet, "/books/facets?library="+lib, nil)
	if counts, _ := facetCounts(t, body); counts["author"]["hidden author"] != 0 {
		t.Errorf("one library's authors: %v", counts["author"])
	}

	// A field's own rules leave its counts alone; the others narrow them.
	filter := `{"all": [{"field": "author", "op": "in", "values": ["Jane Austen"]}, ` +
		`{"any": [{"field": "published", "op": "between", "values": ["1810", "1819"]}]}]}`
	_, body, _ = a.call(reader, http.MethodGet, "/books/facets?"+url.Values{"filter": {filter}}.Encode(), nil)
	counts, _ = facetCounts(t, body)
	narrowed := map[string]map[string]int{
		"author":    {"jane austen": 2},
		"published": {"1810": 2},
		"tag":       {"fiction": 2, "classics": 1},
		"format":    {"epub": 2, "pdf": 1},
		"language":  {"en": 2},
		"series":    {"austen novels": 1},
	}
	for field, values := range narrowed {
		if !mapsEqualInt(counts[field], values) {
			t.Errorf("filtered, %s: %v, want %v", field, counts[field], values)
		}
	}
}

func mapsEqualInt(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
