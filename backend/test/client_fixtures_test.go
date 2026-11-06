//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

var updateFixtures = flag.Bool("update", false, "write the device clients' fixtures anew from the server's answers")

// The fixtures the device clients test themselves against: the KOReader
// plugin's in LuaJIT, the Tolino app's in its unit tests.
var (
	pluginFeed  = filepath.Join("..", "internal", "koplugin", "testdata", "device-feed.xml")
	tolinoFiles = filepath.Join("..", "..", "clients", "android", "app", "src", "test", "resources", "api")
)

// fixed makes an answer the same on every run: IDs are numbered in the order
// they first appear, times are one time, and what is drawn at random takes
// a value of its own.
type fixed struct{ ids map[string]string }

var (
	uuidPattern  = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	timePattern  = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
	drawnPattern = regexp.MustCompile(`"(secret|token|nextCursor|requestId)":"[^"]*"|"(code)":"\d{4}"`)
)

func (f *fixed) apply(body []byte) []byte {
	if f.ids == nil {
		f.ids = map[string]string{}
	}
	body = uuidPattern.ReplaceAllFunc(body, func(id []byte) []byte {
		if _, ok := f.ids[string(id)]; !ok {
			f.ids[string(id)] = fmt.Sprintf("0192f0a0-0000-7000-8000-%012x", len(f.ids)+1)
		}
		return []byte(f.ids[string(id)])
	})
	body = timePattern.ReplaceAll(body, []byte("2026-10-11T12:00:00Z"))
	return drawnPattern.ReplaceAllFunc(body, func(m []byte) []byte {
		sub := drawnPattern.FindSubmatch(m)
		key := string(sub[1]) + string(sub[2])
		return []byte(fmt.Sprintf("%q:%q", key, key+"-drawn-at-random"))
	})
}

// answer asks the server and returns the status and the body as sent.
func (a *app) answer(c *http.Client, method, path string, body any) (int, []byte) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, a.url+httpapi.APIPrefix+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return resp.StatusCode, raw
}

// holdsTo compares what the server answered with the fixture at path, or
// writes it there with -update.
func holdsTo(t *testing.T, path string, got []byte) {
	t.Helper()
	if *updateFixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: write it with make device-fixtures", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not what the server answers; if the answer is meant to change, write it anew with "+
			"make device-fixtures and fix the client's tests.\nThe server answers:\n%s", path, got)
	}
}

func indented(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// TestTheDeviceClientsFixturesAreTheServersAnswers holds the fixtures the
// KOReader plugin and the Tolino app are tested with to what the server
// answers them, so a change to an answer shows in their tests.
func TestTheDeviceClientsFixturesAreTheServersAnswers(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("steve", "admin")
	lib := a.library(admin, "Shelf", "shared")
	upload := func(name string, content []byte) (string, string) {
		t.Helper()
		status, up := a.upload(admin, lib, name, content)
		if status != 200 {
			t.Fatalf("upload %s: %d %v", name, status, up)
		}
		return up["bookId"].(string), up["fileId"].(string)
	}
	edit := func(book string, change map[string]any) {
		t.Helper()
		if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+book, change); status != 200 {
			t.Fatalf("edit: %d %v", status, out)
		}
	}

	// A novel in two formats, with its author, series and cover; and one in
	// a Kindle format, by nobody.
	pride, epub := upload("Pride.epub", append([]byte("PK\x03\x04"), bytes.Repeat([]byte("pride "), 100)...))
	if other, _ := upload("Pride.pdf", []byte("%PDF-1.4 pride and prejudice")); other != pride {
		t.Fatalf("the PDF is not of the EPUB's book: %s, %s", other, pride)
	}
	edit(pride, map[string]any{
		"title":        "Pride & Prejudice: A Novel",
		"series":       map[string]any{"name": "Austen's Novels", "index": 2},
		"contributors": []map[string]any{{"name": "Jane Austen", "role": "author"}, {"name": "Tony Tanner", "role": "editor"}},
	})
	if status, out := a.putCover(admin, pride, squarePNG(t)); status != 200 {
		t.Fatalf("cover: %d %v", status, out)
	}
	mobi := make([]byte, 300)
	copy(mobi[60:], "BOOKMOBI")
	schimmel, _ := upload("Schimmelreiter.azw3", mobi)
	edit(schimmel, map[string]any{"title": "Der Schimmelreiter"})
	// One more, not sent, for the library's list to come in pages.
	upload("Emma.pdf", []byte("%PDF-1.4 emma"))

	_, made, _ := a.call(admin, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	deviceID := made["device"].(map[string]any)["id"].(string)
	device := deviceClient("steve", made["token"].(string))
	for _, book := range []string{pride, schimmel} {
		if status, out, _ := a.call(admin, http.MethodPost, "/devices/"+deviceID+"/books", map[string]any{"books": []string{book}}); status != 200 {
			t.Fatalf("send: %d %v", status, out)
		}
	}

	var f fixed
	// The IDs are numbered in this order, whatever order the answers below
	// name them in.
	f.apply([]byte(pride + epub + schimmel))

	status, header, body := a.fetch(device, httpapi.APIPrefix+"/opds/device")
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "application/atom+xml") {
		t.Fatalf("the device's feed: %d %s %s", status, header.Get("Content-Type"), body)
	}
	holdsTo(t, pluginFeed, f.apply(body))

	tolino := func(name string, c *http.Client, method, path string, body any, wantStatus int) {
		t.Helper()
		status, got := a.answer(c, method, path, body)
		if status != wantStatus {
			t.Fatalf("%s %s: %d %s", method, path, status, got)
		}
		holdsTo(t, filepath.Join(tolinoFiles, name+".json"), indented(t, f.apply(got)))
	}
	readable := url.QueryEscape(`{"field":"format","op":"in","values":["epub","pdf"]}`)
	tolino("device", device, http.MethodGet, "/device", nil, 200)
	tolino("device-books", device, http.MethodGet, "/device/books", nil, 200)
	tolino("books-page", device, http.MethodGet, "/books?sort=title&order=asc&limit=1&filter="+readable, nil, 200)
	tolino("books-count", device, http.MethodGet, "/books/count?filter="+readable, nil, 200)
	tolino("books-search", device, http.MethodGet, "/books/search?q=pride&limit=50", nil, 200)
	tolino("book", device, http.MethodGet, "/books/"+pride, nil, 200)

	progress := "/books/" + pride + "/progress/ebook"
	tolino("progress-none", device, http.MethodGet, "/books/"+pride+"/progress", nil, 200)
	tolino("progress-saved", device, http.MethodPut, progress,
		map[string]any{"fileId": epub, "locator": "fraction:0.25", "fraction": 0.25, "clientId": "tolino-ab12cd34"}, 200)
	if status, out, _ := a.call(admin, http.MethodPut, progress,
		map[string]any{"fileId": epub, "locator": "epubcfi(/6/14!/4/2/1:0)", "fraction": 0.47, "clientId": "web"}); status != 200 {
		t.Fatalf("progress from the web: %d %v", status, out)
	}
	tolino("progress-refused", device, http.MethodPut, progress,
		map[string]any{"fileId": epub, "locator": "fraction:0.3", "fraction": 0.3, "clientId": "tolino-ab12cd34"}, 200)
	tolino("progress", device, http.MethodGet, "/books/"+pride+"/progress", nil, 200)

	status, raw := a.answer(http.DefaultClient, http.MethodPost, "/device/requests", map[string]any{"username": "steve", "deviceName": "Tolino Vision"})
	if status != 201 {
		t.Fatalf("request: %d %s", status, raw)
	}
	holdsTo(t, filepath.Join(tolinoFiles, "pairing.json"), indented(t, f.apply(raw)))
	var ticket struct{ ID, Secret string }
	if err := json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	collect := "/device/requests/" + ticket.ID + "/token"
	tolino("pairing-pending", http.DefaultClient, http.MethodPost, collect, map[string]any{"secret": ticket.Secret}, 200)
	_, list, _ := a.call(admin, http.MethodGet, "/auth/device-requests", nil)
	request := list["requests"].([]any)[0].(map[string]any)["id"].(string)
	if status, _, _ := a.call(admin, http.MethodPost, "/auth/device-requests/"+request+"/accept", nil); status != 204 {
		t.Fatalf("accept: %d", status)
	}
	tolino("pairing-accepted", http.DefaultClient, http.MethodPost, collect, map[string]any{"secret": ticket.Secret}, 200)

	tolino("error-unauthorized", deviceClient("steve", "WRONG-TOKEN"), http.MethodGet, "/device", nil, 401)

}
