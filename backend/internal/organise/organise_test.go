package organise

import (
	"strings"
	"testing"
)

func rel(t *testing.T, pattern string, v Values) string {
	t.Helper()
	p, err := Parse(pattern)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := p.Layout(v).Rel(1)
	if !ok {
		t.Fatal("no room")
	}
	return r
}

func TestDefaultPatternWithAndWithoutSeries(t *testing.T) {
	with := Values{"author": "Jane Austen", "series": "Chronicles", "series_index": "02", "title": "Emma", "ext": "epub"}
	if got := rel(t, "", with); got != "Jane Austen/Chronicles/02 - Emma.epub" {
		t.Errorf("with series: %q", got)
	}
	without := Values{"author": "Jane Austen", "title": "Emma", "ext": "epub"}
	if got := rel(t, "", without); got != "Jane Austen/Emma.epub" {
		t.Errorf("without series: %q", got)
	}
}

func TestUnsafeCharactersAreReplaced(t *testing.T) {
	v := Values{"author": "A/B", "title": "What: a \"title\"? <or> |not|\x07 *", "ext": "epub"}
	got := rel(t, "{author}/{title}.{ext}", v)
	if got != "A_B/What_ a _title__ _or_ _not__ _.epub" {
		t.Errorf("got %q", got)
	}
	if strings.ContainsAny(got[strings.Index(got, "/")+1:], `\:*?"<>|`) {
		t.Errorf("unsafe characters left: %q", got)
	}
}

func TestLeadingDotsTrailingDotsAndReservedNames(t *testing.T) {
	if got := Segment("..hidden. ", 100); got != "hidden" {
		t.Errorf("dots: %q", got)
	}
	if got := Segment("CON", 100); got != "_CON" {
		t.Errorf("reserved: %q", got)
	}
	if got := Segment("nul.txt", 100); got != "_nul.txt" {
		t.Errorf("reserved with extension: %q", got)
	}
	if got := Segment("   ", 100); got != "" {
		t.Errorf("blank: %q", got)
	}
}

func TestNamesAreComposed(t *testing.T) {
	decomposed := "Café"
	if got := Clean(decomposed); got != "Café" {
		t.Errorf("not NFC: %+q", got)
	}
}

func TestLengthLimits(t *testing.T) {
	long := strings.Repeat("ä", 300)
	v := Values{"author": long, "title": long, "ext": "epub"}
	got := rel(t, "{author}/{title}.{ext}", v)
	if len(got) > MaxPathBytes {
		t.Errorf("path of %d bytes", len(got))
	}
	for _, seg := range strings.Split(got, "/") {
		if len(seg) > MaxNameBytes {
			t.Errorf("segment of %d bytes", len(seg))
		}
	}
	if !strings.HasSuffix(got, ".epub") {
		t.Errorf("extension cut off: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("ä", 60)) {
		t.Errorf("cut inside a character or too short: %q", got)
	}
}

func TestCollisionSuffixKeepsTheExtensionAndTheLimit(t *testing.T) {
	p, _ := Parse("{title}.{ext}")
	l := p.Layout(Values{"title": strings.Repeat("x", 400), "ext": "epub"})
	r, _ := l.Rel(3)
	if !strings.HasSuffix(r, " (3).epub") || len(r) > MaxNameBytes {
		t.Errorf("got %q (%d)", r, len(r))
	}
}

func TestFoldersThatLeaveNoRoom(t *testing.T) {
	l := Layout{Dirs: []string{strings.Repeat("a", 120), strings.Repeat("b", 120)}, Stem: "x", Tail: ".epub"}
	if _, ok := l.Rel(1); ok {
		t.Error("expected no room")
	}
}

func TestPartsShareAFolderInOrder(t *testing.T) {
	p, _ := Parse("{author}/{title}.{ext}")
	l := p.Layout(Values{"author": "Homer", "title": "Odyssey", "ext": "mp3"})
	a, _ := l.Part(1, 2).Rel(1)
	b, _ := l.Part(12, 2).Rel(1)
	if a != "Homer/Odyssey/01 - Odyssey.mp3" || b != "Homer/Odyssey/12 - Odyssey.mp3" {
		t.Errorf("got %q and %q", a, b)
	}
}

func TestParseRefusals(t *testing.T) {
	for _, bad := range []string{
		"{author}/{title}", "{author}/{nope}.{ext}", "/{title}.{ext}", "{title}.{ext}/", "{a/{title}.{ext}",
		"[{title}.{ext}", "{title}].{ext}", "{author}//{title}.{ext}", "[{series}/]{ext}", "a\\b/{title}.{ext}",
		"[[{series}]]{title}.{ext}",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := Parse("{author_sort}/{title} ({year}).{ext}"); err != nil {
		t.Error(err)
	}
}

func TestEmptyFolderIsLeftOut(t *testing.T) {
	got := rel(t, "{series}/{title}.{ext}", Values{"title": "Emma", "ext": "epub"})
	if got != "Emma.epub" {
		t.Errorf("got %q", got)
	}
}
