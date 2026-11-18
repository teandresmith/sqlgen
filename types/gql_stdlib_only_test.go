package types_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGraphQLMethods_useOnlyStdlib pins PRD §26.4.1 "Sqlgen-shipped scalar
// types": types.JSON / DateTime / NullDateTime implement MarshalGQL /
// UnmarshalGQL "using only stdlib (io.Writer, any) — no gqlgen import is added
// to the runtime module, the stdlib-only invariant holds."
func TestGraphQLMethods_useOnlyStdlib(t *testing.T) {
	t.Run("the types package imports only stdlib", func(t *testing.T) {
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatalf("listing types sources: %v", err)
		}
		methods := map[string]bool{}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing %s: %v", name, err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
					t.Errorf("%s imports the non-stdlib package %s", name, path)
				}
			}
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil {
					methods[fn.Name.Name] = true
				}
			}
		}
		// Control: the methods this invariant is about are declared here.
		for _, m := range []string{"MarshalGQL", "UnmarshalGQL"} {
			if !methods[m] {
				t.Errorf("no %s method declared in the types package — the check has nothing to guard", m)
			}
		}
	})

	t.Run("the runtime module imports no gqlgen", func(t *testing.T) {
		root := ".."
		gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatalf("reading the runtime go.mod: %v", err)
		}
		for _, dep := range []string{"github.com/99designs/gqlgen", "github.com/vektah/gqlparser"} {
			if strings.Contains(string(gomod), dep) {
				t.Errorf("runtime go.mod requires %s", dep)
			}
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				base := d.Name()
				if strings.HasPrefix(base, ".") || base == "testdata" {
					return filepath.SkipDir
				}
				// A nested go.mod is another module (parser, cmd/sqlgen, …).
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if strings.HasPrefix(p, "github.com/99designs/gqlgen") || strings.HasPrefix(p, "github.com/vektah/gqlparser") {
					t.Errorf("runtime file %s imports %s", path, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking the runtime module: %v", err)
		}
	})
}
