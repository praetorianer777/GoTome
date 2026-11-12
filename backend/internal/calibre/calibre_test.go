package calibre

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/calibre/calibretest"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := calibretest.Write(dir, calibretest.Books(), func(b calibretest.Book, format string) []byte {
		return []byte("x")
	}, []byte("jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func readAll(t *testing.T, dir string) ([]Book, []Column) {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	columns, err := r.Columns(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := r.IDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	books, err := r.Books(context.Background(), ids, columns)
	if err != nil {
		t.Fatal(err)
	}
	return books, columns
}

func TestTheReaderFindsWhatCalibreKeeps(t *testing.T) {
	books, columns := readAll(t, fixture(t))
	if len(books) != 4 || len(columns) != 3 {
		t.Fatalf("%d books and %d columns, want 4 and 3", len(books), len(columns))
	}
	lantern := books[0]
	if lantern.Title != "The Lantern at Dusk" || lantern.Path != "Henrik Halden/The Lantern at Dusk (1)" ||
		lantern.Series != "Halden Chronicles" || lantern.Publisher != "Brinewick Press" || lantern.Rating != 8 || !lantern.HasCover {
		t.Errorf("the first book: %+v", lantern)
	}
	if !slices.Equal(lantern.Tags, []string{"Fiction", "Sea"}) || !slices.Equal(lantern.Languages, []string{"eng"}) ||
		len(lantern.Authors) != 1 || lantern.Authors[0] != (Author{Name: "Henrik Halden", Sort: "Halden, Henrik"}) {
		t.Errorf("the first book's links: %+v", lantern)
	}
	if want := []Format{{"epub", "The Lantern at Dusk - Henrik Halden"}, {"pdf", "The Lantern at Dusk - Henrik Halden"}}; !slices.Equal(lantern.Formats, want) {
		t.Errorf("formats %v, want %v", lantern.Formats, want)
	}
	if !slices.Equal(lantern.Custom["shelf"], []string{"favourites", "sailing"}) || !slices.Equal(lantern.Custom["read"], []string{"1"}) ||
		!slices.Equal(lantern.Custom["finished"], []string{"2021-03-14T00:00:00Z"}) {
		t.Errorf("custom columns: %v", lantern.Custom)
	}
	if books[1].Custom["read"][0] != "0" || len(books[2].Custom) != 0 {
		t.Errorf("custom values of the others: %v %v", books[1].Custom, books[2].Custom)
	}
	counts := map[string]int{}
	for _, c := range columns {
		counts[c.Label] = c.Books
	}
	if counts["read"] != 2 || counts["shelf"] != 1 || counts["finished"] != 1 {
		t.Errorf("books per column: %v", counts)
	}
}

func TestAFolderWithoutMetadataDbIsNoCalibreLibrary(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); !errors.Is(err, ErrNotCalibre) {
		t.Errorf("an empty folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, Database), []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrNotCalibre) {
		t.Errorf("a file that is no database: %v", err)
	}
}

func TestReadingLeavesMetadataDbAsItWas(t *testing.T) {
	dir := fixture(t)
	path := filepath.Join(dir, Database)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(dir)

	readAll(t, dir)

	after, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(old) {
		t.Errorf("metadata.db changed: same bytes %v, modified %v", bytes.Equal(before, after), info.ModTime())
	}
	if now, _ := os.ReadDir(dir); len(now) != len(entries) {
		t.Errorf("the folder gained files: %v", now)
	}
}

func TestReadingASQLiteFileThatIsReadOnlyWorks(t *testing.T) {
	dir := fixture(t)
	path := filepath.Join(dir, Database)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if books, _ := readAll(t, dir); len(books) != 4 {
		t.Errorf("%d books", len(books))
	}
}

func TestMappingTurnsACalibreBookIntoGOtomes(t *testing.T) {
	books, columns := readAll(t, fixture(t))
	opts := Options{Ratings: true, Columns: map[string]string{"read": ChoiceStatus, "shelf": ChoiceTags, "finished": ChoiceIgnore}}
	m := Map(books[0], columns, opts)
	if m.Title != "The Lantern at Dusk" || m.Series != "Halden Chronicles" || m.SeriesIndex == nil || *m.SeriesIndex != 1 ||
		m.Publisher != "Brinewick Press" || m.Language != "en" || m.Published != "1965-03-04" {
		t.Errorf("fields: %+v", m)
	}
	if m.Description != "A keeper trims the light.\n\nNobody comes." {
		t.Errorf("description %q", m.Description)
	}
	if !slices.Equal(m.Tags, []string{"Fiction", "Sea", "favourites", "sailing"}) {
		t.Errorf("tags %v", m.Tags)
	}
	if want := []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780195001013"}, {Type: catalog.IDOther, Value: "goodreads:1001"}}; !slices.Equal(m.Identifiers, want) {
		t.Errorf("identifiers %v, want %v", m.Identifiers, want)
	}
	if m.Cover != "Henrik Halden/The Lantern at Dusk (1)/cover.jpg" || m.Reading.Rating != 4 || m.Reading.Status != catalog.StatusCompleted {
		t.Errorf("cover %q, reading %+v", m.Cover, m.Reading)
	}
	if len(m.Contributors) != 1 || m.Contributors[0].SortName != "Halden, Henrik" {
		t.Errorf("contributors %+v", m.Contributors)
	}

	if m := Map(books[0], columns, Options{}); m.Reading.Rating != 0 || m.Reading.Status != "" || len(m.Tags) != 2 {
		t.Errorf("without choices: %+v", m)
	}
	dated := Map(books[0], columns, Options{Columns: map[string]string{"finished": ChoiceStatus}})
	if dated.Reading.Status != catalog.StatusCompleted || dated.Reading.FinishedOn == nil || dated.Reading.FinishedOn.Format(time.DateOnly) != "2021-03-14" {
		t.Errorf("a date column as status: %+v", dated.Reading)
	}
	if unread := Map(books[1], columns, Options{Columns: map[string]string{"read": ChoiceStatus}}); unread.Reading.Status != "" {
		t.Errorf("a no is not a status: %+v", unread.Reading)
	}
	if ticked := Map(books[0], columns, Options{Columns: map[string]string{"read": ChoiceTags}}); !slices.Contains(ticked.Tags, "Read") {
		t.Errorf("a yes as a tag is the column's name: %v", ticked.Tags)
	}

	german := Map(books[2], columns, opts)
	if german.Language != "de" || german.SeriesIndex == nil || *german.SeriesIndex != 2.5 || german.Reading.Rating != 5 {
		t.Errorf("the German book: %+v", german)
	}
	undated := Map(books[3], columns, opts)
	if undated.Published != "" || undated.Series != "" || undated.SeriesIndex != nil || undated.Cover != "" {
		t.Errorf("the undated book: %+v", undated)
	}
}

func TestRatingsBecomeWholeStars(t *testing.T) {
	for rating, want := range map[int]int16{0: 0, 1: 1, 2: 1, 3: 2, 5: 3, 8: 4, 9: 5, 10: 5} {
		if got := Stars(rating); got != want {
			t.Errorf("Stars(%d) = %d, want %d", rating, got, want)
		}
	}
}

func TestLanguageCodes(t *testing.T) {
	for code, want := range map[string]string{"eng": "en", "deu": "de", "ger": "de", "fre": "fr", "ENG": "en", "zzz": "", "": ""} {
		if got := LanguageTag(code); got != want {
			t.Errorf("LanguageTag(%q) = %q, want %q", code, got, want)
		}
	}
}

// TestWriteTheBrowserFixture writes e2e/fixtures/calibre, the library the
// browser tests preview, when GOTOME_WRITE_CALIBRE_FIXTURE names its folder.
func TestWriteTheBrowserFixture(t *testing.T) {
	dir := os.Getenv("GOTOME_WRITE_CALIBRE_FIXTURE")
	if dir == "" {
		t.Skip("set GOTOME_WRITE_CALIBRE_FIXTURE to write the fixture")
	}
	img := image.NewGray(image.Rect(0, 0, 60, 90))
	var cover bytes.Buffer
	if err := jpeg.Encode(&cover, img, nil); err != nil {
		t.Fatal(err)
	}
	content := func(b calibretest.Book, format string) []byte {
		switch {
		case b.ID == 3:
			return nil
		case format == "EPUB":
			return []byte("PK\x03\x04 not a whole EPUB, only the first bytes of one\n")
		}
		return []byte("%PDF-1.4\n")
	}
	if err := calibretest.Write(dir, calibretest.Books(), content, cover.Bytes()); err != nil {
		t.Fatal(err)
	}
}
