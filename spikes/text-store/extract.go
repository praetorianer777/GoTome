package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/textproc"
)

// charsPerPage is ingest's estimate of a page, which Split uses for pages.
const charsPerPage = 1800

// Libraries is how many libraries the books are spread over, so that a
// search can be limited to one of them.
const Libraries = 2

// Book is one book's chunks as the app would store them. Nothing that names
// the book is kept: the file holds a person's library.
type Book struct {
	ID      int     `json:"id"`
	Library int     `json:"library"`
	Chunks  []Chunk `json:"chunks"`
}

// Chunk is one row of book_chunks.
type Chunk struct {
	Lang     string `json:"lang"`
	Chapter  string `json:"chapter"`
	Offset   int    `json:"offset"`
	PageFrom int    `json:"pageFrom"`
	PageTo   int    `json:"pageTo"`
	Text     string `json:"text"`
}

// sample lists the EPUBs under dir in name order and takes every nth, so
// that the same library gives the same sample.
func sample(dir string, every int) ([]string, error) {
	var all []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".epub") {
			all = append(all, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(all)
	var out []string
	for i := 0; i < len(all); i += every {
		out = append(out, all[i])
	}
	return out, nil
}

// extract reads the sample with the app's EPUB reader, cuts each book into
// chunks with the app's textproc, and writes them to out as gzipped JSON
// lines, in sample order.
func extract(dir string, every int, out string) error {
	files, err := sample(dir, every)
	if err != nil {
		return err
	}
	fmt.Printf("%d EPUBs in the sample\n", len(files))
	results := make([][]Chunk, len(files))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range next {
				chunks, err := readChunks(files[i])
				if err != nil {
					fmt.Fprintf(os.Stderr, "book %d skipped: %v\n", i, err)
					continue
				}
				results[i] = chunks
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	enc := json.NewEncoder(zw)
	var books, chunks, bytes, chars int
	for _, cs := range results {
		if len(cs) == 0 {
			continue
		}
		books++
		if err := enc.Encode(Book{ID: books, Library: books % Libraries, Chunks: cs}); err != nil {
			return err
		}
		chunks += len(cs)
		for _, c := range cs {
			bytes += len(c.Text)
			chars += utf8.RuneCountInString(c.Text)
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	fmt.Printf("%d books, %d chunks, %.1f MB of text (%.1f M characters), %d without text\n",
		books, chunks, float64(bytes)/1e6, float64(chars)/1e6, len(files)-books)
	return f.Close()
}

// readTime times reading the chunks of some of the sample's books back
// from their files, one at a time, as a search would to make snippets if
// the files were the only copy of the text (#268).
func readTime(dir string, every, books int, out string) error {
	files, err := sample(dir, every)
	if err != nil {
		return err
	}
	step := max(len(files)/books, 1)
	l := Latency{Kind: "read", Query: "a book's chunks from its EPUB"}
	var times []float64
	for i := 0; i < len(files) && len(times) < books; i += step {
		start := time.Now()
		if _, err := readChunks(files[i]); err != nil {
			continue
		}
		times = append(times, float64(time.Since(start).Microseconds())/1000)
		l.Hits++
	}
	percentiles(&l, times)
	fmt.Printf("%d books: p50 %.1f ms  p95 %.1f ms  max %.1f ms\n", l.Hits, l.P50, l.P95, l.Max)
	return writeJSON(out, l)
}

func readChunks(path string) (chunks []Chunk, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	b, err := epub.Parse(f, st.Size())
	if err != nil {
		return nil, err
	}
	if b.DRM {
		return nil, errors.New("DRM")
	}
	sections := make([]textproc.Section, len(b.Chapters))
	for i, ch := range b.Chapters {
		sections[i] = textproc.Section{Label: ch.Title, Text: ch.Text}
	}
	for _, c := range textproc.Split(sections, textproc.Options{CharsPerPage: charsPerPage, LangHint: b.Metadata.Language}) {
		chunks = append(chunks, Chunk{
			Lang: c.Lang, Chapter: c.Chapter, Offset: c.Offset,
			PageFrom: c.PageFrom, PageTo: c.PageTo, Text: c.Text,
		})
	}
	return chunks, nil
}

// readBooks calls fn for each book in the file extract wrote.
func readBooks(path string, fn func(Book) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(zr)
	for {
		var b Book
		if err := dec.Decode(&b); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if err := fn(b); err != nil {
			return err
		}
	}
}
