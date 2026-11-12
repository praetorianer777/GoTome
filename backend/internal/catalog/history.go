package catalog

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// HistoryPageSize is how many books a page of the reading history holds.
const HistoryPageSize = 50

// HistoryStatuses are the statuses the reading history lists by default.
var HistoryStatuses = []string{StatusReading, StatusCompleted, StatusAbandoned}

// HistoryFilter narrows the reading history.
type HistoryFilter struct {
	// Statuses defaults to HistoryStatuses.
	Statuses []string
	// Year is the year of the finish, else of the start; 0 for any.
	Year int
	// Stars is the person's own rating; 0 for any.
	Stars int
	// Search is words of the title or of an author.
	Search string
	// After is the Next of the page before.
	After string
}

// HistoryEntry is a book in the reading history, with the person's dates.
type HistoryEntry struct {
	Book       Summary
	StartedOn  *time.Time
	FinishedOn *time.Time
}

// HistoryPage is a page of the reading history.
type HistoryPage struct {
	Entries []HistoryEntry
	// Next is empty at the end.
	Next string
	// Years are the years the history lies in, the latest first.
	Years []int
}

// ListReading lists what the scope's user read, is reading or gave up,
// books the library holds and books it does not alike, the latest first. It
// is their own: no other person's standing is ever in it.
func (s *Service) ListReading(ctx context.Context, scope library.Scope, f HistoryFilter) (HistoryPage, error) {
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = HistoryStatuses
	}
	for _, st := range statuses {
		if !slices.Contains(Statuses, st) {
			return HistoryPage{}, EditError{"status": "The status is " + strings.Join(Statuses, ", ") + "."}
		}
	}
	p := sqlc.ListReadingParams{
		Viewer: scope.Viewer, SeesAll: scope.SeesAll, Statuses: statuses, SearchKey: Key(f.Search), MaxRows: HistoryPageSize + 1,
	}
	if f.Year != 0 {
		year := int32(f.Year)
		p.Year = &year
	}
	if f.Stars != 0 {
		stars := int32(f.Stars)
		p.Stars = &stars
	}
	if f.After != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.After)
		day, id, ok := strings.Cut(string(raw), "|")
		if err != nil || !ok {
			return HistoryPage{}, EditError{"after": "This is not a page of the history."}
		}
		d, err1 := time.Parse(time.DateOnly, day)
		u, err2 := uuid.Parse(id)
		if err1 != nil || err2 != nil {
			return HistoryPage{}, EditError{"after": "This is not a page of the history."}
		}
		p.AfterDay, p.AfterID = &d, &u
	}
	q := sqlc.New(s.pool)
	rows, err := q.ListReading(ctx, p)
	if err != nil {
		return HistoryPage{}, err
	}
	out := HistoryPage{Entries: []HistoryEntry{}}
	if len(rows) > HistoryPageSize {
		last := rows[HistoryPageSize-1]
		out.Next = base64.RawURLEncoding.EncodeToString([]byte(last.Day.Format(time.DateOnly) + "|" + last.BookID.String()))
		rows = rows[:HistoryPageSize]
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.BookID
	}
	books, err := s.Summaries(ctx, scope, ids)
	if err != nil {
		return HistoryPage{}, err
	}
	byID := map[uuid.UUID]Summary{}
	for _, b := range books {
		byID[b.ID] = b
	}
	for _, r := range rows {
		if b, ok := byID[r.BookID]; ok {
			out.Entries = append(out.Entries, HistoryEntry{Book: b, StartedOn: r.StartedOn, FinishedOn: r.FinishedOn})
		}
	}
	years, err := q.ReadingYears(ctx, sqlc.ReadingYearsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return HistoryPage{}, err
	}
	out.Years = []int{}
	for _, y := range years {
		out.Years = append(out.Years, int(y))
	}
	return out, nil
}
