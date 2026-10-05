package textproc

import (
	"strings"
	"unicode"
)

// functionWords are each language's commonest short words. A text's language
// is the one whose words it uses most; they are common enough that a page
// says it plainly, and few enough to keep here.
var functionWords = map[string][]string{
	"en": {"the", "and", "of", "to", "in", "is", "was", "that", "it", "he", "she", "with", "for", "his", "her", "you", "not", "but", "they", "had", "have", "at", "this", "which", "from", "were", "been", "would", "their", "what"},
	"de": {"der", "die", "das", "und", "ist", "nicht", "sich", "mit", "dem", "den", "ein", "eine", "auf", "auch", "es", "sie", "ich", "zu", "war", "wie", "aber", "noch", "nach", "wenn", "doch", "hatte", "wird", "einen", "über", "sind"},
	"fr": {"le", "la", "les", "et", "est", "une", "des", "que", "qui", "dans", "pour", "pas", "sur", "il", "elle", "avec", "mais", "ce", "son", "sa", "nous", "vous", "au", "du", "été", "était", "comme", "tout", "plus", "lui"},
	"es": {"el", "los", "las", "y", "que", "es", "una", "por", "con", "para", "como", "pero", "su", "sus", "del", "lo", "se", "no", "fue", "era", "muy", "también", "cuando", "porque", "este", "esta", "sin", "sobre", "entre", "hasta"},
	"it": {"il", "di", "che", "è", "per", "una", "non", "sono", "con", "del", "della", "gli", "le", "da", "si", "ma", "come", "anche", "più", "era", "questo", "quando", "lui", "lei", "nel", "alla", "dei", "suo", "sua", "perché"},
	"nl": {"de", "het", "een", "en", "van", "is", "niet", "dat", "op", "te", "zijn", "met", "voor", "hij", "zij", "maar", "ook", "als", "aan", "er", "was", "door", "naar", "wel", "nog", "bij", "heeft", "werd", "kan", "uit"},
}

// wordLang maps a function word to the languages that use it.
var wordLang = func() map[string][]string {
	m := map[string][]string{}
	for lang, words := range functionWords {
		for _, w := range words {
			m[w] = append(m[w], lang)
		}
	}
	return m
}()

const (
	// detectWords is how many words of a text Detect reads.
	detectWords = 2000
	// minHits is how many function words a text needs before its language
	// is taken from them.
	minHits = 8
	// margin is how far ahead the leading language must be of the next.
	margin = 1.5
)

// Detect says which language the text is written in: a two-letter code of
// the languages above. A text too short or too mixed to tell gets fallback,
// cut to its primary tag ("de-AT" is "de"); with no fallback, "".
func Detect(text, fallback string) string {
	hits := map[string]int{}
	words := 0
	for w := range strings.FieldsFuncSeq(text, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		if words == detectWords {
			break
		}
		words++
		for _, lang := range wordLang[strings.ToLower(w)] {
			hits[lang]++
		}
	}
	best, second := "", 0
	for lang, n := range hits {
		switch {
		case best == "" || n > hits[best] || (n == hits[best] && lang < best):
			second = max(second, hits[best])
			best = lang
		case n > second:
			second = n
		}
	}
	if best != "" && hits[best] >= minHits && float64(hits[best]) >= margin*float64(second) {
		return best
	}
	tag, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(fallback)), "-")
	tag, _, _ = strings.Cut(tag, "_")
	return tag
}
