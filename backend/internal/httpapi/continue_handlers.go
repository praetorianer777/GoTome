package httpapi

import (
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

// continueOffer is a place to go on at in one medium.
type continueOffer struct {
	// Fraction is the place as a share of the whole book, 0 to 1.
	Fraction float64 `json:"fraction"`
	// PositionMS is the millisecond of the whole audiobook; for the audio
	// offer only.
	PositionMS *int64 `json:"positionMs,omitempty"`
	// FileID is the file the reader opens; for the ebook offer only.
	FileID *uuid.UUID `json:"fileId,omitempty"`
	// Chapter names the chapter the place is in, when the chapters of both
	// media were matched by their titles.
	Chapter string `json:"chapter,omitempty"`
	// Refined says the chapters placed it and not the share alone.
	Refined bool `json:"refined"`
}

// continuation is what the caller may go on with in the other medium: each
// field is there only when the other medium is further along. Nothing is
// moved by asking.
type continuation struct {
	Ebook *continueOffer `json:"ebook,omitempty"`
	Audio *continueOffer `json:"audio,omitempty"`
}

func (s *Server) getContinuation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	state, err := s.Reading.Get(r.Context(), scope, id)
	if errors.Is(err, reading.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	var sides reading.Sides
	for i := range state.Progress {
		if p := &state.Progress[i]; p.Medium == reading.MediumAudio {
			sides.Audio = p
		} else {
			sides.Ebook = p
		}
	}
	out := continuation{}
	if sides.Ebook == nil && sides.Audio == nil {
		writeJSON(w, r, http.StatusOK, out)
		return nil
	}

	tl, err := s.Books.AudioTimeline(r.Context(), scope, id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	// A part not yet read leaves the timeline short, and a place on it
	// would be mapped onto the wrong whole.
	if tl.Complete {
		sides.AudioMS = tl.DurationMS
	}
	for _, c := range tl.Chapters {
		sides.AudioChapters = append(sides.AudioChapters, reading.Landmark{
			Title: c.Title, Start: float64(c.StartMS) / float64(max(tl.DurationMS, 1)),
		})
	}

	book, err := s.Books.Get(r.Context(), scope, id)
	if err != nil {
		return err
	}
	var ebook *catalog.File
	for i := range book.Files {
		f := &book.Files[i]
		if !f.Missing && slices.Contains(ebookFormats, f.Format) &&
			(ebook == nil || catalog.FormatRank(f.Format) > catalog.FormatRank(ebook.Format)) {
			ebook = f
		}
	}
	sides.HasEbook = ebook != nil
	if ebook != nil && ebook.Format == "epub" && len(sides.AudioChapters) > 0 {
		sides.EbookChapters = s.ebookLandmarks(r, scope, ebook.ID)
	}

	offers := reading.Continue(sides)
	if o := offers.Ebook; o != nil {
		out.Ebook = &continueOffer{Fraction: o.Fraction, FileID: &ebook.ID, Chapter: o.Chapter, Refined: o.Refined}
	}
	if o := offers.Audio; o != nil {
		ms := int64(o.Fraction * float64(tl.DurationMS))
		out.Audio = &continueOffer{Fraction: o.Fraction, PositionMS: &ms, Chapter: o.Chapter, Refined: o.Refined}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// ebookFormats are the formats the web app's ebook reader opens.
var ebookFormats = []string{"epub", "mobi", "azw3", "azw"}

// ebookLandmarks reads an EPUB's chapters from its table of contents. A
// file that cannot be read has none, and the share of the whole is all the
// mapping then has.
func (s *Server) ebookLandmarks(r *http.Request, scope library.Scope, fileID uuid.UUID) []reading.Landmark {
	_, f, err := s.Books.OpenFile(r.Context(), scope, fileID)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	entries, err := epub.Outline(f, info.Size())
	if err != nil {
		return nil
	}
	out := make([]reading.Landmark, 0, len(entries))
	for _, e := range entries {
		out = append(out, reading.Landmark{Title: e.Title, Start: e.Start})
	}
	return out
}
