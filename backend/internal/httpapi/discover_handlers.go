package httpapi

import (
	"cmp"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/discover"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// discoverReason says why a book is offered.
type discoverReason struct {
	Kind string `json:"kind" enum:"author,series_next,series_gap,series_more,genre" doc:"author: another work of an author the caller rated highly; series_next: the volume after the last one they read; series_gap: a volume their libraries lack among those they hold; series_more: a later volume; genre: a book of a genre they read most."`
	// Name is the author or the series; for a genre, its key.
	Name string `json:"name"`
	// Stars is the best rating the caller gave the author's books, 0 where none.
	Stars int `json:"stars"`
	// Because are books of the caller's that this one is near.
	Because []string `json:"because"`
}

type discoverItem struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Authors     []string  `json:"authors"`
	Series      string    `json:"series,omitempty"`
	SeriesIndex *float64  `json:"seriesIndex,omitempty"`
	// Year is the year the book came out, where the provider says.
	Year        int            `json:"year,omitempty"`
	Description string         `json:"description,omitempty"`
	Provider    string         `json:"provider"`
	CoverToken  string         `json:"coverToken,omitempty"`
	Reason      discoverReason `json:"reason"`
	// Wish is what POST /books/wishes takes to put the book on the caller's wishlist.
	Wish wishRequest `json:"wish"`
}

type discoverCount struct {
	Asked   int `json:"asked"`
	Planned int `json:"planned"`
}

type discoverBackoff struct {
	Provider string    `json:"provider"`
	Until    time.Time `json:"until"`
}

// discoverProgress is how far the caller's own run is.
type discoverProgress struct {
	// StartedAt is when the run began; absent while it is asked for and no pass has begun it.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// Planned is false until a pass has worked out what to ask; the counts are then zero.
	Planned  bool          `json:"planned"`
	Authors  discoverCount `json:"authors"`
	Series   discoverCount `json:"series"`
	Genres   discoverCount `json:"genres"`
	Activity string        `json:"activity" enum:"working,waiting" doc:"working: a pass is asking the providers now; waiting: the run goes on at the next pass."`
	// NextPassAt is about when a waiting run is worked again; absent when that is at once or not known.
	NextPassAt *time.Time `json:"nextPassAt,omitempty"`
	// BackingOff are providers that asked to be left alone for now; the run goes on without them.
	BackingOff []discoverBackoff `json:"backingOff"`
}

type discoverList struct {
	State string `json:"state" enum:"off,offline,never,running,ready" doc:"off: an administrator has not switched Discover on; offline: this server asks no provider; never: the caller has not asked for a run; running: one is waiting or under way; ready: the last one has ended."`
	// FinishedAt is when the last run ended.
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Items      []discoverItem `json:"items"`
	// Progress is there while state is running.
	Progress *discoverProgress `json:"progress,omitempty"`
}

// listDiscover offers the caller books beyond the libraries they see, from
// their own taste. Only the caller's.
func (s *Server) listDiscover(w http.ResponseWriter, r *http.Request) error {
	scope := library.ScopeOf(*UserFrom(r.Context()))
	view, err := s.Discover.List(r.Context(), scope)
	if err != nil {
		return err
	}
	out := discoverList{State: view.State, FinishedAt: view.FinishedAt, Items: []discoverItem{}}
	for _, it := range view.Items {
		out.Items = append(out.Items, s.discoverItemOf(it))
	}
	if p := view.Progress; p != nil {
		out.Progress = &discoverProgress{
			StartedAt: p.StartedAt, Planned: p.Planned, Activity: p.Activity, NextPassAt: p.NextPassAt,
			Authors: discoverCount(p.Authors), Series: discoverCount(p.Series), Genres: discoverCount(p.Genres),
			BackingOff: []discoverBackoff{},
		}
		for _, b := range p.BackingOff {
			out.Progress.BackingOff = append(out.Progress.BackingOff, discoverBackoff{Provider: b.Provider, Until: b.Until})
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) discoverItemOf(it discover.Item) discoverItem {
	rec := it.Record
	item := discoverItem{
		ID: it.ID, Title: rec.Title, Authors: []string{}, Series: rec.Series, SeriesIndex: rec.SeriesIndex,
		Description: rec.Description, Provider: rec.Provider,
		Reason: discoverReason{Kind: it.Reason.Kind, Name: it.Reason.Name, Stars: it.Reason.Stars, Because: append([]string{}, it.Reason.Because...)},
	}
	if len(rec.Published) >= 4 {
		for _, c := range rec.Published[:4] {
			item.Year = item.Year*10 + int(c-'0')
		}
		if item.Year < 1000 {
			item.Year = 0
		}
	}
	contributors := []contributor{}
	for _, c := range rec.Contributors {
		contributors = append(contributors, contributor{Name: c.Name, Role: cmp.Or(c.Role, catalog.RoleAuthor)})
		if c.Role == "" || c.Role == catalog.RoleAuthor {
			item.Authors = append(item.Authors, c.Name)
		}
	}
	identifiers := []identifier{}
	for _, id := range rec.Identifiers {
		identifiers = append(identifiers, identifier{Type: id.Type, Value: id.Value})
	}
	if rec.CoverURL != "" {
		item.CoverToken = s.Metadata.CoverToken(rec.Provider, rec.CoverURL)
	}
	wish := wishRequest{Provider: rec.Provider, CoverToken: item.CoverToken}
	wish.Title = &rec.Title
	wish.Contributors = &contributors
	wish.Identifiers = &identifiers
	tags := append([]string{}, rec.Tags...)
	wish.Tags = &tags
	for _, f := range []struct {
		to   **string
		from string
	}{{&wish.Subtitle, rec.Subtitle}, {&wish.Description, rec.Description}, {&wish.Language, rec.Language}, {&wish.Published, rec.Published}, {&wish.Publisher, rec.Publisher}} {
		if f.from != "" {
			v := f.from
			*f.to = &v
		}
	}
	if rec.Series != "" {
		wish.Series = &seriesPlace{Name: rec.Series, Index: rec.SeriesIndex}
	}
	if rec.PageCount != nil {
		wish.PageCount = rec.PageCount
	}
	item.Wish = wish
	return item
}

// requestDiscover asks for a run now.
func (s *Server) requestDiscover(w http.ResponseWriter, r *http.Request) error {
	err := s.Discover.Request(r.Context(), UserFrom(r.Context()).ID)
	switch {
	case errors.Is(err, discover.ErrOff):
		return ErrConflict("An administrator has not switched Discover on.")
	case errors.Is(err, discover.ErrOffline):
		return ErrConflict("This server asks no provider, so there is nothing to discover.")
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Server) dismissDiscovered(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "candidateId", "candidate")
	if err != nil {
		return err
	}
	err = s.Discover.Dismiss(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, discover.ErrNotFound) {
		return ErrNotFound("There is no such book to discover.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
