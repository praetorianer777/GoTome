//go:build integration

package test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// twinProvider answers a search with two records alike in everything but
// their descriptions, and a lookup by ISBN with one.
type twinProvider struct{}

func (twinProvider) Name() string            { return "twins" }
func (twinProvider) Limits() metadata.Limits { return metadata.Limits{} }

const (
	lagoonDescription = "A detective investigates a murder among the palaces and canals of an old lagoon city in winter."
	gardenDescription = "A year in a garden: what to sow in spring, how to prune in autumn and which roses suit a cold, wet climate."
)

func (twinProvider) records() []metadata.Record {
	authors := []catalog.NewContributor{{Name: "Ada Quill", Role: catalog.RoleAuthor}}
	return []metadata.Record{
		{ID: "garden", Title: "The Lamp", Language: "en", Contributors: authors, Description: gardenDescription},
		{ID: "lagoon", Title: "The Lamp", Language: "en", Contributors: authors, Description: lagoonDescription},
	}
}

func (p twinProvider) Lookup(_ context.Context, _ metadata.Web, id catalog.Identifier) ([]metadata.Record, error) {
	r := p.records()[1]
	r.Identifiers = []catalog.Identifier{id}
	return []metadata.Record{r}, nil
}

func (p twinProvider) Search(context.Context, metadata.Web, metadata.Query) ([]metadata.Record, error) {
	return p.records(), nil
}

func TestACandidatesDescriptionIsComparedWithTheBooksOwnVectors(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	ctx := context.Background()
	isbn, _ := catalog.NormalizeIdentifier(catalog.IDISBN, isbnFor(7))
	id, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
		LibraryID: lib, Title: "The Lamp", Language: "en", Contributors: []catalog.NewContributor{{Name: "Ada Quill"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Its text is that of the lagoon description, to the fake model.
	fake := &embed.Fake{}
	vecs, err := fake.Embed(ctx, []string{embed.E5Small.Prefix + lagoonDescription})
	if err != nil {
		t.Fatal(err)
	}
	a.putVector(id, similar.KindContent, vecs[0])
	other, err := catalog.NewService(a.pool).CreateBook(ctx, catalog.NewBook{
		LibraryID: lib, Title: "The Lamp", Language: "en", Contributors: []catalog.NewContributor{{Name: "Ada Quill"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var embedded atomic.Int64
	svc := metadata.NewService(a.pool, []metadata.Provider{twinProvider{}}, metadata.Options{
		AllowPrivate: true,
		Weights:      func(context.Context) string { return metadata.ScoreImported },
	})
	svc.Describe = func(ctx context.Context, book uuid.UUID, texts []string) ([]*float64, error) {
		embedded.Add(int64(len(texts)))
		return a.server.Similar.CompareToBook(ctx, book, texts)
	}
	profile := `{"format": "gotome-match-weights", "featureVersion": 1, "gotomeVersion": "test", "books": 10,
		"languages": {}, "providers": {}, "weights": {"bias": -1, "title": 2, "description": 1.5}}`
	if err := svc.ImportProfile(ctx, []byte(profile)); err != nil {
		t.Fatal(err)
	}

	book := catalog.Book{ID: id, Title: "The Lamp", Language: "en", Contributors: []catalog.Contributor{{Name: "Ada Quill", Role: catalog.RoleAuthor}}}
	got, err := svc.Candidates(ctx, book)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].ID != "lagoon" || got[0].Score <= got[1].Score {
		t.Errorf("the description near the book's text should come first: %s %.3f, %s %.3f", got[0].ID, got[0].Score, got[1].ID, got[1].Score)
	}
	if embedded.Load() != 2 {
		t.Errorf("descriptions embedded: %d", embedded.Load())
	}
	if _, err := svc.Candidates(ctx, book); err != nil {
		t.Fatal(err)
	}

	// A book without a content vector embeds nothing, and ranks by the rest.
	none, err := svc.Candidates(ctx, catalog.Book{ID: other, Title: "The Lamp", Language: "en", Contributors: book.Contributors})
	if err != nil || len(none) != 2 || none[0].Score != none[1].Score {
		t.Errorf("without vectors: %+v %v", none, err)
	}

	// A book looked up by its ISBN is not told apart by its description.
	before := embedded.Load()
	withISBN := book
	withISBN.Identifiers = []catalog.BookIdentifier{{Identifier: isbn}}
	if found, err := svc.Candidates(ctx, withISBN); err != nil || len(found) != 1 || found[0].Score != 1 || embedded.Load() != before {
		t.Errorf("by ISBN: %+v %v (embedded %d more)", found, err, embedded.Load()-before)
	}
}
