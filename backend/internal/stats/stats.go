// Package stats counts what the libraries a person sees hold, and how far
// the background work on them is (#176).
package stats

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

// Service counts.
type Service struct {
	pool    *pgxpool.Pool
	vectors *similar.Service
}

// NewService returns a Service on the pool; vectors counts the books'
// vectors of the chosen model.
func NewService(pool *pgxpool.Pool, vectors *similar.Service) *Service {
	return &Service{pool: pool, vectors: vectors}
}

// Count is one kind of thing and how many there are.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	// Bytes is what they take up, where that is counted.
	Bytes int64 `json:"bytes,omitempty"`
}

// Figures are what the books hold.
type Figures struct {
	Books      int   `json:"books"`
	Files      int   `json:"files"`
	Bytes      int64 `json:"bytes"`
	Audiobooks int   `json:"audiobooks"`
	AudioHours int   `json:"audioHours"`
	Pages      int64 `json:"pages"`
	Authors    int   `json:"authors"`
	Series     int   `json:"series"`
	Publishers int   `json:"publishers"`
	Tags       int   `json:"tags"`
	// Without are the books that lack a cover, an author, or text that
	// was read (of those with an ebook).
	WithoutCover  int `json:"withoutCover"`
	WithoutAuthor int `json:"withoutAuthor"`
	WithoutText   int `json:"withoutText"`
	// Formats are the files by format, Languages the books by language
	// ("" for none known), Months the books added in each of the last
	// twelve months that had any, as YYYY-MM.
	Formats   []Count `json:"formats"`
	Languages []Count `json:"languages"`
	Months    []Count `json:"months"`
}

// Figures counts the books the scope sees, of one library when given. Its
// four counts read every book, so they run side by side.
func (s *Service) Figures(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (Figures, error) {
	q := sqlc.New(s.pool)
	f := Figures{Formats: []Count{}, Languages: []Count{}, Months: []Count{}}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		row, err := q.LibraryFigures(ctx, sqlc.LibraryFiguresParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID})
		if err != nil {
			return err
		}
		f.Books, f.Files, f.Bytes, f.Audiobooks = int(row.Books), int(row.Files), row.Bytes, int(row.Audiobooks)
		f.AudioHours = int(math.Round(float64(row.AudioMs) / 3_600_000))
		f.Pages, f.Authors, f.Series = row.Pages, int(row.Authors), int(row.Series)
		f.Publishers, f.Tags = int(row.Publishers), int(row.Tags)
		f.WithoutCover, f.WithoutAuthor, f.WithoutText = int(row.WithoutCover), int(row.WithoutAuthor), int(row.WithoutText)
		return nil
	})
	g.Go(func() error {
		rows, err := q.FilesByFormat(ctx, sqlc.FilesByFormatParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID})
		for _, r := range rows {
			f.Formats = append(f.Formats, Count{Name: r.Format, Count: int(r.Files), Bytes: r.Bytes})
		}
		return err
	})
	g.Go(func() error {
		rows, err := q.BooksByLanguage(ctx, sqlc.BooksByLanguageParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID})
		for _, r := range rows {
			f.Languages = append(f.Languages, Count{Name: r.Language, Count: int(r.Books)})
		}
		return err
	})
	g.Go(func() error {
		rows, err := q.BooksAddedPerMonth(ctx, sqlc.BooksAddedPerMonthParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID})
		for _, r := range rows {
			f.Months = append(f.Months, Count{Name: r.Month, Count: int(r.Books)})
		}
		return err
	})
	if err := g.Wait(); err != nil {
		return Figures{}, err
	}
	return f, nil
}

// Step is one piece of background work: how much of it is done of all
// there is, how much was done in the last hour, and how long what is left
// takes at that pace.
type Step struct {
	Done     int `json:"done"`
	Total    int `json:"total"`
	LastHour int `json:"lastHour"`
	// MinutesLeft is nil while nothing is left or nothing was done in the
	// last hour to tell the pace by.
	MinutesLeft *int `json:"minutesLeft,omitempty"`
}

func step(done, total, lastHour int) Step {
	s := Step{Done: done, Total: total, LastHour: lastHour}
	if left := total - done; left > 0 && lastHour > 0 {
		minutes := int(math.Ceil(float64(left) * 60 / float64(lastHour)))
		s.MinutesLeft = &minutes
	}
	return s
}

// Queue is work that only its jobs keep count of: how much waits, and how
// much was done in the last hour.
type Queue struct {
	Waiting  int `json:"waiting"`
	LastHour int `json:"lastHour"`
	// MinutesLeft is as a Step's.
	MinutesLeft *int `json:"minutesLeft,omitempty"`
}

func queue(waiting, lastHour int) Queue {
	s := step(0, waiting, lastHour)
	return Queue{Waiting: waiting, LastHour: lastHour, MinutesLeft: s.MinutesLeft}
}

// Work is how far the background work on the books is.
type Work struct {
	// Files are those read of those to read; FilesFailed could not be.
	Files       Step `json:"files"`
	FilesFailed int  `json:"filesFailed"`
	// Texts are the books' texts in the search index.
	Texts Step `json:"texts"`
	// Vectors are the books with the chosen model's vectors of their
	// metadata, and Content of those with text, their text's.
	Model   string `json:"model"`
	Vectors Step   `json:"vectors"`
	Content Step   `json:"content"`
	// Lookups are books waiting to be looked up at the sources, and
	// MatchesPending what they found that waits for review.
	Lookups        Queue `json:"lookups"`
	MatchesPending int   `json:"matchesPending"`
	// Checks are books waiting for their duplicate check, PairsOpen the
	// pairs found that nobody has decided about.
	Checks    Queue `json:"checks"`
	PairsOpen int   `json:"pairsOpen"`
}

// jobsQuery counts the lookups and duplicate checks of the visible books
// from their jobs, which the queue keeps for a while once finished. River
// makes its own tables, which sqlc does not know.
const jobsQuery = `
SELECT j.kind,
       count(*) FILTER (WHERE j.state NOT IN ('completed', 'cancelled', 'discarded'))::int,
       count(*) FILTER (WHERE j.state = 'completed' AND j.finalized_at > now() - interval '1 hour')::int
FROM river_job j
JOIN books b ON b.id = (j.args->>'bookId')::uuid
WHERE j.kind IN ('` + lookupKind + `', '` + checkKind + `')
  AND b.deleted_at IS NULL
  AND b.library_id IN (SELECT * FROM visible_library_ids($1, $2))
  AND ($3::uuid IS NULL OR b.library_id = $3)
GROUP BY j.kind`

// The kinds of the jobs that look books up and check them for duplicates.
const (
	lookupKind = "enrich.match_book"
	checkKind  = "dedupe.check_book"
)

// Work counts the background work on the books the scope sees, of one
// library when given. Its three counts run side by side.
func (s *Service) Work(ctx context.Context, scope library.Scope, libraryID *uuid.UUID) (Work, error) {
	var row sqlc.WorkProgressRow
	var vectors similar.Status
	w := Work{Lookups: queue(0, 0), Checks: queue(0, 0)}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		row, err = sqlc.New(s.pool).WorkProgress(ctx, sqlc.WorkProgressParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, LibraryID: libraryID})
		return err
	})
	g.Go(func() (err error) {
		vectors, err = s.vectors.Status(ctx, scope, libraryID)
		return err
	})
	g.Go(func() error {
		rows, err := s.pool.Query(ctx, jobsQuery, scope.Viewer, scope.SeesAll, libraryID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var waiting, lastHour int
			if err := rows.Scan(&kind, &waiting, &lastHour); err != nil {
				return err
			}
			switch kind {
			case lookupKind:
				w.Lookups = queue(waiting, lastHour)
			case checkKind:
				w.Checks = queue(waiting, lastHour)
			}
		}
		return rows.Err()
	})
	if err := g.Wait(); err != nil {
		return Work{}, err
	}
	w.Files = step(int(row.FilesRead+row.FilesFailed), int(row.Files), int(row.FilesLastHour))
	w.FilesFailed = int(row.FilesFailed)
	w.Texts = step(int(row.TextsIndexed), int(row.Texts), int(row.TextsLastHour))
	w.Model = vectors.Model
	w.Vectors = step(vectors.Metadata, vectors.Books, vectors.MetadataLastHour)
	w.Content = step(vectors.Content, vectors.WithText, 0)
	w.MatchesPending, w.PairsOpen = int(row.MatchesPending), int(row.PairsOpen)
	return w, nil
}
