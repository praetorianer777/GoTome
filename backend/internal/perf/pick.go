package perf

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// sampleFiles is how many books' texts a common word and phrase are taken
// from.
const sampleFiles = 10

// TextReader gives the text of a file's chunks, by chunk ID.
type TextReader func(ctx context.Context, fileID uuid.UUID) (map[int64]string, error)

// Picked is what Pick found, with how the words of the texts were chosen.
type Picked struct {
	Params Params
	// TextFiles is how many books' texts the common word and phrase come
	// from; 0 when none could be read and the titles stood in.
	TextFiles int
}

// Pick chooses what the views are asked about from the library the scope
// sees: an author, a series, a tag and a language with many books, the
// commonest words. Nothing it returns is ever printed.
func Pick(ctx context.Context, pool *pgxpool.Pool, scope library.Scope, model string, texts TextReader) (Picked, error) {
	q := sqlc.New(pool)
	var out Picked
	p := &out.Params
	for _, pick := range []struct {
		to  *string
		get func() (string, error)
	}{
		{&p.Author, func() (string, error) {
			return q.PerfAuthor(ctx, sqlc.PerfAuthorParams{Placeholders: cleanup.PlaceholderAuthors(), Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.Series, func() (string, error) {
			return q.PerfSeries(ctx, sqlc.PerfSeriesParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.Tag, func() (string, error) {
			return q.PerfTag(ctx, sqlc.PerfTagParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.Language, func() (string, error) {
			return q.PerfLanguage(ctx, sqlc.PerfLanguageParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.Library, func() (string, error) {
			return q.PerfLibrary(ctx, sqlc.PerfLibraryParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.Book, func() (string, error) {
			return q.PerfBook(ctx, sqlc.PerfBookParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
		}},
		{&p.SimilarBook, func() (string, error) {
			return q.PerfBookWithVectors(ctx, sqlc.PerfBookWithVectorsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, Model: model})
		}},
		{&p.Collection, func() (string, error) { return q.PerfCollection(ctx, scope.Viewer) }},
		{&p.Shelf, func() (string, error) { return q.PerfSmartShelf(ctx, scope.Viewer) }},
	} {
		// A library with no series, no collection, no vectors: the views
		// about one are left out and said so.
		got, err := pick.get()
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Picked{}, errors.New("pick what to ask about: " + queryFailure(err))
		}
		*pick.to = got
	}

	words, err := q.PerfTitleWords(ctx, sqlc.PerfTitleWordsParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return Picked{}, errors.New("pick the words: " + queryFailure(err))
	}
	var inTexts []string
	for _, w := range words {
		if p.Quick == "" {
			p.Quick = w.Word
		}
		if w.InTexts {
			inTexts = append(inTexts, w.Word)
		}
	}
	if len(inTexts) > 0 {
		p.Word = inTexts[0]
	}
	if len(inTexts) > 1 {
		p.Phrase = inTexts[0] + " " + inTexts[1]
	}

	if texts != nil {
		ids, err := q.PerfSampleTextFiles(ctx, sqlc.PerfSampleTextFilesParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll, N: sampleFiles})
		if err != nil {
			return Picked{}, errors.New("pick the texts: " + queryFailure(err))
		}
		var sample [][]string
		for _, id := range ids {
			chunks, err := texts(ctx, id)
			if err != nil || len(chunks) == 0 {
				continue
			}
			for _, text := range chunks {
				sample = append(sample, wordsOf(text))
			}
			out.TextFiles++
		}
		if word, phrase := commonIn(sample); word != "" {
			p.Word = word
			if phrase != "" {
				p.Phrase = phrase
			}
		} else {
			out.TextFiles = 0
		}
	}
	return out, nil
}

// queryFailure is a database error without its text, which can hold a
// value of the library.
func queryFailure(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timed out"
	}
	return "the database refused"
}

var wordPattern = regexp.MustCompile(`\p{L}+`)

func wordsOf(text string) []string {
	found := wordPattern.FindAllString(strings.ToLower(text), -1)
	out := found[:0]
	for _, w := range found {
		if utf8.RuneCountInString(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// commonIn is the word that most chunks hold, of four letters or more,
// and the pair of words next to each other in most of them.
func commonIn(chunks [][]string) (word, phrase string) {
	words, pairs := map[string]int{}, map[string]int{}
	for _, c := range chunks {
		seenWord, seenPair := map[string]bool{}, map[string]bool{}
		for i, w := range c {
			if utf8.RuneCountInString(w) >= 4 && !seenWord[w] {
				seenWord[w] = true
				words[w]++
			}
			if i > 0 && utf8.RuneCountInString(w) >= 4 && utf8.RuneCountInString(c[i-1]) >= 4 {
				pair := c[i-1] + " " + w
				if !seenPair[pair] {
					seenPair[pair] = true
					pairs[pair]++
				}
			}
		}
	}
	return mostCommon(words), mostCommon(pairs)
}

func mostCommon(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
