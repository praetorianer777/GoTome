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
			unit: "Herbert",
			paths: []string{
				"Herbert/Dune.epub", "Herbert/Dune.pdf", "Herbert/dune.m4b", "Herbert/Children of Dune.epub",
			},
			want: []want{
				{title: "Children of Dune", files: []string{"Herbert/Children of Dune.epub"}},
				{title: "Dune", files: []string{"Herbert/Dune.epub", "Herbert/Dune.pdf", "Herbert/dune.m4b"}},
			},
		},
		{
			name: "files named after their position are the book the folder is named after",
			unit: "Herbert/Dune",
			paths: []string{
				"Herbert/Dune/10 - End.mp3", "Herbert/Dune/2 - Middle.mp3", "Herbert/Dune/01 - Start.mp3",
			},
			want: []want{{
				title: "Dune",
				files: []string{"Herbert/Dune/01 - Start.mp3", "Herbert/Dune/2 - Middle.mp3", "Herbert/Dune/10 - End.mp3"},
				parts: []string{"Herbert/Dune/01 - Start.mp3", "Herbert/Dune/2 - Middle.mp3", "Herbert/Dune/10 - End.mp3"},
			}},
		},
		{
			name: "the book in the folder joins its audio parts",
			unit: "Dune",
			paths: []string{
				"Dune/Track 1.mp3", "Dune/Track 2.mp3", "Dune/Dune.epub", "Dune/Notes on Dune.pdf",
			},
			want: []want{
				{
					title: "Dune",
					files: []string{"Dune/Dune.epub", "Dune/Track 1.mp3", "Dune/Track 2.mp3"},
					parts: []string{"Dune/Track 1.mp3", "Dune/Track 2.mp3"},
				},
				{title: "Notes on Dune", files: []string{"Dune/Notes on Dune.pdf"}},
			},
		},
		{
			name: "parts named after the book and their position",
			unit: "Audio",
			paths: []string{
				"Audio/Dune - Part 02.mp3", "Audio/Dune - Part 01.mp3",
				"Audio/Emma (1).m4a", "Audio/Emma (2).m4a", "Audio/Persuasion.mp3",
			},
			want: []want{
				{
					title: "Dune",
					files: []string{"Audio/Dune - Part 01.mp3", "Audio/Dune - Part 02.mp3"},
					parts: []string{"Audio/Dune - Part 01.mp3", "Audio/Dune - Part 02.mp3"},
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
				"Audio/Fahrenheit 451.mp3", "Audio/1984.mp3", "Audio/Catch-22.m4b", "Audio/Blade Runner 2049.mp3",
			},
			want: []want{
				{title: "1984", files: []string{"Audio/1984.mp3"}},
				{title: "Blade Runner 2049", files: []string{"Audio/Blade Runner 2049.mp3"}},
				{title: "Catch-22", files: []string{"Audio/Catch-22.m4b"}},
				{title: "Fahrenheit 451", files: []string{"Audio/Fahrenheit 451.mp3"}},
			},
		},
		{
			name: "discs are ordered before their tracks",
			unit: "Dune",
			paths: []string{
				"Dune/CD2/01.mp3", "Dune/CD1/02.mp3", "Dune/CD1/01.mp3", "Dune/CD 10/01.mp3",
			},
			want: []want{{
				title: "Dune",
				files: []string{"Dune/CD1/01.mp3", "Dune/CD1/02.mp3", "Dune/CD2/01.mp3", "Dune/CD 10/01.mp3"},
				parts: []string{"Dune/CD1/01.mp3", "Dune/CD1/02.mp3", "Dune/CD2/01.mp3", "Dune/CD 10/01.mp3"},
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
		"Dune.epub":                 ".",
		"Herbert/Dune.epub":         "Herbert",
		"Herbert/Dune/CD1/01.mp3":   "Herbert/Dune",
		"Herbert/Dune/Disc 2/1.mp3": "Herbert/Dune",
		"CD1/01.mp3":                "CD1",
		"Herbert/Part Two/x.epub":   "Herbert/Part Two",
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
	f.Add("Dune", "01.mp3\n02.mp3\nDune.epub")
	f.Add("a/b", "CD1/x.mp3\nDune (1).m4b\nDune (2).m4b\n\x00.pdf")
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
