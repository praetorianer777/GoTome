package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
)

// Book is one book of the corpus as the engines index it.
type Book struct {
	ID    int    `json:"id"`
	Lang  string `json:"lang"`
	Title string `json:"title"`
	// Authors and Subjects are for the embedding spike (#15), which judges
	// which books are related by them.
	Authors  []string `json:"authors,omitempty"`
	Subjects []string `json:"subjects,omitempty"`
	Chunks   []string `json:"chunks"`
}

// extract reads every EPUB under dir/<lang>/ with the app's own reader and
// writes the books, cut into chunks, to out as gzipped JSON lines.
func extract(dir, out string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.epub"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	enc := json.NewEncoder(zw)
	var books, chunks, chars, skipped int
	for _, path := range files {
		b, err := readBook(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", path, err)
			skipped++
			continue
		}
		b.ID = books + 1
		if err := enc.Encode(b); err != nil {
			return err
		}
		books++
		chunks += len(b.Chunks)
		for _, c := range b.Chunks {
			chars += len(c)
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	fmt.Printf("%d books, %d chunks, %.1f MB of text, %d skipped\n", books, chunks, float64(chars)/1e6, skipped)
	return f.Close()
}

func readBook(path string) (Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return Book{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Book{}, err
	}
	b, err := epub.Parse(f, st.Size())
	if err != nil {
		return Book{}, err
	}
	chunks := Chunks(b.Text())
	if len(chunks) == 0 {
		return Book{}, errors.New("no text")
	}
	return Book{
		Lang: filepath.Base(filepath.Dir(path)), Title: b.Metadata.Title,
		Authors: b.Metadata.Authors(), Subjects: b.Metadata.Subjects, Chunks: chunks,
	}, nil
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
