package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/version"
)

// Which weights rank candidates, as the setting metadata.scoreWeights names
// them.
const (
	ScoreHandSet  = "handSet"
	ScoreLearned  = "learned"
	ScoreImported = "imported"
)

// Where a learned score is kept.
const (
	slotLearned  = "learned"
	slotImported = "imported"
)

// scorer scores a record against a book.
type scorer func(book catalog.Book, r Record, in scoreInput) float64

func handScorer(book catalog.Book, r Record, _ scoreInput) float64 { return Score(book, r) }

// learnedScorer makes a probability of the weights. A record that carries
// one of the book's identifiers is still the book, and scores 1.
func learnedScorer(w Weights) scorer {
	return func(book catalog.Book, r Record, in scoreInput) float64 {
		if sharesIdentifier(book, r) {
			return 1
		}
		return min(w.probability(features(book, r, in)), bestWithoutIdentifier)
	}
}

type namedScorer struct {
	name  string
	score scorer
}

// scorerSet are the scores there are, and the one in use.
type scorerSet struct {
	using string
	all   []namedScorer
}

// modelCacheTTL is how long the stored weights are remembered: a lookup
// scores hundreds of records and need not ask the database for each.
const modelCacheTTL = 30 * time.Second

type modelCache struct {
	mu      sync.Mutex
	loaded  time.Time
	learned *Profile
	imports *Profile
	shares  map[string]float64
}

func (c *modelCache) forget() {
	c.mu.Lock()
	c.loaded = time.Time{}
	c.mu.Unlock()
}

// stored reads the weights kept for this installation and the mix of its
// books' languages.
func (s *Service) stored(ctx context.Context) (learned, imported *Profile, shares map[string]float64, err error) {
	c := &s.models
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded.IsZero() && time.Since(c.loaded) < modelCacheTTL {
		return c.learned, c.imports, c.shares, nil
	}
	if s.pool == nil {
		return nil, nil, nil, nil
	}
	q := sqlc.New(s.pool)
	rows, err := q.ListScoreModels(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	c.learned, c.imports = nil, nil
	for _, row := range rows {
		var p Profile
		if json.Unmarshal(row.Model, &p) != nil || p.Check() != nil {
			// Weights of a feature set this build no longer uses are not used.
			continue
		}
		switch row.Slot {
		case slotLearned:
			c.learned = &p
		case slotImported:
			c.imports = &p
		}
	}
	counts, err := q.LanguageShares(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	total := 0.0
	for _, row := range counts {
		total += float64(row.Books)
	}
	c.shares = map[string]float64{}
	for _, row := range counts {
		c.shares[row.Language] = float64(row.Books) / max(total, 1)
	}
	c.loaded = time.Now()
	return c.learned, c.imports, c.shares, nil
}

// scorers are the scores a check compares, and the one the setting
// chooses. A failure to read the weights leaves the hand-set one.
func (s *Service) scorers(ctx context.Context) (scorerSet, error) {
	set := scorerSet{using: ScoreHandSet, all: []namedScorer{{ScoreHandSet, handScorer}}}
	learned, imported, shares, err := s.stored(ctx)
	if err != nil {
		return set, err
	}
	ownName, own := "", (*Profile)(nil)
	switch {
	case learned != nil:
		ownName, own = ScoreLearned, learned
	default:
		if name, p := profileFor(shares); p != nil {
			ownName, own = "profile:"+name, p
		}
	}
	if own != nil {
		set.all = append(set.all, namedScorer{ownName, learnedScorer(own.Weights)})
	}
	if imported != nil {
		set.all = append(set.all, namedScorer{ScoreImported, learnedScorer(imported.Weights)})
	}
	choice := ScoreLearned
	if s.weights != nil {
		choice = s.weights(ctx)
	}
	switch {
	case choice == ScoreHandSet:
	case choice == ScoreImported && imported != nil:
		set.using = ScoreImported
	case own != nil:
		set.using = ownName
	}
	return set, nil
}

// activeScorer is the score a lookup ranks with.
func (s *Service) activeScorer(ctx context.Context) scorer {
	set, _ := s.scorers(ctx)
	for _, c := range set.all {
		if c.name == set.using {
			return c.score
		}
	}
	return handScorer
}

// SaveProfile keeps weights for this installation, as it learned them or
// as a file brought them.
func (s *Service) saveProfile(ctx context.Context, slot string, p Profile) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	err = sqlc.New(s.pool).PutScoreModel(ctx, sqlc.PutScoreModelParams{Slot: slot, Model: raw, Books: int32(p.Books)})
	s.models.forget()
	return err
}

// ImportProfile keeps a file of weights someone shared, to be measured
// beside the others before it is switched on. It does not change what
// ranks candidates.
func (s *Service) ImportProfile(ctx context.Context, raw []byte) error {
	p, err := ParseProfile(raw)
	if err != nil {
		return err
	}
	return s.saveProfile(ctx, slotImported, p)
}

// RemoveImported forgets an imported file of weights.
func (s *Service) RemoveImported(ctx context.Context) error {
	err := sqlc.New(s.pool).DeleteScoreModel(ctx, slotImported)
	s.models.forget()
	return err
}

// ErrNoLearnedWeights is an export asked for before anything was learned.
var ErrNoLearnedWeights = errors.New("nothing has been learned yet")

// ExportProfile is the weights learned here, as a file a person can read
// before they share it.
func (s *Service) ExportProfile(ctx context.Context) (Profile, error) {
	learned, _, _, err := s.stored(ctx)
	if err != nil {
		return Profile{}, err
	}
	if learned == nil {
		return Profile{}, ErrNoLearnedWeights
	}
	return *learned, nil
}

// ScoringStatus is what the settings page shows of the match score.
type ScoringStatus struct {
	// Weights is what metadata.scoreWeights says; Using the score that
	// ranks candidates now: handSet, learned, profile:<name> or imported.
	Weights string `json:"weights"`
	Using   string `json:"using"`
	// BooksWithISBN is how many books could be learned from.
	BooksWithISBN int `json:"booksWithIsbn"`
	// LearnFrom is the number of books a weekly learning needs.
	LearnFrom int          `json:"learnFrom"`
	Learned   *WeightsInfo `json:"learned,omitempty"`
	Imported  *WeightsInfo `json:"imported,omitempty"`
	Measure   *RunStatus   `json:"measure,omitempty"`
	Learn     *RunStatus   `json:"learn,omitempty"`
	Report    *CheckReport `json:"measureReport,omitempty"`
	Training  *TrainReport `json:"learnReport,omitempty"`
	Profile   string       `json:"profile,omitempty"`
}

// WeightsInfo says what a stored score was learned from.
type WeightsInfo struct {
	LearnedAt     time.Time          `json:"learnedAt"`
	Books         int                `json:"books"`
	GotomeVersion string             `json:"gotomeVersion"`
	Languages     map[string]float64 `json:"languages"`
	Providers     map[string]float64 `json:"providers"`
}

// RunStatus is the last measure or learning, with its progress while it
// runs.
type RunStatus struct {
	State           string     `json:"state"`
	Done            int        `json:"done"`
	Total           int        `json:"total"`
	EstimateSeconds int        `json:"estimateSeconds"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// LearnFrom is the number of books with an ISBN from which a learning is
// worth its weekly requests.
const LearnFrom = 500

// Scoring reports the match score and the last runs.
func (s *Service) Scoring(ctx context.Context) (ScoringStatus, error) {
	st := ScoringStatus{Weights: ScoreLearned, LearnFrom: LearnFrom}
	if s.weights != nil {
		st.Weights = s.weights(ctx)
	}
	set, err := s.scorers(ctx)
	if err != nil {
		return st, err
	}
	st.Using = set.using
	if name, ok := strings.CutPrefix(set.using, "profile:"); ok {
		st.Profile = name
	}
	q := sqlc.New(s.pool)
	total, err := q.CountMatchCheckBooks(ctx)
	if err != nil {
		return st, err
	}
	st.BooksWithISBN = int(total)
	models, err := q.ListScoreModels(ctx)
	if err != nil {
		return st, err
	}
	for _, m := range models {
		var p Profile
		if json.Unmarshal(m.Model, &p) != nil || p.Check() != nil {
			continue
		}
		info := &WeightsInfo{LearnedAt: m.TrainedAt, Books: p.Books, GotomeVersion: p.GotomeVersion, Languages: p.Languages, Providers: p.Providers}
		if m.Slot == slotLearned {
			st.Learned = info
		} else {
			st.Imported = info
		}
	}
	runs, err := q.ListMatchRuns(ctx)
	if err != nil {
		return st, err
	}
	for _, run := range runs {
		status := &RunStatus{State: run.State, Done: int(run.Done), Total: int(run.Total), EstimateSeconds: int(run.EstimateSeconds),
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Error: run.Error}
		switch run.Kind {
		case runMeasure:
			st.Measure = status
			if run.Report != nil {
				var r CheckReport
				if json.Unmarshal(run.Report, &r) == nil {
					st.Report = &r
				}
			}
		case runLearn:
			st.Learn = status
			if run.Report != nil {
				var r TrainReport
				if json.Unmarshal(run.Report, &r) == nil {
					st.Training = &r
				}
			}
		}
	}
	return st, nil
}

// newProfile is what a learning makes of its weights.
func newProfile(w Weights, books int, languages, providers map[string]float64) Profile {
	return Profile{
		Format: ProfileFormat, FeatureVersion: FeatureVersion, GotomeVersion: version.Current().Version,
		Books: books, Languages: languages, Providers: providers, Weights: w,
	}
}

// How a candidate's description is compared with the book's text: only
// descriptions long enough to say what the book is about, and only the
// first few of each provider's answer, as every one costs an embedding.
const (
	describeTop      = 3
	describeMinRunes = 60
	describeMaxRunes = 1500
)

// describe works out how near the descriptions of a provider's first
// records are to the book's text, where the book has a content vector;
// the other records, and all of them where that cannot be worked out,
// get nil.
func (s *Service) describe(ctx context.Context, book catalog.Book, records []Record) []*float64 {
	out := make([]*float64, len(records))
	if s.Describe == nil || book.ID == uuid.Nil {
		return out
	}
	taken := map[string]int{}
	var at []int
	var texts []string
	for i, r := range records {
		d := strings.TrimSpace(r.Description)
		if utf8.RuneCountInString(d) < describeMinRunes || taken[r.Provider] >= describeTop {
			continue
		}
		taken[r.Provider]++
		if utf8.RuneCountInString(d) > describeMaxRunes {
			d = string([]rune(d)[:describeMaxRunes])
		}
		at, texts = append(at, i), append(texts, d)
	}
	if len(texts) == 0 {
		return out
	}
	cosines, err := s.Describe(ctx, book.ID, texts)
	if err != nil || len(cosines) != len(texts) {
		return out
	}
	for j, i := range at {
		out[i] = cosines[j]
	}
	return out
}
