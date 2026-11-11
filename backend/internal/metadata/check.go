package metadata

import (
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
	// OutcomeEdition is a record of the book's title and an author of it
	// that names neither its ISBN nor its work: most often another
	// edition, as for a provider that names no works (the DNB).
	OutcomeEdition = "edition"
	OutcomeWrong   = "wrong"
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
	// Editions is what an automatic run takes with
	// curation.matches.edition on as well: books one of whose providers'
	// first candidates is SameEdition, where no two agree.
	Editions Agreement `json:"editions"`
	// Using names the score the fields above are of: handSet, learned,
	// profile:<name> or imported.
	Using string `json:"using"`
	// Variants are the same sample ranked by each score there is: the
	// hand-set one, and any learned, shipped or imported one.
	Variants []Variant `json:"variants"`
}

// Variant is a sample ranked by one score.
type Variant struct {
	Name        string            `json:"name"`
	All         Tally             `json:"all"`
	Scores      []ScoreBand       `json:"scores"`
	Thresholds  []ThresholdPoint  `json:"thresholds"`
	Calibration []CalibrationBand `json:"calibration"`
}

// CalibrationBand says, of the first candidates a score put between From
// and To, how many there are, the mean score (what it predicts) and the
// share that carries one of the book's ISBNs (what was found).
type CalibrationBand struct {
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Count     int     `json:"count"`
	Right     int     `json:"right"`
	Predicted float64 `json:"predicted"`
	Found     float64 `json:"found"`
}

// Agreement counts the sampled books for which two providers' first
// candidates agree, which an automatic run trusts without a person, and of
// those how many are the book (carry one of its ISBNs), another edition of
// it (of its work) or wrong. Titles are never kept.
type Agreement struct {
	Accepted int `json:"accepted"`
	Right    int `json:"right"`
	SameWork int `json:"sameWork"`
	Edition  int `json:"edition"`
	Wrong    int `json:"wrong"`
}

func (a *Agreement) add(outcome string) {
	a.Accepted++
	switch outcome {
	case OutcomeRight:
		a.Right++
	case OutcomeSameWork:
		a.SameWork++
	case OutcomeEdition:
		a.Edition++
	default:
		a.Wrong++
	}
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
	Edition  int    `json:"edition"`
	Wrong    int    `json:"wrong"`
	None     int    `json:"none"`
	// Failed are the books a provider failed to answer about, counted
	// nowhere else.
	Failed int `json:"failed"`
	// Ranks counts, of the books whose first candidate is wrong or another
	// edition, the place of the first that is right or of the same work;
	// Missing those with no such candidate at all.
	Ranks   map[int]int `json:"ranks"`
	Missing int         `json:"missing"`
}

// ScoreBand counts the first candidates scored from From up to To.
type ScoreBand struct {
	From     float64 `json:"from"`
	To       float64 `json:"to"`
	Right    int     `json:"right"`
	SameWork int     `json:"sameWork"`
	Edition  int     `json:"edition"`
	Wrong    int     `json:"wrong"`
}

// ThresholdPoint counts the first candidates scored at or above a
// threshold, those a lookup would take.
type ThresholdPoint struct {
	Threshold float64 `json:"threshold"`
	Taken     int     `json:"taken"`
	Right     int     `json:"right"`
	SameWork  int     `json:"sameWork"`
	Edition   int     `json:"edition"`
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
	case OutcomeWrong, OutcomeEdition:
		if outcome == OutcomeWrong {
			t.Wrong++
		} else {
			t.Edition++
		}
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

// providerAnswer is what one provider found for a book of the sample, in
// its own order.
type providerAnswer struct {
	provider string
	records  []Record
	// cosines are how near each record's description is to the book's text,
	// where that was worked out (#491).
	cosines []*float64
	failed  bool
}

// sampled is a book of the sample with what the providers found for it.
type sampled struct {
	book    checkBook
	answers []providerAnswer
	works   map[string]map[string]bool
}

// gather looks the sample up, the only part of a check that asks anybody.
func (s *Service) gather(ctx context.Context, providers []Provider, rows []sqlc.ListMatchCheckBooksRow, progress func(done, of int)) ([]*sampled, error) {
	var out []*sampled
	for i, row := range rows {
		b := sampleBook(row)
		query := Query{Title: b.book.Title, Authors: b.book.Authors(), Language: b.book.Language}
		if query.Language == "" && s.language != nil {
			query.Language = s.language(ctx)
		}
		one := &sampled{book: b, works: map[string]map[string]bool{}}
		for _, p := range providers {
			answer := providerAnswer{provider: p.Name()}
			records, err := p.Search(ctx, s.webs[p.Name()], query)
			if err != nil && !errors.Is(err, ErrNotFound) {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				answer.failed = true
			}
			for _, r := range records {
				r.Provider = p.Name()
				answer.records = append(answer.records, r)
			}
			answer.cosines = s.describe(ctx, b.book, answer.records)
			one.answers = append(one.answers, answer)
		}
		out = append(out, one)
		if progress != nil {
			progress(i+1, len(rows))
		}
	}
	return out, nil
}

// workOf are the works a provider says the book's ISBNs belong to, asked
// once.
func (s *Service) workOf(ctx context.Context, one *sampled, provider string) (map[string]bool, error) {
	if w, ok := one.works[provider]; ok {
		return w, nil
	}
	w, err := s.worksOf(ctx, provider, one.book.isbns)
	if err != nil {
		return nil, err
	}
	one.works[provider] = w
	return w, nil
}

// rankAnswer scores a provider's records with a scorer, best first.
func rankAnswer(book catalog.Book, a providerAnswer, score scorer) []Candidate {
	out := make([]Candidate, 0, len(a.records))
	for i, r := range a.records {
		in := scoreInput{rank: i, cosine: a.cosines[i]}
		out = append(out, Candidate{Record: r, Score: score(book, r, in)})
	}
	return sortCandidates(out, book.Language)
}

// evaluation is what a scorer makes of a sample.
type evaluation struct {
	all         Tally
	perProvider []Tally
	perLanguage []Tally
	agreement   Agreement
	editions    Agreement
	firsts      []first
}

// first is the outcome of a ranking's first candidate and its score.
type first struct {
	score   float64
	outcome string
}

// evaluate ranks the sample with a scorer and counts what comes first.
// It asks only for the works of providers, which the cache remembers.
func (s *Service) evaluate(ctx context.Context, sample []*sampled, providers []Provider, score scorer) (evaluation, error) {
	ev := evaluation{all: newTally(CheckAll)}
	perProvider := map[string]*Tally{}
	for _, p := range providers {
		t := newTally(p.Name())
		perProvider[p.Name()] = &t
	}
	perLanguage := map[string]*Tally{}
	for _, l := range []string{LanguageGerman, LanguageEnglish, LanguageOther} {
		t := newTally(l)
		perLanguage[l] = &t
	}
	for _, one := range sample {
		b := one.book
		workOf := func(provider string) (map[string]bool, error) { return s.workOf(ctx, one, provider) }
		var all, firstOf []Candidate
		var firstOutcome []string
		failed := false
		for _, a := range one.answers {
			tally := perProvider[a.provider]
			if !a.failed {
				ranked := rankAnswer(b.book, a, score)
				all = append(all, ranked...)
				outcome, rank, err := judge(ranked, b, workOf)
				if err == nil {
					tally.add(outcome, rank)
					if len(ranked) > 0 {
						firstOf, firstOutcome = append(firstOf, ranked[0]), append(firstOutcome, outcome)
					}
					continue
				}
			}
			if ctx.Err() != nil {
				return evaluation{}, ctx.Err()
			}
			tally.Books++
			tally.Failed++
			failed = true
		}
		lang := perLanguage[languageOf(b.book.Language)]
		all = sortCandidates(all, b.book.Language)
		outcome, rank, err := judge(all, b, workOf)
		if err != nil && ctx.Err() != nil {
			return evaluation{}, ctx.Err()
		}
		if failed || err != nil {
			ev.all.Books++
			ev.all.Failed++
			lang.Books++
			lang.Failed++
		} else {
			ev.all.add(outcome, rank)
			lang.add(outcome, rank)
			if outcome != OutcomeNone {
				ev.firsts = append(ev.firsts, first{all[0].Score, outcome})
			}
		}
		if failed {
			continue
		}
		accepted := ""
		for x := range firstOf {
			for y := x + 1; y < len(firstOf); y++ {
				if !Agrees(firstOf[x], firstOf[y]) {
					continue
				}
				// The better of the two stands for the book.
				for _, o := range []string{firstOutcome[x], firstOutcome[y]} {
					if accepted == "" || outcomeRank[o] > outcomeRank[accepted] {
						accepted = o
					}
				}
			}
		}
		if accepted != "" {
			ev.agreement.add(accepted)
			continue
		}
		edition := ""
		for x, c := range firstOf {
			if SameEdition(b.book, c.Record) && (edition == "" || outcomeRank[firstOutcome[x]] < outcomeRank[edition]) {
				// The worse of two stands: either might be taken.
				edition = firstOutcome[x]
			}
		}
		if edition != "" {
			ev.editions.add(edition)
		}
	}
	for _, p := range providers {
		ev.perProvider = append(ev.perProvider, *perProvider[p.Name()])
	}
	for _, l := range []string{LanguageGerman, LanguageEnglish, LanguageOther} {
		ev.perLanguage = append(ev.perLanguage, *perLanguage[l])
	}
	return ev, nil
}

// Check looks up a sample of the books that have an ISBN as if they had
// none, by title, authors and language, and counts how often the first
// candidate is the book. The sample is chosen by a hash of the book's ID,
// so a library gives the same one each time. Requests go through the
// providers' Webs, spaced and kept as any lookup's, so a second run asks
// little. It reads the count of books with an ISBN and the sample's
// names, and writes only to the answer cache. The report is of the score
// in use, and beside it of the hand-set and each learned one.
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
	r := CheckReport{Books: int(total), Sample: len(rows), Threshold: opts.Threshold}
	for _, p := range providers {
		r.Providers = append(r.Providers, p.Name())
	}
	if opts.Start != nil {
		opts.Start(len(rows), CheckEstimate(providers, len(rows)))
	}
	sample, err := s.gather(ctx, providers, rows, opts.Progress)
	if err != nil {
		return CheckReport{}, err
	}
	scorers, err := s.scorers(ctx)
	if err != nil {
		return CheckReport{}, err
	}
	r.Using = scorers.using
	for _, c := range scorers.all {
		ev, err := s.evaluate(ctx, sample, providers, c.score)
		if err != nil {
			return CheckReport{}, err
		}
		v := Variant{Name: c.name, All: ev.all}
		v.Scores, v.Thresholds, v.Calibration = bandsOf(ev.firsts, opts.Threshold)
		r.Variants = append(r.Variants, v)
		if c.name != scorers.using {
			continue
		}
		r.All, r.PerProvider, r.PerLanguage = ev.all, ev.perProvider, ev.perLanguage
		r.Agreement, r.Editions = ev.agreement, ev.editions
		r.Scores, r.Thresholds = v.Scores, v.Thresholds
	}
	return r, nil
}

func countOutcome(outcome string, right, same, edition, wrong *int) {
	switch outcome {
	case OutcomeRight:
		*right++
	case OutcomeSameWork:
		*same++
	case OutcomeEdition:
		*edition++
	default:
		*wrong++
	}
}

// bandsOf counts the first candidates by score, by what each threshold
// takes, and by how often each band's candidates are the book against the
// score it predicts.
func bandsOf(firsts []first, threshold float64) ([]ScoreBand, []ThresholdPoint, []CalibrationBand) {
	// In hundredths, so that the bands' edges are exact.
	edges := []int{0, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100}
	var bands []ScoreBand
	for i := 0; i+1 < len(edges); i++ {
		band := ScoreBand{From: float64(edges[i]) / 100, To: float64(edges[i+1]) / 100}
		for _, f := range firsts {
			hundredths := int(f.score*100 + 1e-9)
			if hundredths >= edges[i] && (hundredths < edges[i+1] || edges[i+1] == 100) {
				countOutcome(f.outcome, &band.Right, &band.SameWork, &band.Edition, &band.Wrong)
			}
		}
		bands = append(bands, band)
	}
	steps := []float64{0.5, 0.55, 0.6, 0.65, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 0.99}
	if threshold > 0 && !slices.Contains(steps, threshold) {
		steps = append(steps, threshold)
		slices.Sort(steps)
	}
	var points []ThresholdPoint
	for _, t := range steps {
		p := ThresholdPoint{Threshold: t}
		for _, f := range firsts {
			if f.score >= t-1e-9 {
				p.Taken++
				countOutcome(f.outcome, &p.Right, &p.SameWork, &p.Edition, &p.Wrong)
			}
		}
		points = append(points, p)
	}
	return bands, points, calibration(firsts)
}

// calibration groups the first candidates by score and says, of each
// group, the mean score and the share that is the book.
func calibration(firsts []first) []CalibrationBand {
	edges := []int{0, 50, 60, 70, 80, 90, 95, 100}
	var out []CalibrationBand
	for i := 0; i+1 < len(edges); i++ {
		band := CalibrationBand{From: float64(edges[i]) / 100, To: float64(edges[i+1]) / 100}
		sum := 0.0
		for _, f := range firsts {
			hundredths := int(f.score*100 + 1e-9)
			if hundredths >= edges[i] && (hundredths < edges[i+1] || edges[i+1] == 100) {
				band.Count++
				sum += f.score
				if f.outcome == OutcomeRight {
					band.Right++
				}
			}
		}
		if band.Count > 0 {
			band.Predicted = sum / float64(band.Count)
			band.Found = float64(band.Right) / float64(band.Count)
		}
		out = append(out, band)
	}
	return out
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

// outcomeRank orders outcomes from the book itself down to a wrong one.
var outcomeRank = map[string]int{OutcomeWrong: 1, OutcomeEdition: 2, OutcomeSameWork: 3, OutcomeRight: 4}

// judge says what a ranking put first and, where that is not the book or
// of its work, at which place, from 1, the first candidate that is stands;
// 0 for none. It asks for a provider's works only when a candidate of it
// does not carry one of the ISBNs.
func judge(ranked []Candidate, b checkBook, workOf func(string) (map[string]bool, error)) (string, int, error) {
	if len(ranked) == 0 {
		return OutcomeNone, 0, nil
	}
	classify := func(c Candidate) (string, error) {
		for _, id := range c.Identifiers {
			if id.Type == catalog.IDISBN && slices.Contains(b.isbns, id.Value) {
				return OutcomeRight, nil
			}
		}
		if c.Work != "" {
			works, err := workOf(c.Provider)
			if err != nil {
				return "", err
			}
			if works[c.Work] {
				return OutcomeSameWork, nil
			}
		}
		if SameBook(b.book, c.Record) {
			return OutcomeEdition, nil
		}
		return OutcomeWrong, nil
	}
	outcome, err := classify(ranked[0])
	if err != nil || outcome == OutcomeRight || outcome == OutcomeSameWork || outcome == OutcomeNone {
		return outcome, 0, err
	}
	for i, c := range ranked[1:] {
		o, err := classify(c)
		if err != nil {
			return "", 0, err
		}
		if o == OutcomeRight || o == OutcomeSameWork {
			return outcome, i + 2, nil
		}
	}
	return outcome, 0, nil
}

func sampleBook(row sqlc.ListMatchCheckBooksRow) checkBook {
	b := catalog.Book{
		ID: row.ID, Title: row.Title, PublishedOn: row.PublishedOn,
		PageCount: row.PageCount, SeriesIndex: row.SeriesIndex,
	}
	if row.Publisher != nil {
		b.Publisher = *row.Publisher
	}
	for _, kind := range row.Kinds {
		b.Files = append(b.Files, catalog.File{Kind: kind})
	}
	if row.Series != nil {
		b.Series = *row.Series
	}
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
