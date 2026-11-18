package gen_test

import (
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// tgfProject is one generated temp project: the root it was generated in,
// the Generate result and every top-level Go file of the output package,
// parsed with comments.
type tgfProject struct {
	root   string
	cfg    *config.RootConfig
	schema *parser.Schema
	result *gen.GenerateResult
	files  map[string]*ast.File
}

// tgfGenerate writes postgres ddl and yml into a temp project, loads and pre-validates
// the config and detects relationships the way the CLI does, runs Generate and parses the output package
// (output.dir must be ./models).
// Each edit runs on the parsed schema before Generate, e.g. to add a view.
func tgfGenerate(t *testing.T, ddl, yml string, edits ...func(*parser.Schema)) tgfProject {
	t.Helper()
	p, err := tgfTryGenerate(t, ddl, yml, edits...)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return p
}

// tgfTryGenerate is tgfGenerate returning the pipeline error instead of
// failing the test.
func tgfTryGenerate(t *testing.T, ddl, yml string, edits ...func(*parser.Schema)) (tgfProject, error) {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{"schema.sql": ddl, "sqlgen.yml": yml} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, config.DialectPostgres, ddl)
	// The CLI detects relationships on the parsed schema before Generate.
	parser.DetectRelationships(schema)
	for _, edit := range edits {
		edit(schema)
	}
	cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Fatalf("ValidatePreParse() error: %v", err)
	}
	res, err := gen.Generate(schema, cfg, "test")
	if err != nil {
		return tgfProject{root: root, cfg: cfg, schema: schema}, err
	}
	return tgfProject{root: root, cfg: cfg, schema: schema, result: res, files: tgfParseDir(t, filepath.Join(root, "models"))}, nil
}

// tgfParseDir parses every .go file directly under dir.
func tgfParseDir(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	files := make(map[string]*ast.File)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := goparser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, goparser.ParseComments)
		if err != nil {
			t.Fatalf("parsing generated %s: %v", e.Name(), err)
		}
		files[e.Name()] = f
	}
	return files
}

func (p tgfProject) sortedFiles() []*ast.File {
	names := make([]string, 0, len(p.files))
	for n := range p.files {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*ast.File, 0, len(names))
	for _, n := range names {
		out = append(out, p.files[n])
	}
	return out
}

// typeSpec returns the named type declaration.
func (p tgfProject) typeSpec(name string) (*ast.TypeSpec, bool) {
	for _, f := range p.sortedFiles() {
		if ts, ok := tgfTypeIn(f, name); ok {
			return ts, true
		}
	}
	return nil, false
}

// structField returns the field named name of struct type typeName.
func (p tgfProject) structField(typeName, name string) (*ast.Field, bool) {
	ts, ok := p.typeSpec(typeName)
	if !ok {
		return nil, false
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil, false
	}
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == name {
				return f, true
			}
		}
	}
	return nil, false
}

// funcDecl returns the function (recv == "") or method on recv named name.
func (p tgfProject) funcDecl(recv, name string) (*ast.FuncDecl, bool) {
	for _, f := range p.sortedFiles() {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != name {
				continue
			}
			if tgfRecvName(fd) == recv {
				return fd, true
			}
		}
	}
	return nil, false
}

// valueSpec returns the const/var spec declaring name.
func (p tgfProject) valueSpec(name string) (*ast.ValueSpec, token.Token, bool) {
	for _, f := range p.sortedFiles() {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for _, n := range vs.Names {
					if n.Name == name {
						return vs, gd.Tok, true
					}
				}
			}
		}
	}
	return nil, 0, false
}

func tgfRecvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// tgfExpr renders an expression back to source.
func tgfExpr(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "<unprintable>"
	}
	return b.String()
}

// tgfYAML is a postgres/pgx project config generating into ./models; each
// test appends the blocks it exercises.
const tgfYAML = `version: v1
input:
  dialect: postgres
  paths: [./schema.sql]
output:
  driver: pgx
  dir: ./models
  package: models
`

func tgfTable(t *testing.T, p tgfProject, name string) gen.TableContext {
	t.Helper()
	tc, ok := tableContextByName(p.result.Tables, name)
	if !ok {
		t.Fatalf("Generate() built no context for table %s", name)
	}
	return tc
}

func tgfColumn(t *testing.T, tc gen.TableContext, name string) gen.ColumnContext {
	t.Helper()
	for _, c := range tc.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("table %s has no column %s", tc.TableName, name)
	return gen.ColumnContext{}
}

func tgfRelationship(t *testing.T, tc gen.TableContext, name string) gen.RelationshipContext {
	t.Helper()
	for _, r := range tc.Relationships {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("table %s has no relationship %s", tc.TableName, name)
	return gen.RelationshipContext{}
}

// TestGenerate_SoftDeleteColumnsListOrder pins that generation.soft_delete_columns
// replaces the default list and is checked in list order: a table holding two
// listed columns soft-deletes through the earlier one, and a default-list
// column the explicit list leaves out is not detected.
func TestGenerate_SoftDeleteColumnsListOrder(t *testing.T) {
	const ddl = `CREATE TABLE products (
  id uuid PRIMARY KEY,
  deleted_at timestamptz,
  archived_at timestamptz,
  is_hidden boolean NOT NULL DEFAULT false
);`
	tests := []struct {
		name       string
		list       string
		wantColumn string
		wantType   config.SoftDeleteType
	}{
		{
			name:       "timestamp listed first",
			list:       "    - name: archived_at\n      type: timestamp\n    - name: is_hidden\n      type: bool\n",
			wantColumn: "archived_at",
			wantType:   config.SoftDeleteTimestamp,
		},
		{
			name:       "bool listed first",
			list:       "    - name: is_hidden\n      type: bool\n    - name: archived_at\n      type: timestamp\n",
			wantColumn: "is_hidden",
			wantType:   config.SoftDeleteBool,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tgfGenerate(t, ddl, tgfYAML+"generation:\n  soft_delete_columns:\n"+tt.list)
			sd := tgfTable(t, p, "products").SoftDelete
			if sd == nil {
				t.Fatal("products.SoftDelete = nil, want a soft-delete column from the explicit list")
			}
			if sd.Column != tt.wantColumn || sd.Strategy != tt.wantType {
				t.Errorf("products soft delete = %s (%s), want %s (%s); deleted_at is not in the explicit list", sd.Column, sd.Strategy, tt.wantColumn, tt.wantType)
			}
		})
	}
}

// TestGenerate_UpdateColumnsExplicitList pins that generation.update_columns
// replaces the default list: a listed column is auto-set and a default-list
// column (updated_at) the explicit list leaves out is not.
func TestGenerate_UpdateColumnsExplicitList(t *testing.T) {
	const ddl = `CREATE TABLE products (
  id uuid PRIMARY KEY,
  updated_at timestamptz NOT NULL DEFAULT now(),
  touched_at timestamptz NOT NULL DEFAULT now()
);`
	p := tgfGenerate(t, ddl, tgfYAML+"generation:\n  update_columns: [touched_at]\n")
	ucs := tgfTable(t, p, "products").UpdateColumns
	got := make([]string, 0, len(ucs))
	for _, uc := range ucs {
		got = append(got, uc.Name)
	}
	if diff := cmp.Diff([]string{"touched_at"}, got); diff != "" {
		t.Errorf("products update columns mismatch (-want +got):\n%s", diff)
	}
}

// TestGenerate_ExcludeColumnsRemovedFromEveryTable pins that a column named in
// generation.exclude_columns is left out of the generated model of every
// table that has it.
func TestGenerate_ExcludeColumnsRemovedFromEveryTable(t *testing.T) {
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL, internal_notes text);
CREATE TABLE orders (id uuid PRIMARY KEY, total integer NOT NULL, internal_notes text);`
	p := tgfGenerate(t, ddl, tgfYAML+"generation:\n  exclude_columns: [internal_notes]\n")
	for _, tt := range []struct{ table, structName, kept string }{
		{"products", "Product", "Name"},
		{"orders", "Order", "Total"},
	} {
		if _, ok := p.structField(tt.structName, tt.kept); !ok {
			t.Errorf("%s has no field %s; the fixture expects the model to be generated", tt.structName, tt.kept)
		}
		if _, ok := p.structField(tt.structName, "InternalNotes"); ok {
			t.Errorf("%s has an InternalNotes field, want internal_notes excluded", tt.structName)
		}
		for _, c := range tgfTable(t, p, tt.table).Columns {
			if c.Name == "internal_notes" {
				t.Errorf("%s context carries internal_notes, want it excluded", tt.table)
			}
		}
	}
}

// TestGenerate_UnconditionalMethodsOnEveryPKTable pins the methods generated
// for every table with a primary key whatever its other schema facts: a bare
// table (no soft-delete, incrementable or nested surface) still gets all of
// them on its client interface.
func TestGenerate_UnconditionalMethodsOnEveryPKTable(t *testing.T) {
	const ddl = `CREATE TABLE notes (id uuid PRIMARY KEY, body text NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML)
	ts, ok := p.typeSpec("NoteClient")
	if !ok {
		t.Fatal("no NoteClient interface generated")
	}
	iface, ok := ts.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatalf("NoteClient is %T, want an interface", ts.Type)
	}
	have := make(map[string]bool)
	for _, m := range iface.Methods.List {
		for _, n := range m.Names {
			have[n.Name] = true
		}
	}
	for _, want := range []string{
		"Get", "GetMany", "Count", "Exists", "ExistsWhere", "Stream", "Paginate", "Connection",
		"Create", "CreateMany", "Update", "UpdateMany", "UpdateWhere",
		"HardDelete", "HardDeleteMany", "HardDeleteWhere",
	} {
		if !have[want] {
			t.Errorf("NoteClient has no %s method", want)
		}
		if _, ok := p.funcDecl("noteClient", want); !ok {
			t.Errorf("noteClient implements no %s method", want)
		}
	}
}

// TestGenerate_PageSizeTableWins pins that tables.<t>.page_size beats
// generation.page_size for that table and only that table, down to the
// client constructor.
func TestGenerate_PageSizeTableWins(t *testing.T) {
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY);
CREATE TABLE orders (id uuid PRIMARY KEY);`
	p := tgfGenerate(t, ddl, tgfYAML+"generation:\n  page_size: 40\ntables:\n  products:\n    page_size: 15\n")
	if got := tgfTable(t, p, "products").PageSize; got != 15 {
		t.Errorf("products PageSize = %d, want 15 (table over global 40)", got)
	}
	if got := tgfTable(t, p, "orders").PageSize; got != 40 {
		t.Errorf("orders PageSize = %d, want 40 (global)", got)
	}
	if got := tgfConstructorField(t, p, "newProductClient", "pageSize"); got != "15" {
		t.Errorf("newProductClient sets pageSize: %s, want 15", got)
	}
}

// tgfConstructorField returns the source of the value the composite literal
// in constructor fn assigns to key.
func tgfConstructorField(t *testing.T, p tgfProject, fn, key string) string {
	t.Helper()
	fd, ok := p.funcDecl("", fn)
	if !ok {
		t.Fatalf("no %s function generated", fn)
	}
	var got string
	ast.Inspect(fd, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
				got = tgfExpr(kv.Value)
			}
		}
		return got == ""
	})
	if got == "" {
		t.Fatalf("%s assigns no %s", fn, key)
	}
	return got
}

// TestGenerate_UUIDVersionAppliesOnlyToAppStrategy pins that
// primary_key.uuid_version takes effect only under strategy app: the app
// table generates its key with the v7 call, while a db-strategy table with
// the same uuid_version generates no key at all.
func TestGenerate_UUIDVersionAppliesOnlyToAppStrategy(t *testing.T) {
	const ddl = `CREATE TABLE events (id uuid PRIMARY KEY, name text NOT NULL);
CREATE TABLE audits (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML+`tables:
  events:
    primary_key:
      strategy: app
      uuid_version: v7
  audits:
    primary_key:
      strategy: db
      uuid_version: v7
`)
	if got := tgfTable(t, p, "events").PKAutoGenExpr; got != "uuid.NewV7()" {
		t.Errorf("events PKAutoGenExpr = %q, want %q (strategy app, uuid_version v7)", got, "uuid.NewV7()")
	}
	if got := tgfTable(t, p, "audits").PKAutoGenExpr; got != "" {
		t.Errorf("audits PKAutoGenExpr = %q, want none: uuid_version does not apply under strategy db", got)
	}
	fd, ok := p.funcDecl("auditClient", "Create")
	if !ok {
		t.Fatal("no auditClient.Create generated")
	}
	ast.Inspect(fd, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && (sel.Sel.Name == "NewV7" || sel.Sel.Name == "NewV4") {
			t.Errorf("auditClient.Create calls %s, want no generated key under strategy db", tgfExpr(sel))
		}
		return true
	})
}

// TestGenerate_ColumnMapTypeNullableTakesNoWrapper pins that a nullable column
// retyped by column_map.<col>.type resolves to *T even when overrides.types
// declares a nullable wrapper for its SQL type: decimal.Decimal becomes
// *decimal.Decimal, not decimal.NullDecimal.
func TestGenerate_ColumnMapTypeNullableTakesNoWrapper(t *testing.T) {
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY, price numeric, cost numeric);`
	p := tgfGenerate(t, ddl, tgfYAML+`overrides:
  types:
    numeric:
      type: decimal.Decimal
      import: github.com/shopspring/decimal
      nullable: decimal.NullDecimal
tables:
  products:
    column_map:
      price:
        type: decimal.Decimal
        import: github.com/shopspring/decimal
`)
	tc := tgfTable(t, p, "products")
	if got := tgfColumn(t, tc, "price").GoType; got != "*decimal.Decimal" {
		t.Errorf("products.price GoType = %q, want %q", got, "*decimal.Decimal")
	}
	// The same SQL type outside column_map does take the wrapper, so the
	// fixture's overrides.types binding is live.
	if got := tgfColumn(t, tc, "cost").GoType; got != "decimal.NullDecimal" {
		t.Errorf("products.cost GoType = %q, want %q", got, "decimal.NullDecimal")
	}
}

// tgfRelDDL holds tables with no FOREIGN KEY constraints, so every
// relationship comes from config.
const tgfRelDDL = `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL, detail_id uuid);
CREATE TABLE product_details (id uuid PRIMARY KEY, body text NOT NULL);
CREATE TABLE attachments (id uuid PRIMARY KEY, product_id uuid NOT NULL, created_at timestamptz NOT NULL, title text NOT NULL);
CREATE TABLE tags (id uuid PRIMARY KEY, label text NOT NULL);
CREATE TABLE product_tags (product_id uuid NOT NULL, tag_id uuid NOT NULL, PRIMARY KEY (product_id, tag_id));`

// TestGenerate_RelationshipSideCaseInsensitive pins that an O2O entry's side
// is matched case-insensitively.
func TestGenerate_RelationshipSideCaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		side string
		want parser.RelationshipSide
	}{
		{name: "upper-case child", side: "CHILD", want: parser.SideChild},
		{name: "mixed-case child", side: "Child", want: parser.SideChild},
		{name: "upper-case parent", side: "PARENT", want: parser.SideParent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tgfGenerate(t, tgfRelDDL, tgfYAML+`tables:
  products:
    relationships:
      - name: Detail
        type: o2o
        side: `+tt.side+`
        table: product_details
        fk: detail_id
`)
			if got := tgfRelationship(t, tgfTable(t, p, "products"), "Detail").Side; got != tt.want {
				t.Errorf("Detail side = %v for side: %s, want %v", got, tt.side, tt.want)
			}
		})
	}
}

// TestGenerate_RelationshipSideIgnoredOffO2O pins that side is ignored on o2m
// and m2m entries: setting side: child changes nothing in the generated
// package.
func TestGenerate_RelationshipSideIgnoredOffO2O(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{
			name:  "o2m entry",
			entry: "      - name: Attachments\n        type: o2m\n        table: attachments\n        fk: product_id\n",
		},
		{
			name:  "m2m entry",
			entry: "      - name: Tags\n        type: m2m\n        table: tags\n        junction: product_tags\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tgfGenerate(t, tgfRelDDL, tgfYAML+"tables:\n  products:\n    relationships:\n"+tt.entry)
			want := tgfReadOutput(t, base.root)
			sided := tgfGenerate(t, tgfRelDDL, tgfYAML+"tables:\n  products:\n    relationships:\n"+tt.entry+"        side: child\n")
			got := tgfReadOutput(t, sided.root)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("side: child on an %s changed the generated package (-without +with):\n%s", tt.name, diff)
			}
		})
	}
}

// tgfReadOutput returns every generated file under root/models keyed by its
// path relative to models.
func tgfReadOutput(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	dir := filepath.Join(root, "models")
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path) //nolint:gosec // test-written temp module
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("reading generated output: %v", err)
	}
	return out
}

// TestGenerate_M2MJunctionKeysInferred pins that an M2M entry with neither
// junction_local_fk nor junction_reference_fk reads its junction columns
// from the linked table names: product_id for products, tag_id for tags.
func TestGenerate_M2MJunctionKeysInferred(t *testing.T) {
	p := tgfGenerate(t, tgfRelDDL, tgfYAML+`tables:
  products:
    relationships:
      - name: Tags
        type: m2m
        table: tags
        junction: product_tags
`)
	rel := tgfRelationship(t, tgfTable(t, p, "products"), "Tags")
	if rel.JunctionLocalFK != "product_id" || rel.JunctionReferenceFK != "tag_id" {
		t.Errorf("Tags junction keys = (%q, %q), want the inferred (%q, %q)", rel.JunctionLocalFK, rel.JunctionReferenceFK, "product_id", "tag_id")
	}
}

// TestGenerate_M2MJunctionKeyOverrides pins that junction_local_fk names the
// junction column for this table and junction_reference_fk the one for the
// related table, each in place of the {table_singular}_id the names would
// infer (product_id, tag_id).
func TestGenerate_M2MJunctionKeyOverrides(t *testing.T) {
	const ddl = tgfRelDDL + `
CREATE TABLE product_tag_links (prod_id uuid NOT NULL, tg_id uuid NOT NULL, PRIMARY KEY (prod_id, tg_id));`
	p := tgfGenerate(t, ddl, tgfYAML+`tables:
  products:
    relationships:
      - name: Tags
        type: m2m
        table: tags
        junction: product_tag_links
        junction_local_fk: prod_id
        junction_reference_fk: tg_id
`)
	rel := tgfRelationship(t, tgfTable(t, p, "products"), "Tags")
	if rel.JunctionLocalFK != "prod_id" {
		t.Errorf("Tags JunctionLocalFK = %q, want %q from junction_local_fk", rel.JunctionLocalFK, "prod_id")
	}
	if rel.JunctionReferenceFK != "tg_id" {
		t.Errorf("Tags JunctionReferenceFK = %q, want %q from junction_reference_fk", rel.JunctionReferenceFK, "tg_id")
	}
}

// TestGenerate_RelationshipSortDirectionDefaultsAscending pins that a sort
// entry with no direction orders ascending, down to the loader's default
// sort on the client.
func TestGenerate_RelationshipSortDirectionDefaultsAscending(t *testing.T) {
	p := tgfGenerate(t, tgfRelDDL, tgfYAML+`tables:
  products:
    relationships:
      - name: Attachments
        type: o2m
        table: attachments
        fk: product_id
        sort:
          - column: created_at
          - column: title
            direction: desc
`)
	rel := tgfRelationship(t, tgfTable(t, p, "products"), "Attachments")
	want := []gen.SortContext{{Column: "created_at", Direction: "ASC"}, {Column: "title", Direction: "DESC"}}
	if diff := cmp.Diff(want, rel.Sort); diff != "" {
		t.Errorf("Attachments sort mismatch (-want +got):\n%s", diff)
	}
	const wantLit = `[]sql.Sort{{Column: "created_at", Direction: sql.Asc}, {Column: "title", Direction: sql.Desc}}`
	if got := tgfConstructorField(t, p, "newProductClient", "attachmentsDefaultSort"); got != wantLit {
		t.Errorf("newProductClient sets attachmentsDefaultSort: %s, want %s", got, wantLit)
	}
}

// TestGenerate_RelationshipDescriptionIsFieldDoc pins that a relationship's
// description is the Go doc comment on its entity-struct field.
func TestGenerate_RelationshipDescriptionIsFieldDoc(t *testing.T) {
	p := tgfGenerate(t, tgfRelDDL, tgfYAML+`tables:
  products:
    relationships:
      - name: Attachments
        type: o2m
        table: attachments
        fk: product_id
        description: Files uploaded against the product.
`)
	field, ok := p.structField("Product", "Attachments")
	if !ok {
		t.Fatal("Product has no Attachments field")
	}
	if got := strings.TrimSpace(field.Doc.Text()); !strings.Contains(got, "Files uploaded against the product.") {
		t.Errorf("Product.Attachments doc comment = %q, want it to carry the configured description %q", got, "Files uploaded against the product.")
	}
}

// TestGenerate_TableNestedMutationsInertWithoutGlobal pins that
// tables.<t>.nested_mutations has no effect unless
// generation.nested_mutations.enabled is true: with the global block absent
// no nested surface is generated for the table, and enabling the global
// switch with the same table block generates it.
func TestGenerate_TableNestedMutationsInertWithoutGlobal(t *testing.T) {
	const ddl = `CREATE TABLE users (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL);
CREATE TABLE orders (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id), total integer NOT NULL);`
	const tableBlock = "tables:\n  users:\n    nested_mutations:\n      relationships:\n        - name: Orders\n"
	tests := []struct {
		name       string
		global     string
		wantNested bool
	}{
		{name: "global block absent", global: "", wantNested: false},
		{name: "global switch off", global: "generation:\n  nested_mutations:\n    enabled: false\n", wantNested: false},
		{name: "global switch on", global: "generation:\n  nested_mutations:\n    enabled: true\n", wantNested: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tgfGenerate(t, ddl, tgfYAML+tt.global+tableBlock)
			users := tgfTable(t, p, "users")
			if got := users.Nested != nil; got != tt.wantNested {
				t.Errorf("users nested surface present = %v, want %v", got, tt.wantNested)
			}
			_, has := p.funcDecl("userClient", "CreateWithRelated")
			if has != tt.wantNested {
				t.Errorf("userClient.CreateWithRelated generated = %v, want %v", has, tt.wantNested)
			}
		})
	}
}

// TestGenerate_KnownLiteralOverridesNeedNoImport pins that an overrides.types
// entry naming json.RawMessage, types.JSON or net.IP needs no `import`: it
// passes validation, the field takes the type, the generated file imports the
// literal's package, and the package type-checks.
func TestGenerate_KnownLiteralOverridesNeedNoImport(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const ddl = `CREATE TABLE payloads (id uuid PRIMARY KEY, raw jsonb NOT NULL, doc json NOT NULL, addr inet NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML+`overrides:
  types:
    jsonb:
      type: json.RawMessage
    json:
      type: types.JSON
    inet:
      type: net.IP
`)
	if _, err := gen.ValidateGeneration(p.schema, p.cfg); err != nil {
		t.Errorf("ValidateGeneration() error = %v, want nil", err)
	}
	f := p.files["models_gen.go"]
	if f == nil {
		t.Fatal("no models_gen.go generated")
	}
	imports := make(map[string]bool)
	for _, im := range f.Imports {
		imports[strings.Trim(im.Path.Value, `"`)] = true
	}
	for _, tt := range []struct{ field, goType, importPath string }{
		{"Raw", "json.RawMessage", "encoding/json"},
		{"Doc", "types.JSON", "github.com/teandresmith/sqlgen/types"},
		{"Addr", "net.IP", "net"},
	} {
		field, ok := p.structField("Payload", tt.field)
		if !ok {
			t.Errorf("Payload has no %s field", tt.field)
			continue
		}
		if got := tgfExpr(field.Type); got != tt.goType {
			t.Errorf("Payload.%s type = %s, want %s", tt.field, got, tt.goType)
		}
		if !imports[tt.importPath] {
			t.Errorf("models_gen.go does not import %q for %s", tt.importPath, tt.goType)
		}
	}
	vetGeneratedModels(t, p.root, repo)
}

// tgfAddView returns a schema edit appending a public view with the given
// NOT NULL columns.
func tgfAddView(name string, cols ...parser.Column) func(*parser.Schema) {
	return func(s *parser.Schema) {
		s.Views = append(s.Views, parser.View{Name: name, Schema: "public", Columns: cols})
	}
}

func tgfView(t *testing.T, p tgfProject, name string) gen.ViewContext {
	t.Helper()
	for _, v := range p.result.Views {
		if v.ViewName == name {
			return v
		}
	}
	t.Fatalf("Generate() built no context for view %s", name)
	return gen.ViewContext{}
}

// TestGenerate_ViewStructNameDerivedWhenUnset pins that a views.<v> entry
// without struct_name is valid and the struct name derives from the view
// name, singular PascalCase as for a table.
func TestGenerate_ViewStructNameDerivedWhenUnset(t *testing.T) {
	const ddl = `CREATE TABLE sales (id uuid PRIMARY KEY, region text NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML+"views:\n  sales_summaries:\n    cursor_keys: [region]\n",
		tgfAddView("sales_summaries", parser.Column{Name: "region", Type: "text"}, parser.Column{Name: "total", Type: "integer"}))
	if got := tgfView(t, p, "sales_summaries").StructName; got != "SalesSummary" {
		t.Errorf("sales_summaries StructName = %q, want %q", got, "SalesSummary")
	}
	if _, ok := p.typeSpec("SalesSummary"); !ok {
		t.Error("no SalesSummary type generated for view sales_summaries")
	}
}

// TestGenerate_ViewColumnsInheritGlobalTypeOverrides pins that a view's
// columns resolve under the global overrides.types binding, as a table's do.
func TestGenerate_ViewColumnsInheritGlobalTypeOverrides(t *testing.T) {
	const ddl = `CREATE TABLE sales (id uuid PRIMARY KEY, amount numeric NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML+`overrides:
  types:
    numeric:
      type: decimal.Decimal
      import: github.com/shopspring/decimal
`, tgfAddView("sales_totals", parser.Column{Name: "region", Type: "text"}, parser.Column{Name: "total", Type: "numeric"}))
	var got string
	for _, c := range tgfView(t, p, "sales_totals").Columns {
		if c.Name == "total" {
			got = c.GoType
		}
	}
	if got != "decimal.Decimal" {
		t.Errorf("sales_totals.total GoType = %q, want %q from overrides.types.numeric", got, "decimal.Decimal")
	}
	field, ok := p.structField("SalesTotal", "Total")
	if !ok {
		t.Fatal("SalesTotal has no Total field")
	}
	if got := tgfExpr(field.Type); got != "decimal.Decimal" {
		t.Errorf("SalesTotal.Total type = %s, want decimal.Decimal", got)
	}
}

// TestGenerate_ExtrasShapeTheStruct pins how an extras entry maps onto the
// generated Go struct: the key is the struct name as written, each fields key
// a field name, and a field's description, type, import and tags become its doc
// comment, Go type, an import of the file and its struct tags.
func TestGenerate_ExtrasShapeTheStruct(t *testing.T) {
	const ddl = `CREATE TABLE sites (id uuid PRIMARY KEY, address jsonb NOT NULL);`
	added := make(map[string][]string)
	gen.ObserveImportDriftForTest(t, func(filename string, paths []string) {
		added[filepath.Base(filename)] = append(added[filepath.Base(filename)], paths...)
	})
	p := tgfGenerate(t, ddl, tgfYAML+`extras:
  PostalAddress:
    description: A postal address.
    fields:
      Gateway:
        description: Address of the site's gateway.
        type: decimal.Decimal
        import: github.com/shopspring/decimal
        tags:
          json: gateway
          db: gateway_addr
      Zip:
        type: string
      CIDRs:
        type: string
  CIDRs:
    fields:
      Block:
        type: string
`)
	gateway, gatewayFound := p.structField("PostalAddress", "Gateway")
	t.Run("key is the struct name", func(t *testing.T) {
		if _, ok := p.typeSpec("PostalAddress"); !ok {
			t.Error("no PostalAddress struct generated for extras.PostalAddress")
		}
	})
	t.Run("key is kept verbatim as the struct name", func(t *testing.T) {
		// CIDRs is an exported Go identifier the naming engine would respell.
		if _, ok := p.typeSpec("CIDRs"); !ok {
			t.Error("no CIDRs struct generated for extras.CIDRs")
		}
	})
	t.Run("fields key is the field name", func(t *testing.T) {
		for _, name := range []string{"Gateway", "Zip"} {
			if _, ok := p.structField("PostalAddress", name); !ok {
				t.Errorf("PostalAddress has no %s field", name)
			}
		}
	})
	t.Run("fields key is kept verbatim as the field name", func(t *testing.T) {
		// CIDRs is an exported Go identifier the naming engine would respell.
		if _, ok := p.structField("PostalAddress", "CIDRs"); !ok {
			ts, _ := p.typeSpec("PostalAddress")
			t.Errorf("PostalAddress has no CIDRs field for fields.CIDRs; struct is %s", tgfExpr(ts.Type))
		}
	})
	t.Run("field description is its doc comment", func(t *testing.T) {
		if !gatewayFound {
			t.Fatal("PostalAddress has no Gateway field")
		}
		if got := strings.TrimSpace(gateway.Doc.Text()); !strings.HasSuffix(got, "Address of the site's gateway.") {
			t.Errorf("PostalAddress.Gateway doc = %q, want it to carry %q", got, "Address of the site's gateway.")
		}
	})
	t.Run("field type is its Go type", func(t *testing.T) {
		if !gatewayFound {
			t.Fatal("PostalAddress has no Gateway field")
		}
		if got := tgfExpr(gateway.Type); got != "decimal.Decimal" {
			t.Errorf("PostalAddress.Gateway type = %s, want decimal.Decimal", got)
		}
	})
	t.Run("field import is added to the file", func(t *testing.T) {
		var found bool
		for name, f := range p.files {
			if _, ok := tgfTypeIn(f, "PostalAddress"); !ok {
				continue
			}
			for _, im := range f.Imports {
				if im.Path.Value == `"github.com/shopspring/decimal"` {
					found = true
				}
			}
			if !found {
				t.Errorf("%s declares PostalAddress but does not import github.com/shopspring/decimal", name)
			}
			// goimports would find the module on its own; the import must
			// come from fields.<F>.import, not from that repair.
			if slices.Contains(added[name], "github.com/shopspring/decimal") {
				t.Errorf("%s imports github.com/shopspring/decimal only through goimports' repair, not from fields.Gateway.import", name)
			}
			return
		}
		t.Error("no file declares PostalAddress")
	})
	t.Run("field tags keyed by tag name", func(t *testing.T) {
		if !gatewayFound {
			t.Fatal("PostalAddress has no Gateway field")
		}
		if gateway.Tag == nil {
			t.Fatal("PostalAddress.Gateway has no struct tags")
		}
		tag := reflect.StructTag(strings.Trim(gateway.Tag.Value, "`"))
		for key, want := range map[string]string{"json": "gateway", "db": "gateway_addr"} {
			if got := tag.Get(key); got != want {
				t.Errorf("PostalAddress.Gateway tag %s = %q, want %q (tags %s)", key, got, want, gateway.Tag.Value)
			}
		}
	})
}

// tgfTypeIn returns f's declaration of the named type.
func tgfTypeIn(f *ast.File, name string) (*ast.TypeSpec, bool) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			if ts := s.(*ast.TypeSpec); ts.Name.Name == name {
				return ts, true
			}
		}
	}
	return nil, false
}

// tgfEnumDDL declares one enum whose SQL name is plural, so the singular
// default and the override are both distinguishable from it.
const tgfEnumDDL = `CREATE TYPE statuses AS ENUM ('active', 'on_hold');
CREATE TABLE products (id uuid PRIMARY KEY, status statuses NOT NULL);`

const tgfEnumAPI = `api:
  enabled: true
  graphql:
    enabled: true
`

// TestGenerate_EnumTypeNameDefaultsToSingular pins that with
// enums.<e>.struct_name unset the Go type name is the singular PascalCase of
// the SQL enum name.
func TestGenerate_EnumTypeNameDefaultsToSingular(t *testing.T) {
	p := tgfGenerate(t, tgfEnumDDL, tgfYAML)
	if _, ok := p.typeSpec("Status"); !ok {
		t.Error("no Status type generated for enum statuses")
	}
	if _, ok := p.typeSpec("Statuses"); ok {
		t.Error("enum statuses generated as Statuses, want the singular Status")
	}
}

// TestGenerate_EnumStructNameOverride pins what enums.<e>.struct_name moves
// and what it leaves alone: the constants, All<Type>, Validate<Type>,
// <Type>Slice and the gqlgen models binding take the new name, while the SQL
// literals and the GraphQL wire identifiers stay derived from the values.
func TestGenerate_EnumStructNameOverride(t *testing.T) {
	p := tgfGenerate(t, tgfEnumDDL, tgfYAML+tgfEnumAPI+"enums:\n  statuses:\n    struct_name: MediaStatus\n")
	t.Run("constants", func(t *testing.T) {
		for _, tt := range []struct{ name, old string }{{"MediaStatusActive", "StatusActive"}, {"MediaStatusOnHold", "StatusOnHold"}} {
			if _, tok, ok := p.valueSpec(tt.name); !ok || tok != token.CONST {
				t.Errorf("no const %s generated", tt.name)
			}
			if _, _, ok := p.valueSpec(tt.old); ok {
				t.Errorf("const %s still generated under the derived name", tt.old)
			}
		}
	})
	t.Run("All var", func(t *testing.T) {
		if _, tok, ok := p.valueSpec("AllMediaStatus"); !ok || tok != token.VAR {
			t.Error("no var AllMediaStatus generated")
		}
		if _, _, ok := p.valueSpec("AllStatus"); ok {
			t.Error("var AllStatus still generated under the derived name")
		}
	})
	t.Run("Validate function", func(t *testing.T) {
		if _, ok := p.funcDecl("", "ValidateMediaStatus"); !ok {
			t.Error("no func ValidateMediaStatus generated")
		}
		if _, ok := p.funcDecl("", "ValidateStatus"); ok {
			t.Error("func ValidateStatus still generated under the derived name")
		}
	})
	t.Run("named slice type", func(t *testing.T) {
		if _, ok := p.typeSpec("MediaStatusSlice"); !ok {
			t.Error("no type MediaStatusSlice generated")
		}
		if _, ok := p.typeSpec("StatusSlice"); ok {
			t.Error("type StatusSlice still generated under the derived name")
		}
	})
	t.Run("gqlgen models binding", func(t *testing.T) {
		e := tgfUsedEnum(t, p, "statuses")
		if e.GraphQLName != "MediaStatus" || e.GoTypeName != "MediaStatus" || e.SliceGoTypeName != "MediaStatusSlice" {
			t.Errorf("statuses binding = %s → (%s, %s), want MediaStatus → (MediaStatus, MediaStatusSlice)", e.GraphQLName, e.GoTypeName, e.SliceGoTypeName)
		}
	})
	t.Run("SQL literals unaffected", func(t *testing.T) {
		for name, want := range map[string]string{"MediaStatusActive": `"active"`, "MediaStatusOnHold": `"on_hold"`} {
			vs, _, ok := p.valueSpec(name)
			if !ok || len(vs.Values) != 1 {
				t.Errorf("const %s not generated with a value", name)
				continue
			}
			if got := tgfExpr(vs.Values[0]); got != want {
				t.Errorf("const %s = %s, want the SQL literal %s", name, got, want)
			}
		}
	})
	t.Run("GraphQL wire identifiers unaffected", func(t *testing.T) {
		values := tgfUsedEnum(t, p, "statuses").Values
		got := make([]string, 0, len(values))
		for _, v := range values {
			got = append(got, v.GraphQLName)
		}
		if diff := cmp.Diff([]string{"ACTIVE", "ON_HOLD"}, got); diff != "" {
			t.Errorf("statuses GraphQL values mismatch (-want +got):\n%s", diff)
		}
	})
}

func tgfUsedEnum(t *testing.T, p tgfProject, sqlName string) gen.APIEnumContext {
	t.Helper()
	if p.result.API == nil {
		t.Fatal("Generate() built no API context")
	}
	for _, e := range p.result.API.UsedEnums {
		if e.SQLName == sqlName {
			return e
		}
	}
	t.Fatalf("API context projects no enum %s", sqlName)
	return gen.APIEnumContext{}
}

// TestGenerate_OverrideScanTargetIsResolvedType pins that sqlgen adds no
// conversion of its own for an overridden type: the field is the resolved Go
// type and every scan function passes the field's address straight to Scan.
func TestGenerate_OverrideScanTargetIsResolvedType(t *testing.T) {
	const ddl = `CREATE TABLE products (id uuid PRIMARY KEY, external_id varchar(27) NOT NULL);`
	p := tgfGenerate(t, ddl, tgfYAML+`overrides:
  types:
    varchar:
      type: ksuid.KSUID
      import: github.com/segmentio/ksuid
`)
	field, ok := p.structField("Product", "ExternalID")
	if !ok {
		t.Fatal("Product has no ExternalID field")
	}
	if got := tgfExpr(field.Type); got != "ksuid.KSUID" {
		t.Errorf("Product.ExternalID type = %s, want ksuid.KSUID", got)
	}
	var cases int
	for _, f := range p.sortedFiles() {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !strings.HasPrefix(fd.Name.Name, "scanProduct") {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				cc, ok := n.(*ast.CaseClause)
				if !ok || len(cc.List) != 1 || tgfExpr(cc.List[0]) != `"external_id"` {
					return true
				}
				cases++
				for _, stmt := range cc.Body {
					as, ok := stmt.(*ast.AssignStmt)
					if !ok || len(as.Rhs) != 1 {
						continue
					}
					u, ok := as.Rhs[0].(*ast.UnaryExpr)
					sel, isSel := ast.Expr(nil), false
					if ok && u.Op == token.AND {
						_, isSel = u.X.(*ast.SelectorExpr)
						sel = u.X
					}
					if !isSel || !strings.HasSuffix(tgfExpr(sel), ".ExternalID") {
						t.Errorf("%s scans external_id into %s, want the field's address &<row>.ExternalID", fd.Name.Name, tgfExpr(as.Rhs[0]))
					}
				}
				return true
			})
		}
	}
	if cases == 0 {
		t.Error("no scanProduct* function scans external_id")
	}
}

// tgfRuntimeDDL is the SQLite schema the runtime probe generates against.
// widgets carries table-level runtime values that differ from the global
// ones, gadgets turns strict_updates off, and opaque_rows retypes its text
// column to a struct the driver cannot fill.
const tgfRuntimeDDL = `CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE gadgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE opaque_rows (id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT NOT NULL);
`

const tgfRuntimeYAML = `version: v1
input:
  dialect: sqlite
  paths: [./schema.sql]
output:
  driver: stdlib
  dir: ./models
  package: models
generation:
  query_limit: 5
  batch_size: 4
  page_size: 6
tables:
  widgets:
    query_limit: 3
    batch_size: 2
    page_size: 4
  gadgets:
    strict_updates: false
  opaque_rows:
    overrides:
      types:
        text:
          type: Opaque
`

// tgfPgxDDL / tgfPgxYAML generate a pgx package, the driver whose UpdateMany
// sends one pipelined batch per sub-batch, so the split is observable.
const tgfPgxDDL = `CREATE TABLE widgets (id bigint PRIMARY KEY, name text NOT NULL);`

const tgfPgxYAML = `version: v1
input:
  dialect: postgres
  paths: [./pg_schema.sql]
output:
  driver: pgx
  dir: ./pgmodels
  package: pgmodels
generation:
  batch_size: 4
tables:
  widgets:
    batch_size: 2
`

// tgfOpaqueGo is the same-package type opaque_rows.label is retyped to: it
// implements no sql.Scanner, so no driver can fill it from TEXT.
const tgfOpaqueGo = `package models

// Opaque has no Scan method.
type Opaque struct{ V int }
`

// tgfRuntimeTests drives the generated clients. Each test is one runtime
// clause of the generation fields; TestGenerate_RuntimeGenerationFields maps
// them to its subtests.
const tgfRuntimeTests = `package tests

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "modernc.org/sqlite"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/mock"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"

	"example.com/app/models"
	"example.com/app/pgmodels"
)

// recorder passes every statement through to the real database and keeps
// its SQL and arguments.
type recorder struct {
	database.Querier
	mu    sync.Mutex
	stmts []stmt
}

type stmt struct {
	sql  string
	args []any
}

func (r *recorder) record(q string, args []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stmts = append(r.stmts, stmt{q, args})
}

func (r *recorder) Exec(ctx context.Context, q string, args ...any) (database.Result, error) {
	r.record(q, args)
	return r.Querier.Exec(ctx, q, args...)
}

func (r *recorder) Query(ctx context.Context, q string, args ...any) (database.Rows, error) {
	r.record(q, args)
	return r.Querier.Query(ctx, q, args...)
}

func (r *recorder) QueryRow(ctx context.Context, q string, args ...any) database.Row {
	r.record(q, args)
	return r.Querier.QueryRow(ctx, q, args...)
}

func (r *recorder) prefixed(prefix string) []stmt {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []stmt
	for _, s := range r.stmts {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s.sql)), prefix) {
			out = append(out, s)
		}
	}
	return out
}

func open(t *testing.T, widgets int) (*sql.DB, *recorder, *models.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ddl, err := os.ReadFile("../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	for range widgets {
		if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('w')"); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recorder{Querier: dbstdlib.New(db)}
	return db, rec, models.New(rec)
}

func TestGetManyNilLimitAppliesQueryLimit(t *testing.T) {
	_, _, c := open(t, 5)
	got, err := c.Widgets().GetMany(context.Background(), &models.GetWidgetsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("GetMany(Limit: nil) returned %d rows of 5, want widgets' query_limit 3", len(got))
	}
}

func TestGetManyLimitN(t *testing.T) {
	_, _, c := open(t, 5)
	for _, n := range []int{2, 4} {
		got, err := c.Widgets().GetMany(context.Background(), &models.GetWidgetsInput{Limit: new(n)})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != n {
			t.Errorf("GetMany(Limit: new(%d)) returned %d rows of 5, want %d", n, len(got), n)
		}
	}
}

func TestGetManyOffsetZeroOmitsClause(t *testing.T) {
	for _, tt := range []struct {
		name       string
		offset     int
		wantOffset bool
	}{{"zero", 0, false}, {"one", 1, true}} {
		t.Run(tt.name, func(t *testing.T) {
			_, rec, c := open(t, 5)
			if _, err := c.Widgets().GetMany(context.Background(), &models.GetWidgetsInput{Offset: new(tt.offset)}); err != nil {
				t.Fatal(err)
			}
			selects := rec.prefixed("SELECT")
			if len(selects) != 1 {
				t.Fatalf("GetMany issued %d SELECTs, want 1", len(selects))
			}
			if got := strings.Contains(strings.ToUpper(selects[0].sql), "OFFSET"); got != tt.wantOffset {
				t.Errorf("GetMany(Offset: new(%d)) SQL %q has OFFSET = %v, want %v", tt.offset, selects[0].sql, got, tt.wantOffset)
			}
		})
	}
}

func TestCreateManyBatches(t *testing.T) {
	_, rec, c := open(t, 0)
	inputs := make([]*models.CreateWidgetInput, 5)
	for i := range inputs {
		inputs[i] = &models.CreateWidgetInput{Name: "w"}
	}
	if _, err := c.Widgets().CreateMany(context.Background(), inputs); err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, s := range rec.prefixed("INSERT") {
		sizes = append(sizes, len(s.args))
	}
	if !slices.Equal(sizes, []int{2, 2, 1}) {
		t.Errorf("CreateMany of 5 sent INSERTs binding %v rows, want [2 2 1] (widgets' batch_size 2)", sizes)
	}
}

// batcher is a pgx connection that records each pipelined batch's size and
// reports every queued UPDATE as matching one row.
type batcher struct {
	*mock.Querier
	sizes []int
}

func (b *batcher) SendBatch(_ context.Context, batch *pgx.Batch) pgx.BatchResults {
	b.sizes = append(b.sizes, batch.Len())
	return batchResults{}
}

type batchResults struct{}

func (batchResults) Exec() (pgconn.CommandTag, error) { return pgconn.NewCommandTag("UPDATE 1"), nil }
func (batchResults) Query() (pgx.Rows, error)         { return nil, errors.New("unexpected Query") }
func (batchResults) QueryRow() pgx.Row                { return nil }
func (batchResults) Close() error                     { return nil }

func TestUpdateManyBatches(t *testing.T) {
	b := &batcher{Querier: mock.New()}
	c := pgmodels.New(b)
	items := make([]pgmodels.UpdateWidgetItem, 5)
	for i := range items {
		items[i] = pgmodels.UpdateWidgetItem{ID: int64(i + 1), Input: &pgmodels.UpdateWidgetInput{Name: omittable.Set("w")}}
	}
	if _, err := c.Widgets().UpdateMany(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(b.sizes, []int{2, 2, 1}) {
		t.Errorf("UpdateMany of 5 sent batches of %v, want [2 2 1] (widgets' batch_size 2)", b.sizes)
	}
}

func TestConnectionDefaultPageSize(t *testing.T) {
	_, _, c := open(t, 6)
	conn, err := c.Widgets().Connection(context.Background(), models.ConnectionInput[models.WidgetFilter]{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.Edges) != 4 || !conn.PageInfo.HasNextPage {
		t.Errorf("Connection with no first/last returned %d edges (has next %v) of 6, want widgets' page_size 4 and a next page", len(conn.Edges), conn.PageInfo.HasNextPage)
	}
}

func TestStrictUpdateNotFound(t *testing.T) {
	_, _, c := open(t, 0)
	_, err := c.Widgets().Update(context.Background(), 999, &models.UpdateWidgetInput{Name: omittable.Set("x")})
	if !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Update on a missing PK with strict_updates on = %v, want ErrNotFound", err)
	}
}

func TestNonStrictUpdateNil(t *testing.T) {
	_, _, c := open(t, 0)
	got, err := c.Gadgets().Update(context.Background(), 999, &models.UpdateGadgetInput{Name: omittable.Set("x")})
	if err != nil {
		t.Errorf("Update on a missing PK with strict_updates: false = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Update on a missing PK with strict_updates: false returned entity %+v, want nil", got)
	}
}

func TestUnfillableTypeScanError(t *testing.T) {
	db, _, c := open(t, 0)
	res, err := db.Exec("INSERT INTO opaque_rows (label) VALUES ('x')")
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.OpaqueRows().Get(context.Background(), id)
	if err == nil || errors.Is(err, models.ErrNotFound) || !strings.Contains(err.Error(), "name \"label\"") {
		t.Errorf("Get of a row whose label the driver cannot fill = %v, want a scan error naming column label", err)
	}
}
`

// TestGenerate_RuntimeGenerationFields generates a SQLite client and a pgx
// client from configs whose table-level runtime values differ from the
// global ones and from the defaults, then runs tgfRuntimeTests against them:
// a real in-memory SQLite database, and for UpdateMany's pipelined batches a
// pgx connection that records each batch. The opaque_rows override names a
// type no driver can fill, which must generate cleanly and fail only at scan
// time.
func TestGenerate_RuntimeGenerationFields(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the generated package; skipped in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(repo, "go.sum")) //nolint:gosec // the repo's own go.sum
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files := map[string]string{
		"schema.sql":    tgfRuntimeDDL,
		"sqlgen.yml":    tgfRuntimeYAML,
		"pg_schema.sql": tgfPgxDDL,
		"pg.yml":        tgfPgxYAML,
		"go.mod":        "module example.com/app\n" + goDirective() + "\nrequire github.com/teandresmith/sqlgen v0.0.0\n\nreplace github.com/teandresmith/sqlgen => " + filepath.ToSlash(repo) + "\n",
		"go.sum":        string(goSum),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)
	for _, run := range []struct {
		dialect  config.Dialect
		ddl, yml string
	}{
		{config.DialectSQLite, tgfRuntimeDDL, "sqlgen.yml"},
		{config.DialectPostgres, tgfPgxDDL, "pg.yml"},
	} {
		schema := parseDDLForTest(t, run.dialect, run.ddl)
		cfg, err := config.LoadConfig(filepath.Join(root, run.yml))
		if err != nil {
			t.Fatalf("LoadConfig(%s) error: %v", run.yml, err)
		}
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Fatalf("ValidatePreParse(%s) error: %v", run.yml, err)
		}
		if _, err := gen.Generate(schema, cfg, "test"); err != nil {
			t.Fatalf("Generate(%s) error = %v; an override to a type the driver cannot fill must not fail generation", run.yml, err)
		}
	}
	for name, body := range map[string]string{
		filepath.Join("models", "opaque.go"):      tgfOpaqueGo,
		filepath.Join("tests", "runtime_test.go"): tgfRuntimeTests,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	cmd := exec.Command(goBin, "test", "-json", "-count=1", "./tests/") //nolint:gosec // the go binary from PATH, run on a test-written temp module
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, runErr := cmd.CombinedOutput()
	results := tgfParseTestJSON(out)
	if len(results) == 0 {
		t.Fatalf("go test on the generated package ran no tests (%v):\n%s", runErr, out)
	}

	for _, tt := range []struct {
		name  string
		inner string
	}{
		{name: "get many with nil limit applies the table query_limit", inner: "TestGetManyNilLimitAppliesQueryLimit"},
		{name: "get many with limit N returns N rows", inner: "TestGetManyLimitN"},
		{name: "offset zero omits the OFFSET clause", inner: "TestGetManyOffsetZeroOmitsClause"},
		{name: "create many splits into batch_size sub-batches", inner: "TestCreateManyBatches"},
		{name: "update many splits into batch_size sub-batches", inner: "TestUpdateManyBatches"},
		{name: "connection without first or last uses the table page_size", inner: "TestConnectionDefaultPageSize"},
		{name: "strict update of a missing key returns ErrNotFound", inner: "TestStrictUpdateNotFound"},
		{name: "non-strict update of a missing key returns nil", inner: "TestNonStrictUpdateNil"},
		{name: "unfillable override type is a scan error naming the column", inner: "TestUnfillableTypeScanError"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := results[tt.inner]
			if !ok {
				t.Fatalf("%s did not run (%v):\n%s", tt.inner, runErr, out)
			}
			if r.action != "pass" {
				t.Errorf("%s %s:\n%s", tt.inner, r.action, r.output)
			}
		})
	}
}

type tgfTestResult struct {
	action string
	output string
}

// tgfParseTestJSON folds `go test -json` output into one result per
// top-level test.
func tgfParseTestJSON(out []byte) map[string]tgfTestResult {
	results := make(map[string]tgfTestResult)
	for line := range strings.SplitSeq(string(out), "\n") {
		var ev struct {
			Action string
			Test   string
			Output string
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		r := results[ev.Test]
		switch ev.Action {
		case "output":
			r.output += ev.Output
		case "pass", "fail", "skip":
			r.action = ev.Action
		}
		results[ev.Test] = r
	}
	return results
}
