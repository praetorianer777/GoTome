package main

import (
	"context"
	"encoding/binary"
	"encoding/gob"
	"flag"
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// The passage-vector measurements of #174 (docs/decisions/passage-vectors.md):
// whether a vector per passage finds books a vector per book does not, and
// what quantising the passage vectors costs in what is found.

// A description and the subject headings of the books it is about.
type description struct {
	Text     string
	Subjects []string
}

var descriptions = []description{
	{"stories of cowboys, ranches and gunfights in the American West", []string{"Western stories", "Cowboys -- Fiction"}},
	{"a detective investigates a murder", []string{"Detective and mystery stories", "Mystery fiction", "Murder -- Investigation -- Fiction"}},
	{"science fiction about space travel and the future", []string{"Science fiction"}},
	{"a love story", []string{"Love stories", "Man-woman relationships -- Fiction"}},
	{"a fantasy tale of magic and strange worlds", []string{"Fantasy fiction"}},
	{"a funny, humorous story", []string{"Humorous stories"}},
	{"psychoanalysis and the unconscious mind", []string{"Psychoanalysis"}},
	{"the story of a life, told by the one who lived it", []string{"Autobiographies"}},
	{"a play for the theatre", []string{"Drama"}},
	{"a young man grows up and finds his place in the world", []string{"Bildungsromans"}},
	{"a historical novel", []string{"Historical fiction"}},
	{"children learn to live as good Christians", []string{"Christian life -- Juvenile fiction", "Sunday school literature", "Religious fiction"}},
	{"poems", []string{"Poetry", "American poetry -- 20th century", "German poetry -- 19th century", "German poetry -- 20th century", "English poetry -- 20th century", "German poetry"}},
	{"soldiers in the trenches of the First World War", []string{"World War, 1914-1918 -- Fiction", "War stories"}},
	{"a ghost story that frightens", []string{"Horror tales"}},
	{"the French Revolution", []string{"France -- History -- Revolution, 1789-1799"}},
	{"animals as the heroes of a children's book", []string{"Animals -- Juvenile fiction"}},
	{"philosophy", []string{"Philosophy"}},
}

// A probe is a sentence of a book's text, looked for as a person might look
// for a scene: the book it is from is the one to find.
type probe struct {
	Book int
	// Inside: from the start of a chunk, which the model read; otherwise
	// from past what a 512-token passage holds.
	Inside bool
	Text   string
}

// Queries are the vectors of the descriptions and probes, "query: " first.
type Queries struct {
	Descriptions [][]float32
	Probes       []probe
	ProbeVectors [][]float32
}

var sentence = regexp.MustCompile(`[A-ZÄÖÜÀ-Ý][^.!?\n]{60,220}[.!?]`)

// firstSentence is the first sentence of 60 to 220 characters at or after
// from.
func firstSentence(s string, from int) string {
	if from >= len(s) {
		return ""
	}
	return sentence.FindString(s[from:])
}

func runProbes(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probes", flag.ContinueOnError)
	var c common
	c.flags(fs)
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the chunk file")
	n := fs.Int("n", 400, "books to take probes from")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(174, 1))
	var probes []probe
	var total int
	if err := readBooks(*chunks, func(b Book) error { total++; return nil }); err != nil {
		return err
	}
	keep := 2 * *n * 1000 / total
	if err := readBooks(*chunks, func(b Book) error {
		if rng.IntN(1000) >= keep || len(b.Chunks) == 0 {
			return nil
		}
		// A chunk of those the vectors were made of, not the first,
		// which is mostly a title page or a licence.
		idx := SampleIndexes(len(b.Chunks))
		if len(idx) > 2 {
			idx = idx[1 : len(idx)-1]
		}
		ch := b.Chunks[idx[rng.IntN(len(idx))]]
		in := firstSentence(ch, 200)
		out := firstSentence(ch, 4500)
		if in == "" || out == "" || len(ch) < 5500 {
			return nil
		}
		probes = append(probes, probe{b.ID, true, in}, probe{b.ID, false, out})
		return nil
	}); err != nil {
		return err
	}
	e, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	var q Queries
	q.Probes = probes
	texts := make([]string, 0, len(descriptions))
	for _, d := range descriptions {
		texts = append(texts, "query: "+d.Text)
	}
	if q.Descriptions, err = e.Embed(ctx, texts); err != nil {
		return err
	}
	for i := 0; i < len(probes); i += 16 {
		var t []string
		for _, p := range probes[i:min(i+16, len(probes))] {
			t = append(t, "query: "+p.Text)
		}
		v, err := e.Embed(ctx, t)
		if err != nil {
			return err
		}
		q.ProbeVectors = append(q.ProbeVectors, v...)
	}
	fmt.Printf("%d descriptions, %d probes from %d books\n", len(q.Descriptions), len(probes), len(probes)/2)
	f, err := os.Create(c.out)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(q)
}

// A coding is how passage vectors are stored and compared with a query.
type coding struct {
	name string
	// score of the query against passage i.
	score func(q []float32, i int) float64
}

// store holds every passage vector of the corpus in each coding.
type store struct {
	dim    int
	book   []int // index into books
	fp32   [][]float32
	fp16   [][]float32 // rounded to half precision and back
	i8     [][]int8
	i8s    []float32
	bin    [][6]uint64
	// binC are the signs of each vector less the mean of all: e5's vectors
	// share a large common part, which plain signs mostly record.
	binC   [][6]uint64
	mean   []float32
	nBooks int
}

func newStore(vecs map[int][][]float32, order []int) *store {
	s := &store{nBooks: len(order)}
	for bi, id := range order {
		for _, v := range vecs[id] {
			s.dim = len(v)
			s.book = append(s.book, bi)
			s.fp32 = append(s.fp32, v)
			h := make([]float32, len(v))
			for i, x := range v {
				h[i] = halfRound(x)
			}
			s.fp16 = append(s.fp16, h)
			q, sc := quantize(v)
			s.i8 = append(s.i8, q)
			s.i8s = append(s.i8s, sc)
			s.bin = append(s.bin, signs(v))
		}
	}
	s.mean = make([]float32, s.dim)
	for _, v := range s.fp32 {
		for i, x := range v {
			s.mean[i] += x / float32(len(s.fp32))
		}
	}
	for _, v := range s.fp32 {
		s.binC = append(s.binC, signs(s.centre(v)))
	}
	return s
}

func (s *store) centre(v []float32) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x - s.mean[i]
	}
	return out
}

// bitScores scores every passage by the Hamming distance of the signs,
// and with int8 rescores the best rescore of them, which then come first.
func (s *store) bitScores(q []float32, name string) []float64 {
	sc := make([]float64, len(s.book))
	codes, qb := s.bin, signs(q)
	if strings.Contains(name, "centred") {
		codes, qb = s.binC, signs(s.centre(q))
	}
	for i := range sc {
		sc[i] = -float64(hamming(&qb, &codes[i]))
	}
	if strings.HasSuffix(name, "+int8") {
		idx := topIdx(sc, rescore)
		for i := range sc {
			sc[i] -= 1e6
		}
		for _, i := range idx {
			sc[i] = dot8(q, s.i8[i]) * float64(s.i8s[i])
		}
	}
	return sc
}

// halfRound rounds to IEEE half precision, as pgvector's halfvec stores.
func halfRound(x float32) float32 {
	b := math.Float32bits(x)
	// Ten bits of mantissa kept, rounded to nearest.
	b = (b + 0x1000) &^ 0x1fff
	return math.Float32frombits(b)
}

// quantize is v as int8 with one scale: x ≈ q·scale.
func quantize(v []float32) ([]int8, float32) {
	var m float32
	for _, x := range v {
		m = max(m, float32(math.Abs(float64(x))))
	}
	sc := m / 127
	q := make([]int8, len(v))
	for i, x := range v {
		q[i] = int8(math.Round(float64(x / sc)))
	}
	return q, sc
}

// signs is one bit per dimension, set where it is above zero: pgvector's
// binary_quantize.
func signs(v []float32) [6]uint64 {
	var b [6]uint64
	for i, x := range v {
		if x > 0 {
			b[i/64] |= 1 << (i % 64)
		}
	}
	return b
}

func hamming(a, b *[6]uint64) int {
	n := 0
	for i := range a {
		n += bits.OnesCount64(a[i] ^ b[i])
	}
	return n
}

func dot8(q []float32, v []int8) float64 {
	var s float32
	for i, x := range v {
		s += q[i] * float32(x)
	}
	return float64(s)
}

func dot8i(a, b []int8) int32 {
	var s int32
	for i := range a {
		s += int32(a[i]) * int32(b[i])
	}
	return s
}

func (s *store) codings() []coding {
	return []coding{
		{"fp32", func(q []float32, i int) float64 { return dot(q, s.fp32[i]) }},
		{"halfvec", func(q []float32, i int) float64 { return dot(q, s.fp16[i]) }},
		{"int8", func(q []float32, i int) float64 { return dot8(q, s.i8[i]) * float64(s.i8s[i]) }},
		{"bit", nil}, // hamming on both sides, then the best of them
		{"bit+int8", nil},
		{"bit centred", nil},
		{"bit centred+int8", nil},
	}
}

// rank scores every passage for q and returns the books, best first, with a
// book's score its best passage's (or the mean of its best k).
func (s *store) rank(q []float32, c coding, topk int) []int {
	n := len(s.book)
	sc := make([]float64, n)
	switch {
	case strings.HasPrefix(c.name, "bit"):
		sc = s.bitScores(q, c.name)
	default:
		for i := range sc {
			sc[i] = c.score(q, i)
		}
	}
	return s.booksOf(sc, topk)
}

// rescore is how many passages the binary pass hands to the int8 one.
var rescore = 2000

func topIdx(sc []float64, k int) []int {
	idx := make([]int, len(sc))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		switch {
		case sc[a] > sc[b]:
			return -1
		case sc[a] < sc[b]:
			return 1
		}
		return a - b
	})
	return idx[:min(k, len(idx))]
}

func (s *store) booksOf(sc []float64, topk int) []int {
	per := make([][]float64, s.nBooks)
	for i, x := range sc {
		per[s.book[i]] = append(per[s.book[i]], x)
	}
	bs := make([]float64, s.nBooks)
	for b, xs := range per {
		slices.SortFunc(xs, func(a, b float64) int {
			if a > b {
				return -1
			}
			if a < b {
				return 1
			}
			return 0
		})
		k := min(topk, len(xs))
		var sum float64
		for _, x := range xs[:k] {
			sum += x
		}
		bs[b] = sum / float64(max(k, 1))
	}
	return topIdx(bs, len(bs))
}

// PassageQuality is what one way of searching finds.
type PassageQuality struct {
	Method string `json:"method"`
	// Description precision: of the ten best books for a description, how
	// many have one of its subjects; and its average precision over all.
	DescP10 float64 `json:"descriptionP10"`
	DescMAP float64 `json:"descriptionMAP"`
	// Probes: where the book a sentence is from stands.
	InsideMRR  float64 `json:"insideMRR"`
	InsideAt1  float64 `json:"insideAt1"`
	InsideAt10 float64 `json:"insideAt10"`
	OutMRR     float64 `json:"outsideMRR"`
	OutAt1     float64 `json:"outsideAt1"`
	OutAt10    float64 `json:"outsideAt10"`
}

// PassageResult is the passages step's output.
type PassageResult struct {
	Books, Passages int
	Quality         []PassageQuality
	// Recall of the best 100 passages of each coding against fp32's.
	Recall map[string]float64
}

func runPassages(args []string) error {
	fs := flag.NewFlagSet("passages", flag.ContinueOnError)
	only := fs.String("only", "", "only the methods and codings whose names hold this")
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the chunk file")
	vectors := fs.String("vectors", "", "the vectors of books with -samples 64 -prefix 'passage: '")
	queries := fs.String("queries", "", "what probes wrote")
	out := fs.String("out", "", "the result file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var books []Book
	if err := readBooks(*chunks, func(b Book) error {
		b.Chunks = nil
		books = append(books, b)
		return nil
	}); err != nil {
		return err
	}
	byID := map[int][][]float32{}
	f, err := os.Open(*vectors)
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(f)
	for {
		var s BookSamples
		if err := dec.Decode(&s); err != nil {
			break
		}
		byID[s.ID] = s.Vectors
	}
	f.Close()
	books = slices.DeleteFunc(books, func(b Book) bool { return len(byID[b.ID]) == 0 })
	order := make([]int, len(books))
	index := map[int]int{}
	for i, b := range books {
		order[i] = b.ID
		index[b.ID] = i
	}
	var q Queries
	qf, err := os.Open(*queries)
	if err != nil {
		return err
	}
	if err := gob.NewDecoder(qf).Decode(&q); err != nil {
		return err
	}
	qf.Close()
	s := newStore(byID, order)
	res := PassageResult{Books: len(books), Passages: len(s.book), Recall: map[string]float64{}}
	fmt.Printf("%d books, %d passages, %d probes\n", len(books), len(s.book), len(q.Probes))

	// The book vectors as similar books have them: the mean of 16.
	bookVecs := make([][]float32, len(books))
	for i, b := range books {
		bookVecs[i] = BookVector(Subset(byID[b.ID], 16))
	}
	rankBooks := func(qv []float32) []int {
		sc := make([]float64, len(books))
		for i, v := range bookVecs {
			sc[i] = dot(qv, v)
		}
		return topIdx(sc, len(sc))
	}
	type method struct {
		name string
		rank func(qv []float32) []int
	}
	methods := []method{{"book vector (mean of 16)", rankBooks}}
	for _, c := range s.codings() {
		methods = append(methods, method{"passages, " + c.name + ", best one", func(qv []float32) []int { return s.rank(qv, c, 1) }})
	}
	fp := s.codings()[0]
	methods = append(methods,
		method{"passages, fp32, mean of best 3", func(qv []float32) []int { return s.rank(qv, fp, 3) }},
		method{"blend 0.5 book vector + 0.5 best passage (fp32)", func(qv []float32) []int {
			pass := make([]float64, len(books))
			for i := range pass {
				pass[i] = -1
			}
			for i, b := range s.book {
				pass[b] = max(pass[b], dot(qv, s.fp32[i]))
			}
			sc := make([]float64, len(books))
			for i, v := range bookVecs {
				sc[i] = 0.5*dot(qv, v) + 0.5*pass[i]
			}
			return topIdx(sc, len(sc))
		}},
	)
	relevant := func(d description, b int) bool {
		for _, x := range books[b].Subjects {
			if slices.Contains(d.Subjects, x) {
				return true
			}
		}
		return false
	}
	for _, m := range methods {
		if !strings.Contains(m.name, *only) {
			continue
		}
		var pq PassageQuality
		pq.Method = m.name
		for di, d := range descriptions {
			ranked := m.rank(q.Descriptions[di])
			var hits, rel int
			var ap float64
			for _, b := range books {
				if relevant(d, index[b.ID]) {
					rel++
				}
			}
			for r, b := range ranked {
				if relevant(d, b) {
					hits++
					ap += float64(hits) / float64(r+1)
					if r < 10 {
						pq.DescP10++
					}
				}
			}
			pq.DescMAP += ap / float64(max(rel, 1))
		}
		pq.DescP10 /= float64(10 * len(descriptions))
		pq.DescMAP /= float64(len(descriptions))
		var nIn, nOut float64
		for pi, p := range q.Probes {
			ranked := m.rank(q.ProbeVectors[pi])
			r := slices.Index(ranked, index[p.Book])
			if r < 0 {
				continue
			}
			rr, a1, a10 := 1/float64(r+1), b2f(r == 0), b2f(r < 10)
			if p.Inside {
				nIn++
				pq.InsideMRR += rr
				pq.InsideAt1 += a1
				pq.InsideAt10 += a10
			} else {
				nOut++
				pq.OutMRR += rr
				pq.OutAt1 += a1
				pq.OutAt10 += a10
			}
		}
		pq.InsideMRR /= nIn
		pq.InsideAt1 /= nIn
		pq.InsideAt10 /= nIn
		pq.OutMRR /= nOut
		pq.OutAt1 /= nOut
		pq.OutAt10 /= nOut
		fmt.Printf("%-50s desc P@10 %.3f MAP %.3f | inside MRR %.3f @1 %.2f @10 %.2f | outside MRR %.3f @1 %.2f @10 %.2f\n",
			pq.Method, pq.DescP10, pq.DescMAP, pq.InsideMRR, pq.InsideAt1, pq.InsideAt10, pq.OutMRR, pq.OutAt1, pq.OutAt10)
		res.Quality = append(res.Quality, pq)
	}

	// Recall of the best 100 passages, over every query.
	all := append(slices.Clone(q.Descriptions), q.ProbeVectors...)
	for _, c := range s.codings()[1:] {
		if !strings.Contains(c.name, *only) {
			continue
		}
		var got float64
		for _, qv := range all {
			exact := make([]float64, len(s.book))
			for i := range exact {
				exact[i] = dot(qv, s.fp32[i])
			}
			want := topIdx(exact, 100)
			approx := make([]float64, len(s.book))
			switch {
			case strings.HasPrefix(c.name, "bit"):
				approx = s.bitScores(qv, c.name)
			default:
				for i := range approx {
					approx[i] = c.score(qv, i)
				}
			}
			have := topIdx(approx, 100)
			n := 0
			for _, i := range have {
				if slices.Contains(want, i) {
					n++
				}
			}
			got += float64(n) / 100
		}
		res.Recall[c.name] = got / float64(len(all))
		fmt.Printf("recall@100 of %s: %.3f\n", c.name, res.Recall[c.name])
	}
	return writeJSON(*out, res)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// ScaleResult is how long a scan over n passages takes, per coding.
type ScaleResult struct {
	Passages int                `json:"passages"`
	Threads  int                `json:"threads"`
	Millis   map[string]float64 `json:"msP50"`
}

// runScale times exact scans over n synthetic passages made from the
// corpus's real ones, as a search over a library of that size would run
// them in the app: int8 dot products, and Hamming distances of the signs
// with the best rescored from int8. Half the libraries are visible.
func runScale(args []string) error {
	fs := flag.NewFlagSet("passage-scale", flag.ContinueOnError)
	vectors := fs.String("vectors", "", "the vectors of books")
	sizes := fs.String("n", "3190000,10000000", "passage counts")
	threads := fs.Int("threads", runtime.GOMAXPROCS(0), "goroutines a scan uses")
	int8Max := fs.Int("int8-max", 3190000, "the largest count the int8 scan is timed at (memory)")
	out := fs.String("out", "", "the result file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var base [][]float32
	f, err := os.Open(*vectors)
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(f)
	for {
		var s BookSamples
		if err := dec.Decode(&s); err != nil {
			break
		}
		base = append(base, s.Vectors...)
	}
	f.Close()
	rng := rand.New(rand.NewPCG(174, 2))
	queries := make([][]float32, 20)
	for i := range queries {
		queries[i] = base[rng.IntN(len(base))]
	}
	var results []ScaleResult
	for _, n := range parseInts(*sizes) {
		r := ScaleResult{Passages: n, Threads: *threads, Millis: map[string]float64{}}
		lib := make([]uint8, n)
		bin := make([][6]uint64, n)
		var i8 []int8
		if n <= *int8Max {
			i8 = make([]int8, n*384)
		}
		for i := range n {
			v := blend(base, rng)
			lib[i] = uint8(rng.IntN(4))
			bin[i] = signs(v)
			if i8 != nil {
				q, _ := quantize(v)
				copy(i8[i*384:], q)
			}
		}
		visible := [4]bool{true, false, true, false}
		time1 := func(name string, fn func(q []float32)) {
			var ts []float64
			for _, q := range queries {
				t := time.Now()
				fn(q)
				ts = append(ts, float64(time.Since(t).Microseconds())/1000)
			}
			slices.Sort(ts)
			r.Millis[name] = ts[len(ts)/2]
			fmt.Printf("%d passages, %s: %.1f ms\n", n, name, r.Millis[name])
		}
		parallel := func(fn func(lo, hi int)) {
			var wg sync.WaitGroup
			step := (n + *threads - 1) / *threads
			for lo := 0; lo < n; lo += step {
				wg.Go(func() { fn(lo, min(lo+step, n)) })
			}
			wg.Wait()
		}
		if i8 != nil {
			time1("int8 exact", func(q []float32) {
				qq, _ := quantize(q)
				best := make([]topK, *threads+1)
				var mu sync.Mutex
				parallel(func(lo, hi int) {
					t := newTop(100)
					for i := lo; i < hi; i++ {
						if visible[lib[i]] {
							t.add(i, float64(dot8i(qq, i8[i*384:i*384+384])))
						}
					}
					mu.Lock()
					best = append(best, *t)
					mu.Unlock()
				})
			})
		}
		time1("bit exact", func(q []float32) {
			qb := signs(q)
			var mu sync.Mutex
			parallel(func(lo, hi int) {
				t := newTop(rescore)
				for i := lo; i < hi; i++ {
					if visible[lib[i]] {
						t.add(i, -float64(hamming(&qb, &bin[i])))
					}
				}
				mu.Lock()
				mu.Unlock()
			})
		})
		results = append(results, r)
		bin, i8, lib = nil, nil, nil
		runtime.GC()
	}
	return writeJSON(*out, results)
}

// blend is a synthetic passage vector: two real ones mixed and normalised.
func blend(base [][]float32, rng *rand.Rand) []float32 {
	a, b := base[rng.IntN(len(base))], base[rng.IntN(len(base))]
	w := float32(0.6 + 0.3*rng.Float64())
	v := make([]float32, len(a))
	var norm float64
	for i := range v {
		v[i] = w*a[i] + (1-w)*b[i]
		norm += float64(v[i]) * float64(v[i])
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// topK keeps the k best scores seen, as a scan does.
type topK struct {
	k     int
	idx   []int
	score []float64
	floor float64
}

func newTop(k int) *topK { return &topK{k: k, floor: math.Inf(-1)} }

func (t *topK) add(i int, s float64) {
	if len(t.idx) == t.k && s <= t.floor {
		return
	}
	t.idx = append(t.idx, i)
	t.score = append(t.score, s)
	if len(t.idx) >= 2*t.k {
		t.compact()
	}
}

func (t *topK) compact() {
	o := make([]int, len(t.idx))
	for i := range o {
		o[i] = i
	}
	slices.SortFunc(o, func(a, b int) int {
		if t.score[a] > t.score[b] {
			return -1
		}
		if t.score[a] < t.score[b] {
			return 1
		}
		return 0
	})
	o = o[:t.k]
	idx, sc := make([]int, t.k), make([]float64, t.k)
	for j, i := range o {
		idx[j], sc[j] = t.idx[i], t.score[i]
	}
	t.idx, t.score, t.floor = idx, sc, sc[t.k-1]
}

// runExport writes n synthetic passages as COPY text for Postgres: a chunk
// ID, a library of four, and the vector.
func runExport(args []string) error {
	fs := flag.NewFlagSet("passage-export", flag.ContinueOnError)
	vectors := fs.String("vectors", "", "the vectors of books")
	n := fs.Int("n", 1000000, "passages")
	out := fs.String("out", "", "the COPY file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var base [][]float32
	f, err := os.Open(*vectors)
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(f)
	for {
		var s BookSamples
		if err := dec.Decode(&s); err != nil {
			break
		}
		base = append(base, s.Vectors...)
	}
	f.Close()
	w, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer w.Close()
	rng := rand.New(rand.NewPCG(174, 3))
	libs := []string{
		"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004",
	}
	var sb strings.Builder
	var id [16]byte
	for range *n {
		v := blend(base, rng)
		binary.BigEndian.PutUint64(id[:8], rng.Uint64())
		binary.BigEndian.PutUint64(id[8:], rng.Uint64())
		sb.Reset()
		fmt.Fprintf(&sb, "%x-%x-%x-%x-%x\t%s\t[", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16], libs[rng.IntN(4)])
		for i, x := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "%.5f", x)
		}
		sb.WriteString("]\n")
		if _, err := w.WriteString(sb.String()); err != nil {
			return err
		}
	}
	return w.Close()
}

// RecallResult is how many of the best 100 passages by int8 the centred
// signs find among n, rescoring their best r by int8.
type RecallResult struct {
	Passages int             `json:"passages"`
	Recall   map[int]float64 `json:"recallAt100ByRescored"`
	Books    map[int]float64 `json:"bestTenBooksByRescored"`
}

// runRecall measures, over n synthetic passages and the probes' and
// descriptions' real query vectors, what the coarse pass over the centred
// signs loses as the library grows.
func runRecall(args []string) error {
	fs := flag.NewFlagSet("passage-recall", flag.ContinueOnError)
	vectors := fs.String("vectors", "", "the vectors of books")
	queries := fs.String("queries", "", "what probes wrote")
	sizes := fs.String("n", "100000,1000000,3190000", "passage counts")
	rescored := fs.String("r", "2000,5000,10000,20000", "passages rescored")
	nq := fs.Int("q", 100, "queries")
	out := fs.String("out", "", "the result file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var base [][]float32
	f, err := os.Open(*vectors)
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(f)
	for {
		var s BookSamples
		if err := dec.Decode(&s); err != nil {
			break
		}
		base = append(base, s.Vectors...)
	}
	f.Close()
	var qs Queries
	qf, err := os.Open(*queries)
	if err != nil {
		return err
	}
	if err := gob.NewDecoder(qf).Decode(&qs); err != nil {
		return err
	}
	qf.Close()
	all := append(slices.Clone(qs.Descriptions), qs.ProbeVectors...)
	rng := rand.New(rand.NewPCG(174, 4))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	all = all[:min(*nq, len(all))]
	mean := make([]float32, 384)
	for _, v := range base {
		for i, x := range v {
			mean[i] += x / float32(len(base))
		}
	}
	centred := func(v []float32) [6]uint64 {
		c := make([]float32, len(v))
		for i, x := range v {
			c[i] = x - mean[i]
		}
		return signs(c)
	}
	var results []RecallResult
	for _, n := range parseInts(*sizes) {
		res := RecallResult{Passages: n, Recall: map[int]float64{}, Books: map[int]float64{}}
		i8 := make([]int8, n*384)
		sc8 := make([]float32, n)
		bin := make([][6]uint64, n)
		// A passage's book: 66 passages a book, as on the owner's library.
		book := func(i int) int { return i / 66 }
		for i := range n {
			v := blend(base, rng)
			q, s := quantize(v)
			copy(i8[i*384:], q)
			sc8[i] = s
			bin[i] = centred(v)
		}
		score8 := func(q []float32, i int) float64 { return dot8(q, i8[i*384:i*384+384]) * float64(sc8[i]) }
		topBooks := func(idx []int, score func(int) float64) []int {
			best := map[int]float64{}
			for _, i := range idx {
				if s, ok := best[book(i)]; !ok || score(i) > s {
					best[book(i)] = score(i)
				}
			}
			var bs []int
			for b := range best {
				bs = append(bs, b)
			}
			slices.SortFunc(bs, func(a, b int) int {
				if best[a] > best[b] {
					return -1
				}
				if best[a] < best[b] {
					return 1
				}
				return a - b
			})
			return bs[:min(10, len(bs))]
		}
		for _, q := range all {
			exact := newTop(100)
			coarse := map[int]*topK{}
			for _, r := range parseInts(*rescored) {
				coarse[r] = newTop(r)
			}
			qb := centred(q)
			for i := range n {
				exact.add(i, score8(q, i))
				h := -float64(hamming(&qb, &bin[i]))
				for _, t := range coarse {
					t.add(i, h)
				}
			}
			exact.compact()
			wantBooks := topBooks(exact.idx, func(i int) float64 { return score8(q, i) })
			for r, t := range coarse {
				if len(t.idx) > t.k {
					t.compact()
				}
				re := newTop(100)
				for _, i := range t.idx {
					re.add(i, score8(q, i))
				}
				re.compact()
				hit := 0
				for _, i := range re.idx {
					if slices.Contains(exact.idx, i) {
						hit++
					}
				}
				res.Recall[r] += float64(hit) / 100 / float64(len(all))
				got := topBooks(t.idx, func(i int) float64 { return score8(q, i) })
				bh := 0
				for _, b := range got {
					if slices.Contains(wantBooks, b) {
						bh++
					}
				}
				res.Books[r] += float64(bh) / float64(len(wantBooks)) / float64(len(all))
			}
		}
		fmt.Printf("%d passages: recall@100 %v, best ten books %v\n", n, res.Recall, res.Books)
		results = append(results, res)
		i8, bin, sc8 = nil, nil, nil
		runtime.GC()
	}
	return writeJSON(*out, results)
}
