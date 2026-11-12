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
		"sw.js":                {Data: []byte("self.x=1")},
		"manifest.webmanifest": {Data: []byte("{}")},
		"assets/art-abc.webp":  {Data: []byte("RIFF....WEBP")},
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
		// The service worker is looked for again at every visit, or a new
		// build would never reach a browser that has an old one.
		{"/sw.js", "self.x=1", revalidate, "javascript"},
		{"/manifest.webmanifest", "{}", revalidate, "application/manifest+json"},
		{"/assets/art-abc.webp", "RIFF....WEBP", forever, "image/webp"},
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

func TestEveryResponseCarriesThePolicyThatKeepsBookScriptsFromRunning(t *testing.T) {
	h := New(built())
	for _, target := range []string{"/", "/books/42/read/7", "/assets/index-abc.js", "/favicon.ico"} {
		policy := get(t, h, http.MethodGet, target).Header().Get("Content-Security-Policy")
		directives := map[string]string{}
		for _, d := range strings.Split(policy, ";") {
			name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
			directives[name] = value
		}
		if s := directives["script-src"]; !strings.HasPrefix(s, "'self'") || strings.Contains(s, "unsafe-inline") ||
			strings.Contains(s, "blob:") || strings.Contains(s, "'unsafe-eval'") {
			t.Errorf("%s: script-src %q lets a book's script run", target, s)
		}
		if directives["connect-src"] != "'self'" || directives["default-src"] != "'self'" {
			t.Errorf("%s: a book could reach the network: %q", target, policy)
		}
		if directives["object-src"] != "'none'" {
			t.Errorf("%s: object-src %q", target, directives["object-src"])
		}
	}
}

func compressedBuild() fstest.MapFS {
	files := built()
	files["assets/index-abc.js.br"] = &fstest.MapFile{Data: []byte("br-bytes")}
	files["assets/index-abc.js.gz"] = &fstest.MapFile{Data: []byte("gz-bytes")}
	files["assets/index-abc.css.gz"] = &fstest.MapFile{Data: []byte("css-gz")}
	files["index.html.br"] = &fstest.MapFile{Data: []byte("html-br")}
	return files
}

func TestSendsTheBestCompressedCopyTheRequestAccepts(t *testing.T) {
	h := New(compressedBuild())
	cases := []struct {
		target, accept, body, encoding string
	}{
		{"/assets/index-abc.js", "gzip, deflate, br", "br-bytes", "br"},
		{"/assets/index-abc.js", "gzip", "gz-bytes", "gzip"},
		{"/assets/index-abc.js", "br;q=0, gzip;q=0.5", "gz-bytes", "gzip"},
		{"/assets/index-abc.js", "*", "br-bytes", "br"},
		{"/assets/index-abc.js", "", "console.log(1)", ""},
		{"/assets/index-abc.js", "identity", "console.log(1)", ""},
		{"/assets/index-abc.js", "gzip;q=0, br;q=0", "console.log(1)", ""},
		{"/assets/index-abc.css", "br, gzip", "css-gz", "gzip"},
		{"/assets/index-abc.css", "br", "body{}", ""},
		{"/books/42", "br", "html-br", "br"},
		{"/books/42", "gzip", "<html>app</html>", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.target, nil)
		if c.accept != "" {
			req.Header.Set("Accept-Encoding", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		what := c.target + " (" + c.accept + ")"
		if rec.Code != http.StatusOK || rec.Body.String() != c.body {
			t.Errorf("%s: status %d, body %q, want %q", what, rec.Code, rec.Body, c.body)
		}
		if got := rec.Header().Get("Content-Encoding"); got != c.encoding {
			t.Errorf("%s: Content-Encoding %q, want %q", what, got, c.encoding)
		}
		if got := rec.Header().Values("Vary"); len(got) != 1 || got[0] != "Accept-Encoding" {
			t.Errorf("%s: Vary %q", what, got)
		}
		// The type is the file's, not that of the compressed copy.
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/") {
			t.Errorf("%s: Content-Type %q", what, got)
		}
		cache := forever
		if !strings.HasPrefix(c.target, "/assets/") {
			cache = revalidate
		}
		if got := rec.Header().Get("Cache-Control"); got != cache {
			t.Errorf("%s: Cache-Control %q", what, got)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no policy", what)
		}
	}
}

func TestAFileWithoutCopiesDoesNotVary(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/art-abc.webp", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	New(compressedBuild()).ServeHTTP(rec, req)
	if rec.Body.String() != "RIFF....WEBP" || rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != "" {
		t.Errorf("body %q, headers %v", rec.Body, rec.Header())
	}
}

func TestTellsThePageItsBasePath(t *testing.T) {
	fs := built()
	fs["index.html"] = &fstest.MapFile{Data: []byte(`<head><base href="/"><meta name="gotome-base" content="/"></head>`)}
	fs["index.html.gz"] = &fstest.MapFile{Data: []byte("not the page")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/books/42", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	NewAt(fs, "/gotome/").ServeHTTP(rec, req)
	want := `<head><base href="/gotome/"><meta name="gotome-base" content="/gotome/"></head>`
	if rec.Body.String() != want || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("got %q (%q), want %q", rec.Body, rec.Header().Get("Content-Encoding"), want)
	}
}
