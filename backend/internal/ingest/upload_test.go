package ingest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUploadNames(t *testing.T) {
	cases := []struct {
		name, stem, format, folder string
	}{
		{"Emma.epub", "Emma", "epub", "Emma"},
		{"Emma.EPUB", "Emma", "epub", "Emma"},
		{`C:\Users\me\Books\Emma.epub`, "Emma", "epub", "Emma"},
		{"../../etc/passwd.epub", "passwd", "epub", "passwd"},
		{"..hidden.pdf", "hidden", "pdf", "hidden"},
		{`What? A "Book": 1*2<3>4|5.pdf`, "What A Book 1 2 3 4 5", "pdf", "What A Book 1 2 3 4 5"},
		{"tab\there\x00.mobi", "tab here", "mobi", "tab here"},
		{"Dune - Part 02.mp3", "Dune - Part 02", "mp3", "Dune"},
		{"Harry_Potter_03.m4b", "Harry_Potter_03", "m4b", "Harry Potter"},
		// A number alone in a book's name is part of its title.
		{"Fahrenheit 451.epub", "Fahrenheit 451", "epub", "Fahrenheit 451"},
		{".epub", "epub", "epub", "epub"},
		{"???.pdf", "pdf", "pdf", "pdf"},
		{"Bad\xffUTF.epub", "BadUTF", "epub", "BadUTF"},
	}
	for _, c := range cases {
		stem, format := splitName(c.name)
		folder := folderFor(stem, format)
		if stem != c.stem || format != c.format || folder != c.folder {
			t.Errorf("%q: stem %q, format %q, folder %q; want %q, %q, %q",
				c.name, stem, format, folder, c.stem, c.format, c.folder)
		}
	}
}

func TestLongNamesAreCutOnACharacter(t *testing.T) {
	name := strings.Repeat("ä", 150)
	got := safeName(name)
	if len(got) > maxNameBytes || !utf8.ValidString(got) || got == "" {
		t.Errorf("%d bytes, valid %v", len(got), utf8.ValidString(got))
	}
}
