package similar

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestPickLeavesOutCopiesAndCapsAuthorsAndSeries(t *testing.T) {
	series, other := uuid.New(), uuid.New()
	book := traits{id: uuid.New(), title: "Rauklands Schwert", series: &series, people: []string{"jordis lank"}}
	near := []traits{
		{title: "Rauklands Sohn: Raukland Trilogie (German Edition)", series: &series, people: []string{"jordis lank"}},
		{title: "Rauklands Blut", series: &series, people: []string{"jordis lank"}},
		{title: "Rauklands Sohn", series: &series, people: []string{"jordis lank"}},
		{title: "Rauklands Schwert (German Edition)", people: []string{"jordis lank"}},
		{title: "Im Auftrag der Rache", series: &other, people: []string{"buchanan col"}},
		{title: "Die Goldspinnerin", people: []string{"bertram gerit"}},
		{title: "Raukland: Der Anfang", people: []string{"jordis lank"}},
		{title: "Die Hexe von Paris", people: []string{"bertram gerit"}},
		{title: "Der Bastard", people: []string{"bertram gerit"}},
		{title: "Ohne Autor"},
	}
	names := map[uuid.UUID]string{}
	for i := range near {
		near[i].id = uuid.New()
		names[near[i].id] = near[i].title
	}
	var got []string
	for _, id := range pick(book, near, 6) {
		got = append(got, names[id])
	}
	// The Sohn's copy comes after it; the Schwert's is the book's own. A
	// series takes two places, an author two.
	want := []string{
		"Rauklands Sohn: Raukland Trilogie (German Edition)", "Rauklands Blut",
		"Im Auftrag der Rache", "Die Goldspinnerin", "Die Hexe von Paris", "Ohne Autor",
	}
	if !slices.Equal(got, want) {
		t.Errorf("picked %q,\nwant %q", got, want)
	}
}

func TestTwoVolumesWithTheirOwnSubtitlesAreNoCopies(t *testing.T) {
	people := []string{"c s lewis"}
	for _, c := range []struct {
		a, b string
		copy bool
	}{
		{"Die Chroniken von Narnia: Prinz Kaspian", "Die Chroniken von Narnia: Der silberne Sessel", false},
		{"Wolfs Brut", "Wolfs Brut: Kommissar Kilians zweiter Fall", true},
		{"Das letzte Evangelium: Historischer Roman (German Edition)", "Das letzte Evangelium", true},
		{"Emma", "Emma", true},
	} {
		if got := isCopy(traits{title: c.a, people: people}, traits{title: c.b, people: people}); got != c.copy {
			t.Errorf("%q and %q: copy %v", c.a, c.b, got)
		}
	}
	if isCopy(traits{title: "Emma", people: []string{"jane austen"}}, traits{title: "Emma", people: []string{"m c beaton"}}) {
		t.Error("two authors' Emma are one book")
	}
}
