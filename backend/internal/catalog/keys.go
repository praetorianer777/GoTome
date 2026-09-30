package catalog

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Key is a name or title as it is compared: lower case, accents removed,
// punctuation dropped, whitespace collapsed. "Brontë, Charlotte" and
// "bronte charlotte" have the same key; so do "Tolkien, J.R.R." and
// "Tolkien J R R".
func Key(s string) string {
	var b strings.Builder
	space := false
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// A combining mark: the accent NFKD split off its letter.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteString(foldRune(unicode.ToLower(r)))
		default:
			space = true
		}
	}
	return b.String()
}

// foldRune spells out the letters that NFKD leaves whole but that people type
// without: a search for "strasse" must find "Straße".
func foldRune(r rune) string {
	switch r {
	case 'ß':
		return "ss"
	case 'æ':
		return "ae"
	case 'œ':
		return "oe"
	case 'ø':
		return "o"
	case 'đ', 'ð':
		return "d"
	case 'ł':
		return "l"
	case 'þ':
		return "th"
	}
	return string(r)
}

// articles are the leading words a title is not sorted by, per language. A
// title is only rearranged when its language is known: "Die Hard" is English.
var articles = map[string][]string{
	"en": {"the", "a", "an"},
	"de": {"der", "die", "das", "ein", "eine", "einer", "eines", "einem", "einen"},
	"fr": {"le", "la", "les", "un", "une", "des"},
	"es": {"el", "la", "los", "las", "un", "una", "unos", "unas"},
	"it": {"il", "lo", "la", "i", "gli", "le", "un", "uno", "una"},
	"nl": {"de", "het", "een"},
}

// SortTitle is the title as lists order it: a leading article moves to the
// end, so "The Hobbit" sorts under H as "Hobbit, The".
func SortTitle(title, language string) string {
	title = strings.Join(strings.Fields(title), " ")
	lang, _, _ := strings.Cut(strings.ToLower(language), "-")

	// French and Italian write some articles onto the word: "L'Étranger".
	if lang == "fr" || lang == "it" {
		for _, elided := range []string{"l'", "l’"} {
			if len(title) > len(elided) && strings.EqualFold(title[:len(elided)], elided) {
				return title[len(elided):] + ", " + title[:len(elided)]
			}
		}
	}

	first, rest, found := strings.Cut(title, " ")
	if !found {
		return title
	}
	for _, article := range articles[lang] {
		if strings.EqualFold(first, article) {
			return rest + ", " + first
		}
	}
	return title
}

// particles belong to the surname they stand before: "Ursula K. Le Guin" is
// filed under "Le Guin", "Ludwig van Beethoven" under "van Beethoven".
var particles = map[string]bool{
	"van": true, "von": true, "de": true, "der": true, "den": true, "del": true,
	"della": true, "di": true, "da": true, "du": true, "la": true, "le": true,
	"ten": true, "ter": true, "zu": true, "af": true, "st": true, "st.": true,
}

// suffixes follow a name without being part of the surname.
var suffixes = map[string]bool{"jr": true, "jr.": true, "sr": true, "sr.": true, "ii": true, "iii": true, "iv": true}

// SortName turns a name as it is credited into the form lists order it by:
// "Jane Austen" becomes "Austen, Jane". It is a guess, used only when the
// file does not say how the name is filed: names do not all put the family
// name last, and a name that already has a comma is left as it is.
func SortName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || strings.Contains(name, ",") {
		return name
	}
	words := strings.Split(name, " ")
	suffix := ""
	if len(words) > 2 && suffixes[strings.ToLower(words[len(words)-1])] {
		suffix = " " + words[len(words)-1]
		words = words[:len(words)-1]
	}
	if len(words) < 2 {
		return name
	}
	// The surname is the last word and any particles directly before it.
	start := len(words) - 1
	for start > 1 && particles[strings.ToLower(words[start-1])] {
		start--
	}
	return strings.Join(words[start:], " ") + ", " + strings.Join(words[:start], " ") + suffix
}
