package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
)

// Weights names the ONNX files of the model directory.
var Weights = map[string]string{
	"fp32": "model.onnx",
	"int8": "model_qint8_avx512_vnni.onnx",
}

// Embedder turns texts into normalised, mean-pooled vectors.
type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	// spans tokenizes without special tokens and with each token's place in
	// the text, to cut texts that are too long.
	spans     *hftokenizer.Tokenizer
	maxTokens int
}

// Open loads the model in dir with the backend ("go" or "ort") and the
// weights ("fp32" or "int8"). ortLib is the directory of the ONNX Runtime
// library, for ort. maxTokens, when above 0, cuts every text to that many
// tokens, special tokens included: hugot's Go tokenizer cuts nothing.
func Open(ctx context.Context, backend, weights, dir, ortLib string, threads, maxTokens int) (*Embedder, error) {
	file, ok := Weights[weights]
	if !ok {
		return nil, fmt.Errorf("unknown weights %q", weights)
	}
	var s *hugot.Session
	var err error
	switch backend {
	case "go":
		s, err = hugot.NewGoSession(ctx)
	case "ort":
		s, err = hugot.NewORTSession(ctx,
			options.WithOnnxLibraryPath(ortLib),
			options.WithIntraOpNumThreads(threads),
			options.WithInterOpNumThreads(1),
		)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
	if err != nil {
		return nil, err
	}
	p, err := s.NewPipeline(hugot.FeatureExtractionConfig{
		ModelPath:    dir,
		Name:         backend + "-" + weights,
		OnnxFilename: file,
		Options:      []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
	})
	if err != nil {
		_ = s.Destroy()
		return nil, fmt.Errorf("loading %s: %w", filepath.Join(dir, file), err)
	}
	e := &Embedder{session: s, pipeline: p, maxTokens: maxTokens}
	if maxTokens > 0 {
		content, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
		if err != nil {
			e.Close()
			return nil, err
		}
		if e.spans, err = hftokenizer.NewFromContent(nil, content); err != nil {
			e.Close()
			return nil, err
		}
		if err := e.spans.With(api.EncodeOptions{IncludeSpans: true}); err != nil {
			e.Close()
			return nil, err
		}
	}
	return e, nil
}

// truncate cuts the text after the last token that fits, leaving room for
// the two special tokens. The spans the Go tokenizer reports are a token or
// two off on long texts, so the cut is counted again and moved back by what
// it overshot.
func (e *Embedder) truncate(text string) string {
	if e.spans == nil {
		return text
	}
	limit := e.maxTokens - 2
	enc := e.spans.EncodeWithAnnotations(text)
	k := limit
	for len(enc.IDs) > limit && k > 0 {
		cut := text[:enc.Spans[k].Start]
		n := len(e.spans.EncodeWithAnnotations(cut).IDs)
		if n <= limit {
			return cut
		}
		k -= n - limit
	}
	return text
}

// Embed returns one vector per text.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	cut := make([]string, len(texts))
	for i, t := range texts {
		cut[i] = e.truncate(t)
	}
	out, err := e.pipeline.RunPipeline(ctx, cut)
	if err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// TokenIDs is what the model is given for the text, special tokens included.
func (e *Embedder) TokenIDs(text string) []int {
	return e.pipeline.Model.Tokenizer.GoTokenizer.Tokenizer.EncodeWithAnnotations(e.truncate(text)).IDs
}

func (e *Embedder) Close() { _ = e.session.Destroy() }
