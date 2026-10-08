//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// fakeWorks lists, for whatever is asked, what the test set last.
type fakeWorks struct {
	mu      sync.Mutex
	answers []metadata.Answer
	asked   int
}

func (f *fakeWorks) set(answers ...metadata.Answer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = answers
}

func (f *fakeWorks) WorksOf(_ context.Context, _, _ string) ([]metadata.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	return slices.Clone(f.answers), nil
}

func work(title, published string, authors ...string) metadata.Record {
	r := metadata.Record{ID: title, Title: title, Published: published}
	for _, a := range authors {
		r.Contributors = append(r.Contributors, catalog.NewContributor{Name: a, Role: catalog.RoleAuthor})
	}
	return r
}

// in is the day so many days from today, as the database counts days: in UTC.
func in(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format(time.DateOnly) }

// bookBy is a book of the library by the author, as a scan would make it.
func (a *app) bookBy(libraryID, title, author string) uuid.UUID {
	a.t.Helper()
	id := a.bookIn(libraryID, title)
	if _, err := a.pool.Exec(context.Background(), `
		WITH au AS (INSERT INTO authors (name, sort_name, name_key) VALUES ($2, $2, $3)
		            ON CONFLICT (name_key) DO UPDATE SET name = authors.name RETURNING id)
		INSERT INTO book_contributors (book_id, author_id, role, position) SELECT $1, id, 'author', 0 FROM au`,
		id, author, catalog.Key(author)); err != nil {
		a.t.Fatal(err)
	}
	return id
}

func (a *app) follow(c *http.Client, kind, name string) map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/trackers", map[string]any{"kind": kind, "name": name})
	if status != 201 {
		a.t.Fatalf("follow %s: %d %v", name, status, out)
	}
	return out
}

// poll asks about every subject as if a day had passed since the last time.
func (a *app) poll() {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(), `UPDATE release_subjects SET polled_at = polled_at - interval '2 days'`); err != nil {
		a.t.Fatal(err)
	}
	complete, err := a.server.Releases.Poll(context.Background(), time.Now().Add(time.Minute))
	if err != nil || !complete {
		a.t.Fatalf("poll: %v, complete %v", err, complete)
	}
}

func (a *app) releases(c *http.Client, when string) map[string]map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/releases?when="+when, nil)
	if status != 200 {
		a.t.Fatalf("releases: %d %v", status, out)
	}
	byTitle := map[string]map[string]any{}
	for _, r := range out["releases"].([]any) {
		r := r.(map[string]any)
		byTitle[r["title"].(string)] = r
	}
	return byTitle
}

// titlesTold names each notification by its kind and title, or, for
// several events told as one, their count.
func titlesTold(list []told) []string {
	var out []string
	for _, n := range list {
		if title, ok := n.Data["title"].(string); ok {
			out = append(out, n.Kind+" "+title)
		} else {
			out = append(out, fmt.Sprintf("%s x%v", n.Kind, n.Data["count"]))
		}
	}
	slices.Sort(out)
	return out
}

func TestAFollowedAuthorsNewBookIsToldOnceToEachFollowerWithoutIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	events := a.withEvents()
	a.server.Releases.Events = events
	admin, _ := a.signedIn("admin", "admin")
	ann, _ := a.signedIn("ann", "reader")
	bob, _ := a.signedIn("bob", "reader")
	shared := a.library(admin, "Shared", "shared")
	vault := a.library(admin, "Vault", "private")
	a.bookBy(shared, "The Old One", "Writer, Wanda")
	// Only the administrator sees the vault, and so has this book.
	a.bookBy(vault, "Another New", "Wanda Writer")

	// One author, in either order of the name: one subject for all three.
	first := a.follow(ann, "author", "Writer, Wanda")
	a.follow(bob, "author", "Wanda Writer")
	a.follow(admin, "author", "wanda writer")
	if again := a.follow(ann, "author", "Wanda Writer"); again["id"] != first["id"] {
		t.Errorf("following again made another tracker: %v, %v", again, first)
	}
	if n := a.count("release_subjects"); n != 1 {
		t.Errorf("%d subjects for one author", n)
	}
	status, list, _ := a.call(ann, http.MethodGet, "/trackers", nil)
	if trackers := list["trackers"].([]any); status != 200 || len(trackers) != 1 || trackers[0].(map[string]any)["name"] != "Writer, Wanda" {
		t.Errorf("ann's trackers: %d %v", status, list)
	}

	// What the sources list at first is the author's past, even a book
	// still to come: nobody is told of it as new.
	a.works.set(metadata.Answer{Provider: "shelf", Records: []metadata.Record{
		work("The Old One", "2019", "Wanda Writer"),
		work("Coming Anyway", in(40), "Wanda Writer"),
	}})
	a.poll()
	a.flush(events)
	for name, c := range map[string]*http.Client{"ann": ann, "bob": bob, "admin": admin} {
		if got := a.told(c); len(got) != 0 {
			t.Errorf("%s is told of the author's past: %v", name, got)
		}
	}
	if got := a.releases(ann, "upcoming"); got["Coming Anyway"] == nil {
		t.Errorf("the upcoming book is not listed: %v", got)
	}

	// Later the sources list new books: one still to come, one out last
	// week, one the administrator has, and the old one with its day.
	a.works.set(metadata.Answer{Provider: "shelf", Records: []metadata.Record{
		work("The Next One", in(30), "Wanda Writer"),
		work("Just Out", in(-7), "Wanda Writer"),
		work("Another New", in(60), "Wanda Writer"),
		work("The Old One", "2019-03-01", "Wanda Writer"),
		work("Coming Anyway", in(40), "Wanda Writer"),
	}}, metadata.Answer{Provider: "second", Records: []metadata.Record{
		// Another source's first answer is its past, but these books are
		// known already: the same, not again.
		work("The Next One: A Novel", in(30), "Wanda Writer"),
	}})
	a.poll()
	// Following now is after the books were found: no news to this one.
	late, _ := a.signedIn("late", "reader")
	a.follow(late, "author", "Wanda Writer")
	a.poll()
	a.flush(events)

	// Two announcements found in one poll are one notification.
	want := []string{"release.announced x2", "release.out Just Out"}
	for name, c := range map[string]*http.Client{"ann": ann, "bob": bob} {
		if got := titlesTold(a.told(c)); !slices.Equal(got, want) {
			t.Errorf("%s is told %v, want %v", name, got, want)
		}
	}
	if got := titlesTold(a.told(admin)); !slices.Equal(got, []string{"release.announced The Next One", "release.out Just Out"}) {
		t.Errorf("the administrator, who has Another New, is told %v", got)
	}
	if got := a.told(late); len(got) != 0 {
		t.Errorf("who followed after the books were found is told %v", got)
	}
	if n := a.count("releases"); n != 5 {
		t.Errorf("%d releases, want 5: the same title from two sources is one", n)
	}

	upcoming := a.releases(admin, "upcoming")
	if upcoming["Another New"]["inLibrary"] != true || upcoming["The Next One"]["inLibrary"] != false {
		t.Errorf("the administrator's upcoming books: %v", upcoming)
	}
	if a.releases(ann, "upcoming")["Another New"]["inLibrary"] != false {
		t.Error("ann is shown a book of a library she does not see as hers")
	}
	if recent := a.releases(ann, "recent"); recent["Just Out"] == nil || recent["The Old One"] != nil {
		t.Errorf("recent books: %v", recent)
	}

	// The day of an upcoming book comes, here as its source moves it to
	// today: those who followed before that day are told it is out, once
	// however often the poll runs.
	a.works.set(metadata.Answer{Provider: "shelf", Records: []metadata.Record{work("The Next One", in(0), "Wanda Writer")}})
	if _, err := a.pool.Exec(context.Background(), `UPDATE trackers SET created_at = now() - interval '60 days'`); err != nil {
		t.Fatal(err)
	}
	a.poll()
	a.poll()
	a.flush(events)
	if got := titlesTold(a.told(bob)); !slices.Contains(got, "release.out The Next One") || len(got) != 3 {
		t.Errorf("bob is told %v", got)
	}
}

func TestFollowingIsOnesOwn(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ann, _ := a.signedIn("ann", "reader")
	bob, _ := a.signedIn("bob", "reader")
	for _, body := range []map[string]any{{"kind": "publisher", "name": "X"}, {"kind": "series", "name": " ,. "}} {
		if status, out, _ := a.call(ann, http.MethodPost, "/trackers", body); status != 422 {
			t.Errorf("follow %v: %d %v", body, status, out)
		}
	}
	mine := a.follow(ann, "series", "The Long Road")["id"].(string)
	a.follow(bob, "series", "the long road")
	if status, _, _ := a.call(bob, http.MethodDelete, "/trackers/"+mine, nil); status != 404 {
		t.Errorf("bob ends ann's tracker: %d", status)
	}
	if status, _, _ := a.call(ann, http.MethodDelete, "/trackers/"+mine, nil); status != 204 {
		t.Errorf("ann ends her tracker: %d", status)
	}
	if n := a.count("release_subjects"); n != 1 {
		t.Errorf("%d subjects while bob still follows", n)
	}
	_, list, _ := a.call(bob, http.MethodGet, "/trackers", nil)
	if status, _, _ := a.call(bob, http.MethodDelete, "/trackers/"+list["trackers"].([]any)[0].(map[string]any)["id"].(string), nil); status != 204 {
		t.Errorf("bob ends his tracker: %d", status)
	}
	if n := a.count("release_subjects"); n != 0 {
		t.Errorf("%d subjects nobody follows", n)
	}
}

// TestAReleaseIsInTheLibraryOnlyWhereItIsSeen is the visibility test's
// proof for GET /releases: the releases are of what the caller follows,
// which the sources list for anyone, but whether a private library holds
// one is told only to those who see it.
func TestAReleaseIsInTheLibraryOnlyWhereItIsSeen(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	stranger, _ := a.signedIn("stranger", "reader")
	vault := a.library(admin, "Vault", "private")
	a.bookBy(vault, "Hidden Away", "Sam Secret")
	for _, c := range []*http.Client{admin, stranger} {
		a.follow(c, "author", "Secret, Sam")
	}
	a.works.set(metadata.Answer{Provider: "shelf", Records: []metadata.Record{work("Hidden Away", in(10), "Sam Secret")}})
	a.poll()
	if got := a.releases(admin, "upcoming")["Hidden Away"]; got == nil || got["inLibrary"] != true {
		t.Errorf("the administrator: %v", got)
	}
	if got := a.releases(stranger, "upcoming")["Hidden Away"]; got == nil || got["inLibrary"] != false {
		t.Errorf("the stranger: %v", got)
	}
}
