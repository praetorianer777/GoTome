package main

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const containerPath = "META-INF/container.xml"

// Replicate writes count books under dir by copying the base corpus as often
// as it takes. Every copy gets its own title and identifier, so an importer
// sees distinct books rather than one book count times; the text is unchanged.
func Replicate(ctx context.Context, from, dir string, count int, log io.Writer) (int, error) {
	if count <= 0 {
		return 0, errors.New("-count must be positive")
	}
	sources, err := listEPUBs(from)
	if err != nil {
		return 0, err
	}
	if len(sources) == 0 {
		return 0, fmt.Errorf("no EPUBs under %s; run fetch first", from)
	}

	written := 0
	for round := 1; written < count; round++ {
		for _, src := range sources {
			if written >= count {
				break
			}
			if err := ctx.Err(); err != nil {
				return written, err
			}
			rel, err := filepath.Rel(from, src)
			if err != nil {
				return written, err
			}
			name := fmt.Sprintf("%s-%03d.epub", strings.TrimSuffix(filepath.Base(rel), ".epub"), round)
			dst := filepath.Join(dir, filepath.Dir(rel), name)
			if _, err := os.Stat(dst); err != nil {
				if err := copyEPUB(src, dst, round); err != nil {
					return written, fmt.Errorf("%s: %w", src, err)
				}
			}
			written++
			if written%1000 == 0 {
				fmt.Fprintf(log, "%d/%d\n", written, count)
			}
		}
	}
	return written, nil
}

func listEPUBs(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".epub") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// copyEPUB copies every entry as it is stored, which keeps the mimetype entry
// first and uncompressed as the format requires, and rewrites only the package
// document.
func copyEPUB(src, dst string, copyNo int) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	opfPath, err := packagePath(&zr.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	zw := zip.NewWriter(out)
	for _, file := range zr.File {
		if file.Name != opfPath {
			if err := zw.Copy(file); err != nil {
				out.Close()
				return err
			}
			continue
		}
		opf, err := readEntry(file)
		if err != nil {
			out.Close()
			return err
		}
		w, err := zw.Create(file.Name)
		if err != nil {
			out.Close()
			return err
		}
		if _, err := w.Write(retitle(opf, copyNo)); err != nil {
			out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func packagePath(zr *zip.Reader) (string, error) {
	for _, file := range zr.File {
		if file.Name != containerPath {
			continue
		}
		data, err := readEntry(file)
		if err != nil {
			return "", err
		}
		var container struct {
			Rootfiles []struct {
				FullPath string `xml:"full-path,attr"`
			} `xml:"rootfiles>rootfile"`
		}
		if err := xml.Unmarshal(data, &container); err != nil {
			return "", fmt.Errorf("%s: %w", containerPath, err)
		}
		if len(container.Rootfiles) == 0 || container.Rootfiles[0].FullPath == "" {
			return "", errors.New(containerPath + " names no package document")
		}
		return container.Rootfiles[0].FullPath, nil
	}
	return "", errors.New("no " + containerPath)
}

func readEntry(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

var (
	titleElement      = regexp.MustCompile(`(?s)(<dc:title\b[^>]*>)(.*?)(</dc:title>)`)
	identifierElement = regexp.MustCompile(`(?s)(<dc:identifier\b[^>]*>)(.*?)(</dc:identifier>)`)
)

// retitle marks the first title and the first identifier with the copy number.
// A regular expression rather than an XML round trip: re-encoding would drop
// namespace prefixes and reorder attributes in files the benchmark should see
// as they were published.
func retitle(opf []byte, copyNo int) []byte {
	opf = replaceFirst(opf, titleElement, fmt.Sprintf(" (copy %d)", copyNo))
	return replaceFirst(opf, identifierElement, fmt.Sprintf("-copy-%d", copyNo))
}

func replaceFirst(src []byte, re *regexp.Regexp, suffix string) []byte {
	loc := re.FindSubmatchIndex(src)
	if loc == nil {
		return src
	}
	end := loc[5]
	out := make([]byte, 0, len(src)+len(suffix))
	out = append(out, src[:end]...)
	out = append(out, suffix...)
	return append(out, src[end:]...)
}
