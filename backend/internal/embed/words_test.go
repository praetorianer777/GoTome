package embed

import (
	"context"
	"testing"
)

func TestWordsAreNearAsTheirTextsShareWords(t *testing.T) {
	w := &Words{As: E5Small.Model}
	vecs, err := w.Embed(context.Background(), []string{
		"passage: The lighthouse keeper of Moorland Bay",
		"passage: the Lighthouse keeper of Moorland bay, Fantasy",
		"passage: Accounting for small firms",
		"",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vecs {
		if len(v) != E5Small.Model.Dim || cosine(v, v) < 0.9999 || cosine(v, v) > 1.0001 {
			t.Errorf("vector %d: length %d, norm² %f", i, len(v), cosine(v, v))
		}
	}
	if near, far := cosine(vecs[0], vecs[1]), cosine(vecs[0], vecs[2]); near < 0.8 || far > near/2 {
		t.Errorf("sharing most words %f, sharing one %f", near, far)
	}
	if w.Model() != E5Small.Model {
		t.Errorf("passes for %v", w.Model())
	}
}
