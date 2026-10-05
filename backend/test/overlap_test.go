//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// prose is n words of made-up text, the same for the same seed.
func prose(seed uint64, n int) []string {
	r := rand.New(rand.NewPCG(seed, 7))
	syllables := []string{"ka", "lo", "mi", "ren", "tu", "sa", "vel", "or", "ni", "da", "esh", "pa", "qui", "zor", "bel", "thu"}
	out := make([]string, n)
	for i := range out {
		var w strings.Builder
		for range 2 + r.IntN(3) {
			w.WriteString(syllables[r.IntN(len(syllables))])
		}
		out[i] = w.String()
	}
	return out
}

// paragraphs joins the words into paragraphs of a hundred.
func paragraphs(ws []string) []string {
	var out []string
	for i := 0; i < len(ws); i += 100 {
		out = append(out, strings.Join(ws[i:min(i+100, len(ws))], " ")+".")
	}
	return out
}

// textPDF writes a PDF of the words, sixty lines of twelve to a page, with
// a running head on each, as a print edition has.
func textPDF(t *testing.T, path, head string, ws []string) {
	t.Helper()
	var objects []string
	add := func(o string) int { objects = append(objects, o); return len(objects) }
	catalog := add("")
	tree := add("")
	font := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	var kids []string
	for start := 0; start < len(ws); start += 720 {
		var b strings.Builder
		fmt.Fprintf(&b, "BT /F1 9 Tf 30 800 Td 12 TL (%s) Tj T*", head)
		page := ws[start:min(start+720, len(ws))]
		for i := 0; i < len(page); i += 12 {
			fmt.Fprintf(&b, " (%s) Tj T*", strings.Join(page[i:min(i+12, len(page))], " "))
		}
		b.WriteString(" ET")
		content := b.String()
		stream := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		p := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 595 842] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", tree, stream, font))
		kids = append(kids, fmt.Sprintf("%d 0 R", p))
	}
	objects[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", tree)
	objects[tree-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids))
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, catalog, xref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// overlapOf is the overlap evidence between the two titles, or "".
func overlapOf(pairs []pairView, x, y string) string {
	for _, p := range pairs {
		titles := []string{p.Books[0].Title, p.Books[1].Title}
		if !slices.Contains(titles, x) || !slices.Contains(titles, y) {
			continue
		}
		for _, e := range p.Evidence {
			if e.Kind == "overlap" {
				return e.Detail
			}
		}
	}
	return ""
}

func TestSharedTextIsFoundAcrossFormatsAndInOmnibuses(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	a.scans.OnExtracted = a.server.Duplicates.EnqueueTx
	a.scans.OnChunked = a.server.Duplicates.ChunkedTx

	books := t.TempDir()
	meta := func(id, title string) string {
		return `<dc:identifier id="uid">urn:uuid:` + id + `</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator opf:role="aut">` + title + ` Writer</dc:creator>
    <dc:language>en</dc:language>`
	}
	epub := func(path, id, title string, chapters ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(books, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeEPUB(t, filepath.Join(books, path), meta(id, title), nil, chapters...)
	}
	// One book as an EPUB and, under another title, as a PDF.
	river := prose(1, 30000)
	epub("a/River.epub", "river", "River", strings.Join(paragraphs(river), "\n\n"))
	textPDF(t, filepath.Join(books, "b/Flow.pdf"), "FLOW", river)
	// Three novels in one volume, and the second of them alone.
	var novels []string
	for i := range 3 {
		novels = append(novels, strings.Join(paragraphs(prose(uint64(10+i), 50000)), "\n\n"))
	}
	epub("c/Trilogy.epub", "trilogy", "Trilogy", novels...)
	epub("d/Middle.epub", "middle", "Middle", novels[1])
	// Nothing in common with the rest.
	epub("e/Other.epub", "other", "Other", strings.Join(paragraphs(prose(99, 30000)), "\n\n"))

	lib := a.externalLibrary(admin, "Shelf", "shared", books)
	a.scanNow(lib)
	var bookIDs []uuid.UUID
	for _, f := range a.shelfFiles() {
		a.extract(f.ID)
		bookIDs = append(bookIDs, f.BookID)
	}
	var signed, buckets int
	if err := a.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM file_signatures WHERE signature IS NOT NULL), (SELECT count(*) FROM file_lsh)").Scan(&signed, &buckets); err != nil {
		t.Fatal(err)
	}
	if signed != 5 || buckets < 5*64 {
		t.Fatalf("signed %d files into %d buckets", signed, buckets)
	}
	for _, id := range bookIDs {
		if err := a.server.Duplicates.Check(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	pairs := a.duplicates(admin, "")

	var j, ab, ba float64
	if _, err := fmt.Sscanf(overlapOf(pairs, "River", "Flow"), "jaccard=%f a_in_b=%f b_in_a=%f", &j, &ab, &ba); err != nil || j < 0.6 {
		t.Errorf("EPUB and PDF: %q", overlapOf(pairs, "River", "Flow"))
	}
	detail := overlapOf(pairs, "Middle", "Trilogy")
	if _, err := fmt.Sscanf(detail, "jaccard=%f a_in_b=%f b_in_a=%f", &j, &ab, &ba); err != nil || max(ab, ba) < 0.8 || min(ab, ba) > 0.5 {
		t.Errorf("novel in omnibus: %q", detail)
	}
	for _, title := range []string{"River", "Flow", "Trilogy", "Middle"} {
		if d := overlapOf(pairs, "Other", title); d != "" {
			t.Errorf("Other and %s: %q", title, d)
		}
	}

	// Among 50,000 signed files, checking a book looks its buckets up in
	// the index, which answers at once; reading every bucket would not.
	if _, err := a.pool.Exec(ctx, `
		WITH lib AS (SELECT $1::uuid AS id),
		made AS (
			INSERT INTO books (library_id, title, sort_title, title_key)
			SELECT (SELECT id FROM lib), 'Filler', 'Filler', 'filler' FROM generate_series(1, 50000)
			RETURNING id),
		files AS (
			INSERT INTO book_files (book_id, library_id, kind, format, rel_path, size_bytes, modified_at)
			SELECT id, (SELECT id FROM lib), 'ebook', 'epub', 'filler/' || id || '.epub', 1, now() FROM made
			RETURNING id)
		INSERT INTO file_signatures (file_id, version, shingles, signature)
		SELECT id, 1, 10000, decode(repeat(md5(id::text), 128), 'hex') FROM files`, lib); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `
		UPDATE books b SET primary_text_file_id = f.id FROM book_files f
		WHERE f.book_id = b.id AND b.title = 'Filler';
		INSERT INTO file_lsh (band, bucket, file_id)
		SELECT band, hashtextextended(s.file_id::text, band), s.file_id
		FROM file_signatures s, generate_series(0, 7) band
		WHERE s.shingles = 10000;
		ANALYZE file_lsh; ANALYZE file_signatures; ANALYZE books`); err != nil {
		t.Fatal(err)
	}
	middle := slices.IndexFunc(pairs, func(p pairView) bool { return p.Books[0].Title == "Middle" || p.Books[1].Title == "Middle" })
	var middleID uuid.UUID
	for _, b := range pairs[middle].Books {
		if b.Title == "Middle" {
			middleID = uuid.MustParse(b.ID)
		}
	}
	best := time.Hour
	for range 3 {
		started := time.Now()
		if err := a.server.Duplicates.Check(ctx, middleID); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(started))
	}
	if best > 100*time.Millisecond {
		t.Errorf("checking a book among 50,000 signatures took %s", best)
	}
	if d := overlapOf(a.duplicates(admin, "?limit=100"), "Middle", "Trilogy"); d != detail {
		t.Errorf("after the fixture: %q, want %q", d, detail)
	}
}
