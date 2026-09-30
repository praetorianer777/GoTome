// Package covers keeps the cover images of books: each one once, in the sizes
// the app shows, under a name made from its content.
package covers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	// The formats a cover may arrive in.
	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Size is one of the widths a cover is kept in.
type Size string

const (
	// Small is for grids and lists, sharp on a screen of double density.
	Small Size = "small"
	// Large is for the page of one book.
	Large Size = "large"
)

var widths = map[Size]int{Small: 400, Large: 1000}

const (
	jpegQuality = 82
	// maxPixels bounds what decoding one cover may cost in memory: a small
	// file can declare an image of any size.
	maxPixels = 50_000_000
)

// ErrNotAnImage is returned for cover data that is no image GOtome can read,
// or one too large to be a cover.
var ErrNotAnImage = errors.New("not a usable image")

// ErrNotFound is returned for a cover the store does not hold.
var ErrNotFound = errors.New("no such cover")

var validKey = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Store keeps covers in a directory.
type Store struct {
	dir string
}

// NewStore returns a Store that keeps its files under dataDir/covers.
func NewStore(dataDir string) *Store {
	return &Store{dir: filepath.Join(dataDir, "covers")}
}

// ParseSize reads a size out of a request.
func ParseSize(s string) (Size, bool) {
	_, ok := widths[Size(s)]
	return Size(s), ok
}

// Put stores the image in every size and returns its key, the SHA-256 of the
// data it was given. Storing the same image again costs a look at the disk.
func (s *Store) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])
	if s.has(key) {
		return key, nil
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return "", fmt.Errorf("%w: %d by %d pixels", ErrNotAnImage, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path(key, Small)), 0o750); err != nil {
		return "", err
	}
	// The small size is written last: it is the one has looks for, so a
	// cover half written is a cover not there.
	for _, size := range []Size{Large, Small} {
		if err := writeJPEG(s.path(key, size), scale(src, widths[size])); err != nil {
			return "", err
		}
	}
	return key, nil
}

// Path returns the file holding the cover in that size, or ErrNotFound.
func (s *Store) Path(key string, size Size) (string, error) {
	if _, ok := widths[size]; !ok || !validKey.MatchString(key) {
		return "", ErrNotFound
	}
	path := s.path(key, size)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	return path, nil
}

func (s *Store) has(key string) bool {
	_, err := os.Stat(s.path(key, Small))
	return err == nil
}

// path spreads the files over 256 directories, so that none grows to hold
// every cover of a large library.
func (s *Store) path(key string, size Size) string {
	return filepath.Join(s.dir, key[:2], key+"-"+string(size)+".jpg")
}

// scale returns the image no wider than width, on white: a JPEG has no
// transparency, and a transparent cover is meant to be seen on paper.
func scale(src image.Image, width int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > width {
		h = max(1, h*width/w)
		w = width
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}

// writeJPEG writes under another name first, so that a reader never finds
// half a file.
func writeJPEG(path string, img image.Image) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := jpeg.Encode(tmp, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
