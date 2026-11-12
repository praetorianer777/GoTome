package catalog

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func ptr(r int16) *int16 { return &r }

func TestTagAgeRating(t *testing.T) {
	cases := []struct {
		tags []string
		want *int16
	}{
		{nil, nil},
		{[]string{"Crime", "Young adult"}, nil},
		{[]string{"Erotica"}, ptr(18)},
		{[]string{"Belletristik/Erotik"}, ptr(18)},
		{[]string{"Fantasy", "EROTIK"}, ptr(18)},
		{[]string{"FSK 12"}, ptr(12)},
		{[]string{"ab 6", "Ab 16 Jahre"}, ptr(16)},
		{[]string{"Young adult", "Adult fiction"}, ptr(18)},
		{[]string{"Eroticism studies"}, nil},
	}
	for _, c := range cases {
		got := TagAgeRating(c.tags)
		if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
			t.Errorf("TagAgeRating(%q) = %v, want %v", c.tags, got, c.want)
		}
	}
}

func TestEveryTagRatingIsAStep(t *testing.T) {
	for tag, r := range tagAgeRatings {
		if !ValidAgeRating(r) {
			t.Errorf("tag %q has rating %d, which is no step", tag, r)
		}
		if tag != Key(tag) {
			t.Errorf("tag %q is not in its Key form %q", tag, Key(tag))
		}
	}
}

func TestValidAgeRating(t *testing.T) {
	for _, r := range []int16{0, 6, 12, 16, 18} {
		if !ValidAgeRating(r) {
			t.Errorf("%d should be valid", r)
		}
	}
	for _, r := range []int16{-1, 1, 7, 17, 19} {
		if ValidAgeRating(r) {
			t.Errorf("%d should not be valid", r)
		}
	}
}

// Queries that filter by library but return nothing of a book.
var libraryOnly = []string{
	"UserHistoryLibrary", "VisibleLibraryIDs", "ListVisibleLibraries", "GetVisibleLibrary",
	"QueueNotificationEvent", "ListPassageLibraries", "IsLibraryVisible", "LatestVisibleScans",
	"CountPendingFiles", "CountFailedFiles", "CountUnsearchedLibraries",
}

// A query that decides what a viewer sees of the libraries must decide it of
// the ratings too, as the viewer's maximum is part of the same rule
// (age_limit). A new query that forgets it is caught here.
func TestEveryVisibilityQueryAppliesTheRating(t *testing.T) {
	files, err := filepath.Glob("../../queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no queries found: %v", err)
	}
	named := regexp.MustCompile(`(?m)^-- name: (\w+)`)
	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parts := named.Split(string(text), -1)
		names := named.FindAllStringSubmatch(string(text), -1)
		for i, m := range names {
			body := parts[i+1]
			if strings.Contains(body, "visible_library_ids") && !strings.Contains(body, "age_limit") && !strings.Contains(body, "age_visible") &&
				!slices.Contains(libraryOnly, m[1]) {
				t.Errorf("%s: %s filters by library but not by age rating", filepath.Base(f), m[1])
			}
		}
	}
	err = filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, "/sqlc/") {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if s := string(text); strings.Contains(s, "FROM visible_library_ids") && !strings.Contains(s, "age_limit") &&
			!slices.Contains([]string{"library.go", "results.go"}, filepath.Base(path)) {
			t.Errorf("%s filters by library but not by age rating", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
