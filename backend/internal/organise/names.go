package organise

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Limits, in bytes, that keep a path inside what the common file systems
// and the tools around them accept (255 bytes a name, and Windows' paths
// of about 260 characters), with room for a collision's " (2)".
const (
	MaxFolderBytes = 120
	MaxNameBytes   = 160
	MaxPathBytes   = 240
	minStemBytes   = 12
)

// ErrTooLong is a path that does not fit the limits even with its name
// cut short.
var ErrTooLong = errors.New("the folders alone make the path too long")

// reserved are the names Windows does not allow for a file, with or without
// an extension.
var reserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Clean makes a value fit into one name: Unicode in its composed form
// (NFC), no path separator, control character or character Windows
// refuses (replaced by an underscore), runs of white space as one space.
func Clean(s string) string {
	s = norm.NFC.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteByte('_')
			space = false
		case unicode.IsSpace(r), r == 0x200b, r == 0xfeff:
			if !space {
				b.WriteByte(' ')
			}
			space = true
		default:
			b.WriteRune(r)
			space = false
		}
	}
	return b.String()
}

// Segment makes one folder or file name of a text: cleaned, without
// leading dots (a dot name is hidden, and GOtome's scan passes dot folders
// by), without trailing dots or spaces (Windows drops them), not a name
// Windows reserves, and no longer than max bytes. It may return "".
func Segment(s string, max int) string {
	s = strings.TrimLeft(Clean(s), ". ")
	s = cut(s, max)
	s = strings.TrimRight(s, ". ")
	if reserved[strings.ToLower(strings.SplitN(s, ".", 2)[0])] {
		s = "_" + s
	}
	return cut(s, max)
}

func cut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return strings.TrimRight(s, ". ")
}

// Layout is where a file goes, before a collision is settled.
type Layout struct {
	Dirs []string
	Stem string
	// Tail is the extension with its dot.
	Tail string
}

// Layout renders the pattern for the values.
func (p Pattern) Layout(v Values) Layout {
	s, _ := render(p.parts, v, false)
	segs := strings.Split(s, "/")
	l := Layout{}
	for _, seg := range segs[:len(segs)-1] {
		if seg = Segment(seg, MaxFolderBytes); seg != "" {
			l.Dirs = append(l.Dirs, seg)
		}
	}
	file := segs[len(segs)-1]
	ext := Clean(v["ext"])
	if ext != "" && strings.HasSuffix(file, "."+ext) {
		l.Tail = "." + ext
		file = strings.TrimSuffix(file, l.Tail)
	} else if ext != "" && strings.HasSuffix(file, ext) {
		l.Tail = ext
		file = strings.TrimSuffix(file, ext)
	}
	l.Stem = Segment(file, MaxNameBytes)
	if l.Stem == "" {
		l.Stem = "_"
	}
	return l
}

// Part is the layout of one part of an audiobook that has several: all lie
// in a folder named as the book's file would be, in the order of their
// numbers.
func (l Layout) Part(n, width int) Layout {
	dirs := append(append([]string(nil), l.Dirs...), cut(l.Stem, MaxFolderBytes))
	return Layout{Dirs: dirs, Stem: fmt.Sprintf("%0*d - %s", width, n, l.Stem), Tail: l.Tail}
}

// Rel is the path relative to the library, with forward slashes; n above 1
// makes the name " (n)". It cuts the name short to fit the limits, and says
// false when the folders leave no room.
func (l Layout) Rel(n int) (string, bool) {
	suffix := ""
	if n > 1 {
		suffix = fmt.Sprintf(" (%d)", n)
	}
	dir := strings.Join(l.Dirs, "/")
	room := MaxNameBytes - len(l.Tail) - len(suffix)
	if dir != "" {
		room = min(room, MaxPathBytes-len(dir)-1-len(l.Tail)-len(suffix))
	}
	if room < minStemBytes {
		return "", false
	}
	name := cut(l.Stem, room) + suffix + l.Tail
	if dir == "" {
		return name, true
	}
	return dir + "/" + name, true
}
