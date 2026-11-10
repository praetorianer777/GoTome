package metadata

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// What a ranking's first candidate is, for a book whose ISBNs are known.
const (
	// OutcomeRight carries one of the book's ISBNs.
	OutcomeRight = "right"
	// OutcomeSameWork is another edition of the book: the provider says
	// the record and the book's ISBN belong to one work.
	OutcomeSameWork = "sameWork"
	OutcomeWrong    = "wrong"
	// OutcomeNone is a ranking without any candidate.
	OutcomeNone = "none"
)

// Languages the check counts apart; the rest are LanguageOther.
const (
	LanguageGerman  = "de"
	LanguageEnglish = "en"
	LanguageOther   = "other"
	// CheckAll names the ranking of every provider's candidates together,
	// as a lookup ranks them.
	CheckAll = "all"
)

// checkRequestsPerBook is about how many requests a book costs a
// provider: a search, and where its first candidate is not the book, a
// lookup of the book's ISBNs, which for some providers is a few.
const checkRequestsPerBook = 4

// CheckOptions say what a match-check asks.
type CheckOptions struct {
	// Books is how many books with an ISBN to look up at most.
	Books int
	// Providers names those to ask; none asks those the settings enable.
	Providers []string
	// Threshold is metadata.matchThreshold as it is set now.
	Threshold float64
	// Start is told, before the first request, how many books will be
	// looked up and about how long that takes at the providers' pace.
	Start func(books int, estimate time.Duration)
	// Progress is told after each book.
	Progress func(done, of int)
}

// CheckReport is what a match-check found.
type CheckReport struct {
	// Books is every book with an ISBN; Sample those looked up.
	Books     int      `json:"books"`
	Sample    int      `json:"sample"`
	Providers []string `json:"providers"`
	Threshold float64  `json:"threshold"`
	// All is the ranking of every provider's candidates together.
	All         Tally   `json:"all"`
	PerProvider []Tally `json:"perProvider"`
	// PerLanguage is All by the book's language.
	PerLanguage []Tally `json:"perLanguage"`
	// Scores is All's first candidates by their score.
	Scores []ScoreBand `json:"scores"`
	// Thresholds is what All would take at each threshold.
	Thresholds []ThresholdPoint `json:"thresholds"`
	// Agreement is what the rule of an automatic run takes: books whose
	// providers' first candidates name one ISBN or one work.
	Agreement Agreement `json:"agreement"`
}

// Agreement counts the sampled books for which two providers' first
// candidates agree, which an automatic run trusts without a person, and of
// those how many are the book (carry one of its ISBNs), another edition of
// it (of its work) or wrong. Titles are never kept.
type Agreement struct {
	Accepted int `json:"accepted"`
	Right    int `json:"right"`
	SameWork int `json:"sameWork"`
	Wrong    int `json:"wrong"`
}

// Agrees says whether two candidates of different providers name one ISBN or
// one work, which is when an automatic run trusts them.
func Agrees(a, b Candidate) bool {
	if a.Provider == b.Provider {
		return false
	}
	for _, x := range a.Identifiers {
		if x.Type == catalog.IDISBN && slices.Contains(b.Identifiers, x) {
			return true
		}
	}
	return a.Work != "" && a.Work == b.Work
}

// Tally counts what one ranking put first, a book each.
type Tally struct {
	Name     string `json:"name"`
	Books    int    `json:"books"`
	Right    int    `json:"right"`
	SameWork int    `json:"sameWork"`
	Wrong    int    `json:"wrong"`
	None     int    `json:"none"`
	// Failed are the books a provider failed to answer about, counted
	// nowhere else.
	Failed int `json:"failed"`
	// Ranks counts, of the books whose first candidate is wrong, the
	// place of the first that is right or of the same work; Missing those
	// with no such candidate at all.
	Ranks   map[int]int `json:"ranks"`
	Missing int         `json:"missing"`
}

// ScoreBand counts the first candidates scored from From up to To.
type ScoreBand struct {
	From     float64 `json:"from"`
	To       float64 `json:"to"`
	Right    int     `json:"right"`
	SameWork int     `json:"sameWork"`
	Wrong    int     `json:"wrong"`
}

// ThresholdPoint counts the first candidates scored at or above a
// threshold, those a lookup would take.
type ThresholdPoint struct {
	Threshold float64 `json:"threshold"`
	Taken     int     `json:"taken"`
	Right     int     `json:"right"`
	SameWork  int     `json:"sameWork"`
	Wrong     int     `json:"wrong"`
}

func (t *Tally) add(outcome string, rank int) {
	t.Books++
	switch outcome {
	case OutcomeRight:
		t.Right++
	case OutcomeSameWork:
		t.SameWork++
	case OutcomeNone:
		t.None++
	case OutcomeWrong:
		t.Wrong++
		if rank == 0 {
			t.Missing++
		} else {
			t.Ranks[rank]++
		}
	}
}

func newTally(name string) Tally { return Tally{Name: name, Ranks: map[int]int{}} }

// checkBook is a book of the sample as a lookup sees it without its ISBNs,
// and those ISBNs.
type checkBook struct {
	book  catalog.Book
	isbns []string
}

// Check looks up a sample of the books that have an ISBN as if they had
// none, by title, authors and language, and counts how often the first
// candidate is the book. The sample is chosen by a hash of the book's ID,
// so a library gives the same one each time. Requests go through the
// providers' Webs, spaced and kept as any lookup's, so a second run asks
// little. It reads the count of books with an ISBN and the sample's
// names, and writes only to the answer cache.
func (s *Service) Check(ctx context.Context, opts CheckOptions) (CheckReport, error) {
	if s.pool == nil {
		return CheckReport{}, errors.New("a match-check needs the database")
	}
	providers, err := s.checked(ctx, opts.Providers)
	if err != nil {
		return CheckReport{}, err
	}
	q := sqlc.New(s.pool)
	total, err := q.CountMatchCheckBooks(ctx)
	if err != nil {
		return CheckReport{}, err
	}
	rows, err := q.ListMatchCheckBooks(ctx, int32(max(opts.Books, 0)))
	if err != nil {
		return CheckReport{}, err
	}
	r := CheckReport{Books: int(total), Sample: len(rows), Threshold: opts.Threshold, All: newTally(CheckAll)}
	perProvider := map[string]*Tally{}
	for _, p := range providers {
		r.Providers = append(r.Providers, p.Name())
		t := newTally(p.Name())
		perProvider[p.Name()] = &t
	}
	perLanguage := map[string]*Tally{}
	for _, l := range []string{LanguageGerman, LanguageEnglish, LanguageOther} {
		t := newTally(l)
		perLanguage[l] = &t
	}
	if opts.Start != nil {
		opts.Start(len(rows), CheckEstimate(providers, len(rows)))
	}
	type first struct {
		score   float64
		outcome string
	}
	var firsts []first
	for i, row := range rows {
		b := sampleBook(row)
		query := Query{Title: b.book.Title, Authors: b.book.Authors(), Language: b.book.Language}
		if query.Language == "" && s.language != nil {
			query.Language = s.language(ctx)
		}
		works := map[string]map[string]bool{}
		workOf := func(provider string) (map[string]bool, error) {
			if w, ok := works[provider]; ok {
				return w, nil
			}
			w, err := s.worksOf(ctx, provider, b.isbns)
			if err != nil {
				return nil, err
			}
			works[provider] = w
			return w, nil
		}
		var all, firstOf []Candidate
		var firstOutcome []string
		failed := false
		for _, p := range providers {
			tally := perProvider[p.Name()]
			ranked, err := s.searchRanked(ctx, p, b.book, query)
			if err == nil {
				all = append(all, ranked...)
				var outcome string
				var rank int
				outcome, rank, err = judge(ranked, b.isbns, workOf)
				if err == nil {
					tally.add(outcome, rank)
					if len(ranked) > 0 {
						firstOf, firstOutcome = append(firstOf, ranked[0]), append(firstOutcome, outcome)
					}
					continue
				}
			}
			if ctx.Err() != nil {
				return CheckReport{}, ctx.Err()
			}
			tally.Books++
			tally.Failed++
			failed = true
		}
		lang := perLanguage[languageOf(b.book.Language)]
		outcome, rank, err := judge(sortCandidates(all), b.isbns, workOf)
		if err != nil && ctx.Err() != nil {
			return CheckReport{}, ctx.Err()
		}
		if failed || err != nil {
			r.All.Books++
			r.All.Failed++
			lang.Books++
			lang.Failed++
		} else {
			r.All.add(outcome, rank)
			lang.add(outcome, rank)
			if outcome != OutcomeNone {
				firsts = append(firsts, first{all[0].Score, outcome})
			}
		}
		if !failed {
			accepted := ""
			for x := range firstOf {
				for y := x + 1; y < len(firstOf); y++ {
					if !Agrees(firstOf[x], firstOf[y]) {
						continue
					}
					// The better of the two stands for the book.
					for _, o := range []string{firstOutcome[x], firstOutcome[y]} {
						if accepted == "" || o == OutcomeRight || o == OutcomeSameWork && accepted == OutcomeWrong {
							accepted = o
						}
					}
				}
			}
			if accepted != "" {
				r.Agreement.Accepted++
				switch accepted {
				case OutcomeRight:
					r.Agreement.Right++
				case OutcomeSameWork:
					r.Agreement.SameWork++
				default:
					r.Agreement.Wrong++
				}
			}
		}
		if opts.Progress != nil {
			opts.Progress(i+1, len(rows))
		}
	}
	for _, name := range r.Providers {
		r.PerProvider = append(r.PerProvider, *perProvider[name])
	}
	for _, l := range []string{LanguageGerman, LanguageEnglish, LanguageOther} {
		r.PerLanguage = append(r.PerLanguage, *perLanguage[l])
	}

	count := func(outcome string, right, same, wrong *int) {
		switch outcome {
		case OutcomeRight:
			*right++
		case OutcomeSameWork:
			*same++
		default:
			*wrong++
		}
	}
	// In hundredths, so that the bands' edges are exact.
	edges := []int{0, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100}
	for i := 0; i+1 < len(edges); i++ {
		band := ScoreBand{From: float64(edges[i]) / 100, To: float64(edges[i+1]) / 100}
		for _, f := range firsts {
			hundredths := int(f.score*100 + 1e-9)
			if hundredths >= edges[i] && (hundredths < edges[i+1] || edges[i+1] == 100) {
				count(f.outcome, &band.Right, &band.SameWork, &band.Wrong)
			}
		}
		r.Scores = append(r.Scores, band)
	}
	steps := []float64{0.5, 0.55, 0.6, 0.65, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 0.99}
	if opts.Threshold > 0 && !slices.Contains(steps, opts.Threshold) {
		steps = append(steps, opts.Threshold)
		slices.Sort(steps)
	}
	for _, t := range steps {
		p := ThresholdPoint{Threshold: t}
		for _, f := range firsts {
			if f.score >= t-1e-9 {
				p.Taken++
				count(f.outcome, &p.Right, &p.SameWork, &p.Wrong)
			}
		}
		r.Thresholds = append(r.Thresholds, p)
	}
	return r, nil
}

// CheckEstimate is about how long a match-check of that many books takes
// when nothing it asks is in the cache.
func CheckEstimate(providers []Provider, books int) time.Duration {
	var perBook time.Duration
	for _, p := range providers {
		perBook += checkRequestsPerBook * p.Limits().Interval
	}
	return time.Duration(books) * perBook
}

// checked are the providers a check asks: those named, or else those the
// settings enable.
func (s *Service) checked(ctx context.Context, names []string) ([]Provider, error) {
	var out []Provider
	if len(names) > 0 {
		for _, name := range names {
			i := slices.IndexFunc(s.providers, func(p Provider) bool { return p.Name() == name })
			if i < 0 {
				var known []string
				for _, p := range s.providers {
					known = append(known, p.Name())
				}
				return nil, fmt.Errorf("no provider %q; there are: %s", name, strings.Join(known, ", "))
			}
			out = append(out, s.providers[i])
		}
		return out, nil
	}
	var enabled []string
	if s.enabled != nil {
		enabled = s.enabled(ctx)
	}
	for _, p := range s.providers {
		if s.enabled == nil || slices.Contains(enabled, p.Name()) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no provider to ask")
	}
	return out, nil
}

// searchRanked is what one provider's search finds for the book, best
// first.
func (s *Service) searchRanked(ctx context.Context, p Provider, book catalog.Book, q Query) ([]Candidate, error) {
	records, err := p.Search(ctx, s.webs[p.Name()], q)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var out []Candidate
	for _, r := range records {
		r.Provider = p.Name()
		out = append(out, Candidate{Record: r, Score: Score(book, r)})
	}
	return sortCandidates(out), nil
}

func sortCandidates(c []Candidate) []Candidate {
	slices.SortStableFunc(c, func(a, b Candidate) int { return cmp.Compare(b.Score, a.Score) })
	return c
}

// worksOf are the works a provider says the ISBNs belong to, by looking
// them up as Candidates does.
func (s *Service) worksOf(ctx context.Context, provider string, isbns []string) (map[string]bool, error) {
	i := slices.IndexFunc(s.providers, func(p Provider) bool { return p.Name() == provider })
	if i < 0 {
		return nil, fmt.Errorf("no provider %q", provider)
	}
	works := map[string]bool{}
	for _, isbn := range isbns[:min(len(isbns), maxLookups)] {
		records, err := s.providers[i].Lookup(ctx, s.webs[provider], catalog.Identifier{Type: catalog.IDISBN, Value: isbn})
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		for _, r := range records {
			if r.Work != "" {
				works[r.Work] = true
			}
		}
	}
	return works, nil
}

// judge says what a ranking put first and, where that is wrong, at which
// place, from 1, the first candidate that is the book or of its work
// stands; 0 for none. It asks for a provider's works only when a
// candidate of it does not carry one of the ISBNs.
func judge(ranked []Candidate, isbns []string, workOf func(string) (map[string]bool, error)) (string, int, error) {
	if len(ranked) == 0 {
		return OutcomeNone, 0, nil
	}
	classify := func(c Candidate) (string, error) {
		for _, id := range c.Identifiers {
			if id.Type == catalog.IDISBN && slices.Contains(isbns, id.Value) {
				return OutcomeRight, nil
			}
		}
		if c.Work == "" {
			return OutcomeWrong, nil
		}
		works, err := workOf(c.Provider)
		if err != nil {
			return "", err
		}
		if works[c.Work] {
			return OutcomeSameWork, nil
		}
		return OutcomeWrong, nil
	}
	outcome, err := classify(ranked[0])
	if err != nil || outcome != OutcomeWrong {
		return outcome, 0, err
	}
	for i, c := range ranked[1:] {
		o, err := classify(c)
		if err != nil {
			return "", 0, err
		}
		if o != OutcomeWrong {
			return OutcomeWrong, i + 2, nil
		}
	}
	return OutcomeWrong, 0, nil
}

func sampleBook(row sqlc.ListMatchCheckBooksRow) checkBook {
	b := catalog.Book{ID: row.ID, Title: row.Title, PublishedOn: row.PublishedOn}
	if row.Subtitle != nil {
		b.Subtitle = *row.Subtitle
	}
	if row.Language != nil {
		b.Language = *row.Language
	}
	if row.PublishedPrecision != nil {
		b.PublishedPrecision = *row.PublishedPrecision
	}
	for _, name := range row.Authors {
		b.Contributors = append(b.Contributors, catalog.Contributor{Name: name, Role: catalog.RoleAuthor})
	}
	return checkBook{book: b, isbns: row.Isbns}
}

func languageOf(tag string) string {
	switch l := baseLanguage(tag); l {
	case LanguageGerman, LanguageEnglish:
		return l
	}
	return LanguageOther
}
