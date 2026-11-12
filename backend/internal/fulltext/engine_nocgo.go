//go:build !cgo

package fulltext

import "fmt"

var errNoCgo = fmt.Errorf("%w: this gotome is built without cgo", ErrUnavailable)

func Load(path string) (string, error) { return "", errNoCgo }

type Index struct{}

func Open(dir string, heap int64) (*Index, error) { return nil, errNoCgo }

func (ix *Index) Apply(b Batch) error           { return errNoCgo }
func (ix *Index) Commit() error                 { return errNoCgo }
func (ix *Index) Reload() error                 { return errNoCgo }
func (ix *Index) Search(q Query) ([]Hit, error) { return nil, errNoCgo }
func (ix *Index) Snippets(q Query, passages []Passage, maxChars int) ([]Snippet, error) {
	return nil, errNoCgo
}
func (ix *Index) Info() (Info, error) { return Info{}, errNoCgo }
func (ix *Index) Close() error        { return nil }
