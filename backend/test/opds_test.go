//go:build integration

package test

import (
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
	if status, _, _ := a.fetch(device, httpapi.APIPrefix+"/books"); status != 401 {
		t.Errorf("the library's list with a device token: %d", status)
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
