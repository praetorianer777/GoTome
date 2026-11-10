package metadata

import (
	"slices"
	"testing"
)

func TestNamesInEitherOrder(t *testing.T) {
	for in, want := range map[string]string{
		"Devlin, Gerald":   "Gerald Devlin",
		"Gerald Devlin":    "Gerald Devlin",
		" Keller , Jonas ": "Jonas Keller",
		// Several commas are a list of names, which is left alone.
		"Frommert, Christian, Clasen, Jens": "Frommert, Christian, Clasen, Jens",
		"Prince,":                           "Prince,",
	} {
		if got := NaturalName(in); got != want {
			t.Errorf("NaturalName(%q) = %q, want %q", in, got, want)
		}
	}
	if !SameName("Grimm, Liza", "LIZA GRIMM") || SameName("Grimm, Liza", "Jacob Grimm") {
		t.Error("SameName")
	}
	if keys := NameKeys("Grimm, Liza"); !slices.Equal(keys, []string{"grimm liza", "liza grimm"}) {
		t.Errorf("NameKeys = %v", keys)
	}
}
