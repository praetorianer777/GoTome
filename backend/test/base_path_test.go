//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// underBase serves the app again below the path, as a reverse proxy that
// keeps the prefix passes requests on, and points the app's client at it.
func underBase(t *testing.T, a *app, base string) *httptest.Server {
	t.Helper()
	a.server.BasePath = base
	srv := httptest.NewServer(a.server.Routes())
	t.Cleanup(srv.Close)
	a.url = srv.URL + strings.TrimSuffix(base, "/")
	return srv
}

func TestGOtomeServedUnderASubPathAnswersOnlyThere(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	srv := underBase(t, a, "/library/")
	plain := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) *http.Response {
		t.Helper()
		resp, err := plain.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	for path, want := range map[string]int{
		"/library/api/v1/version": 200,
		"/api/v1/version":         404,
		"/other/api/v1/version":   404,
		"/library":                http.StatusPermanentRedirect,
		"/healthz":                200,
		"/readyz":                 200,
	} {
		if got := get(path).StatusCode; got != want {
			t.Errorf("GET %s: %d, want %d", path, got, want)
		}
	}
	if loc := get("/library").Header.Get("Location"); loc != "/library/" {
		t.Errorf("the bare path redirects to %q", loc)
	}
	// Outside the prefix is an error in the envelope, as everywhere else.
	resp, err := plain.Get(srv.URL + "/api/v1/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || errorCode(body) != "not_found" {
		t.Errorf("outside the prefix: %v %v", body, err)
	}
}

func TestTheSessionCookieBelongsToTheSubPath(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	underBase(t, a, "/library/")
	a.signedIn("reader", "reader")

	c := a.browser()
	status, _, header := a.call(c, http.MethodPost, "/auth/login", map[string]any{"username": "reader", "password": "a long password"})
	if status != 200 {
		t.Fatalf("sign in: %d", status)
	}
	cookie := header.Get("Set-Cookie")
	if !strings.Contains(cookie, "gotome_session=") || !strings.Contains(cookie, "Path=/library/;") {
		t.Errorf("the session cookie: %s", cookie)
	}
	if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 200 {
		t.Errorf("signed in under the prefix: %d", status)
	}
	_, _, header = a.call(c, http.MethodPost, "/auth/logout", nil)
	if cookie := header.Get("Set-Cookie"); !strings.Contains(cookie, "Path=/library/;") || !strings.Contains(cookie, "Max-Age=0") {
		t.Errorf("the cleared cookie: %s", cookie)
	}
}

func TestDeviceAddressesCarryTheSubPath(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	underBase(t, a, "/library/")
	reader, _ := a.signedIn("reader", "reader")
	status, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Tolino"})
	if status != 201 {
		t.Fatalf("create device: %d %v", status, made)
	}
	token := made["token"].(string)
	if made["catalog"] != "/library"+httpapi.APIPrefix+"/opds" {
		t.Errorf("catalog: %v", made["catalog"])
	}

	device := deviceClient("reader", token)
	root := a.feed(device, httpapi.APIPrefix+"/opds")
	for _, l := range root.Links {
		if !strings.HasPrefix(l.Href, "/library/api/v1/opds") {
			t.Errorf("a link of the root: %+v", l)
		}
	}
	for _, e := range root.Entries {
		if href := e.Links[0].Href; !strings.HasPrefix(href, "/library/api/v1/opds/") {
			t.Errorf("entry %s links to %s", e.Title, href)
		}
	}
	// A client follows the links as they are, below the prefix.
	authors := a.feed(device, strings.TrimPrefix(root.Entries[2].Links[0].Href, "/library"))
	if authors.Title == "" {
		t.Errorf("authors: %+v", authors)
	}
	status, _, body := a.fetch(device, httpapi.APIPrefix+"/opds/opensearch.xml")
	if status != 200 || !strings.Contains(string(body), `template="/library/api/v1/opds/search?q={searchTerms}"`) {
		t.Errorf("the search description: %d %s", status, body)
	}
}

func TestTheKOReaderPluginAndKoboEndpointKnowTheSubPath(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	srv := underBase(t, a, "/library/")
	reader, _ := a.signedIn("reader", "reader")
	_, made, _ := a.call(reader, http.MethodPost, "/auth/devices", map[string]any{"name": "Kobo"})
	token := made["token"].(string)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/library"+httpapi.APIPrefix+"/kobo/"+token+"/v1/initialization", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var init struct{ Resources map[string]string }
	if err := json.NewDecoder(resp.Body).Decode(&init); err != nil || resp.StatusCode != 200 {
		t.Fatalf("initialization: %d %v", resp.StatusCode, err)
	}
	if got, want := init.Resources["library_sync"], srv.URL+"/library/api/v1/kobo/"+token+"/v1/library/sync"; got != want {
		t.Errorf("library_sync = %q, want %q", got, want)
	}

	status, header, body := a.fetch(reader, httpapi.APIPrefix+"/koreader/plugin.zip")
	if status != 200 || header.Get("Content-Type") != "application/zip" {
		t.Fatalf("plugin: %d", status)
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
	if !strings.Contains(server, `return { "`+srv.URL+`/library" }`) {
		t.Errorf("the plugin's server: %q", server)
	}
}
