package gen

import (
	"go/ast"
	"testing"

	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// TestKeywordTable_UnifiedClientFieldsAreDeclared pins the escape for the
// unified client's unexported fields (PRD §8.5 "Reserved Word Handling"): a
// table whose singular is a Go keyword keeps its exposed struct name, its
// client field takes the `Val` escape, and the field is still typed by the
// entity's own client, which the package declares. A view is wired into the
// unified client the same way.
func TestKeywordTable_UnifiedClientFieldsAreDeclared(t *testing.T) {
	child := sqlparser.Table{Name: "ranges", Columns: []sqlparser.Column{rwdIDColumn, {Name: "map_id", Type: "bigint"}}}
	tests := []struct {
		name   string
		schema *sqlparser.Schema
		fields []ktcField
	}{
		{name: "table maps", schema: ktcSchema("maps"), fields: []ktcField{{name: "mapVal", typ: "mapClient"}}},
		{name: "table ranges", schema: ktcSchema("ranges"), fields: []ktcField{{name: "rangeVal", typ: "rangeClient"}}},
		{name: "table defaults", schema: ktcSchema("defaults"), fields: []ktcField{{name: "defaultVal", typ: "defaultClient"}}},
		{
			name: "view interfaces",
			schema: &sqlparser.Schema{Views: []sqlparser.View{{
				Name:    "interfaces",
				Columns: []sqlparser.Column{{Name: "id", Type: "bigint"}, {Name: "label", Type: "text"}},
			}}},
			fields: []ktcField{{name: "interfaceVal", typ: "interfaceClient"}},
		},
		{
			// The entity clients are wired to each other through these fields,
			// so a loader edge between two keyword tables reaches every
			// reference to the field, not only its declaration.
			name: "keyword tables joined by a relationship",
			schema: &sqlparser.Schema{
				Tables: []sqlparser.Table{{Name: "maps", Columns: []sqlparser.Column{rwdIDColumn}}, child},
				Relationships: []sqlparser.Relationship{{
					Name: "ranges", Type: sqlparser.OneToMany, SourceTable: "maps", TargetTable: "ranges",
					Key: []sqlparser.KeyPair{{Local: "map_id", Referenced: "id"}},
				}},
			},
			fields: []ktcField{{name: "mapVal", typ: "mapClient"}, {name: "rangeVal", typ: "rangeClient"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, _ := rwdGenerate(t, tt.schema, "")
			declared := rwdTypeNames(files)

			fields := ktcClientFields(t, files)
			for _, want := range tt.fields {
				typ, ok := fields[want.name]
				if !ok {
					t.Fatalf("Client has no field %s; fields = %v", want.name, fields)
				}
				if typ != want.typ {
					t.Errorf("Client.%s has type *%s, want *%s", want.name, typ, want.typ)
				}
				if !declared[typ] {
					t.Errorf("Client.%s has type *%s, which the package does not declare", want.name, typ)
				}
			}
		})
	}
}

// ktcField is one unified Client field and the entity client type it holds.
type ktcField struct {
	name string
	typ  string
}

// ktcSchema is one keyword-named table with only a key.
func ktcSchema(table string) *sqlparser.Schema {
	return &sqlparser.Schema{Tables: []sqlparser.Table{{Name: table, Columns: []sqlparser.Column{rwdIDColumn}}}}
}

// ktcClientFields returns the unified Client struct's fields that point at a
// named type, keyed by field name, valued by that type's name.
func ktcClientFields(t *testing.T, files map[string]*ast.File) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != "Client" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, fld := range st.Fields.List {
					star, ok := fld.Type.(*ast.StarExpr)
					if !ok {
						continue
					}
					ident, ok := star.X.(*ast.Ident)
					if !ok {
						continue
					}
					for _, n := range fld.Names {
						out[n.Name] = ident.Name
					}
				}
				return out
			}
		}
	}
	t.Fatal("generated package declares no Client struct")
	return nil
}
