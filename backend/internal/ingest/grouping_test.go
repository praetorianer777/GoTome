package ingest

import (
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestGroupUnit(t *testing.T) {
	type want struct {
		title string
		files []string
		// parts lists the files that are numbered parts, in order.
		parts []string
	}
	tests := []struct {
		name  string
		unit  string
		paths []string
		want  []want
	}{
		{
			name: "one title in several formats is one book",
			unit: "Halden",
			paths: []string{
				"Halden/Dusk.epub", "Halden/Dusk.pdf", "Halden/dusk.m4b", "Halden/Children of Dusk.epub",
			},
			want: []want{
				{title: "Children of Dusk", files: []string{"Halden/Children of Dusk.epub"}},
				{title: "Dusk", files: []string{"Halden/Dusk.epub", "Halden/Dusk.pdf", "Halden/dusk.m4b"}},
			},
		},
		{
			name: "a Calibre folder is one book, whatever its formats are called",
			unit: "Halden/Dusk (12)",
			paths: []string{
				"Halden/Dusk (12)/Dusk - Halden.epub", "Halden/Dusk (12)/Dusk.mobi", "Halden/Dusk (12)/Dusk - Halden.pdf",
			},
			want: []want{
				{title: "Dusk", files: []string{"Halden/Dusk (12)/Dusk - Halden.epub", "Halden/Dusk (12)/Dusk - Halden.pdf", "Halden/Dusk (12)/Dusk.mobi"}},
			},
		},
		{
			name: "a numbered folder deeper down is not a Calibre folder",
			unit: "Halden/Dusk (2)/Extras",
			paths: []string{
				"Halden/Dusk (2)/Extras/Map.epub", "Halden/Dusk (2)/Extras/Notes.epub",
			},
			want: []want{
				{title: "Map", files: []string{"Halden/Dusk (2)/Extras/Map.epub"}},
				{title: "Notes", files: []string{"Halden/Dusk (2)/Extras/Notes.epub"}},
			},
		},
		{
			name: "files named after their position are the book the folder is named after",
			unit: "Halden/Dusk",
			paths: []string{
				"Halden/Dusk/10 - End.mp3", "Halden/Dusk/2 - Middle.mp3", "Halden/Dusk/01 - Start.mp3",
			},
			want: []want{{
				title: "Dusk",
				files: []string{"Halden/Dusk/01 - Start.mp3", "Halden/Dusk/2 - Middle.mp3", "Halden/Dusk/10 - End.mp3"},
				parts: []string{"Halden/Dusk/01 - Start.mp3", "Halden/Dusk/2 - Middle.mp3", "Halden/Dusk/10 - End.mp3"},
			}},
		},
		{
			name: "the book in the folder joins its audio parts",
			unit: "Dusk",
			paths: []string{
				"Dusk/Track 1.mp3", "Dusk/Track 2.mp3", "Dusk/Dusk.epub", "Dusk/Notes on Dusk.pdf",
			},
			want: []want{
				{
					title: "Dusk",
					files: []string{"Dusk/Dusk.epub", "Dusk/Track 1.mp3", "Dusk/Track 2.mp3"},
					parts: []string{"Dusk/Track 1.mp3", "Dusk/Track 2.mp3"},
				},
				{title: "Notes on Dusk", files: []string{"Dusk/Notes on Dusk.pdf"}},
			},
		},
		{
			name: "parts named after the book and their position",
			unit: "Audio",
			paths: []string{
				"Audio/Dusk - Part 02.mp3", "Audio/Dusk - Part 01.mp3",
				"Audio/Emma (1).m4a", "Audio/Emma (2).m4a", "Audio/Persuasion.mp3",
			},
			want: []want{
				{
					title: "Dusk",
					files: []string{"Audio/Dusk - Part 01.mp3", "Audio/Dusk - Part 02.mp3"},
					parts: []string{"Audio/Dusk - Part 01.mp3", "Audio/Dusk - Part 02.mp3"},
				},
				{
					title: "Emma",
					files: []string{"Audio/Emma (1).m4a", "Audio/Emma (2).m4a"},
					parts: []string{"Audio/Emma (1).m4a", "Audio/Emma (2).m4a"},
				},
				{title: "Persuasion", files: []string{"Audio/Persuasion.mp3"}},
			},
		},
		{
			name: "a number in a title is not a position",
			unit: "Audio",
			paths: []string{
				"Audio/Celsius 212.mp3", "Audio/1957.mp3", "Audio/Trap-17.m4b", "Audio/Night Courier 2049.mp3",
			},
			want: []want{
				{title: "1957", files: []string{"Audio/1957.mp3"}},
				{title: "Celsius 212", files: []string{"Audio/Celsius 212.mp3"}},
				{title: "Night Courier 2049", files: []string{"Audio/Night Courier 2049.mp3"}},
				{title: "Trap-17", files: []string{"Audio/Trap-17.m4b"}},
			},
		},
		{
			name: "discs are ordered before their tracks",
			unit: "Dusk",
			paths: []string{
				"Dusk/CD2/01.mp3", "Dusk/CD1/02.mp3", "Dusk/CD1/01.mp3", "Dusk/CD 10/01.mp3",
			},
			want: []want{{
				title: "Dusk",
				files: []string{"Dusk/CD1/01.mp3", "Dusk/CD1/02.mp3", "Dusk/CD2/01.mp3", "Dusk/CD 10/01.mp3"},
				parts: []string{"Dusk/CD1/01.mp3", "Dusk/CD1/02.mp3", "Dusk/CD2/01.mp3", "Dusk/CD 10/01.mp3"},
			}},
		},
		{
			name:  "numbered files in the library's own folder have no folder to be named after",
			unit:  ".",
			paths: []string{"01.mp3", "02.mp3"},
			want: []want{
				{title: "01", files: []string{"01.mp3"}},
				{title: "02", files: []string{"02.mp3"}},
			},
		},
		{
			name:  "underscores were spaces, and other formats are left out",
			unit:  "x",
			paths: []string{"x/The_Left_Hand_of_Darkness.epub", "x/cover.jpg", "x/---.epub"},
			want: []want{
				{title: "---", files: []string{"x/---.epub"}},
				{title: "The Left Hand of Darkness", files: []string{"x/The_Left_Hand_of_Darkness.epub"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The order the files arrive in must not matter.
			for _, paths := range [][]string{tt.paths, reversed(tt.paths)} {
				var got []want
				for _, g := range groupUnit(tt.unit, paths) {
					w := want{title: g.Title, files: g.Files}
					for _, p := range g.Files {
						if index, ok := g.Parts[p]; ok {
							if int(index) != len(w.parts) {
								t.Errorf("%s is part %d, want %d", p, index, len(w.parts))
							}
							w.parts = append(w.parts, p)
						}
					}
					got = append(got, w)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("got\n%+v\nwant\n%+v", got, tt.want)
				}
			}
		})
	}
}

func reversed(s []string) []string {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

func TestUnitOf(t *testing.T) {
	for relPath, want := range map[string]string{
		"Dusk.epub":                ".",
		"Halden/Dusk.epub":         "Halden",
		"Halden/Dusk/CD1/01.mp3":   "Halden/Dusk",
		"Halden/Dusk/Disc 2/1.mp3": "Halden/Dusk",
		"CD1/01.mp3":               "CD1",
		"Halden/Part Two/x.epub":   "Halden/Part Two",
	} {
		if got := unitOf(relPath); got != want {
			t.Errorf("unitOf(%q) = %q, want %q", relPath, got, want)
		}
	}
}

func TestCompareNatural(t *testing.T) {
	sorted := []string{"01", "1", "2", "10", "a1", "a2", "a10", "a10b", "b"}
	for i, a := range sorted {
		for j, b := range sorted {
			got := compareNatural(a, b)
			if (got < 0) != (i < j) || (got == 0) != (i == j) {
				t.Errorf("compareNatural(%q, %q) = %d", a, b, got)
			}
		}
	}
}

// FuzzGroupUnit checks what the scan relies on whatever the files are called:
// every file of a known format lands in exactly one group, and the parts of a
// group are numbered without gaps.
func FuzzGroupUnit(f *testing.F) {
	f.Add("Dusk", "01.mp3\n02.mp3\nDusk.epub")
	f.Add("a/b", "CD1/x.mp3\nDusk (1).m4b\nDusk (2).m4b\n\x00.pdf")
	f.Fuzz(func(t *testing.T, unit, names string) {
		var paths []string
		for _, name := range strings.Split(names, "\n") {
			if name != "" {
				paths = append(paths, path.Join(unit, name))
			}
		}
		slices.Sort(paths)
		paths = slices.Compact(paths)

		seen := map[string]bool{}
		for _, g := range groupUnit(path.Clean(unit), paths) {
			if len(g.Files) == 0 {
				t.Fatal("a group without files")
			}
			indexes := map[int32]bool{}
			for _, p := range g.Files {
				if seen[p] {
					t.Fatalf("%q is in two groups", p)
				}
				seen[p] = true
				if index, ok := g.Parts[p]; ok {
					indexes[index] = true
				}
			}
			for i := range len(g.Parts) {
				if !indexes[int32(i)] {
					t.Fatalf("parts of %q are not numbered 0 to %d: %v", g.Title, len(g.Parts)-1, g.Parts)
				}
			}
		}
		for _, p := range paths {
			format := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
			_, known := catalog.KindOf(format)
			if known != seen[p] {
				t.Fatalf("%q: known format %v, grouped %v", p, known, seen[p])
			}
		}
	})
}
