package gen_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// twoTableAPITestSchema provides a fixture with two tables (products + reviews)
// so per-table API config can be exercised: disabling API on one table must
// leave the other intact. Both tables include a `deleted_at` column so the
// soft-delete + restore presets register on every fixture.
func twoTableAPITestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
			{
				Name: "reviews",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "rating", Type: "integer", Nullable: false},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
}

// renderAllAPITables renders every per-table schema in apiCtx and returns the
// concatenated body. The tests below grep this stream for surface-level
// presence/absence, so a single buffer is enough.
func renderAllAPITables(t *testing.T, apiCtx *gen.APIContext) string {
	t.Helper()
	var sb strings.Builder
	for _, tc := range apiCtx.Tables {
		sb.WriteString(renderAPITableSchema(t, tc))
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestApiEnabledFalseExcludesType pins PRD §26.10's `api.enabled: false`
// behavior: the disabled table is dropped wholesale from the GraphQL surface.
// Not just queries / mutations — the type itself plus every supporting input
// (Filter, Sort, Connection, Edge, ListResult, CreateInput, UpdateInput)
// must be absent from the schema so no orphan reference survives
// the wrapper-driven gqlgen run.
func TestApiEnabledFalseExcludesType(t *testing.T) {
	in := apiTestInput(t, twoTableAPITestSchema())
	f := false
	in.Config.Tables["products"] = config.TableConfig{
		API: &config.TableAPIConfig{Enabled: &f},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if got := len(apiCtx.Tables); got != 1 {
		t.Fatalf("Tables: got %d, want 1 (products excluded)", got)
	}
	if apiCtx.Tables[0].StructName != "Review" {
		t.Fatalf("remaining table = %q, want Review", apiCtx.Tables[0].StructName)
	}

	out := renderAllAPITables(t, apiCtx)

	// Every disabled-table-derived schema element must be absent. The full
	// list is enumerated so a regression that re-introduces any single
	// orphan input fails fast.
	for _, forbidden := range []string{
		"type Product ",
		"type Product\n",
		"type Product{",
		"ProductConnection",
		"ProductEdge",
		"ProductListResult",
		"ProductFilter",
		"ProductSort",
		"ProductSortField",
		"CreateProductInput",
		"UpdateProductInput",
		"product(id:",
		"products(filter:",
		"productList(",
		"createProduct(",
		"createProducts(",
		"updateProduct(",
		"updateProducts(",
		"deleteProduct(",
		"hardDeleteProduct(",
		"softDeleteProduct(",
		"restoreProduct(",
		"upsertProduct(",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("output contains forbidden symbol %q (api.enabled:false should drop it)\n--- output ---\n%s", forbidden, out)
		}
	}

	// Sanity: the kept table's surface still emits.
	for _, want := range []string{
		"type Review {",
		"type ReviewConnection",
		"input ReviewFilter",
		"input CreateReviewInput",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing kept-table symbol %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestAPIObjectType_APIDisabledTargetDropped pins the other half of §26.10's
// `api.enabled: false`: a relationship into the hidden entity has no
// field on the parent's object type, no walker case and no API relationship,
// while the Go model keeps the edge. Previously the field survived and
// named a type the schema never declared, so gqlgen rejected the document.
//
// Covered for a list field (M2M Categories) and a single-object field (O2O
// Bio), each built exposed and hidden, so a keying mistake that strips an
// exposed target fails here too.
func TestAPIObjectType_APIDisabledTargetDropped(t *testing.T) {
	tests := []struct {
		name      string
		target    string // SQL table the edge points at
		goField   string
		fieldLine string // the object-type field as the schema renders it
		walkCase  string
	}{
		{"m2m list", "categories", "Categories", "categories: [Category!]!", `case "categories":`},
		{"o2o single", "bios", "Bio", "bio: Bio", `case "bio":`},
	}
	for _, tt := range tests {
		for _, hidden := range []bool{false, true} {
			name := tt.name + "/exposed"
			if hidden {
				name = tt.name + "/hidden"
			}
			t.Run(name, func(t *testing.T) {
				apiCtx, tables := buildNestedAPI(t, func(cfg *config.RootConfig) {
					if hidden {
						setTableAPI(cfg, tt.target, config.TableAPIConfig{Enabled: new(false)})
					}
				})
				at := apiUsers(t, apiCtx)

				hasRel := slices.ContainsFunc(at.Relationships, func(r gen.APIRelationshipContext) bool { return r.GoFieldName == tt.goField })
				if hasRel == hidden {
					t.Errorf("API relationship %s present = %v, want %v", tt.goField, hasRel, !hidden)
				}

				typeBlock, _, _ := strings.Cut(renderAPITableSchema(t, *at), "type UserConnection")
				if got := strings.Contains(typeBlock, tt.fieldLine); got == hidden {
					t.Errorf("type User has %q = %v, want %v\n%s", tt.fieldLine, got, !hidden, typeBlock)
				}

				walker := walkerBody(t, renderAPIFieldOptions(t, apiCtx), "userFieldOptionsFromCollected")
				if got := strings.Contains(walker, tt.walkCase); got == hidden {
					t.Errorf("user walker has %q = %v, want %v", tt.walkCase, got, !hidden)
				}

				if !slices.ContainsFunc(tables["users"].Relationships, func(r gen.RelationshipContext) bool { return r.FieldName == tt.goField }) {
					t.Errorf("Go model lost edge %s — api gates the API, never the client", tt.goField)
				}
			})
		}
	}
}

// TestAPIObjectType_APIDisabledViewTargetDropped is the view half of the rule
// above: a declared relationship may point at a view, and a view is on the API
// exactly when `views.<n>.api.enabled` says so. The exposed case pins that the
// exposure set includes views at all — keyed on tables alone, it would strip a
// working field with every lint still passing.
func TestAPIObjectType_APIDisabledViewTargetDropped(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprintf("hidden=%v", hidden), func(t *testing.T) {
			in := apiTestInput(t, relTargetSchema())
			in.Config.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
				{Name: "Stats", Type: "one_to_many", Table: "user_stats", FK: "user_id"},
			}}
			if hidden {
				in.Config.Views["user_stats"] = config.ViewConfig{API: &config.ViewAPIConfig{Enabled: new(false)}}
			}
			tables, views, err := gen.BuildEntityContextsFromSchema(in.Schema, in.Config)
			if err != nil {
				t.Fatalf("BuildEntityContextsFromSchema() error: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, views, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext() error: %v", err)
			}
			at := apiUsers(t, apiCtx)

			if got := slices.ContainsFunc(at.Relationships, func(r gen.APIRelationshipContext) bool { return r.GoFieldName == "Stats" }); got == hidden {
				t.Errorf("API relationship Stats present = %v, want %v", got, !hidden)
			}
			typeBlock, _, _ := strings.Cut(renderAPITableSchema(t, *at), "type UserConnection")
			if got := strings.Contains(typeBlock, "stats: [UserStat!]!"); got == hidden {
				t.Errorf("type User has the stats field = %v, want %v\n%s", got, !hidden, typeBlock)
			}
			users := tables[slices.IndexFunc(tables, func(tc gen.TableContext) bool { return tc.TableName == "users" })]
			if !slices.ContainsFunc(users.Relationships, func(r gen.RelationshipContext) bool { return r.FieldName == "Stats" }) {
				t.Error("Go model lost edge Stats — api gates the API, never the client")
			}
		})
	}
}

// renderAPIFieldOptions renders the api/field-options template over a built
// APIContext.
func renderAPIFieldOptions(t *testing.T, apiCtx *gen.APIContext) string {
	t.Helper()
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/field-options", apiCtx); err != nil {
		t.Fatalf("rendering api/field-options: %v", err)
	}
	return buf.String()
}

// TestOperationsPreset_ReadOnly pins PRD §26.5.1 + §26.10: a `read_only`
// preset on a table emits Query fields only — no Mutation extension at all.
// gqlgen rejects empty `extend type Mutation { }` blocks at parse time, so
// the `hasAnyMutation` template guard must keep the whole block out of the
// output.
func TestOperationsPreset_ReadOnly(t *testing.T) {
	in := apiTestInput(t, twoTableAPITestSchema())
	in.Config.Tables["products"] = config.TableConfig{
		API: &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetReadOnly}},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	productCtx := apiProductTable(t, apiCtx)
	out := renderAPITableSchema(t, productCtx)

	if strings.Contains(out, "extend type Mutation") {
		t.Errorf("read_only preset must not emit Mutation extension\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "extend type Query {") {
		t.Errorf("read_only preset must still emit Query extension\n--- output ---\n%s", out)
	}

	// No mutation-class field of any kind survives the preset.
	for _, forbidden := range []string{
		"createProduct",
		"updateProduct",
		"upsertProduct",
		"deleteProduct",
		"hardDeleteProduct",
		"softDeleteProduct",
		"restoreProduct",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("read_only preset must not emit %q\n--- output ---\n%s", forbidden, out)
		}
	}
}

// TestOperationsPreset_NoDelete pins PRD §26.5.1 + §26.10: a `no_delete`
// preset on a table excludes every delete-class mutation
// (`delete<Table>` / `hardDelete<Table>` / `softDelete<Table>` / `restore<Table>`)
// while leaving create + update + upsert + read paths intact.
func TestOperationsPreset_NoDelete(t *testing.T) {
	in := apiTestInput(t, twoTableAPITestSchema())
	in.Config.Tables["products"] = config.TableConfig{
		API: &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetNoDelete}},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	productCtx := apiProductTable(t, apiCtx)
	out := renderAPITableSchema(t, productCtx)

	for _, forbidden := range []string{
		"deleteProduct(",
		"hardDeleteProduct(",
		"softDeleteProduct(",
		"restoreProduct(",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("no_delete preset must not emit %q\n--- output ---\n%s", forbidden, out)
		}
	}

	for _, want := range []string{
		"createProduct(",
		"updateProduct(",
		"upsertProduct(",
		"product(id:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no_delete preset must still emit %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestOperationsPreset_NoHardDelete pins PRD §26.5.1 + §26.10: a
// `no_hard_delete` preset on a table excludes the hard-delete mutation while
// leaving soft-delete + restore in place. The schema template emits the
// unified `delete<Table>(id): <Table>` form (returning the soft-deleted row)
// when SoftDelete is set and HardDelete is not — that surface plus
// `restore<Table>` is what the test pins.
func TestOperationsPreset_NoHardDelete(t *testing.T) {
	in := apiTestInput(t, twoTableAPITestSchema())
	in.Config.Tables["products"] = config.TableConfig{
		API: &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetNoHardDelete}},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	productCtx := apiProductTable(t, apiCtx)
	out := renderAPITableSchema(t, productCtx)

	if strings.Contains(out, "hardDeleteProduct") {
		t.Errorf("no_hard_delete preset must not emit hardDeleteProduct\n--- output ---\n%s", out)
	}

	// Soft-delete with no hard-delete renders as `delete<Table>(id): <Table>`
	// per the schema template's two-of-three operation gating.
	for _, want := range []string{
		"deleteProduct(id: UUID!): Product",
		"restoreProduct(id: UUID!): Product",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no_hard_delete preset must emit %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestOperationsPreset_AppendOnly pins PRD §26.5.1 + §26.10: an `append_only`
// preset on a table emits create + read paths only. Update / upsert / every
// delete-class mutation are excluded.
func TestOperationsPreset_AppendOnly(t *testing.T) {
	in := apiTestInput(t, twoTableAPITestSchema())
	in.Config.Tables["products"] = config.TableConfig{
		API: &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetAppendOnly}},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	productCtx := apiProductTable(t, apiCtx)
	out := renderAPITableSchema(t, productCtx)

	for _, forbidden := range []string{
		"updateProduct(",
		"updateProducts(",
		"upsertProduct(",
		"deleteProduct(",
		"hardDeleteProduct(",
		"softDeleteProduct(",
		"restoreProduct(",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("append_only preset must not emit %q\n--- output ---\n%s", forbidden, out)
		}
	}

	for _, want := range []string{
		"createProduct(",
		"createProducts(",
		"product(id:",
		"products(filter:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("append_only preset must emit %q\n--- output ---\n%s", want, out)
		}
	}
}

// apiProductTable returns the per-table API context for the `Product` struct
// in the two-table fixture (or fails the test when it is missing). Keeps the
// preset assertions readable when a preset accidentally drops the table
// entirely.
func apiProductTable(t *testing.T, apiCtx *gen.APIContext) gen.APITableContext {
	t.Helper()
	for _, tc := range apiCtx.Tables {
		if tc.StructName == "Product" {
			return tc
		}
	}
	t.Fatalf("api table %q not found in apiCtx.Tables (have %d entries)", "Product", len(apiCtx.Tables))
	return gen.APITableContext{}
}
