package similar

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestPickLeavesOutCopiesAndCapsAuthorsAndSeries(t *testing.T) {
	series, other := uuid.New(), uuid.New()
	book := traits{id: uuid.New(), title: "Brennholds Schwert", series: &series, people: []string{"marta sieber"}}
	near := []traits{
		{title: "Brennholds Sohn: Brennhold Trilogie (German Edition)", series: &series, people: []string{"marta sieber"}},
		{title: "Brennholds Blut", series: &series, people: []string{"marta sieber"}},
		{title: "Brennholds Sohn", series: &series, people: []string{"marta sieber"}},
		{title: "Brennholds Schwert (German Edition)", people: []string{"marta sieber"}},
		{title: "Im Namen der Nacht", series: &other, people: []string{"buchanan col"}},
		{title: "Die Silberweberin", people: []string{"berndt greta"}},
		{title: "Brennhold: Der Anfang", people: []string{"marta sieber"}},
		{title: "Die Wäscherin von Lyon", people: []string{"berndt greta"}},
		{title: "Der Findling", people: []string{"berndt greta"}},
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
		"Brennholds Sohn: Brennhold Trilogie (German Edition)", "Brennholds Blut",
		"Im Namen der Nacht", "Die Silberweberin", "Die Wäscherin von Lyon", "Ohne Autor",
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
		{"Fuchsbrut", "Fuchsbrut: Kommissar Lenzens zweiter Fall", true},
		{"Das stille Archiv: Historischer Roman (German Edition)", "Das stille Archiv", true},
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
