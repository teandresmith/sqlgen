package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// walkerFixtureSchema returns a fixture exercising every relationship variant
// the walker template emits — O2O (`company`), O2M (`reviews`), and M2M
// (`tags` via `product_tags`) — alongside scalar columns (PK + non-null +
// nullable) so the per-table emission shape covers both column and
// relationship cases.
func walkerFixtureSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "description", Type: "text", Nullable: true},
					{Name: "created_at", Type: "timestamp", Nullable: false},
				},
			},
			{
				Name: "companies",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
			{
				Name: "reviews",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "product_id", Type: "uuid", Nullable: false},
					{Name: "rating", Type: "integer", Nullable: false},
				},
			},
			{
				Name: "tags",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "label", Type: "text", Nullable: false},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "company",
				Type:        parser.OneToOne,
				SourceTable: "products",
				TargetTable: "companies",
				FKColumn:    "company_id",
			},
			{
				Name:        "reviews",
				Type:        parser.OneToMany,
				SourceTable: "products",
				TargetTable: "reviews",
				FKColumn:    "product_id",
			},
			{
				Name:                "tags",
				Type:                parser.ManyToMany,
				SourceTable:         "products",
				TargetTable:         "tags",
				JunctionTable:       "product_tags",
				JunctionLocalFK:     "product_id",
				JunctionReferenceFK: "tag_id",
			},
		},
	}
}

// renderWalker renders the api/field-options template against an APIContext
// derived from the given schema and returns the body. The package alias and
// import block are not included — the test asserts on the rendered template
// body only.
func renderWalker(t *testing.T, schema *parser.Schema) string {
	t.Helper()
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
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

// renderConnectionWalker renders the shared api/connection-walker template.
// The connection walker has no per-table data; it's emitted once per project.
func renderConnectionWalker(t *testing.T) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/connection-walker", nil); err != nil {
		t.Fatalf("rendering api/connection-walker: %v", err)
	}
	return buf.String()
}

func TestWalker_PerTableEmissionShape(t *testing.T) {
	out := renderWalker(t, walkerFixtureSchema())

	// Every table has both an entry-point and a recursive walker.
	wantEntryPoints := []string{
		"func productFieldOptionsFromContext(ctx context.Context) *models.ProductFieldOptions",
		"func productFieldOptionsFromCollected(opCtx *graphql.OperationContext, fields []graphql.CollectedField) *models.ProductFieldOptions",
		"func companyFieldOptionsFromContext(ctx context.Context) *models.CompanyFieldOptions",
		"func reviewFieldOptionsFromContext(ctx context.Context) *models.ReviewFieldOptions",
		"func tagFieldOptionsFromContext(ctx context.Context) *models.TagFieldOptions",
	}
	for _, sym := range wantEntryPoints {
		mustContain(t, out, sym)
	}

	// Every column on `products` has a case clause that flips the bool field.
	productColumnCases := []string{
		`case "id":`,
		"fo.ID = true",
		`case "name":`,
		"fo.Name = true",
		`case "description":`,
		"fo.Description = true",
		`case "createdAt":`,
		"fo.CreatedAt = true",
	}
	for _, sym := range productColumnCases {
		mustContain(t, out, sym)
	}

	// Per-relationship cases descend into the related table's recursive walker.
	// O2O wraps a *<Target>FieldOptions directly; O2M / M2M wrap into a
	// *<Target>RelationshipOptions per shared/_field_options.tmpl.
	mustContain(t, out, `case "company":`)
	mustContain(t, out, "fo.Company = companyFieldOptionsFromCollected(opCtx, f.Sub)")

	mustContain(t, out, `case "reviews":`)
	mustContain(t, out, "fo.Reviews = &models.ReviewRelationshipOptions{")
	mustContain(t, out, "FieldOptions: reviewFieldOptionsFromCollected(opCtx, f.Sub),")

	mustContain(t, out, `case "tags":`)
	mustContain(t, out, "fo.Tags = &models.TagRelationshipOptions{")
	mustContain(t, out, "FieldOptions: tagFieldOptionsFromCollected(opCtx, f.Sub),")

	// Entry point routes through the shared connection unwrapper so root
	// Connection-shaped queries descend edges → node before walking. The
	// helper takes the table-specific envelope type names as args, so the
	// per-table emission carries the matching `<T>Connection` / `<T>ListResult`
	// literals.
	mustContain(t, out, `entityFields, wantsRows := unwrapConnectionFields(ctx, fields, "ProductConnection", "ProductListResult")`)
}

func TestWalker_DeeplyNestedFixtureQuery(t *testing.T) {
	// "Deeply-nested" coverage at the codegen layer: with a fixture that
	// touches O2O, O2M, and M2M relationships, the rendered walker must emit
	// a `case` for every relationship that recurses into the related table's
	// own walker (`<target>FieldOptionsFromCollected`). The runtime regression
	// guard against mis-walked selection sets is the §16.8 E2E example,
	// which exercises a real GraphQL document end-to-end.
	out := renderWalker(t, walkerFixtureSchema())

	// O2O: company → CompanyFieldOptions (single pointer, no wrapper)
	mustContain(t, out, "fo.Company = companyFieldOptionsFromCollected(opCtx, f.Sub)")

	// O2M: reviews → ReviewRelationshipOptions with FieldOptions inside
	mustContain(t, out, "fo.Reviews = &models.ReviewRelationshipOptions{")
	mustContain(t, out, "FieldOptions: reviewFieldOptionsFromCollected(opCtx, f.Sub),")

	// M2M: tags → TagRelationshipOptions with FieldOptions inside
	mustContain(t, out, "fo.Tags = &models.TagRelationshipOptions{")
	mustContain(t, out, "FieldOptions: tagFieldOptionsFromCollected(opCtx, f.Sub),")

	// Each related table's own walker is also present (not just referenced) —
	// so a deeply-nested query that recursed into `company { name }` or
	// `reviews { rating }` would resolve correctly.
	mustContain(t, out, "func companyFieldOptionsFromCollected(opCtx *graphql.OperationContext, fields []graphql.CollectedField) *models.CompanyFieldOptions")
	mustContain(t, out, "func reviewFieldOptionsFromCollected(opCtx *graphql.OperationContext, fields []graphql.CollectedField) *models.ReviewFieldOptions")
	mustContain(t, out, "func tagFieldOptionsFromCollected(opCtx *graphql.OperationContext, fields []graphql.CollectedField) *models.TagFieldOptions")
}

func TestWalker_FragmentAndDirectiveCoverage(t *testing.T) {
	// Fragment and directive handling is delegated to gqlgen's CollectFieldsCtx
	// / CollectFields built-ins — they flatten named + inline fragments, apply
	// `@skip` / `@include`, and resolve aliases transparently (PRD §26.5.2).
	// The codegen-layer guarantee is that the walker calls those helpers with
	// the correct OperationContext and selection-set args; doing so means
	// fragments and directives are handled correctly by construction.
	//
	// Runtime verification of fragment / directive expansion against a real
	// gqlgen-parsed document is exercised in the §16.8 E2E example — there's
	// no value in re-asserting gqlgen's own contract here.
	out := renderWalker(t, walkerFixtureSchema())

	// Entry point uses CollectFieldsCtx (which feeds GetOperationContext +
	// CollectFields under the hood, including fragment / directive handling).
	mustContain(t, out, "graphql.CollectFieldsCtx(ctx, nil)")
	mustContain(t, out, "graphql.GetOperationContext(ctx)")

	// gqlgen resolves the alias but keeps one CollectedField per
	// alias, so a selection set naming one field twice reaches the walker
	// twice. Every walker loop must therefore run over the coalesced view,
	// where each name appears once carrying the union of every alias's
	// sub-selections — otherwise the last alias wins and the others render
	// from columns the SELECT never fetched.
	mustContain(t, out, "for _, f := range coalesceAliases(opCtx, fields) {")

	// Recursive descent still uses CollectFields with the threaded
	// OperationContext — the same call CollectFieldsCtx makes, so the same
	// fragment / directive expansion applies one level down. The call is
	// made once per field *name* inside the shared coalesceAliases
	// helper, and the per-table relationship arms consume its result as
	// `f.Sub`, so it is asserted against the shared emission.
	mustContain(t, renderConnectionWalker(t), "graphql.CollectFields(opCtx, f.Selections, nil)")
}

func TestWalker_CompletenessLint_NegativeTest(t *testing.T) {
	// Build a TableContext with three columns and an APITableContext whose
	// Fields slice deliberately omits one — the lint must detect the
	// missing case and return a descriptive error naming the column and
	// table.
	tc := gen.TableContext{
		TableName: "products",
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID"},
			{Name: "name", FieldName: "Name"},
			{Name: "description", FieldName: "Description"},
		},
	}
	at := gen.APITableContext{
		StructName: "Product",
		Fields: []gen.APIFieldContext{
			{SQLName: "id", GoFieldName: "ID", Readable: true},
			{SQLName: "name", GoFieldName: "Name", Readable: true},
			// "description" deliberately omitted — must trigger lint
		},
		// Carried because production always resolves one; without it
		// the fixture would trip the row-identity direction of the lint instead
		// of the missing-case direction these cases are about.
		RowIdentityFields: []string{"ID"},
	}
	err := gen.ValidateAPIWalkerCompleteness(gen.APIEntityFromTable(tc), at, nil)
	if err == nil {
		t.Fatal("expected ValidateAPIWalkerCompleteness to error on missing column")
	}
	msg := err.Error()
	if !strings.Contains(msg, "products") {
		t.Errorf("error must name the table; got %q", msg)
	}
	if !strings.Contains(msg, "description") {
		t.Errorf("error must name the missing column; got %q", msg)
	}

	// Same check for relationships — drop one from the APITableContext.
	tcRel := gen.TableContext{
		TableName: "products",
		Columns:   []gen.ColumnContext{{Name: "id", FieldName: "ID"}},
		Relationships: []gen.RelationshipContext{
			{Name: "company", TargetStructName: "Company"},
			{Name: "reviews", TargetStructName: "Review"},
		},
	}
	atRel := gen.APITableContext{
		StructName: "Product",
		Fields:     []gen.APIFieldContext{{SQLName: "id", GoFieldName: "ID", Readable: true}},
		Relationships: []gen.APIRelationshipContext{
			{SQLName: "company"},
			// "reviews" deliberately omitted
		},
		RowIdentityFields: []string{"ID"},
	}
	err = gen.ValidateAPIWalkerCompleteness(gen.APIEntityFromTable(tcRel), atRel, map[string]bool{"Company": true, "Review": true})
	if err == nil {
		t.Fatal("expected ValidateAPIWalkerCompleteness to error on missing relationship")
	}
	msg = err.Error()
	if !strings.Contains(msg, "products") {
		t.Errorf("error must name the table; got %q", msg)
	}
	if !strings.Contains(msg, "reviews") {
		t.Errorf("error must name the missing relationship; got %q", msg)
	}
}

// TestWalker_CompletenessLint_Relationships covers the relationship half of
// the walker lint in both directions (PRD §26.5.2, §26.10): a
// relationship into an exposed entity needs a case, and one into a hidden
// entity must not have one — the object type has no field for it.
func TestWalker_CompletenessLint_Relationships(t *testing.T) {
	src := gen.APIEntityFromTable(gen.TableContext{
		TableName:     "products",
		Columns:       []gen.ColumnContext{{Name: "id", FieldName: "ID"}},
		Relationships: []gen.RelationshipContext{{Name: "reviews", TargetStructName: "Review"}},
	})
	tests := []struct {
		name    string
		exposed bool
		hasCase bool
		wantErr string
	}{
		{"exposed with case", true, true, ""},
		{"exposed without case", true, false, `has no case for relationship "reviews"`},
		{"hidden without case", false, false, ""},
		{"hidden with case", false, true, `has a case for relationship "reviews" into Review, which is not on the API`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := gen.APITableContext{
				StructName:        "Product",
				Fields:            []gen.APIFieldContext{{SQLName: "id", GoFieldName: "ID", Readable: true}},
				RowIdentityFields: []string{"ID"},
			}
			if tt.hasCase {
				at.Relationships = []gen.APIRelationshipContext{{SQLName: "reviews"}}
			}
			err := gen.ValidateAPIWalkerCompleteness(src, at, map[string]bool{"Review": tt.exposed})
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateAPIWalkerCompleteness() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateAPIWalkerCompleteness() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestWalker_ConnectionUnwrap(t *testing.T) {
	// Single shared `unwrapConnectionFields` helper emitted once across the
	// whole project — graph/connection_walker_gen.go. The helper descends
	// both envelope shapes the curated surface emits: the Relay Connection
	// (`{ edges { node { real fields } } }`) and the offset ListResult
	// (`{ items { real fields } totalCount … }`), so a root query like
	// `products { edges { node { name } } }` or
	// `productList { items { name } totalCount }` produces a FieldOptions
	// with `Name: true` after the entry point routes through it.
	//
	// Each branch is gated on the request's parent GraphQL type
	// matching the table's `<T>Connection` / `<T>ListResult` envelope name
	// (passed in by the per-table entry point), so a column or relationship
	// literally named `edges` or `items` on the bare entity surface is no
	// longer misread as the envelope shape.
	out := renderConnectionWalker(t)

	mustContain(t, out, "func unwrapConnectionFields(ctx context.Context, fields []graphql.CollectedField, connectionType, listResultType string) ([]graphql.CollectedField, bool)")
	mustContain(t, out, "opCtx := graphql.GetOperationContext(ctx)")
	mustContain(t, out, "graphql.GetFieldContext(ctx)")
	mustContain(t, out, "def.Type.Name()")
	mustContain(t, out, "if parentType != connectionType && parentType != listResultType {")
	mustContain(t, out, `case "edges":`)
	mustContain(t, out, "if parentType != connectionType {")
	mustContain(t, out, `if e.Name == "node"`)
	mustContain(t, out, `case "items":`)
	mustContain(t, out, "if parentType != listResultType {")
	// Both envelope sites *union* rather than assign, and both loops
	// run over the coalesced view. `a: edges { node { email } } b: edges { node
	// { name } }` reaches this helper as two entries both named "edges"; an
	// assignment would keep whichever the loop visited last and answer the
	// other alias from an unfetched column.
	mustContain(t, out, "for _, f := range coalesceAliases(opCtx, fields) {")
	mustContain(t, out, "for _, e := range coalesceAliases(opCtx, f.Sub) {")
	mustContain(t, out, "entityFields = append(entityFields, e.Sub...)")
	mustContain(t, out, "entityFields = append(entityFields, f.Sub...)")
	mustNotContain(t, out, "entityFields = graphql.CollectFields(")

	// Fall-through: when the parent type matches neither envelope (Get
	// queries) the helper returns the input unchanged so the per-table walker
	// sees the real field selections directly — and reports that the request
	// wants rows, because a bare entity selection always does.
	mustContain(t, out, "return fields, true")

	// Per-table walker entry points must route through this helper with the
	// table-specific envelope names — a regression here (e.g. passing the
	// wrong type name, or dropping the gate) would silently break Connection
	// or List queries, or reintroduce the envelope misread for entity-level
	// `edges` / `items` field names.
	walkerOut := renderWalker(t, walkerFixtureSchema())
	mustContain(t, walkerOut, `unwrapConnectionFields(ctx, fields, "ProductConnection", "ProductListResult")`)
	mustContain(t, walkerOut, `unwrapConnectionFields(ctx, fields, "CompanyConnection", "CompanyListResult")`)
}

// TestWalker_EnvelopeGatingNamedItemsRelationship pins envelope gating: when a managed
// table has a relationship literally named `items` (collides with the offset
// ListResult envelope key), the per-table walker must still emit a `case` for
// the relationship and route it through the related table's recursive walker
// — the shared `unwrapConnectionFields` helper's `items` branch is gated on
// the parent GraphQL type being `<T>ListResult`, so the relationship case
// fires unchanged on a bare-entity Get-by-PK selection.
func TestWalker_EnvelopeGatingNamedItemsRelationship(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "carts",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
				},
			},
			{
				Name: "cart_items",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "cart_id", Type: "uuid", Nullable: false},
					{Name: "quantity", Type: "integer", Nullable: false},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "items",
				Type:        parser.OneToMany,
				SourceTable: "carts",
				TargetTable: "cart_items",
				FKColumn:    "cart_id",
			},
		},
	}
	out := renderWalker(t, schema)

	// The relationship literally named `items` must produce its own case
	// clause in the carts walker — envelope gating guards against the helper
	// swallowing it as an envelope key, but the per-table walker itself must
	// still emit the case so the relationship resolves when selected.
	mustContain(t, out, `case "items":`)
	mustContain(t, out, "fo.Items = &models.CartItemRelationshipOptions{")
	mustContain(t, out, "FieldOptions: cartItemFieldOptionsFromCollected(opCtx, f.Sub),")

	// The cart entry point must pass `CartConnection` / `CartListResult` as
	// the envelope names — the gating contract. Without the gate the
	// `items` relationship would be mis-walked as a ListResult envelope on a
	// `cart(id) { items { quantity } }` query.
	mustContain(t, out, `unwrapConnectionFields(ctx, fields, "CartConnection", "CartListResult")`)
}

// TestWalker_SubCategorizedRelationships pins the per-relationship
// walker emission: when a parent declares multiple relationships into the same
// child table distinguished only by `filter:` (PRD §13.7.1), each relationship
// MUST surface as its own `case "<field>":` clause in the per-table walker AND
// its own selectable field on the parent's GraphQL type. The relationship
// dedup relaxation made multi-entry contexts structurally valid; the walker /
// schema-generator contracts are what cash that out at the GraphQL surface.
//
// The fixture mirrors PRD §13.7.1's asset / documents example: three
// relationships on `assets` (`PrimaryDocument` o2o + `Attachments` o2m +
// `Invoices` o2m) all targeting `documents.entity_id` with distinct
// `entity_type` discriminators. The walker / schema generator must surface
// three independent fields per the relationship `name:` values — no
// dedup-by-target collapses them.
func TestWalker_SubCategorizedRelationships(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "assets",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "title", Type: "text", Nullable: false},
				},
			},
			{
				Name: "documents",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "entity_id", Type: "uuid", Nullable: false},
					{Name: "entity_type", Type: "text", Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	in.Config.Tables["assets"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "PrimaryDocument", Type: "o2o", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.primary'"},
			{Name: "Attachments", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.attachment'"},
			{Name: "Invoices", Type: "o2m", Table: "documents", FK: "entity_id", Filter: "entity_type = 'asset.invoice'"},
		},
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ModelsPackage = "models"
	apiCtx.ModelsImportPath = "example.com/foo/gen"
	apiCtx.ClientName = "Client"

	tmpl := loadAPIAllTemplates(t)
	var walkerBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&walkerBuf, "api/field-options", apiCtx); err != nil {
		t.Fatalf("rendering api/field-options: %v", err)
	}
	walker := walkerBuf.String()

	// Each relationship's GraphQL field name (camelCased per default casing) must
	// produce its own case clause. Dedup-by-target would collapse all three into
	// a single `case "document":` — explicitly disallowed by PRD §13.7.1.
	wantWalkerCases := []string{
		`case "primaryDocument":`,
		`case "attachments":`,
		`case "invoices":`,
	}
	for _, sym := range wantWalkerCases {
		mustContain(t, walker, sym)
	}

	// O2O wraps the target FieldOptions directly; O2M wraps into a
	// DocumentRelationshipOptions — the same shared wrapper struct is reused
	// across both Attachments and Invoices (one struct per target, not per
	// relationship — pinned upstream in TestBuildTableContexts_SubCategorizedRelationships).
	mustContain(t, walker, "fo.PrimaryDocument = documentFieldOptionsFromCollected(opCtx, f.Sub)")
	mustContain(t, walker, "fo.Attachments = &models.DocumentRelationshipOptions{")
	mustContain(t, walker, "fo.Invoices = &models.DocumentRelationshipOptions{")

	// Walker-completeness lint (PRD §26.5.2) holds end-to-end: the per-table
	// APITableContext.Relationships must carry one entry per sub-categorized
	// relationship, otherwise the lint would error on missing cases.
	var assetTable *gen.APITableContext
	for i := range apiCtx.Tables {
		if apiCtx.Tables[i].SQLTable == "assets" {
			assetTable = &apiCtx.Tables[i]
			break
		}
	}
	if assetTable == nil {
		t.Fatal("assets APITableContext not found")
	}
	var assetSrc *gen.TableContext
	for i := range tables {
		if tables[i].TableName == "assets" {
			assetSrc = &tables[i]
			break
		}
	}
	if assetSrc == nil {
		t.Fatal("assets TableContext not found")
	}
	exposed := make(map[string]bool, len(tables))
	for i := range tables {
		exposed[tables[i].StructName] = true
	}
	if err := gen.ValidateAPIWalkerCompleteness(gen.APIEntityFromTable(*assetSrc), *assetTable, exposed); err != nil {
		t.Errorf("walker-completeness lint failed: %v", err)
	}

	// Schema generator must also emit one selectable field per relationship.
	var schemaBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&schemaBuf, "api/table-schema", *assetTable); err != nil {
		t.Fatalf("rendering api/table-schema: %v", err)
	}
	schema17 := schemaBuf.String()
	mustContain(t, schema17, "primaryDocument: Document")
	mustContain(t, schema17, "attachments: [Document!]!")
	mustContain(t, schema17, "invoices: [Document!]!")
}

func TestWalker_UnselectedRelationshipReturnsNil(t *testing.T) {
	// "Selected ⇒ available, unselected ⇒ nil" contract (PRD §26.5.2): a
	// relationship that is not selected at runtime stays nil on the produced
	// FieldOptions — the parent client call does not load it.
	//
	// The walker template enforces this by initializing FieldOptions with
	// `&<pkg>.<Table>FieldOptions{}` (zero value — every relationship pointer
	// nil) and only assigning a relationship pointer inside the matching
	// `case` clause. A relationship absent from the request's selection set
	// never enters its case and stays nil.
	out := renderWalker(t, walkerFixtureSchema())

	// Initialization is the zero value — no eager relationship assignment.
	mustContain(t, out, "fo := &models.ProductFieldOptions{}")

	// Each relationship is assigned exclusively inside its case clause —
	// search for `fo.Company =` only appears once (inside the case clause),
	// never as a top-level eager assignment.
	if got := strings.Count(out, "fo.Company ="); got != 1 {
		t.Errorf("expected exactly one assignment to fo.Company (inside its case clause); got %d\n%s", got, out)
	}
	if got := strings.Count(out, "fo.Reviews ="); got != 1 {
		t.Errorf("expected exactly one assignment to fo.Reviews (inside its case clause); got %d\n%s", got, out)
	}
	if got := strings.Count(out, "fo.Tags ="); got != 1 {
		t.Errorf("expected exactly one assignment to fo.Tags (inside its case clause); got %d\n%s", got, out)
	}
}
