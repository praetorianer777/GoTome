package readimport

import (
	"os"
	"slices"
	"testing"
)

func read(t *testing.T, name string) (string, []Row) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	source, rows, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return source, rows
}

func TestGoodreadsRowsAreReadWithTheirQuirks(t *testing.T) {
	source, rows := read(t, "goodreads.csv")
	if source != SourceGoodreads || len(rows) != 6 {
		t.Fatalf("%s, %d rows", source, len(rows))
	}
	dusk := rows[0]
	if dusk.Title != "Dusk (Dusk, #1)" || dusk.MatchTitle() != "Dusk" || dusk.Status != "completed" || dusk.Rating != 5 ||
		!slices.Equal(dusk.ISBNs, []string{"9780195001013"}) || dusk.ReadOn == nil || dusk.ReadOn.Format("2006-01-02") != "2021-03-14" ||
		!slices.Equal(dusk.Shelves, []string{"favorites", "sci-fi"}) || dusk.Review != "Brine must flow." {
		t.Errorf("Dusk: %+v", dusk)
	}
	if rows[2].Rating != 3 || len(rows[2].ISBNs) != 0 {
		t.Errorf("Emma: %+v", rows[2])
	}
	if rows[3].Status != "wishlist" || rows[3].Rating != 0 || rows[4].Status != "reading" {
		t.Errorf("shelves: %+v %+v", rows[3], rows[4])
	}
	if rows[5].Skip == "" || rows[5].Status != "" {
		t.Errorf("an unknown shelf is skipped: %+v", rows[5])
	}
}

func TestStoryGraphRatingsAreRoundedToWholeStars(t *testing.T) {
	source, rows := read(t, "storygraph.csv")
	if source != SourceStoryGraph || len(rows) != 5 {
		t.Fatalf("%s, %d rows", source, len(rows))
	}
	if rows[0].Rating != 5 || !rows[0].Rounded || rows[1].Rating != 4 || rows[1].Rounded {
		t.Errorf("ratings: %+v %+v", rows[0], rows[1])
	}
	if rows[4].Status != "abandoned" || rows[4].Rating != 2 || rows[4].Review != "Too dense." {
		t.Errorf("Deepsight: %+v", rows[4])
	}
	if rows[0].ReadOn == nil || !slices.Equal(rows[0].Shelves, []string{"favorites", "sci-fi"}) {
		t.Errorf("Dusk: %+v", rows[0])
	}
}

func TestAFileOfAnotherKindIsRefused(t *testing.T) {
	if _, _, err := Parse([]byte("a,b\n1,2\n")); err != ErrUnknownFormat {
		t.Errorf("err = %v", err)
	}
	if _, _, err := Parse(nil); err != ErrUnknownFormat {
		t.Errorf("empty: %v", err)
	}
}
