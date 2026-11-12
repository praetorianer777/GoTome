package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/openapi"
)

const stableSnapshot = "testdata/stable-subset.txt"

var schemaRef = regexp.MustCompile(`#/components/schemas/([A-Za-z0-9_.]+)`)

// stableShape is the JSON of a version's operations and of every schema
// they reach, which is all a client of the subset depends on.
func stableShape(t *testing.T, doc *openapi.Document, version string) []byte {
	t.Helper()
	shape := map[string]any{}
	need := map[string]bool{}
	var pending []string
	for path, item := range doc.Paths {
		for method, op := range item {
			if op.Stable != version {
				continue
			}
			shape[method+" "+path] = op
			raw, err := json.Marshal(op)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range schemaRef.FindAllStringSubmatch(string(raw), -1) {
				if !need[m[1]] {
					need[m[1]] = true
					pending = append(pending, m[1])
				}
			}
		}
	}
	schemas := map[string]*openapi.Schema{}
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		schema := doc.Components.Schemas[name]
		schemas[name] = schema
		raw, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range schemaRef.FindAllStringSubmatch(string(raw), -1) {
			if !need[m[1]] {
				need[m[1]] = true
				pending = append(pending, m[1])
			}
		}
	}
	shape["schemas"] = schemas
	raw, err := json.Marshal(shape)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(withoutProse(generic))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// withoutProse drops summaries and descriptions, which a client does not
// depend on: a reworded text is no change of shape. A property that is
// called description has an object as its value and stays.
func withoutProse(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for key, value := range v {
			if _, isText := value.(string); isText && (key == "description" || key == "summary") {
				delete(v, key)
				continue
			}
			v[key] = withoutProse(value)
		}
	case []any:
		for i := range v {
			v[i] = withoutProse(v[i])
		}
	}
	return v
}

func TestTheStableSubsetKeepsItsShape(t *testing.T) {
	doc := Spec()
	versions := map[string]bool{}
	for _, rt := range testServer().routes() {
		if rt.Stable != "" {
			versions[rt.Stable] = true
		}
	}
	snapshot := map[string]string{}
	raw, err := os.ReadFile(stableSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if version, hash, ok := strings.Cut(line, " "); ok {
			snapshot[version] = hash
		}
	}
	names := make([]string, 0, len(versions))
	for v := range versions {
		names = append(names, v)
	}
	slices.Sort(names)
	for _, version := range names {
		sum := sha256.Sum256(stableShape(t, doc, version))
		got := hex.EncodeToString(sum[:])
		want, known := snapshot[version]
		switch {
		case !known:
			t.Errorf("the stable subset %s has no entry in %s; add the line %q", version, stableSnapshot, version+" "+got)
		case want != got:
			t.Errorf("the shape of the stable subset %s changed (paths, methods, parameters, request or response schemas). "+
				"Clients depend on it: undo the change, or give the changed routes a new version (Route.Stable, e.g. audio/2), "+
				"keep the old routes' shape, add the new version here and update docs/api-audio.md. "+
				"The current line would be %q", version, version+" "+got)
		}
	}
	for version := range snapshot {
		if !versions[version] {
			t.Errorf("%s lists the stable subset %s, which no route has any more", stableSnapshot, version)
		}
	}
}
