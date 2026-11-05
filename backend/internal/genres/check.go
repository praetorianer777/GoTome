package genres

import (
	"cmp"
	"context"
	"crypto/md5"
	"encoding/hex"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// Methods a check measures: the zero-shot score from the content vector
// alone, from the metadata vector alone and from both blended, the
// neighbours, and the two combined as the setting combines them.
const (
	CheckContent  = "vector-content"
	CheckMetadata = "vector-metadata"
)

// CheckOptions say what a check asks.
type CheckOptions struct {
	// Books is how many tagged books to ask about.
	Books int
	// Precision is the share of right genres a genre's threshold must
	// reach; the check takes the one that finds the most at that.
	Precision float64
	// Neighbours asks for each book's nearest books too, a look at the
	// vectors of its library per book.
	Neighbours bool
}

// CheckReport is what a check found.
type CheckReport struct {
	Model string `json:"model"`
	// Books is every book counted, Tagged those with any tag, FromTags
	// those whose tags give a genre and Embedded those with a vector of
	// the model.
	Books    int `json:"books"`
	Tagged   int `json:"tagged"`
	FromTags int `json:"fromTags"`
	Embedded int `json:"embedded"`
	// PerGenre is how many books each genre's tags give.
	PerGenre map[string]int `json:"perGenre"`
	// Unmapped are the tags of the most books that give no genre: what to
	// add to Spellings first.
	Unmapped []TagCount `json:"unmapped"`
	// Sample is how many tagged books were asked about.
	Sample  int            `json:"sample"`
	Methods []MethodReport `json:"methods"`
}

// TagCount is a tag and how many books have it.
type TagCount struct {
	Tag   string `json:"tag"`
	Books int    `json:"books"`
}

// MethodReport is how well one method gave back the genres of the sample's
// tags.
type MethodReport struct {
	Method string `json:"method"`
	// Current is the method with the thresholds in the code now.
	Current Point `json:"current"`
	// Sweep is the method with one threshold for every genre, at most
	// MaxGenres a book.
	Sweep []Point `json:"sweep,omitempty"`
	// Genres is per genre the threshold of the sweep's that reaches the
	// precision asked for with the most recall, or failing that the best
	// F1.
	Genres []GenreResult `json:"genres,omitempty"`
}

// Point is a method's precision and recall at a threshold, micro-averaged
// over the sample, and the share of the sample given any genre.
type Point struct {
	Threshold float64 `json:"threshold"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	Covered   float64 `json:"covered"`
}

// GenreResult is one genre's best threshold.
type GenreResult struct {
	Genre     string  `json:"genre"`
	Positives int     `json:"positives"`
	Threshold float64 `json:"threshold"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	// Reached says whether the precision asked for was reached.
	Reached bool `json:"reached"`
}

// sampled is one book of the sample: the genres of its tags and each
// method's scores.
type sampled struct {
	truth  []string
	scores map[string]map[string]float64
}

// Check measures how well each method finds the genres the tags of a
// sample of tagged books give, as if they had no tags: its metadata vector
// is made again without them, and its neighbours' genres are those of
// their tags. The sample is chosen by a hash of the book's ID, so a
// library gives the same one each time. It reads one row per book and
// per tag of the catalogue, and for each book of the sample its own
// vectors, and with Neighbours a look at those of its library. It writes
// nothing.
func (s *Service) Check(ctx context.Context, opts CheckOptions) (CheckReport, error) {
	spec, err := s.vectors.Spec(ctx)
	if err != nil {
		return CheckReport{}, err
	}
	q := sqlc.New(s.pool)
	books, err := q.ListGenreCheckBooks(ctx, sqlc.ListGenreCheckBooksParams{
		Model: spec.Model.Name, ModelVersion: similar.Version(spec.Model),
	})
	if err != nil {
		return CheckReport{}, err
	}
	tagRows, err := q.ListAllBookTags(ctx)
	if err != nil {
		return CheckReport{}, err
	}
	tags := map[uuid.UUID][]string{}
	for _, r := range tagRows {
		tags[r.BookID] = append(tags[r.BookID], r.Name)
	}
	r := CheckReport{Model: spec.Model.Name, Books: len(books), PerGenre: map[string]int{}}
	unmapped := map[string]int{}
	type candidate struct {
		id   uuid.UUID
		hash string
	}
	var candidates []candidate
	truth := map[uuid.UUID][]string{}
	for _, b := range books {
		if b.HasContent || b.HasMetadata {
			r.Embedded++
		}
		bt := tags[b.ID]
		if len(bt) == 0 {
			continue
		}
		r.Tagged++
		for _, t := range bt {
			if len(FromTag(t)) == 0 {
				unmapped[t]++
			}
		}
		gs := FromTags(bt)
		if len(gs) == 0 {
			continue
		}
		r.FromTags++
		for _, g := range gs {
			r.PerGenre[g]++
		}
		truth[b.ID] = gs
		if b.HasContent || b.HasMetadata {
			sum := md5.Sum([]byte(b.ID.String()))
			candidates = append(candidates, candidate{b.ID, hex.EncodeToString(sum[:])})
		}
	}
	for t, n := range unmapped {
		r.Unmapped = append(r.Unmapped, TagCount{t, n})
	}
	slices.SortFunc(r.Unmapped, func(a, b TagCount) int { return cmp.Or(b.Books-a.Books, cmp.Compare(a.Tag, b.Tag)) })
	r.Unmapped = r.Unmapped[:min(len(r.Unmapped), 40)]
	slices.SortFunc(candidates, func(a, b candidate) int { return cmp.Compare(a.hash, b.hash) })
	candidates = candidates[:min(len(candidates), opts.Books)]

	genreVecs, err := s.genreVectors(ctx, spec)
	if err != nil {
		return CheckReport{}, err
	}
	var sample []sampled
	for _, c := range candidates {
		book, library, err := s.bookVectors(ctx, spec, c.id)
		if err != nil {
			return CheckReport{}, err
		}
		text, err := q.GetUntaggedMetadataText(ctx, c.id)
		if err != nil {
			return CheckReport{}, err
		}
		if book.Metadata != nil {
			vecs, err := s.vectors.EmbedQueries(ctx, spec, []string{text})
			if err != nil {
				return CheckReport{}, err
			}
			book.Metadata = vecs[0]
		}
		one := sampled{truth: truth[c.id], scores: map[string]map[string]float64{}}
		for method, v := range map[string]Vectors{
			CheckContent: {Content: book.Content}, CheckMetadata: {Metadata: book.Metadata}, MethodVector: book,
		} {
			if m := Margins(v, genreVecs); m != nil {
				one.scores[method] = map[string]float64{}
				for i, g := range List {
					one.scores[method][g.Key] = m[i]
				}
			}
		}
		if opts.Neighbours {
			near, err := s.neighbours(ctx, spec, c.id, library, book)
			if err != nil {
				return CheckReport{}, err
			}
			if shares := Shares(near); shares != nil {
				one.scores[MethodNeighbours] = shares
			}
		}
		sample = append(sample, one)
	}
	r.Sample = len(sample)
	methods := []string{CheckContent, CheckMetadata, MethodVector}
	if opts.Neighbours {
		methods = append(methods, MethodNeighbours, MethodCombined)
	}
	for _, m := range methods {
		r.Methods = append(r.Methods, measure(sample, m, opts.Precision))
	}
	return r, nil
}

// thresholdOf is the threshold a method gives a genre now.
func thresholdOf(method string) func(string) float64 {
	if method == MethodNeighbours {
		return func(string) float64 { return NeighbourShare }
	}
	return func(key string) float64 {
		g, _ := Get(key)
		return g.threshold()
	}
}

// predict is what a method gives a book at the thresholds, at most
// MaxGenres. Combined takes the neighbours' where enough of them have
// genres and the vector's else, at the thresholds in the code, as Suggest
// does.
func predict(b sampled, method string, threshold func(string) float64) []string {
	if method == MethodCombined {
		if _, ok := b.scores[MethodNeighbours]; ok {
			return predict(b, MethodNeighbours, thresholdOf(MethodNeighbours))
		}
		return predict(b, MethodVector, thresholdOf(MethodVector))
	}
	var picked []Scored
	for g, score := range b.scores[method] {
		if score >= threshold(g) {
			picked = append(picked, Scored{g, score})
		}
	}
	var out []string
	for _, p := range best(picked, MaxGenres) {
		out = append(out, p.Genre)
	}
	return out
}

// point counts a method's right and wrong genres over the sample.
func point(sample []sampled, method string, threshold func(string) float64) Point {
	var right, given, wanted, covered int
	for _, b := range sample {
		got := predict(b, method, threshold)
		given += len(got)
		wanted += len(b.truth)
		if len(got) > 0 {
			covered++
		}
		for _, g := range got {
			if slices.Contains(b.truth, g) {
				right++
			}
		}
	}
	return rates(right, given, wanted, covered, len(sample))
}

func rates(right, given, wanted, covered, books int) Point {
	p := Point{Precision: share(right, given), Recall: share(right, wanted), Covered: share(covered, books)}
	if p.Precision+p.Recall > 0 {
		p.F1 = 2 * p.Precision * p.Recall / (p.Precision + p.Recall)
	}
	return p
}

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// measure is a method's report: at the thresholds now, swept over
// thresholds taken from its scores, and the best threshold per genre.
func measure(sample []sampled, method string, precision float64) MethodReport {
	r := MethodReport{Method: method, Current: point(sample, method, thresholdOf(method))}
	if method == MethodCombined {
		return r
	}
	steps := sweep(sample, method)
	for _, t := range steps {
		p := point(sample, method, func(string) float64 { return t })
		p.Threshold = t
		r.Sweep = append(r.Sweep, p)
	}
	for _, g := range List {
		res := GenreResult{Genre: g.Key}
		for _, b := range sample {
			if slices.Contains(b.truth, g.Key) {
				res.Positives++
			}
		}
		if res.Positives == 0 {
			continue
		}
		bestF1 := GenreResult{}
		for _, t := range steps {
			var right, given int
			for _, b := range sample {
				score, ok := b.scores[method][g.Key]
				if !ok || score < t {
					continue
				}
				given++
				if slices.Contains(b.truth, g.Key) {
					right++
				}
			}
			p := rates(right, given, res.Positives, 0, 0)
			at := GenreResult{
				Genre: g.Key, Positives: res.Positives, Threshold: t,
				Precision: p.Precision, Recall: p.Recall, F1: p.F1, Reached: given > 0 && p.Precision >= precision,
			}
			if at.Reached && (!res.Reached || at.Recall > res.Recall) {
				res = at
			}
			if at.F1 > bestF1.F1 {
				bestF1 = at
			}
		}
		if !res.Reached {
			bestF1.Genre, bestF1.Positives = g.Key, res.Positives
			res = bestF1
		}
		r.Genres = append(r.Genres, res)
	}
	return r
}

// sweep are the thresholds a method is tried at: shares in tenths for the
// neighbours, and for a zero-shot score the values above which the
// largest 50% to 0.5% of the scores lie, as a margin's scale is the
// model's.
func sweep(sample []sampled, method string) []float64 {
	if method == MethodNeighbours {
		return []float64{0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1}
	}
	var all []float64
	for _, b := range sample {
		for _, s := range b.scores[method] {
			all = append(all, s)
		}
	}
	if len(all) == 0 {
		return nil
	}
	sort.Float64s(all)
	var out []float64
	for _, q := range []float64{0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.92, 0.94, 0.95, 0.96, 0.97, 0.98, 0.99, 0.995} {
		t := all[min(int(q*float64(len(all))), len(all)-1)]
		if len(out) == 0 || t > out[len(out)-1] {
			out = append(out, t)
		}
	}
	return out
}
