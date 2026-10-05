package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Book is one book of the chunk file the search spike extracts
// (make search-spike-extract).
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

// MaxSamples is the most chunks of a book that are embedded. Smaller sample
// counts are taken from these, so one run serves them all.
const MaxSamples = 64

// SampleIndexes are the positions of the chunks embedded for a book of n
// chunks: MaxSamples spread evenly over the book, or all of them.
func SampleIndexes(n int) []int {
	if n <= MaxSamples {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, MaxSamples)
	for i := range out {
		out[i] = (2*i + 1) * n / (2 * MaxSamples)
	}
	return out
}

// Subset picks k of the samples, spread evenly over them: every second for
// half as many, every fourth for a quarter. A book with fewer samples than k
// keeps them all.
func Subset[T any](samples []T, k int) []T {
	if len(samples) <= k {
		return samples
	}
	out := make([]T, k)
	for i := range out {
		out[i] = samples[(2*i+1)*len(samples)/(2*k)]
	}
	return out
}
