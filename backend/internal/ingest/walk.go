package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// found is a file of a known format as the walk saw it.
type found struct {
	// relPath is relative to the library's folder, with forward slashes.
	relPath  string
	format   string
	size     int64
	modified time.Time
}

// walked is what one pass over a library's folder saw.
type walked struct {
	files []found
	// unreadable are the folders that could not be listed, as relative paths.
	// What the library knows below them is neither confirmed nor gone.
	unreadable []string
	// skipped counts those folders and the files that could not be looked at.
	skipped int
}

// Folders other software keeps beside the books, which hold no books.
var ignoredFolders = map[string]bool{
	"@eaDir": true, "#recycle": true, "$RECYCLE.BIN": true,
	"lost+found": true, "System Volume Information": true,
}

// walk lists the files of known formats below root. Only an unreadable root
// is an error: one folder that cannot be opened must not end the scan of all
// the others.
func walk(ctx context.Context, root string, log *slog.Logger) (walked, error) {
	var out walked
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if full == root {
			return err
		}
		rel, relErr := filepath.Rel(root, full)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if err != nil {
			log.Warn("scan: cannot read", "path", rel, "error", err)
			out.skipped++
			if d != nil && d.IsDir() {
				out.unreadable = append(out.unreadable, rel)
			}
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || ignoredFolders[name] {
				return filepath.SkipDir
			}
			return nil
		}
		format := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if _, known := catalog.KindOf(format); !known || strings.HasPrefix(name, ".") {
			return nil
		}
		// A link may point anywhere on the server, outside the folder the
		// library was given.
		if !d.Type().IsRegular() {
			return nil
		}
		if !utf8.ValidString(rel) {
			log.Warn("scan: the name is not valid UTF-8 and cannot be stored", "path", strings.ToValidUTF8(rel, "?"))
			out.skipped++
			return nil
		}
		info, err := d.Info()
		if err != nil {
			log.Warn("scan: cannot read", "path", rel, "error", err)
			out.skipped++
			return nil
		}
		out.files = append(out.files, found{
			relPath: rel, format: format, size: info.Size(), modified: storedTime(info.ModTime()),
		})
		return nil
	})
	return out, err
}

// storedTime is a time as the database keeps it, to the microsecond, so that
// what was stored compares equal to what the disk says next time.
func storedTime(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

// errChanged is returned for a file that was written to while it was read:
// its hash would be of neither the old content nor the new.
var errChanged = errors.New("the file changed while it was read")

// hashFile returns the SHA-256 of the file and what it looked like when it
// was read. It gives up as soon as ctx ends: a large audiobook takes a while.
func hashFile(ctx context.Context, full string, relPath, format string) (found, []byte, error) {
	f, err := os.Open(full)
	if err != nil {
		return found{}, nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return found{}, nil, err
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return found{}, nil, err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return found{}, nil, err
		}
	}
	after, err := os.Stat(full)
	if err != nil {
		return found{}, nil, err
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return found{}, nil, errChanged
	}
	return found{
		relPath: relPath, format: format, size: after.Size(), modified: storedTime(after.ModTime()),
	}, h.Sum(nil), nil
}
