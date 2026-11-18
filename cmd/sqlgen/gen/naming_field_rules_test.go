package gen_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestViewStructName_DefaultsToSingularPascalCase pins the view half of the
// PRD §8.5 struct-name default: like a table, a view's struct name is the
// singular PascalCase of its name.
func TestViewStructName_DefaultsToSingularPascalCase(t *testing.T) {
	tests := []struct {
		name string
		view string
		want string
	}{
		{name: "a plural snake_case view name gives a singular PascalCase struct", view: "order_totals", want: "OrderTotal"},
		{name: "a view named after its suffix keeps its word", view: "s", want: "S"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			views, err := gen.BuildViewContexts(testInput(&parser.Schema{Views: []parser.View{namedView(tt.view, "")}}), nil)
			if err != nil {
				t.Fatalf("BuildViewContexts() error = %v", err)
			}
			if len(views) != 1 {
				t.Fatalf("BuildViewContexts() built %d contexts, want 1", len(views))
			}
			if views[0].StructName != tt.want {
				t.Errorf("view %q StructName = %q, want %q", tt.view, views[0].StructName, tt.want)
			}
		})
	}
}

// TestStructName_NoUpperCaseNamesStructNameKey pins that a table or view whose
// name's first letter has no upper case (`ß`) gives no exported struct name,
// and the generation error names the struct_name key that fixes it.
func TestStructName_NoUpperCaseNamesStructNameKey(t *testing.T) {
	tests := []struct {
		name    string
		view    bool
		wantKey string
	}{
		{name: "a table named ß is an error naming its tables struct_name key", wantKey: "tables.public.ß.struct_name"},
		{name: "a view named ß is an error naming its views struct_name key", view: true, wantKey: "views.public.ß.struct_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			columns := []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}
			schema := &parser.Schema{}
			var err error
			if tt.view {
				schema.Views = []parser.View{{Name: "ß", Schema: "public", Columns: columns}}
				_, err = gen.BuildViewContexts(testInput(schema), nil)
			} else {
				schema.Tables = []parser.Table{{Name: "ß", Schema: "public", Columns: columns}}
				_, err = gen.BuildTableContexts(testInput(schema), nil)
			}
			if err == nil {
				t.Fatalf("build error = nil, want an error naming %s", tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("build error = %q, want it to name %s", err, tt.wantKey)
			}
		})
	}
}

// nfrGenerate runs the full pipeline over two tables and returns every
// generated Go file concatenated:
//
//   - events has a composite PK (id, seq), with seq renamed to Sequence;
//   - tickets has a UNIQUE code column renamed to Ident and an
//     increment-eligible retry_count renamed to Retries.
func nfrGenerate(t *testing.T) string {
	t.Helper()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "seq", Type: "integer", PrimaryKey: true},
					{Name: "label", Type: "text"},
				},
			},
			{
				Name: "tickets",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "code", Type: "text", Unique: true},
					{Name: "retry_count", Type: "integer"},
				},
			},
		},
	}
	outDir := t.TempDir()
	cfg := loadGenerateConfig(t, outDir, config.DialectPostgres, `tables:
  events:
    column_map:
      seq:
        name: Sequence
  tickets:
    column_map:
      code:
        name: Ident
      retry_count:
        name: Retries
`)
	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	paths, err := filepath.Glob(filepath.Join(outDir, "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no generated Go files in %s (err %v)", outDir, err)
	}
	var all strings.Builder
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // test helper, reading back a t.TempDir the test just generated into
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		all.Write(b)
	}
	return all.String()
}

// TestColumnNameOverride_GeneratedSurfaces renders a package and checks the
// override surfaces of the PRD §8.5 field-override table that live only in
// generated code: the composite-PK struct follows a column_map rename, while
// the conflict-target constant names and the column-name value carried by a
// generated constant stay on the SQL name.
func TestColumnNameOverride_GeneratedSurfaces(t *testing.T) {
	out := nfrGenerate(t)

	t.Run("a renamed composite PK column is renamed on the PK struct", func(t *testing.T) {
		m := regexp.MustCompile(`(?s)type EventPK struct \{(.*?)\n\}`).FindStringSubmatch(out)
		if m == nil {
			t.Fatal("generated code declares no EventPK struct")
		}
		if !regexp.MustCompile(`\bSequence\b`).MatchString(m[1]) {
			t.Errorf("EventPK = {%s}, want a Sequence field", m[1])
		}
		if regexp.MustCompile(`\bSeq\b`).MatchString(m[1]) {
			t.Errorf("EventPK = {%s}, want no field under the SQL-derived name Seq", m[1])
		}
	})

	t.Run("a renamed unique column keeps its conflict constant on the SQL name", func(t *testing.T) {
		if !regexp.MustCompile(`\bTicketConflictCode\b`).MatchString(out) {
			t.Error("generated code declares no TicketConflictCode")
		}
		if regexp.MustCompile(`\bTicketConflictIdent\b`).MatchString(out) {
			t.Error("generated code declares TicketConflictIdent, want the conflict constant on the SQL name")
		}
	})

	t.Run("a renamed unique column keeps its SQL name in the conflict column list", func(t *testing.T) {
		m := regexp.MustCompile(`(?s)var ticketConflictColumns = map\[TicketConflictTarget\]\[\]string\{(.*?)\n\}`).FindStringSubmatch(out)
		if m == nil {
			t.Fatal("generated code declares no ticketConflictColumns")
		}
		if !regexp.MustCompile(`\bTicketConflictCode:\s*\{\s*"code"\s*\}`).MatchString(m[1]) {
			t.Errorf("ticketConflictColumns = {%s}, want the code target to hold the SQL name \"code\"", m[1])
		}
		if strings.Contains(strings.ToLower(m[1]), "ident") {
			t.Errorf("ticketConflictColumns = {%s}, want no entry under the Go field name Ident", m[1])
		}
	})

	t.Run("a renamed column's generated constant keeps the SQL column name as its value", func(t *testing.T) {
		if !regexp.MustCompile(`\bTicketIncrementRetries\s+TicketIncrementColumn\s*=\s*"retry_count"`).MatchString(out) {
			t.Error(`generated code does not declare TicketIncrementRetries with the SQL name "retry_count" as its value`)
		}
	})
}

// TestFieldNameCollision_OnlyOverrideIntroduced pins the boundary of the
// duplicate-field rule: two columns whose SQL names resolve to one field name
// without any override are a pre-existing schema condition sqlgen does not
// report.
func TestFieldNameCollision_OnlyOverrideIntroduced(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid"},
					{Name: "user__id", Type: "uuid"},
				},
			},
		},
	}
	tables, err := gen.BuildTableContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error = %v, want nil for a collision no override introduced", err)
	}
	// Guard the fixture: both columns must really land on one field name.
	var fields []string
	for _, c := range tables[0].Columns {
		if c.Name == "user_id" || c.Name == "user__id" {
			fields = append(fields, c.FieldName)
		}
	}
	if len(fields) != 2 || fields[0] != fields[1] {
		t.Fatalf("user_id / user__id resolve to %v, want one shared field name", fields)
	}
}

// nfrSkippedMemberSchema is a users table with a many-to-many edge to a
// composite-PK target. A relationship filter joins the junction to a single
// target PK column, so this edge gets no <T>Filter member.
func nfrSkippedMemberSchema(relName string) *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "groups",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "region", Type: "text", PrimaryKey: true},
				},
			},
			{
				Name: "user_groups",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
					{Name: "group_id", Type: "uuid", PrimaryKey: true},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:              relName,
				Type:              parser.ManyToMany,
				SourceTable:       "users",
				TargetTable:       "groups",
				JunctionTable:     "user_groups",
				JunctionLocal:     []parser.KeyPair{{Local: "user_id", Referenced: "id"}},
				JunctionReference: []parser.KeyPair{{Local: "group_id", Referenced: "id"}},
			},
		},
	}
}

// TestReservedFieldName_SkippedFilterMember pins that a list relationship's
// name is reserved even on an edge whose <T>Filter member is skipped, so the
// config stays valid as that surface grows.
func TestReservedFieldName_SkippedFilterMember(t *testing.T) {
	t.Run("an edge to a composite-PK target gets no filter member", func(t *testing.T) {
		tables, err := gen.BuildTableContexts(testInput(nfrSkippedMemberSchema("memberships")), nil)
		if err != nil {
			t.Fatalf("BuildTableContexts() error = %v", err)
		}
		for _, tc := range tables {
			if tc.TableName != "users" {
				continue
			}
			if len(tc.M2MRelationships) != 1 {
				t.Fatalf("users has %d many-to-many relationships, want the edge generated", len(tc.M2MRelationships))
			}
			for _, f := range tc.RelationshipFilters {
				if f.FieldName == tc.M2MRelationships[0].FieldName {
					t.Fatalf("users filter carries a member for %s; fixture does not skip the member", f.FieldName)
				}
			}
			return
		}
		t.Fatal("no users context built")
	})

	t.Run("the same edge derived as ToConditions is still rejected naming the filter", func(t *testing.T) {
		_, err := gen.BuildTableContexts(testInput(nfrSkippedMemberSchema("to_conditions")), nil)
		if err == nil {
			t.Fatal("BuildTableContexts() error = nil, want the relationship rejected")
		}
		for _, want := range []string{"ToConditions", "UserFilter"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("BuildTableContexts() error = %q, want it to name %s", err, want)
			}
		}
	})
}

// TestReservedFieldName_IgnoresAPIOperations pins that reserved-name
// conditioning stops at structural facts: an api.operations mask that hides
// the owning surface does not free its names, so a later mask change never
// turns a valid config invalid.
func TestReservedFieldName_IgnoresAPIOperations(t *testing.T) {
	tests := []struct {
		name     string
		column   string
		reserved string
		owner    string
	}{
		{
			name:   "an increment-eligible column renamed to Column is rejected with increment masked off",
			column: "retry_count", reserved: "Column", owner: "EventIncrementColumn",
		},
		{
			name:   "a filterable column renamed to And is rejected with read_only and get_many masked off",
			column: "label", reserved: "And", owner: "EventFilter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(reservedSchema())
			input.Config.API = &config.APIConfig{
				Enabled: true,
				Operations: &config.Operations{
					Preset:    config.PresetReadOnly,
					Increment: new(false),
					GetMany:   new(false),
				},
			}
			input.Config.Tables["events"] = config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{tt.column: {Name: tt.reserved}},
			}
			_, err := gen.BuildTableContexts(input, nil)
			if err == nil {
				t.Fatalf("BuildTableContexts() error = nil, want %q rejected under the operations mask", tt.reserved)
			}
			for _, want := range []string{tt.reserved, tt.owner} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("BuildTableContexts() error = %q, want it to name %s", err, want)
				}
			}
		})
	}
}

// nfrOwnerSchema is a composite-PK events table (id, seq) with a to-one edge
// to venues and a one-to-many edge to notes, plus a UNIQUE and an
// increment-eligible column, so every optional surface around the owner
// structs is generated.
func nfrOwnerSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "seq", Type: "integer", PrimaryKey: true},
					{Name: "venue_id", Type: "uuid"},
					{Name: "code", Type: "text", Unique: true},
					{Name: "retry_count", Type: "integer"},
					{Name: "label", Type: "text", Nullable: true},
				},
			},
			{Name: "venues", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}}},
			{
				Name: "notes",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "event_id", Type: "uuid"},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name: "venue", Type: parser.OneToOne, SourceTable: "events", TargetTable: "venues",
				Key: []parser.KeyPair{{Local: "venue_id", Referenced: "id"}},
			},
			{
				Name: "notes", Type: parser.OneToMany, SourceTable: "events", TargetTable: "notes",
				Key: []parser.KeyPair{{Local: "event_id", Referenced: "id"}},
			},
		},
	}
}

// nfrGenerateFiles runs the full pipeline over schema and parses every
// generated Go file.
func nfrGenerateFiles(t *testing.T, schema *parser.Schema) []*ast.File {
	t.Helper()
	outDir := t.TempDir()
	cfg := loadGenerateConfig(t, outDir, config.DialectPostgres, "")
	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	paths, err := filepath.Glob(filepath.Join(outDir, "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no generated Go files in %s (err %v)", outDir, err)
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, p := range paths {
		f, err := goparser.ParseFile(fset, p, nil, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		files = append(files, f)
	}
	return files
}

// nfrStructFields returns the field names of the named struct type, or
// ok=false when the package does not declare it.
func nfrStructFields(files []*ast.File, name string) (fields []string, ok bool) {
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if ts.Name.Name != name {
					continue
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct {
					return nil, false
				}
				for _, fld := range st.Fields.List {
					if len(fld.Names) == 0 {
						fields = append(fields, "<embedded>")
					}
					for _, n := range fld.Names {
						fields = append(fields, n.Name)
					}
				}
				return fields, true
			}
		}
	}
	return nil, false
}

// nfrMethods returns the methods declared with name (or *name) as receiver.
func nfrMethods(files []*ast.File, name string) []string {
	var out []string
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			typ := fd.Recv.List[0].Type
			if star, isStar := typ.(*ast.StarExpr); isStar {
				typ = star.X
			}
			if id, isIdent := typ.(*ast.Ident); isIdent && id.Name == name {
				out = append(out, fd.Name.Name)
			}
		}
	}
	return out
}

// TestNamingOwnerStructsDeclareNothing pins PRD §8.5: the entity struct,
// <T>PK, Create<T>Input and Update<T>Input carry column fields and declare
// nothing of their own, which is why they reserve no names. Each owner is
// checked for methods (none) and for fields (only column-derived ones, plus
// the user-named relationship fields on the entity struct).
func TestNamingOwnerStructsDeclareNothing(t *testing.T) {
	schema := nfrOwnerSchema()
	tables, err := gen.BuildTableContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error = %v", err)
	}
	var events *gen.TableContext
	for i := range tables {
		if tables[i].TableName == "events" {
			events = &tables[i]
		}
	}
	if events == nil {
		t.Fatal("no events context built")
	}
	columnFields := map[string]bool{}
	pkFields := map[string]bool{}
	for _, c := range events.Columns {
		columnFields[c.FieldName] = true
		if c.PrimaryKey {
			pkFields[c.FieldName] = true
		}
	}
	entityFields := map[string]bool{}
	for f := range columnFields {
		entityFields[f] = true
	}
	for _, r := range events.Relationships {
		entityFields[r.FieldName] = true
	}
	if len(events.Relationships) == 0 {
		t.Fatal("events has no relationships; fixture does not exercise relationship fields")
	}

	files := nfrGenerateFiles(t, schema)

	check := func(t *testing.T, owner string, allowed map[string]bool) {
		t.Helper()
		fields, ok := nfrStructFields(files, owner)
		if !ok {
			t.Fatalf("generated package declares no struct %s", owner)
		}
		if len(fields) == 0 {
			t.Errorf("%s has no fields, want the column fields", owner)
		}
		for _, f := range fields {
			if !allowed[f] {
				t.Errorf("%s declares field %s, which is no column (or relationship) field", owner, f)
			}
		}
		if m := nfrMethods(files, owner); len(m) != 0 {
			t.Errorf("%s declares methods %v, want none", owner, m)
		}
	}

	t.Run("the entity struct", func(t *testing.T) {
		check(t, events.StructName, entityFields)
	})
	t.Run("the composite PK struct", func(t *testing.T) {
		check(t, events.CompositePKStructName, pkFields)
	})
	t.Run("the create input", func(t *testing.T) {
		check(t, "Create"+events.StructName+"Input", columnFields)
	})
	t.Run("the update input", func(t *testing.T) {
		check(t, "Update"+events.StructName+"Input", columnFields)
	})
}
