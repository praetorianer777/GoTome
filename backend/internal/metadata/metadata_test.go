package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// fakeProvider asks a test server that knows Emma by its ISBN and by search.
type fakeProvider struct {
	name     string
	base     string
	interval time.Duration
}

func (p *fakeProvider) Name() string   { return p.name }
func (p *fakeProvider) Limits() Limits { return Limits{Interval: p.interval} }

func (p *fakeProvider) Lookup(ctx context.Context, web Web, id catalog.Identifier) ([]Record, error) {
	return p.records(ctx, web, p.base+"/isbn/"+id.Value)
}

func (p *fakeProvider) Search(ctx context.Context, web Web, q Query) ([]Record, error) {
	return p.records(ctx, web, p.base+"/search?"+url.Values{"q": {q.Title}}.Encode())
}

func (p *fakeProvider) records(ctx context.Context, web Web, u string) ([]Record, error) {
	body, err := web.Get(ctx, Request{URL: u, SecretQuery: url.Values{"key": {"sekrit"}}})
	if err != nil {
		return nil, err
	}
	var out []Record
	return out, json.Unmarshal(body, &out)
}

var (
	emmaRecord = Record{
		ID: "emma-1815", Title: "Emma", Language: "en", Published: "1815-12",
		Contributors: []catalog.NewContributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
		Identifiers:  []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780141439587"}},
	}
	emmaFilm = Record{
		ID: "emma-film", Title: "Emma: The Screenplay", Published: "1996",
		Contributors: []catalog.NewContributor{{Name: "Douglas McGrath"}},
	}
)

// providerServer answers like the fake provider's source, and counts and
// keeps what it is asked.
type providerServer struct {
	*httptest.Server
	mu    sync.Mutex
	asked []string
	at    []time.Time
	keys  []string
}

func newProviderServer(t *testing.T) *providerServer {
	s := &providerServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.asked = append(s.asked, r.URL.Path+"?"+r.URL.Query().Get("q"))
		s.at = append(s.at, time.Now())
		s.keys = append(s.keys, r.URL.Query().Get("key"))
		s.mu.Unlock()
		switch {
		case r.Header.Get("User-Agent") != userAgent:
			http.Error(w, "who are you", http.StatusForbidden)
		case r.URL.Path == "/isbn/9780141439587":
			_ = json.NewEncoder(w).Encode([]Record{emmaRecord})
		case r.URL.Path == "/search" && r.URL.Query().Get("q") == "Emma":
			_ = json.NewEncoder(w).Encode([]Record{emmaFilm, emmaRecord})
		case strings.HasPrefix(r.URL.Path, "/broken"):
			http.Error(w, "oops", http.StatusInternalServerError)
		case r.URL.Path == "/busy":
			http.Error(w, "slow down", http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *providerServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

func emmaBook(withISBN bool) catalog.Book {
	b := catalog.Book{
		Title: "Emma", Language: "en-GB",
		Contributors: []catalog.Contributor{{Name: "Jane Austen", Role: catalog.RoleAuthor}},
	}
	if withISBN {
		b.Identifiers = []catalog.BookIdentifier{{Identifier: catalog.Identifier{Type: catalog.IDISBN, Value: "9780141439587"}}}
	}
	return b
}

func TestCandidatesLookUpByISBNThenSearch(t *testing.T) {
	srv := newProviderServer(t)
	s := NewService(nil, []Provider{&fakeProvider{name: "fake", base: srv.URL}}, Options{AllowPrivate: true})

	got, err := s.Candidates(context.Background(), emmaBook(true))
	if err != nil || len(got) != 1 || got[0].ID != "emma-1815" || got[0].Score != 1 || got[0].Provider != "fake" {
		t.Fatalf("by ISBN: %+v, %v", got, err)
	}
	if asked := srv.requests(); !slices.Equal(asked, []string{"/isbn/9780141439587?"}) {
		t.Errorf("asked %v", asked)
	}

	got, err = s.Candidates(context.Background(), emmaBook(false))
	if err != nil || len(got) != 2 || got[0].ID != "emma-1815" || got[0].Score < 0.9 || got[1].Score > 0.6 {
		t.Fatalf("by search: %+v, %v", got, err)
	}
}

func TestOnlyEnabledProvidersAreAsked(t *testing.T) {
	srv := newProviderServer(t)
	enabled := []string{"other"}
	s := NewService(nil, []Provider{&fakeProvider{name: "fake", base: srv.URL}}, Options{
		AllowPrivate: true, Enabled: func(context.Context) []string { return enabled },
	})
	if got, err := s.Candidates(context.Background(), emmaBook(true)); err != nil || len(got) != 0 || len(srv.requests()) != 0 {
		t.Errorf("a disabled provider: %v, %v, %d requests", got, err, len(srv.requests()))
	}
	enabled = []string{"fake"}
	if got, _ := s.Candidates(context.Background(), emmaBook(true)); len(got) != 1 {
		t.Errorf("enabled again: %v", got)
	}
}

func TestOneFailingProviderDoesNotHideTheOthers(t *testing.T) {
	srv := newProviderServer(t)
	s := NewService(nil, []Provider{
		&fakeProvider{name: "broken", base: srv.URL + "/broken"},
		&fakeProvider{name: "fake", base: srv.URL},
	}, Options{AllowPrivate: true})

	got, err := s.Candidates(context.Background(), emmaBook(true))
	var perr *ProviderError
	if !errors.As(err, &perr) || perr.Provider != "broken" || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if strings.Contains(err.Error(), "sekrit") {
		t.Errorf("the error tells the key: %v", err)
	}
	for _, k := range srv.keys {
		if k != "sekrit" {
			t.Errorf("a request without the key: %q", k)
		}
	}
}

func TestAProviderThatIsBusySaysSo(t *testing.T) {
	srv := newProviderServer(t)
	s := NewService(nil, []Provider{&fakeProvider{name: "fake", base: srv.URL}}, Options{AllowPrivate: true})
	if _, err := s.webs["fake"].Get(context.Background(), Request{URL: srv.URL + "/busy"}); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v", err)
	}
	if _, err := s.webs["fake"].Get(context.Background(), Request{URL: srv.URL + "/nothing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestRequestsAreSpacedAcrossGoroutines(t *testing.T) {
	srv := newProviderServer(t)
	const interval = 40 * time.Millisecond
	s := NewService(nil, []Provider{&fakeProvider{name: "fake", base: srv.URL, interval: interval}}, Options{AllowPrivate: true})

	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Go(func() {
			if _, err := s.Candidates(context.Background(), emmaBook(true)); err != nil {
				failed.Add(1)
			}
		})
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d searches failed", failed.Load())
	}
	at := slices.Clone(srv.at)
	slices.SortFunc(at, func(a, b time.Time) int { return a.Compare(b) })
	for i := 1; i < len(at); i++ {
		// The server notes the time a little after the request left.
		if gap := at[i].Sub(at[i-1]); gap < interval-10*time.Millisecond {
			t.Errorf("requests %d and %d %v apart, want at least %v", i-1, i, gap, interval)
		}
	}
}

func TestInternalAddressesAreRefused(t *testing.T) {
	srv := newProviderServer(t)
	s := NewService(nil, []Provider{&fakeProvider{name: "fake", base: srv.URL}}, Options{})
	_, err := s.Candidates(context.Background(), emmaBook(true))
	if !errors.Is(err, ErrForbiddenAddress) || len(srv.requests()) != 0 {
		t.Errorf("err = %v, %d requests", err, len(srv.requests()))
	}
	if _, err := s.Cover(context.Background(), "fake", "file:///etc/passwd"); err == nil {
		t.Error("a cover from a file was fetched")
	}

	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2a00:1450:4001::200e": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false,
		"fd00::1": false, "fe80::1": false, "::ffff:127.0.0.1": false, "224.0.0.1": false,
	} {
		if got := public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("public(%s) = %v", addr, got)
		}
	}
}

func TestScore(t *testing.T) {
	book := emmaBook(false)
	book.PublishedOn, book.PublishedPrecision = new(time.Date(1815, 12, 1, 0, 0, 0, 0, time.UTC)), catalog.PrecisionMonth
	cases := []struct {
		name     string
		r        Record
		low, top float64
	}{
		{"the same book", emmaRecord, 0.95, 0.99},
		{"another spelling of the author", Record{Title: "Emma", Contributors: []catalog.NewContributor{{Name: "Austen, Jane"}}, Published: "1815"}, 0.95, 0.99},
		{"another author", Record{Title: "Emma", Contributors: []catalog.NewContributor{{Name: "Emma Donoghue"}}, Published: "2010"}, 0.5, 0.65},
		{"another book", Record{Title: "Persuasion", Contributors: []catalog.NewContributor{{Name: "Jane Austen"}}}, 0.3, 0.5},
		{"nothing alike", Record{Title: "Dune", Contributors: []catalog.NewContributor{{Name: "Frank Herbert"}}, Language: "en"}, 0, 0.1},
	}
	for _, c := range cases {
		if got := Score(book, c.r); got < c.low || got > c.top {
			t.Errorf("%s: %.3f, want %.2f to %.2f", c.name, got, c.low, c.top)
		}
	}
	if got := Score(emmaBook(true), Record{Title: "Something else", Identifiers: emmaRecord.Identifiers}); got != 1 {
		t.Errorf("a shared ISBN scores %.3f", got)
	}
}

// memoryWeb answers every request with its URL.
type memoryWeb struct{ asked int }

func (w *memoryWeb) Get(_ context.Context, r Request) ([]byte, error) {
	w.asked++
	if strings.HasSuffix(r.URL, "/missing") {
		return nil, ErrNotFound
	}
	return []byte("answer to " + r.URL), nil
}

func TestFixturesRecordOnceAndReplay(t *testing.T) {
	dir := t.TempDir()
	live := &memoryWeb{}
	recording := &Fixtures{Dir: dir, Live: live}
	ctx := context.Background()
	for _, u := range []string{"https://example.org/a", "https://example.org/missing"} {
		_, _ = recording.Get(ctx, Request{URL: u, SecretQuery: url.Values{"key": {"sekrit"}}})
	}

	replay := &Fixtures{Dir: dir}
	if got, err := replay.Get(ctx, Request{URL: "https://example.org/a"}); err != nil || string(got) != "answer to https://example.org/a" {
		t.Errorf("replayed %q, %v", got, err)
	}
	if _, err := replay.Get(ctx, Request{URL: "https://example.org/missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a recorded 404: %v", err)
	}
	if _, err := replay.Get(ctx, Request{URL: "https://example.org/b"}); !errors.Is(err, ErrNoFixture) {
		t.Errorf("an unrecorded request: %v", err)
	}
	if live.asked != 2 {
		t.Errorf("the live web was asked %d times", live.asked)
	}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if data, _ := os.ReadFile(filepath.Join(dir, f.Name())); strings.Contains(string(data), "sekrit") {
			t.Errorf("%s holds the key", f.Name())
		}
	}
}

// askedIDs is a provider that knows nothing and remembers what it was asked.
type askedIDs struct{ ids []catalog.Identifier }

func (*askedIDs) Name() string   { return "asked" }
func (*askedIDs) Limits() Limits { return Limits{} }
func (p *askedIDs) Lookup(_ context.Context, _ Web, id catalog.Identifier) ([]Record, error) {
	p.ids = append(p.ids, id)
	return nil, ErrNotFound
}
func (*askedIDs) Search(context.Context, Web, Query) ([]Record, error) { return nil, nil }

func TestABookIsLookedUpByItsDOITooAsPapersHaveNoISBN(t *testing.T) {
	doi := catalog.Identifier{Type: catalog.IDDOI, Value: "10.1038/nature14539"}
	isbn := catalog.Identifier{Type: catalog.IDISBN, Value: "9780141439587"}
	p := &askedIDs{}
	s := NewService(nil, []Provider{p}, Options{})
	book := catalog.Book{Title: "Deep learning", Identifiers: []catalog.BookIdentifier{
		{Identifier: doi}, {Identifier: isbn}, {Identifier: catalog.Identifier{Type: catalog.IDASIN, Value: "B000"}},
	}}
	if _, err := s.Candidates(context.Background(), book); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.ids, []catalog.Identifier{isbn, doi}) {
		t.Errorf("looked up %v, want the ISBN and the DOI", p.ids)
	}
}

// listing is a provider that lists works in several languages.
type listing struct{ records []Record }

func (*listing) Name() string                                                      { return "listing" }
func (*listing) Limits() Limits                                                    { return Limits{} }
func (*listing) Lookup(context.Context, Web, catalog.Identifier) ([]Record, error) { return nil, nil }
func (*listing) Search(context.Context, Web, Query) ([]Record, error)              { return nil, nil }
func (l *listing) ByAuthor(context.Context, Web, string) ([]Record, error) {
	return slices.Clone(l.records), nil
}
func (l *listing) BySeries(context.Context, Web, string) ([]Record, error) { return nil, nil }

func TestWorksAreKeptInTheLanguagesAsked(t *testing.T) {
	l := &listing{records: []Record{
		{Title: "The Original", Language: "en"},
		{Title: "Die Übersetzung", Language: "de-AT"},
		{Title: "La traducción", Language: "es"},
		{Title: "Unknown"},
	}}
	s := NewService(nil, []Provider{l}, Options{Language: func(context.Context) string { return "de" }})
	answers, err := s.WorksOf(context.Background(), WorksAuthor, "Someone")
	if err != nil || len(answers) != 1 {
		t.Fatalf("WorksOf: %+v, %v", answers, err)
	}
	var titles []string
	for _, r := range answers[0].Records {
		titles = append(titles, r.Title)
		if r.Provider != "listing" {
			t.Errorf("%q has provider %q", r.Title, r.Provider)
		}
	}
	if !slices.Equal(titles, []string{"The Original", "Die Übersetzung", "Unknown"}) {
		t.Errorf("kept %v", titles)
	}
}
