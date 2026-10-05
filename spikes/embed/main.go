// Command embed measures the runtimes for GOtome's embeddings on the
// benchmark corpus (#15): multilingual-e5-small through hugot's pure-Go
// backend and through ONNX Runtime, with fp32 and int8 weights. See README.md.
package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

const usage = `Usage: embed <command> [flags]

Commands:
  check    Compare a backend's tokens and vectors with the reference
  speed    Embed passages of the corpus and measure the rate and memory
  books    Embed the sampled chunks of every book
  quality  Judge the book vectors by the related books they find

Run "embed <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "embed:", err)
		os.Exit(1)
	}
}

// common are the flags every command that loads the model takes.
type common struct {
	backend, weights, model, ortLib, out string
	threads, maxTokens                   int
}

func (c *common) flags(fs *flag.FlagSet) {
	fs.StringVar(&c.backend, "backend", "go", "go or ort (hugot), or direct (ONNX Runtime with this package's tokenizer)")
	fs.StringVar(&c.weights, "weights", "fp32", "fp32 or int8")
	fs.StringVar(&c.model, "model", "/embed/model", "the model directory")
	fs.StringVar(&c.ortLib, "ort", "", "the directory of the ONNX Runtime library, for ort")
	fs.IntVar(&c.threads, "threads", runtime.GOMAXPROCS(0), "threads for ONNX Runtime")
	fs.IntVar(&c.maxTokens, "max-tokens", 512, "tokens a text is cut to; 0 is hugot's default")
	fs.StringVar(&c.out, "out", "", "the result file")
}

// embedder is what every backend offers the commands.
type embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	TokenIDs(text string) []int
	Close()
}

// openDirect is set by direct.go in builds with cgo, which the ONNX Runtime
// binding needs.
var openDirect func(weights, dir, ortLib string, threads, maxTokens int) (embedder, error)

func (c *common) open(ctx context.Context) (embedder, error) {
	if c.backend == "direct" {
		if openDirect == nil {
			return nil, errors.New("the direct backend needs a build with cgo")
		}
		return openDirect(c.weights, c.model, c.ortLib, c.threads, c.maxTokens)
	}
	return Open(ctx, c.backend, c.weights, c.model, c.ortLib, c.threads, c.maxTokens)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no command given")
	}
	switch args[0] {
	case "check":
		return runCheck(ctx, args[1:])
	case "speed":
		return runSpeed(ctx, args[1:])
	case "books":
		return runBooks(ctx, args[1:])
	case "quality":
		return runQuality(args[1:])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type reference struct {
	Revision     string `json:"revision"`
	MaxSeqLength int    `json:"maxSeqLength"`
	Texts        []struct {
		Text   string    `json:"text"`
		IDs    []int     `json:"ids"`
		Vector []float32 `json:"vector"`
	} `json:"texts"`
}

// CheckResult says how far a backend is from the reference.
type CheckResult struct {
	Backend string `json:"backend"`
	Weights string `json:"weights"`
	// MinCosine is the lowest cosine between a vector and its reference.
	MinCosine float64 `json:"minCosine"`
	// TokensMatch counts the texts whose token IDs equal the reference's.
	TokensMatch int         `json:"tokensMatch"`
	Texts       []TextCheck `json:"texts"`
	Error       string      `json:"error,omitempty"`
}

type TextCheck struct {
	Text   string  `json:"text"`
	Tokens int     `json:"tokens"`
	Cosine float64 `json:"cosine"`
	// FirstMismatch is the first position where the token IDs differ, or -1.
	FirstMismatch int `json:"firstMismatch"`
}

func runCheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	var c common
	c.flags(fs)
	refPath := fs.String("reference", "testdata/reference.json", "the reference vectors")
	if err := fs.Parse(args); err != nil {
		return err
	}
	b, err := os.ReadFile(*refPath)
	if err != nil {
		return err
	}
	var ref reference
	if err := json.Unmarshal(b, &ref); err != nil {
		return err
	}
	res := CheckResult{Backend: c.backend, Weights: c.weights, MinCosine: 1}
	e, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	texts := make([]string, len(ref.Texts))
	for i, t := range ref.Texts {
		texts[i] = t.Text
	}
	vecs, err := e.Embed(ctx, texts)
	if err != nil {
		// A failure is a result too: it says what the backend cannot do.
		res.Error = err.Error()
		return writeJSON(c.out, res)
	}
	for i, t := range ref.Texts {
		ids := e.TokenIDs(t.Text)
		mismatch := -1
		for j := range max(len(ids), len(t.IDs)) {
			if j >= len(ids) || j >= len(t.IDs) || ids[j] != t.IDs[j] {
				mismatch = j
				break
			}
		}
		if mismatch == -1 {
			res.TokensMatch++
		}
		cos := dot(vecs[i], t.Vector)
		res.MinCosine = math.Min(res.MinCosine, cos)
		res.Texts = append(res.Texts, TextCheck{Text: abbreviate(t.Text, 60), Tokens: len(ids), Cosine: cos, FirstMismatch: mismatch})
	}
	fmt.Printf("%s/%s: tokens match for %d of %d texts, lowest cosine %.6f\n", c.backend, c.weights, res.TokensMatch, len(ref.Texts), res.MinCosine)
	return writeJSON(c.out, res)
}

// SpeedResult is the rate of one backend and weights.
type SpeedResult struct {
	Backend  string `json:"backend"`
	Weights  string `json:"weights"`
	Threads  int    `json:"threads"`
	Batch    int    `json:"batch"`
	Passages int    `json:"passages"`
	// Tokens is the mean number of tokens per passage, after truncation.
	Tokens      float64 `json:"meanTokens"`
	LoadSeconds float64 `json:"loadSeconds"`
	// LoadedAnon is the container's anonymous memory once the model is
	// loaded, before the first passage.
	LoadedAnon int64 `json:"loadedAnon"`
	// MemoryLimit is the container's, in bytes.
	MemoryLimit     int64      `json:"memoryLimit"`
	Seconds         float64    `json:"seconds"`
	PassagesPerSec  float64    `json:"passagesPerSecond"`
	Memory          MemoryPeak `json:"memory"`
	OOMKills        int64      `json:"oomKills"`
	ModelFileBytes  int64      `json:"modelFileBytes"`
	LibraryBytes    int64      `json:"libraryBytes,omitempty"`
	ExecutableBytes int64      `json:"executableBytes"`
}

func runSpeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("speed", flag.ContinueOnError)
	var c common
	c.flags(fs)
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the chunk file")
	n := fs.Int("n", 256, "passages to embed")
	batch := fs.Int("batch", 8, "passages per call")
	cgroup := fs.String("cgroup", "", "the cgroup of the container, for memory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passages, err := corpusPassages(*chunks, *n)
	if err != nil {
		return err
	}
	mem := WatchMemory(*cgroup)
	t := time.Now()
	e, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	res := SpeedResult{Backend: c.backend, Weights: c.weights, Threads: c.threads, Batch: *batch, Passages: len(passages), LoadSeconds: time.Since(t).Seconds()}
	res.LoadedAnon = statValue(*cgroup+"/memory.stat", "anon")
	res.MemoryLimit = fileValue(*cgroup + "/memory.max")
	var tokens int
	for _, p := range passages {
		tokens += len(e.TokenIDs(p))
	}
	res.Tokens = float64(tokens) / float64(len(passages))
	// One batch first, so that the rate leaves out what the first call
	// prepares (GoMLX compiles a graph per shape).
	if _, err := e.Embed(ctx, passages[:min(*batch, len(passages))]); err != nil {
		return err
	}
	t = time.Now()
	for i := 0; i < len(passages); i += *batch {
		if _, err := e.Embed(ctx, passages[i:min(i+*batch, len(passages))]); err != nil {
			return err
		}
	}
	res.Seconds = time.Since(t).Seconds()
	res.PassagesPerSec = float64(len(passages)) / res.Seconds
	res.Memory = mem.Stop()
	res.OOMKills = OOMKills(*cgroup)
	res.ModelFileBytes = fileSize(c.model + "/" + Weights[c.weights])
	if c.backend != "go" {
		res.LibraryBytes = fileSize(c.ortLib + "/libonnxruntime.so")
	}
	if exe, err := os.Executable(); err == nil {
		res.ExecutableBytes = fileSize(exe)
	}
	fmt.Printf("%s/%s batch %d: %.1f passages/s (%.0f tokens each), anon %d MB loaded, %d MB at peak\n", c.backend, c.weights, *batch, res.PassagesPerSec, res.Tokens, res.LoadedAnon>>20, res.Memory.Anon>>20)
	return writeJSON(c.out, res)
}

// corpusPassages takes the middle chunk of n books spread evenly over the
// corpus, so that all three languages are in it, prefixed as passages. It
// keeps only those: holding every book's samples would take a gigabyte of the
// container's memory.
func corpusPassages(path string, n int) ([]string, error) {
	total := 0
	if err := readBooks(path, func(Book) error { total++; return nil }); err != nil {
		return nil, err
	}
	stride := max(total/n, 1)
	var out []string
	i := 0
	err := readBooks(path, func(b Book) error {
		if i%stride == 0 && len(out) < n && len(b.Chunks) > 0 {
			out = append(out, "passage: "+b.Chunks[len(b.Chunks)/2])
		}
		i++
		return nil
	})
	return out, err
}

// BookSamples are the vectors of a book's sampled chunks, in order.
type BookSamples struct {
	ID      int
	Vectors [][]float32
}

func runBooks(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("books", flag.ContinueOnError)
	var c common
	c.flags(fs)
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the chunk file")
	samples := fs.Int("samples", MaxSamples, "chunks per book, at most 64")
	prefix := fs.String("prefix", "passage: ", "what each chunk is prefixed with")
	batch := fs.Int("batch", 8, "passages per call")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	f, err := os.Create(c.out)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	t := time.Now()
	var books, passages int
	err = readBooks(*chunks, func(b Book) error {
		idx := Subset(SampleIndexes(len(b.Chunks)), *samples)
		texts := make([]string, len(idx))
		for i, j := range idx {
			texts[i] = *prefix + b.Chunks[j]
		}
		var vecs [][]float32
		for i := 0; i < len(texts); i += *batch {
			v, err := e.Embed(ctx, texts[i:min(i+*batch, len(texts))])
			if err != nil {
				return fmt.Errorf("book %d: %w", b.ID, err)
			}
			vecs = append(vecs, v...)
		}
		books++
		passages += len(texts)
		if books%100 == 0 {
			fmt.Printf("  %d books, %d passages, %.1f passages/s\n", books, passages, float64(passages)/time.Since(t).Seconds())
		}
		return enc.Encode(BookSamples{ID: b.ID, Vectors: vecs})
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s: %d books, %d passages in %.0f s, %.1f passages/s\n", c.backend, c.weights, books, passages, time.Since(t).Seconds(), float64(passages)/time.Since(t).Seconds())
	return f.Close()
}

// QualityResult is the quality of the book vectors made from k samples.
type QualityResult struct {
	Vectors string  `json:"vectors"`
	Samples int     `json:"samples"`
	Quality Quality `json:"quality"`
}

func runQuality(args []string) error {
	fs := flag.NewFlagSet("quality", flag.ContinueOnError)
	chunks := fs.String("chunks", "/corpus/chunks.jsonl.gz", "the chunk file")
	vectors := fs.String("vectors", "", "what books wrote")
	counts := fs.String("samples", "16,32,64", "sample counts to judge")
	out := fs.String("out", "", "the result file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var books []Book
	if err := readBooks(*chunks, func(b Book) error {
		b.Chunks = nil
		books = append(books, b)
		return nil
	}); err != nil {
		return err
	}
	byID := map[int][][]float32{}
	f, err := os.Open(*vectors)
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(f)
	for {
		var s BookSamples
		if err := dec.Decode(&s); err != nil {
			break
		}
		byID[s.ID] = s.Vectors
	}
	f.Close()
	books = slices.DeleteFunc(books, func(b Book) bool { return byID[b.ID] == nil })
	var results []QualityResult
	for _, k := range parseInts(*counts) {
		vecs := make([][]float32, len(books))
		for i, b := range books {
			vecs[i] = BookVector(Subset(byID[b.ID], k))
		}
		q := Measure(books, vecs)
		fmt.Printf("%d samples: volumes MRR %.3f (@1 %.2f, @10 %.2f, %d books), authors MRR %.3f (@10 %.2f, %d books), subject precision %.3f (baseline %.3f), same language %.3f\n",
			k, q.Volumes.MRR, q.Volumes.At1, q.Volumes.At10, q.Volumes.Books, q.Authors.MRR, q.Authors.At10, q.Authors.Books, q.SubjectPrecision, q.SubjectBaseline, q.SameLanguage)
		results = append(results, QualityResult{Vectors: *vectors, Samples: k, Quality: q})
	}
	return writeJSON(*out, results)
}

func parseInts(s string) []int {
	var out []int
	for f := range strings.SplitSeq(s, ",") {
		var n int
		if _, err := fmt.Sscan(f, &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func writeJSON(path string, v any) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func abbreviate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
