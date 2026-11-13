package genres

import (
	"bytes"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

func TestTheShippedEnsembleReadsAndHoldsOnlyGenresOfTheList(t *testing.T) {
	e, err := Shipped()
	if err != nil || e == nil {
		t.Fatalf("shipped: %v, %v", e, err)
	}
	for _, g := range e.Genres {
		if _, ok := Get(g); !ok {
			t.Errorf("the ensemble knows %q, which the list does not", g)
		}
	}
	if e.Content[embed.E5Small.Model.Name] == nil || e.Metadata[embed.E5Small.Model.Name] == nil || e.Words["en"] == nil {
		t.Errorf("heads %v / %v, words %v", e.Content, e.Metadata, e.Words)
	}
	if len(e.Words) != 1 {
		t.Errorf("word tables for %d languages; only English ships", len(e.Words))
	}
	decides := 0
	for _, st := range e.Stackers {
		for _, th := range st.Thresholds {
			if th < 0 || (th > 1 && th != abstain) {
				t.Errorf("stacker %04b has threshold %v", st.Mask, th)
			}
			if th <= 1 {
				decides++
			}
		}
	}
	if len(e.Stackers) != 1<<numInputs-1 || decides == 0 {
		t.Errorf("%d stackers, %d thresholds that decide", len(e.Stackers), decides)
	}
	body, err := e.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := DecodeEnsemble(body, e.Words)
	if err != nil {
		t.Fatal(err)
	}
	if b2, _ := again.Encode(); !bytes.Equal(body, b2) {
		t.Error("the ensemble reads back otherwise")
	}
}

func TestTheShippedWordsTellAWestern(t *testing.T) {
	e, _ := Shipped()
	counts := wordcount.Count([]string{repeat(
		"The sheriff rode across the prairie to the ranch, where the cowboys drove the cattle. "+
			"He drew his revolver; the outlaws fled from the saloon to their horses and the canyon. "+
			"The rustlers had stolen the herd, and the posse followed the trail over the mesa. ", 6)})
	j := e.Judge(Input{Lang: "en", Counts: counts})
	western := slices.Index(e.Genres, "western")
	if j.Inputs[InputWords] == nil || j.Inputs[InputWords][western] < 0.5 {
		t.Fatalf("the words give a western %v", j.Inputs[InputWords])
	}
	got, why := e.Decide(j, nil, MaxGenres)
	if len(got) == 0 || got[0].Genre != "western" {
		t.Fatalf("decided %v", got)
	}
	if _, ok := why[0].Inputs["words"]; !ok || len(why[0].Words) == 0 || len(why[0].Words) > WhyWords {
		t.Errorf("reasons %+v", why[0])
	}
	for _, w := range why[0].Words {
		if counts[w] == 0 {
			t.Errorf("a reason is %q, which the book does not hold", w)
		}
	}
	if got, _ := e.Decide(j, []string{"western"}, MaxGenres); slices.ContainsFunc(got, func(s Scored) bool { return s.Genre == "western" }) {
		t.Error("a genre the book has was suggested again")
	}
}

func repeat(s string, n int) string {
	var b bytes.Buffer
	for range n {
		b.WriteString(s)
	}
	return b.String()
}

// synthetic are books of two genres whose vectors point two ways and one
// whose label is chance.
func synthetic(n int) []TrainBook {
	rng := rand.New(rand.NewPCG(528, 2))
	var out []TrainBook
	for i := range n {
		v := make([]float32, 8)
		for j := range v {
			v[j] = float32(rng.NormFloat64() * 0.3)
		}
		labels := []string{"crime"}
		if i%2 == 1 {
			labels = []string{"fantasy"}
			v[1] += 1
		} else {
			v[0] += 1
		}
		if rng.IntN(3) == 0 {
			labels = append(labels, "poetry")
		}
		out = append(out, TrainBook{ID: strconv.Itoa(i), Content: v, Labels: labels})
	}
	return out
}

func TestTheEnsembleLearnsCalibratesAndAbstains(t *testing.T) {
	keys := []string{"crime", "fantasy", "poetry"}
	opt := EnsembleOptions{Version: "test", Model: "m", Workers: 2}
	e := TrainEnsemble(synthetic(400), keys, opt)
	if again := TrainEnsemble(synthetic(400), keys, EnsembleOptions{Version: "test", Model: "m", Workers: 1}); !sameEncoding(t, e, again) {
		t.Error("training twice gave other numbers")
	}
	st := e.stackerFor(1 << InputContent)
	if st == nil || st.Mask != 1<<InputContent {
		t.Fatalf("stacker of the content %+v", st)
	}
	if st.Thresholds[2] != abstain || st.Thresholds[0] > 1 || st.Thresholds[1] > 1 {
		t.Errorf("thresholds %v: crime and fantasy should decide, poetry abstain", st.Thresholds)
	}
	j := e.Judge(Input{Model: "m", Content: []float32{0, 1, 0, 0, 0, 0, 0, 0}})
	got, why := e.Decide(j, nil, MaxGenres)
	if len(got) != 1 || got[0].Genre != "fantasy" || why[0].Inputs["content"] < 0.5 {
		t.Errorf("decided %v, %+v", got, why)
	}
	if got, _ := e.Decide(j, nil, 0); len(got) != 0 {
		t.Errorf("a book with no room left was given %v", got)
	}

	// Another model's vectors, or none, are no input: nothing is decided.
	for _, in := range []Input{{Model: "other", Content: []float32{0, 1, 0, 0, 0, 0, 0, 0}}, {Model: "m"}, {Model: "m", Content: []float32{1}}} {
		if got, _ := e.Decide(e.Judge(in), nil, MaxGenres); got != nil {
			t.Errorf("%+v decided %v", in, got)
		}
	}

	// Neighbours the stackers learned with are used; an input no stacker
	// was trained on is left out rather than guessed at.
	shares := map[string]float64{"crime": 1}
	if j := e.Judge(Input{Model: "m", Content: []float32{1, 0, 0, 0, 0, 0, 0, 0}, Shares: shares}); j.stacker.Mask != 1<<InputContent|1<<InputNeighbours {
		t.Errorf("content and neighbours took stacker %04b", j.stacker.Mask)
	}
	if j := e.Judge(Input{Model: "m", Lang: "en", Counts: wordcount.Counts{"word": 3}, Shares: shares}); j.stacker == nil || j.stacker.Mask != 1<<InputNeighbours {
		t.Errorf("words without a table took %+v", j.stacker)
	}
}

func sameEncoding(t *testing.T, a, b *Ensemble) bool {
	t.Helper()
	x, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(x, y)
}

func TestADamagedEnsembleIsRefused(t *testing.T) {
	e := TrainEnsemble(synthetic(200), []string{"crime", "fantasy"}, EnsembleOptions{Model: "m"})
	body, _ := e.Encode()
	if _, err := DecodeEnsemble(body, nil); err != nil {
		t.Fatalf("a whole one: %v", err)
	}
	for name, damage := range map[string]func(*Ensemble){
		"prior":     func(e *Ensemble) { e.Prior = e.Prior[:1] },
		"head":      func(e *Ensemble) { e.Content["m"].Weights[0] = e.Content["m"].Weights[0][:3] },
		"genres":    func(e *Ensemble) { e.Content["m"].Weights = e.Content["m"].Weights[:1] },
		"mask":      func(e *Ensemble) { e.Stackers[0].Mask = 1 << numInputs },
		"stacker":   func(e *Ensemble) { e.Stackers[0].Weights = e.Stackers[0].Weights[:2] },
		"threshold": func(e *Ensemble) { e.Stackers[0].Thresholds = nil },
	} {
		d, _ := DecodeEnsemble(body, nil)
		damage(d)
		b, _ := d.Encode()
		if _, err := DecodeEnsemble(b, nil); err == nil {
			t.Errorf("a damaged %s was read", name)
		}
	}
	if _, err := DecodeEnsemble([]byte("{}"), nil); err == nil {
		t.Error("an empty one was read")
	}
}

func TestThresholdsTakeTheLowestScoreAtThePrecision(t *testing.T) {
	// Scores from high to low with their truth: the first eight are seven
	// right; the ninth is wrong.
	truth := []bool{true, true, true, true, false, true, true, true, false, false}
	var s [][]float64
	var y [][]bool
	var rows []int
	for i, v := range truth {
		s = append(s, []float64{1 - float64(i)/10})
		y = append(y, []bool{v})
		rows = append(rows, i)
	}
	th := thresholds(s, y, rows, 1)
	if math.Abs(th[0]-0.3) > 1e-9 {
		t.Errorf("threshold %v, want 0.3 (7 of 8)", th[0])
	}
	if th := thresholds(s[:4], y[:4], rows[:4], 1); th[0] != abstain {
		t.Errorf("four books set a threshold %v", th[0])
	}
}
