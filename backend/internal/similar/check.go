package similar

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// CheckMinBooks is how many books the author of a book a check asks about
// has at least: an author of one book has no neighbours of their own to
// tell anything by.
const CheckMinBooks = 5

// Report is what a check found about the similar books of a sample.
type Report struct {
	// Books is how many books were asked about, InSeries how many of them
	// are in a series, and Empty how many had no similar books.
	Books    int `json:"books"`
	InSeries int `json:"inSeries"`
	Empty    int `json:"empty"`
	// Neighbours is how many similar books there were in all.
	Neighbours int `json:"neighbours"`
	// SameAuthor and Copies are shares of the neighbours, SameSeries of
	// those of books in a series: by an author of the book, a copy of it
	// (the same title once a shop's additions are off, and an author in
	// common), from its series.
	SameAuthor float64 `json:"sameAuthor"`
	SameSeries float64 `json:"sameSeries"`
	Copies     float64 `json:"copies"`
	// Authors is how many authors the neighbours of a book have, on
	// average: the more, the more a list shows besides the book's own.
	Authors float64 `json:"authors"`
	// Samples are the books asked about with their neighbours.
	Samples []Sample `json:"samples,omitempty"`
}

// Sample is one book asked about and what came back.
type Sample struct {
	Title      string   `json:"title"`
	Neighbours []string `json:"neighbours"`
}

// Check asks for the similar books of up to n books, k each, as the book
// page does for someone who sees every library, and measures them. The
// books are those with text by an author of at least CheckMinBooks books,
// chosen by a hash of their ID: a library gives the same sample each time.
// It changes nothing.
func (s *Service) Check(ctx context.Context, n, k int) (Report, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return Report{}, err
	}
	q := sqlc.New(s.pool)
	sample, err := q.ListSimilarCheckSample(ctx, sqlc.ListSimilarCheckSampleParams{
		Model: spec.Model.Name, ModelVersion: Version(spec.Model), MinBooks: CheckMinBooks, Max: int32(n),
	})
	if err != nil {
		return Report{}, err
	}
	everything := library.Scope{SeesAll: true}
	var r Report
	var sameAuthor, sameSeries, seriesNeighbours, copies, authors int
	for _, id := range sample {
		near, err := s.Similar(ctx, everything, id, k)
		if err != nil {
			return Report{}, err
		}
		all, err := traitsOf(ctx, q, append([]uuid.UUID{id}, near...))
		if err != nil {
			return Report{}, err
		}
		book := all[id]
		r.Books++
		if book.series != nil {
			r.InSeries++
		}
		if len(near) == 0 {
			r.Empty++
		}
		seen := map[string]bool{}
		got := Sample{Title: book.title}
		for _, other := range near {
			t := all[other]
			got.Neighbours = append(got.Neighbours, t.title)
			r.Neighbours++
			if slices.ContainsFunc(t.people, func(p string) bool { return slices.Contains(book.people, p) }) {
				sameAuthor++
			}
			if isCopy(book, t) {
				copies++
			}
			if book.series != nil {
				seriesNeighbours++
				if t.series != nil && *t.series == *book.series {
					sameSeries++
				}
			}
			if len(t.people) > 0 {
				seen[t.people[0]] = true
			}
		}
		authors += len(seen)
		r.Samples = append(r.Samples, got)
	}
	r.SameAuthor = share(sameAuthor, r.Neighbours)
	r.SameSeries = share(sameSeries, seriesNeighbours)
	r.Copies = share(copies, r.Neighbours)
	r.Authors = share(authors, r.Books-r.Empty)
	return r, nil
}

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}
