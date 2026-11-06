package releases

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/outbound"
)

const rssFeedText = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
  <title>Verlag  Neuheiten</title>
  <atom:link href="https://example.org/feed" rel="self"/>
  <item>
    <title>Feuerlotus</title>
    <link>/books/feuerlotus</link>
    <guid isPermaLink="false">tag:verlag,2026:42</guid>
    <dc:creator>Liza Grimm</dc:creator>
    <category>Fantasy</category>
    <description>&lt;p&gt;Der zweite Band der &lt;b&gt;Götter&lt;/b&gt;-Reihe.&lt;/p&gt;</description>
    <pubDate>Tue, 03 Nov 2026 08:00:00 +0100</pubDate>
  </item>
  <item>
    <title>Without a guid</title>
    <link>javascript:alert(1)</link>
    <atom:link href="https://example.org/books/no-guid" rel="alternate"/>
    <author>press@example.org (Wanda Writer)</author>
  </item>
  <item><description>No title and no link</description></item>
  <item><link></link></item>
</channel>
</rss>`

const atomFeedText = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title type="html">Author &amp;amp; Co</title>
  <entry>
    <id>urn:uuid:1</id>
    <title type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml">The <em>Next</em> One</div></title>
    <link rel="enclosure" href="https://cdn.example.org/cover.jpg"/>
    <link href="https://example.org/next"/>
    <author><name>Cornwell, Bernard</name></author>
    <category term="fiction" label="Historical fiction"/>
    <content type="html">&lt;p&gt;Out in spring.&lt;/p&gt;</content>
    <updated>2026-11-01T10:00:00Z</updated>
  </entry>
</feed>`

func base(t *testing.T) *url.URL {
	u, err := url.Parse("https://example.org/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAnRSSFeedIsRead(t *testing.T) {
	f, err := parseFeed([]byte(rssFeedText), base(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Verlag Neuheiten" || len(f.Items) != 3 {
		t.Fatalf("feed %+v", f)
	}
	first := f.Items[0]
	if first.Key != "tag:verlag,2026:42" || first.Title != "Feuerlotus" || first.Link != "https://example.org/books/feuerlotus" {
		t.Errorf("first item %+v", first)
	}
	if len(first.Authors) != 1 || first.Authors[0] != "Liza Grimm" || first.Summary != "Der zweite Band der Götter-Reihe." {
		t.Errorf("first item %+v", first)
	}
	if first.Published == nil || first.Published.Format("2006-01-02T15:04") != "2026-11-03T07:00" {
		t.Errorf("published %v", first.Published)
	}
	second := f.Items[1]
	if second.Link != "https://example.org/books/no-guid" || second.Key != second.Link {
		t.Errorf("an item without a guid is its link, and no javascript: address is one: %+v", second)
	}
	if len(second.Authors) != 1 || second.Authors[0] != "Wanda Writer" {
		t.Errorf("RSS's author is an address and a name: %v", second.Authors)
	}
	if third := f.Items[2]; third.Title != "No title and no link" || third.Key != "no title and no link" {
		t.Errorf("an item with only a description: %+v", third)
	}
}

func TestAnAtomFeedIsRead(t *testing.T) {
	f, err := parseFeed([]byte(atomFeedText), base(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Author & Co" || len(f.Items) != 1 {
		t.Fatalf("feed %+v", f)
	}
	e := f.Items[0]
	if e.Key != "urn:uuid:1" || e.Title != "The Next One" || e.Link != "https://example.org/next" || e.Summary != "Out in spring." {
		t.Errorf("entry %+v", e)
	}
	if len(e.Categories) != 1 || e.Categories[0] != "Historical fiction" || e.Published == nil {
		t.Errorf("entry %+v", e)
	}
}

func TestALongItemKeyIsHashed(t *testing.T) {
	link := "https://example.org/" + strings.Repeat("a", 5000)
	doc := `<rss><channel><item><title>Long</title><link>` + link + `</link></item>` +
		`<item><title>Longer</title><link>` + link + `b</link></item></channel></rss>`
	f, err := parseFeed([]byte(doc), nil)
	if err != nil || len(f.Items) != 2 {
		t.Fatalf("%+v, %v", f, err)
	}
	a, b := f.Items[0].Key, f.Items[1].Key
	if len(a) > maxKey || !strings.HasPrefix(a, "sha256:") || a == b || f.Items[0].Link != link {
		t.Errorf("keys %q and %q", a, b)
	}
}

func TestAFeedInLatin1IsRead(t *testing.T) {
	doc := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>B\xfccher</title></channel></rss>"
	f, err := parseFeed([]byte(doc), nil)
	if err != nil || f.Title != "Bücher" {
		t.Errorf("%+v, %v", f, err)
	}
}

func TestWhatIsNoFeedIsMalformed(t *testing.T) {
	for name, doc := range map[string]string{
		"html":      "<!doctype html><html><body>Not here</body></html>",
		"broken":    "<rss><channel><title>Cut off</channel>",
		"empty":     "",
		"other xml": `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`,
		"json":      `{"items": []}`,
	} {
		if _, err := parseFeed([]byte(doc), nil); !errors.Is(err, ErrMalformedFeed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAFeedFiltersByKeywordsAndAuthors(t *testing.T) {
	item := feedItem{
		Title: "Der neue Roman", Summary: "Liza Grimm erzählt von Göttern.", Categories: []string{"Fantasy"},
		Authors: []string{"Grimm, Liza"},
	}
	for _, c := range []struct {
		keywords, authors []string
		want              bool
	}{
		{nil, nil, true},
		{[]string{"fantasy"}, nil, true},
		{[]string{"GÖTTERN"}, nil, true},
		{[]string{"Gott"}, nil, false},
		{[]string{"krimi", "roman"}, nil, true},
		{nil, []string{"Liza Grimm"}, true},
		{nil, []string{"Grimm, Liza"}, true},
		{nil, []string{"Bernard Cornwell"}, false},
		{[]string{"krimi"}, []string{"Liza Grimm"}, false},
		{[]string{"roman"}, []string{"Bernard Cornwell", "Liza Grimm"}, true},
	} {
		if got := newFeedFilter(c.keywords, c.authors).matches(item); got != c.want {
			t.Errorf("keywords %v, authors %v: %v", c.keywords, c.authors, got)
		}
	}
	// An author named only in the text is found there, in either order.
	named := feedItem{Title: "Neu von Bernard Cornwell"}
	if !newFeedFilter(nil, []string{"Cornwell, Bernard"}).matches(named) {
		t.Error("an author named in the title is not found")
	}
}

func TestAFeedAddressIsOfTheWeb(t *testing.T) {
	for raw, want := range map[string]string{
		" https://example.org/feed.xml#top ": "https://example.org/feed.xml",
		"http://example.org:8080/rss":        "http://example.org:8080/rss",
	} {
		if got, err := FeedURL(raw); err != nil || got != want {
			t.Errorf("%q: %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "example.org/feed", "file:///etc/passwd", "ftp://example.org/feed", "https://user:pw@example.org/feed", "https:///feed"} {
		if _, err := FeedURL(raw); !errors.Is(err, ErrBadFeedURL) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

// loopback is a client that may reach the test's servers.
func loopback() *http.Client {
	return outbound.NewClient(outbound.Options{AllowPrivate: true, Timeout: feedTimeout})
}

func TestAFeedThatPointsInsideIsRefused(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(rssFeedText))
	}))
	defer srv.Close()
	_, err := getFeed(context.Background(), NewFeedClient(), srv.URL, nil, nil)
	if !errors.Is(err, ErrNoPublicServer) || hits.Load() != 0 {
		t.Fatalf("a feed on the loopback: %v, %d requests", err, hits.Load())
	}
	// Nor does a name that stands for an address inside.
	_, err = getFeed(context.Background(), NewFeedClient(), "http://localhost:1/feed", nil, nil)
	if !errors.Is(err, ErrNoPublicServer) {
		t.Errorf("a name for the loopback: %v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("the error names the address inside: %v", err)
	}
}

func TestAnUnchangedFeedIsOneConditionalRequest(t *testing.T) {
	var requests []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Header.Clone())
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Tue, 03 Nov 2026 08:00:00 GMT")
		_, _ = w.Write([]byte(atomFeedText))
	}))
	defer srv.Close()
	first, err := getFeed(context.Background(), loopback(), srv.URL, nil, nil)
	if err != nil || first.unchanged || len(first.feed.Items) != 1 || *first.etag != `"v1"` || first.lastModified == nil {
		t.Fatalf("first fetch %+v, %v", first, err)
	}
	again, err := getFeed(context.Background(), loopback(), srv.URL, first.etag, first.lastModified)
	if err != nil || !again.unchanged {
		t.Fatalf("second fetch %+v, %v", again, err)
	}
	if len(requests) != 2 || requests[1].Get("If-Modified-Since") != "Tue, 03 Nov 2026 08:00:00 GMT" {
		t.Errorf("requests %v", requests)
	}
}

func TestAnOversizedFeedIsRefused(t *testing.T) {
	big := "<rss><channel>" + strings.Repeat("<item><title>x</title></item>", MaxFeedBytes/20) + "</channel></rss>"
	for name, handler := range map[string]http.HandlerFunc{
		"said": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(len(big)))
			_, _ = w.Write([]byte(big))
		},
		"unsaid": func(w http.ResponseWriter, _ *http.Request) {
			// Without a length: cut off at the limit as it streams.
			w.Header().Set("Content-Type", "application/rss+xml")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(big))
		},
	} {
		srv := httptest.NewServer(handler)
		_, err := getFeed(context.Background(), loopback(), srv.URL, nil, nil)
		srv.Close()
		if !errors.Is(err, ErrFeedTooLarge) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAFeedThatFailsSaysHow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := getFeed(context.Background(), loopback(), srv.URL, nil, nil); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a feed that is not there: %v", err)
	}
}
