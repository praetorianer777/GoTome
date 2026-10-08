package releases

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

func TestStageOfANewlyListedBook(t *testing.T) {
	today := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		published, want string
	}{
		{"2026-10-08", stageAnnounced},
		{"2026-10-07", stageOut},
		{"2026-08-01", stageOut},
		{"2026-06-01", ""},
		{"2026-11", stageAnnounced},
		// This month or this year may still be to come.
		{"2026-10", stageAnnounced},
		{"2026", stageAnnounced},
		{"2026-09", stageOut},
		{"2025", ""},
		{"", ""},
	}
	for _, c := range cases {
		date, precision := publishedDate(c.published)
		if got := stageOf(date, precision, today); got != c.want {
			t.Errorf("%q: %q, want %q", c.published, got, c.want)
		}
	}
}

func TestPublishedDates(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-03": "2026-11-03 day", "2026-11": "2026-11-01 month", "2026": "2026-01-01 year",
	} {
		date, precision := publishedDate(in)
		if date == nil || date.Format(time.DateOnly)+" "+*precision != want {
			t.Errorf("%q: %v %v, want %s", in, date, precision, want)
		}
	}
	for _, in := range []string{"", "November 2026", "26-11-03"} {
		if date, precision := publishedDate(in); date != nil || precision != nil {
			t.Errorf("%q is a date: %v %v", in, date, precision)
		}
	}
}

func TestATitleIsComparedWithoutWhatProvidersAdd(t *testing.T) {
	for _, title := range []string{"Eislotus", "Eislotus: Roman", "Eislotus (Die Götter, #1)", "EISLOTUS [Kindle]"} {
		if got := titleKey(title); got != "eislotus" {
			t.Errorf("%q: %q", title, got)
		}
	}
	if titleKey("(1984)") != "1984" {
		t.Errorf("a title that is all brackets is kept: %q", titleKey("(1984)"))
	}
}

func TestAReleaseKeepsAuthorsInBothOrders(t *testing.T) {
	subject := uuid.New()
	index := 2.0
	p, ok := releaseOf(subject, metadata.Record{
		Provider: "hardcover", ID: "42", Title: " Feuerlotus ", Published: "2026-11",
		Series: "Die Götter", SeriesIndex: &index,
		Contributors: []catalog.NewContributor{
			{Name: "Liza Grimm", Role: catalog.RoleAuthor},
			{Name: "Some Reader", Role: catalog.RoleNarrator},
		},
		Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9783426227602"}, {Type: catalog.IDHardcover, Value: "42"}},
	})
	if !ok || p.Title != "Feuerlotus" || p.DedupeKey != "feuerlotus" || *p.Precision != "month" {
		t.Fatalf("release %+v", p)
	}
	if len(p.Authors) != 1 || p.Authors[0] != "Liza Grimm" {
		t.Errorf("authors %v: a narrator is no author", p.Authors)
	}
	if len(p.AuthorKeys) != 1 || p.AuthorKeys[0] != "liza grimm" {
		t.Errorf("author keys %v", p.AuthorKeys)
	}
	if len(p.Isbns) != 1 || *p.Series != "Die Götter" || *p.SeriesIndex != 2 {
		t.Errorf("release %+v", p)
	}
	if _, ok := releaseOf(subject, metadata.Record{Title: " : "}); ok {
		t.Error("a record without a title is a release")
	}
}

func TestAnAuthorIsOneSubjectInEitherOrder(t *testing.T) {
	if subjectKey(KindAuthor, "Grimm, Liza") != subjectKey(KindAuthor, "Liza Grimm") {
		t.Error("the two orders of a name are two authors")
	}
	if subjectKey(KindSeries, "Ende, Anfang") == subjectKey(KindSeries, "Anfang Ende") {
		t.Error("a series' name is turned round like a person's")
	}
}

func TestAFirstOfJanuaryFarAheadIsAYear(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for published, placeholder := range map[string]bool{
		"2030-01-01": true, "2027-01-01": false, "2030-01-02": false, "2030": false,
	} {
		date, precision := publishedDate(published)
		if got := placeholderDay(date, precision, now); got != placeholder {
			t.Errorf("%s: %v, want %v", published, got, placeholder)
		}
	}
}
