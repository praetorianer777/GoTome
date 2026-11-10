// Package readimport brings a reading history from Goodreads or StoryGraph
// into GOtome: the export's rows are matched to books the person sees, shown
// as a preview, and then set a row at a time by a job.
package readimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// The services an export can come from.
const (
	SourceGoodreads  = "goodreads"
	SourceStoryGraph = "storygraph"
)

// MaxRows is the most rows an export may hold.
const MaxRows = 20000

// MaxBytes is the largest export that is read.
const MaxBytes = 20 << 20

var (
	// ErrUnknownFormat is a file whose columns are neither Goodreads' nor
	// StoryGraph's.
	ErrUnknownFormat = errors.New("not a Goodreads or StoryGraph export")
	ErrTooMany       = errors.New("too many rows")
)

// Row is one book of an export, as GOtome reads it.
type Row struct {
	// No is the row's place in the file, counted from 1 after the header.
	No      int
	Title   string
	Authors []string
	// ISBNs are the normalised ISBN-13s the row names.
	ISBNs []string
	// Status is reading, completed, abandoned or wishlist; empty when the
	// row's shelf is none GOtome knows (Skip then says so).
	Status string
	// Rating is 1 to 5 stars, 0 for none; Rounded says the export gave a
	// fraction of a star.
	Rating  int16
	Rounded bool
	ReadOn  *time.Time
	// Shelves are the custom shelves or tags, which become collections.
	Shelves []string
	Review  string
	// Skip is why nothing can be imported from the row.
	Skip string
}

var seriesInTitle = regexp.MustCompile(`\s*\([^()]*#[^()]*\)\s*$`)

// MatchTitle is the title the row is matched by: Goodreads writes a book's
// series after its title, in brackets.
func (r Row) MatchTitle() string {
	return strings.TrimSpace(seriesInTitle.ReplaceAllString(r.Title, ""))
}

// Parse reads an export. The source is found from the header; the file is
// read from memory and nothing of it is kept.
func Parse(data []byte) (string, []Row, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return "", nil, ErrUnknownFormat
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var source string
	switch {
	case has(col, "title", "exclusive shelf"):
		source = SourceGoodreads
	case has(col, "title", "read status"):
		source = SourceStoryGraph
	default:
		return "", nil, ErrUnknownFormat
	}
	var rows []Row
	for no := 1; ; no++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, ErrUnknownFormat
		}
		if len(rows) >= MaxRows {
			return "", nil, ErrTooMany
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		var row Row
		if source == SourceGoodreads {
			row = goodreadsRow(get)
		} else {
			row = storyGraphRow(get)
		}
		row.No = no
		if row.Title == "" {
			row.Skip = "The row has no title."
		} else if row.Skip == "" && row.Status == "" {
			row.Skip = "The row's shelf is none GOtome knows."
		}
		rows = append(rows, row)
	}
	return source, rows, nil
}

func has(col map[string]int, names ...string) bool {
	for _, n := range names {
		if _, ok := col[n]; !ok {
			return false
		}
	}
	return true
}

func goodreadsRow(get func(string) string) Row {
	row := Row{
		Title:   get("title"),
		Authors: names(get("author"), get("additional authors")),
		ISBNs:   isbns(get("isbn13"), get("isbn")),
		Review:  get("my review"),
	}
	row.Status = map[string]string{
		"read": "completed", "currently-reading": "reading", "to-read": "wishlist",
		"did-not-finish": "abandoned", "dnf": "abandoned",
	}[strings.ToLower(get("exclusive shelf"))]
	if n, err := strconv.Atoi(get("my rating")); err == nil && n >= 1 && n <= catalog.MaxRating {
		row.Rating = int16(n)
	}
	row.ReadOn = date(get("date read"))
	for _, shelf := range strings.Split(get("bookshelves"), ",") {
		shelf = strings.TrimSpace(shelf)
		switch strings.ToLower(shelf) {
		case "", "read", "currently-reading", "to-read":
		default:
			row.Shelves = append(row.Shelves, shelf)
		}
	}
	return row
}

func storyGraphRow(get func(string) string) Row {
	row := Row{
		Title:   get("title"),
		Authors: names(get("authors")),
		ISBNs:   isbns(get("isbn/uid")),
		Review:  get("review"),
	}
	row.Status = map[string]string{
		"read": "completed", "currently-reading": "reading", "to-read": "wishlist",
		"did-not-finish": "abandoned", "paused": "reading",
	}[strings.ToLower(get("read status"))]
	if f, err := strconv.ParseFloat(strings.ReplaceAll(get("star rating"), ",", "."), 64); err == nil && f > 0 {
		whole := math.Floor(f + 0.5)
		row.Rating = int16(min(max(whole, 1), catalog.MaxRating))
		row.Rounded = whole != f
	}
	row.ReadOn = date(get("last date read"))
	for _, tag := range strings.Split(get("tags"), ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			row.Shelves = append(row.Shelves, tag)
		}
	}
	return row
}

func names(lists ...string) []string {
	var out []string
	for _, list := range lists {
		for _, n := range strings.Split(list, ",") {
			if n = strings.Join(strings.Fields(n), " "); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// isbns reads the ISBNs of a row. Goodreads writes them as ="9780195001013"
// so that a spreadsheet keeps the digits; the quirk comes off first.
func isbns(fields ...string) []string {
	var out []string
	for _, f := range fields {
		f = strings.NewReplacer("=", "", `"`, "", "'", "").Replace(f)
		id, ok := catalog.NormalizeIdentifier(catalog.IDISBN, f)
		if ok && !slices.Contains(out, id.Value) {
			out = append(out, id.Value)
		}
	}
	return out
}

var dateLayouts = []string{"2006/01/02", "2006-01-02", "2006/1/2", "1/2/2006", "2 Jan 2006", "Jan 2, 2006"}

func date(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
