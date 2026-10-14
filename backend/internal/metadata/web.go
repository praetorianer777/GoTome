package metadata

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

var (
	// ErrNotFound is a provider saying it knows no such book.
	ErrNotFound = errors.New("the provider knows no such book")
	// ErrForbiddenAddress is a request to an address inside the server's own
	// network, which no provider has any business with.
	ErrForbiddenAddress = outbound.ErrForbiddenAddress
	// ErrBusy is a provider asking to be asked later.
	ErrBusy = errors.New("the provider asks to be asked later")
)

// Request is one request to a provider.
type Request struct {
	// Method is GET when empty.
	Method string
	URL    string
	Body   []byte
	// Header is sent and neither stored nor part of the cache key: an API
	// token goes here.
	Header http.Header
	// SecretQuery is added to the URL when it is sent, and is neither stored
	// nor part of the cache key: an API key that a provider wants in the
	// query goes here.
	SecretQuery url.Values
	// NoCache neither looks the answer up nor keeps it.
	NoCache bool
	// MaxBytes caps the answer; zero is maxAnswerBytes.
	MaxBytes int64
}

// Web is how a provider reaches its source.
type Web interface {
	// Get sends the request and returns the body of a 200 answer. A 404 is
	// ErrNotFound, a 429 or 503 ErrBusy.
	Get(ctx context.Context, r Request) ([]byte, error)
}

const (
	// maxAnswerBytes is the largest answer read: a page of search results
	// is a few hundred kilobytes.
	maxAnswerBytes = 5 << 20
	requestTimeout = 20 * time.Second
	// cacheFor is how long an answer is kept. What a provider knows about a
	// book changes seldom; a person who wants it fresh asks with NoCache.
	cacheFor = 30 * 24 * time.Hour
)

// Options configure a Service.
type Options struct {
	// Language is the BCP 47 tag asked for when a book has none, read on
	// every search: an administrator may change it. Nil asks for none.
	Language func(context.Context) string
	// Enabled names the providers to ask, read on every search. Nil asks
	// them all.
	Enabled func(context.Context) []string
	// AllowPrivate lets requests reach addresses inside the server's own
	// network. Only tests set it, for a provider served on the loopback.
	AllowPrivate bool
}

// Service asks the providers and ranks what they say.
type Service struct {
	providers []Provider
	webs      map[string]Web
	language  func(context.Context) string
	enabled   func(context.Context) []string
	// tokenKey signs cover tokens. It is new with every start: a token is
	// good for as long as the page that holds it.
	tokenKey []byte
}

// NewService returns a Service asking the providers in their order. With a
// pool, answers are kept in provider_records.
func NewService(pool *pgxpool.Pool, providers []Provider, opts Options) *Service {
	client := newClient(opts.AllowPrivate)
	s := &Service{providers: providers, webs: map[string]Web{}, language: opts.Language, enabled: opts.Enabled, tokenKey: make([]byte, 32)}
	_, _ = rand.Read(s.tokenKey)
	for _, p := range providers {
		s.webs[p.Name()] = newWeb(p, client, pool)
	}
	return s
}

// LiveWeb is a provider's Web without a cache, for recording fixtures.
func LiveWeb(p Provider) Web { return newWeb(p, newClient(false), nil) }

func newWeb(p Provider, client *http.Client, pool *pgxpool.Pool) *web {
	return &web{provider: p.Name(), client: client, pool: pool, limit: &spacer{interval: p.Limits().Interval}}
}

func newClient(allowPrivate bool) *http.Client {
	return outbound.NewClient(outbound.Options{Timeout: requestTimeout, AllowPrivate: allowPrivate})
}

// web is the Web of one provider.
type web struct {
	provider string
	client   *http.Client
	pool     *pgxpool.Pool
	limit    *spacer
}

var userAgent = "GOtome/" + version.Current().Version + " (+https://github.com/praetorianer777/GoTome)"

func (w *web) Get(ctx context.Context, r Request) ([]byte, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("not a web address: %q", r.URL)
	}
	sum := sha256.Sum256([]byte(method + "\x00" + r.URL + "\x00" + string(r.Body)))
	key := sum[:]
	cached := w.pool != nil && !r.NoCache
	if cached {
		row, err := sqlc.New(w.pool).GetProviderRecord(ctx, sqlc.GetProviderRecordParams{Provider: w.provider, RequestKey: key})
		switch {
		case err == nil && row.Status == http.StatusNotFound:
			return nil, ErrNotFound
		case err == nil:
			return row.Body, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
	}

	if len(r.SecretQuery) > 0 {
		q := u.Query()
		for k, vs := range r.SecretQuery {
			q[k] = vs
		}
		u.RawQuery = q.Encode()
	}
	if err := w.limit.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	for k, vs := range r.Header {
		req.Header[k] = vs
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := w.client.Do(req)
	if err != nil {
		// The URL in the error may carry the secret query.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("%s %s: %w", method, r.URL, err)
	}
	defer resp.Body.Close()
	limit := r.MaxBytes
	if limit == 0 {
		limit = maxAnswerBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s %s: the answer is larger than %d bytes", method, r.URL, limit)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return nil, fmt.Errorf("%w: %s", ErrBusy, resp.Status)
	default:
		return nil, fmt.Errorf("%s %s: %s", method, r.URL, resp.Status)
	}
	if cached {
		err := sqlc.New(w.pool).PutProviderRecord(ctx, sqlc.PutProviderRecordParams{
			Provider: w.provider, RequestKey: key, Url: r.URL, Status: int32(resp.StatusCode), Body: body,
			ExpiresAt: time.Now().Add(cacheFor),
		})
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	return body, nil
}

// spacer keeps requests at least an interval apart, however many goroutines
// ask: each takes the next free slot and waits for it.
type spacer struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func (s *spacer) wait(ctx context.Context) error {
	if s.interval <= 0 {
		return nil
	}
	s.mu.Lock()
	now := time.Now()
	slot := s.next
	if slot.Before(now) {
		slot = now
	}
	s.next = slot.Add(s.interval)
	s.mu.Unlock()
	t := time.NewTimer(time.Until(slot))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
