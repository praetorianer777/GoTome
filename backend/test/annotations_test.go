//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// putDevice sends a device's annotations of a document as the KOReader
// plugin does: Basic auth with the user name and the device token.
func (a *app) putDevice(user, token, document string, body any) (int, map[string]any) {
	a.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, a.url+"/api/v1/device/annotations/"+document, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func annotationsOf(t *testing.T, a *app, c *http.Client, query string) []map[string]any {
	t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/annotations"+query, nil)
	if status != 200 {
		t.Fatalf("list annotations: %d %v", status, out)
	}
	var list []map[string]any
	for _, x := range out["annotations"].([]any) {
		list = append(list, x.(map[string]any))
	}
	return list
}

func TestKOReaderHighlightsSyncOnceEditAndDisappearWithTheDevice(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	shared := a.library(admin, "Shared", "shared")
	vault := a.library(admin, "Vault", "private")
	epubBytes := []byte("PK\x03\x04 an epub of sorts")
	_, epub := a.upload(admin, shared, "Emma.epub", epubBytes)
	_, secret := a.upload(admin, vault, "Secret.epub", []byte("PK\x03\x04 a secret"))
	if _, err := a.server.Sync.HashFiles(ctx); err != nil {
		t.Fatal(err)
	}
	token := func(c *http.Client) string {
		_, made, _ := a.call(c, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
		return made["token"].(string)
	}
	readerToken, strangerToken := token(reader), token(stranger)
	document := md5Of(epubBytes)
	book := epub["bookId"].(string)

	first := map[string]any{
		"key":  "2026-03-01 10:00:00|/body/DocFragment[3]/body/p[2]/text().4|/body/DocFragment[3]/body/p[2]/text().40",
		"kind": "xpointer", "locator": "/body/DocFragment[3]/body/p[2]/text().4", "locatorEnd": "/body/DocFragment[3]/body/p[2]/text().40",
		"passage": "It is a truth universally acknowledged", "color": "olive", "chapter": "Chapter 1", "fraction": 0.2,
		"createdAt": "2026-03-01 10:00:00",
	}
	second := map[string]any{
		"key":  "2026-03-02 09:30:00|/body/DocFragment[5]/body/p[1]/text().0|/body/DocFragment[5]/body/p[1]/text().20",
		"kind": "xpointer", "locator": "/body/DocFragment[5]/body/p[1]/text().0", "passage": "Vanity and pride are different things",
		"note": "Compare with the first chapter", "color": "red", "createdAt": "2026-03-02T09:30:00Z",
	}
	send := func(token string, items ...map[string]any) (int, map[string]any) {
		return a.putDevice("reader", token, document, map[string]any{"clientId": "koreader:TOLINO1", "annotations": items})
	}
	if status, out := send(readerToken, first, second); status != 200 || out["kept"] != 2.0 || out["removed"] != 0.0 {
		t.Fatalf("first send: %d %v", status, out)
	}
	if status, out := send(readerToken, first, second); status != 200 || out["kept"] != 2.0 || out["removed"] != 0.0 {
		t.Fatalf("the same again: %d %v", status, out)
	}
	list := annotationsOf(t, a, reader, "?book="+book)
	if len(list) != 2 {
		t.Fatalf("sent twice, kept %d: %v", len(list), list)
	}
	byPassage := map[string]map[string]any{}
	for _, x := range list {
		byPassage[x["passage"].(string)] = x
	}
	one := byPassage["It is a truth universally acknowledged"]
	if one["color"] != "green" || one["clientId"] != "koreader:TOLINO1" || one["kind"] != "xpointer" || one["bookTitle"] == nil {
		t.Errorf("as the list says: %v", one)
	}

	// A note changed on the device follows; one changed here stays.
	second["note"] = "Compare with chapter one"
	send(readerToken, first, second)
	if got := annotationsOf(t, a, reader, "?q=chapter+one"); len(got) != 1 || got[0]["note"] != "Compare with chapter one" {
		t.Errorf("an edit on the device: %v", got)
	}
	id := one["id"].(string)
	if status, out, _ := a.call(reader, http.MethodPatch, "/annotations/"+id, map[string]any{"note": "Mine", "color": "blue"}); status != 200 || out["note"] != "Mine" {
		t.Fatalf("edit here: %d %v", status, out)
	}
	first["note"], first["color"] = "Device note", "yellow"
	send(readerToken, first, second)
	if got := annotationsOf(t, a, reader, "?q=universally")[0]; got["note"] != "Mine" || got["color"] != "blue" {
		t.Errorf("a device's send over what was changed here: %v", got)
	}

	// Another client's annotation is not the device's to remove.
	if status, out, _ := a.call(reader, http.MethodPost, "/books/"+book+"/annotations", map[string]any{
		"kind": "cfi", "locator": "epubcfi(/6/4!/4/2/1:0)", "passage": "Made in the browser", "fileId": epub["fileId"],
	}); status != 201 {
		t.Fatalf("create: %d %v", status, out)
	}
	if status, out := send(readerToken, first); status != 200 || out["removed"] != 1.0 {
		t.Fatalf("a send without the second: %d %v", status, out)
	}
	if got := annotationsOf(t, a, reader, "?book="+book); len(got) != 2 {
		t.Errorf("after the deletion: %v", got)
	}
	if status, out := send(readerToken); status != 200 || out["removed"] != 1.0 {
		t.Fatalf("an empty send: %d %v", status, out)
	}
	if got := annotationsOf(t, a, reader, "?book="+book); len(got) != 1 || got[0]["clientId"] != "web" {
		t.Errorf("only the browser's is left: %v", got)
	}

	// Refusals: unknown or unseen documents, bad shapes, no client.
	for name, d := range map[string]string{"unknown": md5Of([]byte("nothing")), "a private library's": md5Of([]byte("PK\x03\x04 a secret"))} {
		if status, _ := a.putDevice("reader", readerToken, d, map[string]any{"clientId": "koreader:T", "annotations": []any{first}}); status != 404 {
			t.Errorf("%s document: %d", name, status)
		}
	}
	if status, _ := a.putDevice("reader", readerToken, document, map[string]any{"clientId": "web", "annotations": []any{first}}); status != 422 {
		t.Errorf("a device named web: %d", status)
	}
	bad := map[string]any{"key": "k", "kind": "xpointer", "locator": "/body", "passage": " "}
	if status, _ := send(readerToken, bad); status != 422 {
		t.Errorf("an empty passage: %d", status)
	}
	if status, _ := a.putDevice("reader", "wrong-token", document, map[string]any{"clientId": "koreader:T"}); status != 401 {
		t.Errorf("a wrong token: %d", status)
	}

	// Privacy: nobody else sees or changes them.
	send(readerToken, first)
	if got := annotationsOf(t, a, stranger, ""); len(got) != 0 {
		t.Errorf("the stranger sees: %v", got)
	}
	if status, _ := a.putDevice("stranger", strangerToken, document, map[string]any{"clientId": "koreader:S", "annotations": []any{first}}); status != 200 {
		t.Errorf("the stranger sends the same book: %d", status)
	}
	if got := annotationsOf(t, a, stranger, ""); len(got) != 1 {
		t.Errorf("the stranger's own: %v", got)
	}
	if got := annotationsOf(t, a, reader, "?q=universally"); len(got) != 1 {
		t.Errorf("the reader's, still one: %v", got)
	}
	if status, _, _ := a.call(stranger, http.MethodPatch, "/annotations/"+id, map[string]any{"note": "x"}); status != 404 {
		t.Errorf("change another's: %d", status)
	}
	if status, _, _ := a.call(stranger, http.MethodDelete, "/annotations/"+id, nil); status != 404 {
		t.Errorf("delete another's: %d", status)
	}
	if status, _, _ := a.call(stranger, http.MethodPost, "/books/"+secret["bookId"].(string)+"/annotations", map[string]any{
		"kind": "cfi", "locator": "epubcfi(/6/4)", "passage": "Out of reach",
	}); status != 404 {
		t.Errorf("highlight in a private library: %d", status)
	}

	// A device's token reaches nothing else of this.
	req, _ := http.NewRequest(http.MethodGet, a.url+"/api/v1/annotations", nil)
	req.SetBasicAuth("reader", readerToken)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 401 && resp.StatusCode != 403 {
		t.Errorf("a device lists annotations: %v %v", resp, err)
	}
}

func TestAFormerMemberLosesTheirHighlightsFromView(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	member, memberID := a.signedIn("member", "reader")
	vault := a.library(admin, "Vault", "private")
	a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+memberID, nil)
	_, up := a.upload(admin, vault, "Vault.epub", []byte("PK\x03\x04 vault"))
	book := up["bookId"].(string)
	if status, out, _ := a.call(member, http.MethodPost, "/books/"+book+"/annotations", map[string]any{
		"kind": "cfi", "locator": "epubcfi(/6/4)", "passage": "Kept for the member",
	}); status != 201 {
		t.Fatalf("create: %d %v", status, out)
	}
	if got := annotationsOf(t, a, member, ""); len(got) != 1 {
		t.Fatalf("a member: %v", got)
	}
	a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+memberID, nil)
	if got := annotationsOf(t, a, member, ""); len(got) != 0 {
		t.Errorf("a former member sees: %v", got)
	}
	for _, path := range []string{"/annotations/export.md", "/annotations/export.csv"} {
		_, body := a.get(member, path)
		if strings.Contains(string(body), "Kept for the member") {
			t.Errorf("%s gives a former member: %s", path, body)
		}
	}
	a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+memberID, nil)
	if got := annotationsOf(t, a, member, ""); len(got) != 1 {
		t.Errorf("a member again: %v", got)
	}
}

func TestAnnotationsAreSearchedPagedExportedAndMerged(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	lib := a.library(admin, "Novels", "shared")
	a.call(admin, http.MethodPut, "/libraries/"+lib+"/members/"+readerID, nil)
	upload := func(name string) string {
		_, up := a.upload(admin, lib, name, []byte("%PDF-1.4 "+name))
		return up["bookId"].(string)
	}
	keep, gone := upload("Emma.pdf"), upload("Emma Novel.pdf")
	for book, title := range map[string]string{keep: "Emma", gone: "Persuasion"} {
		a.call(admin, http.MethodPatch, "/books/"+book, map[string]any{
			"title": title, "contributors": []map[string]string{{"name": "Jane Austen", "role": "author"}},
		})
	}
	highlight := func(book, passage, note string, page int, y float64) {
		t.Helper()
		if status, out, _ := a.call(reader, http.MethodPost, "/books/"+book+"/annotations", map[string]any{
			"kind": "pdf", "locator": "page:" + string(rune('0'+page)), "page": page, "passage": passage, "note": note,
			"rects": []map[string]float64{{"x": 0.1, "y": y, "w": 0.5, "h": 0.02}}, "fraction": y,
		}); status != 201 {
			t.Fatalf("create: %d %v", status, out)
		}
	}
	for i, p := range []string{"first", "second", "third"} {
		highlight(keep, "A passage "+p, "", 2, 0.1*float64(i+1))
	}
	highlight(gone, "=cmd|' /C calc'!A0 and a, comma", "A \"quoted\" note", 4, 0.5)

	if status, out, _ := a.call(reader, http.MethodPost, "/books/"+keep+"/annotations", map[string]any{
		"kind": "pdf", "locator": "page:2", "passage": "No page",
	}); status != 422 {
		t.Errorf("a PDF highlight without a page: %d %v", status, out)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/books/"+keep+"/annotations", map[string]any{
		"kind": "cfi", "locator": "x", "passage": "p", "color": "mauve",
	}); status != 422 {
		t.Errorf("a colour: %d", status)
	}

	status, page, _ := a.call(reader, http.MethodGet, "/annotations?limit=3", nil)
	if status != 200 || len(page["annotations"].([]any)) != 3 || page["next"] == nil {
		t.Fatalf("first page: %d %v", status, page)
	}
	_, rest, _ := a.call(reader, http.MethodGet, "/annotations?limit=3&after="+url.QueryEscape(page["next"].(string)), nil)
	if len(rest["annotations"].([]any)) != 1 || rest["next"] != nil {
		t.Errorf("second page: %v", rest)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/annotations?after=junk", nil); status != 422 {
		t.Errorf("a junk cursor: %d", status)
	}
	if got := annotationsOf(t, a, reader, "?q=persuasion+quoted"); len(got) != 1 {
		t.Errorf("search by title and note: %v", got)
	}
	if got := annotationsOf(t, a, reader, "?q=austen+second"); len(got) != 1 {
		t.Errorf("search by author and passage: %v", got)
	}
	if got := annotationsOf(t, a, reader, "?q=nothing+like+it"); len(got) != 0 {
		t.Errorf("search for nothing: %v", got)
	}

	status, md := a.get(reader, "/annotations/export.md?book="+keep)
	if status != 200 || !strings.Contains(string(md), "## Emma") || strings.Contains(string(md), "Persuasion") ||
		strings.Index(string(md), "first") > strings.Index(string(md), "third") {
		t.Errorf("markdown of one book: %d %s", status, md)
	}
	status, csvBody := a.get(reader, "/annotations/export.csv")
	if status != 200 || !strings.HasPrefix(string(csvBody), "Title,Authors,") || !strings.Contains(string(csvBody), "'=cmd|") ||
		!strings.Contains(string(csvBody), `"A ""quoted"" note"`) {
		t.Errorf("csv: %d %s", status, csvBody)
	}
	if status, _ := a.get(reader, "/annotations/export.csv?book=junk"); status != 422 {
		t.Errorf("csv of a junk book: %d", status)
	}

	// Merging moves them to the book that is kept.
	if status, out, _ := a.call(admin, http.MethodPost, "/books/"+keep+"/merge", map[string]any{"from": gone, "take": []string{}}); status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	if got := annotationsOf(t, a, reader, "?book="+keep); len(got) != 4 {
		t.Errorf("after the merge: %d", len(got))
	}

	// Undoing the merge gives the merged book its highlights back.
	var action string
	if err := a.pool.QueryRow(context.Background(), `SELECT id::text FROM curation_actions ORDER BY id DESC LIMIT 1`).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/curation/actions/"+action+"/undo", nil); status != 200 {
		t.Fatalf("undo: %d %v", status, out)
	}
	if got := annotationsOf(t, a, reader, "?book="+keep); len(got) != 3 {
		t.Errorf("after the undo, the kept book has %d", len(got))
	}
	if got := annotationsOf(t, a, reader, "?book="+gone); len(got) != 1 {
		t.Errorf("after the undo, the other book has %d", len(got))
	}
}
