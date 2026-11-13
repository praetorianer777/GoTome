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

// The trainer of the ensemble, as spikes/genres measured it: the inputs are
// trained in innerFolds folds and score the books they did not see; on
// those scores a stacker is fitted for every set of inputs, and calibrated
// on stackers fitted in folds again; the inputs are then trained on every
// book.

const (
	innerFolds = 4
	// headC and headIters are the heads' regression, as the spike's.
	headC     = 1.0
	headIters = 80
	headMin   = 4
	// stackerC and stackerIters are the stacker's.
	stackerC     = 1.0
	stackerIters = 100
	// minCalibration is how many books above a threshold must reach the
	// precision before it is taken.
	minCalibration = 5
	// minStackerBooks is how many books a set of inputs needs to be given
	// a stacker of its own.
	minStackerBooks = 100
)

// TrainBook is one labelled book to train the ensemble on.
type TrainBook struct {
	// ID decides the folds.
	ID       string
	Lang     string
	Counts   wordcount.Counts
	Content  []float32
	Metadata []float32
	Labels   []string
}

// EnsembleOptions set the trainer.
type EnsembleOptions struct {
	// Version names the result.
	Version string
	// Model is the embedding model whose vectors the books carry.
	Model string
	// WordLangs are the languages that get a word table.
	WordLangs []string
	Words     WordOptions
	Workers   int
}

// TrainEnsemble trains the ensemble on the books for the genres given; books
// that carry none of them are left out.
func TrainEnsemble(books []TrainBook, genres []string, opt EnsembleOptions) *Ensemble {
	opt.Workers = max(opt.Workers, 1)
	opt.Words.Workers = opt.Workers
	var docs []TrainBook
	for _, b := range books {
		if slices.ContainsFunc(b.Labels, func(l string) bool { return slices.Contains(genres, l) }) {
			docs = append(docs, b)
		}
	}
	slices.SortFunc(docs, func(a, b TrainBook) int { return cmp.Compare(a.ID, b.ID) })
	y := labelsOf(docs, genres)
	G := len(genres)

	oof := make([][numInputs][]float64, len(docs))
	for k := range innerFolds {
		var train, test []int
		for i, d := range docs {
			if foldOf(d.ID, "528:inputs") == k {
				test = append(test, i)
			} else {
				train = append(train, i)
			}
		}
		trainDocs := pick(docs, train)
		m := trainInputs(trainDocs, genres, opt)
		vecs, labels := make([]Vectors, len(trainDocs)), make([][]string, len(trainDocs))
		for i, d := range trainDocs {
			vecs[i], labels[i] = Vectors{d.Content, d.Metadata}, d.Labels
		}
		for _, i := range test {
			d := docs[i]
			in := Input{Lang: d.Lang, Counts: d.Counts, Model: opt.Model, Content: d.Content, Metadata: d.Metadata,
				Shares: SharesAmong(Vectors{d.Content, d.Metadata}, vecs, labels)}
			oof[i] = m.Judge(in).Inputs
		}
	}

	e := trainInputs(docs, genres, opt)
	e.Version, e.Books = opt.Version, len(docs)
	e.Prior = make([]float64, G)
	for g := range G {
		p := 0
		for i := range docs {
			if y[i][g] {
				p++
			}
		}
		e.Prior[g] = math.Log((float64(p) + 1) / float64(len(docs)+2))
	}
	for mask := 1; mask < 1<<numInputs; mask++ {
		var rows []int
		for i := range docs {
			if maskOf(&oof[i])&mask == mask {
				rows = append(rows, i)
			}
		}
		if len(rows) < minStackerBooks {
			continue
		}
		cal := make([][]float64, len(docs))
		for k := range innerFolds {
			var fit, hold []int
			for _, i := range rows {
				if foldOf(docs[i].ID, "528:stacker") == k {
					hold = append(hold, i)
				} else {
					fit = append(fit, i)
				}
			}
			st := fitStacker(mask, oof, y, e.Prior, fit)
			for _, i := range hold {
				cal[i] = make([]float64, G)
				for g := range G {
					cal[i][g] = st.score(&oof[i], e.Prior[g], g)
				}
			}
		}
		st := fitStacker(mask, oof, y, e.Prior, rows)
		st.Thresholds = thresholds(cal, y, rows, G)
		st.Books = len(rows)
		e.Stackers = append(e.Stackers, st)
	}
	return e
}

func foldOf(id, salt string) int {
	h := fnv.New32a()
	h.Write([]byte(salt + ":" + id))
	return int(h.Sum32() % innerFolds)
}

func pick(docs []TrainBook, idx []int) []TrainBook {
	out := make([]TrainBook, len(idx))
	for k, i := range idx {
		out[k] = docs[i]
	}
	return out
}

func labelsOf(docs []TrainBook, genres []string) [][]bool {
	y := make([][]bool, len(docs))
	for i, d := range docs {
		y[i] = make([]bool, len(genres))
		for g, key := range genres {
			y[i][g] = slices.Contains(d.Labels, key)
		}
	}
	return y
}

// trainInputs trains the word tables and the heads; the neighbours need no
// training.
func trainInputs(docs []TrainBook, genres []string, opt EnsembleOptions) *Ensemble {
	e := &Ensemble{Genres: genres, Content: map[string]*Head{}, Metadata: map[string]*Head{}, Words: map[string]*WordTable{}}
	for _, lang := range opt.WordLangs {
		var wb []WordBook
		for _, d := range docs {
			if d.Lang == lang {
				wb = append(wb, WordBook{ID: d.ID, Counts: d.Counts, Labels: d.Labels})
			}
		}
		if t := TrainWords(lang, wb, genres, opt.Words); t != nil {
			e.Words[lang] = t
		}
	}
	y := labelsOf(docs, genres)
	if h := trainHead(docs, y, func(d TrainBook) []float32 { return d.Content }, opt.Workers); h != nil {
		e.Content[opt.Model] = h
	}
	if h := trainHead(docs, y, func(d TrainBook) []float32 { return d.Metadata }, opt.Workers); h != nil {
		e.Metadata[opt.Model] = h
	}
	return e
}

func trainHead(docs []TrainBook, y [][]bool, vec func(TrainBook) []float32, workers int) *Head {
	var x [][]float32
	var owner []int
	for i, d := range docs {
		if v := vec(d); v != nil {
			x = append(x, v)
			owner = append(owner, i)
		}
	}
	if len(x) == 0 {
		return nil
	}
	G := len(y[0])
	mean, inv := standardiser(x)
	h := &Head{Mean: mean, Inv: inv, Weights: make([][]float32, G)}
	z := standardise(x, mean, inv)
	parallel(G, workers, func(g int) {
		col := make([]bool, len(z))
		p := 0
		for r, i := range owner {
			col[r] = y[i][g]
			if col[r] {
				p++
			}
		}
		if p < headMin || len(z)-p < headMin {
			return
		}
		w := fitDense(z, col, headC, headIters)
		h.Weights[g] = make([]float32, len(w))
		for j, v := range w {
			h.Weights[g][j] = float32(v)
		}
	})
	return h
}

func fitStacker(mask int, in [][numInputs][]float64, y [][]bool, prior []float64, rows []int) *Stacker {
	G := len(prior)
	var x [][]float32
	var lab []bool
	for _, i := range rows {
		for g := range G {
			x = append(x, stackFeatures(mask, &in[i], prior[g], g))
			lab = append(lab, y[i][g])
		}
	}
	mean, inv := standardiser(x)
	return &Stacker{Mask: mask, Mean: mean, Inv: inv, Weights: fitDense(standardise(x, mean, inv), lab, stackerC, stackerIters)}
}

// thresholds takes for each genre the lowest score from which the rows above
// it reach TargetPrecision with at least minCalibration of them, and abstain
// where none does.
func thresholds(s [][]float64, y [][]bool, rows []int, G int) []float64 {
	th := make([]float64, G)
	type scored struct {
		s float64
		y bool
	}
	for g := range G {
		var ps []scored
		for _, i := range rows {
			ps = append(ps, scored{s[i][g], y[i][g]})
		}
		sort.SliceStable(ps, func(a, b int) bool { return ps[a].s > ps[b].s })
		th[g] = abstain
		tp := 0
		for k, p := range ps {
			if p.y {
				tp++
			}
			if k+1 < len(ps) && ps[k+1].s == p.s {
				continue
			}
			if k+1 >= minCalibration && float64(tp)/float64(k+1) >= TargetPrecision {
				th[g] = p.s
			}
		}
	}
	return th
}

func standardiser(rows [][]float32) (mean, inv []float32) {
	d := len(rows[0])
	m := make([]float64, d)
	for _, r := range rows {
		for j, v := range r {
			m[j] += float64(v)
		}
	}
	for j := range m {
		m[j] /= float64(len(rows))
	}
	sd := make([]float64, d)
	for _, r := range rows {
		for j, v := range r {
			e := float64(v) - m[j]
			sd[j] += e * e
		}
	}
	mean, inv = make([]float32, d), make([]float32, d)
	for j := range m {
		mean[j] = float32(m[j])
		if s := math.Sqrt(sd[j] / float64(len(rows))); s > 1e-6 {
			inv[j] = float32(1 / s)
		}
	}
	return mean, inv
}

func standardise(rows [][]float32, mean, inv []float32) [][]float32 {
	out := make([][]float32, len(rows))
	for i, r := range rows {
		out[i] = make([]float32, len(r))
		for j, v := range r {
			out[i][j] = (v - mean[j]) * inv[j]
		}
	}
	return out
}

// fitDense is an L2-penalised logistic regression on dense rows, the bias
// last, positives weighed as fitSparse weighs them.
func fitDense(x [][]float32, y []bool, c float64, iters int) []float64 {
	d := len(x[0])
	var p, n float64
	for _, v := range y {
		if v {
			p++
		} else {
			n++
		}
	}
	wp := 1.0
	if p > 0 {
		wp = math.Min(n/p, maxPosScale)
	}
	f := func(w, g []float64) float64 {
		clear(g)
		loss := 0.0
		for r, row := range x {
			z := w[d]
			for j, v := range row {
				z += float64(v) * w[j]
			}
			s, wt := -1.0, 1.0
			if y[r] {
				s, wt = 1, wp
			}
			loss += wt * logLoss(s*z)
			dz := -wt * s * sigmoid(-s*z)
			for j, v := range row {
				g[j] += dz * float64(v)
			}
			g[d] += dz
		}
		for j := range d {
			loss += 0.5 / c * w[j] * w[j]
			g[j] += w[j] / c
		}
		return loss
	}
	w := make([]float64, d+1)
	minimize(f, w, iters)
	return w
}

func parallel(n, workers int, fn func(i int)) {
	var wg sync.WaitGroup
	next := make(chan int)
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}
	for i := range n {
		next <- i
	}
	close(next)
	wg.Wait()
}
