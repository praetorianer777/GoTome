package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

// progressView is where the caller is in a book in one medium.
type progressView struct {
	// FileID is the file the position is in.
	FileID *uuid.UUID `json:"fileId,omitempty"`
	// Locator is the exact place, as the reader wrote it: an EPUB CFI, a
	// PDF page, a millisecond of the audio.
	Locator string `json:"locator"`
	// Fraction is how much of the book lies before the position, 0 to 1.
	Fraction   float64 `json:"fraction"`
	Chapter    string  `json:"chapter,omitempty"`
	Page       *int32  `json:"page,omitempty"`
	PositionMS *int64  `json:"positionMs,omitempty"`
	// ClientID is the device or browser that wrote it.
	ClientID string `json:"clientId"`
	// UpdatedAt is when it was written; a client sends it back as basedOn.
	UpdatedAt time.Time `json:"updatedAt"`
}

func progressViewOf(p reading.Progress) *progressView {
	return &progressView{
		FileID: p.FileID, Locator: p.Locator, Fraction: p.Fraction, Chapter: p.Chapter, Page: p.Page,
		PositionMS: p.PositionMS, ClientID: p.ClientID, UpdatedAt: p.UpdatedAt,
	}
}

// progressState is where the caller is in a book, in its text and in its
// audio, each on its own.
type progressState struct {
	Ebook *progressView `json:"ebook,omitempty"`
	Audio *progressView `json:"audio,omitempty"`
	// Finishes is how often the caller read the book to its end.
	Finishes int `json:"finishes"`
}

func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	state, err := s.Reading.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, reading.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	out := progressState{Finishes: state.Finishes}
	for _, p := range state.Progress {
		if p.Medium == reading.MediumAudio {
			out.Audio = progressViewOf(p)
		} else {
			out.Ebook = progressViewOf(p)
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// progressUpdate is a position a client reports.
type progressUpdate struct {
	FileID     *uuid.UUID `json:"fileId,omitempty"`
	Locator    string     `json:"locator"`
	Fraction   float64    `json:"fraction"`
	Chapter    string     `json:"chapter,omitempty"`
	Page       *int32     `json:"page,omitempty"`
	PositionMS *int64     `json:"positionMs,omitempty"`
	// ClientID names the device or browser, the same on every write.
	ClientID string `json:"clientId"`
	// BasedOn is the updatedAt of the position the client last read; left
	// out when it read none.
	BasedOn *time.Time `json:"basedOn,omitempty"`
	// Force writes even over a further position another client wrote.
	Force bool `json:"force,omitempty"`
}

// progressSaved answers a position. When another client wrote a further
// one since this client last read, saved is false and progress is that
// further position, for the person to choose between; sent again with
// force, the client's is written.
type progressSaved struct {
	Saved    bool          `json:"saved"`
	Progress *progressView `json:"progress"`
}

func (s *Server) putProgress(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	medium := chi.URLParam(r, "medium")
	if !slices.Contains(reading.Media, medium) {
		return ErrNotFound("There is no such medium; it is ebook or audio.")
	}
	var req progressUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	p, saved, err := s.Reading.Save(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, reading.Update{
		Progress: reading.Progress{
			Medium: medium, FileID: req.FileID, Locator: req.Locator, Fraction: req.Fraction, Chapter: req.Chapter,
			Page: req.Page, PositionMS: req.PositionMS, ClientID: req.ClientID,
		},
		BasedOn: req.BasedOn, Force: req.Force,
	})
	var invalid reading.Problems
	switch {
	case errors.Is(err, reading.ErrNotFound):
		return ErrNotFound("There is no such book.")
	case errors.As(err, &invalid):
		return ErrValidation(invalid)
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, progressSaved{Saved: saved, Progress: progressViewOf(p)})
	return nil
}
