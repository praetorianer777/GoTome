package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

func askCompressed(t *testing.T, h http.Handler, target, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, body io.Reader) string {
	t.Helper()
	zr, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLargeJSONIsSentCompressed(t *testing.T) {
	large := `{"books":"` + strings.Repeat("a", 5000) + `"}`
	h := compressed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, large[:10])
		_, _ = io.WriteString(w, large[10:])
	}))

	rec := askCompressed(t, h, "/", "br, gzip")
	if rec.Code != http.StatusCreated || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	if rec.Body.Len() >= len(large)/10 {
		t.Errorf("%d bytes sent for %d", rec.Body.Len(), len(large))
	}
	if got := gunzip(t, rec.Body); got != large {
		t.Errorf("decompressed to %d bytes, want %d", len(got), len(large))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary %q", rec.Header().Get("Vary"))
	}

	for _, accept := range []string{"", "br", "gzip;q=0"} {
		plain := askCompressed(t, h, "/", accept)
		if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != large {
			t.Errorf("Accept-Encoding %q: compressed %q", accept, plain.Header().Get("Content-Encoding"))
		}
	}
}

func TestOnlyLargeJSONIsCompressed(t *testing.T) {
	big := strings.Repeat("x", 4*compressFrom)
	cases := []struct {
		name, contentType, body string
		status                  int
	}{
		{"small JSON", "application/json", `{"ok":true}`, http.StatusOK},
		{"an image", "image/jpeg", big, http.StatusOK},
		{"a file", "application/epub+zip", big, http.StatusOK},
		{"events", "text/event-stream", big, http.StatusOK},
		{"no content", "application/json", "", http.StatusNoContent},
	}
	for _, c := range cases {
		h := compressed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", c.contentType)
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}))
		rec := askCompressed(t, h, "/", "gzip")
		if rec.Code != c.status || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != c.body {
			t.Errorf("%s: status %d, encoding %q, %d bytes", c.name, rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
		}
	}
}

func TestAFlushSendsWhatIsHeld(t *testing.T) {
	flushed := make(chan string, 1)
	h := compressed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"a":1}`)
		_ = http.NewResponseController(w).Flush()
		flushed <- w.(*compressWriter).ResponseWriter.(*httptest.ResponseRecorder).Body.String()
	}))
	rec := askCompressed(t, h, "/", "gzip")
	if got := <-flushed; got != `{"a":1}` || !rec.Flushed {
		t.Errorf("after the flush %q went out, flushed %v", got, rec.Flushed)
	}
}

func TestRoutesThatSendFilesAreNotCompressed(t *testing.T) {
	big := strings.Repeat("x", 4*compressFrom)
	write := func(contentType string) HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) error {
			w.Header().Set("Content-Type", contentType)
			_, _ = io.WriteString(w, big)
			return nil
		}
	}
	r := chi.NewRouter()
	mount(r, []Route{
		{Method: http.MethodGet, Path: "/json", Reads: ReadsNoLibrary, Permission: auth.Public, Handler: write("application/json")},
		// A file of a JSON type is still a file, sent as its bytes.
		{Method: http.MethodGet, Path: "/file", Produces: "application/json", Reads: ReadsNoLibrary, Permission: auth.Public, Handler: write("application/json")},
	})
	if got := askCompressed(t, r, "/json", "gzip").Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("JSON route: Content-Encoding %q", got)
	}
	file := askCompressed(t, r, "/file", "gzip")
	if file.Header().Get("Content-Encoding") != "" || file.Header().Get("Vary") != "" || file.Body.String() != big {
		t.Errorf("file route: headers %v", file.Header())
	}
}
