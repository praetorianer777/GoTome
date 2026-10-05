//go:build integration

package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/notify"
)

// readCase is one way to call a read route. Path and query hold
// placeholders in braces, filled from the secrets or their twins.
type readCase struct {
	path  string
	query url.Values
	// control says who must see the secret through this call, which proves
	// the call reaches it: "insider" (or the administrator, for a route the
	// insider may not use), "former" before they lose access, or "" with a
	// reason in why for a call that cannot show it to anyone.
	control string
	why     string
}

// readCases says, for every route that reads what belongs to libraries,
// how to call it about the private library's book. A route that is in the
// table but not here fails the test: a new read route must say how it is
// scoped, and be proven to be.
func readCases() map[string][]readCase {
	q := func(kv ...string) url.Values {
		v := url.Values{}
		for i := 0; i < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}
	byAuthor := `{"field":"author","op":"in","values":["{author}"]}`
	return map[string][]readCase{
		"listLibraries":      {{path: "/libraries", control: "insider"}},
		"getLibrary":         {{path: "/libraries/{library}", control: "insider"}},
		"listLibraryMembers": {{path: "/libraries/{library}/members", control: "insider"}},
		"listJobs":           {{path: "/jobs", control: "insider"}},
		"listBooks": {
			{path: "/books", control: "insider"},
			{path: "/books", query: q("library", "{library}"), control: "insider"},
			{path: "/books", query: q("filter", byAuthor), control: "insider"},
		},
		"searchBooks": {{path: "/books/search", query: q("q", "{title}"), control: "insider"}},
		"searchText": {
			{path: "/search", query: q("q", "{title}"), control: "insider"},
			{path: "/search", query: q("q", "{title}", "library", "{library}"), control: "insider"},
		},
		"listBookFacets": {
			{path: "/books/facets", control: "insider"},
			{path: "/books/facets", query: q("library", "{library}"), control: "insider"},
		},
		"countBooks": {
			{path: "/books/count", query: q("filter", byAuthor), control: "insider"},
			{path: "/books/count", query: q("library", "{library}"), control: "insider"},
		},
		"listNames": {
			{path: "/books/names", query: q("kind", "author", "q", "{author}"), control: "insider"},
			{path: "/books/names", query: q("kind", "tag", "q", "{tag}"), control: "insider"},
		},
		"getBook":         {{path: "/books/{book}", control: "insider"}},
		"listCandidates":  {{path: "/books/{book}/candidates", control: "insider"}},
		"getAudio":        {{path: "/books/{book}/audio", control: "insider"}},
		"getProgress":     {{path: "/books/{book}/progress", control: "insider"}},
		"getBookCover":    {{path: "/books/{book}/covers/small", control: "insider"}},
		"downloadFile":    {{path: "/files/{file}/download", control: "insider"}},
		"getStats":        {{path: "/me/stats", control: "insider"}},
		"listReview":      {{path: "/matches", control: "insider"}},
		"listCollections": {{path: "/collections", query: q("book", "{book}"), control: "insider"}},
		"getCollection": {
			{path: "/collections/{sharedCollection}", control: "insider"},
			{path: "/collections/{formerCollection}", control: "former"},
		},
		"listSmartShelves": {{path: "/smart-shelves", why: "shelves list names and counts, which the shelf's own route checks"}},
		"getSmartShelf":    {{path: "/smart-shelves/{formerShelf}", why: "a shelf says how many books it holds, checked on its own below"}},
		"listSmartShelfBooks": {
			{path: "/smart-shelves/{sharedShelf}/books", control: "insider"},
			{path: "/smart-shelves/{formerShelf}/books", control: "former"},
		},
		"listNotifications":   {{path: "/notifications", control: "former"}},
		"streamNotifications": {{path: "/notifications/stream", why: "it carries the unread count alone, checked against the list below"}},
		"getBulk": {
			{path: "/bulk/{insiderBulk}", control: "insider"},
			{path: "/bulk/{formerBulk}", control: "former"},
		},
	}
}

// twinned are the placeholders that name the secret itself. A call that
// uses one must be answered to someone outside the library exactly as the
// same call about something that does not exist.
var twinned = []string{"book", "file", "library", "title", "author", "tag"}

var requestIDs = regexp.MustCompile(`"requestId":"[^"]*"`)

// get calls a path and returns the status and the body, whatever it is; of
// a stream of events, the first event.
func (a *app) get(c *http.Client, path string) (int, []byte) {
	a.t.Helper()
	resp, err := c.Get(a.url + httpapi.APIPrefix + path)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		lines := bufio.NewReader(resp.Body)
		event, err := lines.ReadString('\n')
		for err == nil && !strings.HasSuffix(event, "\n\n") {
			var line string
			line, err = lines.ReadString('\n')
			event += line
		}
		return resp.StatusCode, []byte(event)
	}
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, requestIDs.ReplaceAll(body, nil)
}

func TestEveryReadRouteKeepsToTheCallersLibraries(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withProvider()
	a.matcher()
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	insider, insiderID := a.signedIn("insider", "editor")
	former, formerID := a.signedIn("former", "editor")
	stranger, _ := a.signedIn("stranger", "reader")
	mark := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	twinMark := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]

	open := a.library(admin, "Open "+mark, "shared")
	vaultName := "Vault " + mark
	vault := a.library(admin, vaultName, "private")
	for _, id := range []string{insiderID, formerID} {
		if status, out, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+id, nil); status != 204 {
			t.Fatalf("membership: %d %v", status, out)
		}
	}
	if status, out := a.upload(admin, open, "Plain.pdf", []byte("%PDF-1.4 plain")); status != 200 {
		t.Fatalf("upload: %d %v", status, out)
	}
	status, up := a.upload(admin, vault, "secret.pdf", []byte("%PDF-1.4 "+mark))
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	secret := map[string]string{
		"book": up["bookId"].(string), "file": up["fileId"].(string), "library": vault,
		"title": "Secretum " + mark, "author": "Abditus " + mark, "tag": "Arcanum" + mark,
	}
	twin := map[string]string{
		"book": uuid.NewString(), "file": uuid.NewString(), "library": uuid.NewString(),
		// Other words than the secret's: the quick search and the names
		// match words alike, which would find the secret for its twin.
		"title": "Quaesitum " + twinMark, "author": "Ignotus " + twinMark, "tag": "Nullum" + twinMark,
	}
	book := secret["book"]
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+book, map[string]any{
		"title": secret["title"], "series": map[string]any{"name": "Occultum " + mark},
		"contributors": []map[string]string{{"name": secret["author"], "role": "author"}},
		"tags":         []string{secret["tag"]},
	}); status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}
	if status, out := a.putCover(admin, book, squarePNG(t)); status != 200 {
		t.Fatalf("cover: %d %v", status, out)
	}
	record, _ := json.Marshal(metadata.Record{ID: "secret", Title: secret["title"]})
	if _, err := a.pool.Exec(ctx, `INSERT INTO metadata_matches (book_id, provider, record_id, score, record)
		VALUES ($1, 'shelf', 'secret', 0.5, $2)`, book, record); err != nil {
		t.Fatal(err)
	}
	// The file is no PDF, so its text is the test's: a passage that names
	// the book, for the full-text search to find.
	if _, err := a.pool.Exec(ctx, `INSERT INTO book_chunks (book_id, library_id, file_id, position, char_offset, lang, body_en)
		VALUES ($1, $2, $3, 0, 0, 'en', $4)`, book, vault, secret["file"], "A passage about "+secret["title"]+" and nothing else."); err != nil {
		t.Fatal(err)
	}
	markers := []string{secret["title"], secret["author"], secret["tag"], "Occultum " + mark, book, secret["file"], vault, vaultName}

	// What the insider and the former member do with the book while both
	// may see it: reading, rating, shelving, changing it in bulk.
	owned := map[string]string{}
	// A shelf's rules are its owner's own words, which they may share; so
	// that they tell nothing, the shelves here match the book by rating.
	rated := `{"field":"rating","op":"in","values":["5"]}`
	for name, c := range map[string]*http.Client{"insider": insider, "former": former} {
		if status, out, _ := a.call(c, http.MethodPut, "/books/"+book+"/progress/ebook", map[string]any{
			"locator": "page:3", "fraction": 0.3, "clientId": name,
		}); status != 200 {
			t.Fatalf("progress: %d %v", status, out)
		}
		if status, out, _ := a.call(c, http.MethodPut, "/books/"+book+"/reading", map[string]any{"rating": 5}); status != 200 {
			t.Fatalf("rating: %d %v", status, out)
		}
		visibility := map[string]string{"insider": "shared", "former": "private"}[name]
		_, col, _ := a.call(c, http.MethodPost, "/collections", map[string]any{"name": "Shelf of " + name, "visibility": visibility})
		a.call(c, http.MethodPost, "/collections/"+col["id"].(string)+"/books", map[string]any{"books": []string{book}})
		_, shelf, _ := a.call(c, http.MethodPost, "/smart-shelves", map[string]any{"name": "By the author", "filter": rated, "visibility": visibility})
		bulk := a.runBulk(c, map[string]any{"books": []string{book}, "action": "writeBack"})
		owned[map[string]string{"insider": "sharedCollection", "former": "formerCollection"}[name]] = col["id"].(string)
		owned[map[string]string{"insider": "sharedShelf", "former": "formerShelf"}[name]] = shelf["id"].(string)
		owned[name+"Bulk"] = bulk["id"].(string)
	}

	if err := db.InTx(ctx, a.pool, func(tx pgx.Tx) error {
		secretBook := uuid.MustParse(book)
		_, err := notify.CreateTx(ctx, tx, uuid.MustParse(formerID), notify.New{
			Kind: notify.KindBulkFinished, Data: map[string]string{"title": secret["title"]}, BookID: &secretBook,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	fill := func(s string, values map[string]string) string {
		for k, v := range owned {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		for k, v := range values {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s
	}
	target := func(c readCase, values map[string]string) string {
		path := fill(c.path, values)
		if len(c.query) > 0 {
			v := url.Values{}
			for k, vs := range c.query {
				v.Set(k, fill(vs[0], values))
			}
			path += "?" + v.Encode()
		}
		return path
	}
	usesTwin := func(c readCase) bool {
		all := c.path + fmt.Sprint(c.query)
		for _, k := range twinned {
			if strings.Contains(all, "{"+k+"}") {
				return true
			}
		}
		return false
	}
	// asTwin is a body as it reads about the secret, so that two answers
	// differ only where they would tell something.
	asTwin := func(body []byte) []byte {
		for _, k := range twinned {
			body = bytes.ReplaceAll(body, []byte(twin[k]), []byte(secret[k]))
		}
		return body
	}
	reaches := func(c *http.Client, rc readCase) bool {
		status, body := a.get(c, target(rc, secret))
		if usesTwin(rc) {
			twinStatus, twinBody := a.get(c, target(rc, twin))
			return status != twinStatus || !bytes.Equal(body, asTwin(twinBody))
		}
		for _, m := range markers {
			if bytes.Contains(body, []byte(m)) {
				return true
			}
		}
		return false
	}

	routes := map[string]httpapi.Route{}
	for _, rt := range a.server.Table() {
		if rt.Method == http.MethodGet && rt.Reads == httpapi.ReadsLibraries {
			routes[rt.ID] = rt
		}
	}
	cases := readCases()
	for id := range routes {
		if len(cases[id]) == 0 {
			t.Errorf("%s reads from libraries, but this test does not say how to call it about a private one", id)
		}
	}
	for id := range cases {
		if _, ok := routes[id]; !ok {
			t.Errorf("%s is called here, but is not a route that reads from libraries", id)
		}
	}

	// The former member's calls prove themselves before they lose access.
	for id, list := range cases {
		for _, rc := range list {
			if rc.control == "former" && !reaches(former, rc) {
				t.Errorf("%s %s does not show the former member the book while they may see it", id, rc.path)
			}
		}
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+formerID, nil); status != 204 {
		t.Fatalf("removing the former member: %d", status)
	}

	for id, list := range cases {
		rt := routes[id]
		for _, rc := range list {
			name := id + " " + target(rc, secret)
			switch {
			case rc.control == "insider":
				control := insider
				if !auth.Allows(auth.RoleEditor, rt.Permission) {
					control = admin
				}
				if !reaches(control, rc) {
					t.Errorf("%s: the control does not see the book, so the call proves nothing", name)
				}
			case rc.control == "" && rc.why == "":
				t.Errorf("%s: no control and no reason why", name)
			}
			for who, c := range map[string]*http.Client{"former member": former, "stranger": stranger} {
				status, body := a.get(c, target(rc, secret))
				for _, m := range markers {
					if bytes.Contains(body, []byte(m)) {
						t.Errorf("%s tells the %s %q: %d %s", name, who, m, status, body)
					}
				}
				if !usesTwin(rc) {
					continue
				}
				twinStatus, twinBody := a.get(c, target(rc, twin))
				if status != twinStatus || !bytes.Equal(body, asTwin(twinBody)) {
					t.Errorf("%s answers the %s otherwise than about nothing:\n  %d %s\n  %d %s", name, who, status, body, twinStatus, asTwin(twinBody))
				}
			}
		}
	}

	// The unread count is of what may be seen, as the stream's is.
	_, told, _ := a.call(former, http.MethodGet, "/notifications", nil)
	unread := 0
	for _, n := range told["notifications"].([]any) {
		if n.(map[string]any)["readAt"] == nil {
			unread++
		}
	}
	if told["unread"].(float64) != float64(unread) {
		t.Errorf("the former member is told of %v unread, but sees %d", told["unread"], unread)
	}
	if _, event := a.get(former, "/notifications/stream"); !strings.Contains(string(event), fmt.Sprintf(`{"unread":%d}`, unread)) {
		t.Errorf("the former member's stream says %q, not %d unread", event, unread)
	}

	_, shelf, _ := a.call(former, http.MethodGet, "/smart-shelves/"+owned["formerShelf"], nil)
	if shelf["books"].(float64) != 0 {
		t.Errorf("the former member's smart shelf still counts the book: %v", shelf)
	}
	_, list, _ := a.call(stranger, http.MethodGet, "/smart-shelves", nil)
	for _, s := range list["shelves"].([]any) {
		if s := s.(map[string]any); s["books"].(float64) != 0 {
			t.Errorf("the stranger is told a shared shelf holds %v books", s["books"])
		}
	}
}
