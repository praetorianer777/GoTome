package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// RecordVariable, set to anything, has NewFixtures record what is missing
// from the provider itself. Nothing else in the tests reaches the network.
const RecordVariable = "GOTOME_RECORD_FIXTURES"

// ErrNoFixture is a request no recorded answer is there for.
var ErrNoFixture = errors.New("no recorded answer")

// Fixtures is a Web that answers from files recorded earlier, one per
// request, so that a provider is tested against what its source really
// says without asking it. Secrets are never recorded: neither Header nor
// SecretQuery is part of a file or its name.
type Fixtures struct {
	Dir string
	// Live, when set, is asked what no file answers, and its answer kept.
	Live Web
}

// NewFixtures replays the answers in dir, and with RecordVariable set
// records those that are missing from the provider.
func NewFixtures(dir string, p Provider) *Fixtures {
	f := &Fixtures{Dir: dir}
	if os.Getenv(RecordVariable) != "" {
		f.Live = LiveWeb(p)
	}
	return f
}

type fixture struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	// Request is the request's body, Answer the answer's.
	Request string `json:"request,omitempty"`
	Status  int    `json:"status"`
	Answer  []byte `json:"answer,omitempty"`
}

func (f *Fixtures) Get(ctx context.Context, r Request) ([]byte, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	sum := sha256.Sum256([]byte(method + "\x00" + r.URL + "\x00" + string(r.Body)))
	path := filepath.Join(f.Dir, hex.EncodeToString(sum[:8])+".json")

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var fx fixture
		if err := json.Unmarshal(data, &fx); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", path, err)
		}
		if fx.Status == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return fx.Answer, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	case f.Live == nil:
		return nil, fmt.Errorf("%w for %s %s; record it with %s=1", ErrNoFixture, method, r.URL, RecordVariable)
	}

	answer, err := f.Live.Get(ctx, r)
	fx := fixture{Method: method, URL: r.URL, Request: string(r.Body), Status: http.StatusOK, Answer: answer}
	switch {
	case errors.Is(err, ErrNotFound):
		fx.Status = http.StatusNotFound
	case err != nil:
		return nil, err
	}
	data, err = json.MarshalIndent(fx, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if fx.Status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	return answer, nil
}
