package similar

import (
	"context"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Taste is how a person's books weigh in what they are recommended, and
// into how many centres they are grouped at most. The weights are what
// `gotome rec-check` compares (docs/similarity.md).
type Taste struct {
	Five, Four, Three, Unrated float64
	// Low is the weight of a book given one or two stars; below zero it
	// counts against, zero leaves such books out.
	Low float64
	// Centres is the most groups the books they liked are cut into.
	Centres int
}

// DefaultTaste is what recommendations use: 5 stars count fully, a
// finished book with no stars a little less than a 4.
var DefaultTaste = Taste{Five: 1, Four: 0.7, Three: 0.3, Unrated: 0.4, Low: 0, Centres: 4}

func (t Taste) weight(status string, rating *int16) float64 {
	if rating == nil {
		if status == "completed" {
			return t.Unrated
		}
		return 0
	}
	switch {
	case *rating >= 5:
		return t.Five
	case *rating == 4:
		return t.Four
	case *rating == 3:
		return t.Three
	}
	return t.Low
}

const (
	// MinTaste is how many books a person must have finished or rated
	// well before there is anything to recommend from.
	MinTaste = 3
	// MaxRecommended is the most books Recommend returns.
	MaxRecommended = 50
	// tasteBooks is how many of a person's books the profile is made from:
	// the best rated and the latest.
	tasteBooks = 300
	// because is how many of the books they liked a recommendation names.
	because = 2
)

// liked is a book of the person's taste with its vectors.
type liked struct {
	id        uuid.UUID
	weight    float64
	content   []float32
	metadata  []float32
	clustered []float32
}

// centre is a group of the books a person liked, as the recommendations
// from it are scored: by the mean of their vectors of each kind, weighed.
type centre struct {
	content, metadata []float32
	members           []liked
}

// Profile is what a person likes: a few centres, not one grey mean, so that
// someone who reads crime and fantasy is recommended both.
type Profile struct {
	centres []centre
	// Books is how many books it was made from.
	Books int
}

// Centres is how many groups the profile has.
func (p Profile) Centres() int { return len(p.centres) }

func (s *Service) taste(ctx context.Context, scope library.Scope, spec embed.Spec, t Taste) ([]liked, error) {
	rows, err := sqlc.New(s.pool).ListTasteBooks(ctx, sqlc.ListTasteBooksParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Model: spec.Model.Name, ModelVersion: Version(spec.Model), Max: tasteBooks,
	})
	if err != nil {
		return nil, err
	}
	var out []liked
	index := map[uuid.UUID]int{}
	for _, r := range rows {
		w := t.weight(r.Status, r.Rating)
		if w == 0 {
			continue
		}
		v, err := ParseLiteral(r.Embedding)
		if err != nil {
			return nil, err
		}
		i, ok := index[r.BookID]
		if !ok {
			i = len(out)
			index[r.BookID] = i
			out = append(out, liked{id: r.BookID, weight: w})
		}
		if r.Kind == KindContent {
			out[i].content = v
		} else {
			out[i].metadata = v
		}
	}
	return out, nil
}

// buildProfile groups the books a person liked into at most t.Centres
// centres, each of at least two books, and says whether there is a profile:
// MinTaste books with a weight above zero. Books that count against are
// held by the nearest centre.
func buildProfile(books []liked, t Taste) (Profile, bool) {
	var pos, neg []liked
	for _, b := range books {
		switch {
		case b.weight > 0:
			pos = append(pos, b)
		case b.weight < 0:
			neg = append(neg, b)
		}
	}
	if len(pos) < MinTaste {
		return Profile{}, false
	}
	slices.SortFunc(pos, func(a, b liked) int {
		if a.weight != b.weight {
			if a.weight > b.weight {
				return -1
			}
			return 1
		}
		return slices.Compare(a.id[:], b.id[:])
	})
	for i := range pos {
		pos[i].clustered = clusterKey(pos[i])
	}
	var groups [][]liked
	var keys [][]float32
	for k := min(max(t.Centres, 1), len(pos)/2); ; k-- {
		k = max(k, 1)
		assign, centres := kmeans(pos, k)
		groups = make([][]liked, k)
		for i, c := range assign {
			groups[c] = append(groups[c], pos[i])
		}
		groups, keys = mergeNear(groups, centres)
		if k == 1 || !slices.ContainsFunc(groups, func(g []liked) bool { return len(g) < 2 }) {
			break
		}
	}
	for _, n := range neg {
		n.clustered = clusterKey(n)
		best, bestCos := 0, math.Inf(-1)
		for c, key := range keys {
			if cos := dot(n.clustered, key); cos > bestCos {
				best, bestCos = c, cos
			}
		}
		groups[best] = append(groups[best], n)
	}
	p := Profile{Books: len(pos) + len(neg)}
	for _, g := range groups {
		p.centres = append(p.centres, centre{
			content:  weightedMean(g, func(b liked) []float32 { return b.content }),
			metadata: weightedMean(g, func(b liked) []float32 { return b.metadata }),
			members:  g,
		})
	}
	return p, true
}

// sameTaste is the cosine of two centres from which they are one: books of
// one taste cut in two by a count of centres that was too many.
const sameTaste = 0.9

// mergeNear joins the groups whose centres are the same taste, nearest
// first, and returns them with their centres.
func mergeNear(groups [][]liked, keys [][]float32) ([][]liked, [][]float32) {
	for len(groups) > 1 {
		a, b, best := 0, 0, sameTaste
		for i := range keys {
			for j := i + 1; j < len(keys); j++ {
				if cos := dot(keys[i], keys[j]); cos >= best {
					a, b, best = i, j, cos
				}
			}
		}
		if a == b {
			break
		}
		groups[a] = append(groups[a], groups[b]...)
		if m := weightedMean(groups[a], func(l liked) []float32 { return l.clustered }); m != nil {
			keys[a] = m
		}
		groups = slices.Delete(groups, b, b+1)
		keys = slices.Delete(keys, b, b+1)
	}
	return groups, keys
}

// clusterKey is the unit vector a book is grouped by: its metadata vector,
// which every book has, else its content vector.
func clusterKey(b liked) []float32 {
	if b.metadata != nil {
		return unit(b.metadata)
	}
	return unit(b.content)
}

// kmeans groups the books by the cosine of their keys into k groups, and
// returns each book's group and the groups' unit centres. It starts from
// the heaviest book and then the book farthest from every centre so far,
// so the same books always give the same groups.
func kmeans(books []liked, k int) ([]int, [][]float32) {
	centres := [][]float32{books[0].clustered}
	for len(centres) < k {
		far, farCos := -1, math.Inf(1)
		for i, b := range books {
			nearest := math.Inf(-1)
			for _, c := range centres {
				nearest = max(nearest, dot(b.clustered, c))
			}
			if nearest < farCos {
				far, farCos = i, nearest
			}
		}
		centres = append(centres, books[far].clustered)
	}
	assign := make([]int, len(books))
	for range 12 {
		changed := false
		for i, b := range books {
			best, bestCos := 0, math.Inf(-1)
			for c, key := range centres {
				if cos := dot(b.clustered, key); cos > bestCos {
					best, bestCos = c, cos
				}
			}
			if assign[i] != best {
				assign[i], changed = best, true
			}
		}
		for c := range centres {
			var members []liked
			for i, b := range books {
				if assign[i] == c {
					members = append(members, b)
				}
			}
			if m := weightedMean(members, func(b liked) []float32 { return b.clustered }); m != nil {
				centres[c] = m
			}
		}
		if !changed {
			break
		}
	}
	return assign, centres
}

// weightedMean is the unit vector of the weighed sum of the unit vectors
// the books have of a kind; nil when none has one or they cancel out.
func weightedMean(books []liked, of func(liked) []float32) []float32 {
	var sum []float64
	for _, b := range books {
		v := of(b)
		if v == nil {
			continue
		}
		if sum == nil {
			sum = make([]float64, len(v))
		}
		u := unit(v)
		for i := range sum {
			sum[i] += b.weight * float64(u[i])
		}
	}
	if sum == nil {
		return nil
	}
	out := make([]float32, len(sum))
	var norm float64
	for _, x := range sum {
		norm += x * x
	}
	if norm == 0 {
		return nil
	}
	norm = math.Sqrt(norm)
	for i, x := range sum {
		out[i] = float32(x / norm)
	}
	return out
}

func unit(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	norm = math.Sqrt(norm)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// Because is a book the person liked that a recommendation is near.
type Because struct {
	ID    uuid.UUID
	Title string
}

// Recommendation is an unread book of the libraries the person sees and
// the books they liked that it is near.
type Recommendation struct {
	BookID  uuid.UUID
	Score   float64
	Because []Because
}

// Recommend returns up to limit unread books of the libraries the scope's
// user sees that are near what they liked, the best first, within the caps
// per author and series of similar books, and without copies of a book they
// liked. A book they read, are reading or gave up, one they are not
// interested in, and the next volume only of a series they read are all
// there is. It reports whether they have a profile: fewer than MinTaste
// finished or rated books give none, and nothing to show.
func (s *Service) Recommend(ctx context.Context, scope library.Scope, limit int) ([]Recommendation, bool, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return nil, false, err
	}
	books, err := s.taste(ctx, scope, spec, DefaultTaste)
	if err != nil {
		return nil, false, err
	}
	profile, ok := buildProfile(books, DefaultTaste)
	if !ok {
		return nil, false, nil
	}
	out, err := s.recommend(ctx, scope, spec, profile, min(max(limit, 1), MaxRecommended), nil, true)
	return out, true, err
}

// recommend ranks the books near a profile. keep names books that count
// although they are the person's own, and seriesRule says whether a volume
// of a series is left out while an earlier one is unread.
func (s *Service) recommend(ctx context.Context, scope library.Scope, spec embed.Spec, p Profile, limit int, keep []uuid.UUID, seriesRule bool) ([]Recommendation, error) {
	contents, metadatas := make([]string, len(p.centres)), make([]string, len(p.centres))
	for i, c := range p.centres {
		if c.content != nil {
			contents[i] = Literal(c.content)
		}
		if c.metadata != nil {
			metadatas[i] = Literal(c.metadata)
		}
	}
	q := sqlc.New(s.pool)
	var rows []sqlc.ListRecommendedRow
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// The plan's estimated cost passes the thresholds of JIT, whose
		// compiling takes longer than the scan it speeds up.
		if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
			return err
		}
		var err error
		rows, err = sqlc.New(tx).ListRecommended(ctx, sqlc.ListRecommendedParams{
			Contents: contents, Metadatas: metadatas, Model: spec.Model.Name, ModelVersion: Version(spec.Model),
			Viewer: scope.Viewer, SeesAll: scope.SeesAll, Keep: append([]uuid.UUID{}, keep...), SeriesRule: seriesRule,
			Max: int32(limit * candidates),
		})
		return err
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.BookID
	}
	var members []uuid.UUID
	for _, c := range p.centres {
		for _, m := range c.members {
			members = append(members, m.id)
		}
	}
	all, err := traitsOf(ctx, q, append(slices.Clone(ids), members...))
	if err != nil {
		return nil, err
	}
	own := make([]traits, 0, len(members))
	for _, id := range members {
		own = append(own, all[id])
	}
	near := make([]traits, len(rows))
	for i, id := range ids {
		near[i] = all[id]
	}
	picked := pickAmong(own, near, limit)
	byID := make(map[uuid.UUID]sqlc.ListRecommendedRow, len(rows))
	for _, r := range rows {
		byID[r.BookID] = r
	}
	out := make([]Recommendation, len(picked))
	for i, id := range picked {
		out[i] = Recommendation{BookID: id, Score: byID[id].Score}
	}
	return out, s.explain(ctx, spec, p, out, func(id uuid.UUID) int { return int(byID[id].Centre) - 1 })
}

// explain names, for each recommendation, the books of its centre that it
// is nearest, as ListSimilarBooks scores nearness.
func (s *Service) explain(ctx context.Context, spec embed.Spec, p Profile, recs []Recommendation, centreOf func(uuid.UUID) int) error {
	if len(recs) == 0 {
		return nil
	}
	q := sqlc.New(s.pool)
	ids := make([]uuid.UUID, len(recs))
	for i, r := range recs {
		ids[i] = r.BookID
	}
	rows, err := q.ListVectorsOf(ctx, sqlc.ListVectorsOfParams{Ids: ids, Model: spec.Model.Name, ModelVersion: Version(spec.Model)})
	if err != nil {
		return err
	}
	vecs := map[uuid.UUID]liked{}
	for _, r := range rows {
		v, err := ParseLiteral(r.Embedding)
		if err != nil {
			return err
		}
		b := vecs[r.BookID]
		if r.Kind == KindContent {
			b.content = v
		} else {
			b.metadata = v
		}
		vecs[r.BookID] = b
	}
	var titled []uuid.UUID
	for i, r := range recs {
		c := p.centres[centreOf(r.BookID)]
		type near struct {
			id    uuid.UUID
			score float64
		}
		var ranked []near
		for _, m := range c.members {
			if m.weight <= 0 {
				continue
			}
			ranked = append(ranked, near{m.id, nearness(vecs[r.BookID], m)})
		}
		slices.SortFunc(ranked, func(a, b near) int {
			if a.score != b.score {
				if a.score > b.score {
					return -1
				}
				return 1
			}
			return slices.Compare(a.id[:], b.id[:])
		})
		for _, n := range ranked[:min(because, len(ranked))] {
			recs[i].Because = append(recs[i].Because, Because{ID: n.id})
			titled = append(titled, n.id)
		}
	}
	titles, err := q.ListBookTitles(ctx, titled)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]string, len(titles))
	for _, t := range titles {
		byID[t.ID] = t.Title
	}
	for i := range recs {
		for j := range recs[i].Because {
			recs[i].Because[j].Title = byID[recs[i].Because[j].ID]
		}
	}
	return nil
}

// nearness is how near two books are, blended as ListSimilarBooks blends.
func nearness(a, b liked) float64 {
	var content, metadata float64
	hasContent := a.content != nil && b.content != nil
	hasMetadata := a.metadata != nil && b.metadata != nil
	if hasContent {
		content = Cosine(a.content, b.content)
	}
	if hasMetadata {
		metadata = Cosine(a.metadata, b.metadata)
	}
	switch {
	case hasContent && hasMetadata:
		return 0.6*content + 0.4*metadata
	case hasContent:
		return content
	}
	return metadata
}

// Dismiss leaves a book out of the scope's user's recommendations from now
// on. It returns catalog.ErrNotFound for a book they may not see.
func (s *Service) Dismiss(ctx context.Context, scope library.Scope, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		visible, err := q.LockVisibleBookIDs(ctx, sqlc.LockVisibleBookIDsParams{Ids: []uuid.UUID{id}, Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		if err != nil {
			return err
		}
		if len(visible) == 0 {
			return catalog.ErrNotFound
		}
		return q.DismissRecommendation(ctx, sqlc.DismissRecommendationParams{UserID: scope.Viewer, BookID: id})
	})
}
