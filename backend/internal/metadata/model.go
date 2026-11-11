package metadata

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Weights are a learned score: a weight per feature, and the logistic
// function of their sum with the candidate's features is the probability
// that the candidate is the book.
type Weights map[string]float64

// probability is what the weights make of a candidate's features.
func (w Weights) probability(f map[string]float64) float64 {
	z := 0.0
	for name, v := range f {
		z += w[name] * v
	}
	return sigmoid(z)
}

func sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }

// Valid says whether every weight is a finite number.
func (w Weights) Valid() error {
	if len(w) == 0 {
		return errors.New("there are no weights")
	}
	for name, v := range w {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1000 {
			return fmt.Errorf("the weight of %q is not usable", name)
		}
	}
	return nil
}

// example is a candidate's features and whether it is the book.
type example struct {
	features map[string]float64
	right    bool
}

// DefaultRidge is the strength of the pull of the weights towards zero. It
// keeps a feature that is always set with the label from growing without
// end, which a small sample makes likely, and is no stronger than that.
const DefaultRidge = 1.0

// fit finds the weights of a logistic regression by Newton's method, the
// bias excepted from the ridge.
func fit(examples []example, ridge float64) Weights {
	index := map[string]int{}
	var names []string
	for _, e := range examples {
		for name := range e.features {
			if _, ok := index[name]; !ok {
				index[name] = len(names)
				names = append(names, name)
			}
		}
	}
	d := len(names)
	w := make([]float64, d)
	if d == 0 {
		return Weights{}
	}
	for range 50 {
		grad := make([]float64, d)
		hess := make([][]float64, d)
		for i := range hess {
			hess[i] = make([]float64, d)
		}
		for _, e := range examples {
			z := 0.0
			for name, v := range e.features {
				z += w[index[name]] * v
			}
			p := sigmoid(z)
			y := 0.0
			if e.right {
				y = 1
			}
			for name, v := range e.features {
				i := index[name]
				grad[i] += (p - y) * v
				for name2, v2 := range e.features {
					hess[i][index[name2]] += p * (1 - p) * v * v2
				}
			}
		}
		for name, i := range index {
			if name == FeatureBias {
				continue
			}
			grad[i] += ridge * w[i]
			hess[i][i] += ridge
		}
		// A little on every diagonal keeps the system solvable where a
		// feature never varies.
		for i := range hess {
			hess[i][i] += 1e-6
		}
		step, ok := solve(hess, grad)
		if !ok {
			break
		}
		largest := 0.0
		for i := range w {
			// A step that long is a sign of a sample too small for the feature.
			s := max(-5, min(5, step[i]))
			w[i] -= s
			largest = max(largest, math.Abs(s))
		}
		if largest < 1e-6 {
			break
		}
	}
	out := Weights{}
	for name, i := range index {
		out[name] = w[i]
	}
	return out
}

// solve solves a x = b by Gaussian elimination with partial pivoting.
func solve(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	m := make([][]float64, n)
	for i := range m {
		m[i] = append(slices.Clone(a[i]), b[i])
	}
	for col := range n {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(m[row][col]) > math.Abs(m[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(m[pivot][col]) < 1e-12 {
			return nil, false
		}
		m[col], m[pivot] = m[pivot], m[col]
		for row := col + 1; row < n; row++ {
			factor := m[row][col] / m[col][col]
			for k := col; k <= n; k++ {
				m[row][k] -= factor * m[col][k]
			}
		}
	}
	x := make([]float64, n)
	for row := n - 1; row >= 0; row-- {
		sum := m[row][n]
		for k := row + 1; k < n; k++ {
			sum -= m[row][k] * x[k]
		}
		x[row] = sum / m[row][row]
	}
	return x, true
}

// logLoss is the mean negative log likelihood of the examples.
func (w Weights) logLoss(examples []example) float64 {
	if len(examples) == 0 {
		return 0
	}
	total := 0.0
	for _, e := range examples {
		p := min(max(w.probability(e.features), 1e-9), 1-1e-9)
		if e.right {
			total -= math.Log(p)
		} else {
			total -= math.Log(1 - p)
		}
	}
	return total / float64(len(examples))
}
