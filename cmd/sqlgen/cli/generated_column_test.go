package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestGenerate_GeneratedColumnHasNoWriteSurface generates each dialect's DDL
// for a table with generated columns. The model reads them, and no write
// surface offers them: not Create<T>Input, not Update<T>Input, and not the
// <T>IncrementColumn enum (PRD §10.3).
func TestGenerate_GeneratedColumnHasNoWriteSurface(t *testing.T) {
	tests := []struct {
		dialect config.Dialect
		sql     string
	}{
		{
			dialect: config.DialectPostgres,
			sql: `CREATE TABLE items (
				id bigserial PRIMARY KEY,
				qty integer NOT NULL,
				total integer GENERATED ALWAYS AS (qty * 2) STORED,
				vtotal integer NOT NULL GENERATED ALWAYS AS (qty + 1) STORED
			);`,
		},
		{
			dialect: config.DialectMySQL,
			sql: `CREATE TABLE items (
				id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
				qty int NOT NULL,
				total int GENERATED ALWAYS AS (qty * 2) STORED,
				vtotal int AS (qty + 1) NOT NULL
			);`,
		},
		{
			dialect: config.DialectSQLite,
			sql: `CREATE TABLE items (
				id integer PRIMARY KEY,
				qty integer NOT NULL,
				total integer GENERATED ALWAYS AS (qty * 2) STORED,
				vtotal integer AS (qty + 1) NOT NULL
			);`,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.dialect), func(t *testing.T) {
			cfgPath := writeSchemaProject(t, tt.dialect, tt.sql)
			if _, stderr, err := executeCommand("generate", "--config", cfgPath, "--quiet"); err != nil {
				t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
			}
			files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))

			for _, c := range []struct {
				typeName string
				want     []string
			}{
				{typeName: "Item", want: []string{"ID", "Qty", "Total", "Vtotal"}},
				{typeName: "CreateItemInput", want: []string{"Qty"}},
				{typeName: "UpdateItemInput", want: []string{"Qty"}},
			} {
				if diff := cmp.Diff(c.want, structFieldNames(files, c.typeName)); diff != "" {
					t.Errorf("%s fields mismatch (-want +got):\n%s", c.typeName, diff)
				}
			}
			if diff := cmp.Diff([]string{"qty"}, stringConstValues(files, "ItemIncrementColumn")); diff != "" {
				t.Errorf("ItemIncrementColumn values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// parseGeneratedFiles parses every generated Go file in dir.
func parseGeneratedFiles(t *testing.T, dir string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		files = append(files, f)
	}
	return files
}

// structFieldNames returns the field names of the struct type typeName, in
// declaration order, or nil when no file declares it.
func structFieldNames(files []*ast.File, typeName string) []string {
	var names []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != typeName {
				return true
			}
			if st, ok := spec.Type.(*ast.StructType); ok {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						names = append(names, name.Name)
					}
				}
			}
			return false
		})
	}
	return names
}

// stringConstValues returns the values of the string constants declared with
// type typeName, in declaration order.
func stringConstValues(files []*ast.File, typeName string) []string {
	var values []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			ident, ok := spec.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				return false
			}
			for _, v := range spec.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						values = append(values, s)
					}
				}
			}
			return false
		})
	}
	return values
}
