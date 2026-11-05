package httpapi

import (
	"errors"
	"fmt"
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

// feed is an RSS or Atom feed the caller follows for new books.
type feed struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
	// Title is the feed's own, once it has answered.
	Title *string `json:"title,omitempty"`
	// Keywords and Authors are what an item must name: one of the
	// keywords, if any, and one of the authors, if any.
	Keywords []string `json:"keywords"`
	Authors  []string `json:"authors"`
	// CheckedAt is when the feed last answered; left out until it has.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// Failures are the fetches in a row that failed, LastError the last
	// one's reason.
	Failures  int     `json:"failures"`
	LastError *string `json:"lastError,omitempty"`
	// NextFetchAt is when it is fetched next; left out once it failed too
	// often, until it is asked for again.
	NextFetchAt *time.Time `json:"nextFetchAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func feedOf(f releases.Feed) feed {
	return feed{
		ID: f.ID, URL: f.URL, Title: f.Title, Keywords: f.Keywords, Authors: f.Authors,
		CheckedAt: f.CheckedAt, Failures: f.Failures, LastError: f.LastError,
		NextFetchAt: f.NextFetchAt, CreatedAt: f.CreatedAt,
	}
}

type feedList struct {
	Feeds []feed `json:"feeds"`
}

// feedRequest is a feed to follow. Following an address again gives it the
// new filters.
type feedRequest struct {
	URL      string   `json:"url"`
	Keywords []string `json:"keywords,omitempty"`
	Authors  []string `json:"authors,omitempty"`
}

// feedItem is an item of one of the caller's feeds that names what its
// filters ask for.
type feedItem struct {
	FeedID uuid.UUID `json:"feedId"`
	// Feed is the feed's title, or its address.
	Feed  string `json:"feed"`
	Title string `json:"title"`
	// Link is the item's page, an http or https address.
	Link      *string    `json:"link,omitempty"`
	Authors   []string   `json:"authors"`
	Published *time.Time `json:"published,omitempty"`
	SeenAt    time.Time  `json:"seenAt"`
}

type feedItemList struct {
	Items []feedItem `json:"items"`
}

func (s *Server) listFeeds(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Releases.Feeds(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := feedList{Feeds: make([]feed, len(list))}
	for i, f := range list {
		out.Feeds[i] = feedOf(f)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) error {
	var req feedRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	f, err := s.Releases.AddFeed(r.Context(), UserFrom(r.Context()).ID, req.URL, req.Keywords, req.Authors)
	switch {
	case errors.Is(err, releases.ErrBadFeedURL):
		return ErrValidation(map[string]string{"url": "Give the feed's http or https address."})
	case errors.Is(err, releases.ErrTooManyFeeds):
		return ErrValidation(map[string]string{"url": fmt.Sprintf("You follow %d feeds, which is as many as one may.", releases.MaxFeeds)})
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusCreated, feedOf(f))
	return nil
}

func (s *Server) removeFeed(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "feedId", "feed")
	if err != nil {
		return err
	}
	if err := s.Releases.RemoveFeed(r.Context(), UserFrom(r.Context()).ID, id); err != nil {
		if errors.Is(err, releases.ErrNotFound) {
			return ErrNotFound("There is no such feed.")
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) retryFeed(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "feedId", "feed")
	if err != nil {
		return err
	}
	f, err := s.Releases.RetryFeed(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, releases.ErrNotFound) {
		return ErrNotFound("There is no such feed.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, feedOf(f))
	return nil
}

func (s *Server) listFeedItems(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Releases.FeedEntries(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := feedItemList{Items: make([]feedItem, len(list))}
	for i, e := range list {
		out.Items[i] = feedItem{
			FeedID: e.FeedID, Feed: e.Feed, Title: e.Title, Link: e.Link, Authors: e.Authors,
			Published: e.Published, SeenAt: e.SeenAt,
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
