package gen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

// The API module boundary (PRD §26.5.6) and the constraint-error surface the
// generated error mapper is written against (PRD §26.5.5). Both are claims
// about modules other than cmd/sqlgen, so they are read from source here
// rather than through a build: the go.mod require graph of the runtime and
// parser modules, and the declarations of the runtime packages.

// twrRepoRoot returns the repository root, which is the runtime module (`./`).
// The test runs in cmd/sqlgen/gen.
func twrRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// twrParseModFile parses dir/go.mod and checks it declares wantModule, so a
// moved file cannot turn the dependency check into a read of the wrong module.
func twrParseModFile(t *testing.T, dir, wantModule string) *modfile.File {
	t.Helper()
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the repository layout
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	mf, err := modfile.Parse(path, data, nil)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if mf.Module == nil || mf.Module.Mod.Path != wantModule {
		t.Fatalf("%s declares module %v, want %s", path, mf.Module, wantModule)
	}
	return mf
}

// TestAPIModuleBoundary_NoGqlgenDependency pins the §26.5.6 module table's
// "gqlgen dep? NO" rows for the runtime and parser modules. The require list
// is the module graph go records for the module — direct and indirect — so a
// gqlgen package reached transitively through any dependency would appear in
// it as well.
func TestAPIModuleBoundary_NoGqlgenDependency(t *testing.T) {
	root := twrRepoRoot(t)
	tests := []struct {
		name   string
		dir    string
		module string
	}{
		{name: "runtime module", dir: root, module: "github.com/teandresmith/sqlgen"},
		{name: "parser module", dir: filepath.Join(root, "parser"), module: "github.com/teandresmith/sqlgen/parser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf := twrParseModFile(t, tt.dir, tt.module)
			if len(mf.Require) == 0 {
				t.Fatalf("%s/go.mod requires nothing; the check below would be vacuous", tt.dir)
			}
			for _, r := range mf.Require {
				if strings.HasPrefix(r.Mod.Path, "github.com/99designs/gqlgen") {
					t.Errorf("%s requires %s %s; the module must not depend on gqlgen", tt.module, r.Mod.Path, r.Mod.Version)
				}
			}
			for _, r := range mf.Replace {
				if strings.HasPrefix(r.New.Path, "github.com/99designs/gqlgen") || strings.HasPrefix(r.Old.Path, "github.com/99designs/gqlgen") {
					t.Errorf("%s replaces %s => %s; the module must not depend on gqlgen", tt.module, r.Old.Path, r.New.Path)
				}
			}
		})
	}
}

// twrRuntimeTopLevelNames collects every top-level identifier declared in the
// runtime module's non-test Go source. Directories that are their own module
// (cache/redis, metrics/otel, …), the parser and CLI modules, testdata and
// docs are skipped: they are not the runtime module.
func twrRuntimeTopLevelNames(t *testing.T, root string) map[string][]string {
	t.Helper()
	names := make(map[string][]string)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			base := d.Name()
			if strings.HasPrefix(base, ".") || base == "testdata" || base == "docs" {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					names[d.Name.Name] = append(names[d.Name.Name], rel)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						names[s.Name.Name] = append(names[s.Name.Name], rel)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							names[n.Name] = append(names[n.Name], rel)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking runtime module source: %v", err)
	}
	return names
}

// TestRuntimeErrors_NoConstraintSentinelAliases pins §26.5.5 "No standalone
// constraint sentinels": constraint failures are exposed only through the
// structured *database.ConstraintError and its Type constants, and no
// per-kind sentinel alias exists anywhere in the runtime.
func TestRuntimeErrors_NoConstraintSentinelAliases(t *testing.T) {
	root := twrRepoRoot(t)
	names := twrRuntimeTopLevelNames(t, root)

	// The structured surface the mapper dispatches on must be found, so an
	// empty or misdirected walk cannot pass the absence checks below.
	for _, want := range []string{
		"ConstraintError", "ErrConstraintViolation",
		"ConstraintUnique", "ConstraintForeignKey", "ConstraintCheck", "ConstraintNotNull",
	} {
		files := names[want]
		if !slices.Contains(files, filepath.Join("database", "errors.go")) {
			t.Errorf("runtime declares %s in %v, want it in database/errors.go", want, files)
		}
	}

	for _, alias := range []string{
		"ErrUniqueViolation", "ErrForeignKeyViolation", "ErrCheckViolation", "ErrNotNullViolation",
	} {
		if files, ok := names[alias]; ok {
			t.Errorf("runtime declares the constraint sentinel alias %s in %v", alias, files)
		}
	}
}
