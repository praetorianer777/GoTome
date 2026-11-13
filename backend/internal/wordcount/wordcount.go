// Package wordcount counts the words of a book's text, for what learns from
// whole texts (genres, #526), and stores the counts compactly.
package wordcount

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version names what is counted and how it is stored. Raise it with a change
// to either: files counted with another version are counted again.
const Version = 1

const (
	minLetters = 3
	maxLetters = 25
	maxCount   = 65535
)

// Counts is how often each lower-cased word occurs. Words are surface
// forms, not stems: a stem can be applied later to the stored words without
// reading any file again, whereas stemmed counts cannot be undone and would
// tie the data to one stemmer.
type Counts map[string]uint32

const (
	gutenbergStart = "*** start of"
	gutenbergEnd   = "*** end of"
)

// Count counts the words of a text given in pieces, in order. Words are
// runs of letters of 3 to 25 letters. Project Gutenberg's header and
// licence, which say nothing of a book's genre, are left out where their
// markers are found; names are not, as they cannot be told from other words
// without a list for each language, and the learning's minimum document
// frequency drops most of them.
func Count(texts []string) Counts {
	m := Counts{}
	from, to := gutenbergBounds(texts)
	for i, t := range texts {
		if i < from.chunk || i > to.chunk {
			continue
		}
		if i == from.chunk {
			t = t[from.offset:]
			if i == to.chunk {
				t = t[:to.offset-from.offset]
			}
		} else if i == to.chunk {
			t = t[:to.offset]
		}
		add(m, t)
	}
	return m
}

type position struct{ chunk, offset int }

// gutenbergBounds finds the start marker in the first chunks and the end
// marker in the last ones; absent a marker the whole text counts.
func gutenbergBounds(texts []string) (from, to position) {
	to = position{len(texts) - 1, 0}
	if len(texts) > 0 {
		to.offset = len(texts[len(texts)-1])
	}
	for i := 0; i < len(texts) && i < 3; i++ {
		if o, ok := afterMarkerLine(texts[i], gutenbergStart); ok {
			from = position{i, o}
			break
		}
	}
	for i := len(texts) - 1; i >= 0 && i >= len(texts)-3; i-- {
		if o := markerLineStart(texts[i], gutenbergEnd); o >= 0 && (i > from.chunk || o >= from.offset) {
			to = position{i, o}
			break
		}
	}
	if to.chunk < from.chunk || (to.chunk == from.chunk && to.offset < from.offset) {
		return position{}, position{len(texts) - 1, len(texts[len(texts)-1])}
	}
	return from, to
}

func markerLineStart(t, marker string) int {
	l := strings.ToLower(t)
	i := strings.Index(l, marker)
	if i < 0 {
		return -1
	}
	return i
}

func afterMarkerLine(t, marker string) (int, bool) {
	i := markerLineStart(t, marker)
	if i < 0 {
		return 0, false
	}
	nl := strings.IndexByte(t[i:], '\n')
	if nl < 0 {
		return len(t), true
	}
	return i + nl + 1, true
}

func add(m Counts, text string) {
	var buf [4 * maxLetters]byte
	n, letters := 0, 0
	flush := func() {
		if letters >= minLetters && letters <= maxLetters {
			if c := m[string(buf[:n])]; c < maxCount {
				m[string(buf[:n])] = c + 1
			}
		}
		n, letters = 0, 0
	}
	for i := 0; i < len(text); {
		b := text[i]
		var r rune
		size := 1
		if b < utf8.RuneSelf {
			r = rune(b)
		} else {
			r, size = utf8.DecodeRuneInString(text[i:])
		}
		i += size
		if !unicode.IsLetter(r) {
			if letters > 0 {
				flush()
			}
			continue
		}
		letters++
		if letters > maxLetters {
			continue
		}
		if r < utf8.RuneSelf {
			if 'A' <= r && r <= 'Z' {
				r += 'a' - 'A'
			}
			buf[n] = byte(r)
			n++
		} else {
			n += utf8.EncodeRune(buf[n:], unicode.ToLower(r))
		}
	}
	if letters > 0 {
		flush()
	}
}

// Encode stores counts as the words in order, each as the length it shares
// with the one before, its rest and its count, deflated.
func Encode(c Counts) []byte {
	words := make([]string, 0, len(c))
	for w := range c {
		words = append(words, w)
	}
	sort.Strings(words)
	var raw bytes.Buffer
	var tmp [binary.MaxVarintLen64]byte
	put := func(v uint64) { raw.Write(tmp[:binary.PutUvarint(tmp[:], v)]) }
	put(uint64(len(words)))
	prev := ""
	for _, w := range words {
		k := 0
		for k < len(prev) && k < len(w) && prev[k] == w[k] {
			k++
		}
		put(uint64(k))
		put(uint64(len(w) - k))
		raw.WriteString(w[k:])
		put(uint64(c[w]))
		prev = w
	}
	var out bytes.Buffer
	zw, _ := flate.NewWriter(&out, flate.BestSpeed)
	zw.Write(raw.Bytes())
	zw.Close()
	return out.Bytes()
}

// Decode reads what Encode wrote.
func Decode(b []byte) (Counts, error) {
	raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(b)))
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(raw)
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	if n > uint64(len(raw)) {
		return nil, errors.New("wordcount: bad length")
	}
	c := make(Counts, n)
	prev := ""
	for range n {
		k, err1 := binary.ReadUvarint(r)
		l, err2 := binary.ReadUvarint(r)
		if err1 != nil || err2 != nil || k > uint64(len(prev)) || l > uint64(r.Len()) {
			return nil, errors.New("wordcount: bad entry")
		}
		rest := make([]byte, l)
		if _, err := io.ReadFull(r, rest); err != nil {
			return nil, err
		}
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, err
		}
		w := prev[:k] + string(rest)
		c[w] = uint32(v)
		prev = w
	}
	return c, nil
}
