package releases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

const (
	// fetchEvery is how often a feed that answers is fetched.
	fetchEvery = time.Hour
	// maxBackoff is the longest wait after a failure, and maxFailures how
	// many in a row stop the feed until its owner asks for it again.
	maxBackoff  = 24 * time.Hour
	maxFailures = 8
	// MaxFeedBytes is the largest answer a feed may give.
	MaxFeedBytes = 4 << 20
	// feedTimeout bounds one fetch, the body included.
	feedTimeout = 30 * time.Second
	// MaxFeeds is how many feeds one person may follow.
	MaxFeeds = 50
	// maxFilters is how many keywords, and how many authors, a feed may
	// have.
	maxFilters = 20
	// forgetAfter is how long an item the feed no longer lists is kept.
	forgetAfter = 180 * 24 * time.Hour
	maxError    = 300
)

var (
	// ErrBadFeedURL is an address that is not one of the web.
	ErrBadFeedURL = errors.New("a feed is fetched from an http or https address")
	// ErrTooManyFeeds is a person who follows MaxFeeds already.
	ErrTooManyFeeds = errors.New("too many feeds")
	// ErrNoPublicServer is an address that names nothing on the public
	// internet.
	ErrNoPublicServer = errors.New("no server on the public internet answers at that address")
	// ErrFeedTooLarge is an answer past MaxFeedBytes.
	ErrFeedTooLarge = fmt.Errorf("the feed is larger than %d MiB", MaxFeedBytes>>20)
)

var feedAgent = "GOtome/" + version.Current().Version + " (+https://github.com/praetorianer777/GoTome)"

// NewFeedClient is the client feeds are fetched with: an address someone
// typed, so nothing inside is trusted.
func NewFeedClient() *http.Client {
	return outbound.NewClient(outbound.Options{Timeout: feedTimeout})
}

// Feed is a feed a person follows for new books.
type Feed struct {
	ID       uuid.UUID
	URL      string
	Title    *string
	Keywords []string
	Authors  []string
	// CheckedAt is when the feed last answered.
	CheckedAt *time.Time
	Failures  int
	LastError *string
	// NextFetchAt is nil for a feed that failed too often.
	NextFetchAt *time.Time
	CreatedAt   time.Time
}

func feedOf(r sqlc.FeedTracker) Feed {
	return Feed{
		ID: r.ID, URL: r.Url, Title: r.Title, Keywords: r.Keywords, Authors: r.Authors,
		CheckedAt: r.CheckedAt, Failures: int(r.Failures), LastError: r.LastError,
		NextFetchAt: r.NextFetchAt, CreatedAt: r.CreatedAt,
	}
}

// FeedURL is the address as it is stored, or ErrBadFeedURL.
func FeedURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(raw) > 2048 {
		return "", ErrBadFeedURL
	}
	u.Fragment = ""
	return u.String(), nil
}

// AddFeed has the user follow a feed with the filters, and fetches it
// soon. Adding the same address again gives it the new filters.
func (s *Service) AddFeed(ctx context.Context, user uuid.UUID, raw string, keywords, authors []string) (Feed, error) {
	address, err := FeedURL(raw)
	if err != nil {
		return Feed{}, err
	}
	var f Feed
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		mine, err := q.ListFeedTrackers(ctx, user)
		if err != nil {
			return err
		}
		known := slices.ContainsFunc(mine, func(r sqlc.FeedTracker) bool { return r.Url == address })
		if len(mine) >= MaxFeeds && !known {
			return ErrTooManyFeeds
		}
		row, err := q.PutFeedTracker(ctx, sqlc.PutFeedTrackerParams{
			UserID: user, Url: address, Keywords: filterWords(keywords), Authors: filterWords(authors),
		})
		if err != nil {
			return err
		}
		f = feedOf(row)
		return s.queueFetchTx(ctx, tx)
	})
	return f, err
}

func filterWords(list []string) []string {
	out := []string{}
	for _, s := range list {
		s = plain(s)
		if catalog.Key(s) != "" && !slices.Contains(out, s) && len(out) < maxFilters {
			out = append(out, clip(s))
		}
	}
	return out
}

func (s *Service) queueFetchTx(ctx context.Context, tx pgx.Tx) error {
	if s.Queue == nil {
		return nil
	}
	_, err := s.Queue.InsertTx(ctx, tx, FetchFeedsArgs{}, fetchOpts)
	return err
}

// Feeds are the user's feeds, by title.
func (s *Service) Feeds(ctx context.Context, user uuid.UUID) ([]Feed, error) {
	rows, err := sqlc.New(s.pool).ListFeedTrackers(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]Feed, len(rows))
	for i, r := range rows {
		out[i] = feedOf(r)
	}
	return out, nil
}

// RemoveFeed ends one of the user's feeds, with what it found.
func (s *Service) RemoveFeed(ctx context.Context, user, id uuid.UUID) error {
	n, err := sqlc.New(s.pool).DeleteFeedTracker(ctx, sqlc.DeleteFeedTrackerParams{ID: id, UserID: user})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// RetryFeed fetches one of the user's feeds soon, also one that stopped.
func (s *Service) RetryFeed(ctx context.Context, user, id uuid.UUID) (Feed, error) {
	var f Feed
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).RetryFeedTracker(ctx, sqlc.RetryFeedTrackerParams{ID: id, UserID: user})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		f = feedOf(row)
		return s.queueFetchTx(ctx, tx)
	})
	return f, err
}

// FeedEntry is an item of one of the user's feeds that names what the
// feed's filters ask for.
type FeedEntry struct {
	FeedID    uuid.UUID
	Feed      string
	Title     string
	Link      *string
	Authors   []string
	Published *time.Time
	SeenAt    time.Time
}

// FeedEntries are the items of the user's feeds that match, the newest
// first.
func (s *Service) FeedEntries(ctx context.Context, user uuid.UUID) ([]FeedEntry, error) {
	rows, err := sqlc.New(s.pool).ListFeedItems(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]FeedEntry, len(rows))
	for i, r := range rows {
		out[i] = FeedEntry{
			FeedID: r.TrackerID, Feed: r.Feed, Title: r.Title, Link: r.Link, Authors: r.Authors,
			Published: r.PublishedAt, SeenAt: r.FirstSeenAt,
		}
	}
	return out, nil
}

// FetchFeeds fetches the feeds that are due, the longest waiting first,
// until the deadline. It reports whether none is left. Offline, it fetches
// nothing.
func (s *Service) FetchFeeds(ctx context.Context, deadline time.Time) (complete bool, err error) {
	if s.Offline {
		return true, nil
	}
	q := sqlc.New(s.pool)
	for {
		if time.Now().After(deadline) {
			return false, nil
		}
		f, err := q.NextDueFeed(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if err := s.fetchFeed(ctx, f); err != nil {
			return false, err
		}
	}
}

// fetchFeed fetches one feed and records what came of it. Only the
// database's errors are returned: the feed's are the feed's, recorded on
// it.
func (s *Service) fetchFeed(ctx context.Context, f sqlc.FeedTracker) error {
	got, err := getFeed(ctx, s.FeedClient, f.Url, f.Etag, f.LastModified)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	q := sqlc.New(s.pool)
	if err != nil {
		failures := f.Failures + 1
		p := sqlc.FeedFailedParams{ID: f.ID, Failures: failures, LastError: errorText(err)}
		if failures < maxFailures {
			next := time.Now().Add(min(fetchEvery<<failures, maxBackoff))
			p.NextFetchAt = &next
		}
		s.log.Info("a feed could not be read", "feed", f.ID, "failures", failures, "error", err)
		return q.FeedFailed(ctx, p)
	}
	next := time.Now().Add(fetchEvery)
	if got.unchanged {
		return q.FeedAnswered(ctx, sqlc.FeedAnsweredParams{ID: f.ID, NextFetchAt: next})
	}
	match := newFeedFilter(f.Keywords, f.Authors)
	backlog := f.CheckedAt == nil
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.LockFeedTracker(ctx, f.ID); errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		for _, it := range got.feed.Items {
			matched := match.matches(it)
			p := sqlc.PutFeedItemParams{
				TrackerID: f.ID, ItemKey: it.Key, Title: it.Title, Authors: it.Authors,
				PublishedAt: it.Published, Matched: matched, Backlog: backlog,
			}
			if p.Authors == nil {
				p.Authors = []string{}
			}
			if it.Link != "" {
				p.Link = &it.Link
			}
			inserted, err := q.PutFeedItem(ctx, p)
			if err != nil {
				return err
			}
			if inserted && matched && !backlog {
				if err := s.tellFeedItemTx(ctx, tx, f, got.feed.Title, it); err != nil {
					return err
				}
			}
		}
		if err := q.PruneFeedItems(ctx, sqlc.PruneFeedItemsParams{TrackerID: f.ID, Before: time.Now().Add(-forgetAfter)}); err != nil {
			return err
		}
		p := sqlc.FeedAnsweredParams{ID: f.ID, NextFetchAt: next, Etag: got.etag, LastModified: got.lastModified}
		if got.feed.Title != "" {
			p.Title = &got.feed.Title
		}
		return q.FeedAnswered(ctx, p)
	})
}

func (s *Service) tellFeedItemTx(ctx context.Context, tx pgx.Tx, f sqlc.FeedTracker, title string, it feedItem) error {
	if title == "" && f.Title != nil {
		title = *f.Title
	}
	if title == "" {
		title = f.Url
	}
	data := map[string]any{"title": it.Title, "feed": title}
	if len(it.Authors) > 0 {
		data["authors"] = strings.Join(it.Authors, ", ")
	}
	return s.Events.QueueTx(ctx, tx, notify.Event{
		Kind: notify.KindFeedItem, Data: data, Link: "/releases", Recipients: []uuid.UUID{f.UserID},
	})
}

// errorText is what a feed's failure is shown as, short and without the
// bytes of the answer.
func errorText(err error) string {
	s := strings.ToValidUTF8(err.Error(), "")
	if utf8.RuneCountInString(s) > maxError {
		s = string([]rune(s)[:maxError]) + "…"
	}
	return s
}

// unreachable is why a feed could not be fetched, in words that tell
// nothing of the network inside: an address there and a name nobody
// knows read alike, or the answers would map it.
func unreachable(err error) error {
	var dns *net.DNSError
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(err, outbound.ErrForbiddenAddress), errors.As(err, &dns):
		return ErrNoPublicServer
	case errors.As(err, &timeout) && timeout.Timeout():
		return errors.New("the feed did not answer in time")
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		// The address is the owner's own; what is left says why.
		return ue.Err
	}
	return err
}

// fetched is a feed's answer: unchanged since the ETag or date it was
// asked with, or the feed and what to ask with next time.
type fetched struct {
	unchanged          bool
	feed               parsedFeed
	etag, lastModified *string
}

// getFeed fetches a feed, asking only for a changed one where an earlier
// answer gave an ETag or a date.
func getFeed(ctx context.Context, client *http.Client, address string, etag, lastModified *string) (fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("User-Agent", feedAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.1")
	if etag != nil {
		req.Header.Set("If-None-Match", *etag)
	}
	if lastModified != nil {
		req.Header.Set("If-Modified-Since", *lastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fetched{}, unreachable(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return fetched{unchanged: true}, nil
	case resp.StatusCode != http.StatusOK:
		return fetched{}, fmt.Errorf("the feed answered %s", resp.Status)
	case resp.ContentLength > MaxFeedBytes:
		return fetched{}, ErrFeedTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes+1))
	if err != nil {
		return fetched{}, err
	}
	if len(body) > MaxFeedBytes {
		return fetched{}, ErrFeedTooLarge
	}
	feed, err := parseFeed(body, resp.Request.URL)
	if err != nil {
		return fetched{}, err
	}
	out := fetched{feed: feed}
	if v := resp.Header.Get("ETag"); v != "" {
		out.etag = &v
	}
	if v := resp.Header.Get("Last-Modified"); v != "" {
		out.lastModified = &v
	}
	return out, nil
}

// feedFilter is what an item must name: one of the keywords, if there are
// any, and one of the authors, if there are any. Words are compared by
// their keys, whole.
type feedFilter struct {
	keywords []string
	// authors are each author's keys in both orders of the name.
	authors [][]string
}

func newFeedFilter(keywords, authors []string) feedFilter {
	var f feedFilter
	for _, k := range keywords {
		if key := catalog.Key(k); key != "" {
			f.keywords = append(f.keywords, key)
		}
	}
	for _, a := range authors {
		if keys := metadata.NameKeys(a); keys[0] != "" {
			f.authors = append(f.authors, keys)
		}
	}
	return f
}

func (f feedFilter) matches(it feedItem) bool {
	text := " " + catalog.Key(it.Title+" "+strings.Join(it.Categories, " ")+" "+it.Summary) + " "
	has := func(key string) bool { return strings.Contains(text, " "+key+" ") }
	if len(f.keywords) > 0 && !slices.ContainsFunc(f.keywords, has) {
		return false
	}
	if len(f.authors) == 0 {
		return true
	}
	var named []string
	for _, a := range it.Authors {
		named = append(named, metadata.NameKeys(a)...)
	}
	return slices.ContainsFunc(f.authors, func(keys []string) bool {
		return slices.ContainsFunc(keys, func(k string) bool { return slices.Contains(named, k) || has(k) })
	})
}
