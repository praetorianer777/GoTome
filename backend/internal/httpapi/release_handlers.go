package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/releases"
)

// tracker is an author or a series the caller follows for new books.
type tracker struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// PolledAt is when the sources were last asked about it; left out
	// until they have been.
	PolledAt *time.Time `json:"polledAt,omitempty"`
}

func trackerOf(t releases.Tracker) tracker {
	return tracker{ID: t.ID, Kind: t.Kind, Name: t.Name, CreatedAt: t.CreatedAt, PolledAt: t.PolledAt}
}

type trackerList struct {
	Trackers []tracker `json:"trackers"`
}

// followRequest names an author or a series to follow, as the library
// writes it.
type followRequest struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (s *Server) listTrackers(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Releases.Trackers(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := trackerList{Trackers: make([]tracker, len(list))}
	for i, t := range list {
		out.Trackers[i] = trackerOf(t)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) follow(w http.ResponseWriter, r *http.Request) error {
	var req followRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.Kind != releases.KindAuthor && req.Kind != releases.KindSeries {
		return ErrValidation(map[string]string{"kind": "Follow an author or a series."})
	}
	t, err := s.Releases.Follow(r.Context(), UserFrom(r.Context()).ID, req.Kind, req.Name)
	switch {
	case errors.Is(err, releases.ErrBadName):
		return ErrValidation(map[string]string{"name": "Name the author or the series to follow."})
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusCreated, trackerOf(t))
	return nil
}

func (s *Server) unfollow(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "trackerId", "tracker")
	if err != nil {
		return err
	}
	if err := s.Releases.Unfollow(r.Context(), UserFrom(r.Context()).ID, id); err != nil {
		if errors.Is(err, releases.ErrNotFound) {
			return ErrNotFound("You do not follow that.")
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type releasesQuery struct {
	When string `query:"when" enum:"upcoming,recent" doc:"upcoming (the default) lists the books still to come, the soonest first; recent those out within the last year, the newest first."`
}

// release is a book of an author or a series the caller follows, as the
// sources list it.
type release struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Authors     []string  `json:"authors"`
	Series      *string   `json:"series,omitempty"`
	SeriesIndex *float64  `json:"seriesIndex,omitempty"`
	// Date is when it comes or came out, as exactly as precision says: the
	// day, or the first day of the month or the year. Both are left out
	// where no source knows.
	Date      *string `json:"date,omitempty"`
	Precision *string `json:"precision,omitempty"`
	// Source is the provider that listed it first.
	Source string `json:"source"`
	// CoverToken fetches its cover through GET /metadata/covers/{token}.
	CoverToken *string `json:"coverToken,omitempty"`
	// Following is what the caller follows that lists it.
	Following following `json:"following"`
	// InLibrary is a book of a library the caller sees that is this one.
	InLibrary bool `json:"inLibrary"`
}

type following struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type releaseList struct {
	Releases []release `json:"releases"`
}

func (s *Server) listReleases(w http.ResponseWriter, r *http.Request) error {
	var q releasesQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	list, err := s.Releases.Releases(r.Context(), library.ScopeOf(*UserFrom(r.Context())), q.When != "recent")
	if err != nil {
		return err
	}
	out := releaseList{Releases: make([]release, len(list))}
	for i, rel := range list {
		out.Releases[i] = release{
			ID: rel.ID, Title: rel.Title, Authors: rel.Authors, Series: rel.Series, SeriesIndex: rel.SeriesIndex,
			Precision: rel.Precision, Source: rel.Provider, InLibrary: rel.InLibrary,
			Following: following{Kind: rel.SubjectKind, Name: rel.SubjectName},
		}
		if out.Releases[i].Authors == nil {
			out.Releases[i].Authors = []string{}
		}
		if rel.Date != nil {
			date := rel.Date.Format(time.DateOnly)
			out.Releases[i].Date = &date
		}
		if rel.CoverURL != nil && s.Metadata != nil && s.Metadata.Has(rel.Provider) {
			token := s.Metadata.CoverToken(rel.Provider, *rel.CoverURL)
			out.Releases[i].CoverToken = &token
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
