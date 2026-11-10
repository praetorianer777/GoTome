package curation

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var writes = regexp.MustCompile(`(?i)\b(?:UPDATE|INSERT\s+INTO|DELETE\s+FROM)\s+([a-z_]+)`)

// queryBlock is the SQL of the named query in a file of backend/queries,
// without its comments.
func queryBlock(t *testing.T, file, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "queries", file))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "-- name: "):
			in = strings.Fields(line)[2] == name
		case in && !strings.HasPrefix(line, "--"):
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no query %s in %s", name, file)
	}
	return strings.Join(out, "\n")
}

func written(sql string) []string {
	var out []string
	for _, m := range writes.FindAllStringSubmatch(sql, -1) {
		// "DO UPDATE SET" and "an update of" are not tables.
		if name := strings.ToLower(m[1]); name != "set" && name != "of" {
			out = append(out, name)
		}
	}
	return out
}

// TestTablesCoverWhatAMergeWrites holds the snapshot to merge.sql: a table
// the merge writes that the snapshot does not hold would be lost by an
// undo.
func TestTablesCoverWhatAMergeWrites(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "queries", "merge.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var sql []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "--") {
			sql = append(sql, line)
		}
	}
	targets := written(strings.Join(sql, "\n"))
	// The steps of mergeTx that merge.sql does not hold.
	for file, names := range map[string][]string{
		"placeholders.sql": {"MoveFileIdentifiers", "MoveBookFiles", "MergeBookInto"},
		"history.sql":      {"MergeBookNotes"},
		"recommend.sql":    {"MergeRecommendationDismissals"},
		"catalogue.sql":    {"RefreshAuthorSort"},
		"trash.sql":        {"SetFileTrashed"},
	} {
		for _, name := range names {
			targets = append(targets, written(queryBlock(t, file, name))...)
		}
	}
	if len(targets) < 15 {
		t.Fatalf("found only %v: has the pattern stopped matching?", targets)
	}
	known := append(Names(Tables), Rebuilt...)
	for _, table := range targets {
		if !slices.Contains(known, table) {
			t.Errorf("merge.sql writes %s, which neither Tables nor Rebuilt holds", table)
		}
	}
}

func TestEveryTableHasWhatItIsRestoredBy(t *testing.T) {
	seen := map[string]bool{}
	for _, tb := range Tables {
		if seen[tb.Name] {
			t.Errorf("%s twice", tb.Name)
		}
		seen[tb.Name] = true
		if len(tb.PK) == 0 || !strings.Contains(tb.Where, "$1") {
			t.Errorf("%s: no key, or no books in its condition", tb.Name)
		}
		for _, k := range tb.PK {
			if len(tb.Columns) > 0 && !slices.Contains(tb.Columns, k) {
				t.Errorf("%s: key %s is not among its columns", tb.Name, k)
			}
		}
	}
	for _, tb := range SetEdit.tables() {
		if !tb.Edit {
			t.Errorf("%s is not an edit table", tb.Name)
		}
	}
}
