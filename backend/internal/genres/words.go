package genres

import (
	"cmp"
	"hash/fnv"
	"math"
	"slices"
	"sort"
	"sync"

	"github.com/praetorianer777/gotome/backend/internal/wordcount"
)

// Word weights (#526, docs/decisions/genre-detection.md): TF-IDF over the
// whole text of a book, one logistic regression per genre against the rest,
// one table per language. The words are the surface forms wordcount stores;
// stemming is not applied (the spike's figures are without it, and a stem
// can be added to the stored counts later without reading a file again).

// Defaults of the trainer, the spike's.
const (
	// MaxWords is the vocabulary of a table, the commonest words by the
	// number of books that hold them.
	MaxWords = 30000
	// MaxTrainBooks caps the books a language is trained on; the sparse
	// matrix of 4,000 books is about 200 MB.
	MaxTrainBooks = 4000
	// WordsC is the inverse penalty of the regression.
	WordsC = 10.0

	wordsIters  = 60
	minDF       = 3
	maxDFShare  = 0.5
	minPos      = 4
	minTrainDoc = 40
	maxPosScale = 10.0
)

// WordBook is one labelled book to train on: its counts and its genres.
type WordBook struct {
	// ID only decides which books stay when there are more than the cap.
	ID     string
	Counts wordcount.Counts
	Labels []string
}

// WordOptions set the trainer; zero values are the defaults above.
type WordOptions struct {
	MaxWords, MaxBooks int
	C                  float64
	Workers            int
}

// WordGenre is one genre's weights in a table.
type WordGenre struct {
	Key string
	// Positives is how many of the table's books had the genre.
	Positives int
	// Weights are the words' weights as 16-bit numbers times Scale; nil
	// when there were too few books of the genre or without it to learn.
	Weights []int16
	Scale   float32
	Bias    float32
}

// Trained says the genre has weights.
func (g WordGenre) Trained() bool { return g.Weights != nil }

// WordTable is the weights of one language. It keeps what its books were
// counted as, the number of books and the document frequency of each word,
// and no book or title: a library's own training can add to them.
type WordTable struct {
	Lang string
	// Books is how many books the table was trained on.
	Books int
	// Words are the vocabulary in order; DF[i] is the number of books that
	// held Words[i].
	Words  []string
	DF     []uint32
	Genres []WordGenre

	index map[string]int32
	idf   []float32
}

func (t *WordTable) prepare() {
	t.index = make(map[string]int32, len(t.Words))
	t.idf = make([]float32, len(t.Words))
	for i, w := range t.Words {
		t.index[w] = int32(i)
		t.idf[i] = idf(t.Books, int(t.DF[i]))
	}
}

func idf(books, df int) float32 {
	return float32(math.Log(float64(1+books)) - math.Log(float64(1+df)) + 1)
}

type sparse struct {
	idx []int32
	val []float32
}

// vectorize is the sublinear term frequency times the inverse document
// frequency of the words in the vocabulary, scaled to length 1.
func (t *WordTable) vectorize(c wordcount.Counts) sparse {
	var s sparse
	norm := 0.0
	for w, n := range c {
		if j, ok := t.index[w]; ok && n > 0 {
			v := float32((1 + math.Log(float64(n))) * float64(t.idf[j]))
			s.idx = append(s.idx, j)
			s.val = append(s.val, v)
			norm += float64(v) * float64(v)
		}
	}
	if norm > 0 {
		inv := float32(1 / math.Sqrt(norm))
		for i := range s.val {
			s.val[i] *= inv
		}
	}
	return s
}

// Score gives the probability of each genre of the table for a book's
// counts, NaN for a genre without weights and for a book none of whose words
// are in the vocabulary.
func (t *WordTable) Score(c wordcount.Counts) []float64 {
	out := make([]float64, len(t.Genres))
	x := t.vectorize(c)
	for g, wg := range t.Genres {
		if !wg.Trained() || len(x.idx) == 0 {
			out[g] = math.NaN()
			continue
		}
		z := float64(wg.Bias)
		for k, j := range x.idx {
			z += float64(x.val[k]) * float64(wg.Weights[j]) * float64(wg.Scale)
		}
		out[g] = sigmoid(z)
	}
	return out
}

// Explain names the words of the book that weigh most for a genre, the
// reasons for a score.
func (t *WordTable) Explain(c wordcount.Counts, genre, n int) []string {
	wg := t.Genres[genre]
	if !wg.Trained() {
		return nil
	}
	x := t.vectorize(c)
	order := make([]int, len(x.idx))
	for i := range order {
		order[i] = i
	}
	contrib := func(k int) float64 { return float64(x.val[k]) * float64(wg.Weights[x.idx[k]]) }
	sort.Slice(order, func(a, b int) bool { return contrib(order[a]) > contrib(order[b]) })
	var out []string
	for _, k := range order {
		if len(out) == n || contrib(k) <= 0 {
			break
		}
		out = append(out, t.Words[x.idx[k]])
	}
	return out
}

// TrainWords trains a table for one language on the books given, for the
// genres named. Books beyond MaxBooks are left out, by a hash of their ID so
// that the same books stay. It returns nil when there are too few books to
// learn from.
func TrainWords(lang string, books []WordBook, genres []string, opt WordOptions) *WordTable {
	opt.MaxWords = cmp.Or(opt.MaxWords, MaxWords)
	opt.MaxBooks = cmp.Or(opt.MaxBooks, MaxTrainBooks)
	opt.C = cmp.Or(opt.C, WordsC)
	opt.Workers = max(opt.Workers, 1)

	var docs []WordBook
	for _, b := range books {
		if len(b.Counts) > 0 {
			docs = append(docs, b)
		}
	}
	if len(docs) < minTrainDoc {
		return nil
	}
	if len(docs) > opt.MaxBooks {
		slices.SortFunc(docs, func(a, b WordBook) int { return cmp.Compare(idHash(a.ID), idHash(b.ID)) })
		docs = docs[:opt.MaxBooks]
	}

	df := map[string]int{}
	for _, d := range docs {
		for w := range d.Counts {
			df[w]++
		}
	}
	type wc struct {
		w string
		n int
	}
	var terms []wc
	maxDF := int(maxDFShare * float64(len(docs)))
	for w, n := range df {
		if n >= minDF && n <= maxDF {
			terms = append(terms, wc{w, n})
		}
	}
	slices.SortFunc(terms, func(a, b wc) int {
		if c := cmp.Compare(b.n, a.n); c != 0 {
			return c
		}
		return cmp.Compare(a.w, b.w)
	})
	if len(terms) > opt.MaxWords {
		terms = terms[:opt.MaxWords]
	}
	t := &WordTable{Lang: lang, Books: len(docs), Words: make([]string, len(terms)), DF: make([]uint32, len(terms)),
		Genres: make([]WordGenre, len(genres))}
	for i, e := range terms {
		t.Words[i], t.DF[i] = e.w, uint32(e.n)
	}
	t.prepare()

	x := make([]sparse, len(docs))
	for i, d := range docs {
		x[i] = t.vectorize(d.Counts)
	}
	genreIndex := map[string]int{}
	for i, g := range genres {
		genreIndex[g] = i
		t.Genres[i].Key = g
	}
	y := make([][]bool, len(genres))
	for g := range y {
		y[g] = make([]bool, len(docs))
	}
	for i, d := range docs {
		for _, l := range d.Labels {
			if g, ok := genreIndex[l]; ok {
				y[g][i] = true
			}
		}
	}

	var wg sync.WaitGroup
	next := make(chan int)
	for range opt.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range next {
				p := 0
				for _, v := range y[g] {
					if v {
						p++
					}
				}
				t.Genres[g].Positives = p
				if p < minPos || len(docs)-p < minPos {
					continue
				}
				w := fitSparse(x, len(terms), y[g], opt.C)
				quantize(&t.Genres[g], w)
			}
		}()
	}
	for g := range genres {
		next <- g
	}
	close(next)
	wg.Wait()
	return t
}

func idHash(id string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(id))
	return h.Sum64()
}

// quantize stores the weights (bias last in w) as 16-bit numbers times a
// scale. The bias stays exact: one number.
func quantize(g *WordGenre, w []float64) {
	n := len(w) - 1
	top := 0.0
	for _, v := range w[:n] {
		top = math.Max(top, math.Abs(v))
	}
	g.Bias = float32(w[n])
	g.Weights = make([]int16, n)
	if top == 0 {
		g.Scale = 1
		return
	}
	g.Scale = float32(top / math.MaxInt16)
	for i, v := range w[:n] {
		g.Weights[i] = int16(math.Round(v / float64(g.Scale)))
	}
}

// fitSparse is an L2-penalised logistic regression on the rows, the bias the
// last weight; c is the inverse penalty as scikit-learn has it, the loss
// summed over rows. Positives weigh as many as there are negatives for each,
// at most ten times.
func fitSparse(x []sparse, dim int, y []bool, c float64) []float64 {
	var p, n float64
	for _, v := range y {
		if v {
			p++
		} else {
			n++
		}
	}
	wp := math.Min(n/p, maxPosScale)
	f := func(w, g []float64) float64 {
		clear(g)
		loss := 0.0
		for r, row := range x {
			z := w[dim]
			for k, j := range row.idx {
				z += float64(row.val[k]) * w[j]
			}
			s, wt := -1.0, 1.0
			if y[r] {
				s, wt = 1, wp
			}
			loss += wt * logLoss(s*z)
			dz := -wt * s * sigmoid(-s*z)
			for k, j := range row.idx {
				g[j] += dz * float64(row.val[k])
			}
			g[dim] += dz
		}
		for j := range dim {
			loss += 0.5 / c * w[j] * w[j]
			g[j] += w[j] / c
		}
		return loss
	}
	w := make([]float64, dim+1)
	minimize(f, w, wordsIters)
	return w
}
