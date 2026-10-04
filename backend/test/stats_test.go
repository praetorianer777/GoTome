//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestReadingStatisticsComeFromSessionsAndFinishes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := a.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	newBook := func(title string, pages *int32) uuid.UUID {
		t.Helper()
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: title, PageCount: pages})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	two := int32(200)
	emma, persuasion, audio := newBook("Emma", nil), newBook("Persuasion", &two), newBook("Emma, read aloud", nil)
	// Emma's pages come from its EPUB, worked out from its text.
	file, err := books.AddFile(ctx, emma, lib, catalog.NewFile{RelPath: "Emma.epub", Format: "epub", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE book_files SET page_count = 300, pages_estimated = true WHERE id = $1`, file)
	exec(`UPDATE books SET primary_text_file_id = $2 WHERE id = $1`, emma, file)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	session := func(book uuid.UUID, medium string, start time.Time, minutes int, from, to float64) {
		t.Helper()
		exec(`INSERT INTO reading_sessions (user_id, book_id, medium, client_id, started_at, ended_at, from_fraction, to_fraction)
			VALUES ($1, $2, $3, 'phone', $4, $5, $6, $7)`,
			readerID, book, medium, start, start.Add(time.Duration(minutes)*time.Minute), from, to)
	}
	session(emma, "ebook", today.Add(time.Minute), 20, 0.1, 0.2)       // 30 pages
	session(persuasion, "ebook", today.AddDate(0, 0, -1), 40, 0, 0.5)  // 100 pages
	session(audio, "audio", today.Add(2*time.Minute), 30, 0.2, 0.4)    // listened
	session(emma, "ebook", today.AddDate(0, 0, -40), 10, 0.2, 0.3)     // before the window
	session(persuasion, "ebook", today.AddDate(0, 0, -2), 5, 0.5, 0.4) // went back: no pages

	finish := func(book uuid.UUID, at string) {
		t.Helper()
		exec(`INSERT INTO reading_finishes (user_id, book_id, medium, finished_at) VALUES ($1, $2, 'ebook', $3)`, readerID, book, at)
	}
	finish(emma, "2025-03-01T10:00:00Z")
	finish(emma, "2025-11-01T10:00:00Z") // read again
	finish(persuasion, "2026-02-01T10:00:00Z")
	exec(`INSERT INTO user_books (user_id, book_id, status, started_on) VALUES ($1, $2, 'completed', '2025-02-20')`, readerID, emma)

	status, body, _ := a.call(reader, http.MethodGet, "/me/stats?days=30&tz=UTC", nil)
	if status != 200 {
		t.Fatalf("stats: %d %v", status, body)
	}
	days := body["days"].([]any)
	if len(days) != 30 {
		t.Fatalf("%d days", len(days))
	}
	last, before := days[29].(map[string]any), days[28].(map[string]any)
	if last["date"] != today.Format(time.DateOnly) || last["pages"].(float64) != 30 || last["readingMinutes"].(float64) != 20 ||
		last["listeningMinutes"].(float64) != 30 {
		t.Errorf("today: %v", last)
	}
	if before["pages"].(float64) != 100 || before["readingMinutes"].(float64) != 40 {
		t.Errorf("yesterday: %v", before)
	}
	want := map[string]float64{
		"pagesPerDay": 4.3, "readingMinutesPerDay": 2.2, "listeningMinutesPerDay": 1,
		"totalPages": 160, "totalReadingMinutes": 75, "totalListeningMinutes": 30,
	}
	for k, v := range want {
		if body[k].(float64) != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	if body["pagesEstimated"] != true {
		t.Error("pages from an EPUB are not said to be estimated")
	}
	years := body["years"].([]any)
	if len(years) != 2 {
		t.Fatalf("years %v", years)
	}
	if y := years[0].(map[string]any); y["year"].(float64) != 2026 || y["finishes"].(float64) != 1 {
		t.Errorf("2026: %v", y)
	}
	if y := years[1].(map[string]any); y["year"].(float64) != 2025 || y["finishes"].(float64) != 2 || y["books"].(float64) != 1 {
		t.Errorf("2025, with a re-read: %v", y)
	}
	history := body["history"].([]any)
	var events []string
	for _, h := range history {
		e := h.(map[string]any)
		events = append(events, e["date"].(string)+" "+e["event"].(string)+" "+e["title"].(string))
	}
	wantEvents := []string{"2026-02-01 finished Persuasion", "2025-11-01 finished Emma", "2025-03-01 finished Emma", "2025-02-20 started Emma"}
	if len(events) != len(wantEvents) {
		t.Fatalf("history %v", events)
	}
	for i := range events {
		if events[i] != wantEvents[i] {
			t.Errorf("history %v, want %v", events, wantEvents)
			break
		}
	}

	// Another person's statistics are their own, and empty.
	_, body, _ = a.call(admin, http.MethodGet, "/me/stats", nil)
	if body["totalPages"].(float64) != 0 || len(body["years"].([]any)) != 0 || len(body["history"].([]any)) != 0 {
		t.Errorf("the administrator sees %v", body)
	}
	for _, q := range []string{"days=400", "tz=Mars/Olympus", "tz=Local"} {
		if status, _, _ := a.call(reader, http.MethodGet, "/me/stats?"+q, nil); status != 422 {
			t.Errorf("%s: %d", q, status)
		}
	}
}
