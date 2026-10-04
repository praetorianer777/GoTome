package reading

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// MaxStatsDays is the longest stretch the daily statistics cover.
const MaxStatsDays = 366

// maxHistory is how many starts and finishes the history lists.
const maxHistory = 100

// ErrBadZone is a time zone the statistics cannot count days in.
var ErrBadZone = errors.New("unknown time zone")

// Day is what a person read and listened to on one day.
type Day struct {
	Date             time.Time
	ReadingSeconds   int64
	ListeningSeconds int64
	Pages            float64
}

// Year is how often a person read a book to its end in a year, and how
// many different books those were.
type Year struct {
	Year     int
	Finishes int
	Books    int
}

// Event is a book begun or finished.
type Event struct {
	BookID uuid.UUID
	Title  string
	// Kind is "started" or "finished".
	Kind string
	Date time.Time
}

// Stats is what a person read and listened to: per day over the last days,
// in total, per year, and when.
type Stats struct {
	// Days are the last days, the oldest first, each one there even when
	// nothing was read on it.
	Days []Day
	// TotalReadingSeconds and the others count all time.
	TotalReadingSeconds   int64
	TotalListeningSeconds int64
	TotalPages            float64
	// PagesEstimated is set when pages counted come from an EPUB or MOBI,
	// whose pages are worked out from the length of its text.
	PagesEstimated bool
	Years          []Year
	History        []Event
}

// Stats counts what the scope's user read and listened to, from their
// sessions and finishes, in books they may still see. Days are those of
// the zone, an IANA name such as Europe/Berlin; the last of the days is
// today.
func (s *Service) Stats(ctx context.Context, scope library.Scope, days int, zone string, now time.Time) (Stats, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return Stats{}, ErrBadZone
	}
	days = min(max(days, 1), MaxStatsDays)
	local := now.In(loc)
	first := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1-days)

	q := sqlc.New(s.pool)
	out := Stats{Days: make([]Day, days), Years: []Year{}, History: []Event{}}
	for i := range out.Days {
		d := first.AddDate(0, 0, i)
		out.Days[i].Date = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	}
	daily, err := q.DailyActivity(ctx, sqlc.DailyActivityParams{Tz: zone, UserID: scope.Viewer, Since: first, SeesAll: scope.SeesAll})
	if err != nil {
		return Stats{}, err
	}
	for _, row := range daily {
		i := int(row.Day.Sub(out.Days[0].Date).Hours() / 24)
		if i < 0 || i >= days {
			continue
		}
		if row.Medium == MediumAudio {
			out.Days[i].ListeningSeconds += row.Seconds
		} else {
			out.Days[i].ReadingSeconds += row.Seconds
			out.Days[i].Pages += row.Pages
		}
	}
	totals, err := q.ActivityTotals(ctx, sqlc.ActivityTotalsParams{UserID: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Stats{}, err
	}
	for _, row := range totals {
		if row.Medium == MediumAudio {
			out.TotalListeningSeconds += row.Seconds
		} else {
			out.TotalReadingSeconds += row.Seconds
			out.TotalPages += row.Pages
		}
		out.PagesEstimated = out.PagesEstimated || row.Estimated
	}
	years, err := q.FinishesByYear(ctx, sqlc.FinishesByYearParams{Tz: zone, UserID: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Stats{}, err
	}
	for _, y := range years {
		out.Years = append(out.Years, Year{Year: int(y.Year), Finishes: int(y.Finishes), Books: int(y.Books)})
	}
	history, err := q.ReadingHistory(ctx, sqlc.ReadingHistoryParams{UserID: scope.Viewer, Tz: zone, SeesAll: scope.SeesAll, MaxRows: maxHistory})
	if err != nil {
		return Stats{}, err
	}
	for _, h := range history {
		if h.Day != nil {
			out.History = append(out.History, Event{BookID: h.BookID, Title: h.Title, Kind: h.Event, Date: *h.Day})
		}
	}
	return out, nil
}
