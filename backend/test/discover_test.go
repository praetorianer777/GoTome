//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/discover"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// providers answers what the providers would list, by the subject asked.
type providers struct {
	mu       sync.Mutex
	works    map[string][]metadata.Record
	subjects map[string][]metadata.Record
	asked    []string
}

func (p *providers) answer(key string, from map[string][]metadata.Record) []metadata.Answer {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, key)
	if records, ok := from[key]; ok {
		return []metadata.Answer{{Provider: "fake", Records: slices.Clone(records)}}
	}
	return nil
}

func (p *providers) WorksOf(_ context.Context, kind, name string) ([]metadata.Answer, error) {
	return p.answer(kind+":"+name, p.works), nil
}

func (p *providers) SubjectsOf(_ context.Context, subject string) ([]metadata.Answer, error) {
	return p.answer("genre:"+subject, p.subjects), nil
}

func (p *providers) askedAbout() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asked)
}

// tastes says how near the person's taste each title is.
type tastes map[string]float64

func (tastes) Profile(context.Context, library.Scope) (similar.Profile, bool, error) {
	return similar.Profile{}, true, nil
}

func (t tastes) Resemble(_ context.Context, _ similar.Profile, texts []string) ([]*similar.Resemblance, error) {
	out := make([]*similar.Resemblance, len(texts))
	for i, text := range texts {
		for title, score := range t {
			if len(text) >= len(title) && text[:len(title)] == title {
				out[i] = &similar.Resemblance{Score: score, Because: []similar.Because{{Title: "Liked " + title}}}
			}
		}
	}
	return out, nil
}

func listed(authors ...string) []catalog.NewContributor {
	var out []catalog.NewContributor
	for _, a := range authors {
		out = append(out, catalog.NewContributor{Name: a, Role: catalog.RoleAuthor})
	}
	return out
}

func offered(id, title string, authors ...string) metadata.Record {
	return metadata.Record{ID: id, Title: title, Contributors: listed(authors...), Published: "2020"}
}

type discovering struct {
	*app
	admin     *http.Client
	providers *providers
	service   *discover.Service
}

func newDiscovering(t *testing.T, taste discover.Taster) discovering {
	t.Helper()
	a := newApp(t)
	d := discovering{app: a, providers: &providers{works: map[string][]metadata.Record{}, subjects: map[string][]metadata.Record{}}}
	d.admin, _ = a.signedIn("admin", "admin")
	a.server.Metadata = metadata.NewService(a.pool, nil, metadata.Options{})
	d.service = discover.NewService(a.pool, d.providers, taste, slog.New(slog.DiscardHandler))
	d.service.Enabled = func(ctx context.Context) bool {
		v, _ := a.settings.Text(ctx, "recommendations.discover")
		return v == "on"
	}
	a.server.Discover = d.service
	a.setSetting(d.admin, "recommendations.discover", "on")
	return d
}

// run asks for a run as the person and works it to its end.
func (d discovering) run(c *http.Client) {
	d.t.Helper()
	if status, out, _ := d.call(c, http.MethodPost, "/me/discover/run", nil); status != 202 {
		d.t.Fatalf("run: %d %v", status, out)
	}
	complete, err := d.service.Run(context.Background(), time.Now().Add(time.Minute))
	if err != nil || !complete {
		d.t.Fatalf("Run: complete %v, %v", complete, err)
	}
}

func (d discovering) discovered(c *http.Client) (string, []map[string]any) {
	d.t.Helper()
	status, out, _ := d.call(c, http.MethodGet, "/me/discover", nil)
	if status != 200 {
		d.t.Fatalf("discover: %d %v", status, out)
	}
	var items []map[string]any
	for _, it := range out["items"].([]any) {
		items = append(items, it.(map[string]any))
	}
	return out["state"].(string), items
}

func titlesOf(items []map[string]any) []string {
	var out []string
	for _, it := range items {
		out = append(out, it["title"].(string))
	}
	return out
}

func reasonOf(items []map[string]any, title string) map[string]any {
	for _, it := range items {
		if it["title"] == title {
			return it["reason"].(map[string]any)
		}
	}
	return nil
}

// liked has the person finish a book of the library by the author, with stars.
func (d discovering) liked(c *http.Client, libraryID, title, author string, stars int) uuid.UUID {
	d.t.Helper()
	id := d.bookBy(libraryID, title, author)
	d.rate(c, id, stars)
	return id
}

// placeholder is a book of the person's own that no library holds: a wish,
// or a book read.
func (d discovering) placeholder(libraryID, userID, title, author, status string) {
	d.t.Helper()
	id := d.bookBy(libraryID, title, author)
	if _, err := d.pool.Exec(context.Background(), `UPDATE books SET placeholder = true WHERE id = $1`, id); err != nil {
		d.t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO user_books (user_id, book_id, status) VALUES ($1, $2, $3)`, userID, id, status); err != nil {
		d.t.Fatal(err)
	}
}

func TestDiscoverLeavesOutWhatIsHeldWishedReadOrDismissed(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	reader, readerID := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	vault := d.library(d.admin, "Vault", "private")
	d.liked(reader, open, "Harbour Lamp", "Ada Moss", 5)
	d.bookBy(open, "Owned Title", "Ada Moss")
	ownedByIsbn := d.bookBy(open, "Known Under Another Title", "Someone Else")
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO book_identifiers (book_id, type, value) VALUES ($1, 'isbn', '9780306406157')`, ownedByIsbn); err != nil {
		t.Fatal(err)
	}
	d.bookBy(vault, "Vault Title", "Ada Moss")
	d.placeholder(open, readerID, "Wished Title", "Ada Moss", "wishlist")
	d.placeholder(open, readerID, "Read Title", "Moss, Ada", "completed")
	isbn := offered("i1", "Reprinted As Something Else", "Ada Moss")
	isbn.Identifiers = []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780306406157"}}
	d.providers.works["author:Ada Moss"] = []metadata.Record{
		offered("a1", "Owned Title", "Ada Moss"),
		offered("a2", "Vault Title", "Ada Moss"),
		offered("a3", "Wished Title", "Ada Moss"),
		offered("a4", "Read Title", "Ada Moss"),
		offered("a5", "Dismissed Title", "Ada Moss"),
		offered("a6", "Fresh Title", "Ada Moss"),
		{ID: "a7", Work: "w6", Title: "Fresh Title (Large Print)", Contributors: listed("Ada Moss")},
		offered("a8", "Fresh Title", "Ada Moss"),
		isbn,
	}
	records := d.providers.works["author:Ada Moss"]
	records[5].Work, records[7].Work = "w6", "w6"

	if state, _ := d.discovered(reader); state != "never" {
		t.Errorf("before a run: %q", state)
	}
	d.run(reader)
	state, items := d.discovered(reader)
	titles := titlesOf(items)
	if state != "ready" || !sameTitles(titles, "Fresh Title", "Vault Title", "Dismissed Title") {
		t.Fatalf("state %q, offered %v", state, titles)
	}
	if why := reasonOf(items, "Fresh Title"); why["kind"] != "author" || why["name"] != "Ada Moss" || why["stars"] != float64(5) {
		t.Errorf("reason = %v", why)
	}

	var dismissed string
	for _, it := range items {
		if it["title"] == "Dismissed Title" {
			dismissed = it["id"].(string)
		}
	}
	if status, _, _ := d.call(reader, http.MethodPost, "/me/discover/"+dismissed+"/dismiss", nil); status != 204 {
		t.Errorf("dismiss: %d", status)
	}
	if status, _, _ := d.call(reader, http.MethodPost, "/me/discover/"+dismissed+"/dismiss", nil); status != 404 {
		t.Errorf("dismissing again: %d", status)
	}
	d.pool.Exec(context.Background(), `UPDATE discover_runs SET finished_at = now() - interval '2 days'`)
	d.run(reader)
	if _, items := d.discovered(reader); !sameTitles(titlesOf(items), "Fresh Title", "Vault Title") {
		t.Errorf("after a dismissal and a new run: %v", titlesOf(items))
	}

	// A library the person cannot see neither counts as owned nor shows in
	// the answer: with the vault's book gone the answer is the same.
	_, before := d.discovered(reader)
	d.pool.Exec(context.Background(), `UPDATE books SET deleted_at = now() WHERE title = 'Vault Title'`)
	_, after := d.discovered(reader)
	if !sameTitles(titlesOf(before), titlesOf(after)...) {
		t.Errorf("the vault's book changes the answer: %v, %v", titlesOf(before), titlesOf(after))
	}
	// Someone who sees every library has it as owned.
	d.pool.Exec(context.Background(), `UPDATE books SET deleted_at = NULL WHERE title = 'Vault Title'`)
	d.liked(d.admin, open, "Admin Lamp", "Ada Moss", 5)
	d.run(d.admin)
	if _, items := d.discovered(d.admin); slices.Contains(titlesOf(items), "Vault Title") || !slices.Contains(titlesOf(items), "Fresh Title") {
		t.Errorf("a person who sees the vault is offered %v", titlesOf(items))
	}
}

func sameTitles(got []string, want ...string) bool {
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}

func TestDiscoverOffersNothingToWhoRatedNothingAndKeepsToTheCaller(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	reader, _ := d.signedIn("reader", "reader")
	other, _ := d.signedIn("other", "reader")
	open := d.library(d.admin, "Open", "shared")
	d.liked(reader, open, "Harbour Lamp", "Ada Moss", 5)
	d.providers.works["author:Ada Moss"] = []metadata.Record{offered("a1", "Fresh Title", "Ada Moss")}
	d.run(reader)
	d.run(other)
	if _, items := d.discovered(reader); len(items) != 1 {
		t.Errorf("the reader is offered %v", titlesOf(items))
	}
	state, items := d.discovered(other)
	if state != "ready" || len(items) != 0 {
		t.Errorf("someone who rated nothing: %q, %v", state, titlesOf(items))
	}
	if status, _, _ := d.call(other, http.MethodPost, "/me/discover/"+uuid.NewString()+"/dismiss", nil); status != 404 {
		t.Errorf("dismissing a book that is nobody's: %d", status)
	}
}

func TestDiscoverRanksSignalsWithTheTasteProfile(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, tastes{
		"Lantern Four": 0.5, "Lantern Two": 0.1, "Moss Again": 0.6, "Fen Tale": 0.9, "Moss Dull": 0,
	})
	reader, _ := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	r := recommendable{d.app, uuid.MustParse(open)}
	for i, volume := range []float64{1, 3} {
		id := r.book(t, "Lantern "+[]string{"One", "Three"}[i], "Tomas Reed", "Lantern Cycle", volume, direction(map[int]float32{0: 1}))
		d.rate(reader, id, 4)
	}
	d.liked(reader, open, "Harbour Lamp", "Ada Moss", 5)
	harbour := d.bookBy(open, "Fen Source", "Nora Vale")
	d.rate(reader, harbour, 5)
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO book_genres (book_id, genre, source) VALUES ($1, 'fantasy', 'tags')`, harbour); err != nil {
		t.Fatal(err)
	}
	series := func(id, title string, index float64) metadata.Record {
		r := offered(id, title, "Tomas Reed")
		r.Series, r.SeriesIndex = "Lantern Cycle", &index
		return r
	}
	d.providers.works["series:Lantern Cycle"] = []metadata.Record{
		series("s1", "Lantern One", 1), series("s2", "Lantern Two", 2), series("s3", "Lantern Three", 3),
		series("s4", "Lantern Four", 4), series("s5", "Lantern Five", 5), series("s6", "Lantern Six", 6), series("s7", "Lantern Seven", 7),
	}
	d.providers.works["author:Ada Moss"] = []metadata.Record{offered("m1", "Moss Again", "Ada Moss"), offered("m2", "Moss Dull", "Ada Moss")}
	d.providers.subjects["genre:fantasy"] = []metadata.Record{offered("g1", "Fen Tale", "Pia Quill")}

	d.run(reader)
	_, items := d.discovered(reader)
	// Scores: the next volume 0.6 + 0.4 * 0.5, the author's book 0.5 + 0.4 * 0.6,
	// the genre 0.3 + 0.4 * 0.9, the gap 0.35 + 0.4 * 0.1, then the volume after
	// the next 0.2 (no taste known), and the author's other book at 0.5.
	want := []string{"Lantern Four", "Moss Again", "Fen Tale", "Moss Dull", "Lantern Two", "Lantern Five", "Lantern Six"}
	if got := titlesOf(items); !slices.Equal(got, want) {
		t.Fatalf("offered %v, want %v", got, want)
	}
	for title, kind := range map[string]string{
		"Lantern Four": "series_next", "Lantern Two": "series_gap", "Lantern Five": "series_more", "Moss Again": "author", "Fen Tale": "genre",
	} {
		if why := reasonOf(items, title); why["kind"] != kind {
			t.Errorf("%s: reason %v, want %s", title, why, kind)
		}
	}
	if why := reasonOf(items, "Fen Tale"); why["name"] != "fantasy" {
		t.Errorf("genre reason %v", why)
	}
	if why := reasonOf(items, "Moss Again"); len(why["because"].([]any)) != 1 || why["because"].([]any)[0] != "Liked Moss Again" {
		t.Errorf("because %v", why["because"])
	}
}

func TestDiscoverWorksInPassesAndAsksEachSubjectOnce(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	reader, _ := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	for _, author := range []string{"Ada Moss", "Nora Vale", "Tomas Reed", "Ida Brandt"} {
		d.liked(reader, open, "Book by "+author, author, 5)
		d.providers.works["author:"+author] = []metadata.Record{offered(author, "New by "+author, author)}
	}
	d.service.AsksPerPass = 3
	if status, _, _ := d.call(reader, http.MethodPost, "/me/discover/run", nil); status != 202 {
		t.Fatalf("run: %d", status)
	}
	complete, err := d.service.Run(context.Background(), time.Now().Add(time.Minute))
	if err != nil || complete {
		t.Fatalf("first pass: complete %v, %v", complete, err)
	}
	if n := len(d.providers.askedAbout()); n != 3 {
		t.Errorf("the first pass asked %d times: %v", n, d.providers.askedAbout())
	}
	if state, _ := d.discovered(reader); state != "running" {
		t.Errorf("between passes: %q", state)
	}
	complete, err = d.service.Run(context.Background(), time.Now().Add(time.Minute))
	if err != nil || !complete {
		t.Fatalf("second pass: complete %v, %v", complete, err)
	}
	asked := d.providers.askedAbout()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"author:Ada Moss", "author:Ida Brandt", "author:Nora Vale", "author:Tomas Reed"}) {
		t.Errorf("asked %v", asked)
	}
	state, items := d.discovered(reader)
	if state != "ready" || len(items) != 4 {
		t.Errorf("%q, %v", state, titlesOf(items))
	}

	// Run again, and the result is the same with nobody asked.
	for range 2 {
		if complete, err := d.service.Run(context.Background(), time.Now().Add(time.Minute)); err != nil || !complete {
			t.Fatalf("Run: %v, %v", complete, err)
		}
	}
	if n := len(d.providers.askedAbout()); n != 4 {
		t.Errorf("a finished run asked again: %d asks", n)
	}
	if _, again := d.discovered(reader); !slices.Equal(titlesOf(again), titlesOf(items)) {
		t.Errorf("the result changed: %v, %v", titlesOf(again), titlesOf(items))
	}

	// A deadline that has passed asks nobody, and a day later all are asked again.
	d.service.AsksPerPass = 0
	d.pool.Exec(context.Background(), `UPDATE discover_runs SET finished_at = now() - interval '2 days'`)
	if complete, err := d.service.Run(context.Background(), time.Now().Add(-time.Second)); err != nil || complete {
		t.Errorf("past the deadline: complete %v, %v", complete, err)
	}
	if n := len(d.providers.askedAbout()); n != 4 {
		t.Errorf("asked after the deadline: %d", n)
	}
	if complete, err := d.service.Run(context.Background(), time.Now().Add(time.Minute)); err != nil || !complete {
		t.Fatalf("Run: %v, %v", complete, err)
	}
	if n := len(d.providers.askedAbout()); n != 8 {
		t.Errorf("a new day's run asked %d times in all", n)
	}
	if _, again := d.discovered(reader); len(again) != 4 {
		t.Errorf("after the new run: %v", titlesOf(again))
	}
}

func TestDiscoverTakesWhatFollowingKnowsWithoutAsking(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	reader, _ := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	d.liked(reader, open, "Harbour Lamp", "Ada Moss", 5)
	d.works.set(metadata.Answer{Provider: "fake", Records: []metadata.Record{work("Known Through Following", "2021", "Ada Moss")}})
	d.follow(reader, "author", "Ada Moss")
	d.poll()
	d.run(reader)
	_, items := d.discovered(reader)
	if !slices.Equal(titlesOf(items), []string{"Known Through Following"}) {
		t.Errorf("offered %v", titlesOf(items))
	}
	if asked := d.providers.askedAbout(); len(asked) != 0 {
		t.Errorf("the providers were asked again: %v", asked)
	}
}

func TestDiscoverIsOffOrOfflineWithASentenceNotAnAnswer(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	reader, _ := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	d.liked(reader, open, "Harbour Lamp", "Ada Moss", 5)
	d.providers.works["author:Ada Moss"] = []metadata.Record{offered("a1", "Fresh Title", "Ada Moss")}

	d.setSetting(d.admin, "recommendations.discover", "off")
	if state, items := d.discovered(reader); state != "off" || len(items) != 0 {
		t.Errorf("off: %q, %v", state, items)
	}
	if status, out, _ := d.call(reader, http.MethodPost, "/me/discover/run", nil); status != 409 {
		t.Errorf("a run while off: %d %v", status, out)
	}
	d.setSetting(d.admin, "recommendations.discover", "on")
	d.service.Offline = true
	if state, items := d.discovered(reader); state != "offline" || len(items) != 0 {
		t.Errorf("offline: %q, %v", state, items)
	}
	if status, out, _ := d.call(reader, http.MethodPost, "/me/discover/run", nil); status != 409 {
		t.Errorf("a run while offline: %d %v", status, out)
	}
	if complete, err := d.service.Run(context.Background(), time.Now().Add(time.Minute)); err != nil || !complete {
		t.Errorf("Run offline: %v, %v", complete, err)
	}
	if asked := d.providers.askedAbout(); len(asked) != 0 {
		t.Errorf("a provider was asked: %v", asked)
	}
}

func TestDiscoverUsesTheTasteProfileOfTheLibrary(t *testing.T) {
	t.Parallel()
	d := newDiscovering(t, nil)
	d.service = discover.NewService(d.pool, d.providers, d.server.Similar, slog.New(slog.DiscardHandler))
	d.service.Enabled = func(context.Context) bool { return true }
	d.server.Discover = d.service
	reader, _ := d.signedIn("reader", "reader")
	open := d.library(d.admin, "Open", "shared")
	r := recommendable{d.app, uuid.MustParse(open)}
	for i, title := range []string{"Crime One", "Crime Two", "Crime Three"} {
		d.rate(reader, r.book(t, title, "Ada Moss", "", 0, direction(map[int]float32{0: 1, 3: float32(i) / 10})), 5)
	}
	d.providers.works["author:Ada Moss"] = []metadata.Record{offered("a1", "A Crime Not Held", "Ada Moss")}
	d.run(reader)
	_, items := d.discovered(reader)
	if len(items) != 1 {
		t.Fatalf("offered %v", titlesOf(items))
	}
	why := items[0]["reason"].(map[string]any)["because"].([]any)
	if len(why) == 0 || len(why) > 2 {
		t.Fatalf("because %v", why)
	}
	for _, title := range why {
		if s := title.(string); len(s) < 9 || s[:5] != "Crime" {
			t.Errorf("because of %q", s)
		}
	}
}
