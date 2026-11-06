package canned

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

func open(t *testing.T, text string) *Provider {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func titles(records []metadata.Record) []string {
	var out []string
	for _, r := range records {
		out = append(out, r.Title)
	}
	return out
}

func TestTheRecordsAreFoundByISBNTitleAuthorAndSeries(t *testing.T) {
	p := open(t, `[
		{"id": "a", "title": "The Lantern Keeper", "authors": ["Moss, Ada"], "series": "Lanterns", "seriesIndex": 1, "isbns": ["978-0-306-40615-7"]},
		{"id": "b", "title": "The Lantern Thief", "authors": ["Ada Moss"], "series": "Lanterns", "seriesIndex": 2, "published": "2099"}
	]`)
	ctx := context.Background()
	got, err := p.Lookup(ctx, nil, catalog.Identifier{Type: catalog.IDISBN, Value: "9780306406157"})
	if err != nil || len(got) != 1 || got[0].ID != "a" || got[0].Contributors[0].Role != catalog.RoleAuthor {
		t.Errorf("by ISBN: %v %v", got, err)
	}
	got, err = p.Search(ctx, nil, metadata.Query{Title: "The lantern-keeper, chromium 1730"})
	if err != nil || len(titles(got)) != 1 || got[0].ID != "a" {
		t.Errorf("by a title holding the record's: %v %v", titles(got), err)
	}
	got, err = p.ByAuthor(ctx, nil, "Ada Moss")
	if err != nil || len(got) != 2 {
		t.Errorf("by author under either spelling: %v %v", titles(got), err)
	}
	got, err = p.BySeries(ctx, nil, "lanterns")
	if err != nil || len(got) != 2 || *got[1].SeriesIndex != 2 {
		t.Errorf("by series: %v %v", titles(got), err)
	}
	if _, err := p.Search(ctx, nil, metadata.Query{Title: "Lantern"}); !errors.Is(err, metadata.ErrNotFound) {
		t.Errorf("a title short of every record's: %v", err)
	}
}

func TestAFileWithABrokenISBNIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(`[{"title": "X", "isbns": ["123"]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("an ISBN that is none was taken")
	}
}
