package reconcile

import (
	"slices"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

func f(v float64) *float64 { return &v }

func want(t *testing.T, res Result, field string, value []string, from Side, rule string) {
	t.Helper()
	d, ok := res.Of(field)
	if !ok {
		t.Fatalf("no decision for %s", field)
	}
	if d.Conflict {
		t.Fatalf("%s is a conflict (%v vs %v), want %v", field, d.A, d.B, value)
	}
	if !slices.Equal(d.Value, value) || d.From != from || d.Rule != rule {
		t.Errorf("%s = %v from %s by %s (%s), want %v from %s by %s", field, d.Value, d.From, d.Rule, d.Reason, value, from, rule)
	}
	if d.Reason == "" {
		t.Errorf("%s has no reason", field)
	}
}

func TestASeriesInTheTitleAndAReversedNameResolveFully(t *testing.T) {
	a := Description{
		Title: "Sternwanderer - 130 - Der Turm am Fluss", Authors: []string{"Brandt Lena"},
		Sources: map[string]string{catalog.FieldTitle: catalog.SourceFilename},
	}
	b := Description{Title: "Der Turm am Fluss", Authors: []string{"Lena Brandt"}, Series: "Sternwanderer", SeriesIndex: f(130)}
	res := Reconcile(a, b, Options{Usage: map[string]int{"Lena Brandt": 40}})
	if c := res.Conflicts(); len(c) != 0 {
		t.Fatalf("conflicts: %+v", c)
	}
	want(t, res, catalog.FieldTitle, []string{"Der Turm am Fluss"}, SideA, RuleEqual)
	want(t, res, catalog.FieldContributors, []string{"Lena Brandt"}, SideB, RuleEqual)
	want(t, res, catalog.FieldSeries, []string{"Sternwanderer"}, SideA, RuleEqual)
	want(t, res, FieldSeriesIndex, []string{"130"}, SideA, RuleEqual)
	if res.Merged.SeriesIndex == nil || *res.Merged.SeriesIndex != 130 {
		t.Errorf("merged index = %v", res.Merged.SeriesIndex)
	}
	d, _ := res.Of(catalog.FieldSeries)
	if !slices.ContainsFunc(d.Notes, func(n Note) bool { return n.Rule == NoteSeriesFromTitle }) {
		t.Errorf("series notes = %+v", d.Notes)
	}
}

func TestAnAnthologyTakesTheShortTitleAndAllNinePoets(t *testing.T) {
	poets := []string{"Ida Holm", "Paul Lenz", "Greta Voss", "Emil Sander", "Ruth Falk", "Otto Kern", "Hanna Wolf", "Karl Ries", "Lotte Baum"}
	a := Description{Title: "Verse für jeden Abend", Authors: []string{"Ida Holm"}}
	b := Description{
		Title:   "Verse für jeden Abend: Ida Holm, Paul Lenz, Greta Voss, Emil Sander, Ruth Falk, Otto Kern, Hanna Wolf, Karl Ries, Lotte Baum",
		Authors: poets,
	}
	res := Reconcile(a, b, Options{})
	if c := res.Conflicts(); len(c) != 0 {
		t.Fatalf("conflicts: %+v", c)
	}
	want(t, res, catalog.FieldTitle, []string{"Verse für jeden Abend"}, SideA, RuleEqual)
	want(t, res, catalog.FieldSubtitle, []string{"Ida Holm, Paul Lenz, Greta Voss, Emil Sander, Ruth Falk, Otto Kern, Hanna Wolf, Karl Ries, Lotte Baum"}, SideB, RuleEmpty)
	want(t, res, catalog.FieldContributors, poets, SideB, RuleSubset)
}

func TestAPlatformAsAuthorResolvesTheAuthorAndLeavesTheTitle(t *testing.T) {
	a := Description{Title: "Nachtfalter 1 (DEU)", Authors: []string{"Jonas Keller"}}
	b := Description{Title: "Nachtfalter Collector's Pack Deutsch - Webnovel Edition", Authors: []string{"Webnovel"}}
	res := Reconcile(a, b, Options{})
	want(t, res, catalog.FieldContributors, []string{"Jonas Keller"}, SideA, RuleEmpty)
	want(t, res, catalog.FieldPublisher, []string{"Webnovel"}, SideB, RuleEmpty)
	c := res.Conflicts()
	if len(c) != 1 || c[0].Field != catalog.FieldTitle {
		t.Fatalf("conflicts = %+v, want the title only", c)
	}
	if !slices.Equal(c[0].A, []string{"Nachtfalter 1"}) || len(c[0].Value) != 0 {
		t.Errorf("conflict = %+v", c[0])
	}
	d, _ := res.Of(catalog.FieldContributors)
	if !slices.ContainsFunc(d.Notes, func(n Note) bool { return n.Rule == NotePlatformAuthor }) {
		t.Errorf("author notes = %+v", d.Notes)
	}
}

func TestRules(t *testing.T) {
	locked := func(fields ...string) []string { return fields }
	dune := metadata.Record{Provider: "openlibrary", Title: "Dune Messiah (German Edition)"}
	tests := []struct {
		name  string
		a, b  Description
		o     Options
		field string
		value []string
		from  Side
		rule  string
	}{
		{"locked beats automatic",
			Description{Title: "The Tale", Locked: locked(catalog.FieldTitle)}, Description{Title: "A Tale"},
			Options{}, catalog.FieldTitle, []string{"The Tale"}, SideA, RuleLocked},
		{"hand-typed beats automatic",
			Description{Publisher: "Heyne"}, Description{Publisher: "Penguin", Sources: map[string]string{catalog.FieldPublisher: catalog.SourceManual}},
			Options{}, catalog.FieldPublisher, []string{"Penguin"}, SideB, RuleLocked},
		{"empty takes the other",
			Description{Publisher: ""}, Description{Publisher: "Heyne"},
			Options{}, catalog.FieldPublisher, []string{"Heyne"}, SideB, RuleEmpty},
		{"filename guess loses",
			Description{Title: "scan_0042_final", Sources: map[string]string{catalog.FieldTitle: catalog.SourceFilename}},
			Description{Title: "Der Hobbit"},
			Options{}, catalog.FieldTitle, []string{"Der Hobbit"}, SideB, RuleFilename},
		{"provider with a shared identifier decides",
			Description{Title: "Dune Messiah"}, Description{Title: "Children of Dune"},
			Options{Providers: []metadata.Record{dune}}, catalog.FieldTitle, []string{"Dune Messiah"}, SideProvider, RuleProvider},
		{"equal after normalising keeps the cleanest spelling",
			Description{Title: "Der Hobbit (German Edition)", Authors: []string{"Tolkien, J.R.R."}},
			Description{Title: "Der Hobbit", Authors: []string{"J.R.R. Tolkien"}},
			Options{}, catalog.FieldContributors, []string{"J.R.R. Tolkien"}, SideA, RuleEqual},
		{"the most used spelling wins",
			Description{Series: "Sternwanderer"}, Description{Series: "sternwanderer"},
			Options{Usage: map[string]int{"sternwanderer": 3}}, catalog.FieldSeries, []string{"sternwanderer"}, SideB, RuleEqual},
		{"a year is part of a date",
			Description{Published: "2010"}, Description{Published: "2010-08-31"},
			Options{}, catalog.FieldPublished, []string{"2010-08-31"}, SideB, RuleSubset},
		{"a placeholder author is no author",
			Description{Authors: []string{"Unknown"}}, Description{Authors: []string{"Ann Leckie"}},
			Options{}, catalog.FieldContributors, []string{"Ann Leckie"}, SideB, RuleEmpty},
		{"identifiers of both are kept",
			Description{Identifiers: []catalog.Identifier{{Type: "isbn", Value: "9783608960358"}}},
			Description{Identifiers: []catalog.Identifier{{Type: "isbn", Value: "9783608960358"}, {Type: "asin", Value: "B000"}}},
			Options{}, catalog.FieldIdentifiers, []string{"isbn:9783608960358", "asin:B000"}, SideB, RuleSubset},
		{"tags are united",
			Description{Tags: []string{"Fantasy"}}, Description{Tags: []string{"fantasy", "Epic"}},
			Options{}, catalog.FieldTags, []string{"fantasy", "Epic"}, SideB, RuleSubset},
		{"series position goes with its series",
			Description{Series: "Dune", SeriesIndex: f(2)}, Description{Series: "Dune Chronicles", SeriesIndex: f(1), Sources: map[string]string{catalog.FieldSeries: catalog.SourceManual}},
			Options{}, FieldSeriesIndex, []string{"1"}, SideB, RuleLocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want(t, Reconcile(tt.a, tt.b, tt.o), tt.field, tt.value, tt.from, tt.rule)
		})
	}
}

func TestConflicts(t *testing.T) {
	both := []string{catalog.FieldPublisher}
	tests := []struct {
		name  string
		a, b  Description
		field string
	}{
		{"two different values", Description{Publisher: "Heyne"}, Description{Publisher: "Piper"}, catalog.FieldPublisher},
		{"both locked",
			Description{Publisher: "Heyne", Locked: both}, Description{Publisher: "Piper", Locked: both}, catalog.FieldPublisher},
		{"different people", Description{Authors: []string{"Ann Leckie"}}, Description{Authors: []string{"Ursula Le Guin"}}, catalog.FieldContributors},
		{"different positions", Description{Series: "X", SeriesIndex: f(1)}, Description{Series: "X", SeriesIndex: f(2)}, FieldSeriesIndex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Reconcile(tt.a, tt.b, Options{})
			d, _ := res.Of(tt.field)
			if !d.Conflict || d.Rule != RuleConflict || len(d.Value) != 0 || len(d.A) == 0 || len(d.B) == 0 {
				t.Errorf("decision = %+v, want a conflict with both values", d)
			}
		})
	}
}

func TestPlatformsAreNoPlaceholders(t *testing.T) {
	for _, k := range PlatformAuthors() {
		if catalog.Key(k) != k {
			t.Errorf("%q is not a key", k)
		}
	}
}
