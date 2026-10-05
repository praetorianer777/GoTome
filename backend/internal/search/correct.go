package search

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	// sparseBooks is how few books the query as written may find before
	// its typos are repaired too.
	sparseBooks = 3
	// candidates is how many words of the text a typo may stand for.
	candidates = 3
	// maxVariants is how many phrases a phrase with typos may stand for.
	maxVariants = 9
	// shortWord is the longest word that may be one edit off, not two.
	shortWord = 4
	// minSimilarity is the trigram similarity a word of the text needs to
	// be looked at as what a typo meant. Lower than pg_trgm's default: a
	// short word with two letters swapped shares few trigrams with itself.
	minSimilarity = "0.2"
)

// correct takes every word of the query that no chunk holds for a typo of
// the words of the text that are fewest edits from it. A free word becomes a
// group of them, a phrase the phrases its words may have meant. It says
// whether anything changed.
//
// The words come from search_words, which holds those of every library; the
// corrected query is searched like any other, within what the scope may
// see, and the words are never shown.
func correct(ctx context.Context, tx pgx.Tx, p parsed) (parsed, bool, error) {
	if _, err := tx.Exec(ctx, "SELECT set_config('pg_trgm.similarity_threshold', $1, true)", minSimilarity); err != nil {
		return p, false, err
	}
	out := parsed{Groups: p.Groups}
	changed := false
	var kept []string
	for w := range strings.FieldsSeq(p.Words) {
		alts, err := nearest(ctx, tx, w)
		if err != nil {
			return p, false, err
		}
		if alts == nil {
			kept = append(kept, w)
			continue
		}
		out.Groups = append(out.Groups, alts)
		changed = true
	}
	out.Words = strings.Join(kept, " ")
	for _, ph := range p.Phrases {
		var words [][]string
		fixed := false
		for w := range strings.FieldsSeq(ph) {
			alts, err := nearest(ctx, tx, w)
			if err != nil {
				return p, false, err
			}
			if alts == nil {
				alts = []string{w}
			} else {
				fixed = true
			}
			words = append(words, alts)
		}
		if !fixed {
			out.Phrases = append(out.Phrases, ph)
			continue
		}
		out.Variants = append(out.Variants, variants(words))
		changed = true
	}
	return out, changed, nil
}

// nearest is the words of the text nearest to w, nearest first, when w is
// not one of them itself; nil when it is, when it is not a plain word, or
// when none is near enough.
func nearest(ctx context.Context, tx pgx.Tx, w string) ([]string, error) {
	w = strings.ToLower(w)
	if !plainWord(w) {
		return nil, nil
	}
	var known bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM search_words WHERE word = $1)", w).Scan(&known); err != nil || known {
		return nil, err
	}
	edits := 2
	if utf8.RuneCountInString(w) <= shortWord {
		edits = 1
	}
	rows, err := tx.Query(ctx, `
		SELECT word FROM search_words
		WHERE word % $1 AND levenshtein(word, $1) <= $2
		ORDER BY levenshtein(word, $1), similarity(word, $1) DESC, word
		LIMIT $3`, w, edits, candidates)
	if err != nil {
		return nil, err
	}
	words, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if len(words) == 0 {
		return nil, err
	}
	return words, err
}

// plainWord is a word of letters alone, at least three: numbers, codes and
// stray letters are not corrected.
func plainWord(w string) bool {
	n := 0
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return false
		}
		n++
	}
	return n >= 3
}

// variants are the phrases made of one choice from each word's
// alternatives, nearest choices first, at most maxVariants of them.
func variants(words [][]string) []string {
	out := []string{""}
	for _, alts := range words {
		var next []string
		for _, prefix := range out {
			for _, a := range alts {
				if len(next) == maxVariants {
					break
				}
				next = append(next, strings.TrimSpace(prefix+" "+a))
			}
		}
		out = next
	}
	return out
}
