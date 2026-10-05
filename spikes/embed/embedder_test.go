package main

import (
	"os"
	"strings"
	"testing"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
)

// The model's tokenizer is in .cache/embed after make embed-assets; without
// it the test has nothing to check.
const tokenizerPath = "../../.cache/embed/model/tokenizer.json"

func TestTruncateFitsTheModel(t *testing.T) {
	content, err := os.ReadFile(tokenizerPath)
	if err != nil {
		t.Skip("no tokenizer: run make embed-assets")
	}
	full, err := hftokenizer.NewFromContent(nil, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := full.With(api.EncodeOptions{AddSpecialTokens: true}); err != nil {
		t.Fatal(err)
	}
	spans, err := hftokenizer.NewFromContent(nil, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := spans.With(api.EncodeOptions{IncludeSpans: true}); err != nil {
		t.Fatal(err)
	}
	e := &Embedder{spans: spans, maxTokens: 512}
	for _, text := range []string{
		"passage: " + strings.Repeat("Am Ufer des Flusses standen die alten Häuser dicht beieinander. ", 200),
		"passage: " + strings.Repeat("The harbour lay quiet under a grey sky. ", 200),
	} {
		cut := e.truncate(text)
		if n := len(full.EncodeWithAnnotations(cut).IDs); n > 512 || n < 500 {
			t.Errorf("cut to %d tokens, %d bytes: %q…", n, len(cut), cut[:40])
		}
	}
	short := "query: Wale"
	if e.truncate(short) != short {
		t.Error("a short text was cut")
	}
}
