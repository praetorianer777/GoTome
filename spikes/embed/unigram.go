package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Unigram is a SentencePiece Unigram tokenizer as multilingual-e5's
// tokenizer.json describes it, written here because the Go tokenizer hugot
// uses takes the longest piece first instead of the likeliest segmentation,
// and so splits words differently from the model's own tokenizer.
type Unigram struct {
	pieces   map[string]piece
	maxRunes int
	unk      piece
	bos, eos int
}

type piece struct {
	id    int
	score float64
}

// unkPenalty is what an unknown character costs below the least likely
// piece, as in SentencePiece and Hugging Face's tokenizers.
const unkPenalty = 10

// LoadUnigram reads the Unigram model of a tokenizer.json.
func LoadUnigram(path string) (*Unigram, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Model struct {
			Type  string               `json:"type"`
			UnkID int                  `json:"unk_id"`
			Vocab [][2]json.RawMessage `json:"vocab"`
		} `json:"model"`
		AddedTokens []struct {
			ID      int    `json:"id"`
			Content string `json:"content"`
		} `json:"added_tokens"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Model.Type != "Unigram" {
		return nil, fmt.Errorf("a %s model, not Unigram", doc.Model.Type)
	}
	u := &Unigram{pieces: make(map[string]piece, len(doc.Model.Vocab)), bos: -1, eos: -1}
	minScore := math.Inf(1)
	for id, v := range doc.Model.Vocab {
		var text string
		var score float64
		if err := json.Unmarshal(v[0], &text); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(v[1], &score); err != nil {
			return nil, err
		}
		if id == doc.Model.UnkID {
			continue
		}
		u.pieces[text] = piece{id: id, score: score}
		u.maxRunes = max(u.maxRunes, utf8.RuneCountInString(text))
		minScore = math.Min(minScore, score)
	}
	u.unk = piece{id: doc.Model.UnkID, score: minScore - unkPenalty}
	for _, t := range doc.AddedTokens {
		switch t.Content {
		case "<s>":
			u.bos = t.ID
		case "</s>":
			u.eos = t.ID
		}
		// Added tokens are matched before the model; none of them occur in
		// prose, so they are not looked for in the text.
		delete(u.pieces, t.Content)
	}
	if u.bos < 0 || u.eos < 0 {
		return nil, fmt.Errorf("no <s> and </s> among the added tokens")
	}
	return u, nil
}

// normalize does what the tokenizer's normalizers do: NFKC with
// SentencePiece's whitespace and control characters, runs of spaces made one,
// and no space at the ends.
func normalize(text string) string {
	text = norm.NFKC.String(text)
	var b strings.Builder
	space := false
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			space = true
			continue
		case unicode.IsControl(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// Encode returns the token IDs of the text with <s> and </s>, cut to at
// most maxTokens of them when maxTokens is above 0.
func (u *Unigram) Encode(text string, maxTokens int) []int {
	ids := []int{u.bos}
	for word := range strings.FieldsSeq(normalize(text)) {
		ids = append(ids, u.word("▁"+word)...)
	}
	if maxTokens > 0 && len(ids) > maxTokens-1 {
		ids = ids[:maxTokens-1]
	}
	return append(ids, u.eos)
}

// word is the likeliest segmentation of one word into pieces: Viterbi over
// the pieces' log probabilities, with consecutive unknown characters as one
// unknown token.
func (u *Unigram) word(w string) []int {
	runes := []rune(w)
	n := len(runes)
	best := make([]float64, n+1)
	from := make([]int, n+1)
	id := make([]int, n+1)
	for i := 1; i <= n; i++ {
		best[i] = math.Inf(-1)
	}
	for end := 1; end <= n; end++ {
		for start := max(0, end-u.maxRunes); start < end; start++ {
			if math.IsInf(best[start], -1) {
				continue
			}
			p, ok := u.pieces[string(runes[start:end])]
			if !ok {
				if end-start > 1 {
					continue
				}
				p = u.unk
			}
			if s := best[start] + p.score; s > best[end] {
				best[end], from[end], id[end] = s, start, p.id
			}
		}
	}
	var out []int
	for end := n; end > 0; end = from[end] {
		if id[end] == u.unk.id && len(out) > 0 && out[len(out)-1] == u.unk.id {
			continue
		}
		out = append(out, id[end])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
