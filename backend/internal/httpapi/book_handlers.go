package httpapi

import (
	"cmp"
	"encoding/hex"
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
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

type listBooksQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
	Sort    string `query:"sort" enum:"title,author,added" doc:"What the books are ordered by; title when left out."`
	Order   string `query:"order" enum:"asc,desc" doc:"The direction; asc when left out."`
	Cursor  string `query:"cursor" doc:"The nextCursor of the page before."`
	Limit   int    `query:"limit" doc:"How many books a page holds, at most 200; 50 when left out."`
}

// bookSummary is a book as a list shows it.
type bookSummary struct {
	ID            uuid.UUID `json:"id"`
	LibraryID     uuid.UUID `json:"libraryId"`
	Title         string    `json:"title"`
	Subtitle      string    `json:"subtitle,omitempty"`
	Authors       []string  `json:"authors"`
	Series        string    `json:"series,omitempty"`
	SeriesIndex   *float64  `json:"seriesIndex,omitempty"`
	PublishedYear *int32    `json:"publishedYear,omitempty"`
	// CoverKey is what a cover's address carries as v, so that it can be
	// kept for good. Left out when the book has no cover.
	CoverKey string    `json:"coverKey,omitempty"`
	Formats  []string  `json:"formats"`
	AddedAt  time.Time `json:"addedAt"`
}

type bookList struct {
	Books []bookSummary `json:"books"`
	// NextCursor asks for the page after this one; left out on the last.
	NextCursor string `json:"nextCursor,omitempty"`
}

type contributor struct {
	Name string `json:"name"`
	// Role is author, narrator, translator, editor or illustrator.
	Role string `json:"role"`
}

type identifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type bookFile struct {
	ID     uuid.UUID `json:"id"`
	Kind   string    `json:"kind"`
	Format string    `json:"format"`
	// Name is the file's name without its folders.
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Missing is set when the last scan did not find the file.
	Missing    bool   `json:"missing"`
	DurationMS *int64 `json:"durationMs,omitempty"`
	PageCount  *int32 `json:"pageCount,omitempty"`
	// HasText is false for a scan without a text layer and for audio.
	HasText *bool `json:"hasText,omitempty"`
	DRM     bool  `json:"drm"`
	// Part is the file's place among the parts of an audiobook.
	Part *int32 `json:"part,omitempty"`
	// RelPath is where the file lies in its library, for those who manage
	// storage.
	RelPath *string `json:"relPath,omitempty"`
}

type bookDetail struct {
	ID          uuid.UUID `json:"id"`
	LibraryID   uuid.UUID `json:"libraryId"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle,omitempty"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language,omitempty"`
	// Published is as much of the date as is known: "2010", "2010-08" or
	// "2010-08-31".
	Published    string        `json:"published,omitempty"`
	Publisher    string        `json:"publisher,omitempty"`
	Series       string        `json:"series,omitempty"`
	SeriesIndex  *float64      `json:"seriesIndex,omitempty"`
	PageCount    *int32        `json:"pageCount,omitempty"`
	Contributors []contributor `json:"contributors"`
	Tags         []string      `json:"tags"`
	Identifiers  []identifier  `json:"identifiers"`
	Files        []bookFile    `json:"files"`
	CoverKey     string        `json:"coverKey,omitempty"`
	// DurationMS is the length of the audiobook, all its parts together.
	DurationMS *int64    `json:"durationMs,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
}

// defaultPage is how many books a page holds when the client does not say.
const defaultPage = 50

func (s *Server) listBooks(w http.ResponseWriter, r *http.Request) error {
	var q listBooksQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	p := catalog.ListParams{Order: cmp.Or(q.Sort, catalog.OrderTitle), Desc: q.Order == "desc", After: q.Cursor, Limit: cmp.Or(q.Limit, defaultPage)}
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		}
		p.LibraryID = &id
	}
	page, err := s.Books.List(r.Context(), library.ScopeOf(*UserFrom(r.Context())), p)
	if errors.Is(err, catalog.ErrBadCursor) {
		return ErrValidation(map[string]string{"cursor": "This cursor belongs to another list; start again from the first page."})
	}
	if err != nil {
		return err
	}
	out := bookList{Books: make([]bookSummary, len(page.Books)), NextCursor: page.Next}
	for i, b := range page.Books {
		out.Books[i] = bookSummary{
			ID: b.ID, LibraryID: b.LibraryID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
			Series: b.Series, SeriesIndex: b.SeriesIndex, PublishedYear: b.PublishedYear,
			CoverKey: b.CoverKey, Formats: b.Formats, AddedAt: b.AddedAt,
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) getBook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	b, err := s.Books.Get(r.Context(), library.ScopeOf(*user), id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	out := bookDetail{
		ID: b.ID, LibraryID: b.LibraryID, Title: b.Title, Subtitle: b.Subtitle, Description: b.Description,
		Language: b.Language, Publisher: b.Publisher, Series: b.Series, SeriesIndex: b.SeriesIndex,
		PageCount: b.PageCount, CoverKey: b.CoverKey, AddedAt: b.AddedAt,
		Contributors: []contributor{}, Tags: []string{}, Identifiers: []identifier{}, Files: []bookFile{},
	}
	if b.PublishedOn != nil {
		layout := map[string]string{catalog.PrecisionYear: "2006", catalog.PrecisionMonth: "2006-01"}[b.PublishedPrecision]
		out.Published = b.PublishedOn.Format(cmp.Or(layout, "2006-01-02"))
	}
	for _, c := range b.Contributors {
		out.Contributors = append(out.Contributors, contributor{Name: c.Name, Role: c.Role})
	}
	out.Tags = append(out.Tags, b.Tags...)
	// The same ISBN in two files is one identifier to whoever reads it.
	seen := map[identifier]bool{}
	for _, ident := range b.Identifiers {
		if i := (identifier{Type: ident.Type, Value: ident.Value}); !seen[i] {
			seen[i] = true
			out.Identifiers = append(out.Identifiers, i)
		}
	}
	manages := auth.Allows(user.Role, auth.StorageManage)
	for _, f := range b.Files {
		file := bookFile{
			ID: f.ID, Kind: f.Kind, Format: f.Format, Name: path.Base(f.RelPath), Size: f.Size,
			Missing: f.Missing, DurationMS: f.DurationMS, PageCount: f.PageCount, HasText: f.HasText,
			DRM: f.DRM, Part: f.PartIndex,
		}
		if manages {
			file.RelPath = &f.RelPath
		}
		if f.DurationMS != nil {
			total := derefInt64(out.DurationMS) + *f.DurationMS
			out.DurationMS = &total
		}
		out.Files = append(out.Files, file)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// mediaTypes are what a file of each format is sent as.
var mediaTypes = map[string]string{
	"epub": "application/epub+zip", "pdf": "application/pdf",
	"mobi": "application/x-mobipocket-ebook", "azw": "application/vnd.amazon.ebook",
	"azw3": "application/vnd.amazon.ebook",
	"m4b":  "audio/mp4", "m4a": "audio/mp4", "mp3": "audio/mpeg", "flac": "audio/flac",
	"ogg": "audio/ogg", "opus": "audio/ogg",
}

// downloadFile sends a file as it lies on disk. Range requests are answered,
// so a download that broke off goes on where it stopped, and a player can
// seek. A file is not a request: it may take hours, and the server's write
// deadline is lifted for it.
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	file, f, err := s.Books.OpenFile(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrNotFound("There is no such file.")
	case errors.Is(err, catalog.ErrFileGone):
		return ErrNotFound("The file is no longer in the library's folder.")
	case err != nil:
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", cmp.Or(mediaTypes[file.Format], "application/octet-stream"))
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	h.Set("Cache-Control", "private, no-cache")
	if len(file.SHA256) > 0 {
		h.Set("ETag", `"`+hex.EncodeToString(file.SHA256)+`"`)
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
	return nil
}
