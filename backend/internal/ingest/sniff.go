package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrWrongContent is a file whose first bytes are not those of the format
// its name claims: a web page saved as .pdf, a zip renamed .mobi.
var ErrWrongContent = errors.New("the file's content is not the format its name says")

// sniffBytes is how much of a file is looked at: a PDF's header may come
// after up to a kilobyte of anything.
const sniffBytes = 1024

// formatNames are how a person calls the formats in a message.
var formatNames = map[string]string{
	"epub": "an EPUB", "pdf": "a PDF", "mobi": "a MOBI", "azw3": "an AZW3", "azw": "an AZW",
	"m4b": "an M4B", "m4a": "an M4A", "mp3": "an MP3", "flac": "a FLAC", "ogg": "an Ogg", "opus": "an Opus",
}

// FormatName is how a message names a format: "an EPUB".
func FormatName(format string) string {
	if name, ok := formatNames[format]; ok {
		return name
	}
	return "a " + strings.ToUpper(format)
}

// sniff reports whether a file's first bytes are those of the format.
// Formats GOtome knows no signature of pass.
func sniff(format string, head []byte) bool {
	switch format {
	case "epub":
		return bytes.HasPrefix(head, []byte("PK\x03\x04"))
	case "pdf":
		return bytes.Contains(head, []byte("%PDF-"))
	case "mobi", "azw", "azw3":
		// A Palm database names its type and creator at offset 60. Old
		// Kindle books in Amazon's Topaz format are AZW too.
		if len(head) >= 68 && (string(head[60:68]) == "BOOKMOBI" || string(head[60:68]) == "TEXtREAd") {
			return true
		}
		return format == "azw" && bytes.HasPrefix(head, []byte("TPZ"))
	case "m4b", "m4a":
		return len(head) >= 8 && string(head[4:8]) == "ftyp"
	case "mp3":
		return bytes.HasPrefix(head, []byte("ID3")) || len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0
	case "flac":
		return bytes.HasPrefix(head, []byte("fLaC")) || bytes.HasPrefix(head, []byte("ID3"))
	case "ogg", "opus":
		return bytes.HasPrefix(head, []byte("OggS"))
	}
	return true
}

// checkContent returns ErrWrongContent for a file at path whose content
// is not of the format.
func checkContent(path, format string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	if !sniff(format, head[:n]) {
		return fmt.Errorf("%w: it is not %s", ErrWrongContent, FormatName(format))
	}
	return nil
}
