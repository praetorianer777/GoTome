package similar

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// Whether a model can run here, as Readiness.State says it.
const (
	// ReadyHere: its files are downloaded and ONNX Runtime is loaded, or
	// the embedding server answers with it.
	ReadyHere = "ready"
	// ReadyAfterDownload: it runs once its files are downloaded, which the
	// first pass does.
	ReadyAfterDownload = "download"
	// NoRuntime: ONNX Runtime cannot be loaded.
	NoRuntime = "noRuntime"
	// NotDownloaded: its files are missing and GOTOME_OFFLINE forbids
	// downloading them.
	NotDownloaded = "notDownloaded"
	// NoServer: it runs only on an embedding server, and none is set.
	NoServer = "noServer"
	// ServerDown: the embedding server does not answer, or not with the
	// model's vectors.
	ServerDown = "serverDown"
)

// ReadinessStates are every Readiness.State.
var ReadinessStates = []string{ReadyHere, ReadyAfterDownload, NoRuntime, NotDownloaded, NoServer, ServerDown}

// Readiness is whether a model can run here, and whether on the embedding
// server or in GOtome on the CPU.
type Readiness struct {
	State    string `json:"state"`
	OnServer bool   `json:"onServer"`
}

// Probe tells whether a spec can run here without downloading anything:
// what its Opener would find.
type Probe func(ctx context.Context, spec embed.Spec) Readiness

// serverProbeTimeout bounds asking the embedding server, so that the
// settings page does not wait on a server that hangs.
const serverProbeTimeout = 10 * time.Second

// OnnxProbe is OnnxOpener's probe: the runtime loads, and the files are
// there or may be downloaded. It compares the files' sizes, not their
// hashes, which opening checks.
func OnnxProbe(opts embed.OnnxOptions) Probe {
	return func(_ context.Context, spec embed.Spec) Readiness {
		if !spec.Runs() {
			return Readiness{State: NoServer, OnServer: true}
		}
		if _, err := embed.LoadRuntime(opts.Runtime); err != nil {
			return Readiness{State: NoRuntime}
		}
		switch {
		case embed.Present(opts.ModelDir, spec):
			return Readiness{State: ReadyHere}
		case opts.Fetch.Offline:
			return Readiness{State: NotDownloaded}
		}
		return Readiness{State: ReadyAfterDownload}
	}
}

// ServerProbe is ServerOpener's probe: a spec with a Server is asked of
// the server the address names, as opening it does, and any other one of
// local.
func ServerProbe(local Probe, address func(ctx context.Context) (string, error)) Probe {
	return func(ctx context.Context, spec embed.Spec) Readiness {
		if spec.Server == "" {
			return local(ctx, spec)
		}
		url, err := address(ctx)
		if err != nil {
			return Readiness{State: ServerDown, OnServer: true}
		}
		if url == "" {
			if spec.Runs() {
				return local(ctx, spec)
			}
			return Readiness{State: NoServer, OnServer: true}
		}
		ctx, cancel := context.WithTimeout(ctx, serverProbeTimeout)
		defer cancel()
		o, err := embed.OpenOllama(ctx, url, spec)
		if err != nil {
			return Readiness{State: ServerDown, OnServer: true}
		}
		_ = o.Close()
		return Readiness{State: ReadyHere, OnServer: true}
	}
}

// ModelChoice is one of embed.Specs with what an administrator needs to
// choose it: what it costs, whether it runs here, and how many books
// already have its vectors.
type ModelChoice struct {
	// Name is the value embedding.model takes.
	Name    string `json:"name"`
	Model   string `json:"model"`
	Weights string `json:"weights"`
	Dim     int    `json:"dimensions"`
	// DiskBytes is the size of its files, 0 for a model only a server runs.
	DiskBytes int64 `json:"diskBytes"`
	// MemoryBytes is roughly what it takes in memory on the CPU, an
	// estimate; 0 for a model only a server runs.
	MemoryBytes int64     `json:"memoryBytes"`
	Readiness   Readiness `json:"readiness"`
	// Chosen is the model embedding.model names.
	Chosen bool `json:"chosen"`
	// Metadata and Content are how many of the visible books have that
	// vector of it.
	Metadata int `json:"metadata"`
	Content  int `json:"content"`
}

// ModelList is every model to choose from, the smallest first, with the
// visible books the counts are of.
type ModelList struct {
	Books    int           `json:"books"`
	WithText int           `json:"withText"`
	Models   []ModelChoice `json:"models"`
}

// Models lists embed.Specs for the scope: the counts are of the books it
// sees. Without a Probe every model's readiness is NoRuntime.
func (s *Service) Models(ctx context.Context, scope library.Scope) (ModelList, error) {
	status, err := s.Status(ctx, scope, nil)
	if err != nil {
		return ModelList{}, err
	}
	spec, _, err := s.config(ctx)
	if err != nil {
		return ModelList{}, err
	}
	rows, err := sqlc.New(s.pool).CountVectorsByModel(ctx, sqlc.CountVectorsByModelParams{Viewer: scope.Viewer, SeesAll: scope.SeesAll})
	if err != nil {
		return ModelList{}, err
	}
	type key struct{ model, version string }
	counts := make(map[key]sqlc.CountVectorsByModelRow, len(rows))
	for _, r := range rows {
		counts[key{r.Model, r.ModelVersion}] = r
	}
	out := ModelList{Books: status.Books, WithText: status.WithText, Models: make([]ModelChoice, 0, len(embed.Specs))}
	for name, sp := range embed.Specs {
		c := counts[key{sp.Model.Name, Version(sp.Model)}]
		m := ModelChoice{
			Name: name, Model: sp.Model.Name, Weights: sp.Model.Weights, Dim: sp.Model.Dim,
			DiskBytes: sp.DiskBytes(), Readiness: Readiness{State: NoRuntime},
			Chosen:   sp.Model == spec.Model,
			Metadata: int(c.Metadata), Content: int(c.Content),
		}
		if sp.Runs() {
			m.MemoryBytes = sp.Memory
		}
		if s.Probe != nil {
			m.Readiness = s.Probe(ctx, sp)
		}
		out.Models = append(out.Models, m)
	}
	slices.SortFunc(out.Models, func(a, b ModelChoice) int {
		return cmp.Or(cmp.Compare(a.DiskBytes, b.DiskBytes), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}
