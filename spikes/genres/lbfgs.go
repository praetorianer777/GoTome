package main

import "math"

// minimize finds a minimum of f with L-BFGS (memory 8) and a backtracking
// line search. f returns the value and writes the gradient.
func minimize(f func(w, g []float64) float64, w []float64, iters int) {
	const m = 8
	n := len(w)
	g := make([]float64, n)
	val := f(w, g)
	var ss, ys [][]float64
	var rho []float64
	dir := make([]float64, n)
	wn := make([]float64, n)
	gn := make([]float64, n)
	for it := 0; it < iters; it++ {
		gnorm := 0.0
		for _, x := range g {
			gnorm += x * x
		}
		if math.Sqrt(gnorm) < 1e-5 {
			return
		}
		copy(dir, g)
		alpha := make([]float64, len(ss))
		for i := len(ss) - 1; i >= 0; i-- {
			alpha[i] = rho[i] * dot(ss[i], dir)
			axpy(-alpha[i], ys[i], dir)
		}
		if k := len(ss); k > 0 {
			gamma := dot(ss[k-1], ys[k-1]) / dot(ys[k-1], ys[k-1])
			for i := range dir {
				dir[i] *= gamma
			}
		} else {
			s := 1 / math.Max(math.Sqrt(gnorm), 1)
			for i := range dir {
				dir[i] *= s
			}
		}
		for i := range ss {
			beta := rho[i] * dot(ys[i], dir)
			axpy(alpha[i]-beta, ss[i], dir)
		}
		slope := -dot(g, dir)
		if slope >= 0 {
			ss, ys, rho = nil, nil, nil
			continue
		}
		step := 1.0
		var nv float64
		ok := false
		for try := 0; try < 20; try++ {
			for i := range w {
				wn[i] = w[i] - step*dir[i]
			}
			nv = f(wn, gn)
			if nv <= val+1e-4*step*slope {
				ok = true
				break
			}
			step /= 2
		}
		if !ok {
			return
		}
		s := make([]float64, n)
		y := make([]float64, n)
		for i := range w {
			s[i] = wn[i] - w[i]
			y[i] = gn[i] - g[i]
		}
		if sy := dot(s, y); sy > 1e-12 {
			if len(ss) == m {
				ss, ys, rho = ss[1:], ys[1:], rho[1:]
			}
			ss, ys, rho = append(ss, s), append(ys, y), append(rho, 1/sy)
		}
		copy(w, wn)
		copy(g, gn)
		if math.Abs(val-nv) < 1e-9*math.Max(1, math.Abs(val)) {
			return
		}
		val = nv
	}
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func axpy(a float64, x, y []float64) {
	for i := range x {
		y[i] += a * x[i]
	}
}

func sigmoid(z float64) float64 {
	if z >= 0 {
		return 1 / (1 + math.Exp(-z))
	}
	e := math.Exp(z)
	return e / (1 + e)
}

// logLoss is log(1+exp(-z)) for z = y·score with y = ±1, and its derivative
// with respect to the score times y.
func logLoss(z float64) float64 {
	if z > 0 {
		return math.Log1p(math.Exp(-z))
	}
	return -z + math.Log1p(math.Exp(z))
}
