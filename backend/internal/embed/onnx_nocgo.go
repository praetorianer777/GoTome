//go:build !cgo

package embed

import (
	"context"
	"errors"
)

var errNoCgo = errors.New("this gotome is built without cgo, and ONNX Runtime needs it")

func LoadRuntime(path string) (string, error) { return "", errNoCgo }

type Onnx struct{ Embedder }

type OnnxOptions struct {
	Runtime  string
	ModelDir string
	Threads  int
	Fetch    FetchOptions
}

func OpenOnnx(ctx context.Context, spec Spec, opts OnnxOptions) (*Onnx, error) {
	return nil, errNoCgo
}

func (o *Onnx) Tokens(text string) []int { return nil }
