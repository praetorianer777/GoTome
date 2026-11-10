package reconcile

import (
	"regexp"
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// platformAuthors are the keys of publishing platforms and services that
// tools write where an author belongs. They are publishers, so a side that
// names one as author has it moved to its publisher. The list is reviewed
// like the placeholder authors of clean-up; add to it with a test.
var platformAuthors = []string{
	"webnovel", "wattpad", "kindle direct publishing", "bookrix", "neobooks",
	"epubli", "tredition", "books on demand", "bod",
}

// PlatformAuthors returns the keys (catalog.Key) of the platforms that
// stand as author.
func PlatformAuthors() []string { return slices.Clone(platformAuthors) }

// languageAdditions are what shops put after a title beyond clean-up's
// TitleAdditions: the language as a code, an illustrated edition.
var languageAdditions = regexp.MustCompile(`(?i)\s*(\((deu|ger|eng|fra|fre|spa|ita|de|en)\)` +
	`|[-–:]\s*(illustrierte|illustrated)\s+(ausgabe|edition)|\((illustrierte|illustrated)\s+(ausgabe|edition)\))\s*$`)

var titleSeparators = []string{": ", " - ", " – ", " — "}

func (r *run) normalise(d Description, who string) Description {
	d.Title = r.cleanTitle(catalog.FieldTitle, who, d.Title)
	d.Subtitle = r.cleanTitle(catalog.FieldSubtitle, who, d.Subtitle)
	if st, ok := cleanup.SeriesInTitle(d.Title); ok &&
		(d.Series == "" || catalog.Key(d.Series) == catalog.Key(st.Series)) {
		if d.Series == "" {
			d.Series = st.Series
		}
		if d.SeriesIndex == nil {
			p := st.Position
			d.SeriesIndex = &p
		}
		d.Title = st.Title
		r.note(catalog.FieldTitle, NoteSeriesFromTitle, who, "series read out of the title")
		r.note(catalog.FieldSeries, NoteSeriesFromTitle, who, "series taken from the title")
	}

	var authors []string
	seen := map[string]bool{}
	for _, name := range d.Authors {
		name = metadata.NaturalName(strings.Join(strings.Fields(name), " "))
		key := catalog.Key(name)
		switch {
		case key == "":
			continue
		case slices.Contains(cleanup.PlaceholderAuthors(), key):
			r.note(catalog.FieldContributors, NotePlaceholderAuthor, who, "placeholder author "+quote(name)+" removed")
			continue
		case slices.Contains(platformAuthors, key):
			r.note(catalog.FieldContributors, NotePlatformAuthor, who, quote(name)+" is a platform, not an author")
			if strings.TrimSpace(d.Publisher) == "" {
				d.Publisher = name
				r.note(catalog.FieldPublisher, NotePlatformAuthor, who, quote(name)+" taken from the authors")
			}
			continue
		}
		if pk := catalog.PersonKey(name); !seen[pk] {
			seen[pk] = true
			authors = append(authors, name)
		}
	}
	d.Authors = authors
	d.Series = strings.Join(strings.Fields(d.Series), " ")
	d.Publisher = strings.Join(strings.Fields(d.Publisher), " ")
	d.Language = strings.TrimSpace(d.Language)
	d.Published = strings.TrimSpace(d.Published)
	d.Description = strings.TrimSpace(d.Description)
	return d
}

func (r *run) cleanTitle(field, who, title string) string {
	title = strings.Join(strings.Fields(title), " ")
	out := title
	for {
		next := strings.TrimSpace(languageAdditions.ReplaceAllString(cleanup.CleanTitle(out), ""))
		if next == out || next == "" {
			break
		}
		out = next
	}
	if out != title {
		r.note(field, NoteTitleAddition, who, "shop addition removed from "+quote(title))
	}
	return out
}

// alignTitles splits a subtitle off the side whose title is the other's
// title followed by a separator and more.
func (r *run) alignTitles() {
	if catalog.Key(r.a.d.Title) == catalog.Key(r.b.d.Title) ||
		r.a.d.Title == "" || r.b.d.Title == "" {
		return
	}
	if r.split(&r.a.d, r.b.d.Title, "a") {
		return
	}
	r.split(&r.b.d, r.a.d.Title, "b")
}

func (r *run) split(d *Description, target, who string) bool {
	for _, sep := range titleSeparators {
		for from := 0; ; {
			i := strings.Index(d.Title[from:], sep)
			if i < 0 {
				break
			}
			i += from
			from = i + len(sep)
			head, rest := strings.TrimSpace(d.Title[:i]), strings.TrimSpace(d.Title[i+len(sep):])
			if catalog.Key(head) != catalog.Key(target) || rest == "" {
				continue
			}
			if d.Subtitle != "" {
				rest += ": " + d.Subtitle
			}
			d.Title, d.Subtitle = head, rest
			r.note(catalog.FieldTitle, NoteSubtitleSplit, who, "subtitle split off the title")
			r.note(catalog.FieldSubtitle, NoteSubtitleSplit, who, "subtitle split off the title")
			return true
		}
	}
	return false
}

func quote(s string) string { return `"` + s + `"` }
