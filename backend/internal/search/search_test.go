package search

import (
	"errors"
	"reflect"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/fulltext"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want parsed
		err  error
	}{
		{"white whale", parsed{Words: "white whale"}, nil},
		{`  "white   whale" harpoon `, parsed{Words: "harpoon", Phrases: []string{"white whale"}}, nil},
		{`"es war einmal" "der alte Mann"`, parsed{Phrases: []string{"es war einmal", "der alte Mann"}}, nil},
		{`whale "open quote`, parsed{Words: "whale", Phrases: []string{"open quote"}}, nil},
		{`"" ... !`, parsed{}, ErrEmptyQuery},
	} {
		got, err := parse(tc.in)
		if !errors.Is(err, tc.err) || (err == nil && !reflect.DeepEqual(got, tc.want)) {
			t.Errorf("parse(%q) = %+v, %v; want %+v, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestSnippetParts(t *testing.T) {
	got := snippetParts(fulltext.Snippet{Fragment: "the <b> whale & harpoons", Highlighted: [][2]int{{8, 13}, {16, 24}}})
	want := []Part{{Text: "the <b> "}, {Text: "whale", Match: true}, {Text: " & "}, {Text: "harpoons", Match: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("snippetParts = %+v", got)
	}
	if got := snippetParts(fulltext.Snippet{Fragment: "nothing matched"}); got != nil {
		t.Errorf("a snippet without a match = %+v", got)
	}
}

func TestVariants(t *testing.T) {
	got := variants([][]string{{"with", "white"}, {"whale"}})
	if !reflect.DeepEqual(got, []string{"with whale", "white whale"}) {
		t.Errorf("variants = %v", got)
	}
	got = variants([][]string{{"a", "b", "c"}, {"d", "e", "f"}, {"g", "h"}})
	if len(got) != maxVariants || got[0] != "a d g" {
		t.Errorf("variants of many: %v", got)
	}
}
