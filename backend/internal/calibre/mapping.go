package calibre

import (
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/language"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/format/markup"
)

// What a custom column may become, chosen per column.
const (
	ChoiceIgnore = "ignore"
	// ChoiceStatus makes a yes/no or date column the importing person's
	// reading status: a yes or a date is "completed", on that date.
	ChoiceStatus = "status"
	// ChoiceTags makes each value a tag; a yes/no column gives its name.
	ChoiceTags = "tags"
)

// Choices lists what a column of the datatype may become.
func Choices(datatype string) []string {
	switch datatype {
	case TypeBool, TypeDatetime:
		return []string{ChoiceIgnore, ChoiceStatus, ChoiceTags}
	case TypeComments, TypeComposite:
		return []string{ChoiceIgnore}
	}
	return []string{ChoiceIgnore, ChoiceTags}
}

// MaxDescription is as long as GOtome keeps a description, in characters.
const MaxDescription = 50_000

// Mapped is what a Calibre book says, in GOtome's terms. An empty field says
// nothing.
type Mapped struct {
	Title        string
	Description  string
	Language     string
	Published    string
	Publisher    string
	Series       string
	SeriesIndex  *float64
	Contributors []catalog.NewContributor
	Tags         []string
	Identifiers  []catalog.Identifier
	// Cover is the cover file's path below the library's folder.
	Cover string
	// Reading is the importing person's: the rating if asked for, the status
	// a custom column gives.
	Reading catalog.ImportedReading
}

// Options are the person's choices for an import.
type Options struct {
	Ratings bool
	// Columns maps a custom column's label to a Choice.
	Columns map[string]string
}

// Map turns a Calibre book into what GOtome stores.
func Map(b Book, columns []Column, opts Options) Mapped {
	m := Mapped{Title: strings.TrimSpace(b.Title), Series: strings.TrimSpace(b.Series), Publisher: strings.TrimSpace(b.Publisher)}
	if m.Series != "" && b.SeriesIndex > 0 {
		index := b.SeriesIndex
		m.SeriesIndex = &index
	}
	for _, a := range b.Authors {
		m.Contributors = append(m.Contributors, catalog.NewContributor{Name: a.Name, SortName: a.Sort, Role: catalog.RoleAuthor})
	}
	m.Tags = append(m.Tags, b.Tags...)
	if text, _ := markup.Text([]byte(b.Comments)); strings.TrimSpace(text) != "" {
		text = strings.TrimSpace(text)
		if utf8.RuneCountInString(text) > MaxDescription {
			text = string([]rune(text)[:MaxDescription])
		}
		m.Description = text
	}
	if len(b.Languages) > 0 {
		m.Language = LanguageTag(b.Languages[0])
	}
	if t, err := parseTime(b.PubDate); err == nil && t.Year() > 1000 {
		m.Published = t.UTC().Format(time.DateOnly)
	}
	for _, id := range b.Identifiers {
		if ident, ok := catalog.NormalizeIdentifier(id.Type, id.Value); ok {
			m.Identifiers = append(m.Identifiers, ident)
		}
	}
	if b.HasCover && b.Path != "" {
		m.Cover = path.Join(b.Path, "cover.jpg")
	}
	if opts.Ratings && b.Rating > 0 {
		m.Reading.Rating = Stars(b.Rating)
	}
	for _, c := range columns {
		values := b.Custom[c.Label]
		switch opts.Columns[c.Label] {
		case ChoiceTags:
			for _, v := range values {
				if c.Datatype == TypeBool {
					if v == "1" {
						m.Tags = append(m.Tags, c.Name)
					}
					continue
				}
				m.Tags = append(m.Tags, dateOnly(c, v))
			}
		case ChoiceStatus:
			if len(values) == 0 {
				continue
			}
			switch c.Datatype {
			case TypeBool:
				if values[0] == "1" {
					m.Reading.Status = catalog.StatusCompleted
				}
			case TypeDatetime:
				if t, err := parseTime(values[0]); err == nil && t.Year() > 1000 {
					m.Reading.Status = catalog.StatusCompleted
					day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
					m.Reading.FinishedOn = &day
				}
			}
		}
	}
	return m
}

func dateOnly(c Column, v string) string {
	if c.Datatype == TypeDatetime {
		if t, err := parseTime(v); err == nil {
			return t.UTC().Format(time.DateOnly)
		}
	}
	return v
}

// Stars turns Calibre's rating of 0 to 10 into whole stars; a half star
// rounds up.
func Stars(rating int) int16 {
	return int16(min((rating+1)/2, catalog.MaxRating))
}

// bibliographic maps the ISO 639-2 codes that differ between its two sets to
// the terminology code Calibre stores, which x/text reads.
var bibliographic = map[string]string{
	"ger": "deu", "fre": "fra", "dut": "nld", "chi": "zho", "cze": "ces", "gre": "ell",
	"ice": "isl", "rum": "ron", "slo": "slk", "wel": "cym", "per": "fas", "arm": "hye",
	"baq": "eus", "geo": "kat", "mac": "mkd", "mao": "mri", "may": "msa", "bur": "mya",
	"alb": "sqi", "tib": "bod",
}

// LanguageTag turns Calibre's three-letter language code into a BCP 47 tag,
// or "" for one it does not know.
func LanguageTag(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if to, ok := bibliographic[code]; ok {
		code = to
	}
	base, err := language.ParseBase(code)
	if err != nil {
		return ""
	}
	return base.String()
}

// parseTime reads a timestamp as the driver or Calibre writes it.
func parseTime(s string) (time.Time, error) {
	var err error
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05", "2006-01-02"} {
		var t time.Time
		if t, err = time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}
