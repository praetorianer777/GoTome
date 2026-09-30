package catalog

import (
	"testing"
	"time"
)

func TestKey(t *testing.T) {
	same := [][]string{
		{"Brontë, Charlotte", "bronte charlotte", "BRONTE  CHARLOTTE", "Bronte; Charlotte."},
		{"J.R.R. Tolkien", "J. R. R. Tolkien", "j r r tolkien"},
		{"Straße", "Strasse", "STRASSE"},
		{"Æsop’s Fables", "Aesop's Fables", "aesop s fables"},
		{"The Hitchhiker's Guide—to the Galaxy", "the hitchhiker s guide to the galaxy"},
		// A decomposed e with a combining accent, and the single character.
		{"Cafe" + string(rune(0x0301)), "Café", "cafe"},
		{"Война и мир", "война и мир"},
	}
	for _, group := range same {
		want := Key(group[0])
		if want == "" {
			t.Errorf("Key(%q) is empty", group[0])
		}
		for _, s := range group[1:] {
			if got := Key(s); got != want {
				t.Errorf("Key(%q) = %q, but Key(%q) = %q", s, got, group[0], want)
			}
		}
	}
	if Key("Dune") == Key("Dune Messiah") {
		t.Error("different titles share a key")
	}
	for _, empty := range []string{"", "   ", "—", "!?"} {
		if got := Key(empty); got != "" {
			t.Errorf("Key(%q) = %q, want empty", empty, got)
		}
	}
}

func TestSortTitle(t *testing.T) {
	cases := []struct{ title, language, want string }{
		{"The Hobbit", "en", "Hobbit, The"},
		{"A Game of Thrones", "en-US", "Game of Thrones, A"},
		{"An Unexpected Journey", "EN", "Unexpected Journey, An"},
		{"Der Prozess", "de", "Prozess, Der"},
		{"Die Verwandlung", "de-AT", "Verwandlung, Die"},
		{"Les Misérables", "fr", "Misérables, Les"},
		{"L'Étranger", "fr", "Étranger, L'"},
		{"  The   Two  Towers ", "en", "Two Towers, The"},
		// The language decides: this is not a German article.
		{"Die Hard", "en", "Die Hard"},
		{"The Hobbit", "", "The Hobbit"},
		{"The Hobbit", "de", "The Hobbit"},
		// An article alone, or as the start of a longer word, is the title.
		{"The", "en", "The"},
		{"Theology", "en", "Theology"},
		{"Dune", "en", "Dune"},
	}
	for _, c := range cases {
		if got := SortTitle(c.title, c.language); got != c.want {
			t.Errorf("SortTitle(%q, %q) = %q, want %q", c.title, c.language, got, c.want)
		}
	}
}

func TestSortName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"Jane Austen", "Austen, Jane"},
		{"J. R. R. Tolkien", "Tolkien, J. R. R."},
		{"Ursula K. Le Guin", "Le Guin, Ursula K."},
		{"Ludwig van Beethoven", "van Beethoven, Ludwig"},
		{"Johann Wolfgang von Goethe", "von Goethe, Johann Wolfgang"},
		{"Martin Luther King Jr.", "King, Martin Luther Jr."},
		{"  Jane   Austen ", "Austen, Jane"},
		// Already filed, a single name, or nothing: left alone.
		{"Austen, Jane", "Austen, Jane"},
		{"Voltaire", "Voltaire"},
		{"", ""},
		// A particle is not a surname on its own.
		{"Van Morrison", "Morrison, Van"},
	}
	for _, c := range cases {
		if got := SortName(c.name); got != c.want {
			t.Errorf("SortName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNormalizeISBN(t *testing.T) {
	valid := map[string]string{
		"978-3-16-148410-0": "9783161484100",
		"9783161484100":     "9783161484100",
		"0-306-40615-2":     "9780306406157",
		"0306406152":        "9780306406157",
		"080442957X":        "9780804429573",
		"0 8044 2957 x":     "9780804429573",
	}
	for in, want := range valid {
		if got, ok := NormalizeISBN(in); !ok || got != want {
			t.Errorf("NormalizeISBN(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", "123", "9783161484101", "0306406153", "978316148410X", "X306406152",
		"97831614841000", "isbn 9783161484100", "B000FC1PJI",
	} {
		if got, ok := NormalizeISBN(in); ok {
			t.Errorf("NormalizeISBN(%q) = %q, want it refused", in, got)
		}
	}
}

func TestNormalizeIdentifier(t *testing.T) {
	cases := []struct {
		scheme, value string
		want          Identifier
		ok            bool
	}{
		{"ISBN", "0-306-40615-2", Identifier{IDISBN, "9780306406157"}, true},
		{"isbn13", "978-3-16-148410-0", Identifier{IDISBN, "9783161484100"}, true},
		{"", "9783161484100", Identifier{IDISBN, "9783161484100"}, true},
		{"isbn", "not-an-isbn", Identifier{IDISBN, ""}, false},
		{"doi", "https://doi.org/10.1000/XYZ123", Identifier{IDDOI, "10.1000/xyz123"}, true},
		{"DOI", "doi:10.1000/xyz123", Identifier{IDDOI, "10.1000/xyz123"}, true},
		{"doi", "11.1000/x", Identifier{IDDOI, "11.1000/x"}, false},
		{"mobi-asin", "b000fc1pji", Identifier{IDASIN, "B000FC1PJI"}, true},
		{"uuid", "urn:uuid:123E4567-E89B-12D3-A456-426614174000", Identifier{IDUUID, "123e4567-e89b-12d3-a456-426614174000"}, true},
		{"", "B000FC1PJI", Identifier{IDOther, "B000FC1PJI"}, true},
		{"calibre", "42", Identifier{IDOther, "calibre:42"}, true},
		{"google", "abc", Identifier{IDGoogle, "abc"}, true},
		{"isbn", "  ", Identifier{}, false},
	}
	for _, c := range cases {
		got, ok := NormalizeIdentifier(c.scheme, c.value)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("NormalizeIdentifier(%q, %q) = %+v, %v, want %+v, %v", c.scheme, c.value, got, ok, c.want, c.ok)
		}
	}
}

func TestParsePublished(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		in        string
		want      time.Time
		precision string
	}{
		{"2010", day(2010, 1, 1), PrecisionYear},
		{"2010-08", day(2010, 8, 1), PrecisionMonth},
		{"2010-08-31", day(2010, 8, 31), PrecisionDay},
		{" 2010-08-31T00:00:00+02:00 ", day(2010, 8, 31), PrecisionDay},
		{"2010-08-31T12:30:00Z", day(2010, 8, 31), PrecisionDay},
		{"1915", day(1915, 1, 1), PrecisionYear},
	}
	for _, c := range cases {
		got, precision, ok := ParsePublished(c.in)
		if !ok || !got.Equal(c.want) || precision != c.precision {
			t.Errorf("ParsePublished(%q) = %v, %q, %v, want %v, %q", c.in, got, precision, ok, c.want, c.precision)
		}
	}
	for _, in := range []string{"", "August 2010", "0000", "2010-13-01", "31.08.2010", "unknown"} {
		if got, _, ok := ParsePublished(in); ok {
			t.Errorf("ParsePublished(%q) = %v, want it refused", in, got)
		}
	}
}

func TestKindOf(t *testing.T) {
	for format, want := range map[string]string{"epub": KindEbook, "PDF": KindEbook, "m4b": KindAudio, "mp3": KindAudio} {
		if got, ok := KindOf(format); !ok || got != want {
			t.Errorf("KindOf(%q) = %q, %v, want %q", format, got, ok, want)
		}
	}
	if _, ok := KindOf("docx"); ok {
		t.Error("docx is a known format")
	}
}
