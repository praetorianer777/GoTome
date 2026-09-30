package httpapi

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// getBookCover sends a book's cover. A cover is stored under the hash of its
// image, so the address a client builds with that hash as v never shows
// another image and may be kept for good; without it the browser asks again
// each time and is mostly told that nothing changed.
func (s *Server) getBookCover(w http.ResponseWriter, r *http.Request) error {
	const gone = "There is no such cover."
	id, err := pathID(r, "bookId", "cover")
	if err != nil {
		return err
	}
	size, ok := covers.ParseSize(chi.URLParam(r, "size"))
	if !ok {
		return ErrNotFound(gone)
	}
	key, err := s.Books.CoverKey(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound(gone)
	}
	if err != nil {
		return err
	}
	path, err := s.Covers.Path(key, size)
	if errors.Is(err, covers.ErrNotFound) {
		return ErrNotFound(gone)
	}
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("ETag", `"`+key+"-"+string(size)+`"`)
	// Private: who may see a cover depends on who is asking.
	if r.URL.Query().Get("v") == key {
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, no-cache")
	}
	http.ServeContent(w, r, "", time.Time{}, f)
	return nil
}
