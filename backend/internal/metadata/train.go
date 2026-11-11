package metadata

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// What a learning needs at least to be worth keeping: candidates that are
// the book, and books held out to measure on.
const (
	minRightCandidates = 30
	minHeldOutBooks    = 10
	// heldOutOf is the share of the books, one in so many, held out of the
	// fit and measured on.
	heldOutOf = 5
)

// ErrTooLittleToLearn is a sample with too few right candidates or too few
// books to hold some out.
var ErrTooLittleToLearn = errors.New("the books looked up do not give enough right candidates to learn from; look up more, or ask more providers")

// TrainOptions say what a learning looks up.
type TrainOptions struct {
	// Books is how many books with an ISBN to look up at most.
	Books int
	// Providers names those to ask; none asks those the settings enable.
	Providers []string
	// Threshold is metadata.matchThreshold, to mark in the report.
	Threshold float64
	Start     func(books int, estimate time.Duration)
	Progress  func(done, of int)
}

// WeightRow is one feature's weight, the hand-set one beside the learned.
type WeightRow struct {
	Feature string   `json:"feature"`
	HandSet *float64 `json:"handSet,omitempty"`
	Learned float64  `json:"learned"`
}

// TrainReport is what a learning found. Only numbers.
type TrainReport struct {
	Books      int      `json:"books"`
	Sample     int      `json:"sample"`
	TrainBooks int      `json:"trainBooks"`
	TestBooks  int      `json:"testBooks"`
	Candidates int      `json:"candidates"`
	Right      int      `json:"right"`
	Providers  []string `json:"providers"`
	Threshold  float64  `json:"threshold"`
	// LogLossTrain and LogLossTest are the mean log loss of the fit on the
	// books it was fitted on and on those held out.
	LogLossTrain float64 `json:"logLossTrain"`
	LogLossTest  float64 `json:"logLossTest"`
	// Weights are those kept: fitted on every book of the sample. The
	// measures below are of a fit on the books not held out, ranking the
	// held-out ones.
	Weights   []WeightRow        `json:"weights"`
	HandSet   Variant            `json:"handSet"`
	Learned   Variant            `json:"learned"`
	Languages map[string]float64 `json:"languages"`
	Shares    map[string]float64 `json:"providerShares"`
}

// heldOut says whether a book is one of those the fit is measured on.
func heldOut(id [16]byte) bool {
	h := fnv.New32a()
	h.Write(id[:])
	return h.Sum32()%heldOutOf == 0
}

// examplesOf are the candidates of the sampled books as labelled examples:
// right when the record carries one of the book's ISBNs.
func examplesOf(sample []*sampled) []example {
	var out []example
	for _, one := range sample {
		for _, a := range one.answers {
			if a.failed {
				continue
			}
			for i, r := range a.records {
				f := features(one.book.book, r, scoreInput{rank: i, cosine: a.cosines[i]})
				out = append(out, example{features: f, right: carriesISBN(r, one.book.isbns)})
			}
		}
	}
	return out
}

func carriesISBN(r Record, isbns []string) bool {
	for _, id := range r.Identifiers {
		if id.Type == "isbn" && slices.Contains(isbns, id.Value) {
			return true
		}
	}
	return false
}

// Train looks a sample of the books with an ISBN up without it, fits a
// logistic regression of whether a candidate carries the ISBN on the
// features Score uses, measures it on the books held out, and keeps the
// weights fitted on all of them. The requests are a match-check's, through
// the cache.
func (s *Service) Train(ctx context.Context, opts TrainOptions) (TrainReport, error) {
	if s.pool == nil {
		return TrainReport{}, errors.New("a match-train needs the database")
	}
	providers, err := s.checked(ctx, opts.Providers)
	if err != nil {
		return TrainReport{}, err
	}
	q := sqlc.New(s.pool)
	total, err := q.CountMatchCheckBooks(ctx)
	if err != nil {
		return TrainReport{}, err
	}
	rows, err := q.ListMatchCheckBooks(ctx, int32(max(opts.Books, 0)))
	if err != nil {
		return TrainReport{}, err
	}
	r := TrainReport{Books: int(total), Sample: len(rows), Threshold: opts.Threshold, Languages: map[string]float64{}, Shares: map[string]float64{}}
	for _, p := range providers {
		r.Providers = append(r.Providers, p.Name())
	}
	if opts.Start != nil {
		opts.Start(len(rows), CheckEstimate(providers, len(rows)))
	}
	sample, err := s.gather(ctx, providers, rows, opts.Progress)
	if err != nil {
		return TrainReport{}, err
	}
	var train, test []*sampled
	for _, one := range sample {
		if heldOut(one.book.book.ID) {
			test = append(test, one)
		} else {
			train = append(train, one)
		}
		r.Languages[languageOf(one.book.book.Language)]++
	}
	r.TrainBooks, r.TestBooks = len(train), len(test)
	all := examplesOf(sample)
	r.Candidates = len(all)
	byProvider := map[string]float64{}
	for _, one := range sample {
		for _, a := range one.answers {
			byProvider[a.provider] += float64(len(a.records))
		}
	}
	for _, e := range all {
		if e.right {
			r.Right++
		}
	}
	if r.Right < minRightCandidates || len(test) < minHeldOutBooks {
		return r, fmt.Errorf("%w (%d right of %d candidates, %d books held out)", ErrTooLittleToLearn, r.Right, r.Candidates, len(test))
	}
	for l := range r.Languages {
		r.Languages[l] = round2(r.Languages[l] / float64(len(sample)))
	}
	for p, n := range byProvider {
		r.Shares[p] = round2(n / float64(r.Candidates))
	}

	trainExamples := examplesOf(train)
	partial := fit(trainExamples)
	r.LogLossTrain, r.LogLossTest = partial.logLoss(trainExamples), partial.logLoss(examplesOf(test))
	for _, c := range []struct {
		dst   *Variant
		name  string
		score scorer
	}{{&r.HandSet, ScoreHandSet, handScorer}, {&r.Learned, ScoreLearned, learnedScorer(partial)}} {
		ev, err := s.evaluate(ctx, test, providers, c.score)
		if err != nil {
			return r, err
		}
		*c.dst = Variant{Name: c.name, All: ev.all}
		c.dst.Scores, c.dst.Thresholds, c.dst.Calibration = bandsOf(ev.firsts, opts.Threshold)
	}

	final := fit(all)
	for name, w := range final {
		row := WeightRow{Feature: name, Learned: w}
		if h, ok := handSet[name]; ok {
			row.HandSet = &h
		}
		r.Weights = append(r.Weights, row)
	}
	for name, h := range handSet {
		if _, ok := final[name]; !ok {
			r.Weights = append(r.Weights, WeightRow{Feature: name, HandSet: &h})
		}
	}
	slices.SortFunc(r.Weights, func(a, b WeightRow) int {
		return strings.Compare(a.Feature, b.Feature)
	})
	if err := s.saveProfile(ctx, slotLearned, newProfile(final, len(sample), r.Languages, r.Shares)); err != nil {
		return r, err
	}
	return r, nil
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
