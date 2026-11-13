package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	folds       = 5
	innerFolds  = 4
	targetPrec  = 0.8
	maxGenres   = 3
	minCalPos   = 5
	minGenrePos = 15
)

func foldOf(r *Rec, k int) int {
	h := fnv.New32a()
	h.Write([]byte("451:" + r.Group))
	return int(h.Sum32() % uint32(k))
}

// Outputs of the trainers, and the ensembles built from them.
var (
	singleNames = []string{"tfidf", "tfidf1000", "mean", "passages-topk", "passages-share", "passages-mean", "meta", "neighbours"}
	ensembles   = map[string][]string{
		"ens-all":        {"tfidf", "mean", "passages-topk", "passages-share", "meta", "neighbours"},
		"ens-text":       {"tfidf", "mean", "passages-topk", "passages-share", "neighbours"},
		"ens-embed":      {"mean", "passages-topk", "passages-share", "meta", "neighbours"},
		"ens-light":      {"passages-topk", "passages-share", "meta"},
		"ens-nopassages": {"tfidf", "mean", "meta", "neighbours"},
	}
	ensembleNames = []string{"ens-all", "ens-text", "ens-embed", "ens-light", "ens-nopassages"}
)

// trainAll trains every method on train and scores docs with all outputs.
func trainAll(train, docs []*Rec, genres []string, workers int, cost map[string]*Cost) map[string]Scores {
	out := map[string]Scores{}
	for _, tr := range trainers {
		t := time.Now()
		m := tr(train, genres, workers)
		trainTime := time.Since(t)
		t = time.Now()
		res := m.Predict(docs)
		predTime := time.Since(t)
		for name, s := range res {
			out[name] = s
			if cost != nil {
				c := cost[name]
				if c == nil {
					c = &Cost{}
					cost[name] = c
				}
				c.TrainNS += trainTime.Nanoseconds() / int64(len(res))
				c.PredictNS += predTime.Nanoseconds() / int64(len(res))
				c.Docs += len(docs)
				c.Fits++
				c.TrainDocs += len(train)
			}
		}
	}
	return out
}

// Cost is the time the harness spent on one output.
type Cost struct {
	TrainNS, PredictNS int64
	Docs               int
	Fits, TrainDocs    int
}

// calibrate scores the training documents out of fold, for thresholds and
// the stacker.
func calibrationScores(train []*Rec, genres []string, workers int) map[string]Scores {
	out := map[string]Scores{}
	for k := 0; k < innerFolds; k++ {
		var tr, te []*Rec
		var pos []int
		for i, d := range train {
			if foldOf(d, innerFolds*7)%innerFolds == k {
				te = append(te, d)
				pos = append(pos, i)
			} else {
				tr = append(tr, d)
			}
		}
		res := trainAll(tr, te, genres, workers, nil)
		for name, s := range res {
			if out[name] == nil {
				out[name] = newScores(len(train), len(genres))
			}
			for j, i := range pos {
				out[name][i] = s[j]
			}
		}
	}
	return out
}

// ---- stacking ----

type stacker struct {
	methods []string
	std     standardizer
	w       []float64
	prior   []float64
}

func stackFeatures(methods []string, sc map[string]Scores, prior []float64, i, g int) []float32 {
	f := make([]float32, 0, 3*len(methods)+1)
	for _, m := range methods {
		v := sc[m][i][g]
		if math.IsNaN(v) {
			f = append(f, 0, 0, 1)
			continue
		}
		c := math.Min(math.Max(v, 1e-4), 1-1e-4)
		f = append(f, float32(v), float32(math.Log(c/(1-c))), 0)
	}
	return append(f, float32(prior[g]))
}

func fitStacker(methods []string, sc map[string]Scores, y [][]bool, rows []int) *stacker {
	G := len(y[0])
	prior := make([]float64, G)
	for g := 0; g < G; g++ {
		p := 0
		for _, i := range rows {
			if y[i][g] {
				p++
			}
		}
		prior[g] = math.Log((float64(p) + 1) / float64(len(rows)+2))
	}
	var x [][]float32
	var lab []bool
	for _, i := range rows {
		for g := 0; g < G; g++ {
			x = append(x, stackFeatures(methods, sc, prior, i, g))
			lab = append(lab, y[i][g])
		}
	}
	st := &stacker{methods: methods, prior: prior, std: fitStd(x)}
	for i := range x {
		x[i] = st.std.apply(x[i])
	}
	st.w = trainDense(x, lab, 1.0, 100)
	return st
}

func (st *stacker) score(sc map[string]Scores, i, g int) float64 {
	v := st.std.apply(stackFeatures(st.methods, sc, st.prior, i, g))
	z := st.w[len(v)]
	for j, x := range v {
		z += float64(x) * st.w[j]
	}
	return sigmoid(z)
}

func (st *stacker) all(sc map[string]Scores, n, G int) Scores {
	out := make(Scores, n)
	for i := range out {
		out[i] = make([]float64, G)
		for g := range out[i] {
			out[i][g] = st.score(sc, i, g)
		}
	}
	return out
}

// crossStack gives the ensemble's scores for the calibration documents,
// each from a stacker that did not see it.
func crossStack(methods []string, sc map[string]Scores, y [][]bool, train []*Rec) Scores {
	n, G := len(train), len(y[0])
	out := make(Scores, n)
	for k := 0; k < innerFolds; k++ {
		var fit, hold []int
		for i, d := range train {
			if foldOf(d, innerFolds*13)%innerFolds == k {
				hold = append(hold, i)
			} else {
				fit = append(fit, i)
			}
		}
		st := fitStacker(methods, sc, y, fit)
		for _, i := range hold {
			out[i] = make([]float64, G)
			for g := 0; g < G; g++ {
				out[i][g] = st.score(sc, i, g)
			}
		}
	}
	return out
}

// ---- thresholds and decisions ----

// thresholds takes for each genre the lowest score from which the pairs
// above it reach the target precision with at least minCalPos of them; +Inf
// when none does, which is the genre abstaining.
func thresholds(s Scores, y [][]bool, G int) []float64 {
	th := make([]float64, G)
	for g := 0; g < G; g++ {
		type pr struct {
			s float64
			y bool
		}
		var ps []pr
		for i := range s {
			if !math.IsNaN(s[i][g]) {
				ps = append(ps, pr{s[i][g], y[i][g]})
			}
		}
		sort.Slice(ps, func(a, b int) bool { return ps[a].s > ps[b].s })
		th[g] = math.Inf(1)
		tp := 0
		for k, p := range ps {
			if p.y {
				tp++
			}
			if k+1 >= minCalPos && float64(tp)/float64(k+1) >= targetPrec {
				// take the whole run of equal scores
				if k+1 < len(ps) && ps[k+1].s == p.s {
					continue
				}
				th[g] = p.s
			}
		}
	}
	return th
}

// decide gives each document its genres: those at or above their
// thresholds, the best maxGenres of them.
func decide(s Scores, th []float64) [][]int {
	out := make([][]int, len(s))
	for i := range s {
		var c []int
		for g, v := range s[i] {
			if !math.IsNaN(v) && v >= th[g] {
				c = append(c, g)
			}
		}
		sort.Slice(c, func(a, b int) bool { return s[i][c[a]] > s[i][c[b]] })
		if len(c) > maxGenres {
			c = c[:maxGenres]
		}
		out[i] = c
	}
	return out
}

func uniformThreshold(G int, t float64) []float64 {
	th := make([]float64, G)
	for i := range th {
		th[i] = t
	}
	return th
}

// ---- metrics ----

// Tally counts decisions.
type Tally struct {
	TP, FP, FN, Books, Given, Labelled int
	PerGenre                           map[string][3]int
}

func (t *Tally) add(dec [][]int, y [][]bool, genres []string) {
	if t.PerGenre == nil {
		t.PerGenre = map[string][3]int{}
	}
	for i, d := range dec {
		t.Books++
		t.Labelled++
		if len(d) > 0 {
			t.Given++
		}
		got := map[int]bool{}
		for _, g := range d {
			got[g] = true
		}
		for g := range genres {
			c := t.PerGenre[genres[g]]
			switch {
			case got[g] && y[i][g]:
				t.TP++
				c[0]++
			case got[g]:
				t.FP++
				c[1]++
			case y[i][g]:
				t.FN++
				c[2]++
			}
			t.PerGenre[genres[g]] = c
		}
	}
}

func (t *Tally) merge(o *Tally) {
	t.TP += o.TP
	t.FP += o.FP
	t.FN += o.FN
	t.Books += o.Books
	t.Given += o.Given
	t.Labelled += o.Labelled
	if t.PerGenre == nil {
		t.PerGenre = map[string][3]int{}
	}
	for g, c := range o.PerGenre {
		x := t.PerGenre[g]
		for k := range x {
			x[k] += c[k]
		}
		t.PerGenre[g] = x
	}
}

func prf(tp, fp, fn int) (p, r, f float64) {
	if tp+fp > 0 {
		p = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		r = float64(tp) / float64(tp+fn)
	}
	if p+r > 0 {
		f = 2 * p * r / (p + r)
	}
	return
}

// Row is one method's result.
type Row struct {
	Method    string     `json:"method"`
	P         float64    `json:"precision"`
	R         float64    `json:"recall"`
	F1        float64    `json:"f1"`
	MacroF1   float64    `json:"macroF1"`
	Given     float64    `json:"given"`
	BestF1    float64    `json:"bestSingleThresholdF1"`
	BestAt    float64    `json:"bestSingleThreshold"`
	BestP     float64    `json:"bestP"`
	BestR     float64    `json:"bestR"`
	BestGiven float64    `json:"bestGiven"`
	Abstains  int        `json:"genresAbstaining"`
	Genres    []GenreRow `json:"genres,omitempty"`
}

// GenreRow is one genre of one method.
type GenreRow struct {
	Genre   string  `json:"genre"`
	Support int     `json:"support"`
	P       float64 `json:"precision"`
	R       float64 `json:"recall"`
	F1      float64 `json:"f1"`
}

func (t *Tally) row(name string, genres []string) Row {
	r := Row{Method: name}
	r.P, r.R, r.F1 = prf(t.TP, t.FP, t.FN)
	if t.Books > 0 {
		r.Given = float64(t.Given) / float64(t.Books)
	}
	var sum float64
	var n int
	for _, g := range genres {
		c := t.PerGenre[g]
		p, rc, f := prf(c[0], c[1], c[2])
		r.Genres = append(r.Genres, GenreRow{g, c[0] + c[2], p, rc, f})
		if c[0]+c[2] >= 5 {
			sum += f
			n++
		}
	}
	if n > 0 {
		r.MacroF1 = sum / float64(n)
	}
	return r
}

// ---- experiments ----

// Experiment is one measurement: train on train (plus, in a cross-validated
// run, the folds of cv not tested), test on cv or test.
type Experiment struct {
	Name   string
	Base   []*Rec // always in the training set
	CV     []*Rec // cross-validated; nil for a plain transfer
	Test   []*Rec // for a transfer: what is tested
	Genres []string
}

type Result struct {
	Name      string             `json:"name"`
	Genres    []string           `json:"genres"`
	Books     int                `json:"testBooks"`
	Support   map[string]int     `json:"support"`
	Rows      []Row              `json:"rows"`
	ByLang    map[string][]Row   `json:"byLang,omitempty"`
	TrainSecs float64            `json:"seconds"`
	Costs     map[string]CostOut `json:"costs"`
}

// CostOut is time per document.
type CostOut struct {
	TrainSecsPerFit float64 `json:"trainSecondsPerFit"`
	TrainDocsPerFit int     `json:"trainDocsPerFit"`
	PredictMSPerDoc float64 `json:"predictMsPerDoc"`
}

func (e *Experiment) run(workers int) *Result {
	t0 := time.Now()
	G := len(e.Genres)
	all := append(append([]*Rec{}, e.Base...), e.CV...)
	_ = all
	tallies := map[string]*Tally{}
	langTallies := map[string]map[string]*Tally{}
	costs := map[string]*Cost{}
	record := func(name string, th []float64, s Scores, y [][]bool, docs []*Rec) {
		dec := decide(s, th)
		t := tallies[name]
		if t == nil {
			t = &Tally{}
			tallies[name] = t
		}
		t.add(dec, y, e.Genres)
	}
	best := map[string]map[float64]*Tally{}
	recordSweep := func(name string, s Scores, y [][]bool) {
		if best[name] == nil {
			best[name] = map[float64]*Tally{}
		}
		for ti := 1; ti < 20; ti++ {
			t := float64(ti) / 20
			if best[name][t] == nil {
				best[name][t] = &Tally{}
			}
			best[name][t].add(decide(s, uniformThreshold(G, t)), y, e.Genres)
		}
	}
	sliceRecord := func(name string, s Scores, th []float64, test []*Rec, yt [][]bool) {
		slices := map[string]func(*Rec) bool{
			"en":     func(d *Rec) bool { return d.Lang == "en" },
			"de":     func(d *Rec) bool { return d.Lang == "de" },
			"desc":   func(d *Rec) bool { return d.HasDesc },
			"nodesc": func(d *Rec) bool { return !d.HasDesc },
		}
		for l, in := range slices {
			var sub Scores
			var suby [][]bool
			for i, d := range test {
				if in(d) {
					sub = append(sub, s[i])
					suby = append(suby, yt[i])
				}
			}
			if len(sub) == 0 {
				continue
			}
			if langTallies[name] == nil {
				langTallies[name] = map[string]*Tally{}
			}
			if langTallies[name][l] == nil {
				langTallies[name][l] = &Tally{}
			}
			langTallies[name][l].add(decide(sub, th), suby, e.Genres)
		}
	}
	round := func(train, test []*Rec) {
		y := labelMatrix(train, e.Genres)
		yt := labelMatrix(test, e.Genres)
		cal := calibrationScores(train, e.Genres, workers)
		sc := trainAll(train, test, e.Genres, workers, costs)
		for _, name := range singleNames {
			th := thresholds(cal[name], y, G)
			record(name, th, sc[name], yt, test)
			recordSweep(name, sc[name], yt)
			if name == "meta" || name == "tfidf" || name == "mean" {
				sliceRecord(name, sc[name], th, test, yt)
			}
		}
		// Stackers, fit on the out-of-fold scores of the training set.
		rows := make([]int, len(train))
		for i := range rows {
			rows[i] = i
		}
		for _, name := range ensembleNames {
			ms := ensembles[name]
			calStack := crossStack(ms, cal, y, train)
			th := thresholds(calStack, y, G)
			st := fitStacker(ms, cal, y, rows)
			s := st.all(sc, len(test), G)
			record(name, th, s, yt, test)
			recordSweep(name, s, yt)
			sliceRecord(name, s, th, test, yt)
		}
		fmt.Fprintf(os.Stderr, "  %s: round done (%d train, %d test) at %.0f s\n", e.Name, len(train), len(test), time.Since(t0).Seconds())
	}
	testCount := 0
	if e.CV == nil {
		round(e.Base, e.Test)
		testCount = len(e.Test)
	} else {
		for k := 0; k < folds; k++ {
			train := append([]*Rec{}, e.Base...)
			var test []*Rec
			for _, d := range e.CV {
				if foldOf(d, folds) == k {
					test = append(test, d)
				} else {
					train = append(train, d)
				}
			}
			round(train, test)
			testCount += len(test)
		}
	}
	res := &Result{Name: e.Name, Genres: e.Genres, Books: testCount, Costs: map[string]CostOut{}, ByLang: map[string][]Row{}, Support: map[string]int{}}
	names := append(append([]string{}, singleNames...), ensembleNames...)
	for _, name := range names {
		t := tallies[name]
		if t == nil {
			continue
		}
		row := t.row(name, e.Genres)
		for _, gr := range row.Genres {
			res.Support[gr.Genre] = gr.Support
			if gr.P == 0 && gr.R == 0 {
				row.Abstains++
			}
		}
		var bf float64
		for th, bt := range best[name] {
			p, r, f := prf(bt.TP, bt.FP, bt.FN)
			if f > bf {
				bf, row.BestAt, row.BestP, row.BestR = f, th, p, r
				row.BestGiven = float64(bt.Given) / float64(max(bt.Books, 1))
			}
		}
		row.BestF1 = bf
		res.Rows = append(res.Rows, row)
		for l, lt := range langTallies[name] {
			res.ByLang[l] = append(res.ByLang[l], lt.row(name, e.Genres))
		}
	}
	for n, c := range costs {
		res.Costs[n] = CostOut{TrainSecsPerFit: float64(c.TrainNS) / 1e9 / float64(max(c.Fits, 1)), TrainDocsPerFit: c.TrainDocs / max(c.Fits, 1), PredictMSPerDoc: float64(c.PredictNS) / 1e6 / float64(max(c.Docs, 1))}
	}
	res.TrainSecs = time.Since(t0).Seconds()
	return res
}

// ---- command ----

func labelled(recs []Rec, genres []string) []*Rec {
	set := map[string]bool{}
	for _, g := range genres {
		set[g] = true
	}
	var out []*Rec
	for i := range recs {
		r := &recs[i]
		var ls []string
		for _, l := range r.Labels {
			if set[l] {
				ls = append(ls, l)
			}
		}
		if len(ls) == 0 || (len(r.Vecs) == 0 && len(r.Terms) == 0 && len(r.Meta) == 0) {
			continue
		}
		c := *r
		c.Labels = ls
		out = append(out, &c)
	}
	return out
}

func genresWith(recs []Rec, min int) map[string]int {
	n := map[string]int{}
	for _, r := range recs {
		for _, l := range r.Labels {
			n[l]++
		}
	}
	out := map[string]int{}
	for g, c := range n {
		if c >= min {
			out[g] = c
		}
	}
	return out
}

func runEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	gut := fs.String("gutenberg", "gutenberg.gob", "")
	lib := fs.String("library", "", "records of the library sample (optional)")
	workers := fs.Int("workers", 3, "")
	outDir := fs.String("out", "results", "")
	only := fs.String("only", "", "comma-separated experiments")
	_ = fs.Parse(args)
	g, err := loadRecs(*gut)
	if err != nil {
		return err
	}
	var l []Rec
	if *lib != "" {
		if l, err = loadRecs(*lib); err != nil {
			return err
		}
	}
	gc, lc := genresWith(g, minGenrePos), genresWith(l, minGenrePos)
	var gGenres, lGenres, both []string
	for k := range gc {
		gGenres = append(gGenres, k)
		if lc[k] > 0 {
			both = append(both, k)
		}
	}
	for k := range lc {
		lGenres = append(lGenres, k)
	}
	sort.Strings(gGenres)
	sort.Strings(lGenres)
	sort.Strings(both)
	fmt.Fprintf(os.Stderr, "gutenberg genres %d, library genres %d, both %d\n", len(gGenres), len(lGenres), len(both))
	want := func(n string) bool { return *only == "" || strings.Contains(","+*only+",", ","+n+",") }
	var exps []*Experiment
	if want("gutenberg") {
		exps = append(exps, &Experiment{Name: "gutenberg", CV: labelled(g, gGenres), Genres: gGenres})
	}
	if l != nil {
		if want("library") {
			exps = append(exps, &Experiment{Name: "library", CV: labelled(l, lGenres), Genres: lGenres})
		}
		if want("library5") {
			exps = append(exps, &Experiment{Name: "library5", CV: labelled(l, both), Genres: both})
		}
		if want("transfer") {
			exps = append(exps, &Experiment{Name: "transfer", Base: labelled(g, both), Test: labelled(l, both), Genres: both})
		}
		if want("adapted") {
			exps = append(exps, &Experiment{Name: "adapted", Base: labelled(g, both), CV: labelled(l, both), Genres: both})
		}
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	for _, e := range exps {
		fmt.Fprintf(os.Stderr, "== %s\n", e.Name)
		res := e.run(*workers)
		b, _ := json.MarshalIndent(res, "", " ")
		if err := os.WriteFile(fmt.Sprintf("%s/%s.json", *outDir, e.Name), b, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(fmt.Sprintf("%s/%s.md", *outDir, e.Name), []byte(markdown(res)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func markdown(r *Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s: %d test books, %d genres\n\n", r.Name, r.Books, len(r.Genres))
	fmt.Fprintf(&b, "| Method | P | R | F1 | macro F1 | given | genres abstaining | best single threshold: F1 (P, R, given) |\n|---|---|---|---|---|---|---|---|\n")
	for _, x := range r.Rows {
		fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.2f | %.0f%% | %d | %.2f at %.2f (%.2f, %.2f, %.0f%%) |\n",
			x.Method, x.P, x.R, x.F1, x.MacroF1, 100*x.Given, x.Abstains, x.BestF1, x.BestAt, x.BestP, x.BestR, 100*x.BestGiven)
	}
	for _, lang := range []string{"en", "de", "desc", "nodesc"} {
		if rows := r.ByLang[lang]; len(rows) > 0 {
			fmt.Fprintf(&b, "\nMethods on %s books (en, de by language; desc, nodesc by whether the file has a description):\n\n| Method | P | R | F1 | given |\n|---|---|---|---|---|\n", lang)
			for _, x := range rows {
				fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.0f%% |\n", x.Method, x.P, x.R, x.F1, 100*x.Given)
			}
		}
	}
	// per genre: F1 of main methods
	show := []string{"tfidf", "passages-topk", "meta", "neighbours", "ens-all"}
	fmt.Fprintf(&b, "\nPer genre at the calibrated thresholds (P / R, support):\n\n| Genre | n |")
	for _, s := range show {
		fmt.Fprintf(&b, " %s |", s)
	}
	fmt.Fprintf(&b, "\n|---|---|")
	for range show {
		fmt.Fprintf(&b, "---|")
	}
	fmt.Fprintln(&b)
	rows := map[string]Row{}
	for _, x := range r.Rows {
		rows[x.Method] = x
	}
	for gi, g := range r.Genres {
		fmt.Fprintf(&b, "| %s | %d |", g, r.Support[g])
		for _, s := range show {
			x := rows[s].Genres[gi]
			if x.P == 0 && x.R == 0 {
				fmt.Fprintf(&b, " - |")
			} else {
				fmt.Fprintf(&b, " %.2f / %.2f |", x.P, x.R)
			}
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// runStats prints what a set of records is made of.
func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	in := fs.String("in", "", "")
	_ = fs.Parse(args)
	recs, err := loadRecs(*in)
	if err != nil {
		return err
	}
	n := float64(len(recs))
	var tagged, genre, desc, notext, novec int
	var chunks, chars, parse, tok, emb float64
	langs := map[string]int{}
	per := map[string]int{}
	for _, r := range recs {
		langs[r.Lang]++
		if r.Tagged {
			tagged++
		}
		if len(r.Labels) > 0 {
			genre++
		}
		for _, l := range r.Labels {
			per[l]++
		}
		if r.HasDesc {
			desc++
		}
		if r.Chunks == 0 {
			notext++
		}
		if len(r.Vecs) == 0 {
			novec++
		}
		chunks += float64(r.Chunks)
		chars += float64(r.Chars)
		parse += float64(r.ParseNS)
		tok += float64(r.TokenizeNS)
		emb += float64(r.EmbedNS)
	}
	out := map[string]any{
		"books": len(recs), "languages": langs, "taggedShare": float64(tagged) / n, "genreFromTagsShare": float64(genre) / n,
		"descriptionShare": float64(desc) / n, "noTextShare": float64(notext) / n, "noVectorsShare": float64(novec) / n,
		"meanChunks": chunks / n, "meanChars": chars / n, "parseMsPerBook": parse / n / 1e6, "tokenizeMsPerBook": tok / n / 1e6,
		"embedWallMsPerBook": emb / n / 1e6, "perGenre": per,
	}
	b, _ := json.MarshalIndent(out, "", " ")
	fmt.Println(string(b))
	return nil
}
