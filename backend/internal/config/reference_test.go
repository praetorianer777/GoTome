package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// The test runs in backend/internal/config.
const (
	backendRoot = "../.."
	reference   = "../../../docs/configuration.md"
)

// deployFiles name the variables an operator sets for the compose file
// itself, which no Go code reads.
var deployFiles = []string{"../../../deploy/docker-compose.yml", "../../../deploy/.env.example"}

var variable = regexp.MustCompile(`\bGOTOME_[A-Z0-9_]*[A-Z0-9]\b`)

// rowKey is the first cell of a table row, when it is code: the variable or
// setting the row documents.
var rowKey = regexp.MustCompile("(?m)^\\| `([^`]+)` \\|(.*)$")

// variablesRead are the GOTOME_* names in the backend's code, outside its
// tests, and in the deployment's files. Scanning the source rather than
// keeping a list means a variable cannot be added without the reference.
func variablesRead(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	scan := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range variable.FindAllString(string(data), -1) {
			seen[name] = true
		}
	}
	err := filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			scan(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range deployFiles {
		scan(path)
	}
	if len(seen) < 10 {
		t.Fatalf("found only %d GOTOME_ variables; is the scan looking in the right place?", len(seen))
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func documentedRows(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, m := range rowKey.FindAllStringSubmatch(string(data), -1) {
		rows[m[1]] = m[2]
	}
	return rows
}

func TestTheReferenceDocumentsEveryVariable(t *testing.T) {
	rows := documentedRows(t)
	read := variablesRead(t)
	for _, name := range read {
		if _, ok := rows[name]; !ok {
			t.Errorf("%s is read but has no row in docs/configuration.md", name)
		}
	}
	for key := range rows {
		if variable.MatchString(key) && !slices.Contains(read, key) {
			t.Errorf("docs/configuration.md documents %s, which nothing reads any more", key)
		}
	}
}

func TestTheReferenceDocumentsEverySetting(t *testing.T) {
	rows := documentedRows(t)
	keys := map[string]bool{}
	for _, d := range settings.Definitions {
		keys[d.Key] = true
		row, ok := rows[d.Key]
		if !ok {
			t.Errorf("setting %s has no row in docs/configuration.md", d.Key)
			continue
		}
		if d.Default != "" && !strings.Contains(row, "`"+d.Default+"`") {
			t.Errorf("the row of %s does not give its default `%s`", d.Key, d.Default)
		}
	}
	for key := range rows {
		if strings.Contains(key, ".") && !strings.HasPrefix(key, "GOTOME_") && !keys[key] {
			t.Errorf("docs/configuration.md documents the setting %s, which does not exist", key)
		}
	}
}
