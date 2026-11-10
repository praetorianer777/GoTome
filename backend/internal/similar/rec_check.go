package similar

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// RecommendationConfig is a way of weighing and grouping a person's taste
// that a check tries.
type RecommendationConfig struct {
	Name  string `json:"name"`
	Taste Taste  `json:"-"`
}

// RecommendationResult is how a configuration did: of the books of 4 and 5
// stars hidden from the profile, the share that came back among the first
// 10 and the first 20 recommendations.
type RecommendationResult struct {
	Name     string  `json:"name"`
	Hidden   int     `json:"hidden"`
	Recall10 float64 `json:"recall10"`
	Recall20 float64 `json:"recall20"`
}

// RecommendationReport is what a check of the recommendations found.
type RecommendationReport struct {
	// People is how many had enough 4 and 5 star books, and Hidden how
	// many of those books were hidden from their profiles in all.
	People  int                    `json:"people"`
	Hidden  int                    `json:"hidden"`
	Results []RecommendationResult `json:"results"`
}

// RecommendationConfigs are the ways a check compares: each count of
// centres, with books of one or two stars left out and counting against.
func RecommendationConfigs() []RecommendationConfig {
	var out []RecommendationConfig
	for _, low := range []float64{0, -0.3} {
		for k := 1; k <= 4; k++ {
			t := DefaultTaste
			t.Low, t.Centres = low, k
			name := fmt.Sprintf("%d centres, 1-2 stars left out", k)
			if low < 0 {
				name = fmt.Sprintf("%d centres, 1-2 stars count against", k)
			}
			out = append(out, RecommendationConfig{Name: name, Taste: t})
		}
	}
	return out
}

// hiddenShare is the one in this many liked books a check hides from the
// profile it builds.
const hiddenShare = 5

// CheckRecommendations measures the recommendations on the library's own
// ratings. For each person with at least minBooks books of 4 or 5 stars it
// hides about a fifth of them, builds the profile from the rest, and
// counts how many of the hidden ones land in the first 10 and 20
// recommendations, for each configuration. The books hidden are chosen by
// a hash of person and book, so a library gives the same split each time.
// It changes nothing.
func (s *Service) CheckRecommendations(ctx context.Context, minBooks int) (RecommendationReport, error) {
	spec, _, err := s.config(ctx)
	if err != nil {
		return RecommendationReport{}, err
	}
	people, err := sqlc.New(s.pool).ListRecommendationCheckUsers(ctx, sqlc.ListRecommendationCheckUsersParams{
		Model: spec.Model.Name, ModelVersion: Version(spec.Model), MinBooks: int32(max(minBooks, hiddenShare)),
	})
	if err != nil {
		return RecommendationReport{}, err
	}
	configs := RecommendationConfigs()
	type tally struct{ hidden, at10, at20 int }
	tallies := make([]tally, len(configs))
	var report RecommendationReport
	for _, person := range people {
		hidden := map[uuid.UUID]bool{}
		for _, b := range person.Books {
			h := fnv.New32a()
			h.Write(person.UserID[:])
			h.Write(b[:])
			if h.Sum32()%hiddenShare == 0 {
				hidden[b] = true
			}
		}
		if len(hidden) == 0 || len(hidden) == len(person.Books) {
			continue
		}
		scope := library.Scope{Viewer: person.UserID, SeesAll: person.SeesAll}
		keep := make([]uuid.UUID, 0, len(hidden))
		for b := range hidden {
			keep = append(keep, b)
		}
		slices.SortFunc(keep, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		counted := false
		for i, cfg := range configs {
			books, err := s.taste(ctx, scope, spec, cfg.Taste)
			if err != nil {
				return RecommendationReport{}, err
			}
			books = slices.DeleteFunc(books, func(b liked) bool { return hidden[b.id] })
			profile, ok := buildProfile(books, cfg.Taste)
			if !ok {
				continue
			}
			recs, err := s.recommend(ctx, scope, spec, profile, 20, keep, false)
			if err != nil {
				return RecommendationReport{}, err
			}
			for rank, r := range recs {
				if !hidden[r.BookID] {
					continue
				}
				tallies[i].at20++
				if rank < 10 {
					tallies[i].at10++
				}
			}
			tallies[i].hidden += len(hidden)
			counted = true
		}
		if counted {
			report.People++
			report.Hidden += len(hidden)
		}
	}
	for i, cfg := range configs {
		r := RecommendationResult{Name: cfg.Name, Hidden: tallies[i].hidden}
		if r.Hidden > 0 {
			r.Recall10 = float64(tallies[i].at10) / float64(r.Hidden)
			r.Recall20 = float64(tallies[i].at20) / float64(r.Hidden)
		}
		report.Results = append(report.Results, r)
	}
	return report, nil
}
