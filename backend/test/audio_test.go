//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestAnAudiobookInPartsIsOneTimeline(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	book, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: "Emma"})
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string, part *int32, durationMS any) uuid.UUID {
		t.Helper()
		id, err := books.AddFile(ctx, book, lib, catalog.NewFile{RelPath: "Emma/" + name, Format: "mp3", Size: 1, PartIndex: part})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.pool.Exec(ctx, `UPDATE book_files SET duration_ms = $2 WHERE id = $1`, id, durationMS); err != nil {
			t.Fatal(err)
		}
		return id
	}
	one, two, three := int32(0), int32(1), int32(2)
	// Listed out of order, the second part first.
	second := add("Emma - Part 2.mp3", &two, 50_000)
	first := add("Emma - Part 1.mp3", &one, 60_000)
	unread := add("Emma - Part 3.mp3", &three, nil)
	for i, c := range []struct {
		title      string
		start, end int64
	}{{"Volume I", 0, 30_000}, {"Volume II", 30_000, 60_000}} {
		if _, err := a.pool.Exec(ctx, `INSERT INTO audio_chapters (file_id, position, title, start_ms, end_ms) VALUES ($1, $2, $3, $4, $5)`,
			first, i, c.title, c.start, c.end); err != nil {
			t.Fatal(err)
		}
	}

	status, body, _ := a.call(reader, http.MethodGet, "/books/"+book.String()+"/audio", nil)
	if status != 200 {
		t.Fatalf("audio: %d %v", status, body)
	}
	if body["durationMs"].(float64) != 110_000 || body["complete"] != false {
		t.Errorf("the whole: %v", body)
	}
	parts := body["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts %v", parts)
	}
	p1, p2 := parts[0].(map[string]any), parts[1].(map[string]any)
	if p1["fileId"] != first.String() || p1["startMs"].(float64) != 0 || p2["fileId"] != second.String() ||
		p2["startMs"].(float64) != 60_000 || p2["mediaType"] != "audio/mpeg" {
		t.Errorf("parts %v", parts)
	}
	var titles []string
	var starts []float64
	for _, c := range body["chapters"].([]any) {
		titles = append(titles, c.(map[string]any)["title"].(string))
		starts = append(starts, c.(map[string]any)["startMs"].(float64))
	}
	if len(titles) != 3 || titles[0] != "Volume I" || titles[2] != "Emma - Part 2" || starts[1] != 30_000 || starts[2] != 60_000 {
		t.Errorf("chapters %v at %v", titles, starts)
	}

	if _, err := a.pool.Exec(ctx, `UPDATE book_files SET duration_ms = 10000 WHERE id = $1`, unread); err != nil {
		t.Fatal(err)
	}
	if _, body, _ := a.call(reader, http.MethodGet, "/books/"+book.String()+"/audio", nil); body["complete"] != true || body["durationMs"].(float64) != 120_000 {
		t.Errorf("once every part is read: %v", body)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+uuid.NewString()+"/audio", nil); status != 404 {
		t.Errorf("an unknown book: %d", status)
	}
}
