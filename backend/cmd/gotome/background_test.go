package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

const module = "github.com/praetorianer777/gotome/backend"

// declared walks the backend's packages, test files left out, and calls
// visit with each package's import path and file.
func declared(t *testing.T, visit func(pkg string, file *ast.File)) {
	t.Helper()
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		visit(path.Join(module, "internal", filepath.ToSlash(rel)), file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func typeName(v any) string {
	ty := reflect.TypeOf(v)
	return ty.PkgPath() + "." + ty.Name()
}

func TestEveryJobKindHasItsWorker(t *testing.T) {
	registered := map[string]string{}
	for _, r := range (background{}).registrations() {
		name := typeName(r.args)
		if other, ok := registered[r.args.Kind()]; ok {
			t.Errorf("%s and %s are both of the kind %q", other, name, r.args.Kind())
		}
		registered[r.args.Kind()] = name
	}
	workers := map[string]bool{}
	for _, name := range registered {
		workers[name] = true
	}

	var kinds []string
	declared(t, func(pkg string, file *ast.File) {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Kind" || fn.Type.Params.NumFields() != 0 ||
				fn.Type.Results.NumFields() != 1 || !isIdent(fn.Type.Results.List[0].Type, "string") {
				continue
			}
			recv, ok := fn.Recv.List[0].Type.(*ast.Ident)
			if !ok {
				continue
			}
			kinds = append(kinds, pkg+"."+recv.Name)
		}
	})
	if len(kinds) < len(registered) {
		t.Fatalf("found %d job kinds in the sources and %d workers: the search for them missed some", len(kinds), len(registered))
	}
	for _, name := range kinds {
		if !workers[name] {
			t.Errorf("%s is a job nothing works: register its worker in registrations", name)
		}
	}

	// The workers are what serve gives River, which refuses a kind twice.
	_ = (background{}).workers()
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func TestEveryScheduleIsOfAJobThatIsWorked(t *testing.T) {
	kinds := map[string]bool{}
	for _, r := range (background{}).registrations() {
		kinds[r.args.Kind()] = true
	}
	scheduled := map[string]bool{}
	for _, s := range schedules(6 * time.Hour) {
		kind := s.Args.Kind()
		if !kinds[kind] {
			t.Errorf("%s is scheduled but nothing works it", kind)
		}
		if scheduled[kind] {
			t.Errorf("%s is scheduled twice", kind)
		}
		scheduled[kind] = true
		if s.Interval <= 0 || !slices.Contains(jobs.Queues(), s.Queue) {
			t.Errorf("%s: every %v on the queue %q", kind, s.Interval, s.Queue)
		}
	}
	if !scheduled["ingest.scan_all_libraries"] {
		t.Error("scans are not scheduled at an interval")
	}
	for _, s := range schedules(0) {
		if s.Args.Kind() == "ingest.scan_all_libraries" {
			t.Error("scans are scheduled with the interval off")
		}
	}
}

// A package that says how its job is scheduled, by a function that returns
// a jobs.Schedule, has that function called where serve's schedules are.
func TestEveryPeriodicJobIsScheduled(t *testing.T) {
	src, err := os.ReadFile("background.go")
	if err != nil {
		t.Fatal(err)
	}
	called := map[string]bool{}
	file, err := parser.ParseFile(token.NewFileSet(), "background.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	imports := map[string]string{}
	for _, spec := range file.Imports {
		p := strings.Trim(spec.Path.Value, `"`)
		imports[path.Base(p)] = p
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Name.Name != "schedules" {
			return false
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok {
					called[imports[pkg.Name]+"."+sel.Sel.Name] = true
				}
			}
			return true
		})
		return false
	})

	found := 0
	declared(t, func(pkg string, file *ast.File) {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.Results.NumFields() != 1 {
				continue
			}
			result := fn.Type.Results.List[0].Type
			sel, isSel := result.(*ast.SelectorExpr)
			local := pkg == module+"/internal/jobs" && isIdent(result, "Schedule")
			if !local && (!isSel || !isIdent(sel.X, "jobs") || sel.Sel.Name != "Schedule") {
				continue
			}
			found++
			if name := pkg + "." + fn.Name.Name; !called[name] {
				t.Errorf("%s says how a job is scheduled, but serve's schedules do not call it", name)
			}
		}
	})
	if found < 9 {
		t.Errorf("found %d functions that return a schedule: the search for them missed some", found)
	}
}
