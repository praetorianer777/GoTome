package ingest

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// Group is the files in one folder that are one book: the same title in
// several formats, and the parts of an audiobook.
type Group struct {
	// Title is read off the file or folder name. It stands in until the file
	// itself has been read.
	Title string
	// Files are the group's paths: whole files first, then the parts in the
	// order they are listened to.
	Files []string
	// Parts is the position of each part of an audiobook that comes in
	// several files, and empty for every other group.
	Parts map[string]int32
}

var (
	// discFolder is a folder that holds one disc of an audiobook, not a book.
	discFolder = regexp.MustCompile(`(?i)^(?:cd|disc|disk|part|teil|vol|volume)[\s._-]*(\d{1,3})$`)
	// trackNumber leads a file named after its position: "01 - Arrival".
	// Three digits at most, so that "1984" and "2001" stay titles.
	trackNumber = regexp.MustCompile(`^(\d{1,3})(?:\D|$)`)
	// partSuffix ends a file named after its book and its position:
	// "Dune - Part 02", "Dune (3)", "Dune_04".
	partSuffix = regexp.MustCompile(`(?i)^(.*?)[\s._,(\[-]*(?:(?:part|pt|teil|cd|disc|disk|track|chapter|kapitel)[\s._]*)?(\d{1,3})[)\]]?$`)
)

// unitOf is the folder whose files are grouped together: the file's own, or
// the one above it when the file lies in a disc folder.
func unitOf(relPath string) string {
	dir := path.Dir(relPath)
	if dir == "." || !discFolder.MatchString(path.Base(dir)) {
		return dir
	}
	// A disc folder directly in the library has no book folder above it to
	// take the title from, so it is its own book.
	if parent := path.Dir(dir); parent != "." {
		return parent
	}
	return dir
}

// part is an audio file that may be one of several.
type part struct {
	path string
	// disc and number order the parts; name breaks ties.
	disc, number int
	// base is the book's name for a file named "<book> <number>", and empty
	// for one named after its position alone.
	base string
}

// groupUnit sorts the files of one unit into books. Every path lands in
// exactly one group; paths of formats GOtome does not know are left out.
func groupUnit(unit string, paths []string) []Group {
	type draft struct {
		title string
		whole []string
		parts []part
	}
	drafts := map[string]*draft{}
	var order []string
	add := func(key, title string) *draft {
		d, ok := drafts[key]
		if !ok {
			d = &draft{title: title}
			drafts[key] = d
			order = append(order, key)
		}
		return d
	}
	whole := func(p string) {
		stem := stemOf(p)
		d := add(nameKey(stem), cleanTitle(stem))
		d.whole = append(d.whole, p)
	}

	sorted := slices.Clone(paths)
	slices.SortFunc(sorted, compareNatural)

	var byPosition []part
	byBase := map[string][]part{}
	var bases []string
	for _, p := range sorted {
		kind, known := catalog.KindOf(strings.TrimPrefix(path.Ext(p), "."))
		switch {
		case !known:
		case kind != catalog.KindAudio:
			whole(p)
		default:
			pt := partOf(unit, p)
			switch {
			case pt.number < 0:
				whole(p)
			case pt.base == "":
				byPosition = append(byPosition, pt)
			default:
				key := nameKey(pt.base)
				if _, ok := byBase[key]; !ok {
					bases = append(bases, key)
				}
				byBase[key] = append(byBase[key], pt)
			}
		}
	}

	// One file with a number in its name is a title with a number in it:
	// "Fahrenheit 451", "Catch-22".
	for _, key := range bases {
		parts := byBase[key]
		if len(parts) < 2 {
			whole(parts[0].path)
			continue
		}
		d := add(key, cleanTitle(parts[0].base))
		d.parts = append(d.parts, parts...)
	}
	// Files named after their position alone belong to the book the folder
	// is named after. In the library's own folder there is no such name.
	inDisc := slices.ContainsFunc(byPosition, func(pt part) bool { return pt.disc > 0 })
	if unit != "." && (len(byPosition) >= 2 || inDisc) {
		name := path.Base(unit)
		d := add(nameKey(name), cleanTitle(name))
		d.parts = append(d.parts, byPosition...)
	} else {
		for _, pt := range byPosition {
			whole(pt.path)
		}
	}

	groups := make([]Group, 0, len(order))
	for _, key := range order {
		d := drafts[key]
		slices.SortFunc(d.whole, compareNatural)
		g := Group{Title: d.title, Files: d.whole}
		slices.SortStableFunc(d.parts, func(a, b part) int {
			if a.disc != b.disc {
				return a.disc - b.disc
			}
			if a.number != b.number {
				return a.number - b.number
			}
			return compareNatural(a.path, b.path)
		})
		if len(d.parts) > 0 {
			g.Parts = make(map[string]int32, len(d.parts))
		}
		for i, pt := range d.parts {
			g.Files = append(g.Files, pt.path)
			g.Parts[pt.path] = int32(i)
		}
		groups = append(groups, g)
	}
	slices.SortFunc(groups, func(a, b Group) int { return compareNatural(a.Files[0], b.Files[0]) })
	return groups
}

// partOf reads the position out of an audio file's name. number is -1 for a
// name without one.
func partOf(unit, p string) part {
	pt := part{path: p, number: -1}
	if dir := path.Dir(p); dir != unit {
		if m := discFolder.FindStringSubmatch(path.Base(dir)); m != nil {
			pt.disc, _ = strconv.Atoi(m[1])
		}
	}
	stem := stemOf(p)
	if m := trackNumber.FindStringSubmatch(stem); m != nil {
		pt.number, _ = strconv.Atoi(m[1])
		return pt
	}
	if m := partSuffix.FindStringSubmatch(stem); m != nil {
		base := strings.TrimSpace(m[1])
		// "Blade Runner 2049" ends in a year, not in part 49 of "Blade Runner 2".
		if base == "" || !isDigit(base[len(base)-1]) {
			pt.number, _ = strconv.Atoi(m[2])
			if catalog.Key(base) != "" {
				pt.base = base
			}
			return pt
		}
	}
	if pt.disc > 0 {
		// In a disc folder even a file without a number is a part.
		pt.number = 0
	}
	return pt
}

func stemOf(p string) string {
	name := path.Base(p)
	return strings.TrimSuffix(name, path.Ext(name))
}

// nameKey is the form two names are compared in to decide that they are the
// same book. A name of punctuation alone has no such form and stands for
// itself.
func nameKey(name string) string {
	if key := catalog.Key(name); key != "" {
		return key
	}
	return "\x00" + strings.ToLower(name)
}

// cleanTitle makes a file name presentable: underscores were spaces once.
func cleanTitle(name string) string {
	title := strings.Join(strings.Fields(strings.ReplaceAll(name, "_", " ")), " ")
	if title == "" {
		return name
	}
	return title
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// compareNatural orders names the way a person does: "2" before "10". Names
// that differ only in leading zeros fall back to byte order, so that the
// order of any two different names is fixed.
func compareNatural(a, b string) int {
	if c := compareNumbered(a, b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func compareNumbered(a, b string) int {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, nb := digits(a), digits(b)
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(ta) != len(tb) {
				return len(ta) - len(tb)
			}
			if c := strings.Compare(ta, tb); c != 0 {
				return c
			}
			a, b = a[len(na):], b[len(nb):]
			continue
		}
		if a[0] != b[0] {
			return int(a[0]) - int(b[0])
		}
		a, b = a[1:], b[1:]
	}
	return len(a) - len(b)
}

func digits(s string) string {
	end := 0
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	return s[:end]
}
