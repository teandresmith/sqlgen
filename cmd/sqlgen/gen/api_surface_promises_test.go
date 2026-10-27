package gen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// PRD §26.5.1 – §26.5.3 promises about the generated GraphQL surface, pinned
// against the real generation pipeline (gen.Generate over parsed DDL and a
// loaded sqlgen.yml) wherever the promise is about what a consumer's config
// produces.

// trsGenerate parses ddl for dialect, detects its FK relationships the way the
// CLI does, loads cfgBody through the production config loader and runs the
// whole generator in a fresh module directory. It returns the run's result;
// read files from it with pprReadGenerated.
func trsGenerate(t *testing.T, dialect config.Dialect, ddl, cfgBody string) *gen.GenerateResult {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	t.Chdir(root)
	schema := parseDDLForTest(t, dialect, ddl)
	detectRelationships(schema)
	res, err := gen.Generate(schema, pprLoadConfig(t, cfgBody), "test")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	return res
}

// trsRootFields returns the field names declared inside every `extend type
// <typ> {` block of a rendered schema file, in order.
func trsRootFields(schema, typ string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(schema, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case l == "extend type "+typ+" {":
			in = true
		case in && l == "}":
			in = false
		case in && l != "" && !strings.HasPrefix(l, "\"") && !strings.HasPrefix(l, "#"):
			name, _, _ := strings.Cut(l, "(")
			name, _, _ = strings.Cut(name, ":")
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

// trsSoftDeleteTop is the top-level config block that names `deleted_at` as
// the soft-delete column.
const trsSoftDeleteTop = `generation:
  soft_delete_columns:
    - name: deleted_at
      type: timestamp
`

// TestAPIViewSurface_ExcludedMethodsGetNoRootField pins PRD §26.5.1: "The
// `Excluded` table below applies to views unchanged." A view gets exactly its
// three read queries and no root field or resolver for any excluded client
// method.
func TestAPIViewSurface_ExcludedMethodsGetNoRootField(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	apiCtx := buildAPIWithViews(t, in)
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"
	v := findAPIEntity(t, apiCtx, "ProductSummary")
	if v == nil {
		t.Fatal("view ProductSummary is absent from the API context")
	}
	schema := renderAPITableSchema(t, *v)

	if diff := slices.Compare(trsRootFields(schema, "Query"), []string{"productSummary", "productSummaries", "productSummaryList"}); diff != 0 {
		t.Errorf("view Query fields = %v, want exactly the by-pk, connection and list queries", trsRootFields(schema, "Query"))
	}
	if got := trsRootFields(schema, "Mutation"); len(got) != 0 {
		t.Errorf("view Mutation fields = %v, want none", got)
	}

	resolvers := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)
	methodRE := regexp.MustCompile(`func \((?:q \*Q|m \*M|r \*queryResolver|r \*mutationResolver)\) (\w*ProductSummar\w*)\(`)
	for _, src := range []struct {
		name string
		body string
	}{
		{name: "sqlgenresolver Q/M methods", body: resolvers},
		{name: "graph seeds", body: seeds},
	} {
		var got []string
		for _, m := range methodRE.FindAllStringSubmatch(src.body, -1) {
			got = append(got, m[1])
		}
		slices.Sort(got)
		want := []string{"ProductSummaries", "ProductSummary", "ProductSummaryList"}
		if !slices.Equal(got, want) {
			t.Errorf("%s for the view = %v, want exactly %v (Exists, Count, GetMany, Find, Increment, Decrement, UpsertMany get none)", src.name, got, want)
		}
	}
}

// TestAPIDeleteNaming_SoftDeleteTableHardOnly pins the "hard delete only" row
// of PRD §26.5.1's naming rules on a table that HAS a soft-delete column:
// keeping `hard_delete` and masking `soft_delete` gives the bare
// `delete<T>(id): Boolean!`, backed by HardDelete, and neither of the pair.
func TestAPIDeleteNaming_SoftDeleteTableHardOnly(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres,
		`CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL, deleted_at timestamptz);`,
		pprGraphQLConfig("", trsSoftDeleteTop+`tables:
  products:
    api:
      operations:
        soft_delete: false
`))
	schema := pprReadGenerated(t, res, "models/graph/product_gen.graphqls")
	if !pprHasLine(schema, "deleteProduct(id: UUID!): Boolean!") {
		t.Errorf("schema has no `deleteProduct(id: UUID!): Boolean!`:\n%s", schema)
	}
	for _, unwanted := range []string{"hardDeleteProduct(", "softDeleteProduct("} {
		if strings.Contains(schema, unwanted) {
			t.Errorf("hard-only surface still declares %s", unwanted)
		}
	}
	resolvers := pprReadGenerated(t, res, "models/graph/sqlgenresolver/resolvers_gen.go")
	del := extractFunc(t, resolvers, "DeleteProduct")
	if !strings.Contains(del, ".HardDelete(") || strings.Contains(del, ".SoftDelete(") {
		t.Errorf("DeleteProduct must call HardDelete only:\n%s", del)
	}
}

// TestAPIDeleteVariants_TenantedSoftDeleteTable pins the tenanted column of the
// hard-soft-delete-pair and restore rows (PRD §26.5.1): tenancy does not change
// the delete-variant surface of a table with a soft-delete column.
func TestAPIDeleteVariants_TenantedSoftDeleteTable(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres,
		`CREATE TABLE notes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL, body text NOT NULL, deleted_at timestamptz);`,
		pprGraphQLConfig("", trsSoftDeleteTop+`tenancy:
  enabled: true
  column: workspace_id
  required: true
  type:
    type: uuid.UUID
    import: uuid
`))
	// The fixture is only meaningful if the table really is tenanted.
	if client := pprReadGenerated(t, res, "models/models_gen.go"); !strings.Contains(client, "func (c *noteClient) tenantValue(") {
		t.Fatal("notes is not tenanted in the generated client; the fixture tests nothing")
	}
	schema := pprReadGenerated(t, res, "models/graph/note_gen.graphqls")
	resolvers := pprReadGenerated(t, res, "models/graph/sqlgenresolver/resolvers_gen.go")

	tests := []struct {
		name       string
		field      string
		method     string
		clientCall string
	}{
		{name: "hard delete returns Boolean", field: "hardDeleteNote(id: UUID!): Boolean!", method: "HardDeleteNote", clientCall: ".HardDelete("},
		{name: "soft delete returns the nullable row", field: "softDeleteNote(id: UUID!): Note", method: "SoftDeleteNote", clientCall: ".SoftDelete("},
		{name: "restore returns the nullable row", field: "restoreNote(id: UUID!): Note", method: "RestoreNote", clientCall: ".Restore("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !pprHasLine(schema, tt.field) {
				t.Errorf("schema has no line %q:\n%s", tt.field, schema)
			}
			if body := extractFunc(t, resolvers, tt.method); !strings.Contains(body, tt.clientCall) {
				t.Errorf("%s does not call %s:\n%s", tt.method, tt.clientCall, body)
			}
		})
	}
	t.Run("no bare delete", func(t *testing.T) {
		if slices.Contains(trsRootFields(schema, "Mutation"), "deleteNote") {
			t.Errorf("both variants on, yet a bare deleteNote is declared:\n%s", schema)
		}
	})
}

// TestAPIExcluded_NoBatchUpsertMutation pins PRD §26.5.1's `UpsertMany` row:
// the Go client has the batch form, and no GraphQL mutation reaches it.
func TestAPIExcluded_NoBatchUpsertMutation(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres,
		`CREATE TABLE products (id uuid PRIMARY KEY, sku text NOT NULL UNIQUE, name text NOT NULL);`,
		pprGraphQLConfig("", ""))
	client := pprReadGenerated(t, res, "models/models_gen.go")
	if !strings.Contains(client, "func (c *productClient) UpsertMany(") {
		t.Fatal("the Go client has no UpsertMany; the fixture tests nothing")
	}
	schema := pprReadGenerated(t, res, "models/graph/product_gen.graphqls")
	mutations := trsRootFields(schema, "Mutation")
	if !slices.Contains(mutations, "upsertProduct") {
		t.Fatalf("Mutation fields = %v, want the single-row upsertProduct", mutations)
	}
	for _, f := range mutations {
		if strings.HasPrefix(f, "upsert") && f != "upsertProduct" && f != "upsertProductWithRelated" {
			t.Errorf("Mutation declares %q, a batch-upsert root field", f)
		}
	}
	if resolvers := pprReadGenerated(t, res, "models/graph/sqlgenresolver/resolvers_gen.go"); strings.Contains(resolvers, ".UpsertMany(") {
		t.Error("a generated resolver calls UpsertMany")
	}
}

// TestAPIExcluded_NoTransactionPrimitive pins PRD §26.5.1's "Tx primitives,
// savepoints … not exposed": no root field across the whole generated schema
// names a transaction, savepoint, begin, commit or rollback.
func TestAPIExcluded_NoTransactionPrimitive(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres, `CREATE TABLE users (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE, deleted_at timestamptz);
CREATE TABLE orders (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id), total integer NOT NULL);`,
		pprGraphQLConfig("", trsSoftDeleteTop+"  nested_mutations:\n    enabled: true\n"))
	txRE := regexp.MustCompile(`(?i)(^tx|Tx$|transaction|savepoint|^begin|commit|rollback)`)
	var roots []string
	for _, f := range res.Files {
		if !strings.HasSuffix(f, ".graphqls") {
			continue
		}
		b, err := os.ReadFile(f) //nolint:gosec // a file this test run just generated under its temp dir
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		roots = append(roots, trsRootFields(string(b), "Query")...)
		roots = append(roots, trsRootFields(string(b), "Mutation")...)
	}
	if !slices.Contains(roots, "updateUserWithRelated") {
		t.Fatalf("root fields = %v, want the nested mutations the fixture enables (the transactional surface)", roots)
	}
	for _, f := range roots {
		if txRE.MatchString(f) {
			t.Errorf("root field %q exposes a transaction primitive", f)
		}
	}
}

// TestAPIMask_MaskedReadsKeepClientMethods pins PRD §26.5.1 "Reads are
// maskable too … the client method stays available to hand-written
// resolvers".
func TestAPIMask_MaskedReadsKeepClientMethods(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres,
		`CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);`,
		pprGraphQLConfig("", `tables:
  products:
    api:
      operations:
        get: false
        paginate: false
        connection: false
`))
	schema := pprReadGenerated(t, res, "models/graph/product_gen.graphqls")
	if got := trsRootFields(schema, "Query"); len(got) != 0 {
		t.Fatalf("masked reads left Query fields %v", got)
	}
	client := pprReadGenerated(t, res, "models/models_gen.go")
	tests := []struct {
		name   string
		method string
	}{
		{name: "get keeps Get", method: "Get"},
		{name: "paginate keeps Paginate", method: "Paginate"},
		{name: "connection keeps Connection", method: "Connection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(client, "func (c *productClient) "+tt.method+"(") {
				t.Errorf("the Go client lost %s after the API mask hid its query", tt.method)
			}
		})
	}
}

// TestAPIMask_SetMaskTurnsGetManyOff pins PRD §26.5.1 "Whenever a mask is set,
// the masked set's `GetMany` … [is] off", whatever the mask says and wherever
// it is set — and that the Go client keeps GetMany regardless.
func TestAPIMask_SetMaskTurnsGetManyOff(t *testing.T) {
	tests := []struct {
		name        string
		global      *config.Operations
		perTable    *config.Operations
		wantGetMany bool
	}{
		{name: "no mask leaves GetMany on", wantGetMany: true},
		{name: "global all preset", global: &config.Operations{Preset: config.PresetAll}},
		{name: "global read_only preset", global: &config.Operations{Preset: config.PresetReadOnly}},
		{name: "per-table single key", perTable: &config.Operations{Create: new(false)}},
		{name: "per-table key naming a read", perTable: &config.Operations{Paginate: new(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, resolverTestSchema())
			in.Config.API.Operations = tt.global
			if tt.perTable != nil {
				in.Config.Tables["products"] = config.TableConfig{API: &config.TableAPIConfig{Operations: tt.perTable}}
			}
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			if !tables[0].Operations.GetMany {
				t.Fatal("the Go client lost GetMany; the API mask must never narrow the client")
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			if got := apiTableByName(t, apiCtx, "Product").Operations.GetMany; got != tt.wantGetMany {
				t.Errorf("API Operations.GetMany = %v, want %v", got, tt.wantGetMany)
			}
		})
	}
}

// TestAPINested_SuppressedFlatInputFamilyNotEmitted pins PRD §26.5.1 "a family
// whose flat input is suppressed (the table has no API-writable column) is not
// emitted". The control case keeps the column writable to show the fixture
// does emit the family otherwise.
func TestAPINested_SuppressedFlatInputFamilyNotEmitted(t *testing.T) {
	const ddl = `CREATE TABLE authors (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text);
CREATE TABLE books (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), author_id uuid REFERENCES authors(id), title text NOT NULL);`
	const nested = "generation:\n  nested_mutations:\n    enabled: true\n"
	tests := []struct {
		name     string
		tables   string
		wantEmit bool
	}{
		{name: "writable column emits the families", wantEmit: true},
		{name: "no API-writable column emits none", tables: "tables:\n  authors:\n    column_map:\n      name:\n        access: read_only\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := trsGenerate(t, config.DialectPostgres, ddl, pprGraphQLConfig("", nested+tt.tables))
			schema := pprReadGenerated(t, res, "models/graph/author_gen.graphqls")
			if !strings.Contains(schema, "books: [Book!]!") {
				t.Fatalf("Author has no books edge; the fixture has no eligible relationship:\n%s", schema)
			}
			mutations := trsRootFields(schema, "Mutation")
			for _, name := range []string{"createAuthorWithRelated", "updateAuthorWithRelated", "upsertAuthorWithRelated"} {
				if got := slices.Contains(mutations, name); got != tt.wantEmit {
					t.Errorf("Mutation declares %s = %v, want %v (fields: %v)", name, got, tt.wantEmit, mutations)
				}
			}
			for _, input := range []string{"input CreateAuthorWithRelatedInput", "input UpdateAuthorWithRelatedInput", "input UpsertAuthorWithRelatedInput"} {
				if got := strings.Contains(schema, input); got != tt.wantEmit {
					t.Errorf("schema declares %q = %v, want %v", input, got, tt.wantEmit)
				}
			}
		})
	}
}

// TestAPIObjectType_DropsRelationshipIntoUnreadableTarget pins PRD §26.5.3:
// "The object type drops its relationship field into such a target by the same
// exposure test" — here the target whose every column §32.2 drops.
func TestAPIObjectType_DropsRelationshipIntoUnreadableTarget(t *testing.T) {
	apiCtx := relFilterAPIContext(t, func(cfg *config.RootConfig) {
		cfg.Tables = map[string]config.TableConfig{
			"notes": {ColumnMap: map[string]config.ColumnOverride{
				"id":         {Access: config.AccessHidden},
				"user_id":    {Access: config.AccessHidden},
				"body":       {Access: config.AccessHidden},
				"deleted_at": {Access: config.AccessHidden},
			}},
		}
	})
	users := apiTableByName(t, apiCtx, "User")
	rels := make([]string, 0, len(users.Relationships))
	for _, r := range users.Relationships {
		rels = append(rels, r.SQLName)
	}
	if slices.Contains(rels, "notes") {
		t.Errorf("User relationships = %v, want the edge into the unreadable notes table dropped", rels)
	}
	if !slices.Contains(rels, "orders") {
		t.Errorf("User relationships = %v, want the edge into the readable orders table kept", rels)
	}
	if obj := trsTypeBlock(renderAPITableSchema(t, users), "type User {"); strings.Contains(obj, "notes:") {
		t.Errorf("type User still declares a notes field:\n%s", obj)
	}
}

// trsTypeBlock returns the `header … }` block of a rendered schema.
func trsTypeBlock(schema, header string) string {
	start := strings.Index(schema, header)
	if start < 0 {
		return ""
	}
	end := strings.Index(schema[start:], "\n}")
	if end < 0 {
		return schema[start:]
	}
	return schema[start : start+end+2]
}

// TestAPIFilter_NoProjectionColumnKeepsGoFilterMember pins PRD §26.5.3's
// fourth non-filterable class: a comparator with no sound GraphQL projection
// "keeps its member on the **Go** `<T>Filter` struct; only the GraphQL face is
// dropped".
func TestAPIFilter_NoProjectionColumnKeepsGoFilterMember(t *testing.T) {
	tests := []struct {
		name   string
		column parser.Column
		setup  func(t *testing.T, in *gen.GenerateInput)
	}{
		{
			name:   "enum-like Go type that is not a schema enum",
			column: parser.Column{Name: "shipping_address", Type: "text"},
			setup: func(t *testing.T, in *gen.GenerateInput) {
				in.Config.Output.Package = "models"
				in.Config.Tables["products"] = config.TableConfig{
					ColumnMap: map[string]config.ColumnOverride{"shipping_address": {Type: "Address"}},
				}
				in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
					"Address": {GoType: "Address", Marshaling: config.ScalarMarshalingMethod},
				}
				in.Resolver = gotype.NewResolver(config.DialectPostgres, true, in.Config.Overrides.Types)
			},
		},
		{
			name:   "array element outside the measured set",
			column: parser.Column{Name: "seen_at", Type: "timestamptz[]"},
			setup:  func(t *testing.T, in *gen.GenerateInput) { configureNullVariantOverrides(t, in) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, &parser.Schema{Tables: []parser.Table{{
				Name:    "products",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, tt.column},
			}}})
			tt.setup(t, in)
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			if f := fieldBySQLName(t, apiTableByName(t, apiCtx, "Product"), tt.column.Name); f.Filterable {
				t.Fatalf("column %s is still on the GraphQL filter; the fixture is not in the no-projection class", tt.column.Name)
			}
			i := slices.IndexFunc(tables[0].FilterFields, func(f gen.FilterFieldContext) bool { return f.ColumnName == tt.column.Name })
			if i < 0 {
				t.Fatalf("Go ProductFilter has no member for %s", tt.column.Name)
			}
			if ff := tables[0].FilterFields[i]; !ff.Filterable || ff.ComparatorType == "" {
				t.Errorf("Go ProductFilter member for %s = %+v, want it kept (Filterable with a comparator type)", tt.column.Name, ff)
			}
		})
	}
}

// TestAPILint_RunsOnTheBuiltContext pins PRD §26.5.3 "The lint runs on the
// built context, before any template is rendered": building the API context
// is where a context that would render one half of a filter member is refused,
// so no template ever sees it.
func TestAPILint_RunsOnTheBuiltContext(t *testing.T) {
	build := func(t *testing.T, perturb func([]gen.TableContext)) error {
		t.Helper()
		in := apiTestInput(t, &parser.Schema{Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "name", Type: "text"}}},
			{Name: "orders", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			}},
		}})
		detectRelationships(in.Schema)
		tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
		if err != nil {
			t.Fatalf("BuildEntityContextsFromSchema: %v", err)
		}
		perturb(tables)
		_, err = gen.BuildAPIContext(tables, views, nil, nil, in.Config)
		return err
	}
	t.Run("a sound context builds", func(t *testing.T) {
		if err := build(t, func([]gen.TableContext) {}); err != nil {
			t.Fatalf("BuildAPIContext() error on the unperturbed fixture: %v", err)
		}
	})
	t.Run("a context that would render half a filter member is refused at build", func(t *testing.T) {
		err := build(t, func(tables []gen.TableContext) {
			for i := range tables {
				if tables[i].TableName == "users" {
					if len(tables[i].RelationshipFilters) == 0 {
						t.Fatal("users carries no relationship filter; the fixture has nothing to perturb")
					}
					tables[i].RelationshipFilters[0].FieldName = ""
				}
			}
		})
		if err == nil {
			t.Fatal("BuildAPIContext() accepted a relationship member with no Go half")
		}
		if !strings.Contains(err.Error(), "renders only one half") {
			t.Errorf("BuildAPIContext() error = %v, want the filter-completeness lint's half-rendered message", err)
		}
	})
	t.Run("a failed context build writes no file", func(t *testing.T) {
		// The API context build — the step the lint runs in — happens before
		// the first write: a run it refuses leaves the tree empty.
		mod := t.TempDir()
		if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/shop\n\ngo 1.27\n"), 0o600); err != nil {
			t.Fatalf("writing go.mod: %v", err)
		}
		t.Chdir(mod)
		root := t.TempDir()
		schema := parseDDLForTest(t, config.DialectPostgres, `CREATE TABLE products (id uuid PRIMARY KEY, stock integer NOT NULL, stock_inc integer NOT NULL);`)
		if _, err := gen.GenerateInto(schema, pprLoadConfig(t, pprGraphQLConfig("", "")), "test", root); err == nil {
			t.Fatal("GenerateInto() accepted a table whose API context cannot be built")
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
		if len(entries) != 0 {
			t.Errorf("a refused run wrote %d entries into the output root", len(entries))
		}
	})
}

// TestValidateGeneration_BuildsTheLintedAPIContext pins PRD §26.5.3 "so it
// covers `sqlgen validate` as well as `sqlgen generate`": the validate entry
// point builds the same API context the lint runs on, so a table whose API
// context is refused fails validate too. (No validated config can reach the
// completeness lints themselves — they are invariant guards — so the shared
// build step is what is observable.)
func TestValidateGeneration_BuildsTheLintedAPIContext(t *testing.T) {
	tests := []struct {
		name    string
		ddl     string
		wantErr string
	}{
		{name: "a buildable API context validates", ddl: `CREATE TABLE products (id uuid PRIMARY KEY, stock integer NOT NULL);`},
		{name: "a refused API context fails validate", ddl: `CREATE TABLE products (id uuid PRIMARY KEY, stock integer NOT NULL, stock_inc integer NOT NULL);`, wantErr: "stock_inc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := parseDDLForTest(t, config.DialectPostgres, tt.ddl)
			_, err := gen.ValidateGeneration(schema, pprLoadConfig(t, pprGraphQLConfig("", "")))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateGeneration() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateGeneration() error = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
}

// TestGenerate_EmitsNoTestFiles pins PRD §26.5.2 "sqlgen emits no test files
// into the consumer's packages", over a config that turns on every surface.
func TestGenerate_EmitsNoTestFiles(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres, `CREATE TABLE users (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE, deleted_at timestamptz);
CREATE TABLE notes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL, user_id uuid REFERENCES users(id), body text NOT NULL);`,
		pprGraphQLConfig("", trsSoftDeleteTop+`  nested_mutations:
    enabled: true
  manifest:
    enabled: true
cache:
  enabled: true
events:
  enabled: true
tenancy:
  enabled: true
  column: workspace_id
  type:
    type: uuid.UUID
    import: uuid
`))
	if len(res.Files) == 0 {
		t.Fatal("Generate() wrote no files")
	}
	var sawGraph bool
	for _, f := range res.Files {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("generate wrote a test file: %s", f)
		}
		sawGraph = sawGraph || strings.Contains(filepath.ToSlash(f), "/graph/sqlgenresolver/")
	}
	if !sawGraph {
		t.Errorf("the run emitted no sqlgenresolver package; files:\n%s", strings.Join(res.Files, "\n"))
	}
}

// TestAPINested_MySQLConflictTargetShapesOnlyTheUpdateList pins PRD §26.5.1's
// MySQL note: "`ON DUPLICATE KEY UPDATE` takes the update branch on any unique
// violation … and the target shapes only the update half's column list." The
// GraphQL argument reaches the client unchanged, the client turns it into the
// excluded-from-update set, and the statement the runtime builds names no
// constraint — so which unique key collided is the engine's choice, not the
// target's. (The engine half — that MySQL then takes the update branch on any
// unique key — is database behavior and is not exercised here.)
func TestAPINested_MySQLConflictTargetShapesOnlyTheUpdateList(t *testing.T) {
	cfgBody := strings.NewReplacer("dialect: postgres", "dialect: mysql", "driver: pgx", "driver: mysql").
		Replace(pprGraphQLConfig("", "generation:\n  nested_mutations:\n    enabled: true\n"))
	res := trsGenerate(t, config.DialectMySQL,
		"CREATE TABLE authors (id BIGINT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE, name VARCHAR(255) NOT NULL);\n"+
			"CREATE TABLE books (id BIGINT AUTO_INCREMENT PRIMARY KEY, author_id BIGINT REFERENCES authors(id), title VARCHAR(255) NOT NULL);",
		cfgBody)

	schema := pprReadGenerated(t, res, "models/graph/author_gen.graphqls")
	if !strings.Contains(schema, "upsertAuthorWithRelated(input: UpsertAuthorWithRelatedInput!, conflictTarget: AuthorConflictTarget! = PK): Author!") {
		t.Fatalf("schema has no upsertAuthorWithRelated taking conflictTarget:\n%s", schema)
	}
	enum := trsTypeBlock(schema, "enum AuthorConflictTarget {")
	for _, v := range []string{"PK", "EMAIL"} {
		if !pprHasLine(enum, v) {
			t.Errorf("enum AuthorConflictTarget lacks %s:\n%s", v, enum)
		}
	}
	resolvers := pprReadGenerated(t, res, "models/graph/sqlgenresolver/resolvers_gen.go")
	if body := extractFunc(t, resolvers, "UpsertAuthorWithRelated"); !strings.Contains(body, ".UpsertWithRelated(ctx, input, target,") {
		t.Errorf("UpsertAuthorWithRelated does not hand the conflict target to the client:\n%s", body)
	}
	client := pprReadGenerated(t, res, "models/models_gen.go")
	for _, want := range []string{
		`AuthorConflictEmail: {"email"},`,
		`AuthorConflictPK:    {"id"},`,
		`updateColumns := excludeColumns(columns, append(conflictColumns, "id"))`,
	} {
		if !strings.Contains(client, want) {
			t.Errorf("generated client lacks %q", want)
		}
	}

	// What the runtime builds from each target's key set, the way the
	// generated client computes it.
	columns := []string{"email", "name"}
	build := func(keys []string) string {
		update := slices.DeleteFunc(slices.Clone(columns), func(c string) bool { return slices.Contains(keys, c) || c == "id" })
		q, _ := sql.BuildInsert(sql.NewMySQLDialect(), sql.Table{Name: "authors"}, sql.InsertOptions{
			Columns: columns, Values: []any{"a@example.com", "A"},
			UpsertConflictKeys: keys, UpsertUpdateColumns: update,
		})
		return q
	}
	tests := []struct {
		name       string
		keys       []string
		wantUpdate string
	}{
		// PK is unwritten here (AUTO_INCREMENT), so it carries no clause at all —
		// a plain insert, on MySQL as on every dialect (PRD §9.5).
		{name: "PK target on a generated key is a plain insert", keys: []string{"id"}, wantUpdate: ""},
		{name: "EMAIL target updates every other written column", keys: []string{"email"}, wantUpdate: "ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := build(tt.keys)
			_, clause, _ := strings.Cut(q, ") VALUES (?, ?)")
			if got := strings.TrimSpace(clause); got != tt.wantUpdate {
				t.Errorf("BuildInsert(target %v) tail = %q, want %q\nfull: %s", tt.keys, got, tt.wantUpdate, q)
			}
			if strings.Contains(q, "ON CONFLICT") || strings.Contains(q, "CONSTRAINT") {
				t.Errorf("the MySQL statement names a conflict target: %s", q)
			}
		})
	}
}
