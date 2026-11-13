package wordcount

import (
	"reflect"
	"strings"
	"testing"
)

func TestCountLowercasesAndLimitsLength(t *testing.T) {
	got := Count([]string{"The Cat, the cat! An ox. Überall überall " + strings.Repeat("x", 30)})
	want := Counts{"the": 2, "cat": 2, "überall": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCountLeavesGutenbergBoilerplateOut(t *testing.T) {
	texts := []string{
		"Produced by transcribers\n*** START OF THE PROJECT GUTENBERG EBOOK EMMA ***\nharriet walked",
		"harriet talked\n*** END OF THE PROJECT GUTENBERG EBOOK EMMA ***\nlicence terms",
	}
	got := Count(texts)
	want := Counts{"harriet": 2, "walked": 1, "talked": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCountWithoutMarkersCountsAll(t *testing.T) {
	got := Count([]string{"one two", "three"})
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	c := Count([]string{"stream streams streamed dream dreams dreamt stream"})
	back, err := Decode(Encode(c))
	if err != nil || !reflect.DeepEqual(back, c) {
		t.Fatalf("%v %v", back, err)
	}
	if _, err := Decode([]byte("junk")); err == nil {
		t.Fatal("junk decoded")
	}
	if back, err := Decode(Encode(Counts{})); err != nil || len(back) != 0 {
		t.Fatalf("%v %v", back, err)
	}
}
