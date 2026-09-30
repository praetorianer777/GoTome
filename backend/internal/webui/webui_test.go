package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	forever    = "public, max-age=31536000, immutable"
	revalidate = "no-cache"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<html>app</html>")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc.css": {Data: []byte("body{}")},
		"favicon.ico":          {Data: []byte("icon")},
	}
}

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestServesTheApp(t *testing.T) {
	h := New(built())
	cases := []struct {
		path, body, cache, contentType string
	}{
		{"/", "<html>app</html>", revalidate, "text/html"},
		{"/index.html", "<html>app</html>", revalidate, "text/html"},
		// The app routes in the browser: a deep link loads the app.
		{"/books/42", "<html>app</html>", revalidate, "text/html"},
		{"/books/42?tab=files", "<html>app</html>", revalidate, "text/html"},
		{"/assets/index-abc.js", "console.log(1)", forever, "javascript"},
		{"/assets/index-abc.css", "body{}", forever, "text/css"},
		{"/favicon.ico", "icon", revalidate, ""},
	}
	for _, c := range cases {
		rec := get(t, h, http.MethodGet, c.path)
		if rec.Code != http.StatusOK || rec.Body.String() != c.body {
			t.Errorf("%s: status %d, body %q", c.path, rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Cache-Control"); got != c.cache {
			t.Errorf("%s: Cache-Control = %q, want %q", c.path, got, c.cache)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, c.contentType) {
			t.Errorf("%s: Content-Type = %q, want %q", c.path, got, c.contentType)
		}
	}
}

func TestStaleAssetIsNotAnsweredWithTheApp(t *testing.T) {
	rec := get(t, New(built()), http.MethodGet, "/assets/index-old.js")
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<html>app") {
		t.Errorf("status %d, body %q", rec.Code, rec.Body)
	}
}

func TestPathsStayInsideTheBuild(t *testing.T) {
	h := New(built())
	for _, target := range []string{"/../secret", "/assets/../../secret", "/%2e%2e/secret"} {
		rec := get(t, h, http.MethodGet, target)
		// Whatever such a path resolves to, it is the app or nothing.
		if rec.Code == http.StatusOK && rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: served %q", target, rec.Body)
		}
	}
}

func TestOnlyReads(t *testing.T) {
	h := New(built())
	if rec := get(t, h, http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d, want 405", rec.Code)
	}
	head := get(t, h, http.MethodHead, "/books/42")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, %d bytes", head.Code, head.Body.Len())
	}
}

func TestSaysSoWhenTheAppWasNotBuilt(t *testing.T) {
	rec := get(t, New(fstest.MapFS{".gitkeep": {}}), http.MethodGet, "/")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "make web-embed") {
		t.Errorf("status %d, body %q", rec.Code, rec.Body)
	}
}

func TestEmbeddedDirectoryIsServable(t *testing.T) {
	rec := get(t, Handler(), http.MethodGet, "/")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("status %d from the embedded build", rec.Code)
	}
}
