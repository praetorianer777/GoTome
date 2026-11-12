package metadata

import (
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestJudgeTellsAnotherEditionFromAnotherBook(t *testing.T) {
	book, _ := bare()
	b := checkBook{book: book, isbns: []string{"9780000000002"}}
	noWorks := func(string) (map[string]bool, error) { return nil, nil }
	edition := Candidate{Record: Record{Title: "Buddenbrooks",
		Contributors: []catalog.NewContributor{{Name: "Thomas Mann"}}}}
	other := Candidate{Record: Record{Title: "Der Zauberberg",
		Contributors: []catalog.NewContributor{{Name: "Thomas Mann"}}}}
	right := Candidate{Record: Record{Title: "Buddenbrooks",
		Identifiers: []catalog.Identifier{{Type: catalog.IDISBN, Value: "9780000000002"}}}}

	cases := []struct {
		name    string
		ranked  []Candidate
		outcome string
		rank    int
	}{
		{"the book", []Candidate{right, edition}, OutcomeRight, 0},
		{"another edition first", []Candidate{edition, other, right}, OutcomeEdition, 3},
		{"another book first", []Candidate{other, edition}, OutcomeWrong, 0},
		{"another book, then the book", []Candidate{other, right}, OutcomeWrong, 2},
		{"nothing", nil, OutcomeNone, 0},
	}
	for _, c := range cases {
		outcome, rank, err := judge(c.ranked, b, noWorks)
		if err != nil || outcome != c.outcome || rank != c.rank {
			t.Errorf("%s: %s at %d (%v), want %s at %d", c.name, outcome, rank, err, c.outcome, c.rank)
		}
	}
}

func TestATallyCountsEditionsApartAndWhereTheBookStood(t *testing.T) {
	tally := newTally("all")
	tally.add(OutcomeEdition, 3)
	tally.add(OutcomeWrong, 0)
	tally.add(OutcomeRight, 0)
	if tally.Edition != 1 || tally.Wrong != 1 || tally.Right != 1 || tally.Ranks[3] != 1 || tally.Missing != 1 {
		t.Errorf("tally = %+v", tally)
	}
}
