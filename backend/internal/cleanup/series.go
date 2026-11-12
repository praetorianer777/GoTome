package cleanup

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The forms of a title that holds its series, as Go reads them. The SQL
// function title_series_key holds the same patterns for Postgres, with
// [[:alpha:]] for \pL: change both, and seriesWords with them.
var (
	// seriesFirst is "<series> <n> - <title>": "Sternwanderer 01 -
	// Leuchtturmwärter", "Nordlicht-Saga - 2500 - Projekt Kupfer", "Die Glasbläser,
	// Band 3: Das Schicksal der Glasbläser", "Salt Road #2: The Long Crossing".
	// The number stands apart from the series and is followed by a dash or
	// a colon and a title, so "Trap17 - …", "1957 - …" and "Stadt und
	// Land - Teil 1" are not read so.
	seriesFirst = regexp.MustCompile(`(?i)^(.*?\pL.*?)(?:\s*[,(:–—-]\s*|\s+)` +
		`(?:(?:band|bd\.|teil|folge|buch|book|vol\.|volume|nr\.|no\.|#)\s*)?` +
		`(\d{1,4}(?:\.\d)?)\s*\)?\s*[:–—-]\s*(.*\pL.*)$`)
	// seriesLast is "<title> (<series> #<n>)" with a word or # before the
	// number: "Leuchtturmwärter (Sternwanderer #1)", "Die Brückenbauer (Das
	// Salzland, Band 1)". "Die Laterne (Teil 1)" names no series.
	seriesLast = regexp.MustCompile(`(?i)^(.*?\pL.*?)\s*\(\s*([^()]*?\pL[^()]*?)(?:\s*,\s*|\s+)` +
		`(?:band|bd\.|teil|folge|buch|book|vol\.|volume|nr\.|no\.|#)\s*(\d{1,4}(?:\.\d)?)\s*\)$`)
)

// seriesWords are what a title may start with that is not a series:
// "Teil 2 - Die Rückkehr" is a part of something.
var seriesWords = []string{
	"band", "bd", "teil", "folge", "buch", "book", "vol", "volume", "nr", "no",
	"part", "chapter", "kapitel", "episode", "cd", "disc", "track",
}

// SeriesTitle is what a title says about the book's series.
type SeriesTitle struct {
	// Series is the series as the title spells it.
	Series string
	// Position is the book's place in it.
	Position float64
	// Title is the title without the series.
	Title string
}

// SeriesInTitle reads the series and the book's position in it out of a
// title such as "Sternwanderer 01 - Leuchtturmwärter", or says there is
// none. A four-digit number from 1800 to 2099 is a year ("Hamburg 1923 -
// …"), not a position.
func SeriesInTitle(title string) (SeriesTitle, bool) {
	title = strings.TrimSpace(title)
	var series, number, rest string
	if m := seriesFirst.FindStringSubmatch(title); m != nil {
		series, number, rest = m[1], m[2], m[3]
	} else if m := seriesLast.FindStringSubmatch(title); m != nil {
		series, number, rest = m[2], m[3], m[1]
	} else {
		return SeriesTitle{}, false
	}
	series, rest = strings.TrimSpace(series), strings.TrimSpace(rest)
	if len([]rune(series)) < 2 || slices.Contains(seriesWords, looseKey(series)) {
		return SeriesTitle{}, false
	}
	if len(number) == 4 && number >= "1800" && number < "2100" {
		return SeriesTitle{}, false
	}
	position, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return SeriesTitle{}, false
	}
	return SeriesTitle{Series: series, Position: position, Title: rest}, true
}

var notAlnum = regexp.MustCompile(`[^\pL\pN]+`)

// looseKey is the SQL function loose_key: lower case, runs of anything but
// letters and digits one space. Unlike catalog.Key it keeps accents, as
// Postgres has no way to take them off that is IMMUTABLE.
func looseKey(s string) string {
	return strings.TrimSpace(notAlnum.ReplaceAllString(strings.ToLower(s), " "))
}
