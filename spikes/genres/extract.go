package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/textproc"
)

func openEmbedder(ctx context.Context, modelRoot, ort string, threads int) (*embed.Onnx, error) {
	return embed.OpenOnnx(ctx, embed.E5Small, embed.OnnxOptions{Runtime: ort, ModelDir: modelRoot, Threads: threads, Fetch: embed.FetchOptions{Offline: true}})
}

func embedAll(ctx context.Context, e *embed.Onnx, texts []string) ([][]float32, error) {
	var out [][]float32
	for i := 0; i < len(texts); i += 8 {
		v, err := e.Embed(ctx, texts[i:min(i+8, len(texts))])
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	return out, nil
}

func cpuNS() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return ru.Utime.Nano() + ru.Stime.Nano()
}

func peakKB() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return ru.Maxrss
}

func metaText(title string, authors []string, desc string) string {
	parts := []string{title}
	if len(authors) > 0 {
		parts = append(parts, strings.Join(authors, ", "))
	}
	if desc != "" {
		parts = append(parts, desc)
	}
	return embed.E5Small.Prefix + strings.Join(parts, ". ")
}

// extractGutenberg makes records of the public corpus: its text from the
// chunk file, its passage vectors from the file #15 made (int8, 16 per
// book), and the metadata vectors here.
func extractGutenberg(args []string) error {
	fs := flag.NewFlagSet("gutenberg", flag.ExitOnError)
	chunks := fs.String("chunks", ".cache/corpus/chunks.jsonl.gz", "the chunk file")
	vectors := fs.String("vectors", ".cache/embed/vectors-direct-int8-16-passage.gob", "the passage vectors of #15")
	model := fs.String("model-root", "", "folder holding multilingual-e5-small")
	ort := fs.String("ort", "", "libonnxruntime.so")
	threads := fs.Int("threads", 2, "")
	out := fs.String("out", "gutenberg.gob", "")
	_ = fs.Parse(args)
	ctx := context.Background()
	vecs, err := loadPassageVectors(*vectors)
	if err != nil {
		return err
	}
	e, err := openEmbedder(ctx, *model, *ort, *threads)
	if err != nil {
		return err
	}
	var recs []Rec
	err = readBooks(*chunks, func(b Book) error {
		r := Rec{ID: fmt.Sprint(b.ID), Group: fmt.Sprint(b.ID), Lang: b.Lang, Labels: LabelsOf(b.Subjects), Terms: map[string]uint16{},
			Tagged: len(b.Subjects) > 0, Chunks: len(b.Chunks), Vecs: vecs[b.ID]}
		t := time.Now()
		for _, c := range b.Chunks {
			r.Chars += len(c)
			countTerms(r.Terms, c)
		}
		r.TokenizeNS = time.Since(t).Nanoseconds()
		t = time.Now()
		m, err := embedAll(ctx, e, []string{metaText(b.Title, b.Authors, "")})
		if err != nil {
			return err
		}
		r.Meta = m[0]
		r.EmbedNS = time.Since(t).Nanoseconds()
		recs = append(recs, r)
		return nil
	})
	if err != nil {
		return err
	}
	return saveRecs(*out, recs)
}

// extractLibrary reads whole folders of a library, in the order of a hash of
// their names, until it has n EPUBs. Nothing of what it reads is printed or
// kept but the counts, the words, the vectors and the genres the tags give.
func extractLibrary(args []string) error {
	fs := flag.NewFlagSet("library", flag.ExitOnError)
	root := fs.String("root", "", "the library, read only")
	n := fs.Int("n", 1500, "books to read")
	passages := fs.Int("passages", 16, "passages embedded per book")
	model := fs.String("model-root", "", "folder holding multilingual-e5-small")
	ort := fs.String("ort", "", "libonnxruntime.so")
	threads := fs.Int("threads", 3, "")
	out := fs.String("out", "library.gob", "")
	salt := fs.String("salt", "gotome-451", "")
	_ = fs.Parse(args)
	ctx := context.Background()
	entries, err := os.ReadDir(*root)
	if err != nil {
		return err
	}
	type dir struct{ name, key string }
	var dirs []dir
	for _, e := range entries {
		if e.IsDir() {
			h := sha1.Sum([]byte(*salt + e.Name()))
			dirs = append(dirs, dir{e.Name(), hex.EncodeToString(h[:])})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].key < dirs[j].key })
	e, err := openEmbedder(ctx, *model, *ort, *threads)
	if err != nil {
		return err
	}
	var recs []Rec
	var failed int
	t0 := time.Now()
	for _, d := range dirs {
		if len(recs) >= *n {
			break
		}
		var files []string
		_ = filepath.WalkDir(filepath.Join(*root, d.name), func(p string, de os.DirEntry, err error) error {
			if err == nil && !de.IsDir() && strings.EqualFold(filepath.Ext(p), ".epub") {
				files = append(files, p)
			}
			return nil
		})
		sort.Strings(files)
		for _, p := range files {
			r, err := libraryRec(ctx, e, p, *passages)
			if err != nil {
				failed++
				continue
			}
			r.ID = fmt.Sprint(len(recs))
			r.Group = d.key[:12]
			recs = append(recs, r)
			if len(recs)%50 == 0 {
				fmt.Printf("%d books, %d failed, %.0f s, cpu %.0f s\n", len(recs), failed, time.Since(t0).Seconds(), float64(cpuNS())/1e9)
			}
		}
	}
	fmt.Printf("done: %d books, %d unreadable, %.0f s wall, %.0f s cpu, peak %d MB\n", len(recs), failed, time.Since(t0).Seconds(), float64(cpuNS())/1e9, peakKB()/1024)
	return saveRecs(*out, recs)
}

func libraryRec(ctx context.Context, e *embed.Onnx, path string, passages int) (Rec, error) {
	var r Rec
	t := time.Now()
	f, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return r, err
	}
	b, err := epub.Parse(f, st.Size())
	if err != nil {
		return r, err
	}
	text := b.Text()
	chunks := Chunks(text)
	r.ParseNS = time.Since(t).Nanoseconds()
	r.Tagged = len(b.Metadata.Subjects) > 0
	r.Labels = LabelsOf(b.Metadata.Subjects)
	r.HasDesc = b.Metadata.Description != ""
	r.Chunks = len(chunks)
	r.Chars = len(text)
	sample := text
	if len(sample) > 20000 {
		sample = sample[:20000]
	}
	r.Lang = textproc.Detect(sample, b.Metadata.Language)
	t = time.Now()
	r.Terms = map[string]uint16{}
	for _, c := range chunks {
		countTerms(r.Terms, c)
	}
	r.TokenizeNS = time.Since(t).Nanoseconds()
	t = time.Now()
	desc := b.Metadata.Description
	if len(desc) > 1500 {
		desc = desc[:1500]
	}
	m, err := embedAll(ctx, e, []string{metaText(b.Metadata.Title, b.Metadata.Authors(), desc)})
	if err != nil {
		return r, err
	}
	r.Meta = m[0]
	var texts []string
	for _, c := range pickEven(chunks, passages) {
		texts = append(texts, embed.E5Small.PassageText(c))
	}
	if len(texts) > 0 {
		if r.Vecs, err = embedAll(ctx, e, texts); err != nil {
			return r, err
		}
	}
	r.EmbedNS = time.Since(t).Nanoseconds()
	return r, nil
}
