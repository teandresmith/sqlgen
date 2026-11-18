package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestGenerate_BareBuiltinBesideOwnSchemaDomainTakesDomainGoType generates a
// schema where app declares the domain app.integer over text and an app table
// has int columns written bare. sqlgen reads a bare name as the owner schema's
// type, so those columns take the domain's Go type, the text one, instead of
// int32.
func TestGenerate_BareBuiltinBesideOwnSchemaDomainTakesDomainGoType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "schema.sql", `CREATE SCHEMA app;
CREATE DOMAIN app.integer AS text;
CREATE TABLE app.t (id bigint PRIMARY KEY, a int, b int NOT NULL);
`)
	cfgPath := writeFile(t, dir, "sqlgen.yml", `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
`)
	t.Chdir(dir)
	if _, stderr, err := executeCommand("generate", "--config", cfgPath); err != nil {
		t.Fatalf("generate: %v\nstderr: %s", err, stderr)
	}

	got := gtwStructFieldTypes(t, filepath.Join(dir, "models", "models_gen.go"), "T")
	want := map[string]string{"ID": "int64", "A": "*string", "B": "string"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("struct T field types mismatch (-want +got):\n%s", diff)
	}
}

// gtwStructFieldTypes returns the field types of the struct typeName declared
// in the Go file at path, keyed by field name.
func gtwStructFieldTypes(t *testing.T, path, typeName string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if ts.Name.Name != typeName || !ok {
				continue
			}
			out := make(map[string]string)
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					out[n.Name] = types.ExprString(f.Type)
				}
			}
			return out
		}
	}
	t.Fatalf("no struct %s in %s", typeName, path)
	return nil
}
