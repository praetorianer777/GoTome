//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestProgressIsPickedUpElsewhereAndAStaleClientDoesNotOverwriteIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	emma, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Emma"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Persuasion"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := books.AddFile(context.Background(), other, lib, catalog.NewFile{RelPath: "p.epub", Format: "epub", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	path := "/books/" + emma.String() + "/progress"
	save := func(medium string, body map[string]any) (bool, map[string]any) {
		t.Helper()
		status, out, _ := a.call(reader, http.MethodPut, path+"/"+medium, body)
		if status != 200 {
			t.Fatalf("save %v: %d %v", body, status, out)
		}
		return out["saved"].(bool), out["progress"].(map[string]any)
	}

	// The phone reads to a third and to 40 %.
	saved, first := save("ebook", map[string]any{"locator": "epubcfi(/6/4!/4/2)", "fraction": 0.3, "chapter": "Volume I", "clientId": "phone"})
	if !saved || first["locator"] != "epubcfi(/6/4!/4/2)" {
		t.Fatalf("the first position: %v", first)
	}
	_, latest := save("ebook", map[string]any{"locator": "epubcfi(/6/8!/4/2)", "fraction": 0.4, "clientId": "phone", "basedOn": first["updatedAt"]})

	// The laptop opens the book and finds the phone's place.
	_, state, _ := a.call(reader, http.MethodGet, path, nil)
	ebook := state["ebook"].(map[string]any)
	if ebook["locator"] != "epubcfi(/6/8!/4/2)" || ebook["fraction"].(float64) != 0.4 || ebook["clientId"] != "phone" || state["audio"] != nil {
		t.Errorf("the laptop is told %v", state)
	}
	_, book, _ := a.call(reader, http.MethodGet, "/books/"+emma.String(), nil)
	if r := book["reading"].(map[string]any); r["status"] != "reading" || r["startedOn"] == nil {
		t.Errorf("after the first position: %v", r)
	}

	// A tablet that never looked writes an earlier place: not taken, and
	// told of the further one.
	saved, further := save("ebook", map[string]any{"locator": "epubcfi(/6/2)", "fraction": 0.1, "clientId": "tablet"})
	if saved || further["locator"] != "epubcfi(/6/8!/4/2)" {
		t.Errorf("a stale tablet: saved %v, %v", saved, further)
	}
	// Nor when it read the phone's first place, but not its latest.
	if saved, _ := save("ebook", map[string]any{"locator": "epubcfi(/6/2)", "fraction": 0.35, "clientId": "tablet", "basedOn": first["updatedAt"]}); saved {
		t.Error("a tablet behind the latest position overwrote it")
	}
	// Once it has seen the latest, going back is the person's choice.
	if saved, _ := save("ebook", map[string]any{"locator": "epubcfi(/6/2)", "fraction": 0.1, "clientId": "tablet", "basedOn": latest["updatedAt"]}); !saved {
		t.Error("going back after seeing the latest was refused")
	}
	// The phone, which has not seen that, may still go on further; and the
	// same client going back is not stale.
	if saved, _ := save("ebook", map[string]any{"locator": "epubcfi(/6/10)", "fraction": 0.5, "clientId": "phone"}); !saved {
		t.Error("a further position was refused")
	}
	if saved, _ := save("ebook", map[string]any{"locator": "epubcfi(/6/9)", "fraction": 0.45, "clientId": "phone"}); !saved {
		t.Error("the same client going back was refused")
	}
	if saved, _ := save("ebook", map[string]any{"locator": "epubcfi(/6/2)", "fraction": 0.1, "clientId": "tablet", "force": true}); !saved {
		t.Error("a forced position was refused")
	}

	// Audio is a place of its own.
	if saved, p := save("audio", map[string]any{"locator": "3723000", "positionMs": 3723000, "fraction": 0.8, "clientId": "car"}); !saved || p["positionMs"].(float64) != 3723000 {
		t.Errorf("audio: %v", p)
	}
	_, state, _ = a.call(reader, http.MethodGet, path, nil)
	if state["ebook"].(map[string]any)["fraction"].(float64) != 0.1 || state["audio"].(map[string]any)["fraction"].(float64) != 0.8 {
		t.Errorf("ebook and audio: %v", state)
	}
	// The administrator has no place in the reader's book.
	if _, state, _ := a.call(admin, http.MethodGet, path, nil); state["ebook"] != nil || state["audio"] != nil {
		t.Errorf("another person's progress: %v", state)
	}

	// The end makes a finish, and reading it again another.
	save("ebook", map[string]any{"locator": "end", "fraction": 1, "clientId": "phone", "force": true})
	_, book, _ = a.call(reader, http.MethodGet, "/books/"+emma.String(), nil)
	if r := book["reading"].(map[string]any); r["status"] != "completed" || r["finishedOn"] == nil {
		t.Errorf("after the end: %v", r)
	}
	save("ebook", map[string]any{"locator": "end", "fraction": 1, "clientId": "phone"})
	save("ebook", map[string]any{"locator": "start", "fraction": 0.02, "clientId": "phone"})
	save("ebook", map[string]any{"locator": "end", "fraction": 0.995, "clientId": "phone"})
	if _, state, _ = a.call(reader, http.MethodGet, path, nil); state["finishes"].(float64) != 2 {
		t.Errorf("finishes: %v", state["finishes"])
	}

	var sessions int
	if err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM reading_sessions WHERE book_id = $1 AND medium = 'ebook'`, emma).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Errorf("%d ebook sessions, want one each for the phone and the tablet", sessions)
	}
	if _, err := a.pool.Exec(context.Background(), `UPDATE reading_sessions SET ended_at = ended_at - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	save("ebook", map[string]any{"locator": "x", "fraction": 0.5, "clientId": "phone"})
	if err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM reading_sessions WHERE book_id = $1 AND medium = 'ebook'`, emma).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 3 {
		t.Errorf("%d sessions after a pause, want 3", sessions)
	}

	for name, body := range map[string]map[string]any{
		"no locator":     {"locator": "", "fraction": 0.1, "clientId": "phone"},
		"past the end":   {"locator": "x", "fraction": 1.5, "clientId": "phone"},
		"no client":      {"locator": "x", "fraction": 0.1},
		"another's file": {"locator": "x", "fraction": 0.1, "clientId": "phone", "fileId": file},
	} {
		if status, out, _ := a.call(reader, http.MethodPut, path+"/ebook", body); status != 422 {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	if status, _, _ := a.call(reader, http.MethodPut, path+"/video", map[string]any{"locator": "x", "fraction": 0.1, "clientId": "phone"}); status != 404 {
		t.Errorf("an unknown medium: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/books/"+uuid.NewString()+"/progress", nil); status != 404 {
		t.Errorf("an unknown book: %d", status)
	}
}
