package discover

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

const (
	// perAsk is how many books an ask keeps, the best first.
	perAsk = 15
	// moreInSeries is how many volumes after the next one a series keeps.
	moreInSeries = 2
	// resembled bounds the texts embedded for one ask.
	resembled = 40
)

// Run works the people whose run is due until the deadline: it starts the
// run of whoever asked for one or has not had one for a day, asks the
// providers about each subject of the plan not asked yet, and finishes the
// run when all are. It reports whether nothing is left, and asks nobody
// while Discover is off or the server offline.
func (s *Service) Run(ctx context.Context, deadline time.Time) (complete bool, err error) {
	if !s.enabled(ctx) || s.Offline {
		return true, nil
	}
	due, err := sqlc.New(s.pool).ListDiscoverDue(ctx, s.now().Add(-runEvery))
	if err != nil {
		return false, err
	}
	asked := 0
	for _, d := range due {
		done, err := s.runUser(ctx, d.UserID, auth.Allows(d.Role, auth.StorageManage), deadline, &asked)
		if err != nil || !done {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) runUser(ctx context.Context, user uuid.UUID, seesAll bool, deadline time.Time, asked *int) (bool, error) {
	q := sqlc.New(s.pool)
	run, err := q.GetDiscoverRun(ctx, user)
	if err != nil {
		return false, err
	}
	if run.FinishedAt != nil {
		if err := q.StartDiscoverRun(ctx, user); err != nil {
			return false, err
		}
		if run, err = q.GetDiscoverRun(ctx, user); err != nil {
			return false, err
		}
	}
	scope := library.Scope{Viewer: user, SeesAll: seesAll}
	plan, err := s.plan(ctx, scope)
	if err != nil {
		return false, err
	}
	counts := map[string]int32{}
	for _, a := range plan {
		counts[a.kind]++
	}
	if err := q.HoldDiscoverRun(ctx, sqlc.HoldDiscoverRunParams{
		UserID: user, Until: &deadline, Authors: counts[kindAuthor], Series: counts[kindSeries], Genres: counts[kindGenre],
	}); err != nil {
		return false, err
	}
	defer func() { _ = q.ReleaseDiscoverRun(context.WithoutCancel(ctx), user) }()
	doneBefore, err := q.ListDiscoverAsked(ctx, sqlc.ListDiscoverAskedParams{UserID: user, AskedAt: run.StartedAt})
	if err != nil {
		return false, err
	}
	var profile similar.Profile
	hasProfile, profiled := false, false
	for _, a := range plan {
		if slices.ContainsFunc(doneBefore, func(r sqlc.ListDiscoverAskedRow) bool { return r.Kind == a.kind && r.NameKey == a.key }) {
			continue
		}
		if s.now().After(deadline) || (s.AsksPerPass > 0 && *asked >= s.AsksPerPass) {
			return false, nil
		}
		*asked++
		if !profiled {
			profiled = true
			if s.taste != nil {
				if profile, hasProfile, err = s.taste.Profile(ctx, scope); err != nil {
					return false, err
				}
			}
		}
		if err := s.ask(ctx, scope, run.StartedAt, a, profile, hasProfile); err != nil {
			return false, err
		}
		if err := q.PutDiscoverAsk(ctx, sqlc.PutDiscoverAskParams{UserID: user, Kind: a.kind, NameKey: a.key}); err != nil {
			return false, err
		}
	}
	return true, s.finish(ctx, user, run.StartedAt)
}

// finish ends a run: books the run did not find again go, and of those that
// are one book under several providers or editions the best is kept.
func (s *Service) finish(ctx context.Context, user uuid.UUID, started time.Time) error {
	q := sqlc.New(s.pool)
	if err := q.DeleteStaleDiscoverCandidates(ctx, sqlc.DeleteStaleDiscoverCandidatesParams{UserID: user, FoundAt: started}); err != nil {
		return err
	}
	rows, err := q.ListDiscoverCandidates(ctx, user)
	if err != nil {
		return err
	}
	var kept []metadata.Record
	var keptIDs []uuid.UUID
	var gone []uuid.UUID
	borrowed := map[int]bool{}
	for _, r := range rows {
		var rec metadata.Record
		if err := json.Unmarshal(r.Record, &rec); err != nil {
			return err
		}
		if i := slices.IndexFunc(kept, func(k metadata.Record) bool { return sameBook(k, rec) }); i >= 0 {
			gone = append(gone, r.ID)
			if kept[i].BorrowCover(rec) {
				borrowed[i] = true
			}
			continue
		}
		kept = append(kept, rec)
		keptIDs = append(keptIDs, r.ID)
	}
	for i := range borrowed {
		data, err := json.Marshal(kept[i])
		if err != nil {
			return err
		}
		if err := q.UpdateDiscoverCandidateRecord(ctx, sqlc.UpdateDiscoverCandidateRecordParams{UserID: user, ID: keptIDs[i], Record: data}); err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		if err := q.DeleteDiscoverCandidates(ctx, sqlc.DeleteDiscoverCandidatesParams{UserID: user, Ids: gone}); err != nil {
			return err
		}
	}
	return q.FinishDiscoverRun(ctx, user)
}

// found is a record with why it was found.
type found struct {
	rec    metadata.Record
	reason string
	signal float64
}

// ask asks the providers about one subject and keeps the books the person
// has a use for.
func (s *Service) ask(ctx context.Context, scope library.Scope, started time.Time, a ask, p similar.Profile, hasProfile bool) error {
	records, err := s.records(ctx, a)
	if err != nil {
		return err
	}
	var cands []found
	switch a.kind {
	case kindSeries:
		cands = seriesBooks(a, records)
	default:
		reason := ReasonAuthor
		if a.kind == kindGenre {
			reason = ReasonGenre
		}
		for _, r := range records {
			cands = append(cands, found{rec: r, reason: reason, signal: signalOf(a, reason)})
		}
	}
	cands = withoutDuplicates(cands)
	keys := make([]keyed, len(cands))
	for i, c := range cands {
		keys[i] = keysOf(c.rec)
	}
	held, err := s.held(ctx, scope, keys)
	if err != nil {
		return err
	}
	var open []found
	for i, c := range cands {
		if _, ok := held[i]; !ok {
			open = append(open, c)
		}
	}
	open = open[:min(len(open), resembled)]
	cosines := make([]*float64, len(open))
	because := make([][]string, len(open))
	if hasProfile && s.taste != nil && len(open) > 0 {
		texts := make([]string, len(open))
		for i, c := range open {
			texts[i] = describe(c.rec)
		}
		got, err := s.taste.Resemble(ctx, p, texts)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			s.log.Info("discover ranks without the taste profile", "error", err)
		}
		for i, r := range got {
			if r == nil {
				continue
			}
			cosines[i] = &r.Score
			for _, b := range r.Because {
				if b.Title != "" {
					because[i] = append(because[i], b.Title)
				}
			}
		}
	}
	type ranked struct {
		found
		score   float64
		because []string
	}
	var out []ranked
	for i, c := range open {
		out = append(out, ranked{c, score(c.signal, cosines[i]), because[i]})
	}
	slices.SortStableFunc(out, func(x, y ranked) int { return cmp.Compare(y.score, x.score) })
	for _, r := range out[:min(len(out), perAsk)] {
		if err := s.put(ctx, scope.Viewer, started, a, r.found, r.score, r.because); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) put(ctx context.Context, user uuid.UUID, started time.Time, a ask, c found, score float64, because []string) error {
	data, err := json.Marshal(c.rec)
	if err != nil {
		return err
	}
	k := keysOf(c.rec)
	id := c.rec.ID
	if id == "" {
		id = "title:" + titleKey(c.rec.Title)
	}
	stars := int16(0)
	name := a.name
	switch a.kind {
	case kindAuthor:
		stars = a.stars
	case kindGenre:
		name = a.key
	}
	return sqlc.New(s.pool).PutDiscoverCandidate(ctx, sqlc.PutDiscoverCandidateParams{
		UserID: user, Provider: c.rec.Provider, ProviderID: id, Work: c.rec.Work, Title: c.rec.Title,
		TitleKeys: nonNil(k.titles), AuthorKeys: nonNil(k.authors), Isbns: nonNil(k.isbns), Record: data,
		Score: score, ReasonKind: c.reason, ReasonName: name, ReasonStars: stars, Because: nonNil(because),
		RunStarted: started,
	})
}

// withoutDuplicates leaves one record of each book, the first, with the
// cover of another record of it where the first has none.
func withoutDuplicates(in []found) []found {
	var out []found
	for _, c := range in {
		if i := slices.IndexFunc(out, func(o found) bool { return sameBook(o.rec, c.rec) }); i >= 0 {
			out[i].rec.BorrowCover(c.rec)
			continue
		}
		out = append(out, c)
	}
	return out
}

// seriesBooks sorts the records of a series into the next volume after the
// last one the person read, the places their libraries lack among those they
// hold, and a few volumes after the next. A volume before where they stand
// that is no gap is not offered.
func seriesBooks(a ask, records []metadata.Record) []found {
	var gaps []catalog.Gap
	gaps = catalog.Gaps(a.held)
	inGap := func(index float64) bool {
		return slices.ContainsFunc(gaps, func(g catalog.Gap) bool { return index >= float64(g.From) && index <= float64(g.To) })
	}
	var after []metadata.Record
	var out []found
	for _, r := range records {
		if r.Series != "" && catalog.Key(r.Series) != a.key {
			continue
		}
		index := catalog.SeriesIndexOf(r.SeriesIndex)
		if index == nil {
			continue
		}
		switch {
		case *index > a.lastRead:
			after = append(after, r)
		case inGap(*index) && *index == float64(int(*index)):
			out = append(out, found{rec: r, reason: ReasonSeriesGap, signal: signalOf(a, ReasonSeriesGap)})
		}
	}
	slices.SortStableFunc(after, func(x, y metadata.Record) int { return cmp.Compare(*x.SeriesIndex, *y.SeriesIndex) })
	for i, r := range after {
		reason := ReasonSeriesMore
		if i == 0 {
			reason = ReasonSeriesNext
		}
		if i > moreInSeries {
			break
		}
		out = append(out, found{rec: r, reason: reason, signal: signalOf(a, reason)})
	}
	return out
}

// records lists the books of the ask: from what following knows of an
// author or a series when it asked lately, else from the providers.
func (s *Service) records(ctx context.Context, a ask) ([]metadata.Record, error) {
	var answers []metadata.Answer
	var err error
	switch a.kind {
	case kindGenre:
		answers, err = s.source.SubjectsOf(ctx, a.name)
	default:
		if known, kerr := s.known(ctx, a); kerr != nil {
			return nil, kerr
		} else if len(known) > 0 {
			return known, nil
		}
		answers, err = s.source.WorksOf(ctx, a.kind, a.name)
	}
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A provider that failed is asked again at the next run; the others count.
	if err != nil {
		s.log.Warn("a provider did not list books for discover", "kind", a.kind, "error", err)
	}
	var out []metadata.Record
	for _, answer := range answers {
		for _, r := range answer.Records {
			r.Provider = answer.Provider
			out = append(out, r)
		}
	}
	return out, nil
}

// known is what the providers listed of a followed author or series since
// the last two days, as records.
func (s *Service) known(ctx context.Context, a ask) ([]metadata.Record, error) {
	key := a.key
	if a.kind == kindAuthor {
		key = catalog.Key(metadata.NaturalName(a.name))
	}
	rows, err := sqlc.New(s.pool).ListKnownReleases(ctx, sqlc.ListKnownReleasesParams{
		Kind: a.kind, NameKey: key, Since: s.now().Add(-knownFor),
	})
	if err != nil {
		return nil, err
	}
	var out []metadata.Record
	for _, r := range rows {
		rec := metadata.Record{Provider: r.Provider, ID: r.ProviderID, Title: r.Title, SeriesIndex: r.SeriesIndex}
		for _, name := range r.Authors {
			rec.Contributors = append(rec.Contributors, catalog.NewContributor{Name: name, Role: catalog.RoleAuthor})
		}
		if r.Series != nil {
			rec.Series = *r.Series
		}
		if r.CoverUrl != nil {
			rec.CoverURL = *r.CoverUrl
		}
		for _, isbn := range r.Isbns {
			rec.Identifiers = append(rec.Identifiers, catalog.Identifier{Type: catalog.IDISBN, Value: isbn})
		}
		if r.ReleaseDate != nil && r.Precision != nil {
			rec.Published = published(*r.ReleaseDate, *r.Precision)
		}
		out = append(out, rec)
	}
	return out, nil
}

func published(d time.Time, precision string) string {
	switch precision {
	case "year":
		return strconv.Itoa(d.Year())
	case "month":
		return d.Format("2006-01")
	}
	return d.Format(time.DateOnly)
}
