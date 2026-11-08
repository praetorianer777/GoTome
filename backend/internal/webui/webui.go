// Package webui serves the web app from inside the binary, so a deployment is
// one container for the app and its API.
package webui

import (
	"cmp"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// dist is filled by the image build, and by make web-embed in a checkout.
// Without either it holds only a placeholder, and Handler says so.
//
//go:embed all:dist
var embedded embed.FS

// ContentSecurityPolicy goes with every response of the web app. The ebook
// reader shows a book's pages in frames of blob: documents, which take the
// policy of the page that made them: a script a book carries does not run,
// and nothing in a book reaches the network, as foliate-js requires
// (web/vendor/foliate-js/README.md). WebAssembly is for PDF.js's image
// decoders.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline' blob:; img-src 'self' blob: data:; font-src 'self' blob: data:; " +
	"media-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'self' blob:; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'"

func init() {
	// Go's table does not know the web app manifest, and a browser ignores
	// one served as plain text.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Handler serves the embedded web app.
func Handler() http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		// The directory is part of the source tree; embed fails the build
		// without it.
		panic(err)
	}
	return New(dist)
}

// New serves a built web app from dist. Files are served as they are, or as
// the compressed copy beside them that the request accepts; every other path
// gets index.html, because the app routes in the browser and a
// reload of /books/42 must load the app, not a 404.
func New(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" && isFile(dist, name) {
			// Vite names everything under assets/ after its content, so such a
			// file never changes and may be kept for good.
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if !serveEncoded(w, r, dist, name) {
				files.ServeHTTP(w, r)
			}
			return
		}

		// A missing file under assets/ is a stale reference to an old build.
		// Answering it with the app's HTML would hand a browser markup where
		// it expects a script.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}

		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "The web app is not part of this build. Build it with make web-embed, or use the published image.", http.StatusNotFound)
			return
		}
		// The page names the current build's assets, so it must be revalidated.
		w.Header().Set("Cache-Control", "no-cache")
		if serveEncoded(w, r, dist, "index.html") {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(index)
	})
}

// encodings are the compressed copies the build leaves beside a file, the
// best first (web/scripts/compress.mjs).
var encodings = []struct{ coding, suffix string }{{"br", ".br"}, {"gzip", ".gz"}}

// serveEncoded sends the best compressed copy of name the request accepts,
// and says whether there was one. A copy is a representation of its own, so
// a range and a cache are of its bytes; Vary keeps caches from handing one
// to a client that did not ask for it.
func serveEncoded(w http.ResponseWriter, r *http.Request, dist fs.FS, name string) bool {
	accept := r.Header.Get("Accept-Encoding")
	varies := false
	for _, e := range encodings {
		if !isFile(dist, name+e.suffix) {
			continue
		}
		if !varies {
			w.Header().Add("Vary", "Accept-Encoding")
			varies = true
		}
		if !Accepts(accept, e.coding) {
			continue
		}
		f, err := dist.Open(name + e.suffix)
		if err != nil {
			return false
		}
		defer f.Close()
		content, ok := f.(io.ReadSeeker)
		if !ok {
			return false
		}
		h := w.Header()
		h.Set("Content-Type", cmp.Or(mime.TypeByExtension(path.Ext(name)), "application/octet-stream"))
		h.Set("Content-Encoding", e.coding)
		http.ServeContent(w, r, "", time.Time{}, content)
		return true
	}
	return false
}

// Accepts says whether an Accept-Encoding header allows a content
// coding: named, or matched by *, with a weight above zero.
func Accepts(header, coding string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != coding && name != "*" {
			continue
		}
		weight := 1.0
		for _, p := range strings.Split(params, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && strings.EqualFold(k, "q") {
				if q, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					weight = q
				}
			}
		}
		if name == coding {
			return weight > 0
		}
		star = weight > 0
	}
	return star
}

func isFile(dist fs.FS, name string) bool {
	info, err := fs.Stat(dist, name)
	return err == nil && !info.IsDir()
}
