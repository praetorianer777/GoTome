package genres

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

var (
	seaWords    = []string{"ship", "captain", "harbour", "voyage", "anchor", "sailor", "storm", "wave"}
	castleWords = []string{"castle", "knight", "dragon", "sword", "kingdom", "tower", "banner", "squire"}
	plainWords  = []string{"morning", "walked", "house", "said", "garden", "letter", "table", "window", "evening", "river"}
)

func invented(r *rand.Rand, id int, topic []string) WordBook {
	c := wordcount.Counts{}
	for range 200 {
		c[plainWords[r.IntN(len(plainWords))]] += uint32(1 + r.IntN(5))
	}
	for range 40 {
		c[topic[r.IntN(len(topic))]] += uint32(1 + r.IntN(4))
	}
	c[fmt.Sprintf("name%d", id%7)]++
	return WordBook{ID: fmt.Sprint(id), Counts: c}
}

func inventedBooks(n int) []WordBook {
	r := rand.New(rand.NewPCG(1, 2))
	var out []WordBook
	for i := range n {
		var b WordBook
		if i%2 == 0 {
			b = invented(r, i, seaWords)
			b.Labels = []string{"adventure"}
		} else {
			b = invented(r, i, castleWords)
			b.Labels = []string{"fantasy"}
		}
		out = append(out, b)
	}
	return out
}

func TestTrainedTableTellsTopicsApart(t *testing.T) {
	tab := TrainWords("en", inventedBooks(120), []string{"adventure", "fantasy", "poetry"}, WordOptions{Workers: 2})
	if tab == nil {
		t.Fatal("no table")
	}
	if tab.Genres[2].Trained() {
		t.Fatal("poetry has no books and must not be trained")
	}
	test := inventedBooks(40)
	right := 0
	for _, b := range test {
		s := tab.Score(b.Counts)
		if !math.IsNaN(s[2]) {
			t.Fatalf("untrained genre scored %v", s[2])
		}
		want := 0
		if b.Labels[0] == "fantasy" {
			want = 1
		}
		if (s[want] > 0.5) == (s[1-want] < 0.5) && s[want] > 0.5 {
			right++
		}
	}
	if right < len(test) {
		t.Fatalf("%d of %d told apart", right, len(test))
	}
	why := tab.Explain(test[0].Counts, 0, 3)
	for _, w := range why {
		if !slicesContains(seaWords, w) {
			t.Fatalf("explained by %q, not a sea word: %v", w, why)
		}
	}
	if len(why) == 0 {
		t.Fatal("no reasons")
	}
}

func slicesContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestTrainingIsCappedAndRepeatable(t *testing.T) {
	books := inventedBooks(100)
	a := TrainWords("en", books, []string{"adventure", "fantasy"}, WordOptions{MaxBooks: 60})
	b := TrainWords("en", books, []string{"adventure", "fantasy"}, WordOptions{MaxBooks: 60})
	if a.Books != 60 || !reflect.DeepEqual(a.Words, b.Words) || !reflect.DeepEqual(a.Genres, b.Genres) {
		t.Fatalf("books %d, tables differ", a.Books)
	}
	if TrainWords("en", books[:10], []string{"adventure"}, WordOptions{}) != nil {
		t.Fatal("ten books trained a table")
	}
}

func TestTableFormatRoundTripsAndStaysSmall(t *testing.T) {
	tab := TrainWords("en", inventedBooks(80), []string{"adventure", "fantasy", "poetry"}, WordOptions{})
	data := tab.Encode()
	back, err := DecodeWordTable(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Lang != "en" || back.Books != tab.Books || !reflect.DeepEqual(back.Words, tab.Words) ||
		!reflect.DeepEqual(back.DF, tab.DF) || !reflect.DeepEqual(back.Genres, tab.Genres) {
		t.Fatal("the table changed in storage")
	}
	c := inventedBooks(2)[0].Counts
	if !reflect.DeepEqual(fmt.Sprint(back.Score(c)), fmt.Sprint(tab.Score(c))) {
		t.Fatal("the stored table scores differently")
	}
	if want := 2 * len(tab.Words) * 2; len(data) > want+4096 {
		t.Fatalf("%d bytes for %d words and two genres", len(data), len(tab.Words))
	}
	for _, bad := range [][]byte{nil, []byte("GWT1"), data[:len(data)/2], []byte("nope")} {
		if _, err := DecodeWordTable(bad); err == nil {
			t.Fatalf("%d bytes decoded", len(bad))
		}
	}
}

func TestQuantisingKeepsScores(t *testing.T) {
	w := []float64{0.5, -3.2, 12.7, 0, -0.01, 0.3}
	var g WordGenre
	quantize(&g, w)
	for i, v := range w[:5] {
		got := float64(g.Weights[i]) * float64(g.Scale)
		if math.Abs(got-v) > 12.7/32767 {
			t.Fatalf("weight %d: %v became %v", i, v, got)
		}
	}
	if g.Bias != 0.3 {
		t.Fatalf("bias %v", g.Bias)
	}
}
