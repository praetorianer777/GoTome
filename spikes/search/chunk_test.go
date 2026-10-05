package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunksKeepParagraphsWhole(t *testing.T) {
	para := strings.Repeat("word ", 1000) // 5,000 characters
	text := strings.Join([]string{para, para, para}, "\n\n")
	got := Chunks(text)
	if len(got) != 3 {
		t.Fatalf("got %d chunks, want 3", len(got))
	}
	for _, c := range got {
		if strings.TrimSpace(c) != strings.TrimSpace(para) {
			t.Errorf("a paragraph was cut: %d characters", len(c))
		}
	}
}

func TestChunksJoinShortParagraphs(t *testing.T) {
	text := strings.Repeat("A short paragraph.\n\n", 100)
	got := Chunks(text)
	if len(got) != 1 {
		t.Fatalf("got %d chunks, want 1", len(got))
	}
}

func TestChunksCutLongParagraphsAtSpaces(t *testing.T) {
	text := strings.Repeat("Wörter ", 5000)
	got := Chunks(text)
	if len(got) != 5 {
		t.Fatalf("got %d chunks, want 5", len(got))
	}
	var total int
	for _, c := range got {
		if n := utf8.RuneCountInString(c); n > ChunkSize {
			t.Errorf("chunk of %d characters", n)
		}
		if !strings.HasPrefix(c, "Wörter") || !utf8.ValidString(c) {
			t.Errorf("chunk cut inside a word: %q", c[:20])
		}
		total += strings.Count(c, "Wörter")
	}
	if total != 5000 {
		t.Errorf("%d words kept, want 5000", total)
	}
}
