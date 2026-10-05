package gen

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/sql"
)

// TestNumericWidthScalarsFileCompilesWithoutCategory4Scalars covers the one
// shape neither the example suite nor the E2E goldens can reach: a schema that
// needs the `float32` marshaler pair but declares NO category-4 scalar.
//
// scalars_gen.go used to be emitted only when ExternalScalars was non-empty, so
// its import list — `fmt` / `io` / `strconv` / graphql — was always exercised by
// a UUID or Decimal body that used all four. Numeric-width scalars added a
// second reason to emit the file, and the Float32 pair alone uses only three of them. Every
// example carries a uuid PK or a numeric column, so a float32-only module
// exists nowhere in the repo; without this test an unused `fmt` would ship as a
// generated file that does not compile, and nothing would catch it.
//
// The assertion runs the real emission path — template, WrapWithPreamble,
// Format (goimports with resolution ON, which is what prunes) — and then checks
// every surviving import is actually referenced. That is the invariant, rather
// than "fmt is absent", so adding a fmt.Errorf to the body later does not make
// this fail spuriously.
func TestNumericWidthScalarsFileCompilesWithoutCategory4Scalars(t *testing.T) {
	apiCtx := &APIContext{
		Package: "graph",
		NumericWidthScalars: []APINumericWidth{
			{Name: "Float32", GoType: "float32", Scalar: "Float"},
		},
	}
	if !apiCtx.NeedsWidthMarshalers() {
		t.Fatal("NeedsWidthMarshalers() = false, want true — a sqlgen-emitted width must gate the file open")
	}

	tmpl, err := loadTemplates(sql.NewPostgresDialect())
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "api/scalars", apiCtx); err != nil {
		t.Fatalf("rendering api/scalars: %v", err)
	}
	if !strings.Contains(body.String(), "func MarshalFloat32(") {
		t.Fatalf("rendered body has no MarshalFloat32:\n%s", body.String())
	}

	raw := WrapWithPreamble(apiCtx.Package, scalarImports(nil), body.Bytes())
	formatted, err := Format(raw, "test", "scalars_gen.go")
	if err != nil {
		t.Fatalf("Format: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "scalars_gen.go", formatted, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, formatted)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			name = path[i+1:]
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if !referencesPackage(file, name) {
			t.Errorf("import %q survives Format but nothing references %q — the generated file would not compile", path, name)
		}
	}
}

// referencesPackage reports whether the file contains a `<name>.X` selector.
func referencesPackage(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return !found
	})
	return found
}
