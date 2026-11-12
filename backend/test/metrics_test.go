//go:build integration

package test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func (a *app) scrape(c *http.Client) (int, string, http.Header) {
	a.t.Helper()
	resp, err := c.Get(a.url + "/api/v1/metrics")
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func TestMetricsAreOffUntilSwitchedOnAndNeedTheMetricsScope(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	_, metricsToken := a.makeKey(admin, "scrape", "metrics")
	_, adminToken := a.makeKey(admin, "admin", "admin")
	scraper, adminKey := keyClient(metricsToken), keyClient(adminToken)

	if status, _, _ := a.scrape(scraper); status != 404 {
		t.Errorf("switched off: %d, want 404", status)
	}
	a.setSetting(admin, "metrics.enabled", "on")

	if status, _, _ := a.scrape(scraper); status != 200 {
		t.Errorf("a metrics key: %d", status)
	}
	if status, _, _ := a.scrape(adminKey); status != 403 {
		t.Errorf("an admin key: %d, want 403", status)
	}
	if status, _, _ := a.scrape(a.browser()); status != 401 {
		t.Errorf("nobody: %d, want 401", status)
	}
	if status, _, _ := a.scrape(admin); status != 200 {
		t.Errorf("an administrator's session: %d", status)
	}
	editor, _ := a.signedIn("editor", "editor")
	if status, _, _ := a.scrape(editor); status != 403 {
		t.Errorf("an editor: %d, want 403", status)
	}
	if status, _, _ := a.call(scraper, http.MethodGet, "/books", nil); status != 403 {
		t.Errorf("a metrics key reads books: %d", status)
	}
}

func TestMetricsNameTheirSeriesAndNoTitle(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Quiet Shelves", "shared")
	a.bookBy(lib, "The Moonlit Harbour", "Ada Writer")
	a.setSetting(admin, "metrics.enabled", "on")
	_, token := a.makeKey(admin, "scrape", "metrics")
	scraper := keyClient(token)
	for range 3 {
		a.call(admin, http.MethodGet, "/libraries", nil)
	}

	status, body, h := a.scrape(scraper)
	if status != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("%d %v", status, h)
	}
	for _, want := range []string{
		"# TYPE gotome_books gauge", "gotome_books 1\n", `gotome_users{state="active"} 1`, "gotome_libraries 1\n",
		"# TYPE gotome_http_request_duration_seconds histogram",
		`gotome_http_requests_total{route="/api/v1/libraries",method="GET",class="2xx"} 3`,
		`gotome_http_request_duration_seconds_bucket{route="/api/v1/libraries",method="GET",class="2xx",le="+Inf"} 3`,
		"gotome_db_pool_connections_max", "gotome_search_outbox_rows", "gotome_jobs", `gotome_build_info{version="test"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the metrics lack %q", want)
		}
	}
	for _, leak := range []string{"Moonlit", "Harbour", "Writer", "Quiet Shelves", "admin", "editor"} {
		if strings.Contains(body, leak) {
			t.Errorf("the metrics name %q", leak)
		}
	}
	if strings.Contains(body, lib) {
		t.Error("the metrics hold a library's ID in a path: routes must be patterns")
	}
	a.call(admin, http.MethodGet, "/libraries/"+lib, nil)
	_, body, _ = a.scrape(scraper)
	if !strings.Contains(body, `route="/api/v1/libraries/{libraryId}"`) || strings.Contains(body, lib) {
		t.Errorf("a path with an ID must count under its pattern")
	}
}
