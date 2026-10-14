package embed

import "math"

// denseOutput is what bge-m3's export calls its one vector per text, beside
// its sparse and multi-vector outputs.
const denseOutput = "dense_vecs"

// pool makes one normalised vector per text of a model's output: shaped
// texts × dimensions, taken as it is, or texts × tokens × dimensions,
// pooled over each text's own tokens.
func pool(values []float32, shape []int64, lengths []int, pooling string) [][]float32 {
	n := len(lengths)
	vecs := make([][]float32, n)
	if len(shape) == 2 {
		dim := int(shape[1])
		for i := range n {
			v := make([]float64, dim)
			for k := range dim {
				v[k] = float64(values[i*dim+k])
			}
			vecs[i] = normalise(v)
		}
		return vecs
	}
	width, dim := int(shape[1]), int(shape[2])
	for i, length := range lengths {
		v := make([]float64, dim)
		tokens := length
		if pooling == PoolCLS {
			tokens = 1
		}
		for j := range tokens {
			off := (i*width + j) * dim
			for k := range dim {
				v[k] += float64(values[off+k])
			}
		}
		for k := range v {
			v[k] /= float64(tokens)
		}
		vecs[i] = normalise(v)
	}
	return vecs
}

func normalise(v []float64) []float32 {
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float32, len(v))
	for k, x := range v {
		out[k] = float32(x / norm)
	}
	return out
}

func (o *Onnx) Close() error { return o.session.Close() }
