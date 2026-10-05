package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	sqlgenparser "github.com/teandresmith/sqlgen/parser"
)

// probeFieldName is a resolved Go field name no template declares, so every
// struct that carries it in a render carries it because a *column* put it
// there. That is what makes the derivation below possible: the template-owned
// members of such a struct are exactly its members minus the column and
// relationship field names.
const probeFieldName = "SqlgenProbeField"

// TestGeneratedEntityMembers_MatchTemplates is the structural pin behind
// generatedEntityMembers, and the reason the reserved set cannot silently fall
// behind the templates.
//
// For each entity shape the rule distinguishes, it renders every table
// template with a probe column, parses the emitted Go, finds the struct types
// that declare the probe as a field, and collects their *other* fields plus
// the methods declared on them. What is left after subtracting the column and
// relationship field names is precisely the set of identifiers a column field
// name may not equal — so it must equal what generatedEntityMembers reserves
// for that shape. A template edit that adds a field or a method to <T>Filter,
// <T>FieldOptions, Stream<T>FieldOptions or Update<T>Item fails this test
// instead of widening the hazard unnoticed.
//
// Scope limit, in the spirit of TestGeneratedCode_AlwaysParses_CreateTemplate:
// the derivation discovers new members of the structs a column already lands
// on. A *new* template introducing a *new* struct that carries column fields
// is not discovered automatically — that remains a review-time obligation.
// The increment enum is package-scope rather than a struct member and is
// pinned separately by TestGeneratedEntityMembers_IncrementEnumType.
func TestGeneratedEntityMembers_MatchTemplates(t *testing.T) {
	shapes := []struct {
		name  string
		probe sqlgenparser.Column
		extra []sqlgenparser.Column
	}{
		{
			name:  "sole pk column",
			probe: sqlgenparser.Column{Name: "probe", Type: "uuid", PrimaryKey: true},
			extra: []sqlgenparser.Column{{Name: "label", Type: "text"}},
		},
		{
			name:  "composite pk column",
			probe: sqlgenparser.Column{Name: "probe", Type: "uuid", PrimaryKey: true},
			extra: []sqlgenparser.Column{
				{Name: "seq", Type: "integer", PrimaryKey: true},
				{Name: "label", Type: "text"},
			},
		},
		{
			name:  "plain filterable column",
			probe: sqlgenparser.Column{Name: "probe", Type: "text"},
			extra: []sqlgenparser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
		},
		{
			name:  "non-filterable column",
			probe: sqlgenparser.Column{Name: "probe", Type: "jsonb[]"},
			extra: []sqlgenparser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			tc := probeTableContext(t, shape.probe, shape.extra)
			body := renderProbeTable(t, tc)

			got := derivedMembers(t, body, tc)
			want := reservedForProbe(tc, config.DialectPostgres)

			if !slices.Equal(got, want) {
				t.Errorf("members claimed alongside a column field = %v, generatedEntityMembers reserves %v\n"+
					"a difference means the templates and reserved_fields.go have drifted", got, want)
			}
		})
	}
}

// TestGeneratedEntityMembers_IncrementEnumType pins the one reserved name that
// is not a struct member: <T>IncrementColumn is a package-level type, and the
// per-column constant <T>Increment<Field> would redeclare it for a column
// whose resolved field name is "Column".
func TestGeneratedEntityMembers_IncrementEnumType(t *testing.T) {
	tc := probeTableContext(
		t,
		sqlgenparser.Column{Name: "probe", Type: "integer"},
		[]sqlgenparser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
	)
	if len(tc.IncrementColumns) != 1 || tc.IncrementColumns[0].FieldName != probeFieldName {
		t.Fatalf("probe column is not increment-eligible; IncrementColumns = %+v", tc.IncrementColumns)
	}

	// Spell the reserved name onto the context directly: BuildTableContexts
	// rejects it, which is the behaviour under test elsewhere. Here the point
	// is what the template *emits* for it.
	reserved := reservedMemberFor("Column", tc.StructName, memberScope{IncrementEligible: true})
	if reserved == "" {
		t.Fatal(`reservedMemberFor("Column", …, IncrementEligible) = "", want the increment enum type`)
	}
	tc.IncrementColumns[0].FieldName = "Column"

	tmpl := probeTemplates(t)
	out, err := executeTemplateSafe(tmpl, "table/increment", tc)
	if err != nil {
		t.Fatalf("rendering table/increment: %v", err)
	}

	file := parseProbeFile(t, out)
	types, consts := packageLevelNames(file)
	enum := tc.StructName + "IncrementColumn"
	if !slices.Contains(types, enum) {
		t.Fatalf("table/increment declared types %v, want the enum type %q", types, enum)
	}
	if !slices.Contains(consts, enum) {
		t.Fatalf("table/increment declared constants %v, want %q among them — the constant no longer "+
			"collides with the enum type, so the %q entry in generatedEntityMembers is stale",
			consts, enum, "Column")
	}
}

// probeTableContext builds a real TableContext through the production path so
// the render sees the same shape generation does.
func probeTableContext(t *testing.T, probe sqlgenparser.Column, extra []sqlgenparser.Column) TableContext {
	t.Helper()

	schema := &sqlgenparser.Schema{
		Tables: []sqlgenparser.Table{{
			Name:    "events",
			Columns: append([]sqlgenparser.Column{probe}, extra...),
		}},
	}

	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "probe"
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.UUIDVersion = "v4"
	cfg.Overrides.UsePointers = new(true)
	cfg.Tables = map[string]config.TableConfig{
		"events": {ColumnMap: map[string]config.ColumnOverride{
			probe.Name: {Name: probeFieldName},
		}},
	}

	input, collisions := newContextBuildInput(schema, cfg)
	contexts, err := BuildTableContexts(input, collisions)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}
	// BuildTableContexts is only half of the production path: the UUID
	// generation expression is a package-wide fact wired in afterwards, and
	// without it an app-strategy probe renders `pkValue = ` and does not parse.
	attachUUIDGeneration(contexts, gotype.UUIDIntegrationFor(""))
	return contexts[0]
}

func probeTemplates(t *testing.T) *template.Template {
	t.Helper()
	resolver := gotype.NewResolver(config.DialectPostgres, true, nil)
	tmpl, err := loadTemplatesWithResolver(resolveDialect(config.DialectPostgres), resolver)
	if err != nil {
		t.Fatalf("loading templates: %v", err)
	}
	return tmpl
}

func renderProbeTable(t *testing.T, tc TableContext) string {
	t.Helper()
	body, err := renderTableBody(probeTemplates(t), tc)
	if err != nil {
		t.Fatalf("rendering table body: %v", err)
	}
	return body
}

func parseProbeFile(t *testing.T, body string) *ast.File {
	t.Helper()
	// Imports are irrelevant to parsing; only the declarations matter.
	file, err := parser.ParseFile(token.NewFileSet(), "probe.go", "package probe\n"+body, 0)
	if err != nil {
		t.Fatalf("rendered code does not parse: %v\n\n--- output ---\n%s", err, body)
	}
	return file
}

// derivedMembers returns, sorted, every identifier declared alongside the
// probe field on a struct that carries it — its sibling fields and the methods
// on that struct — minus the names the columns and relationships contribute.
func derivedMembers(t *testing.T, body string, tc TableContext) []string {
	t.Helper()
	file := parseProbeFile(t, body)

	owners := make(map[string]bool)
	members := make(map[string]bool)

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || !structHasField(st, probeFieldName) {
				continue
			}
			owners[ts.Name.Name] = true
			for _, name := range structFieldNames(st) {
				members[name] = true
			}
		}
	}
	if len(owners) == 0 {
		t.Fatalf("no struct in the rendered table declares the probe field %q", probeFieldName)
	}

	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		if owners[receiverTypeName(fd.Recv.List[0].Type)] {
			members[fd.Name.Name] = true
		}
	}

	for _, col := range tc.Columns {
		delete(members, col.FieldName)
	}
	for _, rel := range tc.Relationships {
		delete(members, rel.FieldName)
	}

	return slices.Sorted(maps.Keys(members))
}

// reservedForProbe returns what generatedEntityMembers reserves for the probe
// column in the context's shape, excluding the package-scope increment entry.
//
// The dialect is threaded in because filterability is dialect-dependent;
// probeTableContext builds every shape under PostgreSQL.
func reservedForProbe(tc TableContext, dialect config.Dialect) []string {
	probe := ColumnContext{}
	for _, col := range tc.Columns {
		if col.FieldName == probeFieldName {
			probe = col
		}
	}
	scope := columnMemberScope(probe, tc.PKColumns, dialect)

	var names []string
	for _, m := range generatedEntityMembers {
		if m.name == "Column" {
			continue // package scope — pinned separately
		}
		if m.applies(scope) {
			names = append(names, m.name)
		}
	}
	slices.Sort(names)
	return names
}

func structHasField(st *ast.StructType, name string) bool {
	return slices.Contains(structFieldNames(st), name)
}

func structFieldNames(st *ast.StructType) []string {
	var names []string
	if st.Fields == nil {
		return names
	}
	for _, field := range st.Fields.List {
		for _, ident := range field.Names {
			names = append(names, ident.Name)
		}
	}
	return names
}

func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// packageLevelNames returns the type names and constant names declared at
// package scope in the file.
func packageLevelNames(file *ast.File) (types, consts []string) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				types = append(types, s.Name.Name)
			case *ast.ValueSpec:
				if gd.Tok != token.CONST {
					continue
				}
				for _, ident := range s.Names {
					consts = append(consts, ident.Name)
				}
			}
		}
	}
	return types, consts
}
