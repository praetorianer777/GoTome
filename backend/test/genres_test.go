//go:build integration

package test

import (
	"context"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
	"github.com/praetorianer777/gotome/backend/migrations"
)

// genreVector is the vector the fake model gives a genre's descriptions.
func (a *app) genreVector(key string) []float32 {
	a.t.Helper()
	vecs, err := genres.GenreVectors(context.Background(), embed.E5Small, a.server.Similar)
	if err != nil {
		a.t.Fatal(err)
	}
	return vecs[slices.Index(genres.Keys(), key)]
}

// useEnsemble has suggestions made by an ensemble trained here on noisy
// copies of the fake model's vectors of the genres' descriptions: content
// vectors alone, as a library without tags or counted words meets the
// shipped one.
func (a *app) useEnsemble(keys ...string) *genres.Ensemble {
	a.t.Helper()
	rng := rand.New(rand.NewPCG(528, 1))
	var books []genres.TrainBook
	for i := range 300 {
		key := keys[i%len(keys)]
		v := slices.Clone(a.genreVector(key))
		for j := range v {
			v[j] += float32(rng.NormFloat64() * 0.02)
		}
		books = append(books, genres.TrainBook{ID: strconv.Itoa(i), Content: v, Labels: []string{key}})
	}
	e := genres.TrainEnsemble(books, keys, genres.EnsembleOptions{Version: "test", Model: embed.E5Small.Model.Name, Workers: 2})
	a.server.Genres.Ensemble = func() (*genres.Ensemble, error) { return e, nil }
	return e
}

func (a *app) setGenreMode(admin *http.Client, mode string) {
	a.t.Helper()
	if status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{
		"values": map[string]any{settings.GenreSuggestions: mode},
	}); status != 200 {
		a.t.Fatalf("setting %s: %d %v", mode, status, body)
	}
}

func (a *app) suggestGenres() {
	a.t.Helper()
	complete, err := a.server.Genres.Suggest(context.Background(), time.Now().Add(time.Minute))
	if err != nil || !complete {
		a.t.Fatalf("suggest: %v, complete %v", err, complete)
	}
}

// genresOf are a book's rows of book_genres as source:state:genre.
func (a *app) genresOf(book uuid.UUID) []string {
	a.t.Helper()
	rows, err := a.pool.Query(context.Background(),
		`SELECT source || ':' || state || ':' || genre FROM book_genres WHERE book_id = $1 ORDER BY genre`, book)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			a.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestGenresAreWrittenOnlyOnceTheSettingAsksAndNeverTouchTags(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	newBook := func(title string, tags []string) uuid.UUID {
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: title, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	tagged := newBook("Death at La Stella", []string{"Krimi", "Venedig"})
	about := newBook("Alba Alta", nil)
	a.putVector(about, similar.KindContent, a.genreVector("crime"))
	var lockedBefore []string
	if err := a.pool.QueryRow(ctx, `UPDATE books SET locked_fields = '{tags}' WHERE id = $1 RETURNING locked_fields`, tagged).Scan(&lockedBefore); err != nil {
		t.Fatal(err)
	}

	a.useEnsemble("crime", "fantasy", "horror")
	a.setGenreMode(admin, genres.Off)
	a.suggestGenres()
	if got := append(a.genresOf(tagged), a.genresOf(about)...); len(got) != 0 {
		t.Fatalf("with the setting off, genres were written: %v", got)
	}

	a.setGenreMode(admin, genres.MethodTags)
	a.suggestGenres()
	if got := a.genresOf(tagged); !slices.Equal(got, []string{"tags:suggested:crime"}) {
		t.Errorf("genres of the tagged book %v", got)
	}
	if got := a.genresOf(about); len(got) != 0 {
		t.Errorf("the tags alone suggested %v for an untagged book", got)
	}

	a.setGenreMode(admin, genres.MethodEnsemble)
	a.suggestGenres()
	status, out, _ := a.call(editor, http.MethodGet, "/genres/suggestions?genre=crime", nil)
	list, _ := out["suggestions"].([]any)
	if status != 200 || len(list) != 1 {
		t.Fatalf("suggestions: %d %v", status, out)
	}
	first := list[0].(map[string]any)
	if first["book"].(map[string]any)["title"] != "Alba Alta" || first["method"] != genres.MethodEnsemble {
		t.Errorf("suggestion %v", first)
	}
	_, all, _ := a.call(editor, http.MethodGet, "/genres/suggestions", nil)
	if best := all["suggestions"].([]any)[0].(map[string]any); best["genre"] != "crime" {
		t.Errorf("the surest suggestion is %v, not crime", best)
	}

	// A suggestion is not a genre the filter finds until a person accepts it.
	filtered := func() []string {
		_, page, _ := a.call(editor, http.MethodGet, "/books?filter="+url.QueryEscape(`{"field":"genre","op":"in","values":["crime"]}`), nil)
		var titles []string
		for _, b := range page["books"].([]any) {
			titles = append(titles, b.(map[string]any)["title"].(string))
		}
		slices.Sort(titles)
		return titles
	}
	if got := filtered(); !slices.Equal(got, []string{"Death at La Stella"}) {
		t.Errorf("the filter finds %v before anything was accepted", got)
	}
	if status, body, _ := a.call(editor, http.MethodPut, "/books/"+about.String()+"/genres/crime", map[string]any{"state": "accepted"}); status != 204 {
		t.Fatalf("accept: %d %v", status, body)
	}
	if got := filtered(); !slices.Equal(got, []string{"Alba Alta", "Death at La Stella"}) {
		t.Errorf("the filter finds %v once accepted", got)
	}

	// What a person rejected stays rejected through every later pass.
	if status, body, _ := a.call(editor, http.MethodPut, "/books/"+tagged.String()+"/genres/crime", map[string]any{"state": "rejected"}); status != 204 {
		t.Fatalf("reject: %d %v", status, body)
	}
	a.setGenreMode(admin, genres.MethodTags)
	a.suggestGenres()
	a.setGenreMode(admin, genres.MethodEnsemble)
	a.suggestGenres()
	if got := a.genresOf(tagged); !slices.Contains(got, "tags:rejected:crime") {
		t.Errorf("a rejected genre came back: %v", got)
	}
	if got := a.genresOf(about); !slices.Contains(got, "automatic:accepted:crime") {
		t.Errorf("an accepted genre changed: %v", got)
	}
	if status, _, _ := a.call(editor, http.MethodPut, "/books/"+about.String()+"/genres/horror", map[string]any{"state": "accepted"}); status != 404 {
		t.Errorf("accepting a genre the book was never given: %d", status)
	}

	var tags, locked []string
	if err := a.pool.QueryRow(ctx, `
		SELECT ARRAY(SELECT t.name FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE bt.book_id = b.id ORDER BY t.name), b.locked_fields
		FROM books b WHERE b.id = $1`, tagged).Scan(&tags, &locked); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tags, []string{"Krimi", "Venedig"}) || !slices.Equal(locked, lockedBefore) {
		t.Errorf("the book's tags became %v, its locks %v", tags, locked)
	}
}

func TestGenresComeFromTheNearestBooksThatHaveThem(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	other := uuid.MustParse(a.library(admin, "Other", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	put := func(library uuid.UUID, title string, tags []string, v []float32) uuid.UUID {
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: library, Title: title, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		a.putVector(id, similar.KindContent, v)
		return id
	}
	for i, title := range []string{"Krimi eins", "Krimi zwei", "Krimi drei"} {
		put(lib, title, []string{"Kriminalroman"}, direction(map[int]float32{0: 1, 1: float32(i) / 20}))
	}
	// Nearer still, but in another library: not a neighbour.
	for _, title := range []string{"Drache eins", "Drache zwei", "Drache drei"} {
		put(other, title, []string{"Fantasy"}, direction(map[int]float32{0: 1}))
	}
	put(lib, "Fern", []string{"Fantasy"}, direction(map[int]float32{2: 1}))
	asked := put(lib, "Ohne Schlagwort", nil, direction(map[int]float32{0: 1, 1: 0.01}))

	alone := uuid.MustParse(a.library(admin, "Alone", "shared"))
	lonely := put(alone, "Allein", nil, a.genreVector("horror"))

	a.setGenreMode(admin, genres.MethodNeighbours)
	a.suggestGenres()
	if got := a.genresOf(asked); !slices.Equal(got, []string{"automatic:suggested:crime"}) {
		t.Errorf("genres from the neighbours %v", got)
	}
	if got := a.genresOf(lonely); len(got) != 0 {
		t.Errorf("a book without neighbours was given %v", got)
	}

	// The ensemble takes the neighbours as one of its inputs, and goes by
	// the vectors alone for a book none of whose neighbours have genres.
	a.useEnsemble("crime", "fantasy", "horror")
	a.setGenreMode(admin, genres.MethodEnsemble)
	a.suggestGenres()
	if got := a.genresOf(asked); !slices.Contains(got, "automatic:suggested:crime") {
		t.Errorf("the ensemble gave %v where the neighbours tell", got)
	}
	if got := a.genresOf(lonely); !slices.Equal(got, []string{"automatic:suggested:horror"}) {
		t.Errorf("the ensemble gave %v where only the vectors tell", got)
	}
}

func TestTheEnsembleIsTheDefaultAndSaysWhy(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: "Ohne Schlagwort"})
	if err != nil {
		t.Fatal(err)
	}
	a.putVector(id, similar.KindContent, a.genreVector("fantasy"))
	a.useEnsemble("crime", "fantasy", "horror")

	// Nothing is set: a library without a single tag gets suggestions.
	a.suggestGenres()
	if got := a.genresOf(id); !slices.Equal(got, []string{"automatic:suggested:fantasy"}) {
		t.Fatalf("by default the book was given %v", got)
	}
	_, out, _ := a.call(editor, http.MethodGet, "/genres/suggestions", nil)
	s := out["suggestions"].([]any)[0].(map[string]any)
	reasons, _ := s["reasons"].(map[string]any)
	inputs, _ := reasons["inputs"].([]any)
	if s["method"] != genres.MethodEnsemble || len(inputs) != 1 || inputs[0].(map[string]any)["input"] != "content" {
		t.Errorf("suggestion %v", s)
	}
	if score := s["score"].(float64); score <= 0 || score > 1 {
		t.Errorf("the ensemble's score %v is no probability", score)
	}

	// The old values are refused; an installation that had them moves.
	if status, _, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{
		"values": map[string]any{settings.GenreSuggestions: genres.MethodCombined},
	}); status != 422 {
		t.Errorf("combined was taken: %d", status)
	}
}

func TestAGenreCheckFindsTheGenresOfTaggedBooksAgain(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	for i, b := range []struct {
		title string
		tags  []string
		genre string
	}{
		{"Krimi eins", []string{"Krimi"}, "crime"},
		{"Krimi zwei", []string{"Belletristik/Kriminalromane"}, "crime"},
		{"Drache", []string{"Fantasy", "Venedig"}, "fantasy"},
		{"Kochbuch", []string{"Kochen"}, "cooking"},
		{"Ohne Genre", []string{"Venedig"}, ""},
		{"Ohne Schlagwort", nil, ""},
	} {
		id, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: b.title, Tags: b.tags})
		if err != nil {
			t.Fatal(err)
		}
		if b.genre != "" {
			a.putVector(id, similar.KindContent, a.genreVector(b.genre))
		} else {
			a.putVector(id, similar.KindContent, direction(map[int]float32{i: 1}))
		}
	}
	r, err := a.server.Genres.Check(ctx, genres.CheckOptions{Books: 10, Precision: 0.8, Neighbours: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Books != 6 || r.Tagged != 5 || r.FromTags != 4 || r.Embedded != 6 || r.Sample != 4 {
		t.Errorf("books %d, tagged %d, with a genre from tags %d, embedded %d, sample %d", r.Books, r.Tagged, r.FromTags, r.Embedded, r.Sample)
	}
	if r.PerGenre["crime"] != 2 || len(r.Unmapped) != 1 || r.Unmapped[0] != (genres.TagCount{Tag: "Venedig", Books: 2}) {
		t.Errorf("per genre %v, unmapped %v", r.PerGenre, r.Unmapped)
	}
	var methods []string
	for _, m := range r.Methods {
		methods = append(methods, m.Method)
		if m.Method == genres.CheckContent && m.Current.Recall != 1 {
			t.Errorf("the content vectors are each their genre's, yet recall is %v", m.Current.Recall)
		}
	}
	if !slices.Equal(methods, []string{genres.CheckContent, genres.CheckMetadata, genres.MethodVector, genres.MethodNeighbours, genres.MethodCombined}) {
		t.Errorf("methods %v", methods)
	}
	var written int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM book_genres`).Scan(&written); err != nil || written != 0 {
		t.Errorf("a check wrote %d genres (%v)", written, err)
	}
}

func TestAMergeKeepsWhatAPersonDecidedAboutGenres(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	ctx := context.Background()
	into, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: "Alba Alta"})
	if err != nil {
		t.Fatal(err)
	}
	from, err := books.CreateBook(ctx, catalog.NewBook{LibraryID: lib, Title: "Alba Alta (German Edition)"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(ctx, `
		INSERT INTO book_genres (book_id, genre, source, state) VALUES
		    ($1, 'crime', 'automatic', 'accepted'), ($1, 'travel', 'automatic', 'suggested'), ($1, 'horror', 'automatic', 'rejected'),
		    ($2, 'crime', 'automatic', 'suggested')`, from, into); err != nil {
		t.Fatal(err)
	}
	if status, out, _ := a.call(admin, http.MethodPost, "/books/"+into.String()+"/merge", map[string]any{"from": from}); status != 200 {
		t.Fatalf("merge: %d %v", status, out)
	}
	if got := a.genresOf(into); !slices.Equal(got, []string{"automatic:accepted:crime", "automatic:rejected:horror"}) {
		t.Errorf("genres after the merge %v", got)
	}
}

func TestAnInstallationSetToARetiredWaySuggestsWithTheEnsemble(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewEmpty(t)
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 102); err != nil {
		t.Fatalf("migrate to before the ensemble: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, 'combined')`, settings.GenreSuggestions); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, settings.GenreSuggestions).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != genres.MethodEnsemble {
		t.Errorf("combined became %q", mode)
	}
}

// westernText is English prose of a western, enough for the shipped word
// table to tell.
var westernText = strings.Repeat("The sheriff rode across the prairie to the ranch, where the cowboys drove the cattle. "+
	"He drew his revolver; the outlaws fled from the saloon to their horses and the canyon. "+
	"The rustlers had stolen the herd, and the posse followed the trail over the mesa. ", 20)

func TestTheShippedEnsembleReadsABooksWordsOnceTheyAreCounted(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	folder := t.TempDir()
	writeEPUB(t, filepath.Join(folder, "Trail.epub"), `
    <dc:identifier id="uid">urn:uuid:5280</dc:identifier>
    <dc:title>The Trail</dc:title>
    <dc:language>en</dc:language>`, nil, westernText)
	a.scanNow(a.externalLibrary(admin, "Shelf", "shared", folder))
	file := a.shelfFiles()["Trail.epub"]
	a.extract(file.ID)
	ctx := context.Background()

	// Its words are not counted yet: it has no input, so nothing is suggested.
	if _, err := a.pool.Exec(ctx, "DELETE FROM file_words WHERE file_id = $1", file.ID); err != nil {
		t.Fatal(err)
	}
	a.suggestGenres()
	if got := a.genresOf(file.BookID); len(got) != 0 {
		t.Fatalf("a book with no input was given %v", got)
	}

	// Counted later, it is looked at again and its words tell.
	if err := a.scans.Chunk(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	a.suggestGenres()
	if got := a.genresOf(file.BookID); !slices.Contains(got, "automatic:suggested:western") {
		t.Fatalf("from its words the book was given %v", got)
	}
	_, out, _ := a.call(editor, http.MethodGet, "/genres/suggestions?genre=western", nil)
	s := out["suggestions"].([]any)[0].(map[string]any)
	reasons := s["reasons"].(map[string]any)
	words, _ := reasons["words"].([]any)
	inputs := reasons["inputs"].([]any)
	if len(words) == 0 || len(inputs) != 1 || inputs[0].(map[string]any)["input"] != "words" {
		t.Errorf("reasons %v", reasons)
	}
	for _, w := range words {
		if !strings.Contains(strings.ToLower(westernText), w.(string)) {
			t.Errorf("a reason is %q, which the book does not hold", w)
		}
	}
}

func TestTheGenreReviewPagesWithoutRepeatingOrSkipping(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	books := catalog.NewService(a.pool)
	want := map[string]bool{}
	for i, key := range []string{"fantasy", "fantasy", "fantasy", "crime", "horror"} {
		id, err := books.CreateBook(context.Background(), catalog.NewBook{LibraryID: lib, Title: "Band " + strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		a.putVector(id, similar.KindContent, a.genreVector(key))
		want[id.String()] = true
	}
	a.useEnsemble("crime", "fantasy", "horror")
	a.suggestGenres()

	var seen []string
	cursor := ""
	for page := 0; ; page++ {
		if page > len(want)+1 {
			t.Fatalf("the pages do not end: %v", seen)
		}
		path := "/genres/suggestions?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		status, out, _ := a.call(editor, http.MethodGet, path, nil)
		if status != 200 {
			t.Fatalf("page %d: %d %v", page, status, out)
		}
		list := out["suggestions"].([]any)
		if len(list) > 1 {
			t.Fatalf("page %d holds %d suggestions", page, len(list))
		}
		for _, s := range list {
			b := s.(map[string]any)["book"].(map[string]any)["id"].(string)
			seen = append(seen, b+":"+s.(map[string]any)["genre"].(string))
		}
		cursor, _ = out["nextCursor"].(string)
		if cursor == "" {
			break
		}
		if page == 0 && len(list) != 1 {
			t.Fatalf("the first page is empty but has a cursor")
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("paging gave %d suggestions, want %d: %v", len(seen), len(want), seen)
	}
	ids := map[string]bool{}
	for _, s := range seen {
		ids[strings.SplitN(s, ":", 2)[0]] = true
	}
	if !maps.Equal(ids, want) {
		t.Errorf("books paged %v, want %v", ids, want)
	}
	_, whole, _ := a.call(editor, http.MethodGet, "/genres/suggestions?limit=50", nil)
	var all []string
	for _, s := range whole["suggestions"].([]any) {
		all = append(all, s.(map[string]any)["book"].(map[string]any)["id"].(string)+":"+s.(map[string]any)["genre"].(string))
	}
	if !slices.Equal(all, seen) {
		t.Errorf("paged %v, whole %v", seen, all)
	}

	for _, bad := range []string{"nonsense", "0.5_nonsense_fantasy", "high_" + uuid.NewString() + "_fantasy", "0.5_" + uuid.NewString()} {
		if status, out, _ := a.call(editor, http.MethodGet, "/genres/suggestions?cursor="+url.QueryEscape(bad), nil); status != 422 {
			t.Errorf("cursor %q: %d %v", bad, status, out)
		}
	}
}
