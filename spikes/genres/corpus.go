package main

import (
	"bufio"
	"compress/gzip"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Book is one book of the chunk file the search spike extracts.
type Book struct {
	ID       int      `json:"id"`
	Lang     string   `json:"lang"`
	Title    string   `json:"title"`
	Authors  []string `json:"authors"`
	Subjects []string `json:"subjects"`
	Chunks   []string `json:"chunks"`
}

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

// BookSamples are the vectors of a book's sampled chunks, as the embedding
// spike (#15) writes them.
type BookSamples struct {
	ID      int
	Vectors [][]float32
}

func loadPassageVectors(path string) (map[int][][]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := gob.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	out := map[int][][]float32{}
	for {
		var b BookSamples
		if err := dec.Decode(&b); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out[b.ID] = b.Vectors
	}
}
