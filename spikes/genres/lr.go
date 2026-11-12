package main

import "math"

// Sparse is a row of a sparse matrix.
type Sparse struct {
	Idx []int32
	Val []float32
}

// posWeight balances the classes: a positive counts as many times as there
// are negatives per positive, at most maxPosWeight.
const maxPosWeight = 10.0

func classWeights(y []bool) (wp float64) {
	var p, n float64
	for _, v := range y {
		if v {
			p++
		} else {
			n++
		}
	}
	if p == 0 {
		return 1
	}
	return math.Min(n/p, maxPosWeight)
}

// trainDense fits an L2-penalised logistic regression on dense rows (already
// standardised). The last weight is the bias. c is the inverse penalty of
// scikit-learn's convention (the loss is summed over rows).
func trainDense(x [][]float32, y []bool, c float64, iters int) []float64 {
	d := len(x[0])
	wp := classWeights(y)
	f := func(w, g []float64) float64 {
		for i := range g {
			g[i] = 0
		}
		loss := 0.0
		for r, row := range x {
			z := w[d]
			for j, v := range row {
				z += float64(v) * w[j]
			}
			s, wt := -1.0, 1.0
			if y[r] {
				s, wt = 1, wp
			}
			loss += wt * logLoss(s*z)
			dz := -wt * s * sigmoid(-s*z)
			for j, v := range row {
				g[j] += dz * float64(v)
			}
			g[d] += dz
		}
		for j := 0; j < d; j++ {
			loss += 0.5 / c * w[j] * w[j]
			g[j] += w[j] / c
		}
		return loss
	}
	w := make([]float64, d+1)
	minimize(f, w, iters)
	return w
}

func trainSparse(x []Sparse, dim int, y []bool, c float64, iters int) []float64 {
	wp := classWeights(y)
	f := func(w, g []float64) float64 {
		for i := range g {
			g[i] = 0
		}
		loss := 0.0
		for r, row := range x {
			z := w[dim]
			for k, j := range row.Idx {
				z += float64(row.Val[k]) * w[j]
			}
			s, wt := -1.0, 1.0
			if y[r] {
				s, wt = 1, wp
			}
			loss += wt * logLoss(s*z)
			dz := -wt * s * sigmoid(-s*z)
			for k, j := range row.Idx {
				g[j] += dz * float64(row.Val[k])
			}
			g[dim] += dz
		}
		for j := 0; j < dim; j++ {
			loss += 0.5 / c * w[j] * w[j]
			g[j] += w[j] / c
		}
		return loss
	}
	w := make([]float64, dim+1)
	minimize(f, w, iters)
	return w
}
