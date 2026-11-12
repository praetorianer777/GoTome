package main

import (
	"math"
	"sort"
	"sync"
)

// Scores are a method's score for each document and genre; NaN says the
// method cannot judge (no text, no vector, no training for the genre).
type Scores [][]float64

// Model is a trained method: it scores documents with every output it has.
type Model interface {
	Predict(docs []*Rec) map[string]Scores
}

// Trainer trains a method on labelled documents.
type Trainer func(train []*Rec, genres []string, workers int) Model

func newScores(n, g int) Scores {
	s := make(Scores, n)
	for i := range s {
		s[i] = make([]float64, g)
		for j := range s[i] {
			s[i][j] = math.NaN()
		}
	}
	return s
}

func labelMatrix(docs []*Rec, genres []string) [][]bool {
	idx := map[string]int{}
	for i, g := range genres {
		idx[g] = i
	}
	y := make([][]bool, len(docs))
	for i, d := range docs {
		y[i] = make([]bool, len(genres))
		for _, l := range d.Labels {
			if j, ok := idx[l]; ok {
				y[i][j] = true
			}
		}
	}
	return y
}

func parallel(n, workers int, fn func(i int)) {
	var wg sync.WaitGroup
	ch := make(chan int)
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := range n {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

func count(y []bool) (p, n int) {
	for _, v := range y {
		if v {
			p++
		} else {
			n++
		}
	}
	return
}

// ---- TF-IDF over the whole text, one model per language ----

const (
	tfidfMinDF    = 3
	tfidfMaxDFShr = 0.5
	tfidfMaxTerms = 30000
	tfidfC        = 10.0
	tfidfMinPos   = 4
	tfidfMinDocs  = 40
	tfidfPruneTop = 1000
)

type langModel struct {
	vocab  map[string]int32
	idf    []float32
	w      [][]float64 // per genre: weights, bias last; nil when untrained
	pruned [][]float64
}

type tfidfModel struct {
	langs map[string]*langModel
	G     int
}

func trainTFIDF(train []*Rec, genres []string, workers int) Model {
	m := &tfidfModel{langs: map[string]*langModel{}, G: len(genres)}
	byLang := map[string][]*Rec{}
	for _, d := range train {
		if len(d.Terms) > 0 {
			byLang[d.Lang] = append(byLang[d.Lang], d)
		}
	}
	for lang, docs := range byLang {
		if len(docs) < tfidfMinDocs {
			continue
		}
		df := map[string]int{}
		for _, d := range docs {
			for t := range d.Terms {
				df[t]++
			}
		}
		type tc struct {
			t string
			n int
		}
		var terms []tc
		maxDF := int(tfidfMaxDFShr * float64(len(docs)))
		for t, n := range df {
			if n >= tfidfMinDF && n <= maxDF {
				terms = append(terms, tc{t, n})
			}
		}
		sort.Slice(terms, func(i, j int) bool {
			if terms[i].n != terms[j].n {
				return terms[i].n > terms[j].n
			}
			return terms[i].t < terms[j].t
		})
		if len(terms) > tfidfMaxTerms {
			terms = terms[:tfidfMaxTerms]
		}
		lm := &langModel{vocab: map[string]int32{}, idf: make([]float32, len(terms))}
		for i, t := range terms {
			lm.vocab[t.t] = int32(i)
			lm.idf[i] = float32(math.Log(float64(1+len(docs)))/float64(1) - math.Log(float64(1+t.n)) + 1)
		}
		x := make([]Sparse, len(docs))
		for i, d := range docs {
			x[i] = lm.vectorize(d)
		}
		y := labelMatrix(docs, genres)
		lm.w = make([][]float64, len(genres))
		lm.pruned = make([][]float64, len(genres))
		parallel(len(genres), workers, func(g int) {
			col := make([]bool, len(docs))
			for i := range docs {
				col[i] = y[i][g]
			}
			if p, n := count(col); p < tfidfMinPos || n < tfidfMinPos {
				return
			}
			w := trainSparse(x, len(terms), col, tfidfC, 60)
			lm.w[g] = w
			lm.pruned[g] = prune(w, tfidfPruneTop)
		})
		m.langs[lang] = lm
	}
	return m
}

// prune keeps the top weights by size, the positive ones first in the
// count, and the bias.
func prune(w []float64, top int) []float64 {
	n := len(w) - 1
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return w[idx[a]] > w[idx[b]] })
	out := make([]float64, len(w))
	out[n] = w[n]
	for _, i := range idx[:min(top, n)] {
		out[i] = w[i]
	}
	return out
}

func (lm *langModel) vectorize(d *Rec) Sparse {
	var s Sparse
	norm := 0.0
	for t, c := range d.Terms {
		if j, ok := lm.vocab[t]; ok {
			v := float32((1 + math.Log(float64(c))) * float64(lm.idf[j]))
			s.Idx = append(s.Idx, j)
			s.Val = append(s.Val, v)
			norm += float64(v) * float64(v)
		}
	}
	if norm > 0 {
		inv := float32(1 / math.Sqrt(norm))
		for i := range s.Val {
			s.Val[i] *= inv
		}
	}
	return s
}

func (m *tfidfModel) Predict(docs []*Rec) map[string]Scores {
	full, pr := newScores(len(docs), m.G), newScores(len(docs), m.G)
	for i, d := range docs {
		lm := m.langs[d.Lang]
		if lm == nil || len(d.Terms) == 0 {
			continue
		}
		x := lm.vectorize(d)
		for g := 0; g < m.G; g++ {
			if lm.w[g] == nil {
				continue
			}
			full[i][g] = sigmoid(sparseDot(x, lm.w[g]))
			pr[i][g] = sigmoid(sparseDot(x, lm.pruned[g]))
		}
	}
	return map[string]Scores{"tfidf": full, "tfidf1000": pr}
}

func sparseDot(x Sparse, w []float64) float64 {
	z := w[len(w)-1]
	for k, j := range x.Idx {
		z += float64(x.Val[k]) * w[j]
	}
	return z
}

// ---- dense heads on vectors ----

const (
	denseC      = 1.0
	denseIters  = 80
	denseMinPos = 4
)

type standardizer struct{ mean, inv []float32 }

func fitStd(rows [][]float32) standardizer {
	d := len(rows[0])
	mean := make([]float64, d)
	for _, r := range rows {
		for j, v := range r {
			mean[j] += float64(v)
		}
	}
	sd := make([]float64, d)
	for j := range mean {
		mean[j] /= float64(len(rows))
	}
	for _, r := range rows {
		for j, v := range r {
			e := float64(v) - mean[j]
			sd[j] += e * e
		}
	}
	s := standardizer{mean: make([]float32, d), inv: make([]float32, d)}
	for j := range mean {
		s.mean[j] = float32(mean[j])
		s.inv[j] = float32(1 / math.Max(math.Sqrt(sd[j]/float64(len(rows))), 1e-6))
	}
	return s
}

func (s standardizer) apply(v []float32) []float32 {
	out := make([]float32, len(v))
	for j := range v {
		out[j] = (v[j] - s.mean[j]) * s.inv[j]
	}
	return out
}

type denseHeads struct {
	std standardizer
	w   [][]float64
}

// fitHeads trains one head per genre on rows, where owner[i] is the index
// of the labelled document row i belongs to.
func fitHeads(rows [][]float32, owner []int, y [][]bool, G, workers int) *denseHeads {
	if len(rows) == 0 {
		return nil
	}
	h := &denseHeads{std: fitStd(rows), w: make([][]float64, G)}
	x := make([][]float32, len(rows))
	for i, r := range rows {
		x[i] = h.std.apply(r)
	}
	parallel(G, workers, func(g int) {
		col := make([]bool, len(rows))
		for i := range rows {
			col[i] = y[owner[i]][g]
		}
		if p, n := count(col); p < denseMinPos || n < denseMinPos {
			return
		}
		h.w[g] = trainDense(x, col, denseC, denseIters)
	})
	return h
}

func (h *denseHeads) score(v []float32, g int) float64 {
	if h == nil || h.w[g] == nil {
		return math.NaN()
	}
	w := h.w[g]
	z := w[len(w)-1]
	for j := range v {
		z += float64(v[j]-h.std.mean[j]) * float64(h.std.inv[j]) * w[j]
	}
	return sigmoid(z)
}

func meanVec(vs [][]float32) []float32 {
	if len(vs) == 0 {
		return nil
	}
	out := make([]float32, len(vs[0]))
	for _, v := range vs {
		for j, x := range v {
			out[j] += x
		}
	}
	var n float64
	for _, x := range out {
		n += float64(x) * float64(x)
	}
	inv := float32(1 / math.Sqrt(n))
	for j := range out {
		out[j] *= inv
	}
	return out
}

type meanModel struct {
	h *denseHeads
	G int
}

func trainMean(train []*Rec, genres []string, workers int) Model {
	var rows [][]float32
	var owner []int
	y := labelMatrix(train, genres)
	for i, d := range train {
		if len(d.Vecs) > 0 {
			rows = append(rows, meanVec(d.Vecs))
			owner = append(owner, i)
		}
	}
	return &meanModel{h: fitHeads(rows, owner, y, len(genres), workers), G: len(genres)}
}

func (m *meanModel) Predict(docs []*Rec) map[string]Scores {
	s := newScores(len(docs), m.G)
	for i, d := range docs {
		if len(d.Vecs) == 0 {
			continue
		}
		v := meanVec(d.Vecs)
		for g := 0; g < m.G; g++ {
			s[i][g] = m.h.score(v, g)
		}
	}
	return map[string]Scores{"mean": s}
}

type metaModel struct {
	h *denseHeads
	G int
}

func trainMeta(train []*Rec, genres []string, workers int) Model {
	var rows [][]float32
	var owner []int
	y := labelMatrix(train, genres)
	for i, d := range train {
		if len(d.Meta) > 0 {
			rows = append(rows, d.Meta)
			owner = append(owner, i)
		}
	}
	return &metaModel{h: fitHeads(rows, owner, y, len(genres), workers), G: len(genres)}
}

func (m *metaModel) Predict(docs []*Rec) map[string]Scores {
	s := newScores(len(docs), m.G)
	for i, d := range docs {
		if len(d.Meta) == 0 {
			continue
		}
		for g := 0; g < m.G; g++ {
			s[i][g] = m.h.score(d.Meta, g)
		}
	}
	return map[string]Scores{"meta": s}
}

// passageTopK is how many of a book's best passages are averaged.
const passageTopK = 3

type passageModel struct {
	h *denseHeads
	G int
}

func trainPassages(train []*Rec, genres []string, workers int) Model {
	var rows [][]float32
	var owner []int
	y := labelMatrix(train, genres)
	for i, d := range train {
		for _, v := range d.Vecs {
			rows = append(rows, v)
			owner = append(owner, i)
		}
	}
	return &passageModel{h: fitHeads(rows, owner, y, len(genres), workers), G: len(genres)}
}

func (m *passageModel) Predict(docs []*Rec) map[string]Scores {
	top, share, mean := newScores(len(docs), m.G), newScores(len(docs), m.G), newScores(len(docs), m.G)
	for i, d := range docs {
		if len(d.Vecs) == 0 {
			continue
		}
		for g := 0; g < m.G; g++ {
			if m.h == nil || m.h.w[g] == nil {
				continue
			}
			ps := make([]float64, len(d.Vecs))
			var sum float64
			var above int
			for k, v := range d.Vecs {
				ps[k] = m.h.score(v, g)
				sum += ps[k]
				if ps[k] > 0.5 {
					above++
				}
			}
			sort.Sort(sort.Reverse(sort.Float64Slice(ps)))
			k := min(passageTopK, len(ps))
			var t float64
			for _, p := range ps[:k] {
				t += p
			}
			top[i][g] = t / float64(k)
			share[i][g] = float64(above) / float64(len(ps))
			mean[i][g] = sum / float64(len(ps))
		}
	}
	return map[string]Scores{"passages-topk": top, "passages-share": share, "passages-mean": mean}
}

// ---- neighbours ----

const neighbourK = 10

type nbrModel struct {
	vecs [][]float32
	y    [][]bool
	G    int
}

func trainNeighbours(train []*Rec, genres []string, _ int) Model {
	m := &nbrModel{G: len(genres)}
	y := labelMatrix(train, genres)
	for i, d := range train {
		if len(d.Vecs) > 0 {
			m.vecs = append(m.vecs, meanVec(d.Vecs))
			m.y = append(m.y, y[i])
		}
	}
	return m
}

func (m *nbrModel) Predict(docs []*Rec) map[string]Scores {
	s := newScores(len(docs), m.G)
	type hit struct {
		sim float64
		i   int
	}
	for i, d := range docs {
		if len(d.Vecs) == 0 || len(m.vecs) == 0 {
			continue
		}
		v := meanVec(d.Vecs)
		hits := make([]hit, len(m.vecs))
		for j, u := range m.vecs {
			var c float64
			for k := range v {
				c += float64(v[k]) * float64(u[k])
			}
			hits[j] = hit{c, j}
		}
		sort.Slice(hits, func(a, b int) bool { return hits[a].sim > hits[b].sim })
		k := min(neighbourK, len(hits))
		for g := 0; g < m.G; g++ {
			n := 0
			for _, h := range hits[:k] {
				if m.y[h.i][g] {
					n++
				}
			}
			s[i][g] = float64(n) / float64(k)
		}
	}
	return map[string]Scores{"neighbours": s}
}

var trainers = []Trainer{trainTFIDF, trainMean, trainMeta, trainPassages, trainNeighbours}
