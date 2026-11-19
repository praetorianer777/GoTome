//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type koboTagEntry struct {
	Tag struct {
		ID    string `json:"Id"`
		Name  string
		Items []struct{ RevisionId, Type string }
	}
}

type koboTagItems struct {
	NewTag, ChangedTag, DeletedTag *koboTagEntry
}

func (e koboTagEntry) books() []string {
	var out []string
	for _, it := range e.Tag.Items {
		out = append(out, it.RevisionId)
	}
	return out
}

func TestCollectionsGoToAKoboReaderAndBack(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	other, otherID := a.signedIn("other", "editor")
	vault := a.library(admin, "Vault", "private")
	for _, id := range []string{readerID, otherID} {
		if status, _, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+id, nil); status != 204 {
			t.Fatalf("membership: %d", status)
		}
	}
	epub := func(title string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), title+".epub")
		writeEPUB(t, path, emmaMetadata, nil, "Chapter one of "+title+".")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		status, up := a.upload(admin, vault, title+".epub", content)
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		return up["bookId"].(string)
	}
	emma, persuasion, mansfield := epub("Emma"), epub("Persuasion"), epub("Mansfield")
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := made["device"].(map[string]any)["id"].(string)
	token := made["token"].(string)
	if status, out, _ := a.call(reader, http.MethodPost, "/devices/"+device+"/books", map[string]any{"books": []string{emma, persuasion}}); status != 200 || out["queued"] != 2.0 {
		t.Fatalf("send: %d %v", status, out)
	}
	synctoken := ""
	sync := func() (tags []koboTagItems) {
		t.Helper()
		status, raw, h := a.kobo(token, http.MethodGet, "/v1/library/sync", synctoken, nil)
		if status != 200 {
			t.Fatalf("sync: %d %s", status, raw)
		}
		synctoken = h.Get("x-kobo-synctoken")
		if err := json.Unmarshal(raw, &tags); err != nil {
			t.Fatal(err)
		}
		var kept []koboTagItems
		for _, it := range tags {
			if it.NewTag != nil || it.ChangedTag != nil || it.DeletedTag != nil {
				kept = append(kept, it)
			}
		}
		return kept
	}
	if tags := sync(); len(tags) != 0 {
		t.Fatalf("tags with no collection: %+v", tags)
	}

	// A collection of the person's goes across with the books on the device.
	_, made, _ = a.call(reader, http.MethodPost, "/collections", map[string]any{"name": "Austen"})
	mine := made["id"].(string)
	for _, b := range []string{emma, mansfield} {
		if status, out, _ := a.call(reader, http.MethodPost, "/collections/"+mine+"/books", map[string]any{"books": []string{b}}); status != 200 {
			t.Fatalf("add: %d %v", status, out)
		}
	}
	tags := sync()
	if len(tags) != 1 || tags[0].NewTag == nil || tags[0].NewTag.Tag.ID != mine || tags[0].NewTag.Tag.Name != "Austen" ||
		strings.Join(tags[0].NewTag.books(), ",") != emma || tags[0].NewTag.Tag.Items[0].Type != "ProductRevisionTagItem" {
		t.Fatalf("the collection on the reader: %s", mustJSON(tags))
	}
	if tags := sync(); len(tags) != 0 {
		t.Fatalf("told twice: %s", mustJSON(tags))
	}
	status, raw, _ := a.kobo(token, http.MethodGet, "/v1/library/tags", "", nil)
	if status != 200 || !strings.Contains(string(raw), `"Name":"Austen"`) || !strings.Contains(string(raw), `"Type":"UserTag"`) {
		t.Errorf("the tag list: %d %s", status, raw)
	}

	// Changes in GOtome arrive as changes, a deletion as a deletion.
	a.call(reader, http.MethodPost, "/collections/"+mine+"/books", map[string]any{"books": []string{persuasion}})
	a.call(reader, http.MethodPut, "/collections/"+mine, map[string]any{"name": "Jane Austen"})
	tags = sync()
	if len(tags) != 1 || tags[0].ChangedTag == nil || tags[0].ChangedTag.Tag.Name != "Jane Austen" || len(tags[0].ChangedTag.books()) != 2 {
		t.Fatalf("a changed collection: %s", mustJSON(tags))
	}

	// What the reader sends changes the collection.
	status, raw, _ = a.kobo(token, http.MethodPost, "/v1/library/tags", "", map[string]any{
		"Name": "From the reader", "Items": []map[string]any{{"RevisionId": persuasion, "Type": "ProductRevisionTagItem"}},
	})
	var made2 string
	if status != 201 || json.Unmarshal(raw, &made2) != nil || made2 == "" {
		t.Fatalf("create: %d %s", status, raw)
	}
	_, got, _ := a.call(reader, http.MethodGet, "/collections/"+made2, nil)
	if got["name"] != "From the reader" || got["visibility"] != "private" || got["books"].(float64) != 1 {
		t.Errorf("the collection the reader made: %v", got)
	}
	if tags := sync(); len(tags) != 0 {
		t.Errorf("the reader is told of what it made: %s", mustJSON(tags))
	}
	item := []map[string]any{{"RevisionId": emma, "Type": "ProductRevisionTagItem"}}
	if status, _, _ := a.kobo(token, http.MethodPost, "/v1/library/tags/"+made2+"/items", "", map[string]any{"Items": item}); status != 201 {
		t.Errorf("add items: %d", status)
	}
	if _, got, _ = a.call(reader, http.MethodGet, "/collections/"+made2, nil); got["books"].(float64) != 2 {
		t.Errorf("after adding: %v", got)
	}
	if status, _, _ := a.kobo(token, http.MethodPost, "/v1/library/tags/"+made2+"/items/delete", "", map[string]any{"Items": item}); status != 200 {
		t.Errorf("remove items: %d", status)
	}
	if status, _, _ := a.kobo(token, http.MethodPut, "/v1/library/tags/"+made2, "", map[string]any{"Name": "Renamed"}); status != 200 {
		t.Errorf("rename: %d", status)
	}
	if _, got, _ = a.call(reader, http.MethodGet, "/collections/"+made2, nil); got["name"] != "Renamed" || got["books"].(float64) != 1 {
		t.Errorf("after the reader's changes: %v", got)
	}
	if tags := sync(); len(tags) != 0 {
		t.Errorf("the reader is told of its own changes: %s", mustJSON(tags))
	}
	if status, _, _ := a.kobo(token, http.MethodDelete, "/v1/library/tags/"+made2, "", nil); status != 200 {
		t.Errorf("delete: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/collections/"+made2, nil); status != 404 {
		t.Errorf("deleted on the reader, still there: %d", status)
	}
	if tags := sync(); len(tags) != 0 {
		t.Errorf("told of a deletion it made: %s", mustJSON(tags))
	}

	// Another person's shared collection and a smart shelf are read only.
	_, shared, _ := a.call(other, http.MethodPost, "/collections", map[string]any{"name": "Others", "visibility": "shared"})
	sharedID := shared["id"].(string)
	a.call(other, http.MethodPost, "/collections/"+sharedID+"/books", map[string]any{"books": []string{emma}})
	_, private, _ := a.call(other, http.MethodPost, "/collections", map[string]any{"name": "Secret"})
	rules, _ := json.Marshal(map[string]any{"all": []map[string]any{{"field": "status", "op": "in", "values": []string{"unread"}}}})
	_, smart, _ := a.call(reader, http.MethodPost, "/smart-shelves", map[string]any{"name": "Unread", "filter": string(rules)})
	smartID := smart["id"].(string)
	tags = sync()
	names := map[string]*koboTagEntry{}
	for _, it := range tags {
		if it.NewTag != nil {
			names[it.NewTag.Tag.Name] = it.NewTag
		}
	}
	if len(tags) != 2 || names["Others (other)"] == nil || names["Unread"] == nil || len(names["Unread"].books()) != 2 || names["Secret"] != nil {
		t.Fatalf("shared and smart: %s", mustJSON(tags))
	}
	_ = private
	for _, id := range []string{sharedID, smartID} {
		for _, c := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPut, "/v1/library/tags/" + id, map[string]any{"Name": "Mine now"}},
			{http.MethodPost, "/v1/library/tags/" + id + "/items", map[string]any{"Items": item}},
			{http.MethodPost, "/v1/library/tags/" + id + "/items/delete", map[string]any{"Items": item}},
			{http.MethodDelete, "/v1/library/tags/" + id, nil},
		} {
			if status, _, _ := a.kobo(token, c.method, c.path, "", c.body); status != 403 {
				t.Errorf("%s %s: %d, want 403", c.method, c.path, status)
			}
		}
	}
	if _, got, _ = a.call(other, http.MethodGet, "/collections/"+sharedID, nil); got["name"] != "Others" || got["books"].(float64) != 1 {
		t.Errorf("the shared collection changed: %v", got)
	}
	// A tag the reader changed against the rules is sent again as it is.
	tags = sync()
	if len(tags) != 2 || tags[0].NewTag == nil || tags[1].NewTag == nil {
		t.Errorf("the refused changes are not undone on the reader: %s", mustJSON(tags))
	}
	// Someone else's private collection is no collection at all.
	secretID := private["id"].(string)
	for _, path := range []string{"/v1/library/tags/" + secretID, "/v1/library/tags/00000000-0000-0000-0000-000000000000"} {
		if status, _, _ := a.kobo(token, http.MethodPut, path, "", map[string]any{"Name": "x"}); status != 404 {
			t.Errorf("PUT %s: %d, want 404", path, status)
		}
	}

	// Nothing of a library the person no longer sees goes to the reader.
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+readerID, nil); status != 204 {
		t.Fatalf("leave: %d", status)
	}
	tags = sync()
	for _, it := range tags {
		if it.ChangedTag != nil {
			if n := len(it.ChangedTag.books()); n != 0 {
				t.Errorf("%s still names %d books of a library lost", it.ChangedTag.Tag.Name, n)
			}
		}
		if it.NewTag != nil {
			t.Errorf("a tag told anew: %s", mustJSON(it))
		}
	}
	_, raw2, _ := a.kobo(token, http.MethodGet, "/v1/library/tags", "", nil)
	if strings.Contains(string(raw2), emma) || strings.Contains(string(raw2), persuasion) {
		t.Errorf("the tag list names a book of a library lost: %s", raw2)
	}
	// Deleting a collection takes it off the reader.
	if status, _, _ := a.call(reader, http.MethodDelete, "/collections/"+mine, nil); status != 204 {
		t.Fatalf("delete: %d", status)
	}
	tags = sync()
	deleted := false
	for _, it := range tags {
		deleted = deleted || (it.DeletedTag != nil && it.DeletedTag.Tag.ID == mine)
	}
	if !deleted {
		t.Errorf("a deleted collection stays on the reader: %s", mustJSON(tags))
	}
}
