package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// Views on the GraphQL surface — context, config and gating.
//
// PRD §26.4 "Views on the GraphQL surface" closes the emitted set: object type,
// three envelopes, filter + sort inputs, up to three Query fields, and NEVER a
// mutation or a Create/Update input. Views live in APIContext.Tables with
// IsView rather than a parallel context type, so everything downstream —
// templates, walker, envelope aliases, gqlgen `models:` merge, translators —
// picks them up through the one slice it already iterates.

// --- fixtures ---

// apiViewCol builds a view column. Views carry no DDL primary key; `pk` models
// the @pk annotation, which is what buildViewColumns reads off parser.Column.
func apiViewCol(name, sqlType string, nullable, pk bool) parser.Column {
	return parser.Column{Name: name, Type: sqlType, Nullable: nullable, PrimaryKey: pk}
}

// apiViewSchema pairs one table with one view so every assertion below can also
// check that the table half is untouched. The view carries `id` so the default
// `generation.cursor_keys` (["id"], see testInput) resolves and Connection is
// emitted; the pk-less variants override the columns.
func apiViewSchema(viewCols ...parser.Column) *parser.Schema {
	if len(viewCols) == 0 {
		viewCols = []parser.Column{
			apiViewCol("id", "uuid", false, true),
			apiViewCol("title", "text", true, false),
			apiViewCol("order_count", "integer", false, false),
		}
	}
	return &parser.Schema{
		Tables: []parser.Table{{
			Name: "products",
			Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "title", Type: "text"},
			},
		}},
		Views: []parser.View{{Name: "product_summary", Columns: viewCols}},
	}
}

// buildAPIWithViews runs the full table + view context build and hands back the
// API context, so each test exercises the same path `sqlgen generate` does.
func buildAPIWithViews(t *testing.T, in *gen.GenerateInput) *gen.APIContext {
	t.Helper()
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext returned nil")
	}
	return apiCtx
}

func findAPIEntity(t *testing.T, ctx *gen.APIContext, structName string) *gen.APITableContext {
	t.Helper()
	for i := range ctx.Tables {
		if ctx.Tables[i].StructName == structName {
			return &ctx.Tables[i]
		}
	}
	return nil
}

// --- context + gating ---

// TestBuildAPIContext_viewIsAFirstClassEntry pins that the view lands in
// APIContext.Tables (not a parallel slice) carrying IsView, a read-only
// operation set, and both mutation-input flags false — the three facts every
// downstream mutation-input gate reads to suppress the write half.
func TestBuildAPIContext_viewIsAFirstClassEntry(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	apiCtx := buildAPIWithViews(t, in)

	if len(apiCtx.Tables) != 2 {
		t.Fatalf("Tables: got %d entries, want 2 (one table + one view)", len(apiCtx.Tables))
	}
	v := findAPIEntity(t, apiCtx, "ProductSummary")
	if v == nil {
		t.Fatal("view ProductSummary absent from APIContext.Tables")
	}

	if !v.IsView {
		t.Error("IsView = false, want true")
	}
	if v.SQLTable != "product_summary" {
		t.Errorf("SQLTable = %q, want %q", v.SQLTable, "product_summary")
	}
	// The unified client names a view singularly and a table plurally; the
	// resolver template calls the accessor the client actually declares.
	if v.ClientAccessor != "ProductSummary" {
		t.Errorf("ClientAccessor = %q, want %q (views are singular on the client)", v.ClientAccessor, "ProductSummary")
	}
	if v.HasCreateInput || v.HasUpdateInput {
		t.Errorf("Has*Input = (%v, %v), want (false, false)", v.HasCreateInput, v.HasUpdateInput)
	}
	if len(v.Relationships) != 0 {
		t.Errorf("Relationships = %d, want 0 (a view has none — PRD §16.4)", len(v.Relationships))
	}
	if len(v.Fields) != 3 {
		t.Errorf("Fields = %d, want 3", len(v.Fields))
	}
	// No column claims membership in a mutation input a view never emits —
	// the flags are skipped for a read-only entity rather than computed and
	// left for a later surface to misread.
	for _, f := range v.Fields {
		if f.InCreateInput || f.InUpdateInput {
			t.Errorf("column %q: InCreateInput=%v InUpdateInput=%v, want both false on a view", f.SQLName, f.InCreateInput, f.InUpdateInput)
		}
	}
	if err := gen.ValidateAPIViewReadOnly(*v); err != nil {
		t.Errorf("the built view context failed the read-only guard: %v", err)
	}

	// Read-only operation set: the three API reads on, every mutation off.
	// GetMany backs no API surface, so it is not part of the set.
	o := v.Operations
	if !o.Get || o.GetMany || !o.Paginate || !o.Connection {
		t.Errorf("read operations = get:%v get_many:%v paginate:%v connection:%v, want get, paginate and connection only", o.Get, o.GetMany, o.Paginate, o.Connection)
	}
	if o.Create || o.CreateMany || o.Update || o.UpdateMany || o.Upsert || o.SoftDelete || o.HardDelete || o.Restore || o.Increment || o.UpdateWhere {
		t.Errorf("view carries a mutation operation: %+v", o)
	}

	// The table half is a plain table, unaffected by the view's presence.
	tbl := findAPIEntity(t, apiCtx, "Product")
	if tbl == nil {
		t.Fatal("table Product absent from APIContext.Tables")
	}
	if tbl.IsView {
		t.Error("table Product: IsView = true, want false")
	}
	if tbl.ClientAccessor != "Products" {
		t.Errorf("table Product: ClientAccessor = %q, want %q", tbl.ClientAccessor, "Products")
	}
}

// TestBuildAPIContext_viewAPIEnabledFalseExcludesEntirely pins the §26.4 gating
// rule: the view is absent from the context, which removes the type, the
// queries, the inputs, the translators and the gqlgen bindings in one move —
// every one of them is derived from the slice it is no longer in.
func TestBuildAPIContext_viewAPIEnabledFalseExcludesEntirely(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	in.Config.Views["product_summary"] = config.ViewConfig{
		API: &config.ViewAPIConfig{Enabled: new(false)},
	}
	apiCtx := buildAPIWithViews(t, in)

	if v := findAPIEntity(t, apiCtx, "ProductSummary"); v != nil {
		t.Fatalf("view present with api.enabled=false: %+v", v.StructName)
	}
	if len(apiCtx.Tables) != 1 {
		t.Fatalf("Tables: got %d, want 1 (the table only)", len(apiCtx.Tables))
	}
	// It must also surrender its GraphQL type-name claims — an excluded entity
	// declares nothing, so it cannot collide with anything (PRD §26.4 Rule 3).
	for _, fam := range apiCtx.ComparatorFamilies {
		if strings.HasPrefix(fam.Name, "ProductSummary") {
			t.Errorf("excluded view still contributed comparator family %q", fam.Name)
		}
	}
}

// TestBuildAPIContext_viewOperationsMaskIsSubtractive pins that
// `views.<n>.api.operations` narrows the read set and can only ever narrow it.
func TestBuildAPIContext_viewOperationsMaskIsSubtractive(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	in.Config.Views["product_summary"] = config.ViewConfig{
		API: &config.ViewAPIConfig{Operations: &config.Operations{Connection: new(false)}},
	}
	apiCtx := buildAPIWithViews(t, in)

	v := findAPIEntity(t, apiCtx, "ProductSummary")
	if v == nil {
		t.Fatal("view absent")
	}
	if v.Operations.Connection {
		t.Error("Operations.Connection = true, want false (masked off)")
	}
	if !v.Operations.Get || !v.Operations.Paginate {
		t.Errorf("unmasked reads dropped: get:%v paginate:%v", v.Operations.Get, v.Operations.Paginate)
	}
	if err := gen.ValidateAPIViewReadOnly(*v); err != nil {
		t.Errorf("masked view failed the read-only guard: %v", err)
	}
}

// TestBuildAPIContext_viewGatesByPKAndConnection pins the two conditional Query
// fields from PRD §26.4's emitted-surface table: `<view>` needs a resolved PK,
// `<views>` needs cursor_keys to resolve. Both are expressed as operation
// gating, so the schema, the resolver and the seed agree by construction.
func TestBuildAPIContext_viewGatesByPKAndConnection(t *testing.T) {
	tests := []struct {
		name           string
		cols           []parser.Column
		wantGet        bool
		wantConnection bool
		wantPKArgs     int
	}{
		{
			name: "pk and cursor key present",
			cols: []parser.Column{
				apiViewCol("id", "uuid", false, true),
				apiViewCol("total", "bigint", false, false),
			},
			wantGet: true, wantConnection: true, wantPKArgs: 1,
		},
		{
			name: "no @pk drops the by-pk query but keeps the connection",
			cols: []parser.Column{
				apiViewCol("id", "uuid", false, false),
				apiViewCol("total", "bigint", false, false),
			},
			wantGet: false, wantConnection: true, wantPKArgs: 0,
		},
		{
			name: "cursor keys unresolvable drops the connection only",
			cols: []parser.Column{
				apiViewCol("category_name", "text", false, true),
				apiViewCol("total", "bigint", false, false),
			},
			wantGet: true, wantConnection: false, wantPKArgs: 1,
		},
		{
			name: "neither leaves the list query as the whole surface",
			cols: []parser.Column{
				apiViewCol("category_name", "text", false, false),
				apiViewCol("total", "bigint", false, false),
			},
			wantGet: false, wantConnection: false, wantPKArgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, apiViewSchema(tt.cols...))
			apiCtx := buildAPIWithViews(t, in)
			v := findAPIEntity(t, apiCtx, "ProductSummary")
			if v == nil {
				t.Fatal("view absent")
			}
			if v.Operations.Get != tt.wantGet {
				t.Errorf("Operations.Get = %v, want %v", v.Operations.Get, tt.wantGet)
			}
			if v.Operations.Connection != tt.wantConnection {
				t.Errorf("Operations.Connection = %v, want %v", v.Operations.Connection, tt.wantConnection)
			}
			if len(v.PKArgs) != tt.wantPKArgs {
				t.Errorf("PKArgs = %d, want %d", len(v.PKArgs), tt.wantPKArgs)
			}
			// The list query is unconditional (PRD §26.4) — it is the one field
			// every API-enabled view always offers.
			if !v.Operations.GetMany && !v.Operations.Paginate {
				t.Error("neither GetMany nor Paginate is set — the <view>List query would not be emitted")
			}

			out := renderAPITableSchema(t, *v)
			hasByPK := strings.Contains(out, "\n  productSummary(")
			if hasByPK != tt.wantGet {
				t.Errorf("schema by-PK query present = %v, want %v\n%s", hasByPK, tt.wantGet, out)
			}
			hasConnection := strings.Contains(out, "productSummaries(filter:")
			if hasConnection != tt.wantConnection {
				t.Errorf("schema connection query present = %v, want %v\n%s", hasConnection, tt.wantConnection, out)
			}
			mustContain(t, out, "productSummaryList(filter:")
		})
	}
}

// --- emission shape ---

// TestAPIViewSchema_emitsReadOnlySurface renders the view entry through the
// same template a table goes through and pins PRD §26.4's closed set: object
// type, three envelopes, filter + sort inputs, no mutation block, and no
// Create/Update input.
func TestAPIViewSchema_emitsReadOnlySurface(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	apiCtx := buildAPIWithViews(t, in)
	v := findAPIEntity(t, apiCtx, "ProductSummary")
	if v == nil {
		t.Fatal("view absent")
	}
	out := renderAPITableSchema(t, *v)

	for _, want := range []string{
		"type ProductSummary {",
		"type ProductSummaryConnection {",
		"type ProductSummaryEdge {",
		"type ProductSummaryListResult {",
		"input ProductSummaryFilter {",
		"enum ProductSummarySortField {",
		"input ProductSummarySort {",
		"extend type Query {",
		// The placeholder description says "view", not "table" — the noun is
		// resolved on the context, not hard-coded in the template.
		"ProductSummary corresponds to the product_summary view.",
	} {
		mustContain(t, out, want)
	}
	for _, unwanted := range []string{
		"extend type Mutation",
		"input CreateProductSummaryInput",
		"input UpdateProductSummaryInput",
		"createProductSummary",
		"updateProductSummary",
		"deleteProductSummary",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("view schema contains %q — a view emits no mutation surface (PRD §16.4 / §26.4)\n%s", unwanted, out)
		}
	}
}

// TestHasAnyMutation_isFalseForEveryViewShape pins the funcmap guard the
// mutation block gates on. It is checked through the registered funcmap entry
// because that is the only thing the template can call.
func TestHasAnyMutation_isFalseForEveryViewShape(t *testing.T) {
	fn, ok := gen.FuncMap(sql.NewPostgresDialect())["hasAnyMutation"].(func(gen.APITableContext) bool)
	if !ok {
		t.Fatal("hasAnyMutation is not registered with the expected signature")
	}

	shapes := []struct {
		name string
		ctx  gen.APITableContext
	}{
		{"bare view", gen.APITableContext{IsView: true}},
		{"view with @pk", gen.APITableContext{IsView: true, PKArgs: []gen.APIPKArg{{Name: "id"}}, Operations: gen.ResolvedOperations{Get: true}}},
		{"view with every read op", gen.APITableContext{IsView: true, Operations: gen.ResolvedOperations{Get: true, GetMany: true, Paginate: true, Connection: true}}},
		// The guard states the rule rather than relying on the read-only
		// context happening to zero every disjunct: even a context wrongly
		// carrying mutation state must not open a Mutation block on a view.
		{"view wrongly carrying mutation state", gen.APITableContext{
			IsView:         true,
			Operations:     gen.ResolvedOperations{Create: true, HardDelete: true, Restore: true},
			HasCreateInput: true,
			HasUpdateInput: true,
		}},
	}
	for _, s := range shapes {
		if fn(s.ctx) {
			t.Errorf("%s: hasAnyMutation = true, want false", s.name)
		}
	}

	// The table path is unchanged.
	table := gen.APITableContext{Operations: gen.ResolvedOperations{Create: true}, HasCreateInput: true}
	if !fn(table) {
		t.Error("table with create: hasAnyMutation = false, want true")
	}
}

// TestAPISchema_allReadsMaskedOpensNoQueryBlock pins the read-side twin of the
// hasAnyMutation guard. Reads are maskable on tables and views alike
// (PRD §26.5.1, §26.4 "Views on the GraphQL surface"), and a mask that turns
// off all four used to leave `extend type Query {\n}`, which gqlgen rejects
// with "expected at least one definition, found }" — measured on the view
// before the guard. The rest of the entity's surface is untouched.
//
// An entity left with no root resolver at all must also get no resolver seed:
// its body is empty, gqlgen renders no resolver file for that schema source,
// and the seed's import block reached the build unused ("\"context\" imported
// and not used", measured on the view once the schema guard let gqlgen get
// that far). The key-only table is the case the root-field list cannot
// answer: its create / update operations stay on, but with no input to take
// none of them emits.
func TestAPISchema_allReadsMaskedOpensNoQueryBlock(t *testing.T) {
	readsOff := func() *config.Operations {
		return &config.Operations{Get: new(false), GetMany: new(false), Paginate: new(false), Connection: new(false)}
	}
	schema := func() *parser.Schema {
		s := apiViewSchema()
		s.Tables = append(s.Tables, parser.Table{
			Name:    "key_only_rows",
			Columns: []parser.Column{{Name: "id", Type: "bigserial", PrimaryKey: true}},
		})
		return s
	}
	tests := []struct {
		name       string
		structName string
		snakeName  string
		configure  func(c *config.RootConfig)
		wantStill  []string // surface the mask must leave in place
		wantSeed   bool
	}{
		{
			name:       "table",
			structName: "Product",
			snakeName:  "product",
			configure: func(c *config.RootConfig) {
				c.Tables["products"] = config.TableConfig{API: &config.TableAPIConfig{Operations: readsOff()}}
			},
			wantStill: []string{"type Product {", "extend type Mutation {", "createProduct("},
			wantSeed:  true,
		},
		{
			name:       "view",
			structName: "ProductSummary",
			snakeName:  "product_summary",
			configure: func(c *config.RootConfig) {
				c.Views["product_summary"] = config.ViewConfig{API: &config.ViewAPIConfig{Operations: readsOff()}}
			},
			wantStill: []string{"type ProductSummary {", "input ProductSummaryFilter {"},
		},
		{
			name:       "key-only table with reads and deletes off",
			structName: "KeyOnlyRow",
			snakeName:  "key_only_row",
			configure: func(c *config.RootConfig) {
				ops := readsOff()
				ops.HardDelete, ops.SoftDelete = new(false), new(false)
				c.Tables["key_only_rows"] = config.TableConfig{API: &config.TableAPIConfig{Operations: ops}}
			},
			wantStill: []string{"type KeyOnlyRow {"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, schema())
			tt.configure(in.Config)
			apiCtx := buildAPIWithViews(t, in)
			e := findAPIEntity(t, apiCtx, tt.structName)
			if e == nil {
				t.Fatalf("%s absent from the API context", tt.structName)
			}
			out := renderAPITableSchema(t, *e)
			if strings.Contains(out, "extend type Query") {
				t.Errorf("%s with every read masked off opens a Query extension (gqlgen rejects an empty one)\n--- output ---\n%s", tt.name, out)
			}
			for _, want := range tt.wantStill {
				if !strings.Contains(out, want) {
					t.Errorf("%s schema lost %q under a reads-only mask\n--- output ---\n%s", tt.name, want, out)
				}
			}

			apiCtx.ModelsPackage = "models"
			apiCtx.GqlgenModelAlias = "gqlmodel"
			apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
			apiCtx.ClientName = "Client"
			seeds, err := gen.RenderAPISeeds(sql.NewPostgresDialect(), apiCtx)
			if err != nil {
				t.Fatalf("RenderAPISeeds: %v", err)
			}
			gotSeed := slices.ContainsFunc(seeds, func(s gen.SeedBody) bool { return s.SnakeName == tt.snakeName })
			if gotSeed != tt.wantSeed {
				t.Errorf("%s: RenderAPISeeds emitted a %q seed = %v, want %v", tt.name, tt.snakeName, gotSeed, tt.wantSeed)
			}
		})
	}
}

// TestCollectSqlgenRootFields_configKey pins the owner each root field
// carries. The wrapper's post-gqlgen stub check names it, with its
// struct_name, when gqlgen spells the resolver differently, so a
// view must point at `views.` and a table at `tables.`.
func TestCollectSqlgenRootFields_configKey(t *testing.T) {
	apiCtx := buildAPIWithViews(t, apiTestInput(t, apiViewSchema()))
	queries, mutations := gen.CollectSqlgenRootFields(apiCtx)
	want := map[string]string{"Product": "tables.products", "ProductSummary": "views.product_summary"}
	seen := make(map[string]bool)
	for _, f := range slices.Concat(queries, mutations) {
		var owner string
		switch {
		case strings.Contains(f.GoName, "ProductSummar"):
			owner = "ProductSummary"
		case strings.Contains(f.GoName, "Product"):
			owner = "Product"
		default:
			t.Fatalf("unexpected root field %+v", f)
		}
		seen[owner] = true
		if f.ConfigKey != want[owner] {
			t.Errorf("%s ConfigKey = %q, want %q", f.GoName, f.ConfigKey, want[owner])
		}
	}
	if len(seen) != len(want) {
		t.Errorf("root fields covered %v, want both the table and the view", seen)
	}
}

// --- lints ---

// TestValidateAPIWalkerCompleteness_view pins that the generalized lint treats a
// view identically: it reports against "view", it passes when every readable
// column has a case, and it fails when one is missing.
func TestValidateAPIWalkerCompleteness_view(t *testing.T) {
	in := apiTestInput(t, apiViewSchema())
	views, err := gen.BuildViewContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views: got %d, want 1", len(views))
	}
	apiCtx := buildAPIWithViews(t, in)
	v := findAPIEntity(t, apiCtx, "ProductSummary")
	if v == nil {
		t.Fatal("view absent")
	}

	src := gen.APIEntityFromView(views[0])
	if src.Kind != "view" {
		t.Errorf("APIEntityFromView Kind = %q, want %q", src.Kind, "view")
	}
	if len(src.Relationships) != 0 {
		t.Errorf("APIEntityFromView Relationships = %d, want 0", len(src.Relationships))
	}
	if err := gen.ValidateAPIWalkerCompleteness(src, *v, nil); err != nil {
		t.Fatalf("walker lint rejected a well-formed view: %v", err)
	}

	// Drop a case and confirm the lint names the view and the column.
	missing := *v
	missing.Fields = v.Fields[:len(v.Fields)-1]
	err = gen.ValidateAPIWalkerCompleteness(src, missing, nil)
	if err == nil {
		t.Fatal("walker lint accepted a view missing a column case")
	}
	for _, want := range []string{"view", "product_summary", "order_count"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestValidateAPIViewReadOnly pins the codegen guard that a view can never
// reach a mutation-emitting code path (PRD §16.4 / §26.4). Each case sets one
// field a mutation template gates on.
func TestValidateAPIViewReadOnly(t *testing.T) {
	base := gen.APITableContext{IsView: true, SQLTable: "product_summary"}

	tests := []struct {
		name string
		ctx  gen.APITableContext
		want string
	}{
		{"clean view", base, ""},
		{"create", withView(base, func(v *gen.APITableContext) { v.Operations.Create = true }), "create"},
		{"update", withView(base, func(v *gen.APITableContext) { v.Operations.Update = true }), "update"},
		{"upsert", withView(base, func(v *gen.APITableContext) { v.Operations.Upsert = true }), "upsert"},
		{"hard delete", withView(base, func(v *gen.APITableContext) { v.Operations.HardDelete = true }), "hard-delete"},
		{"soft delete", withView(base, func(v *gen.APITableContext) { v.Operations.SoftDelete = true }), "soft-delete"},
		{"restore", withView(base, func(v *gen.APITableContext) { v.Operations.Restore = true }), "restore"},
		{"increment", withView(base, func(v *gen.APITableContext) { v.Operations.Increment = true }), "increment"},
		{"create input", withView(base, func(v *gen.APITableContext) { v.HasCreateInput = true }), "create-input"},
		{"update input", withView(base, func(v *gen.APITableContext) { v.HasUpdateInput = true }), "update-input"},
		{"upsert conflict target", withView(base, func(v *gen.APITableContext) { v.HasConflictPK = true }), "upsert-conflict-target"},
		{"increment enum", withView(base, func(v *gen.APITableContext) { v.IncrementEnumType = "X" }), "increment-column-enum"},
		{
			"relationship",
			withView(base, func(v *gen.APITableContext) {
				v.Relationships = []gen.APIRelationshipContext{{SQLName: "orders"}}
			}),
			"relationship",
		},
		{
			"a field claiming mutation-input membership",
			withView(base, func(v *gen.APITableContext) {
				v.Fields = []gen.APIFieldContext{{SQLName: "title", InCreateInput: true}}
			}),
			"claims membership in a mutation input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gen.ValidateAPIViewReadOnly(tt.ctx)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error, want one mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "product_summary") {
				t.Errorf("error %q does not name the view", err)
			}
		})
	}

	// A table is out of scope for the guard entirely — it must not fire on one.
	table := gen.APITableContext{SQLTable: "products", Operations: gen.ResolvedOperations{Create: true}, HasCreateInput: true}
	if err := gen.ValidateAPIViewReadOnly(table); err != nil {
		t.Errorf("guard fired on a table: %v", err)
	}
}

// TestValidateAPIViewReadOnly_catchAllIsLive pins that the guard's closing
// assertion is not dead code. It reads mutationSurface — funcHasAnyMutation's
// disjunction with the IsView short-circuit peeled off — because
// funcHasAnyMutation answers false for every view by its own guard, so asserting
// it would assert the guard against itself and catch nothing.
//
// The shape below sets ONLY fields the explicit per-field checks do not cover
// on their own path to the catch-all, so a regression that reverts the
// assertion to funcHasAnyMutation fails here rather than passing silently.
func TestValidateAPIViewReadOnly_catchAllIsLive(t *testing.T) {
	v := gen.APITableContext{
		IsView:     true,
		SQLTable:   "product_summary",
		Operations: gen.ResolvedOperations{SoftDelete: true},
	}
	err := gen.ValidateAPIViewReadOnly(v)
	if err == nil {
		t.Fatal("guard accepted a view with a live mutation surface")
	}
	if !strings.Contains(err.Error(), "live mutation surface") {
		t.Errorf("error %q does not come from the catch-all assertion — it may have reverted to funcHasAnyMutation, which is false for every view", err)
	}
}

func withView(base gen.APITableContext, mutate func(*gen.APITableContext)) gen.APITableContext {
	out := base
	mutate(&out)
	return out
}

// --- GraphQL type-name ownership (extended to views) ---

// TestBuildAPIContext_viewClaimsItsGraphQLTypeNames pins the view arm on
// collectGraphQLTypeNames. Before it, a view colliding with a table or a scalar
// reached gqlparser as `Cannot redeclare type X`, naming neither claimant.
func TestBuildAPIContext_viewClaimsItsGraphQLTypeNames(t *testing.T) {
	t.Run("view against table", func(t *testing.T) {
		in := apiTestInput(t, &parser.Schema{
			Tables: []parser.Table{{
				Name:    "summaries",
				Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
			}},
			// `summary` singularizes onto the same `Summary` the table claims.
			Views: []parser.View{{
				Name:    "summary",
				Columns: []parser.Column{apiViewCol("id", "uuid", false, true)},
			}},
		})
		tables, err := gen.BuildTableContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts: %v", err)
		}
		views, err := gen.BuildViewContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildViewContexts: %v", err)
		}
		_, err = gen.BuildAPIContext(tables, views, nil, nil, in.Config)
		if err == nil {
			t.Fatal("BuildAPIContext accepted a view and a table claiming one GraphQL type name")
		}
		for _, want := range []string{"view", "table", "Summary"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	// PRD §26.4's own worked example, one entity kind over: an `interval`
	// column declares `scalar Duration`, and a view named `duration` resolves
	// to `type Duration` beside it. Legal in Go, rejected by gqlparser.
	t.Run("view against a scalar in use offers the views: escape", func(t *testing.T) {
		in := apiTestInput(t, &parser.Schema{
			Tables: []parser.Table{{
				Name: "jobs",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "runtime", Type: "interval"},
				},
			}},
			Views: []parser.View{{
				Name:    "duration",
				Columns: []parser.Column{apiViewCol("id", "uuid", false, true)},
			}},
		})
		tables, err := gen.BuildTableContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts: %v", err)
		}
		views, err := gen.BuildViewContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildViewContexts: %v", err)
		}
		if _, err = gen.BuildAPIContext(tables, views, nil, nil, in.Config); err == nil {
			t.Fatal("BuildAPIContext accepted a view claiming the Duration scalar's name")
		}
		if !strings.Contains(err.Error(), "Duration") {
			t.Errorf("error %q does not name the contested type", err)
		}
		if !strings.Contains(err.Error(), "views.duration.struct_name") {
			t.Errorf("error %q does not offer the views.<n>.struct_name escape", err)
		}
	})
}

// TestBuildAPIContext_viewColumnWithNoGraphQLBindingIsRejected pins the
// fail-CLOSED half of the view projection, which nothing else asserts.
//
// `parser/view.go`'s aggregate table falls back to `any` for an unrecognised
// aggregate and `[]any` for ARRAY_AGG, because neither has an element type it
// can infer. Neither has a GraphQL binding, and the correct outcome is the one
// PRD §26.4.1 prescribes: refuse to generate, naming the column and both
// remedies.
//
// This is the guard that keeps that from decaying into the failure mode the
// `JSON_ARRAYAGG` column actually hit before views were bound — there, the
// column DID bind, to the wrong Go type, and the mismatch surfaced only as a
// gqlgen `panic("not implemented")` field resolver in generated code that
// compiled cleanly. A fallback binding added here later would reproduce that
// exactly, so the assertion is that these types are refused, not merely that
// they are handled.
func TestBuildAPIContext_viewColumnWithNoGraphQLBindingIsRejected(t *testing.T) {
	tests := []struct {
		name    string
		goType  string
		aggNote string
	}{
		{"unrecognised aggregate falls back to any", "any", "inferAggregateType's default arm"},
		{"ARRAY_AGG falls back to a slice of any", "[]any", "inferAggregateType's ARRAY_AGG arm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, &parser.Schema{
				Tables: []parser.Table{{
					Name:    "products",
					Columns: []parser.Column{{Name: "id", Type: "uuid", PrimaryKey: true}},
				}},
				Views: []parser.View{{
					Name: "product_rollup",
					Columns: []parser.Column{
						apiViewCol("id", "uuid", false, true),
						// GoTypeLiteral is the channel the parser uses to hand a
						// pre-resolved aggregate type to codegen (context_view.go's
						// viewColumnGoType reads it before the SQL-to-Go resolver).
						{Name: "vals", Type: tt.goType, GoTypeLiteral: tt.goType},
					},
				}},
			})
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			views, err := gen.BuildViewContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildViewContexts: %v", err)
			}

			_, err = gen.BuildAPIContext(tables, views, nil, nil, in.Config)
			if err == nil {
				t.Fatalf("BuildAPIContext accepted a view column typed %q (%s) — it has no GraphQL binding and must be refused, not bound to a fallback (PRD §26.4.1)", tt.goType, tt.aggNote)
			}
			// The message has to carry enough to act on without reading source:
			// the entity and its kind, the column, the offending Go type, and a
			// remedy.
			for _, want := range []string{"view", "product_rollup", `"vals"`, tt.goType, "no GraphQL binding", "overrides.types"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// --- table output is untouched ---

// TestBuildAPIContext_addingAViewLeavesTableOutputIdentical is the stated
// proof of no collateral movement: adding a view adds entries, it does not
// change the ones already there.
func TestBuildAPIContext_addingAViewLeavesTableOutputIdentical(t *testing.T) {
	withoutView := apiTestInput(t, &parser.Schema{Tables: apiViewSchema().Tables})
	baseCtx := buildAPIWithViews(t, withoutView)
	baseTable := findAPIEntity(t, baseCtx, "Product")
	if baseTable == nil {
		t.Fatal("table absent from the view-free context")
	}
	before := renderAPITableSchema(t, *baseTable)

	withView := apiTestInput(t, apiViewSchema())
	viewCtx := buildAPIWithViews(t, withView)
	afterTable := findAPIEntity(t, viewCtx, "Product")
	if afterTable == nil {
		t.Fatal("table absent from the view-bearing context")
	}
	after := renderAPITableSchema(t, *afterTable)

	if before != after {
		t.Errorf("table schema changed when a view joined the context\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
