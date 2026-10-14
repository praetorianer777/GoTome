// Package zipsafe opens zip archives from anywhere, such as EPUBs, and
// refuses those that are built to hurt whoever reads them: too many
// entries, a zip bomb, names that leave the archive or stand twice, and
// links. Every reader of a zip goes through Open.
//
// Sizes are checked as the archive declares them. That is enough, as
// archive/zip refuses to give more bytes of an entry than it declares.
package zipsafe

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

var (
	// ErrNotZip is a file that is not a zip archive.
	ErrNotZip = errors.New("not a zip archive")
	// ErrTooLarge is an archive past a limit: too many entries, too much
	// once inflated, or inflating far more than any real file does.
	ErrTooLarge = errors.New("the archive exceeds a limit")
	// ErrUnsafe is an archive with an entry whose name leaves it or is
	// there twice, or that is a link.
	ErrUnsafe = errors.New("the archive is unsafe")
)

// Limits bound what an archive may hold.
type Limits struct {
	// MaxEntries is the number of entries.
	MaxEntries int
	// MaxTotalBytes is the size of every entry together, inflated.
	MaxTotalBytes int64
	// MaxRatio is how many times its stored size an entry may inflate to,
	// once it is larger than ratioFloor. Text inflates to a few times its
	// size, images to about theirs; a zip bomb to a thousand times.
	MaxRatio uint64
}

// ratioFloor is the size under which an entry's ratio is not looked at: a
// small file of spaces is no bomb, however well it packs.
const ratioFloor = 1 << 20

// DefaultLimits are far above any real book: an illustrated one holds a
// few hundred megabytes in a few thousand entries.
var DefaultLimits = Limits{MaxEntries: 20000, MaxTotalBytes: 4 << 30, MaxRatio: 200}

// Open reads the archive's directory and checks every entry against the
// limits before anything is inflated.
func Open(r io.ReaderAt, size int64, limits Limits) (*zip.Reader, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: %v", ErrNotZip, err)
	}
	if len(zr.File) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: %d entries", ErrTooLarge, len(zr.File))
	}
	seen := make(map[string]bool, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		if !local(f.Name) {
			return nil, fmt.Errorf("%w: the entry %q leaves the archive", ErrUnsafe, f.Name)
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("%w: the entry %q is there twice", ErrUnsafe, f.Name)
		}
		seen[f.Name] = true
		if f.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: the entry %q is a link", ErrUnsafe, f.Name)
		}
		if f.CompressedSize64 > uint64(size) {
			return nil, fmt.Errorf("%w: the entry %q is larger than the archive", ErrNotZip, f.Name)
		}
		if f.UncompressedSize64 > ratioFloor && f.UncompressedSize64/max(f.CompressedSize64, 1) > limits.MaxRatio {
			return nil, fmt.Errorf("%w: the entry %q inflates %d bytes to %d", ErrTooLarge, f.Name, f.CompressedSize64, f.UncompressedSize64)
		}
		total += f.UncompressedSize64
		if total > uint64(limits.MaxTotalBytes) {
			return nil, fmt.Errorf("%w: more than %d bytes inflated", ErrTooLarge, limits.MaxTotalBytes)
		}
	}
	return zr, nil
}

// local reports whether a name stays inside the archive: not empty, not
// absolute, no part of it "..", and no NUL. Backslashes count as
// separators, as tools on Windows write them.
func local(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) {
		return false
	}
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(name, "/") || len(name) >= 2 && name[1] == ':' && ('a' <= name[0]|0x20 && name[0]|0x20 <= 'z') {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
