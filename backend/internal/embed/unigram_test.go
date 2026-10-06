package embed

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The vocabulary's IDs are its positions: <s> 0, <pad> 1, </s> 2, <unk> 3.
const tinyTokenizer = `{
  "added_tokens": [
    {"id": 0, "content": "<s>"}, {"id": 1, "content": "<pad>"},
    {"id": 2, "content": "</s>"}, {"id": 3, "content": "<unk>"}
  ],
  "model": {
    "type": "Unigram",
    "unk_id": 3,
    "vocab": [
      ["<s>", 0], ["<pad>", 0], ["</s>", 0], ["<unk>", 0],
      ["▁", -2], ["▁under", -4], ["stand", -4], ["▁understand", -5],
      ["s", -3], ["▁u", -1], ["nderstand", -9]
    ]
  }
}`

func tinyUnigram(t *testing.T) *Unigram {
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	if err := os.WriteFile(path, []byte(tinyTokenizer), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := LoadUnigram(path)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUnigramTakesTheLikeliestSegmentation(t *testing.T) {
	u := tinyUnigram(t)
	for _, tc := range []struct {
		text string
		max  int
		want []int
	}{
		// ▁understand (-5) over ▁under stand (-8) and ▁u nderstand (-10).
		{"understand", 0, []int{0, 7, 2}},
		{"understands", 0, []int{0, 7, 8, 2}},
		// Unknown characters in a row are one unknown token.
		{"xyz", 0, []int{0, 4, 3, 2}},
		{"  understand\t\nunderstand ", 0, []int{0, 7, 7, 2}},
		{"understand understand understand", 4, []int{0, 7, 7, 2}},
		{"", 0, []int{0, 2}},
	} {
		if got := u.Encode(tc.text, tc.max); !slices.Equal(got, tc.want) {
			t.Errorf("Encode(%q, %d) = %v, want %v", tc.text, tc.max, got, tc.want)
		}
	}
}

func TestLoadUnigramRefusesAnotherModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	os.WriteFile(path, []byte(`{"model": {"type": "BPE", "vocab": []}}`), 0o644)
	if _, err := LoadUnigram(path); err == nil {
		t.Error("a BPE tokenizer loaded as Unigram")
	}
}
