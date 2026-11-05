package genres

import (
	"slices"
	"sort"

	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// Ways of finding a genre, as book_genres.method stores them and as the
// setting genres.suggest names them (with "tags", the tags alone, and
// "combined", the vectors where they are sure and the neighbours else).
const (
	MethodVector     = "vector"
	MethodNeighbours = "neighbours"
	MethodCombined   = "combined"
	MethodTags       = "tags"
)

// Neighbours: a book's genres from its Neighbours nearest books that have
// genres, among the nearest Candidates of its library; a genre at least
// NeighbourShare of them have (weighted by how near each is), once at
// least MinNeighbours have any. Guesses until genre-check has measured.
const (
	Neighbours     = 10
	Candidates     = 40
	MinNeighbours  = 3
	NeighbourShare = 0.5
)

// Vectors are a book's vectors of one model; either may be missing.
type Vectors struct {
	Content, Metadata []float32
}

// Scored is a genre with how sure a method is of it.
type Scored struct {
	Genre string
	Score float64
}

// Margins are, per genre of List, how near the book's vectors are to the
// genre's, blended as similar books are (0.6 the content, 0.4 the
// metadata), less the mean over every genre: a book's vectors are nearer
// to some descriptions than others whatever it is about, and the margin
// leaves only what sets one genre apart. Nil for a book without vectors.
func Margins(book Vectors, genres [][]float32) []float64 {
	if book.Content == nil && book.Metadata == nil {
		return nil
	}
	out := make([]float64, len(genres))
	var mean float64
	for i, g := range genres {
		switch {
		case book.Content != nil && book.Metadata != nil:
			out[i] = 0.6*similar.Cosine(book.Content, g) + 0.4*similar.Cosine(book.Metadata, g)
		case book.Content != nil:
			out[i] = similar.Cosine(book.Content, g)
		default:
			out[i] = similar.Cosine(book.Metadata, g)
		}
		mean += out[i]
	}
	mean /= float64(len(genres))
	for i := range out {
		out[i] -= mean
	}
	return out
}

// pickMargins takes the genres whose margin reaches their threshold, the
// largest first, at most limit, none of skip.
func pickMargins(margins []float64, threshold func(Genre) float64, skip []string, limit int) []Scored {
	var out []Scored
	for i, m := range margins {
		g := List[i]
		if m >= threshold(g) && !slices.Contains(skip, g.Key) {
			out = append(out, Scored{g.Key, m})
		}
	}
	return best(out, limit)
}

// Near is a neighbour of a book: how near, and its genres.
type Near struct {
	Score  float64
	Genres []string
}

// Shares are, per genre, the share of the book's first Neighbours
// neighbours with genres that have it, each weighted by how near it is;
// nil when fewer than MinNeighbours have any.
func Shares(near []Near) map[string]float64 {
	var with []Near
	for _, n := range near {
		if len(n.Genres) > 0 {
			with = append(with, n)
		}
		if len(with) == Neighbours {
			break
		}
	}
	if len(with) < MinNeighbours {
		return nil
	}
	var total float64
	out := map[string]float64{}
	for _, n := range with {
		w := max(n.Score, 0)
		total += w
		for _, g := range n.Genres {
			out[g] += w
		}
	}
	if total == 0 {
		return nil
	}
	for g := range out {
		out[g] /= total
	}
	return out
}

func pickShares(shares map[string]float64, threshold float64, skip []string, limit int) []Scored {
	var out []Scored
	for g, s := range shares {
		if s >= threshold && !slices.Contains(skip, g) {
			out = append(out, Scored{g, s})
		}
	}
	return best(out, limit)
}

// best is the scored genres, the best first (by key within a score), at
// most limit.
func best(in []Scored, limit int) []Scored {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Score != in[j].Score {
			return in[i].Score > in[j].Score
		}
		return in[i].Genre < in[j].Genre
	})
	if limit < 0 {
		limit = 0
	}
	return in[:min(len(in), limit)]
}

// knownGenres are the genres a book is known to have: those of its tags
// and those a person accepted, less those a person rejected.
func knownGenres(tags, accepted, rejected []string) []string {
	var out []string
	for _, g := range unique(append(FromTags(tags), accepted...)) {
		if !slices.Contains(rejected, g) {
			out = append(out, g)
		}
	}
	return out
}
