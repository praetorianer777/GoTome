package main

import (
	"strings"
	"unicode/utf8"
)

const ChunkSize = 8000

// Chunks cuts text at paragraph breaks into pieces of about ChunkSize
// characters, as the search spike and the app do.
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
