package similar

import (
	"slices"
	"testing"
)

func TestSpreadTakesTheMiddlesOfEqualParts(t *testing.T) {
	positions := make([]int32, 64)
	for i := range positions {
		positions[i] = int32(i)
	}
	if got, want := spread(positions, 4), []int32{8, 24, 40, 56}; !slices.Equal(got, want) {
		t.Errorf("spread(64, 4) = %v, want %v", got, want)
	}
	if got := spread(positions[:3], 16); !slices.Equal(got, []int32{0, 1, 2}) {
		t.Errorf("fewer positions than samples: %v", got)
	}
	if got := spread(positions[:17], 16); len(slices.Compact(slices.Clone(got))) != 16 {
		t.Errorf("spread(17, 16) took a position twice: %v", got)
	}
}

func TestLiteralIsWhatPgvectorReads(t *testing.T) {
	if got := literal([]float32{0.5, -0.25, 1e-7}); got != "[0.5,-0.25,1e-07]" {
		t.Errorf("literal = %s", got)
	}
}
