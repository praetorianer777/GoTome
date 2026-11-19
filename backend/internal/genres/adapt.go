package genres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
)

// Adapting the ensemble to an installation (#530, docs/decisions/genre-detection.md,
// 4): the heads and the word tables are trained again on the books whose
// genres tags or a person gave, and merged with the shipped ones, weighing
// each genre by the books that taught it. The shipped training's books are
// not there; its document frequencies, positives and weights are what is
// merged. Stackers and thresholds are calibrated out of fold on the library's
// own books once a set of inputs has minStackerBooks of them, else the
// shipped ones stay.

const (
	// minAdaptBooks is how many labelled books make training worth the
	// time; fewer leave the shipped ensemble as it is.
	minAdaptBooks = 20
	// adaptEvery is how old the adapted ensemble may get before it is
	// trained again, and a fifth more or fewer labelled books is reason
	// enough sooner.
	adaptEvery = 24 * time.Hour
)

// mergeBase merges the shipped heads and word tables into the trained ones
// of e, and keeps the shipped tables of languages the books were too few for.
func mergeBase(e, base *Ensemble, model string) {
	for key, lib := range e.Content {
		e.Content[key] = mergeHead(base.Content[key], lib, base)
	}
	for key, lib := range e.Metadata {
		e.Metadata[key] = mergeHead(base.Metadata[key], lib, base)
	}
	for key, h := range base.Content {
		if e.Content[key] == nil {
			e.Content[key] = h
		}
	}
	for key, h := range base.Metadata {
		if e.Metadata[key] == nil {
			e.Metadata[key] = h
		}
	}
	for lang, t := range base.Words {
		e.Words[lang] = mergeWords(t, e.Words[lang])
	}
}

// keepBaseStackers gives e the base's stacker for every set of inputs it
// has none of, with the prior it was fitted under.
func keepBaseStackers(e, base *Ensemble) {
	if !slices.Equal(e.Genres, base.Genres) {
		return
	}
	for _, st := range base.Stackers {
		if slices.ContainsFunc(e.Stackers, func(o *Stacker) bool { return o.Mask == st.Mask }) {
			continue
		}
		c := *st
		if c.Prior == nil {
			c.Prior = base.Prior
		}
		e.Stackers = append(e.Stackers, &c)
	}
	slices.SortFunc(e.Stackers, func(a, b *Stacker) int { return a.Mask - b.Mask })
}

// shippedPositives are the books of each genre a shipped head learned from,
// as its ensemble's prior tells them.
func shippedPositives(h *Head, base *Ensemble) []float64 {
	out := make([]float64, len(h.Weights))
	for g := range out {
		switch {
		case len(h.Positives) == len(out):
			out[g] = float64(h.Positives[g])
		case len(base.Prior) == len(out):
			out[g] = math.Round(math.Exp(base.Prior[g]) * float64(base.Books))
		}
	}
	return out
}

// mergeHead is base and lib as one head, in base's standardisation: per
// genre the weights of both, in raw units, averaged by the books each had.
func mergeHead(base, lib *Head, baseEnsemble *Ensemble) *Head {
	if base == nil || len(base.Mean) != len(lib.Mean) || len(base.Weights) != len(lib.Weights) {
		return lib
	}
	d := len(base.Mean)
	pb := shippedPositives(base, baseEnsemble)
	out := &Head{Mean: base.Mean, Inv: base.Inv, Weights: make([][]float32, len(base.Weights)),
		Positives: make([]int, len(base.Weights))}
	for g := range out.Weights {
		bw, lw := base.Weights[g], lib.Weights[g]
		pl := 0.0
		if len(lib.Positives) == len(out.Weights) {
			pl = float64(lib.Positives[g])
		}
		out.Positives[g] = int(pb[g] + pl)
		var cl []float64
		if lw != nil {
			cl = make([]float64, d+1)
			bias := float64(lw[d])
			for j := range d {
				raw := float64(lw[j]) * float64(lib.Inv[j])
				bias += raw * (float64(base.Mean[j]) - float64(lib.Mean[j]))
				if base.Inv[j] != 0 {
					cl[j] = raw / float64(base.Inv[j])
				}
			}
			cl[d] = bias
		}
		switch {
		case bw == nil && cl == nil:
		case cl == nil:
			out.Weights[g] = bw
		case bw == nil:
			out.Weights[g] = toFloat32(cl)
		default:
			wb, wl := pb[g], pl
			if wb+wl == 0 {
				wb, wl = 1, 1
			}
			m := make([]float32, d+1)
			for j := range m {
				m[j] = float32((wb*float64(bw[j]) + wl*cl[j]) / (wb + wl))
			}
			out.Weights[g] = m
		}
	}
	return out
}

func toFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out
}

// mergeWords is the two tables of one language as one: the words of both,
// their document frequencies and books added, each genre's weights averaged
// by the positives of each.
func mergeWords(base, lib *WordTable) *WordTable {
	switch {
	case lib == nil:
		return base
	case base == nil:
		return lib
	}
	df := map[string]uint32{}
	for i, w := range base.Words {
		df[w] += base.DF[i]
	}
	for i, w := range lib.Words {
		df[w] += lib.DF[i]
	}
	words := make([]string, 0, len(df))
	for w := range df {
		words = append(words, w)
	}
	slices.Sort(words)
	t := &WordTable{Lang: base.Lang, Books: base.Books + lib.Books, Words: words, DF: make([]uint32, len(words))}
	for i, w := range words {
		t.DF[i] = df[w]
	}
	var keys []string
	for _, g := range base.Genres {
		keys = append(keys, g.Key)
	}
	for _, g := range lib.Genres {
		if !slices.Contains(keys, g.Key) {
			keys = append(keys, g.Key)
		}
	}
	find := func(tab *WordTable, key string) WordGenre {
		for _, g := range tab.Genres {
			if g.Key == key {
				return g
			}
		}
		return WordGenre{Key: key}
	}
	for _, key := range keys {
		bg, lg := find(base, key), find(lib, key)
		g := WordGenre{Key: key, Positives: bg.Positives + lg.Positives}
		if bg.Trained() || lg.Trained() {
			var wb, wl float64
			if bg.Trained() {
				wb = float64(bg.Positives)
			}
			if lg.Trained() {
				wl = float64(lg.Positives)
			}
			w := make([]float64, len(words)+1)
			for i, word := range words {
				var v float64
				if j, ok := base.index[word]; ok && bg.Trained() {
					v += wb * float64(bg.Weights[j]) * float64(bg.Scale)
				}
				if j, ok := lib.index[word]; ok && lg.Trained() {
					v += wl * float64(lg.Weights[j]) * float64(lg.Scale)
				}
				w[i] = v / (wb + wl)
			}
			w[len(words)] = (wb*float64(bg.Bias) + wl*float64(lg.Bias)) / (wb + wl)
			quantize(&g, w)
		}
		t.Genres = append(t.Genres, g)
	}
	t.prepare()
	return t
}

// stored is an adapted ensemble as it lies in genre_adaptation.
type stored struct {
	ensemble []byte
	words    []byte
}

func encodeAdapted(e *Ensemble) (stored, error) {
	words := map[string][]byte{}
	for lang, t := range e.Words {
		words[lang] = t.Encode()
	}
	wb, err := json.Marshal(words)
	if err != nil {
		return stored{}, err
	}
	eb, err := e.Encode()
	return stored{eb, wb}, err
}

func decodeAdapted(s stored) (*Ensemble, error) {
	var raw map[string][]byte
	if err := json.Unmarshal(s.words, &raw); err != nil {
		return nil, err
	}
	words := map[string]*WordTable{}
	for lang, b := range raw {
		t, err := DecodeWordTable(b)
		if err != nil {
			return nil, err
		}
		words[lang] = t
	}
	return DecodeEnsemble(s.ensemble, words)
}

// current is the ensemble a pass suggests with: the one adapted to this
// installation's books if there is one trained beside the shipped ensemble
// as it is now and the model the pass uses, else the shipped one.
func (s *Service) current(ctx context.Context, model string) (*Ensemble, error) {
	base, err := s.Ensemble()
	if err != nil || base == nil {
		return base, err
	}
	row, err := sqlc.New(s.pool).GetGenreAdaptation(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return base, nil
	}
	if err != nil {
		return nil, err
	}
	if row.ShippedVersion != base.Version || row.Model != model {
		return base, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.adapted != nil && s.adapted.Version == row.Version {
		return s.adapted, nil
	}
	e, err := decodeAdapted(stored{row.Ensemble, row.Words})
	if err != nil {
		s.log.Warn("the adapted genre ensemble is damaged; the shipped one is used", "error", err)
		return base, nil
	}
	s.adapted = e
	return e, nil
}

// gathering is the training books read so far; it stays in memory between
// the runs of a job that snoozed, and a run that finds it for another
// ensemble starts over.
type gathering struct {
	key      string
	ids      []uuid.UUID
	labels   map[uuid.UUID][]string
	next     int
	perLang  map[string]int
	books    []TrainBook
	labelled int
}

func (s *Service) due(ctx context.Context, shipped, model string, labelled int) (bool, error) {
	row, err := sqlc.New(s.pool).GetGenreAdaptation(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	diff := max(labelled-int(row.Labelled), int(row.Labelled)-labelled)
	return row.ShippedVersion != shipped || row.Model != model ||
		time.Since(row.TrainedAt) >= adaptEvery || diff*5 >= max(int(row.Labelled), 1), nil
}

// Adapt trains the ensemble on the installation's labelled books when it is
// due: none yet, another shipped ensemble or model, a day old, or a fifth
// more or fewer labelled books. Reading the books stops at the deadline and
// goes on in the next run; nothing is written until the whole is trained,
// and then in one statement, so a run cut off leaves the last ensemble.
// It reports whether it is done.
func (s *Service) Adapt(ctx context.Context, deadline time.Time) (bool, error) {
	mode, err := s.mode(ctx)
	if err != nil || mode == Off || mode == MethodTags || mode == MethodNeighbours {
		return true, err
	}
	spec, err := s.vectors.Spec(ctx)
	if err != nil {
		return false, err
	}
	base, err := s.Ensemble()
	if err != nil {
		return false, err
	}
	q := sqlc.New(s.pool)
	labelled, err := q.CountGenreLabelled(ctx)
	if err != nil {
		return false, err
	}
	if int(labelled) < minAdaptBooks {
		return true, q.DeleteGenreAdaptation(ctx)
	}
	shipped := ""
	if base != nil {
		shipped = base.Version
	}
	due, err := s.due(ctx, shipped, spec.Model.Name, int(labelled))
	if err != nil || !due {
		return true, err
	}
	key := shipped + "/" + spec.Model.Name
	g := s.gather
	if g == nil || g.key != key {
		if g, err = s.startGathering(ctx, key); err != nil {
			return false, err
		}
		s.gather = g
	}
	done, err := s.read(ctx, g, spec, deadline)
	if err != nil || !done {
		return false, err
	}
	s.gather = nil
	genres := Keys()
	if base != nil {
		genres = base.Genres
	}
	var langs []string
	for lang := range g.perLang {
		if lang != "" {
			langs = append(langs, lang)
		}
	}
	slices.Sort(langs)
	e := TrainEnsemble(g.books, genres, EnsembleOptions{
		Model: spec.Model.Name, WordLangs: langs, Workers: max(runtime.NumCPU()/2, 1), Base: base,
	})
	if len(e.Stackers) == 0 {
		return true, q.DeleteGenreAdaptation(ctx)
	}
	return true, s.store(ctx, e, shipped, spec.Model.Name, g)
}

// store names the ensemble by a hash of its numbers: the same training
// again gives the same version, so no book is looked at again for it.
func (s *Service) store(ctx context.Context, e *Ensemble, shipped, model string, g *gathering) error {
	e.Version = ""
	plain, err := encodeAdapted(e)
	if err != nil {
		return err
	}
	sum := sha256.New()
	sum.Write(plain.ensemble)
	sum.Write(plain.words)
	e.Version = shipped + "+" + hex.EncodeToString(sum.Sum(nil))[:8]
	data, err := encodeAdapted(e)
	if err != nil {
		return err
	}
	return sqlc.New(s.pool).PutGenreAdaptation(ctx, sqlc.PutGenreAdaptationParams{
		ShippedVersion: shipped, Model: model, Version: e.Version, Labelled: int32(g.labelled),
		Books: int32(len(g.books)), Ensemble: data.ensemble, Words: data.words,
	})
}

// startGathering lists the labelled books in the order of a hash of their
// ID, so that a cap keeps the same books each time.
func (s *Service) startGathering(ctx context.Context, key string) (*gathering, error) {
	rows, err := sqlc.New(s.pool).ListGenreLabels(ctx)
	if err != nil {
		return nil, err
	}
	g := &gathering{key: key, labels: map[uuid.UUID][]string{}, perLang: map[string]int{}}
	for _, r := range rows {
		if _, ok := g.labels[r.BookID]; !ok {
			g.ids = append(g.ids, r.BookID)
		}
		g.labels[r.BookID] = append(g.labels[r.BookID], r.Genre)
	}
	g.labelled = len(g.ids)
	slices.SortFunc(g.ids, func(a, b uuid.UUID) int {
		ha, hb := idHash(a.String()), idHash(b.String())
		switch {
		case ha < hb:
			return -1
		case ha > hb:
			return 1
		}
		return 0
	})
	return g, nil
}

// read takes in the books' words and vectors until none is left or the
// deadline has passed, at most MaxTrainBooks of a language.
func (s *Service) read(ctx context.Context, g *gathering, spec embed.Spec, deadline time.Time) (bool, error) {
	for ; g.next < len(g.ids); g.next++ {
		if time.Now().After(deadline) {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		id := g.ids[g.next]
		lang, counts, err := s.words(ctx, id)
		if err != nil {
			return false, err
		}
		if g.perLang[lang] >= MaxTrainBooks {
			continue
		}
		v, _, err := s.bookVectors(ctx, spec, id)
		if err != nil {
			return false, err
		}
		if len(counts) == 0 && v.Content == nil && v.Metadata == nil {
			continue
		}
		g.perLang[lang]++
		g.books = append(g.books, TrainBook{ID: id.String(), Lang: lang, Counts: counts,
			Content: v.Content, Metadata: v.Metadata, Labels: g.labels[id]})
	}
	return true, nil
}
