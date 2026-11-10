package autocurate

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

var isbn = catalog.Identifier{Type: "isbn", Value: "9780000000002"}

func book() catalog.Book {
	return catalog.Book{
		ID: uuid.New(), Title: "Das stille Archiv",
		Contributors: []catalog.Contributor{{Name: "Lena Brandt", Role: catalog.RoleAuthor}},
		Identifiers:  []catalog.BookIdentifier{{Identifier: isbn}},
		Sources:      map[string]string{},
	}
}

func match(provider string, r metadata.Record) enrich.Match {
	r.Provider = provider
	return enrich.Match{ID: uuid.New(), Score: 0.8, State: enrich.StatePending, Record: r}
}

func record() metadata.Record {
	return metadata.Record{
		ID: "r1", Title: "Das stille Archiv", Identifiers: []catalog.Identifier{isbn},
		Contributors: []catalog.NewContributor{{Name: "Lena Brandt", Role: catalog.RoleAuthor}},
	}
}

func TestDismissesARecordInAnotherLanguageThanTheText(t *testing.T) {
	r := record()
	r.Language = "fr-CA"
	out := Decide(Input{Book: book(), TextLanguage: "de", Match: match("openlibrary", r)})
	if !out.Dismiss || out.Rule != RuleDismissLanguage {
		t.Fatalf("outcome = %+v", out)
	}
	out = Decide(Input{Book: book(), TextLanguage: "fr", Match: match("openlibrary", r)})
	if out.Dismiss {
		t.Fatalf("same language was dismissed: %+v", out)
	}
	out = Decide(Input{Book: book(), TextLanguage: "", Match: match("openlibrary", r)})
	if out.Dismiss {
		t.Fatalf("an unknown text language dismissed: %+v", out)
	}
}

func TestDismissesARecordYearsFromTheFilesDate(t *testing.T) {
	b := book()
	on := time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)
	b.PublishedOn, b.PublishedPrecision = &on, catalog.PrecisionYear
	b.Sources[catalog.FieldPublished] = "file:epub:" + uuid.NewString()
	for published, dismissed := range map[string]bool{"2010": true, "1998-03-01": true, "1997": false, "1993": false, "1990": true, "": false} {
		r := record()
		r.Published = published
		out := Decide(Input{Book: b, Match: match("crossref", r)})
		if out.Dismiss != dismissed {
			t.Errorf("record of %q: dismissed = %v, want %v", published, out.Dismiss, dismissed)
		}
		if dismissed && out.Rule != RuleDismissYear {
			t.Errorf("record of %q: rule %q", published, out.Rule)
		}
	}
	b.Sources[catalog.FieldPublished] = catalog.SourceManual
	r := record()
	r.Published = "2010"
	if Decide(Input{Book: b, Match: match("crossref", r)}).Dismiss {
		t.Error("a date typed by hand counted as the file's own")
	}
}

func TestATrustedRecordFillsGapsAndLeavesCleanValues(t *testing.T) {
	b := book()
	b.Sources[catalog.FieldTitle] = catalog.SourceFilename
	b.Title = "das stille archiv"
	r := record()
	r.Description = "A librarian finds a ledger."
	r.Publisher = "Nordlicht"
	r.Published = "2019"
	r.Language = "de"
	out := Decide(Input{Book: b, Match: match("openlibrary", r)})
	if out.Agreed == "" {
		t.Fatal("a shared ISBN was not trusted")
	}
	for _, f := range []string{catalog.FieldTitle, catalog.FieldDescription, catalog.FieldPublisher, catalog.FieldPublished, catalog.FieldLanguage} {
		if !slices.Contains(out.Taken, f) {
			t.Errorf("%s was not taken: %v", f, out.Taken)
		}
	}
	if len(out.Conflicts) != 0 || !out.Settles() {
		t.Errorf("outcome = %+v", out)
	}
	if out.Edit.Title == nil || *out.Edit.Title != "Das stille Archiv" || out.Edit.Source != "provider:openlibrary" {
		t.Errorf("edit = %+v", out.Edit)
	}
}

func TestACleanValueStaysAndARealConflictKeepsTheMatchPending(t *testing.T) {
	b := book()
	b.Description = "A ledger turns up in the stacks."
	b.Title = "Das Stille Archiv"
	r := record()
	r.Title = "Das stille Archiv"
	r.Description = "An entirely different blurb."
	r.Publisher = "Nordlicht"
	out := Decide(Input{Book: b, Match: match("openlibrary", r)})
	if out.Edit.Title != nil {
		t.Errorf("a clean title was replaced: %q", *out.Edit.Title)
	}
	if !slices.Equal(out.Conflicts, []string{catalog.FieldDescription}) {
		t.Errorf("conflicts = %v", out.Conflicts)
	}
	if !slices.Equal(out.Taken, []string{catalog.FieldPublisher}) {
		t.Errorf("taken = %v", out.Taken)
	}
	if out.Edit.Description != nil || out.Settles() {
		t.Errorf("a conflicting field was taken or the match settled: %+v", out)
	}
}

func TestAnUntidyTitleIsReplacedByTheRecordsTidyOne(t *testing.T) {
	b := book()
	b.Title = "Das stille Archiv (German Edition)"
	out := Decide(Input{Book: b, Match: match("openlibrary", record())})
	if out.Edit.Title == nil || *out.Edit.Title != "Das stille Archiv" {
		t.Fatalf("edit = %+v, taken %v", out.Edit, out.Taken)
	}
}

func TestARecordNobodyVouchesForIsLeftAlone(t *testing.T) {
	b := book()
	b.Identifiers = nil
	b.Sources[catalog.FieldTitle] = catalog.SourceFilename
	out := Decide(Input{Book: b, Match: match("openlibrary", record())})
	if out.Agreed != "" || out.Edit.ChangesValues() || out.Settles() {
		t.Fatalf("outcome = %+v", out)
	}
	other := match("crossref", metadata.Record{ID: "x", Identifiers: []catalog.Identifier{isbn}})
	out = Decide(Input{Book: b, Match: match("openlibrary", record()), Others: []enrich.Match{other}})
	if out.Agreed == "" {
		t.Error("two providers naming one ISBN were not trusted")
	}
	r := record()
	r.Identifiers, r.Work = nil, "w1"
	w1 := match("openlibrary", r)
	r2 := metadata.Record{ID: "y", Work: "w1"}
	out = Decide(Input{Book: b, Match: w1, Others: []enrich.Match{match("hardcover", r2)}})
	if out.Agreed == "" {
		t.Error("two providers naming one work were not trusted")
	}
	same := match("openlibrary", metadata.Record{ID: "z", Identifiers: []catalog.Identifier{isbn}})
	out = Decide(Input{Book: b, Match: w1, Others: []enrich.Match{same}})
	if out.Agreed != "" {
		t.Error("two records of one provider vouched for each other")
	}
}

func TestAGroupAcceptedByAPersonNeedsNoAgreement(t *testing.T) {
	b := book()
	b.Identifiers = nil
	b.Sources[catalog.FieldTitle] = catalog.SourceFilename
	b.Title = "archiv"
	out := Decide(Input{Book: b, Match: match("openlibrary", record()), Unagreed: true})
	if out.Edit.Title == nil {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestPreferredSpellingGoesByUse(t *testing.T) {
	got := PreferredSpelling([]cleanup.Name{{Name: "Brandt, Lena", Books: 2}, {Name: "Lena Brandt", Books: 5}})
	if got != "Lena Brandt" {
		t.Errorf("got %q", got)
	}
	got = PreferredSpelling([]cleanup.Name{{Name: "J.R.R. Fenwick", Books: 9}, {Name: "JRR Fenwick", Books: 1}})
	if got != "J.R.R. Fenwick" {
		t.Errorf("got %q", got)
	}
}
