//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type pairView struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Books []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"books"`
	Evidence []struct {
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	} `json:"evidence"`
}

func (a *app) duplicates(c *http.Client, query string) []pairView {
	a.t.Helper()
	status, body := a.get(c, "/duplicates"+query)
	if status != 200 {
		a.t.Fatalf("duplicates: %d %s", status, body)
	}
	var out struct {
		Pairs []pairView `json:"pairs"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		a.t.Fatal(err)
	}
	return out.Pairs
}

// described is each pair as "title + title: kinds", sorted, to compare.
func described(pairs []pairView) []string {
	var out []string
	for _, p := range pairs {
		titles := []string{p.Books[0].Title, p.Books[1].Title}
		slices.Sort(titles)
		var kinds []string
		for _, e := range p.Evidence {
			kinds = append(kinds, e.Kind)
		}
		slices.Sort(kinds)
		out = append(out, strings.Join(titles, " + ")+": "+strings.Join(kinds, ","))
	}
	slices.Sort(out)
	return out
}

func TestDuplicatesAreFoundWithTheirEvidence(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")

	books := t.TempDir()
	meta := func(id, title, author, extra string) string {
		return `<dc:identifier id="uid">urn:uuid:` + id + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator opf:role="aut">` + author + `</dc:creator>
    <dc:language>en</dc:language>` + extra
	}
	write := func(path, metadata, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(books, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeEPUB(t, filepath.Join(books, path), metadata, nil, text)
	}
	isbn := `<dc:identifier opf:scheme="ISBN">978-0-306-40615-7</dc:identifier>`
	// The same file twice, in two folders.
	write("one/Alpha.epub", meta("alpha", "Alpha", "Ann Archer", ""), "Alpha's own text.")
	data, err := os.ReadFile(filepath.Join(books, "one/Alpha.epub"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(books, "two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(books, "two/Alpha.epub"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The same text with other details.
	write("three/Beta.epub", meta("beta", "Beta", "Bea Baker", ""), "Beta's own text.")
	write("four/Beta edited.epub", meta("beta2", "Beta, Revised", "Bert Bloggs", ""), "Beta's own text.")
	// Another text, one ISBN.
	write("five/Gamma.epub", meta("gamma", "Gamma", "Gus Grey", isbn), "Gamma's text.")
	write("six/Delta.epub", meta("delta", "Delta", "Dot Dean", isbn), "Delta's text.")
	// Another text, the same title and author.
	write("seven/Epsilon.epub", meta("eps1", "Epsilon", "Eve Ellis", ""), "One telling.")
	write("eight/Epsilon.epub", meta("eps2", "Epsilon", "Eve Ellis", ""), "Another telling.")
	// Nothing in common with anything.
	write("nine/Zeta.epub", meta("zeta", "Zeta", "Zed Zane", ""), "Zeta stands alone.")
	// As serve has it: a book whose file was read is checked.
	a.scans.OnExtracted = a.server.Duplicates.EnqueueTx
	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	var bookIDs []uuid.UUID
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
		bookIDs = append(bookIDs, f.BookID)
	}
	var queued int
	if err := a.pool.QueryRow(ctx, "SELECT count(DISTINCT args->>'bookId') FROM river_job WHERE kind = 'dedupe.check_book'").Scan(&queued); err != nil || queued != len(bookIDs) {
		t.Errorf("checks queued on reading: %d of %d, %v", queued, len(bookIDs), err)
	}
	check := func() {
		t.Helper()
		for _, id := range bookIDs {
			if err := a.server.Duplicates.Check(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	check()
	want := []string{
		"Alpha + Alpha: sha256,title_author",
		"Beta + Beta, Revised: content",
		"Delta + Gamma: isbn",
		"Epsilon + Epsilon: title_author",
	}
	if got := described(a.duplicates(admin, "")); !slices.Equal(got, want) {
		t.Errorf("pairs:\n got %v\nwant %v", got, want)
	}
	for _, p := range a.duplicates(admin, "") {
		for _, e := range p.Evidence {
			if e.Detail == "" {
				t.Errorf("%v: evidence without detail", p)
			}
		}
	}

	// Checking again finds the same, and makes nothing twice.
	check()
	var pairs, evidence int
	if err := a.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM duplicate_pairs), (SELECT count(*) FROM duplicate_evidence)").Scan(&pairs, &evidence); err != nil {
		t.Fatal(err)
	}
	if pairs != 4 || evidence != 5 {
		t.Errorf("after a second check: %d pairs, %d evidence", pairs, evidence)
	}

	// Evidence that no longer holds goes, and a pair without any with it;
	// a pair someone decided about stays as they left it.
	var epsilon string
	for _, p := range a.duplicates(admin, "") {
		if p.Books[0].Title == "Epsilon" {
			epsilon = p.Books[0].ID
		}
		if p.Books[0].Title == "Gamma" || p.Books[0].Title == "Delta" {
			if _, err := a.pool.Exec(ctx, "UPDATE duplicate_pairs SET state = 'kept_both' WHERE id = $1", p.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+epsilon, map[string]any{"title": "Epsilon Retold"}); status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	check()
	want = []string{
		"Alpha + Alpha: sha256,title_author",
		"Beta + Beta, Revised: content",
	}
	if got := described(a.duplicates(admin, "")); !slices.Equal(got, want) {
		t.Errorf("after the edit:\n got %v\nwant %v", got, want)
	}
	if got := described(a.duplicates(admin, "?state=kept_both")); !slices.Equal(got, []string{"Delta + Gamma: isbn"}) {
		t.Errorf("kept both: %v", got)
	}

	// A library's books are checked by asking; a reader may not ask.
	status, out, _ := a.call(admin, http.MethodPost, "/duplicates/checks", map[string]any{"library": lib})
	if status != 202 || out["books"] != float64(len(bookIDs)) {
		t.Errorf("check the library: %d %v", status, out)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/duplicates/checks", map[string]any{}); status != 403 {
		t.Errorf("a reader asks: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodPost, "/duplicates/checks", map[string]any{"library": uuid.NewString()}); status != 404 {
		t.Errorf("no such library: %d", status)
	}
	// Pages, newest first.
	first := a.duplicates(admin, "?limit=1")
	second := a.duplicates(admin, "?limit=1&before="+first[0].ID)
	if len(first) != 1 || len(second) != 1 || first[0].ID <= second[0].ID {
		t.Errorf("pages: %v then %v", first, second)
	}
}
