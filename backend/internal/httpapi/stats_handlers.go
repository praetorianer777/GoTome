package httpapi

import (
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

type statsQuery struct {
	Days int    `query:"days" doc:"How many days, up to today, the daily figures cover: 1 to 366; 30 when left out."`
	TZ   string `query:"tz" doc:"The IANA time zone the days are counted in, such as Europe/Berlin; UTC when left out."`
}

// readingStats is what the caller read and listened to. Only ever the
// caller's own.
type readingStats struct {
	// Days are the last days, the oldest first, every one of them.
	Days []statsDay `json:"days"`
	// PagesPerDay and the minutes per day are averages over those days.
	PagesPerDay            float64 `json:"pagesPerDay"`
	ReadingMinutesPerDay   float64 `json:"readingMinutesPerDay"`
	ListeningMinutesPerDay float64 `json:"listeningMinutesPerDay"`
	// The totals count all time.
	TotalPages            int `json:"totalPages"`
	TotalReadingMinutes   int `json:"totalReadingMinutes"`
	TotalListeningMinutes int `json:"totalListeningMinutes"`
	// PagesEstimated is set when pages come from an EPUB or MOBI, whose
	// pages are worked out from the length of its text.
	PagesEstimated bool `json:"pagesEstimated"`
	// Years count every time a book was read to its end, a re-read too;
	// books is how many different ones.
	Years   []statsYear  `json:"years"`
	History []statsEvent `json:"history"`
}

type statsDay struct {
	// Date is the day, as 2006-01-02.
	Date             string  `json:"date"`
	Pages            float64 `json:"pages"`
	ReadingMinutes   float64 `json:"readingMinutes"`
	ListeningMinutes float64 `json:"listeningMinutes"`
}

type statsYear struct {
	Year     int `json:"year"`
	Finishes int `json:"finishes"`
	Books    int `json:"books"`
}

type statsEvent struct {
	BookID uuid.UUID `json:"bookId"`
	Title  string    `json:"title"`
	// Event is started or finished.
	Event string `json:"event"`
	Date  string `json:"date"`
}

// defaultStatsDays is how many days the statistics cover when the client
// does not say.
const defaultStatsDays = 30

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) error {
	var q statsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	days := q.Days
	if days == 0 {
		days = defaultStatsDays
	}
	if days < 1 || days > reading.MaxStatsDays {
		return ErrValidation(map[string]string{"days": "Count 1 to 366 days."})
	}
	zone := q.TZ
	if zone == "" {
		zone = "UTC"
	}
	st, err := s.Reading.Stats(r.Context(), library.ScopeOf(*UserFrom(r.Context())), days, zone, time.Now())
	if errors.Is(err, reading.ErrBadZone) {
		return ErrValidation(map[string]string{"tz": "Give a time zone such as Europe/Berlin."})
	}
	if err != nil {
		return err
	}
	out := readingStats{
		Days: make([]statsDay, len(st.Days)), Years: []statsYear{}, History: []statsEvent{},
		TotalPages:            int(math.Round(st.TotalPages)),
		TotalReadingMinutes:   int(st.TotalReadingSeconds / 60),
		TotalListeningMinutes: int(st.TotalListeningSeconds / 60),
		PagesEstimated:        st.PagesEstimated,
	}
	for i, d := range st.Days {
		day := statsDay{
			Date: d.Date.Format(time.DateOnly), Pages: round1(d.Pages),
			ReadingMinutes: round1(float64(d.ReadingSeconds) / 60), ListeningMinutes: round1(float64(d.ListeningSeconds) / 60),
		}
		out.Days[i] = day
		out.PagesPerDay += d.Pages
		out.ReadingMinutesPerDay += float64(d.ReadingSeconds) / 60
		out.ListeningMinutesPerDay += float64(d.ListeningSeconds) / 60
	}
	n := float64(len(st.Days))
	out.PagesPerDay, out.ReadingMinutesPerDay, out.ListeningMinutesPerDay =
		round1(out.PagesPerDay/n), round1(out.ReadingMinutesPerDay/n), round1(out.ListeningMinutesPerDay/n)
	for _, y := range st.Years {
		out.Years = append(out.Years, statsYear{Year: y.Year, Finishes: y.Finishes, Books: y.Books})
	}
	for _, e := range st.History {
		out.History = append(out.History, statsEvent{BookID: e.BookID, Title: e.Title, Event: e.Kind, Date: e.Date.Format(time.DateOnly)})
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
