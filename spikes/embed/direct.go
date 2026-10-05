//go:build cgo

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"

	ort "github.com/microsoft/onnxruntime/go/onnxruntime"
)

func init() { openDirect = newDirect }

// direct runs the model through ONNX Runtime's own Go binding, the one hugot
// uses underneath, with the Unigram tokenizer of this package instead of
// hugot's.
type direct struct {
	session   *ort.Session
	tokenizer *Unigram
	inputs    map[string]bool
	output    string
	maxTokens int
}

// padID is <pad> in XLM-RoBERTa's vocabulary.
const padID = 1

func newDirect(weights, dir, ortLib string, threads, maxTokens int) (embedder, error) {
	file, ok := Weights[weights]
	if !ok {
		return nil, fmt.Errorf("unknown weights %q", weights)
	}
	tk, err := LoadUnigram(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	ort.SetSharedLibraryPath(filepath.Join(ortLib, "libonnxruntime.so"))
	if err := ort.Init(); err != nil {
		return nil, err
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Close()
	if err := opts.SetIntraOpNumThreads(threads); err != nil {
		return nil, err
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		return nil, err
	}
	s, err := ort.NewSession(filepath.Join(dir, file), opts)
	if err != nil {
		return nil, err
	}
	d := &direct{session: s, tokenizer: tk, inputs: map[string]bool{}, maxTokens: maxTokens}
	for _, in := range s.Inputs() {
		d.inputs[in.Name] = true
	}
	if !d.inputs["input_ids"] || !d.inputs["attention_mask"] {
		s.Close()
		return nil, errors.New("the model takes no input_ids and attention_mask")
	}
	d.output = s.Outputs()[0].Name
	return d, nil
}

func (d *direct) TokenIDs(text string) []int { return d.tokenizer.Encode(text, d.maxTokens) }

// Embed pads the batch to its longest text, runs the model, and mean-pools
// each text's token vectors over its own tokens before normalising them.
func (d *direct) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	ids := make([][]int, len(texts))
	width := 0
	for i, t := range texts {
		ids[i] = d.TokenIDs(t)
		width = max(width, len(ids[i]))
	}
	n := len(texts)
	tokens := make([]int64, n*width)
	mask := make([]int64, n*width)
	for i, row := range ids {
		for j := range width {
			tokens[i*width+j] = padID
			if j < len(row) {
				tokens[i*width+j] = int64(row[j])
				mask[i*width+j] = 1
			}
		}
	}
	shape := []int64{int64(n), int64(width)}
	inputs := map[string]*ort.Tensor{}
	defer func() {
		for _, t := range inputs {
			t.Close()
		}
	}()
	var err error
	if inputs["input_ids"], err = ort.CreateTensor(shape, tokens); err != nil {
		return nil, err
	}
	if inputs["attention_mask"], err = ort.CreateTensor(shape, mask); err != nil {
		return nil, err
	}
	if d.inputs["token_type_ids"] {
		if inputs["token_type_ids"], err = ort.CreateTensor(shape, make([]int64, n*width)); err != nil {
			return nil, err
		}
	}
	out, err := d.session.Run(ctx, inputs, []string{d.output})
	if err != nil {
		return nil, err
	}
	hidden := out[d.output]
	defer hidden.Close()
	values, err := ort.TensorData[float32](hidden)
	if err != nil {
		return nil, err
	}
	dim := int(hidden.Shape()[2])
	vecs := make([][]float32, n)
	for i, row := range ids {
		v := make([]float64, dim)
		for j := range row {
			off := (i*width + j) * dim
			for k := range dim {
				v[k] += float64(values[off+k])
			}
		}
		var norm float64
		for k := range v {
			v[k] /= float64(len(row))
			norm += v[k] * v[k]
		}
		norm = math.Sqrt(norm)
		vecs[i] = make([]float32, dim)
		for k := range v {
			vecs[i][k] = float32(v[k] / norm)
		}
	}
	return vecs, nil
}

func (d *direct) Close() { d.session.Close() }
