package embed

import (
	"math"
	"testing"
)

func TestPoolTakesTheMeanOrTheFirstToken(t *testing.T) {
	// Two texts of two tokens each, three dimensions; the second text's
	// second token is padding.
	values := []float32{
		1, 0, 0, 0, 1, 0,
		0, 0, 2, 9, 9, 9,
	}
	shape := []int64{2, 2, 3}
	s := float32(1 / math.Sqrt2)
	mean := pool(values, shape, []int{2, 1}, PoolMean)
	if !near(mean[0], []float32{s, s, 0}) || !near(mean[1], []float32{0, 0, 1}) {
		t.Errorf("mean: %v", mean)
	}
	cls := pool(values, shape, []int{2, 1}, PoolCLS)
	if !near(cls[0], []float32{1, 0, 0}) || !near(cls[1], []float32{0, 0, 1}) {
		t.Errorf("first token: %v", cls)
	}
	given := pool([]float32{3, 4, 0, 2}, []int64{2, 2}, []int{5, 5}, PoolCLS)
	if !near(given[0], []float32{0.6, 0.8}) || !near(given[1], []float32{0, 1}) {
		t.Errorf("one vector a text: %v", given)
	}
}

func near(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-6 {
			return false
		}
	}
	return true
}
