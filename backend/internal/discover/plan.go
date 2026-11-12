package discover

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// What a person's run asks about at most: the providers are polite or
// limited, and the page shows the best of each anyway.
const (
	maxAuthors = 8
	maxSeries  = 8
	maxGenres  = 3
	// tasteBooks is how many of a person's best books the plan reads.
	tasteBooks = 300
	// minAuthorWeight is the least weight an author's best book has for
	// the author to be asked about: a book finished with no stars counts
	// 0.4, a rating under 3 stars not at all.
	minAuthorWeight = 0.4
)

func titleKey(s string) string  { return catalog.Key(s) }
func personKey(s string) string { return catalog.PersonKey(s) }

// ask is one subject a run asks the providers about.
type ask struct {
	kind string
	// name is what the providers are asked, key what the ask is told apart by.
	name, key string
	// Author: the best weight and rating of their books the person has.
	weight float64
	stars  int16
	// Series: where the person stands, and the places their libraries hold.
	lastRead float64
	held     []float64
	// Genre: the share of the person's taste it makes, 1 for the largest.
	share float64
}

// plan is what the scope's person is asked about: the series they read,
// then the authors they rated highest, then the genres they read most.
func (s *Service) plan(ctx context.Context, scope library.Scope) ([]ask, error) {
	q := sqlc.New(s.pool)
	books, err := q.ListDiscoverTaste(ctx, sqlc.ListDiscoverTasteParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, Max: tasteBooks})
	if err != nil {
		return nil, err
	}
	authors := map[string]*ask{}
	var order []string
	genreWeight := map[string]float64{}
	for _, b := range books {
		w := similar.DefaultTaste.Weight(b.Status, b.Rating)
		if w <= 0 {
			continue
		}
		for _, name := range b.Authors {
			key := personKey(name)
			if key == "" {
				continue
			}
			a, ok := authors[key]
			if !ok {
				a = &ask{kind: kindAuthor, name: metadata.NaturalName(name), key: key}
				authors[key] = a
				order = append(order, key)
			}
			a.weight = max(a.weight, w)
			if b.Rating != nil {
				a.stars = max(a.stars, *b.Rating)
			}
		}
		for _, g := range b.Genres {
			genreWeight[g] += w
		}
	}
	var out []ask
	series, err := q.ListDiscoverSeries(ctx, sqlc.ListDiscoverSeriesParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, Max: maxSeries})
	if err != nil {
		return nil, err
	}
	for _, r := range series {
		out = append(out, ask{kind: kindSeries, name: r.Name, key: r.NameKey, lastRead: r.LastRead, held: r.Held})
	}
	var best []*ask
	for _, key := range order {
		if a := authors[key]; a.weight >= minAuthorWeight {
			best = append(best, a)
		}
	}
	slices.SortStableFunc(best, func(a, b *ask) int {
		return cmp.Or(cmp.Compare(b.weight, a.weight), cmp.Compare(b.stars, a.stars), strings.Compare(a.key, b.key))
	})
	for _, a := range best[:min(maxAuthors, len(best))] {
		out = append(out, *a)
	}
	type shared struct {
		key    string
		weight float64
	}
	var gs []shared
	for key, w := range genreWeight {
		if _, ok := genres.Get(key); ok {
			gs = append(gs, shared{key, w})
		}
	}
	slices.SortFunc(gs, func(a, b shared) int {
		return cmp.Or(cmp.Compare(b.weight, a.weight), strings.Compare(a.key, b.key))
	})
	for _, g := range gs[:min(maxGenres, len(gs))] {
		out = append(out, ask{kind: kindGenre, name: subjectOf(g.key), key: g.key, share: g.weight / gs[0].weight})
	}
	return out, nil
}

const (
	kindAuthor = "author"
	kindSeries = "series"
	kindGenre  = "genre"
)

// subjects are the words providers file books of a genre under, where the
// genre's own name is not them.
var subjects = map[string]string{
	"crime":           "detective and mystery stories",
	"thriller":        "suspense",
	"dystopia":        "dystopias",
	"historical":      "historical fiction",
	"contemporary":    "fiction",
	"humour":          "humor",
	"children":        "children's stories",
	"young-adult":     "young adult fiction",
	"biography":       "biography",
	"science-fiction": "science fiction",
}

// subjectOf is what a genre is asked of the providers as.
func subjectOf(key string) string {
	if s, ok := subjects[key]; ok {
		return s
	}
	g, ok := genres.Get(key)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(g.English, " & ", " and "))
}
