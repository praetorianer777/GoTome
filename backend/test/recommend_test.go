//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

type recommendable struct {
	a       *app
	library uuid.UUID
}

func (r recommendable) book(t *testing.T, title, author, series string, index float64, v []float32) uuid.UUID {
	t.Helper()
	in := catalog.NewBook{LibraryID: r.library, Title: title, Series: series}
	if author != "" {
		in.Contributors = []catalog.NewContributor{{Name: author, Role: catalog.RoleAuthor}}
	}
	if series != "" {
		in.SeriesIndex = &index
	}
	id, err := catalog.NewService(r.a.pool).CreateBook(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	r.a.putVector(id, similar.KindContent, v)
	r.a.putVector(id, similar.KindMetadata, v)
	return id
}

func (a *app) recommended(c *http.Client) (bool, []string, []map[string]any) {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/me/recommendations?limit=20", nil)
	if status != 200 {
		a.t.Fatalf("recommendations: %d %v", status, out)
	}
	var titles []string
	var items []map[string]any
	for _, b := range out["books"].([]any) {
		item := b.(map[string]any)
		items = append(items, item)
		titles = append(titles, item["book"].(map[string]any)["title"].(string))
	}
	return out["hasProfile"] == true, titles, items
}

func (a *app) rate(c *http.Client, book uuid.UUID, stars int) {
	a.t.Helper()
	body := map[string]any{"status": "completed"}
	if stars > 0 {
		body["rating"] = stars
	}
	if status, out, _ := a.call(c, http.MethodPut, "/books/"+book.String()+"/reading", body); status != 200 {
		a.t.Fatalf("rate: %d %v", status, out)
	}
}

func TestRecommendationsComeFromWhatThePersonLikedInEachOfTheirTastes(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	open := uuid.MustParse(a.library(admin, "Open", "shared"))
	vault := uuid.MustParse(a.library(admin, "Vault", "private"))
	r := recommendable{a, open}
	crime := func(w float32) []float32 { return direction(map[int]float32{0: 1, 3: w}) }
	fantasy := func(w float32) []float32 { return direction(map[int]float32{1: 1, 3: w}) }

	for i, title := range []string{"Crime One", "Crime Two", "Crime Three"} {
		a.rate(reader, r.book(t, title, "", "", 0, crime(float32(i)/10)), []int{5, 4, 0}[i])
	}
	for i, title := range []string{"Fantasy One", "Fantasy Two", "Fantasy Three"} {
		a.rate(reader, r.book(t, title, "", "", 0, fantasy(float32(i)/10)), []int{5, 5, 4}[i])
	}
	if has, _, _ := a.recommended(stranger); has {
		t.Error("someone who read nothing has a profile")
	}

	crimeNext := r.book(t, "Crime Next", "", "", 0, crime(0.05))
	r.book(t, "Fantasy Next", "", "", 0, fantasy(0.05))
	r.book(t, "Romance", "", "", 0, direction(map[int]float32{2: 1}))
	read := r.book(t, "Crime Abandoned", "", "", 0, crime(0.01))
	a.call(reader, http.MethodPut, "/books/"+read.String()+"/reading", map[string]any{"status": "abandoned"})
	dismissed := r.book(t, "Crime Dismissed", "", "", 0, crime(0.02))
	wishlisted := r.book(t, "Crime Wished", "", "", 0, crime(0.03))
	a.call(reader, http.MethodPut, "/books/"+wishlisted.String()+"/reading", map[string]any{"status": "wishlist"})
	placeholder := r.book(t, "Crime Placeholder", "", "", 0, crime(0.04))
	a.pool.Exec(context.Background(), `UPDATE books SET placeholder = true WHERE id = $1`, placeholder)
	secret := recommendable{a, vault}.book(t, "Crime Secret", "", "", 0, crime(0))

	has, titles, items := a.recommended(reader)
	if !has {
		t.Fatal("no profile")
	}
	for _, want := range []string{"Crime Next", "Fantasy Next", "Crime Dismissed", "Crime Wished", "Romance"} {
		if !slices.Contains(titles, want) {
			t.Errorf("%q is not recommended: %v", want, titles)
		}
	}
	for _, not := range []string{"Crime Abandoned", "Crime Placeholder", "Crime Secret", "Crime One", "Fantasy One"} {
		if slices.Contains(titles, not) {
			t.Errorf("%q is recommended: %v", not, titles)
		}
	}
	if slices.Index(titles, "Romance") < slices.Index(titles, "Crime Next") || slices.Index(titles, "Romance") < slices.Index(titles, "Fantasy Next") {
		t.Errorf("both tastes come before what neither is: %v", titles)
	}
	for _, item := range items {
		why := item["because"].([]any)
		name := item["book"].(map[string]any)["title"].(string)
		if len(why) == 0 || len(why) > 2 {
			t.Errorf("%s: because %v", name, why)
			continue
		}
		first := why[0].(map[string]any)["title"].(string)
		switch name {
		case "Crime Next":
			if first[:5] != "Crime" {
				t.Errorf("Crime Next is recommended because of %q", first)
			}
		case "Fantasy Next":
			if first[:7] != "Fantasy" {
				t.Errorf("Fantasy Next is recommended because of %q", first)
			}
		}
	}

	if status, _, _ := a.call(reader, http.MethodPost, "/me/recommendations/"+dismissed.String()+"/dismiss", nil); status != 204 {
		t.Errorf("dismiss: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/me/recommendations/"+secret.String()+"/dismiss", nil); status != 404 {
		t.Errorf("dismissing a book of a private library: %d", status)
	}
	if _, titles, _ = a.recommended(reader); slices.Contains(titles, "Crime Dismissed") || !slices.Contains(titles, "Crime Next") {
		t.Errorf("after dismissing: %v", titles)
	}
	// Nobody else's dismissal, rating or profile counts.
	a.rate(stranger, crimeNext, 5)
	if _, titles, _ = a.recommended(reader); !slices.Contains(titles, "Crime Next") {
		t.Errorf("another person's reading changed the list: %v", titles)
	}
	if status, _, _ := a.call(stranger, http.MethodPost, "/me/recommendations/"+crimeNext.String()+"/dismiss", nil); status != 204 {
		t.Errorf("another dismisses: %d", status)
	}
	if _, titles, _ = a.recommended(reader); !slices.Contains(titles, "Crime Next") {
		t.Errorf("another person's dismissal changed the list: %v", titles)
	}
}

func TestRecommendationsStayWithinTheLibrariesThePersonSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")
	insider, insiderID := a.signedIn("insider", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	open := uuid.MustParse(a.library(admin, "Open", "shared"))
	vault := a.library(admin, "Vault", "private")
	_, _ = adminID, admin
	if status, out, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+insiderID, nil); status != 204 {
		t.Fatalf("membership: %d %v", status, out)
	}
	_ = insiderID
	crime := func(w float32) []float32 { return direction(map[int]float32{0: 1, 3: w}) }
	for _, c := range []*http.Client{insider, stranger} {
		for i, title := range []string{"One " + uuid.NewString(), "Two " + uuid.NewString(), "Three " + uuid.NewString()} {
			a.rate(c, recommendable{a, open}.book(t, title, "", "", 0, crime(float32(i)/10)), 5)
		}
	}
	recommendable{a, uuid.MustParse(vault)}.book(t, "Secretum", "", "", 0, crime(0.05))
	if _, titles, _ := a.recommended(insider); !slices.Contains(titles, "Secretum") {
		t.Errorf("a member of the library is not shown its book: %v", titles)
	}
	if _, titles, _ := a.recommended(stranger); slices.Contains(titles, "Secretum") {
		t.Errorf("a stranger is shown a private library's book: %v", titles)
	}
}

func TestRecommendationsKeepToTwoBooksPerAuthorAndTheNextVolumeOfASeries(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	open := uuid.MustParse(a.library(admin, "Open", "shared"))
	r := recommendable{a, open}
	crime := func(w float32) []float32 { return direction(map[int]float32{0: 1, 3: w}) }
	for i := range 3 {
		a.rate(reader, r.book(t, "Liked "+string(rune('A'+i)), "Someone Else", "", 0, crime(float32(i)/10)), 5)
	}
	volume1 := r.book(t, "Saga 1", "Saga Author", "The Saga", 1, crime(0.01))
	a.rate(reader, volume1, 5)
	for i := 2; i <= 4; i++ {
		r.book(t, "Saga "+string(rune('0'+i)), "Saga Author", "The Saga", float64(i), crime(0.01*float32(i)))
	}
	for i := range 4 {
		r.book(t, "Prolific "+string(rune('A'+i)), "Prolific Author", "", 0, crime(0.5+0.01*float32(i)))
	}
	_, titles, _ := a.recommended(reader)
	var saga, prolific []string
	for _, title := range titles {
		switch title[:4] {
		case "Saga":
			saga = append(saga, title)
		case "Prol":
			prolific = append(prolific, title)
		}
	}
	if !slices.Equal(saga, []string{"Saga 2"}) {
		t.Errorf("the series: %v", saga)
	}
	if len(prolific) != 2 {
		t.Errorf("one author: %v", prolific)
	}
}

func TestTheRecommendationCheckHidesBooksAndCountsHowManyComeBack(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")
	open := uuid.MustParse(a.library(admin, "Open", "shared"))
	r := recommendable{a, open}
	for i := range 30 {
		a.rate(reader, r.book(t, "Crime "+uuid.NewString(), "", "", 0, direction(map[int]float32{0: 1, 3: float32(i) / 100})), 5)
	}
	for i := range 20 {
		r.book(t, "Elsewhere "+uuid.NewString(), "", "", 0, direction(map[int]float32{2: 1, 3: float32(i) / 100}))
	}
	report, err := a.server.Similar.CheckRecommendations(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if report.People != 1 || report.Hidden == 0 || len(report.Results) != 8 {
		t.Fatalf("report: %+v", report)
	}
	for _, res := range report.Results {
		if res.Recall20 < 0.99 {
			t.Errorf("%s: only %.2f of the hidden books came back", res.Name, res.Recall20)
		}
	}
}
