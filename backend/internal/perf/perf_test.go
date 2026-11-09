package perf

import "testing"

func TestTheCommonWordAndPhraseAreThoseOfMostChunks(t *testing.T) {
	chunks := [][]string{
		wordsOf("The lighthouse stood above the harbour, and the harbour lights burned."),
		wordsOf("Above the harbour lights, a lighthouse keeper slept."),
		wordsOf("Nothing but the harbour lights."),
	}
	word, phrase := commonIn(chunks)
	if word != "harbour" || phrase != "harbour lights" {
		t.Errorf("word %q, phrase %q", word, phrase)
	}
	if word, phrase := commonIn(nil); word != "" || phrase != "" {
		t.Errorf("of nothing: %q %q", word, phrase)
	}
}

func TestRowsAreTheLongestListOfTheAnswer(t *testing.T) {
	for _, c := range []struct {
		body string
		feed bool
		want int
	}{
		{`{"items": [1, 2, 3], "facets": [1]}`, false, 3},
		{`[1, 2]`, false, 2},
		{`{"total": 3}`, false, -1},
		{`<feed><entry/><entry/></feed>`, true, 2},
	} {
		if got := rowsOf([]byte(c.body), c.feed); got != c.want {
			t.Errorf("%s: %d, want %d", c.body, got, c.want)
		}
	}
}

func TestAViewWithoutWhatItAsksAboutHasNoPath(t *testing.T) {
	for _, v := range Views(Params{}) {
		switch v.Name {
		case "an author", "a series", "collection", "quick search", "full-text search":
			if v.Path != "" {
				t.Errorf("%s asks %q about nothing", v.Name, v.Path)
			}
		case "authors", "libraries":
			if v.Path == "" {
				t.Errorf("%s needs nothing but has no path", v.Name)
			}
		}
	}
}
