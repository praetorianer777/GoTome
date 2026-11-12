//go:build cgo

package embed

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	ort "github.com/microsoft/onnxruntime/go/onnxruntime"
)

// padID is <pad> in XLM-RoBERTa's vocabulary, which the model's is.
const padID = 1

// LoadRuntime loads the ONNX Runtime library at path, once per process, and
// returns its version. A process loads one library: a later call with
// another path gets the first.
func LoadRuntime(path string) (string, error) {
	ort.SetSharedLibraryPath(path)
	if err := ort.Init(); err != nil {
		return "", fmt.Errorf("load ONNX Runtime from %s: %w", path, err)
	}
	if err := ort.DisableTelemetry(); err != nil {
		return "", err
	}
	return ort.GetVersion()
}

// Onnx runs a model on ONNX Runtime. It is safe for concurrent use.
type Onnx struct {
	spec      Spec
	session   *ort.Session
	tokenizer *Unigram
	typeIDs   bool
	output    string
}

// OnnxOptions say where the runtime and the model are.
type OnnxOptions struct {
	// Runtime is the path of libonnxruntime.so.
	Runtime string
	// ModelDir is the folder models are downloaded into.
	ModelDir string
	// Threads is how many threads one batch uses; 0 lets the runtime decide.
	Threads int
	Fetch   FetchOptions
}

// OpenOnnx loads the runtime, fetches the spec's files where they are
// missing or not the pinned ones, and opens the model.
func OpenOnnx(ctx context.Context, spec Spec, opts OnnxOptions) (*Onnx, error) {
	if !spec.Runs() {
		return nil, fmt.Errorf("%s runs only on an embedding server", spec.Model.Name)
	}
	if _, err := LoadRuntime(opts.Runtime); err != nil {
		return nil, err
	}
	dir, err := Fetch(ctx, opts.ModelDir, spec, opts.Fetch)
	if err != nil {
		return nil, err
	}
	tk, err := LoadUnigram(filepath.Join(dir, spec.Tokenizer.Name))
	if err != nil {
		return nil, err
	}
	so, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer so.Close()
	if err := so.SetIntraOpNumThreads(opts.Threads); err != nil {
		return nil, err
	}
	if err := so.SetInterOpNumThreads(1); err != nil {
		return nil, err
	}
	s, err := ort.NewSession(filepath.Join(dir, spec.Weights.Name), so)
	if err != nil {
		return nil, err
	}
	o := &Onnx{spec: spec, session: s, tokenizer: tk}
	inputs := map[string]bool{}
	for _, in := range s.Inputs() {
		inputs[in.Name] = true
	}
	if !inputs["input_ids"] || !inputs["attention_mask"] {
		s.Close()
		return nil, errors.New("the model takes no input_ids and attention_mask")
	}
	o.typeIDs = inputs["token_type_ids"]
	// An export that gives several vectors names the one wanted; one that
	// gives the token vectors gives only those.
	o.output = s.Outputs()[0].Name
	for _, out := range s.Outputs() {
		if out.Name == denseOutput {
			o.output = out.Name
		}
	}
	return o, nil
}

func (o *Onnx) Model() Model { return o.spec.Model }

// Tokens are the token IDs the model is given for the text.
func (o *Onnx) Tokens(text string) []int {
	return o.tokenizer.Encode(text, o.spec.Model.MaxTokens)
}

// Embed pads the batch to its longest text, runs the model, and pools each
// text's token vectors as its spec says: their mean over its own tokens, as
// e5-small's sentence-transformers configuration does, or the first. A
// model that gives one vector per text is taken as it gives it. Every
// vector is normalised.
func (o *Onnx) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	ids := make([][]int, len(texts))
	width := 0
	for i, t := range texts {
		ids[i] = o.Tokens(t)
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
	if o.typeIDs {
		if inputs["token_type_ids"], err = ort.CreateTensor(shape, make([]int64, n*width)); err != nil {
			return nil, err
		}
	}
	out, err := o.session.Run(ctx, inputs, []string{o.output})
	if err != nil {
		return nil, err
	}
	hidden := out[o.output]
	defer hidden.Close()
	values, err := ort.TensorData[float32](hidden)
	if err != nil {
		return nil, err
	}
	lengths := make([]int, n)
	for i, row := range ids {
		lengths[i] = len(row)
	}
	return pool(values, hidden.Shape(), lengths, o.spec.Pooling), nil
}
