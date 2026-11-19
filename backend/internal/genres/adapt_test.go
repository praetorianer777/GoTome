package genres

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestMergingAHeadKeepsWhatEachSideSays(t *testing.T) {
	rng := rand.New(rand.NewPCG(530, 1))
	const d = 6
	vec := func(shift float32) []float32 {
		v := make([]float32, d)
		for j := range v {
			v[j] = float32(rng.NormFloat64()) + shift
		}
		return v
	}
	mk := func(shift float32, w []float32, pos int) *Head {
		mean, inv := make([]float32, d), make([]float32, d)
		for j := range d {
			mean[j], inv[j] = shift, 1+float32(j)/10
		}
		return &Head{Mean: mean, Inv: inv, Weights: [][]float32{w}, Positives: []int{pos}}
	}
	w := []float32{1, -1, 0.5, 0, 2, -2, 0.3}
	lib := mk(3, w, 10)
	base := mk(-1, []float32{0, 0, 0, 0, 0, 0, 0}, 0)
	// Without books of its own the shipped head says nothing: the merge is
	// the library's head, whatever standardisation it was fitted under.
	m := mergeHead(base, lib, &Ensemble{})
	for range 20 {
		x := vec(1)
		a, b := lib.Score(x)[0], m.Score(x)[0]
		if math.Abs(a-b) > 1e-4 {
			t.Fatalf("the library's head says %v, merged %v", a, b)
		}
	}
	if m.Positives[0] != 10 {
		t.Errorf("positives %v", m.Positives)
	}
}

func TestMergingWordTablesAddsTheirBooksAndWords(t *testing.T) {
	mk := func(words []string, books, pos int, w int16) *WordTable {
		g := WordGenre{Key: "crime", Positives: pos, Weights: make([]int16, len(words)), Scale: 0.001}
		for i := range g.Weights {
			g.Weights[i] = w
		}
		t := &WordTable{Lang: "en", Books: books, Words: words, DF: make([]uint32, len(words)), Genres: []WordGenre{g}}
		for i := range t.DF {
			t.DF[i] = 3
		}
		t.prepare()
		return t
	}
	m := mergeWords(mk([]string{"a", "b"}, 100, 30, 1000), mk([]string{"b", "c"}, 20, 10, 2000))
	if m.Books != 120 || !slices.Equal(m.Words, []string{"a", "b", "c"}) || !slices.Equal(m.DF, []uint32{3, 6, 3}) {
		t.Fatalf("books %d words %v df %v", m.Books, m.Words, m.DF)
	}
	g := m.Genres[0]
	if g.Positives != 40 {
		t.Errorf("positives %d", g.Positives)
	}
	got := func(i int) float64 { return float64(g.Weights[i]) * float64(g.Scale) }
	// a: only the shipped table has it; b: both, 30:10 of 1.0 and 2.0.
	if math.Abs(got(0)-0.75) > 0.01 || math.Abs(got(1)-1.25) > 0.01 || math.Abs(got(2)-0.5) > 0.01 {
		t.Errorf("weights %v %v %v", got(0), got(1), got(2))
	}
}
