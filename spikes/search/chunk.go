package main

import (
	"strings"
	"unicode/utf8"
)

// ChunkSize is the length a chunk grows to, in characters, before the next
// paragraph starts a new one.
const ChunkSize = 8000

// Chunks cuts text into pieces of at most about ChunkSize characters, ending
// each at a paragraph break. A paragraph longer than that is cut at a space.
func Chunks(text string) []string {
	var out []string
	var cur strings.Builder
	n := 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
		n = 0
	}
	for para := range strings.SplitSeq(text, "\n\n") {
		l := utf8.RuneCountInString(para)
		if n > 0 && n+l > ChunkSize {
			flush()
		}
		for l > ChunkSize {
			head, rest := cutAtSpace(para, ChunkSize)
			cur.WriteString(head)
			flush()
			para = rest
			l = utf8.RuneCountInString(para)
		}
		if n > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
		n += l
	}
	flush()
	return out
}

// cutAtSpace splits s after at most max characters, at the last space before
// that if there is one.
func cutAtSpace(s string, max int) (string, string) {
	i, count := 0, 0
	for i < len(s) && count < max {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		count++
	}
	if sp := strings.LastIndexByte(s[:i], ' '); sp > 0 {
		return s[:sp], s[sp+1:]
	}
	return s[:i], s[i:]
}
