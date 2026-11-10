package autodupe

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/reconcile"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

func TestSettingsNameTheClassesTheRulesHandle(t *testing.T) {
	if !slices.Equal(settings.DuplicateClassNames, dedupe.AutoClasses) {
		t.Errorf("settings name %v, dedupe handles %v", settings.DuplicateClassNames, dedupe.AutoClasses)
	}
}

func id(n byte) uuid.UUID { return uuid.UUID{15: n} }

func TestSurvivor(t *testing.T) {
	older, newer := Facts{ID: id(1)}, Facts{ID: id(2)}
	for _, c := range []struct {
		name      string
		a, b      Facts
		firstWins bool
		why       string
	}{
		{"reading state first", Facts{ID: id(2), HasState: true}, Facts{ID: id(1), HasManual: true, Formats: []string{"epub"}, Filled: 9}, true, "someone has a reading state with it"},
		{"reading state of the second", Facts{ID: id(1), HasManual: true}, Facts{ID: id(2), HasState: true}, false, "someone has a reading state with it"},
		{"a field set by hand", Facts{ID: id(2), HasManual: true}, Facts{ID: id(1), Formats: []string{"epub"}}, true, "it has a field set by hand"},
		{"the better format", Facts{ID: id(2), Formats: []string{"pdf"}}, Facts{ID: id(1), Formats: []string{"epub"}}, false, "its format is better"},
		{"any format beats none", Facts{ID: id(2), Formats: []string{"pdf"}}, Facts{ID: id(1)}, true, "its format is better"},
		{"more details", Facts{ID: id(2), Filled: 5}, Facts{ID: id(1), Filled: 3}, true, "it has more details"},
		{"the older", newer, older, false, "it is older"},
		{"the older, first", older, newer, true, "it is older"},
	} {
		t.Run(c.name, func(t *testing.T) {
			first, why := Survivor(c.a, c.b)
			if first != c.firstWins || why != c.why {
				t.Errorf("Survivor = %v %q, want %v %q", first, why, c.firstWins, c.why)
			}
		})
	}
}

func TestCopies(t *testing.T) {
	now := time.Now()
	book1, book2 := id(1), id(2)
	epub := func(n byte, book uuid.UUID, age time.Duration, findings int) File {
		return File{ID: id(n), Book: book, Kind: "ebook", Format: "epub", Modified: now.Add(-age), Findings: findings}
	}
	for _, c := range []struct {
		name  string
		files []File
		want  []uuid.UUID
	}{
		{"the newer of two clean EPUBs stays", []File{epub(10, book1, time.Hour, 0), epub(11, book2, 0, 0)}, []uuid.UUID{id(10)}},
		{"findings lose to age", []File{epub(10, book1, 0, 2), epub(11, book2, time.Hour, 0)}, []uuid.UUID{id(10)}},
		{"equal files: the larger ID stays", []File{epub(10, book1, 0, 0), epub(11, book2, 0, 0)}, []uuid.UUID{id(10)}},
		{"another format stays", []File{epub(10, book1, 0, 0), {ID: id(11), Book: book2, Kind: "ebook", Format: "pdf", Modified: now}}, nil},
		{"two EPUBs in one book are not twins", []File{epub(10, book1, 0, 0), epub(11, book1, 0, 0), epub(12, book2, 0, 0)}, nil},
		{"the parts of an audiobook stay", []File{
			{ID: id(10), Book: book1, Kind: "audio", Format: "mp3"}, {ID: id(11), Book: book2, Kind: "audio", Format: "mp3"},
		}, nil},
		{"one pair among others", []File{
			epub(10, book1, time.Hour, 0), epub(11, book2, 0, 0),
			{ID: id(12), Book: book1, Kind: "ebook", Format: "pdf", Modified: now},
		}, []uuid.UUID{id(10)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Copies(c.files); !slices.Equal(got, c.want) {
				t.Errorf("Copies = %v, want %v", got, c.want)
			}
		})
	}
}

func book(title, author, series string, mods func(*catalog.Book)) catalog.Book {
	b := catalog.Book{
		Title: title, Series: series,
		Contributors: []catalog.Contributor{{Name: author, Role: catalog.RoleAuthor}},
		Sources:      map[string]string{},
	}
	if mods != nil {
		mods(&b)
	}
	return b
}

func TestMergingTakesWhatTheOtherSideHasAndNotWhatTheSurvivorLocked(t *testing.T) {
	keep := book("Der Sternwanderer", "Lena Brandt", "", func(b *catalog.Book) {
		b.Sources[catalog.FieldTitle] = catalog.SourceManual
		b.Locked = []string{catalog.FieldTitle}
	})
	gone := book("Sternwanderer", "Lena Brandt", "Himmelsweg", func(b *catalog.Book) {
		b.Description = "Ein Wanderer folgt einem Stern."
		b.Tags = []string{"Märchen"}
	})
	res := reconcile.Reconcile(DescriptionOf(keep), DescriptionOf(gone), reconcile.Options{})
	f := Merging(res)
	if slices.Contains(f.Take, catalog.FieldTitle) {
		t.Errorf("the locked title is taken from the other: %+v", f)
	}
	for _, want := range []string{catalog.FieldDescription, catalog.FieldSeries} {
		if !slices.Contains(f.Take, want) {
			t.Errorf("%s is not taken: %+v", want, f)
		}
	}
	if len(f.Conflicts) != 0 {
		t.Errorf("conflicts: %+v", f.Conflicts)
	}
}

func TestMergingReportsConflictsAndUnitesTags(t *testing.T) {
	keep := book("Der Sternwanderer", "Lena Brandt", "", func(b *catalog.Book) { b.Tags = []string{"Märchen"} })
	gone := book("Die Mondkarte", "Lena Brandt", "", func(b *catalog.Book) { b.Tags = []string{"Reise"} })
	res := reconcile.Reconcile(DescriptionOf(keep), DescriptionOf(gone), reconcile.Options{})
	f := Merging(res)
	if !slices.Equal(f.Conflicts, []string{catalog.FieldTitle}) {
		t.Errorf("conflicts: %+v", f.Conflicts)
	}
	if !slices.Contains(f.Union, catalog.FieldTags) {
		t.Errorf("tags are not united: %+v", f)
	}
}

func TestAReasonNamesFieldsAndNoValues(t *testing.T) {
	got := Reason("same bytes or same text", "it is older", Fields{Take: []string{"description"}, Conflicts: []string{"title"}}, 1)
	want := "same bytes or same text; kept one book because it is older; took description from the other; kept its own title, which differ; trashed 1 file(s) that copy another of the same format"
	if got != want {
		t.Errorf("Reason =\n%s\nwant\n%s", got, want)
	}
}
