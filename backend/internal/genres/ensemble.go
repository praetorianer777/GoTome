package genres

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/praetorianer777/gotome/backend/internal/similar"
	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

// The ensemble (#528, docs/decisions/genre-detection.md): four inputs, each
// a probability per genre, combined by a logistic stacker and decided by a
// threshold per genre at which the training books, scored out of fold,
// reached TargetPrecision. A genre whose books never reached it abstains.

// MethodEnsemble is the ensemble, as book_genres.method and the setting
// genres.suggest name it.
const MethodEnsemble = "ensemble"

// The inputs, in the order of the stacker's features.
const (
	InputWords = iota
	InputContent
	InputMetadata
	InputNeighbours
	numInputs
)

// InputNames name the inputs in the reasons a suggestion carries.
var InputNames = [numInputs]string{"words", "content", "metadata", "neighbours"}

const (
	// TargetPrecision is what each genre's threshold is calibrated to.
	TargetPrecision = 0.8
	// abstain is a threshold no probability reaches.
	abstain = 2.0
	// WhyWords is how many of a book's words a suggestion names as its reason.
	WhyWords = 5
)

// Head is a logistic regression per genre on a vector, standardised first.
// Weights[g] is nil for a genre with too few books to learn; its last
// number is the bias.
type Head struct {
	Mean    []float32   `json:"mean"`
	Inv     []float32   `json:"inv"`
	Weights [][]float32 `json:"weights"`
}

// Score is the probability of each genre for the vector, NaN where the head
// has no weights or the vector is missing or of another length.
func (h *Head) Score(v []float32) []float64 {
	out := make([]float64, len(h.Weights))
	for g, w := range h.Weights {
		out[g] = math.NaN()
		if w == nil || len(v) != len(h.Mean) {
			continue
		}
		z := float64(w[len(v)])
		for j, x := range v {
			z += float64((x-h.Mean[j])*h.Inv[j]) * float64(w[j])
		}
		out[g] = sigmoid(z)
	}
	return out
}

// Stacker combines the inputs of its Mask (bit i for input i) into one
// probability per genre: per input the probability, its logit and whether it
// is missing, and the genre's prior, standardised, through one logistic
// regression for every genre. Thresholds are per genre.
type Stacker struct {
	Mask       int       `json:"mask"`
	Mean       []float32 `json:"mean"`
	Inv        []float32 `json:"inv"`
	Weights    []float64 `json:"weights"`
	Thresholds []float64 `json:"thresholds"`
	// Books is how many training books it was fitted and calibrated on.
	Books int `json:"books"`
}

func (st *Stacker) score(in *[numInputs][]float64, prior float64, g int) float64 {
	f := stackFeatures(st.Mask, in, prior, g)
	z := st.Weights[len(f)]
	for j, x := range f {
		z += float64((x-st.Mean[j])*st.Inv[j]) * st.Weights[j]
	}
	return sigmoid(z)
}

func stackFeatures(mask int, in *[numInputs][]float64, prior float64, g int) []float32 {
	var f []float32
	for i := range numInputs {
		if mask&(1<<i) == 0 {
			continue
		}
		v := math.NaN()
		if in[i] != nil {
			v = in[i][g]
		}
		if math.IsNaN(v) {
			f = append(f, 0, 0, 1)
			continue
		}
		c := math.Min(math.Max(v, 1e-4), 1-1e-4)
		f = append(f, float32(v), float32(math.Log(c/(1-c))), 0)
	}
	return append(f, float32(prior))
}

// Ensemble is a trained ensemble: its genres, the heads per embedding model,
// the word tables per language, and a stacker for each set of inputs a book
// may have.
type Ensemble struct {
	// Version changes with every training; it is part of the recipe, so a
	// new one has every book looked at again.
	Version string   `json:"version"`
	Genres  []string `json:"genres"`
	// Prior is the log of each genre's share of the training books.
	Prior []float64 `json:"prior"`
	// Content and Metadata are heads keyed by the embedding model's name.
	Content  map[string]*Head `json:"content"`
	Metadata map[string]*Head `json:"metadata"`
	Stackers []*Stacker       `json:"stackers"`
	// Books is how many labelled books it was trained on.
	Books int `json:"books"`
	// Words are the word tables by language, kept apart from the rest.
	Words map[string]*WordTable `json:"-"`
}

// Input is what is known of a book: any part may be missing.
type Input struct {
	Lang     string
	Counts   wordcount.Counts
	Model    string
	Content  []float32
	Metadata []float32
	// Shares are the neighbours' (Shares), nil when too few have genres.
	Shares map[string]float64
}

// Judgement is the ensemble's view of one book.
type Judgement struct {
	// Inputs are each input's probability per genre, nil where missing.
	Inputs [numInputs][]float64
	// Scores are the stacker's probabilities, nil when no stacker fits.
	Scores  []float64
	stacker *Stacker
	words   *WordTable
	counts  wordcount.Counts
}

// Judge scores a book with every input it has.
func (e *Ensemble) Judge(in Input) Judgement {
	var j Judgement
	if t := e.Words[in.Lang]; t != nil && len(in.Counts) > 0 {
		j.Inputs[InputWords] = e.byGenre(t.Genres, t.Score(in.Counts))
		j.words, j.counts = t, in.Counts
	}
	if h := e.Content[in.Model]; h != nil && in.Content != nil {
		j.Inputs[InputContent] = h.Score(in.Content)
	}
	if h := e.Metadata[in.Model]; h != nil && in.Metadata != nil {
		j.Inputs[InputMetadata] = h.Score(in.Metadata)
	}
	if in.Shares != nil {
		s := make([]float64, len(e.Genres))
		for g, key := range e.Genres {
			s[g] = in.Shares[key]
		}
		j.Inputs[InputNeighbours] = s
	}
	for i, s := range j.Inputs {
		if s != nil && allNaN(s) {
			j.Inputs[i] = nil
		}
	}
	j.stacker = e.stackerFor(maskOf(&j.Inputs))
	if j.stacker == nil {
		return j
	}
	j.Scores = make([]float64, len(e.Genres))
	for g := range e.Genres {
		j.Scores[g] = j.stacker.score(&j.Inputs, e.Prior[g], g)
	}
	return j
}

// byGenre puts a word table's scores into the ensemble's order of genres.
func (e *Ensemble) byGenre(keys []WordGenre, scores []float64) []float64 {
	out := make([]float64, len(e.Genres))
	for g, key := range e.Genres {
		out[g] = math.NaN()
		for k, wg := range keys {
			if wg.Key == key {
				out[g] = scores[k]
			}
		}
	}
	return out
}

func allNaN(s []float64) bool {
	for _, v := range s {
		if !math.IsNaN(v) {
			return false
		}
	}
	return true
}

func maskOf(in *[numInputs][]float64) int {
	m := 0
	for i, s := range in {
		if s != nil {
			m |= 1 << i
		}
	}
	return m
}

// stackerFor is the stacker of the inputs a book has, else of the largest
// part of them one was trained for: an input it was not trained with is left
// unused rather than guessed at.
func (e *Ensemble) stackerFor(mask int) *Stacker {
	var best *Stacker
	for _, st := range e.Stackers {
		if st.Mask&^mask != 0 {
			continue
		}
		if best == nil || bitsOf(st.Mask) > bitsOf(best.Mask) {
			best = st
		}
	}
	return best
}

func bitsOf(m int) int {
	n := 0
	for ; m != 0; m &= m - 1 {
		n++
	}
	return n
}

// Why is what made the ensemble suggest a genre.
type Why struct {
	// Inputs are each input's probability for the genre, by InputNames;
	// missing inputs are left out.
	Inputs map[string]float64 `json:"inputs"`
	// Words are the book's words that weigh most for it.
	Words []string `json:"words,omitempty"`
}

// Decide takes the genres whose score reaches their threshold, the surest
// first, at most limit, none of skip, each with its reasons.
func (e *Ensemble) Decide(j Judgement, skip []string, limit int) ([]Scored, []Why) {
	if j.Scores == nil {
		return nil, nil
	}
	var picked []Scored
	for g, key := range e.Genres {
		if j.Scores[g] >= j.stacker.Thresholds[g] && !slices.Contains(skip, key) {
			picked = append(picked, Scored{key, j.Scores[g]})
		}
	}
	picked = best(picked, limit)
	whys := make([]Why, len(picked))
	for k, p := range picked {
		g := slices.Index(e.Genres, p.Genre)
		w := Why{Inputs: map[string]float64{}}
		for i, s := range j.Inputs {
			if s != nil && !math.IsNaN(s[g]) {
				w.Inputs[InputNames[i]] = s[g]
			}
		}
		if j.words != nil {
			if t := slices.IndexFunc(j.words.Genres, func(wg WordGenre) bool { return wg.Key == p.Genre }); t >= 0 {
				w.Words = j.words.Explain(j.counts, t, WhyWords)
			}
		}
		whys[k] = w
	}
	return picked, whys
}

// Encode writes the ensemble but its word tables, which are written each
// with WordTable.Encode.
func (e *Ensemble) Encode() ([]byte, error) {
	return json.MarshalIndent(e, "", "\t")
}

// DecodeEnsemble reads what Encode wrote and gives it the word tables.
func DecodeEnsemble(data []byte, words map[string]*WordTable) (*Ensemble, error) {
	var e Ensemble
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	G := len(e.Genres)
	if G == 0 || len(e.Prior) != G {
		return nil, errBadEnsemble
	}
	for _, h := range append(mapValues(e.Content), mapValues(e.Metadata)...) {
		if len(h.Weights) != G || len(h.Inv) != len(h.Mean) {
			return nil, errBadEnsemble
		}
		for _, w := range h.Weights {
			if w != nil && len(w) != len(h.Mean)+1 {
				return nil, errBadEnsemble
			}
		}
	}
	for _, st := range e.Stackers {
		n := 3*bitsOf(st.Mask) + 1
		if st.Mask <= 0 || st.Mask >= 1<<numInputs || len(st.Thresholds) != G ||
			len(st.Mean) != n || len(st.Inv) != n || len(st.Weights) != n+1 {
			return nil, errBadEnsemble
		}
	}
	e.Words = words
	return &e, nil
}

func mapValues(m map[string]*Head) []*Head {
	var out []*Head
	for _, h := range m {
		out = append(out, h)
	}
	return out
}

var errBadEnsemble = errors.New("genres: damaged ensemble")

// SharesAmong are a book's Shares among others, nearest by the
// blend of the vectors as the library's neighbours are found.
func SharesAmong(book Vectors, others []Vectors, labels [][]string) map[string]float64 {
	near := make([]Near, 0, len(others))
	for i, o := range others {
		s, ok := blend(book, o)
		if ok {
			near = append(near, Near{Score: s, Genres: labels[i]})
		}
	}
	slices.SortStableFunc(near, func(a, b Near) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	return Shares(near[:min(len(near), Candidates)])
}

func blend(a, b Vectors) (float64, bool) {
	hasC := a.Content != nil && b.Content != nil
	hasM := a.Metadata != nil && b.Metadata != nil
	switch {
	case hasC && hasM:
		return 0.6*similar.Cosine(a.Content, b.Content) + 0.4*similar.Cosine(a.Metadata, b.Metadata), true
	case hasC:
		return similar.Cosine(a.Content, b.Content), true
	case hasM:
		return similar.Cosine(a.Metadata, b.Metadata), true
	}
	return 0, false
}

func (e *Ensemble) String() string {
	return fmt.Sprintf("ensemble %s: %d genres, %d books", e.Version, len(e.Genres), e.Books)
}
