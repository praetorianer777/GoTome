package main

import (
	"slices"
	"testing"
)

func TestSampleIndexesSpreadOverTheBook(t *testing.T) {
	got := SampleIndexes(640)
	if len(got) != MaxSamples || got[0] != 5 || got[MaxSamples-1] != 635 {
		t.Fatalf("got %d samples from %d to %d", len(got), got[0], got[len(got)-1])
	}
	if !slices.IsSorted(got) {
		t.Error("samples out of order")
	}
	if got := SampleIndexes(3); !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("a short book: %v", got)
	}
}

func TestSubsetTakesEvenlySpacedSamples(t *testing.T) {
	all := SampleIndexes(640)
	half := Subset(all, 32)
	quarter := Subset(all, 16)
	if len(half) != 32 || len(quarter) != 16 {
		t.Fatalf("got %d and %d", len(half), len(quarter))
	}
	for _, q := range quarter {
		if !slices.Contains(all, q) {
			t.Errorf("%d is not one of the samples", q)
		}
	}
	if quarter[0] != all[2] || quarter[15] != all[62] {
		t.Errorf("quarter from %d to %d", quarter[0], quarter[15])
	}
	if got := Subset([]int{1, 2}, 16); len(got) != 2 {
		t.Errorf("a book with two samples kept %d", len(got))
	}
}
