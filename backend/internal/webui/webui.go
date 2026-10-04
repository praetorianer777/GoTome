// Package webui serves the web app from inside the binary, so a deployment is
// one container for the app and its API.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
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

// New serves a built web app from dist. Files are served as they are; every
// other path gets index.html, because the app routes in the browser and a
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
			files.ServeHTTP(w, r)
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(index)
	})
}

func isFile(dist fs.FS, name string) bool {
	info, err := fs.Stat(dist, name)
	return err == nil && !info.IsDir()
}
