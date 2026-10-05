package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestUnigramMatchesTheReference(t *testing.T) {
	u, err := LoadUnigram(tokenizerPath)
	if err != nil {
		t.Skip("no tokenizer: run make embed-assets")
	}
	b, err := os.ReadFile("testdata/reference.json")
	if err != nil {
		t.Skip("no reference: run make embed-reference")
	}
	var ref reference
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	for _, r := range ref.Texts {
		got := u.Encode(r.Text, ref.MaxSeqLength)
		if !slices.Equal(got, r.IDs) {
			i := 0
			for i < min(len(got), len(r.IDs)) && got[i] == r.IDs[i] {
				i++
			}
			t.Errorf("%.40q differs at token %d of %d: got %v, want %v", r.Text, i, len(r.IDs), got[i:min(i+6, len(got))], r.IDs[i:min(i+6, len(r.IDs))])
		}
	}
}
