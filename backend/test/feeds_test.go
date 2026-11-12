//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/releases"
)

// feedServer serves an RSS feed of the items the test sets, with an ETag
// when asked to, and counts what it is asked.
type feedServer struct {
	*httptest.Server
	mu       sync.Mutex
	body     string
	etag     string
	requests []http.Header
}

func newFeedServer(t *testing.T) *feedServer {
	f := &feedServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Header.Clone())
		if f.etag != "" {
			if r.Header.Get("If-None-Match") == f.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", f.etag)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(f.Close)
	return f
}

// items sets the feed to the items, as title and author, under an ETag
// that changes with them if etags is on.
func (f *feedServer) items(etags bool, items ...[2]string) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Publisher News</title>`)
	for _, it := range items {
		fmt.Fprintf(&b, `<item><title>%s</title><guid>%s</guid><link>https://publisher.example/%s</link><author>%s</author></item>`,
			it[0], it[0], strings.ReplaceAll(it[0], " ", "-"), it[1])
	}
	b.WriteString(`</channel></rss>`)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body = b.String()
	f.etag = ""
	if etags {
		f.etag = fmt.Sprintf(`"%d"`, len(items))
	}
}

func (f *feedServer) asked() []http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// withFeedsOnTheLoopback lets the app fetch the test's feed servers.
func (a *app) withFeedsOnTheLoopback() {
	a.server.Releases.FeedClient = outbound.NewClient(outbound.Options{AllowPrivate: true, Timeout: 10 * time.Second})
}

// fetchFeeds fetches every feed that is not stopped, as if its time had
// come.
func (a *app) fetchFeeds() {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(),
		`UPDATE feed_trackers SET next_fetch_at = now() - interval '1 second' WHERE next_fetch_at IS NOT NULL`); err != nil {
		a.t.Fatal(err)
	}
	complete, err := a.server.Releases.FetchFeeds(context.Background(), time.Now().Add(time.Minute))
	if err != nil || !complete {
		a.t.Fatalf("fetch: %v, complete %v", err, complete)
	}
}

func (a *app) addFeed(c *http.Client, body map[string]any) map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/feeds", body)
	if status != 201 {
		a.t.Fatalf("add feed %v: %d %v", body, status, out)
	}
	return out
}

func (a *app) feeds(c *http.Client) []map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/feeds", nil)
	if status != 200 {
		a.t.Fatalf("feeds: %d %v", status, out)
	}
	var list []map[string]any
	for _, f := range out["feeds"].([]any) {
		list = append(list, f.(map[string]any))
	}
	return list
}

func (a *app) feedTitles(c *http.Client) []string {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/feeds/items", nil)
	if status != 200 {
		a.t.Fatalf("feed items: %d %v", status, out)
	}
	var titles []string
	for _, it := range out["items"].([]any) {
		titles = append(titles, it.(map[string]any)["title"].(string))
	}
	slices.Sort(titles)
	return titles
}

func TestAFeedsMatchingNewItemIsToldOnce(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	events := a.withEvents()
	a.server.Releases.Events = events
	ann, _ := a.signedIn("ann", "reader")
	bob, _ := a.signedIn("bob", "reader")
	srv := newFeedServer(t)
	srv.items(false, [2]string{"An Old Dragon Tale", "Wanda Writer"}, [2]string{"A Cookbook", "Chef"})
	a.addFeed(ann, map[string]any{"url": srv.URL + "/feed.xml", "keywords": []string{"dragon", " "}, "authors": []string{}})
	a.addFeed(bob, map[string]any{"url": srv.URL + "/feed.xml", "authors": []string{"Writer, Wanda"}})

	// What the feed holds at first is its past: listed, not told.
	a.fetchFeeds()
	a.flush(events)
	if got := a.told(ann); len(got) != 0 {
		t.Errorf("ann is told of the feed's past: %v", got)
	}
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"An Old Dragon Tale"}) {
		t.Errorf("ann's items: %v", got)
	}
	feed := a.feeds(ann)[0]
	if feed["title"] != "Publisher News" || feed["checkedAt"] == nil || feed["failures"] != 0.0 {
		t.Errorf("ann's feed: %v", feed)
	}

	// New items: one each matches for, one neither does.
	srv.items(false,
		[2]string{"An Old Dragon Tale", "Wanda Writer"}, [2]string{"A Cookbook", "Chef"},
		[2]string{"Dragon Summer", "Sam Other"}, [2]string{"Quiet Days", "Wanda Writer"}, [2]string{"Gardening", "Gail"})
	a.fetchFeeds()
	// The feed lists them again, and again: they are told once.
	a.fetchFeeds()
	a.flush(events)
	a.fetchFeeds()
	a.flush(events)
	for name, c := range map[string]*http.Client{"ann": ann, "bob": bob} {
		want := map[string]string{"ann": "Dragon Summer", "bob": "Quiet Days"}[name]
		got := a.told(c)
		if len(got) != 1 || got[0].Kind != "feed.item" || got[0].Data["title"] != want || got[0].Data["feed"] != "Publisher News" {
			t.Errorf("%s is told %v, want %s once", name, got, want)
		}
	}
	if got := a.feedTitles(bob); !slices.Equal(got, []string{"An Old Dragon Tale", "Quiet Days"}) {
		t.Errorf("bob's items: %v", got)
	}
}

func TestAnUnchangedFeedCostsOneConditionalRequest(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	ann, _ := a.signedIn("ann", "reader")
	srv := newFeedServer(t)
	srv.items(true, [2]string{"One", "Someone"})
	a.addFeed(ann, map[string]any{"url": srv.URL})
	a.fetchFeeds()
	before := a.countRows(`SELECT count(*) FROM feed_items`)
	a.fetchFeeds()
	asked := srv.asked()
	if len(asked) != 2 || asked[1].Get("If-None-Match") != `"1"` {
		t.Fatalf("requests %v", asked)
	}
	if n := a.countRows(`SELECT count(*) FROM feed_items`); n != before || n != 1 {
		t.Errorf("%d items after an unchanged fetch, %d before", n, before)
	}
	if feed := a.feeds(ann)[0]; feed["failures"] != 0.0 || feed["lastError"] != nil {
		t.Errorf("an unchanged feed is a failure: %v", feed)
	}
}

func TestAFeedThatCannotBeReadIsReportedAndBackedOff(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	ann, _ := a.signedIn("ann", "reader")
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>Not a feed</body></html>"))
	}))
	defer broken.Close()
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<rss><channel>"))
		chunk := []byte(strings.Repeat("<item><title>x</title></item>", 1000))
		for written := 0; written <= releases.MaxFeedBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer huge.Close()
	a.addFeed(ann, map[string]any{"url": broken.URL})
	a.addFeed(ann, map[string]any{"url": huge.URL})

	a.fetchFeeds()
	errs := map[string]string{}
	for _, f := range a.feeds(ann) {
		errs[f["url"].(string)], _ = f["lastError"].(string)
		next, _ := time.Parse(time.RFC3339Nano, f["nextFetchAt"].(string))
		if f["failures"] != 1.0 || time.Until(next) < time.Hour {
			t.Errorf("after one failure: %v", f)
		}
	}
	if !strings.Contains(errs[broken.URL], "not an RSS or Atom feed") || !strings.Contains(errs[huge.URL], "larger than") {
		t.Errorf("errors %v", errs)
	}

	// It is tried again less and less often, and then no more.
	for range 10 {
		a.fetchFeeds()
	}
	var id string
	for _, f := range a.feeds(ann) {
		if f["failures"] != 8.0 || f["nextFetchAt"] != nil || f["lastError"] == nil {
			t.Errorf("a feed that keeps failing: %v", f)
		}
		id = f["id"].(string)
	}
	if n := a.countRows(`SELECT count(*) FROM feed_trackers WHERE next_fetch_at IS NOT NULL`); n != 0 {
		t.Errorf("%d feeds still fetched", n)
	}
	// Its owner may ask for it again.
	status, out, _ := a.call(ann, http.MethodPost, "/feeds/"+id+"/fetch", nil)
	if status != 200 || out["nextFetchAt"] == nil {
		t.Errorf("fetch again: %d %v", status, out)
	}
}

func TestAFeedPointingInsideIsRefused(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ann, _ := a.signedIn("ann", "reader")
	srv := newFeedServer(t)
	srv.items(false, [2]string{"Secret", "Inside"})
	// The app's own client, which trusts nothing a user typed.
	a.server.Releases.FeedClient = releases.NewFeedClient()
	a.addFeed(ann, map[string]any{"url": srv.URL})
	a.fetchFeeds()
	if n := len(srv.asked()); n != 0 {
		t.Errorf("the feed inside was asked %d times", n)
	}
	feed := a.feeds(ann)[0]
	if msg, _ := feed["lastError"].(string); !strings.Contains(msg, "public internet") || strings.Contains(msg, "127.0.0.1") {
		t.Errorf("the feed's error: %v", feed)
	}
	for _, body := range []map[string]any{{"url": "file:///etc/passwd"}, {"url": "gopher://x"}, {"url": ""}} {
		if status, out, _ := a.call(ann, http.MethodPost, "/feeds", body); status != 422 {
			t.Errorf("add %v: %d %v", body, status, out)
		}
	}
}

func TestAFeedIsItsOwnersAlone(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	ann, _ := a.signedIn("ann", "reader")
	bob, _ := a.signedIn("bob", "admin")
	srv := newFeedServer(t)
	srv.items(false, [2]string{"Ann's Book", "Someone"})
	mine := a.addFeed(ann, map[string]any{"url": srv.URL})["id"].(string)
	if again := a.addFeed(ann, map[string]any{"url": srv.URL, "keywords": []string{"book"}}); again["id"] != mine {
		t.Errorf("adding the address again made another feed: %v", again)
	}
	a.fetchFeeds()
	if got := a.feeds(bob); len(got) != 0 {
		t.Errorf("bob sees ann's feeds: %v", got)
	}
	if got := a.feedTitles(bob); len(got) != 0 {
		t.Errorf("bob sees ann's items: %v", got)
	}
	missing := "/feeds/0190d5a4-0000-7000-8000-000000000000"
	for _, path := range []string{"/feeds/" + mine, missing} {
		for _, method := range []string{http.MethodDelete, http.MethodPost} {
			p := path
			if method == http.MethodPost {
				p += "/fetch"
			}
			status, out, _ := a.call(bob, method, p, nil)
			if status != 404 || out["error"].(map[string]any)["message"] != "There is no such feed." {
				t.Errorf("bob %s %s: %d %v", method, p, status, out)
			}
		}
	}
	if status, _, _ := a.call(ann, http.MethodDelete, "/feeds/"+mine, nil); status != 204 {
		t.Errorf("ann ends her feed: %d", status)
	}
	if n := a.count("feed_items"); n != 0 {
		t.Errorf("%d items of a feed nobody follows", n)
	}
}

// A feed whose items cannot be recorded fails as one that cannot be read
// would, and the feeds after it are fetched (#322).
func TestAFeedThatCannotBeRecordedDoesNotHoldUpTheOthers(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	ann, _ := a.signedIn("ann", "reader")
	if _, err := a.pool.Exec(context.Background(), `
		CREATE FUNCTION refuse_poison() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		    IF NEW.title = 'Poison' THEN RAISE EXCEPTION 'refused'; END IF;
		    RETURN NEW;
		END $$;
		CREATE TRIGGER refuse_poison BEFORE INSERT ON feed_items FOR EACH ROW EXECUTE FUNCTION refuse_poison()`); err != nil {
		t.Fatal(err)
	}
	bad := newFeedServer(t)
	bad.items(false, [2]string{"Poison", "Someone"})
	good := newFeedServer(t)
	// No guid, and a link longer than an index takes.
	long := "https://publisher.example/" + strings.Repeat("x", 5000)
	good.body = `<rss version="2.0"><channel><title>Good</title>` +
		`<item><title>Long Link</title><link>` + long + `</link></item></channel></rss>`
	a.addFeed(ann, map[string]any{"url": bad.URL})
	a.addFeed(ann, map[string]any{"url": good.URL})

	a.fetchFeeds()
	for _, f := range a.feeds(ann) {
		next, _ := time.Parse(time.RFC3339Nano, f["nextFetchAt"].(string))
		switch f["url"] {
		case bad.URL:
			if msg, _ := f["lastError"].(string); f["failures"] != 1.0 || !strings.Contains(msg, "could not be recorded") ||
				strings.Contains(msg, "refused") || time.Until(next) < time.Hour {
				t.Errorf("the feed that cannot be recorded: %v", f)
			}
		default:
			if f["failures"] != 0.0 || f["checkedAt"] == nil {
				t.Errorf("the feed after it: %v", f)
			}
		}
	}
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"Long Link"}) {
		t.Errorf("items: %v", got)
	}
}

// Filters that change apply to the items stored too, by all the filters
// look at, and an item matched only so is not news (#322).
func TestNewFiltersApplyToTheItemsStored(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.withFeedsOnTheLoopback()
	events := a.withEvents()
	a.server.Releases.Events = events
	ann, _ := a.signedIn("ann", "reader")
	srv := newFeedServer(t)
	srv.body = `<rss version="2.0"><channel><title>News</title>` +
		`<item><title>An Old Dragon Tale</title><guid>1</guid><author>Wanda Writer</author></item>` +
		`<item><title>Kitchen Notes</title><guid>2</guid><category>Cookbook</category></item>` +
		`<item><title>Quiet Days</title><guid>3</guid><description>Recipes for a cookbook shelf</description></item>` +
		`</channel></rss>`
	a.addFeed(ann, map[string]any{"url": srv.URL, "keywords": []string{"dragon"}})
	a.fetchFeeds()
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"An Old Dragon Tale"}) {
		t.Fatalf("items: %v", got)
	}

	a.addFeed(ann, map[string]any{"url": srv.URL, "keywords": []string{"cookbook"}})
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"Kitchen Notes", "Quiet Days"}) {
		t.Errorf("items under the new keywords: %v", got)
	}
	a.addFeed(ann, map[string]any{"url": srv.URL, "authors": []string{"Writer, Wanda"}})
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"An Old Dragon Tale"}) {
		t.Errorf("items under the new author: %v", got)
	}
	a.addFeed(ann, map[string]any{"url": srv.URL, "keywords": []string{"cookbook"}})
	a.fetchFeeds()
	a.flush(events)
	if got := a.feedTitles(ann); !slices.Equal(got, []string{"Kitchen Notes", "Quiet Days"}) {
		t.Errorf("items fetched again: %v", got)
	}
	if got := a.told(ann); len(got) != 0 {
		t.Errorf("ann is told of items she had: %v", got)
	}
}
