//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// basicAuth sends a user name and device token with every request, as an
// OPDS client does.
type basicAuth struct{ user, token string }

func (b basicAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth(b.user, b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func deviceClient(user, token string) *http.Client {
	return &http.Client{Transport: basicAuth{user, token}}
}

// feed is what a test reads of an OPDS feed.
type feed struct {
	Title   string     `xml:"title"`
	Links   []feedLink `xml:"link"`
	Entries []struct {
		ID      string     `xml:"id"`
		Title   string     `xml:"title"`
		Authors []string   `xml:"author>name"`
		Content string     `xml:"content"`
		Links   []feedLink `xml:"link"`
	} `xml:"entry"`
}

type feedLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

// fetch gets an address of the server, whole, as a device does.
func (a *app) fetch(c *http.Client, href string) (int, http.Header, []byte) {
	a.t.Helper()
	resp, err := c.Get(a.url + href)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

func (a *app) feed(c *http.Client, href string) feed {
	a.t.Helper()
	status, header, body := a.fetch(c, href)
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "application/atom+xml") {
		a.t.Fatalf("%s: %d %s %s", href, status, header.Get("Content-Type"), body)
	}
	var f feed
	if err := xml.Unmarshal(body, &f); err != nil {
		a.t.Fatalf("%s: %v\n%s", href, err, body)
	}
	return f
}

func TestADeviceBrowsesTheCatalogueWithItsTokenAndNothingElse(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("Reader", "reader")
	shared := a.library(admin, "Shared", "shared")
	content := []byte("%PDF-1.4 the lighthouse")
	status, up := a.upload(admin, shared, "Lighthouse.pdf", content)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	book := up["bookId"].(string)
	if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+book, map[string]any{
		"title": "To the Lighthouse", "series": map[string]any{"name": "Waves", "index": 2},
		"contributors": []map[string]string{{"name": "Virginia Woolf", "role": "author"}},
	}); status != 200 {
		t.Fatalf("edit: %d %v", status, out)
	}

	if status, out, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": " "}); status != 422 {
		t.Errorf("a nameless device: %d %v", status, out)
	}
	status, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	if status != 201 {
		t.Fatalf("create device: %d %v", status, made)
	}
	token := made["token"].(string)
	if made["catalog"] != httpapi.APIPrefix+"/opds" || len(token) != 24 {
		t.Errorf("created: %v", made)
	}
	device := deviceClient("reader", strings.ToUpper(token))

	root := a.feed(device, httpapi.APIPrefix+"/opds")
	links := map[string]string{}
	for _, e := range root.Entries {
		links[e.Title] = e.Links[0].Href
	}
	if len(links) != 7 || links["Sent to this device"] != httpapi.APIPrefix+"/opds/device" || links["Authors"] != httpapi.APIPrefix+"/opds/authors" {
		t.Errorf("root: %v", links)
	}

	authors := a.feed(device, links["Authors"])
	if len(authors.Entries) != 1 || authors.Entries[0].Title != "Virginia Woolf" {
		t.Fatalf("authors: %+v", authors)
	}
	books := a.feed(device, authors.Entries[0].Links[0].Href)
	if books.Title != "Virginia Woolf" || len(books.Entries) != 1 || books.Entries[0].Title != "To the Lighthouse" {
		t.Fatalf("books by the author: %+v", books)
	}
	var download string
	for _, l := range books.Entries[0].Links {
		if l.Rel == "http://opds-spec.org/acquisition" {
			download = l.Href
			if l.Type != "application/pdf" {
				t.Errorf("acquisition: %+v", l)
			}
		}
	}
	if status, _, body := a.fetch(device, download); status != 200 || string(body) != string(content) {
		t.Errorf("download: %d %q", status, body)
	}
	if series := a.feed(device, httpapi.APIPrefix+"/opds/series"); len(series.Entries) != 1 || series.Entries[0].Title != "Waves" {
		t.Errorf("series: %+v", series)
	}
	if found := a.feed(device, httpapi.APIPrefix+"/opds/search?q=lighthous"); len(found.Entries) != 1 {
		t.Errorf("search: %+v", found)
	}
	status, header, body := a.fetch(device, httpapi.APIPrefix+"/opds/opensearch.xml")
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "application/opensearchdescription+xml") ||
		!strings.Contains(string(body), `template="/api/v1/opds/search?q={searchTerms}"`) {
		t.Errorf("search description: %d %s", status, body)
	}

	// The token reaches only what is for devices.
	// and to what a device's own app needs: the books, and where the person is in them.
	if status, out, _ := a.call(device, http.MethodGet, "/books?limit=5", nil); status != 200 || len(out["books"].([]any)) != 1 {
		t.Errorf("the library's list with a device token: %d %v", status, out)
	}
	if status, out, _ := a.call(device, http.MethodPut, "/books/"+book+"/progress/ebook", map[string]any{
		"locator": "fraction:0.5", "fraction": 0.5, "clientId": "tolino-1",
	}); status != 200 || out["saved"] != true {
		t.Errorf("progress with a device token: %d %v", status, out)
	}
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/collections"); status != 401 {
		t.Errorf("the collections with a device token: %d", status)
	}
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/auth/devices"); status != 401 {
		t.Errorf("the devices with a device token: %d", status)
	}
	// Without credentials, or with wrong ones, the client is asked for them.
	for name, c := range map[string]*http.Client{
		"none":          http.DefaultClient,
		"another user":  deviceClient("admin", token),
		"a wrong token": deviceClient("reader", "abcd-efgh-ijkm-npqr-stuv"),
	} {
		status, header, _ := a.fetch(c, httpapi.APIPrefix+"/opds")
		if status != 401 || !strings.HasPrefix(header.Get("WWW-Authenticate"), "Basic ") {
			t.Errorf("%s: %d %q", name, status, header.Get("WWW-Authenticate"))
		}
	}

	status, listed, _ := a.call(reader, http.MethodGet, "/auth/devices", nil)
	devices := listed["devices"].([]any)
	if status != 200 || len(devices) != 1 || devices[0].(map[string]any)["lastUsedAt"] == nil {
		t.Fatalf("devices: %d %v", status, listed)
	}
	id := devices[0].(map[string]any)["id"].(string)
	if status, _, _ := a.call(admin, http.MethodDelete, "/auth/devices/"+id, nil); status != 404 {
		t.Errorf("someone else revoking the device: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/auth/devices/"+id, nil); status != 204 {
		t.Fatalf("revoke: %d", status)
	}
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/opds"); status != 401 {
		t.Errorf("a revoked token: %d", status)
	}
}

func TestGuessingADeviceTokenIsSlowedDown(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.signedIn("reader", "reader")
	guess := deviceClient("reader", "abcd-efgh-ijkm-npqr-stuv")
	var status int
	for range 6 {
		status, _, _ = a.fetch(guess, httpapi.APIPrefix+"/opds")
	}
	if status != 429 {
		t.Errorf("the sixth wrong token: %d", status)
	}
}

func TestADisabledAccountsDevicesSignNobodyIn(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, readerID := a.signedIn("reader", "reader")
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := deviceClient("reader", made["token"].(string))
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/opds"); status != 200 {
		t.Fatalf("before: %d", status)
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/users/"+readerID, map[string]any{"disabled": true}); status != 200 {
		t.Fatalf("disable: %d %v", status, out)
	}
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/opds"); status != 401 {
		t.Errorf("after: %d", status)
	}
}

func TestTheKOReaderPluginIsToldWhereItCameFrom(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")
	status, header, body := a.fetch(reader, httpapi.APIPrefix+"/koreader/plugin.zip")
	if status != 200 || header.Get("Content-Type") != "application/zip" {
		t.Fatalf("download: %d %s", status, header.Get("Content-Type"))
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var server string
	for _, f := range z.File {
		if f.Name == "gotome.koplugin/gotome_server.lua" {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			server = string(data)
		}
	}
	if !strings.Contains(server, `return { "`+a.url+`" }`) {
		t.Errorf("the plugin's server: %q, want %s", server, a.url)
	}
	if status, _, _ := a.fetch(http.DefaultClient, httpapi.APIPrefix+"/koreader/plugin.zip"); status != 401 {
		t.Errorf("signed out: %d", status)
	}
}

func TestTheShelvesFeedsListWhatTheShelvesHold(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	shared := a.library(admin, "Shared", "shared")
	hidden := a.library(admin, "Hidden", "private")
	book := func(library, title string) string {
		t.Helper()
		status, up := a.upload(admin, library, title+".pdf", []byte("%PDF-1.4 "+title))
		if status != 200 {
			t.Fatalf("upload: %d %v", status, up)
		}
		id := up["bookId"].(string)
		if status, out, _ := a.call(admin, http.MethodPatch, "/books/"+id, map[string]any{"title": title}); status != 200 {
			t.Fatalf("edit: %d %v", status, out)
		}
		return id
	}
	alpha, beta, gamma := book(shared, "Alpha"), book(shared, "Beta"), book(shared, "Gamma")
	secret := book(hidden, "Secret")
	collection := func(c *http.Client, name, visibility string, books ...string) string {
		t.Helper()
		status, col, _ := a.call(c, http.MethodPost, "/collections", map[string]any{"name": name, "visibility": visibility})
		if status != 201 && status != 200 {
			t.Fatalf("collection: %d %v", status, col)
		}
		id := col["id"].(string)
		if status, out, _ := a.call(c, http.MethodPost, "/collections/"+id+"/books", map[string]any{"books": books}); status >= 300 {
			t.Fatalf("add to the collection: %d %v", status, out)
		}
		return id
	}
	smartShelf := func(c *http.Client, name, visibility, filter string) string {
		t.Helper()
		status, shelf, _ := a.call(c, http.MethodPost, "/smart-shelves", map[string]any{"name": name, "filter": filter, "visibility": visibility})
		if status != 201 && status != 200 {
			t.Fatalf("smart shelf: %d %v", status, shelf)
		}
		return shelf["id"].(string)
	}
	own := collection(reader, "Holiday", "private", gamma, alpha)
	others := collection(admin, "Staff picks", "shared", beta, secret)
	collection(admin, "Admin's own", "private", alpha)
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+beta+"/reading", map[string]any{"rating": 5}); status != 200 {
		t.Fatalf("rating: %d %v", status, out)
	}
	if status, out, _ := a.call(admin, http.MethodPut, "/books/"+gamma+"/reading", map[string]any{"rating": 5}); status != 200 {
		t.Fatalf("rating: %d %v", status, out)
	}
	favourites := smartShelf(reader, "Favourites", "private", `{"field":"rating","op":"in","values":["5"]}`)
	smartShelf(admin, "Admin's favourites", "private", `{"field":"rating","op":"in","values":["5"]}`)
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	device := deviceClient("reader", made["token"].(string))

	shelves := a.feed(device, httpapi.APIPrefix+"/opds/shelves")
	listed := map[string]string{}
	links := map[string]string{}
	for _, e := range shelves.Entries {
		listed[e.Title] = e.Content
		links[e.Title] = e.Links[0].Href
	}
	want := map[string]string{"Holiday": "2 books", "Staff picks": "1 book", "Favourites": "1 book"}
	if len(listed) != len(want) {
		t.Errorf("shelves: %v, want %v", listed, want)
	}
	for name, count := range want {
		if listed[name] != count {
			t.Errorf("%s: %q, want %q", name, listed[name], count)
		}
	}
	if links["Holiday"] != httpapi.APIPrefix+"/opds/collections/"+own || links["Favourites"] != httpapi.APIPrefix+"/opds/smart-shelves/"+favourites {
		t.Errorf("shelf links: %v", links)
	}

	titles := func(href string) []string {
		t.Helper()
		var out []string
		for _, e := range a.feed(device, href).Entries {
			out = append(out, e.Title)
		}
		return out
	}
	for href, want := range map[string]string{
		httpapi.APIPrefix + "/opds/collections/" + own:          "Gamma Alpha",
		httpapi.APIPrefix + "/opds/collections/" + others:       "Beta",
		httpapi.APIPrefix + "/opds/smart-shelves/" + favourites: "Beta",
	} {
		if got := strings.Join(titles(href), " "); got != want {
			t.Errorf("%s: %q, want %q", href, got, want)
		}
	}
	f := a.feed(device, httpapi.APIPrefix+"/opds/collections/"+own)
	if f.Title != "Holiday" || !strings.HasPrefix(f.Entries[0].Links[0].Href, httpapi.APIPrefix+"/files/") {
		t.Errorf("the collection's feed: %+v", f)
	}
	if status, out, _ := a.call(reader, http.MethodPut, "/books/"+alpha+"/reading", map[string]any{"rating": 5}); status != 200 {
		t.Fatalf("rating: %d %v", status, out)
	}
	if got := strings.Join(titles(httpapi.APIPrefix+"/opds/smart-shelves/"+favourites), " "); got != "Alpha Beta" {
		t.Errorf("the smart shelf after a rating: %q", got)
	}
}
