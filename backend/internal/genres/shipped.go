package genres

import (
	"embed"
	"errors"
	"io/fs"
	"strings"
	"sync"
)

// The default ensemble ships in the binary (docs/decisions/genre-detection.md,
// 3): numbers only, trained on Project Gutenberg by `spikes/genres/run.sh
// ship`. shipped/ensemble.json is the heads, stackers and thresholds, and
// shipped/words-<lang>.gwt a word table each.
//
//go:embed shipped
var shippedFiles embed.FS

// Shipped is the default ensemble, read once; nil when the binary carries
// none.
var Shipped = sync.OnceValues(func() (*Ensemble, error) {
	data, err := shippedFiles.ReadFile("shipped/ensemble.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	words := map[string]*WordTable{}
	entries, err := shippedFiles.ReadDir("shipped")
	if err != nil {
		return nil, err
	}
	for _, en := range entries {
		lang, ok := strings.CutPrefix(en.Name(), "words-")
		if !ok {
			continue
		}
		b, err := shippedFiles.ReadFile("shipped/" + en.Name())
		if err != nil {
			return nil, err
		}
		t, err := DecodeWordTable(b)
		if err != nil {
			return nil, err
		}
		words[strings.TrimSuffix(lang, ".gwt")] = t
	}
	return DecodeEnsemble(data, words)
})
