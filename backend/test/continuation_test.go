//go:build integration

package test

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

func TestTheOtherMediumIsOfferedAtTheMatchingChapter(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	folder := t.TempDir()
	path := filepath.Join(folder, "Emma.epub")
	chapter := func(title string, paragraphs int) string {
		return xhtml(title, "<h1>"+title+"</h1>"+strings.Repeat("<p>Emma Woodhouse, handsome, clever, and rich.</p>", paragraphs))
	}
	writeEPUBParts(t, path, "Emma",
		epubPart{id: "nav", name: "nav.xhtml", mediaType: "application/xhtml+xml", properties: "nav", content: xhtml("Contents",
			`<nav epub:type="toc" xmlns:epub="http://www.idpf.org/2007/ops"><ol><li><a href="c1.xhtml">Chapter 1</a></li>
<li><a href="c2.xhtml">Chapter 2</a></li><li><a href="c3.xhtml">Chapter 3</a></li></ol></nav>`)},
		epubPart{id: "c1", name: "c1.xhtml", mediaType: "application/xhtml+xml", read: true, content: chapter("Chapter 1", 10)},
		epubPart{id: "c2", name: "c2.xhtml", mediaType: "application/xhtml+xml", read: true, content: chapter("Chapter 2", 30)},
		epubPart{id: "c3", name: "c3.xhtml", mediaType: "application/xhtml+xml", read: true, content: chapter("Chapter 3", 60)},
	)
	lib := a.externalLibrary(admin, "Shelf", "shared", folder)
	a.scanNow(lib)
	file := a.shelfFiles()["Emma.epub"]
	book := file.BookID

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outline, err := epub.Outline(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(outline) != 3 {
		t.Fatalf("outline %v, %v", outline, err)
	}

	continuation := func() map[string]any {
		t.Helper()
		status, body, _ := a.call(reader, http.MethodGet, "/books/"+book.String()+"/continuation", nil)
		if status != 200 {
			t.Fatalf("continuation: %d %v", status, body)
		}
		return body
	}
	save := func(medium string, body map[string]any) {
		t.Helper()
		body["clientId"] = "phone"
		body["locator"] = "x"
		if status, out, _ := a.call(reader, http.MethodPut, "/books/"+book.String()+"/progress/"+medium, body); status != 200 {
			t.Fatalf("save: %d %v", status, out)
		}
	}

	save("ebook", map[string]any{"fraction": 0.5})
	if got := continuation(); got["audio"] != nil || got["ebook"] != nil {
		t.Errorf("a book with no audiobook is offered %v", got)
	}

	ctx := context.Background()
	books := catalog.NewService(a.pool)
	part := int32(0)
	audio, err := books.AddFile(ctx, book, uuid.MustParse(lib), catalog.NewFile{RelPath: "Emma/Emma.mp3", Format: "mp3", Size: 1, PartIndex: &part})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET duration_ms = 100000 WHERE id = $1`, audio); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct {
		title      string
		start, end int64
	}{{"Opening credits", 0, 10_000}, {"Chapter 1", 10_000, 30_000}, {"Chapter 2", 30_000, 70_000}, {"Chapter 3", 70_000, 100_000}} {
		if _, err := a.pool.Exec(ctx, `INSERT INTO audio_chapters (file_id, position, title, start_ms, end_ms) VALUES ($1, $2, $3, $4, $5)`,
			audio, i, c.title, c.start, c.end); err != nil {
			t.Fatal(err)
		}
	}

	// Read to 50 % of the text, nothing heard: the audiobook is offered.
	got := continuation()
	offer, _ := got["audio"].(map[string]any)
	if offer == nil || got["ebook"] != nil || offer["refined"] != true || offer["positionMs"].(float64) <= 0 {
		t.Fatalf("after reading: %v", got)
	}

	// Back at the start of the text, and 50 s into the recording, the middle
	// of its chapter 2: the text is behind, and is offered the middle of its
	// own chapter 2.
	save("ebook", map[string]any{"fraction": 0.05})
	save("audio", map[string]any{"fraction": 0.5, "positionMs": 50_000})
	got = continuation()
	offer, _ = got["ebook"].(map[string]any)
	middle := (outline[1].Start + outline[2].Start) / 2
	if got["audio"] != nil || offer == nil || offer["chapter"] != "Chapter 2" || offer["refined"] != true ||
		offer["fileId"] != file.ID.String() || math.Abs(offer["fraction"].(float64)-middle) > 1e-6 {
		t.Fatalf("after listening, want chapter 2 at %.4f: %v", middle, got)
	}

	// Reading on to the end of chapter 3 turns it round.
	save("ebook", map[string]any{"fraction": 0.9})
	got = continuation()
	offer, _ = got["audio"].(map[string]any)
	if got["ebook"] != nil || offer == nil || offer["chapter"] != "Chapter 3" || offer["positionMs"].(float64) <= 70_000 {
		t.Errorf("after reading on: %v", got)
	}

	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+uuid.NewString()+"/continuation", nil); status != 404 {
		t.Errorf("an unknown book: %d", status)
	}
}
