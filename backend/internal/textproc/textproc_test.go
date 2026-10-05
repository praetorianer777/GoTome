package textproc

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	english = "It was the best of times, it was the worst of times. The house stood at the end of the road, and the children had played in its garden for years with their dog. "
	german  = "Es war einmal ein Müller, der war arm, aber er hatte eine schöne Tochter. Nun traf es sich, dass er mit dem König zu sprechen kam, und um sich ein Ansehen zu geben, sagte er zu ihm. "
	french  = "Il était une fois un roi et une reine qui étaient bien fâchés de ne point avoir d'enfants, et ce n'est pas peu dire. Elle avait tout ce qu'il fallait pour être heureuse dans la vie. "
)

func TestChunksReproduceTheTextInOrder(t *testing.T) {
	para := strings.Repeat(english, 20) // about 3,400 characters
	sections := []Section{
		{Label: "Chapter One", Text: para + "\n\n" + para},
		{Label: "Chapter Two", Text: para + "\n\n" + para + "\n\n" + para},
	}
	chunks := Split(sections, Options{CharsPerPage: 1800})
	whole := Normalize(sections[0].Text) + "\n\n" + Normalize(sections[1].Text)
	var rebuilt []string
	for i, c := range chunks {
		if c.Position != i {
			t.Errorf("chunk %d has position %d", i, c.Position)
		}
		if n := utf8.RuneCountInString(c.Text); n > ChunkSize {
			t.Errorf("chunk %d has %d characters", i, n)
		}
		if got := string([]rune(whole)[c.Offset : c.Offset+utf8.RuneCountInString(c.Text)]); got != c.Text {
			t.Errorf("chunk %d is not the text at its offset %d", i, c.Offset)
		}
		rebuilt = append(rebuilt, c.Text)
	}
	if strings.Join(rebuilt, "\n\n") != whole {
		t.Error("the chunks joined are not the text")
	}
	if chunks[0].Chapter != "Chapter One" || chunks[len(chunks)-1].Chapter != "Chapter Two" {
		t.Errorf("chapters %q … %q", chunks[0].Chapter, chunks[len(chunks)-1].Chapter)
	}
	if chunks[0].PageFrom != 1 || chunks[1].PageFrom != chunks[1].Offset/1800+1 || chunks[len(chunks)-1].PageTo != (utf8.RuneCountInString(whole)-1)/1800+1 {
		t.Errorf("estimated pages %+v", pages(chunks))
	}
}

func TestPagesComeFromSectionsThatArePages(t *testing.T) {
	var sections []Section
	for p := 1; p <= 10; p++ {
		sections = append(sections, Section{Label: "", Page: p, Text: strings.Repeat(english, 6)})
	}
	chunks := Split(sections, Options{CharsPerPage: 1800})
	if chunks[0].PageFrom != 1 || chunks[len(chunks)-1].PageTo != 10 {
		t.Errorf("pages %+v", pages(chunks))
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].PageFrom < chunks[i-1].PageTo {
			t.Errorf("chunk %d starts on page %d before the last ended on %d", i, chunks[i].PageFrom, chunks[i-1].PageTo)
		}
	}
}

func TestALongParagraphIsCutAtSpaces(t *testing.T) {
	text := strings.Repeat("Wörter ", 5000)
	chunks := Split([]Section{{Text: text}}, Options{})
	if len(chunks) != 5 {
		t.Fatalf("%d chunks, want 5", len(chunks))
	}
	total := 0
	for _, c := range chunks {
		if !strings.HasPrefix(c.Text, "Wörter") || !utf8.ValidString(c.Text) {
			t.Errorf("a chunk starts %q", c.Text[:10])
		}
		total += strings.Count(c.Text, "Wörter")
		if c.PageFrom != 0 {
			t.Errorf("pages without a way to tell them: %d", c.PageFrom)
		}
	}
	if total != 5000 {
		t.Errorf("%d words kept", total)
	}
}

func TestEachChunkHasItsLanguage(t *testing.T) {
	chunks := Split([]Section{
		{Text: strings.Repeat(german, 50)},
		{Text: strings.Repeat(english, 50)},
		{Text: strings.Repeat(french, 50)},
		{Text: "Ende."},
	}, Options{LangHint: "de-DE"})
	var langs []string
	for _, c := range chunks {
		langs = append(langs, c.Lang)
	}
	if langs[0] != "de" || !slices.Contains(langs, "en") || !slices.Contains(langs, "fr") {
		t.Errorf("languages %v", langs)
	}
}

func TestDetect(t *testing.T) {
	for _, tc := range []struct{ text, fallback, want string }{
		{strings.Repeat(english, 3), "", "en"},
		{strings.Repeat(german, 3), "en", "de"},
		{strings.Repeat(french, 3), "", "fr"},
		{"Ende.", "de-AT", "de"},
		{"1, 2, 3", "pt_BR", "pt"},
		{"", "", ""},
	} {
		if got := Detect(tc.text, tc.fallback); got != tc.want {
			t.Errorf("Detect(%.20q, %q) = %q, want %q", tc.text, tc.fallback, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	in := "  First line  \r\nsecond\tline\x00\n\n\n\n  Next   paragraph é\n"
	want := "First line\nsecond\tline\n\nNext   paragraph é"
	if got := Normalize(in); got != want {
		t.Errorf("Normalize = %q, want %q", got, want)
	}
}

func pages(chunks []Chunk) [][2]int {
	var out [][2]int
	for _, c := range chunks {
		out = append(out, [2]int{c.PageFrom, c.PageTo})
	}
	return out
}
