package gen_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
	"github.com/teandresmith/sqlgen/parser"
)

// chainTable builds the single table of schema through the gen pipeline with
// tableCfg applied to it and overrides.use_pointers set to usePointers.
func chainTable(t *testing.T, schema *parser.Schema, tableCfg config.TableConfig, usePointers bool) gen.TableContext {
	t.Helper()
	input := testInput(schema)
	input.Config.Overrides.UsePointers = new(usePointers)
	input.Resolver = gotype.NewResolver(config.DialectPostgres, usePointers, input.Config.Overrides.Types)
	input.Config.Tables[schema.Tables[0].Name] = tableCfg
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}
	return contexts[0]
}

func chainColumn(t *testing.T, tc gen.TableContext, name string) gen.ColumnContext {
	t.Helper()
	for _, c := range tc.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("table %q has no column %q", tc.TableName, name)
	return gen.ColumnContext{}
}

func chainFilterField(t *testing.T, tc gen.TableContext, column string) gen.FilterFieldContext {
	t.Helper()
	for _, ff := range tc.FilterFields {
		if ff.ColumnName == column {
			return ff
		}
	}
	t.Fatalf("table %q has no filter field for column %q", tc.TableName, column)
	return gen.FilterFieldContext{}
}

// TestColumnOverrideNullableUnderUsePointersFalse pins that a nullable column
// retyped through `column_map.<col>.type` keeps the per-column nullable form
// under `overrides.use_pointers: false`: `*T`, or bare `T` for a natively
// nilable type. Only the SQL-type steps of the chain switch to a nullable
// wrapper variant at that setting.
func TestColumnOverrideNullableUnderUsePointersFalse(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "sessions",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "last_seen_ip", Type: "text", Nullable: true},
			{Name: "payload", Type: "text", Nullable: true},
			{Name: "note", Type: "text", Nullable: true},
		},
	}}}
	tableCfg := config.TableConfig{ColumnMap: map[string]config.ColumnOverride{
		"last_seen_ip": {Type: "netip.Addr", Import: "net/netip"},
		"payload":      {Type: "json.RawMessage"},
	}}
	tc := chainTable(t, schema, tableCfg, false)

	tests := []struct {
		name       string
		column     string
		wantGoType string
	}{
		{name: "column_map type takes the pointer form", column: "last_seen_ip", wantGoType: "*netip.Addr"},
		{name: "column_map natively-nilable type stays bare", column: "payload", wantGoType: "json.RawMessage"},
		{name: "dialect mapping takes the nullable wrapper", column: "note", wantGoType: "sql.NullString"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chainColumn(t, tc, tt.column).GoType; got != tt.wantGoType {
				t.Errorf("nullable column %q under use_pointers false: GoType = %q, want %q", tt.column, got, tt.wantGoType)
			}
		})
	}
}

// TestSliceLiteralFiltersThroughScalarComparator pins that a slice literal
// named by `column_map.<col>.type` or `type_map.<col>` is an opaque type, not
// an array column: its filter field is not a `comparator.Slice[T]`, while a
// real array column's is.
func TestSliceLiteralFiltersThroughScalarComparator(t *testing.T) {
	schema := &parser.Schema{Tables: []parser.Table{{
		Name: "posts",
		Columns: []parser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "mapped_tags", Type: "text"},
			{Name: "typed_tags", Type: "text"},
			{Name: "tags", Type: "text[]"},
		},
	}}}
	tableCfg := config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{"mapped_tags": {Type: "[]string"}},
		TypeMap:   map[string]string{"typed_tags": "[]string"},
	}
	tc := chainTable(t, schema, tableCfg, true)

	tests := []struct {
		name      string
		column    string
		wantSlice bool
	}{
		{name: "column_map slice literal", column: "mapped_tags", wantSlice: false},
		{name: "type_map slice literal", column: "typed_tags", wantSlice: false},
		{name: "array column", column: "tags", wantSlice: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chainColumn(t, tc, tt.column).GoType; got != "[]string" {
				t.Fatalf("column %q GoType = %q, want []string", tt.column, got)
			}
			ff := chainFilterField(t, tc, tt.column)
			if !ff.Filterable {
				t.Fatalf("column %q is not filterable, want a comparator", tt.column)
			}
			isSlice := strings.HasPrefix(ff.ComparatorType, "*comparator.Slice[")
			if isSlice != tt.wantSlice {
				t.Errorf("column %q comparator = %q, want Slice comparator = %v", tt.column, ff.ComparatorType, tt.wantSlice)
			}
			if tt.wantSlice && ff.ComparatorType != "*comparator.Slice[string]" {
				t.Errorf("array column %q comparator = %q, want *comparator.Slice[string]", tt.column, ff.ComparatorType)
			}
		})
	}
}

// TestGoogleNullableUUIDTakesNullableIDComparator pins that a nullable uuid
// column bound to google/uuid references `NullableIDComparator` in the
// GraphQL filter input.
func TestGoogleNullableUUIDTakesNullableIDComparator(t *testing.T) {
	tests := []struct {
		name        string
		usePointers bool
	}{
		{name: "use_pointers true", usePointers: true},
		{name: "use_pointers false", usePointers: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{{
				Name: "tickets",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "assignee_ref", Type: "uuid", Nullable: true},
				},
			}}}
			in := apiTestInput(t, schema)
			in.Config.Overrides.UsePointers = new(tt.usePointers)
			in.Config.Overrides.Types["uuid"] = uuidgoogle.Override()
			in.Resolver = gotype.NewResolver(config.DialectPostgres, tt.usePointers, in.Config.Overrides.Types)

			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			if got := chainColumn(t, tables[0], "assignee_ref").GoType; got != "uuid.NullUUID" {
				t.Fatalf("fixture: assignee_ref GoType = %q, want uuid.NullUUID (google binding)", got)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			if got := fieldBySQLName(t, apiCtx.Tables[0], "assignee_ref").Filter.InputTypeName; got != "NullableIDComparator" {
				t.Errorf("nullable google uuid column filter input = %q, want NullableIDComparator", got)
			}
		})
	}
}

// TestUUIDUnmarshalersReportUnparseableInput pins that the emitted GraphQL
// UUID / NullUUID unmarshalers hand the parse call's error back to the caller
// rather than discarding it and returning a zero UUID, under every
// integration.
func TestUUIDUnmarshalersReportUnparseableInput(t *testing.T) {
	tests := []struct {
		name      string
		override  config.TypeOverride
		parseFunc string
		wantFuncs []string
	}{
		{name: "stdlib", override: uuidstd.Override(), parseFunc: uuidstd.ParseFunc, wantFuncs: []string{"UnmarshalUUID"}},
		{name: "google", override: uuidgoogle.Override(), parseFunc: uuidgoogle.ParseFunc, wantFuncs: []string{"UnmarshalUUID", "UnmarshalNullUUID"}},
		{name: "gofrs", override: uuidgofrs.Override(), parseFunc: uuidgofrs.ParseFunc, wantFuncs: []string{"UnmarshalUUID", "UnmarshalNullUUID"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{Tables: []parser.Table{{
				Name: "tickets",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "assignee_ref", Type: "uuid", Nullable: true},
				},
			}}}
			in := apiTestInput(t, schema)
			in.Config.Overrides.Types["uuid"] = tt.override
			in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			// Generate sets this from the package's resolved UUID
			// integration once the API context is built.
			apiCtx.UUIDParseFunc = gotype.UUIDIntegrationFor(tt.override.Import).ParseFunc
			var buf strings.Builder
			if err := loadAPISchemaTemplate(t).ExecuteTemplate(&buf, "api/scalars", apiCtx); err != nil {
				t.Fatalf("rendering api/scalars: %v", err)
			}
			body := buf.String()
			src := body
			if !strings.HasPrefix(strings.TrimSpace(src), "package ") {
				src = "package graph\n\n" + src
			}
			file, err := goparser.ParseFile(token.NewFileSet(), "scalars_gen.go", src, 0)
			if err != nil {
				t.Fatalf("rendered api/scalars does not parse: %v\n%s", err, body)
			}
			for _, fn := range tt.wantFuncs {
				decl := chainFuncDecl(file, fn)
				if decl == nil {
					t.Errorf("rendered api/scalars declares no %s:\n%s", fn, body)
					continue
				}
				if !chainParseErrorReturned(decl.Body.List, tt.parseFunc) {
					t.Errorf("%s does not return the error of its %s(s) call:\n%s", fn, tt.parseFunc, body)
				}
			}
		})
	}
}

func chainFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Recv == nil {
			return fd
		}
	}
	return nil
}

// chainIsCall reports whether expr calls the qualified function want (e.g.
// "uuid.Parse").
func chainIsCall(expr ast.Expr, want string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name+"."+sel.Sel.Name == want
}

// chainParseErrorReturned reports whether stmts hand the parse call's error
// to the caller: either `return parse(s)` directly, or `x, err := parse(s)`
// followed by `if err != nil { return …, err }`.
func chainParseErrorReturned(stmts []ast.Stmt, parseFunc string) bool {
	for i, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if len(s.Results) == 1 && chainIsCall(s.Results[0], parseFunc) {
				return true
			}
		case *ast.AssignStmt:
			if len(s.Rhs) != 1 || len(s.Lhs) != 2 || !chainIsCall(s.Rhs[0], parseFunc) {
				continue
			}
			errIdent, ok := s.Lhs[1].(*ast.Ident)
			if !ok || errIdent.Name == "_" || i+1 >= len(stmts) {
				return false
			}
			ifStmt, ok := stmts[i+1].(*ast.IfStmt)
			if !ok {
				return false
			}
			cond, ok := ifStmt.Cond.(*ast.BinaryExpr)
			if !ok || cond.Op != token.NEQ {
				return false
			}
			if x, ok := cond.X.(*ast.Ident); !ok || x.Name != errIdent.Name {
				return false
			}
			if y, ok := cond.Y.(*ast.Ident); !ok || y.Name != "nil" {
				return false
			}
			for _, inner := range ifStmt.Body.List {
				ret, ok := inner.(*ast.ReturnStmt)
				if !ok || len(ret.Results) == 0 {
					continue
				}
				if last, ok := ret.Results[len(ret.Results)-1].(*ast.Ident); ok && last.Name == errIdent.Name {
					return true
				}
			}
			return false
		}
	}
	return false
}

// TestUUIDFKValuesStringifiedForComparatorID pins that the has-many loader
// converts uuid key values to strings, through the bare type's `.String()`,
// for the `comparator.ID` filter on the children's FK, and stringifies each
// child's FK value to map it back, under every integration.
func TestUUIDFKValuesStringifiedForComparatorID(t *testing.T) {
	tests := []struct {
		name       string
		extra      string
		wantImport string
	}{
		{name: "stdlib", extra: "", wantImport: uuidstd.ImportPath},
		{name: "google", extra: "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n", wantImport: uuidgoogle.ImportPath},
		{name: "gofrs", extra: "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/gofrs/uuid/v5\n", wantImport: uuidgofrs.ImportPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDLForTest(t, config.DialectPostgres, `
				CREATE TABLE orders (
					id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					placed_at timestamptz NOT NULL
				);
				CREATE TABLE shipments (
					id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					order_id uuid NOT NULL REFERENCES orders(id)
				);
			`)
			detectRelationships(schema)

			outDir := t.TempDir()
			cfg := loadGenerateConfig(t, outDir, config.DialectPostgres, tt.extra)
			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			src := readGenFile(t, outDir, "models_gen.go")

			if !strings.Contains(src, `"`+tt.wantImport+`"`) {
				t.Fatalf("models_gen.go does not import %q; the fixture did not bind the integration", tt.wantImport)
			}
			const header = "func (c *orderClient) loadRelationships("
			body := extractTopLevelFunc(t, src, header)
			for _, want := range []string{
				// The parents' keys become the strings comparator.ID filters on.
				"orderIDs[idx] = o.ID.String()",
				"OrderID: &comparator.ID{In: orderIDs}",
				// Each child's FK value is stringified to find its parent.
				"key := r.OrderID.String()",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s missing %q\nfull method:\n%s", header, want, body)
				}
			}
		})
	}
}

// TestViewColumnsResolveUnderGlobalScopeOnly pins that a view column takes
// its Go type from the global `overrides.types` alone: a table-scoped entry
// for the same SQL type, on the view's source table or under the view's own
// name, does not reach it.
func TestViewColumnsResolveUnderGlobalScopeOnly(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name:   "accounts",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "label", Type: "text"},
				{Name: "score", Type: "numeric"},
			},
		}},
		Views: []parser.View{{
			Name:   "account_labels",
			Schema: "public",
			Columns: []parser.Column{
				{Name: "label", Type: "text"},
				{Name: "score", Type: "numeric"},
			},
		}},
	}
	input := testInput(schema)
	input.Config.Overrides.Types = map[string]config.TypeOverride{
		"numeric": {Type: "global.Score", Import: "example.com/global"},
	}
	tableScoped := &config.OverrideConfig{Types: map[string]config.TypeOverride{
		"text":    {Type: "table.Label", Import: "example.com/table"},
		"numeric": {Type: "table.Score", Import: "example.com/table"},
	}}
	input.Config.Tables["accounts"] = config.TableConfig{Overrides: tableScoped}
	input.Config.Tables["account_labels"] = config.TableConfig{Overrides: tableScoped}
	// Generate builds the resolver from the global overrides; do the same.
	input.Resolver = gotype.NewResolver(config.DialectPostgres, true, input.Config.Overrides.Types)

	contexts, err := gen.BuildViewContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() = %d contexts, want 1", len(contexts))
	}
	got := map[string]string{}
	for _, c := range contexts[0].Columns {
		got[c.Name] = c.GoType
	}
	want := map[string]string{"label": "string", "score": "global.Score"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("view column Go types (-want +got):\n%s", diff)
	}
}
