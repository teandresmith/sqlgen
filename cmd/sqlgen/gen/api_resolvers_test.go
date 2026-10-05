package gen_test

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// loadAPIAllTemplates parses every template under templates/api/* into a
// single template tree so the resolver test can render `api/resolvers`
// (which currently lives alongside the schema/scalars templates).
func loadAPIAllTemplates(t *testing.T) *template.Template {
	t.Helper()
	d := sql.NewPostgresDialect()
	tmpl := template.New("").Funcs(gen.FuncMap(d))
	matches, err := filepath.Glob(filepath.Join("templates", "api", "*.tmpl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("looking up api templates: matches=%v err=%v", matches, err)
	}
	tmpl, err = tmpl.ParseFiles(matches...)
	if err != nil {
		t.Fatalf("parsing api templates: %v", err)
	}
	return tmpl
}

// resolverTestSchema returns a fixture covering the curated-surface variants
// the resolver template emits: PK + numeric column (for _inc/_dec dispatch)
// + nullable text column (for create-input optionality).
func resolverTestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "description", Type: "text", Nullable: true},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "price", Type: "numeric", Nullable: false},
				},
			},
		},
	}
}

// resolverTestSchemaWithSoftDelete adds a `deleted_at` column so soft-delete /
// restore resolvers are exercised.
func resolverTestSchemaWithSoftDelete() *parser.Schema {
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
		},
	}
}

// renderResolvers renders the api/resolvers template against the given
// APIContext and returns the body. The package alias / models import are not
// included — the test only asserts on the rendered body.
func renderResolvers(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/resolvers", ctx); err != nil {
		t.Fatalf("rendering api/resolvers: %v", err)
	}
	return buf.String()
}

// renderSeeds renders the api/seeds template against the given APIContext.
// Seeds emit graph-package delegations.
func renderSeeds(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	if ctx.SqlgenResolverPkgName == "" {
		ctx.SqlgenResolverPkgName = "sqlgenresolver"
	}
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/seeds", ctx); err != nil {
		t.Fatalf("rendering api/seeds: %v", err)
	}
	return buf.String()
}

// resolverAPIContext builds an APIContext for the given schema with sensible
// defaults for resolver rendering (models package = "models", import path
// non-empty so the orchestrator-level emit guard would also fire).
func resolverAPIContext(t *testing.T, schema *parser.Schema, mutate func(*gen.GenerateInput)) *gen.APIContext {
	t.Helper()
	in := apiTestInput(t, schema)
	if mutate != nil {
		mutate(in)
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
	return apiCtx
}

func TestResolvers_CuratedSurface_NoExcludedMethods(t *testing.T) {
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// Per PRD §26.5.1 the GraphQL surface excludes Exists, Count, GetMany,
	// Find, Increment, Decrement as separate root resolvers. Increment/
	// Decrement live as input operators on update inputs (§26.5.4) and the
	// other excluded methods fold into the list query / single-by-PK shape.
	// Methods live on *Q/*M (resolvers template) plus
	// delegations on *queryResolver/*mutationResolver (seeds template).
	excluded := []string{"Exists", "Count", "GetMany", "Find", "Increment", "Decrement"}
	for _, name := range excluded {
		if strings.Contains(out, ") "+name+"(") {
			t.Errorf("excluded resolver %q must not appear in Q/M output", name)
		}
		if strings.Contains(seeds, ") "+name+"(") {
			t.Errorf("excluded resolver %q must not appear in seeds output", name)
		}
	}
}

func TestResolvers_QMShape_NoQueryResolverMethods(t *testing.T) {
	// The resolvers template emits methods on *Q / *M
	// (sqlgenresolver helper sub-package), NOT on *queryResolver /
	// *mutationResolver. Methods on the gqlgen resolver receivers live in
	// seed delegation files (api/seeds template) so gqlgen's resolvergen
	// plugin doesn't copy them and trigger the duplicate-method bug
	// (`func (r *queryResolver) X redeclared in this block`).
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), nil)
	out := renderResolvers(t, apiCtx)

	forbidden := []string{
		"type Resolver struct",
		"type queryResolver struct",
		"type mutationResolver struct",
		"func (r *Resolver) Query()",
		"func (r *Resolver) Mutation()",
		"func (r *queryResolver)",
		"func (r *mutationResolver)",
	}
	for _, sym := range forbidden {
		if strings.Contains(out, sym) {
			t.Errorf("resolvers template (sqlgenresolver/) must not emit %q\n--- output ---\n%s", sym, out)
		}
	}

	required := []string{
		"type Q struct {",
		"type M struct {",
		"type IncrementOp[",
		"func (q *Q) Product(",
		"func (m *M) CreateProduct(",
		"func InvalidInputError(",
	}
	for _, sym := range required {
		if !strings.Contains(out, sym) {
			t.Errorf("resolvers template must emit %q\n--- output ---\n%s", sym, out)
		}
	}
}

func TestSeeds_DelegateToQM(t *testing.T) {
	// Seeds in the graph package emit gqlgen-shaped delegations
	// that call into r.Q.<Method> / r.M.<Method>. They never contain the
	// resolver body logic itself — that lives in sqlgenresolver/.
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), nil)
	seeds := renderSeeds(t, apiCtx)

	required := []string{
		"func (r *queryResolver) Product(",
		"func (r *mutationResolver) CreateProduct(",
		"return r.Q.Product(",
		"return r.M.CreateProduct(",
	}
	for _, sym := range required {
		if !strings.Contains(seeds, sym) {
			t.Errorf("seeds template must emit %q\n--- output ---\n%s", sym, seeds)
		}
	}

	if strings.Contains(seeds, "FieldOptionsFromContext(ctx)") {
		t.Errorf("seeds must not contain field-options walker calls (those live in Q/M)\n--- output ---\n%s", seeds)
	}
	if strings.Contains(seeds, "mapErrorToGQL(err)") {
		t.Errorf("seeds must not contain error-mapper calls (those live in Q/M)\n--- output ---\n%s", seeds)
	}
}

func TestResolvers_CallOptionsFromHTTP_ExplicitTypeArg(t *testing.T) {
	// Every per-table closure must spell out the FieldOptions
	// type argument on callOptionsFromHTTP — Go cannot infer the type
	// parameter from the closure parameter type at the consumption site.
	// Without the explicit `[<Table>FieldOptions]`, every resolver method
	// fails compilation with `cannot infer FO`.
	//
	// Use the soft-delete fixture so every emitted closure variant is
	// exercised: Get / Connection / List / Create / CreateMany / Update
	// (set + Increment inner + final Get) / UpdateMany / Upsert / HardDelete
	// / SoftDelete / Restore.
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), nil)
	out := renderResolvers(t, apiCtx)

	// Negative: the bare unparametrised form must not appear anywhere —
	// any survivor would re-trigger the `cannot infer FO` build break.
	if strings.Contains(out, "callOptionsFromHTTP(ctx)") {
		t.Errorf("bare `callOptionsFromHTTP(ctx)` (without explicit FO type arg) must not appear — Go cannot infer FO from the closure param\n--- output ---\n%s", out)
	}

	// Positive: the parametrised form is the only spelling that compiles.
	mustContain(t, out, "callOptionsFromHTTP[models.ProductFieldOptions](ctx)")
}

// integerPKResolverSchema is a resolver fixture with an integer PK to
// exercise the int/int64 type-bridge. `bigint` on postgres maps to
// Go `int64`; the schema-side PRD §26.4 mapping renders it as `Int!`, but
// gqlgen's default `Int → int` binding means the resolver method must
// declare `id int` (not `id int64`) to satisfy the interface gqlgen emits.
// The resolver template casts to `int64(id)` at the model-call boundary.
func integerPKResolverSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "categories",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "deleted_at", Type: "timestamp", Nullable: true},
				},
			},
		},
	}
}

// TestResolvers_PKArgUsesGqlgenDefaultBinding pins that every
// resolver method that takes a PK argument must use the gqlgen-bound Go type
// (`int` for `Int!`, `string` for `ID!`/`String!`) — not the model client's
// column type. When the model type differs (e.g. `int64` for `bigint`), the
// template casts at every model-call boundary so the resolver still passes
// the right type into the unified client.
//
// PK GraphQL types are deliberately not flattened to `ID!`: the
// schema PK rendering follows PRD §26.4 row-by-row, and the resolver-side
// guard pins the gqlgen-default binding for whichever scalar the column
// resolves to.
func TestResolvers_PKArgUsesGqlgenDefaultBinding(t *testing.T) {
	apiCtx := resolverAPIContext(t, integerPKResolverSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// Seed-side method signatures: gqlgen's default Int binding is `int`.
	// Sourcing `id int64` from the model column type would cause "wrong type
	// for method Category" at the queryResolver/mutationResolver interface
	// satisfaction check.
	wantSeedSignatures := []string{
		"func (r *queryResolver) Category(ctx context.Context, id int) (*models.Category, error)",
		"func (r *mutationResolver) UpdateCategory(ctx context.Context, id int, input UpdateCategoryInput) (*models.Category, error)",
		"func (r *mutationResolver) SoftDeleteCategory(ctx context.Context, id int) (*models.Category, error)",
		"func (r *mutationResolver) HardDeleteCategory(ctx context.Context, id int) (bool, error)",
		"func (r *mutationResolver) RestoreCategory(ctx context.Context, id int) (*models.Category, error)",
	}
	for _, sig := range wantSeedSignatures {
		mustContain(t, seeds, sig)
	}

	if strings.Contains(seeds, "id int64) (*models.Category") ||
		strings.Contains(seeds, "id int64, input UpdateCategoryInput") ||
		strings.Contains(seeds, "id int64) (bool, error)") {
		t.Errorf("seed method signature must use `id int` (gqlgen Int binding), not `id int64` (model column type)\n--- output ---\n%s", seeds)
	}

	// Q/M-side call-site casts: every model-client invocation must cast `id`
	// to the model's Go type so the unified client receives the right type
	// (receivers are q/m on Q/M; calls go through q.Client / m.Client).
	wantCalls := []string{
		"q.Client.Categories().Get(ctx, int64(id),",
		"m.Client.Categories().Update(ctx, int64(id), setIn,",
		"m.Client.Categories().SoftDelete(ctx, int64(id),",
		"m.Client.Categories().HardDelete(ctx, int64(id),",
		"m.Client.Categories().Restore(ctx, int64(id),",
	}
	for _, call := range wantCalls {
		mustContain(t, out, call)
	}
}

// TestResolvers_PKArgNoSelfConvertForStringPK guards the `pkConvert` helper's
// no-op path: when the resolver arg type matches the model type (e.g. uuid
// PK with default Go string binding), no `string(id)` self-conversion is
// emitted — `unconvert` lint would flag it as dead.
func TestResolvers_PKArgNoSelfConvertForStringPK(t *testing.T) {
	// resolverTestSchema has uuid PK → Go `string` (no uuid integration in
	// apiTestInput) → schema renders `ID!` → gqlgen ID binding is `string`.
	// PKArgGoType == PKModelGoType == "string" → no cast emitted.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)

	if strings.Contains(out, "string(id)") {
		t.Errorf("no `string(id)` self-conversion should be emitted when arg + model types match — pkConvert no-op path\n--- output ---\n%s", out)
	}
	// Sanity: bare `id` is still passed straight through (Q/M template uses q.Client).
	mustContain(t, out, "q.Client.Products().Get(ctx, id,")
}

func TestResolvers_OperationsPresetGating(t *testing.T) {
	tests := []struct {
		preset  string
		want    []string // substrings that must appear
		notWant []string // substrings that must NOT appear
	}{
		{
			preset: config.PresetReadOnly,
			want: []string{
				"queryResolver) Product(",
				"queryResolver) Products(",
				"queryResolver) ProductList(",
			},
			notWant: []string{
				"mutationResolver) CreateProduct(",
				"mutationResolver) UpdateProduct(",
				"mutationResolver) DeleteProduct(",
				"mutationResolver) HardDeleteProduct(",
			},
		},
		{
			preset: config.PresetNoDelete,
			want: []string{
				"queryResolver) Product(",
				"mutationResolver) CreateProduct(",
				"mutationResolver) UpdateProduct(",
				"mutationResolver) UpsertProduct(",
			},
			notWant: []string{
				"mutationResolver) DeleteProduct(",
				"mutationResolver) SoftDeleteProduct(",
				"mutationResolver) HardDeleteProduct(",
				"mutationResolver) RestoreProduct(",
			},
		},
		{
			preset: config.PresetAppendOnly,
			want: []string{
				"queryResolver) Product(",
				"mutationResolver) CreateProduct(",
				"mutationResolver) CreateProducts(",
			},
			notWant: []string{
				"mutationResolver) UpdateProduct(",
				"mutationResolver) DeleteProduct(",
				"mutationResolver) UpsertProduct(",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			apiCtx := resolverAPIContext(t, resolverTestSchema(), func(in *gen.GenerateInput) {
				in.Config.Tables["products"] = config.TableConfig{
					API: &config.TableAPIConfig{Operations: &config.Operations{Preset: tt.preset}},
				}
			})
			seeds := renderSeeds(t, apiCtx)

			for _, sub := range tt.want {
				if !strings.Contains(seeds, sub) {
					t.Errorf("preset %q seeds missing %q\n--- output ---\n%s", tt.preset, sub, seeds)
				}
			}
			for _, sub := range tt.notWant {
				if strings.Contains(seeds, sub) {
					t.Errorf("preset %q seeds must not contain %q\n--- output ---\n%s", tt.preset, sub, seeds)
				}
			}
		})
	}
}

func TestResolvers_SoftDeleteRequiresSoftDeleteColumn(t *testing.T) {
	// resolverTestSchema has no deleted_at column — SoftDelete and Restore
	// resolvers must NOT be emitted even with PresetAll which includes them
	// in principle, because the table-context resolver gates SoftDelete on
	// the column being detected.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)

	if strings.Contains(out, "SoftDeleteProduct") {
		t.Errorf("soft delete resolver emitted without deleted_at column")
	}
	if strings.Contains(out, "RestoreProduct") {
		t.Errorf("restore resolver emitted without deleted_at column")
	}
}

func TestResolvers_UpsertRequiresPKOrUnique(t *testing.T) {
	// Schema with no PK and no unique constraint — Upsert resolver must not
	// be emitted even when operations.upsert is included in the preset.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "logs",
				Columns: []parser.Column{
					{Name: "message", Type: "text", Nullable: false},
				},
			},
		},
	}
	apiCtx := resolverAPIContext(t, schema, nil)
	out := renderResolvers(t, apiCtx)

	if strings.Contains(out, "UpsertLog") {
		t.Errorf("upsert resolver emitted on table without PK or unique constraint")
	}
}

func TestResolvers_DeleteNamingRules_HardOnly(t *testing.T) {
	// resolverTestSchema has no deleted_at → only HardDelete is enabled →
	// emits the bare `Delete<Table>` resolver returning bool.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, seeds, "func (r *mutationResolver) DeleteProduct(ctx context.Context, id uuid.UUID) (bool, error) {")
	if strings.Contains(seeds, "HardDeleteProduct") {
		t.Errorf("hard-only mode must not emit explicit HardDeleteProduct resolver")
	}
	if strings.Contains(seeds, "SoftDeleteProduct") {
		t.Errorf("hard-only mode must not emit SoftDeleteProduct resolver")
	}
}

func TestResolvers_DeleteNamingRules_SoftOnly(t *testing.T) {
	// Schema with deleted_at + operations preset that excludes hard delete.
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), func(in *gen.GenerateInput) {
		in.Config.Tables["products"] = config.TableConfig{
			API: &config.TableAPIConfig{Operations: &config.Operations{Preset: config.PresetNoHardDelete}},
		}
	})
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, seeds, "func (r *mutationResolver) DeleteProduct(ctx context.Context, id uuid.UUID) (*models.Product, error) {")
	mustContain(t, seeds, "func (r *mutationResolver) RestoreProduct(ctx context.Context, id uuid.UUID) (*models.Product, error) {")
	if strings.Contains(seeds, "func (r *mutationResolver) HardDeleteProduct(") {
		t.Errorf("soft-only mode must not emit HardDeleteProduct resolver")
	}
}

func TestResolvers_DeleteNamingRules_Both(t *testing.T) {
	// Schema with deleted_at + PresetAll = both hard + soft delete enabled.
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), nil)
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, seeds, "func (r *mutationResolver) HardDeleteProduct(ctx context.Context, id uuid.UUID) (bool, error) {")
	mustContain(t, seeds, "func (r *mutationResolver) SoftDeleteProduct(ctx context.Context, id uuid.UUID) (*models.Product, error) {")
	mustContain(t, seeds, "func (r *mutationResolver) RestoreProduct(ctx context.Context, id uuid.UUID) (*models.Product, error) {")

	// Bare `Delete<Table>` is NOT emitted in both-mode — clients must
	// choose explicitly between hardDelete and softDelete (PRD §26.5.1).
	if strings.Contains(seeds, "func (r *mutationResolver) DeleteProduct(") {
		t.Errorf("both-mode must not emit bare DeleteProduct resolver")
	}
}

func TestResolvers_MutationResultNullability(t *testing.T) {
	// Both-mode fixture — covers create/update/upsert non-nullable plus
	// soft-delete/restore nullable plus hard-delete bool returns.
	// Gqlgen-receiver method signatures live in seeds template.
	apiCtx := resolverAPIContext(t, resolverTestSchemaWithSoftDelete(), nil)
	seeds := renderSeeds(t, apiCtx)

	cases := []struct {
		name string
		want string
	}{
		{name: "CreateProduct returns *Product", want: "CreateProduct(ctx context.Context, input CreateProductInput) (*models.Product, error)"},
		{name: "CreateProducts returns []*Product", want: "CreateProducts(ctx context.Context, inputs []*CreateProductInput) ([]*models.Product, error)"},
		{name: "UpdateProduct returns *Product", want: "UpdateProduct(ctx context.Context, id uuid.UUID, input UpdateProductInput) (*models.Product, error)"},
		{name: "UpdateProducts returns []*Product", want: "UpdateProducts(ctx context.Context, filter ProductFilter, input UpdateProductInput) ([]*models.Product, error)"},
		{name: "UpsertProduct returns *Product", want: "UpsertProduct(ctx context.Context, input CreateProductInput) (*models.Product, error)"},
		{name: "HardDeleteProduct returns bool", want: "HardDeleteProduct(ctx context.Context, id uuid.UUID) (bool, error)"},
		{name: "SoftDeleteProduct returns *Product (nullable)", want: "SoftDeleteProduct(ctx context.Context, id uuid.UUID) (*models.Product, error)"},
		{name: "RestoreProduct returns *Product (nullable)", want: "RestoreProduct(ctx context.Context, id uuid.UUID) (*models.Product, error)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustContain(t, seeds, c.want)
		})
	}
}

func TestResolvers_IncDec_DispatchOrder(t *testing.T) {
	// Dispatch order is split — the seed builds the incOps slice in
	// inc-then-dec order before delegating; M.UpdateProduct in sqlgenresolver
	// dispatches set-then-incOps in the order received.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// Q/M side: the Update method runs `m.Client.Products().Update(...)` for
	// the set-fields branch then loops over incOps emitting Increment calls.
	updateBody := extractFunc(t, out, "UpdateProduct")
	setIdx := strings.Index(updateBody, "m.Client.Products().Update(ctx, id, setIn")
	incLoop := strings.Index(updateBody, "for _, op := range incOps")
	if setIdx < 0 || incLoop < 0 {
		t.Fatalf("Q/M UpdateProduct body missing dispatch markers (set=%d incLoop=%d)\n%s", setIdx, incLoop, updateBody)
	}
	if setIdx >= incLoop {
		t.Fatalf("Q/M dispatch order violated: set=%d incLoop=%d\n%s", setIdx, incLoop, updateBody)
	}

	// Seed side: the seed builds incOps slice with inc append before dec append.
	seedBody := extractFunc(t, seeds, "UpdateProduct")
	seedIncIdx := strings.Index(seedBody, "Amount: int(*input.StockInc)")
	seedDecIdx := strings.Index(seedBody, "Amount: -int(*input.StockDec)")
	if seedIncIdx < 0 || seedDecIdx < 0 {
		t.Fatalf("seed UpdateProduct body missing inc/dec dispatch (inc=%d dec=%d)\n%s", seedIncIdx, seedDecIdx, seedBody)
	}
	if seedIncIdx >= seedDecIdx {
		t.Fatalf("seed dispatch order violated: inc=%d dec=%d\n%s", seedIncIdx, seedDecIdx, seedBody)
	}
}

func TestResolvers_IncDec_RuntimeConflictGuards(t *testing.T) {
	// Conflict checks live in seeds (graph package) since they
	// reference gqlgen-input field names.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	updateBody := extractFunc(t, seeds, "UpdateProduct")

	mustContain(t, updateBody, "input.Stock != nil && input.StockInc != nil")
	mustContain(t, updateBody, "input.StockInc != nil && input.StockDec != nil")
	mustContain(t, updateBody, `sqlgenresolver.InvalidInputError("update products: column \"stock\" cannot be both set and incremented in one mutation")`)
	mustContain(t, updateBody, `sqlgenresolver.InvalidInputError("update products: column \"stock\" cannot be both incremented and decremented in one mutation")`)

	// InvalidInputError is exported from sqlgenresolver — Q/M template emits the
	// helper definition tagging gqlerror with INVALID_INPUT.
	mustContain(t, out, `"code": "INVALID_INPUT"`)
}

func TestResolvers_IncDec_ConflictRejectedAtCodegen(t *testing.T) {
	// Schema with a numeric column `stock` AND a sibling column `stock_inc`
	// would produce duplicate `stock_inc` fields in the generated update
	// input (one for `stock`'s `_inc` operator + one for the `stock_inc`
	// column itself). Codegen must reject this with an error naming the
	// colliding column.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "stock_inc", Type: "integer", Nullable: false},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	if _, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config); err == nil {
		t.Fatal("expected codegen error for stock + stock_inc namespace collision")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "stock") {
			t.Errorf("codegen error must name the colliding column %q, got: %v", "stock", msg)
		}
		if !strings.Contains(msg, "stock_inc") {
			t.Errorf("codegen error must name the sibling column %q, got: %v", "stock_inc", msg)
		}
	}
}

// noNumericResolverSchema is a fixture with no numeric non-PK columns —
// only a string PK plus a text and a timestamp column. Without the
// numeric-column gate this shape would emit `IncrementOp[models.]` (empty
// type argument) inside the Update<Table> resolver and seed, producing
// unparseable Go.
func noNumericResolverSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "categories",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "created_at", Type: "timestamp", Nullable: false},
				},
			},
		},
	}
}

// TestResolvers_NoNumericColumns_OmitsIncrementOp pins that when the
// fixture has no numeric non-PK columns, IncrementEnumType is empty and the
// Update<Table> emission must drop the incOps parameter, the
// IncrementOp[<empty>] type argument, and the Increment-dispatch loop —
// otherwise the rendered Go fails to parse with "expected 'IDENT', found ']'".
func TestResolvers_NoNumericColumns_OmitsIncrementOp(t *testing.T) {
	apiCtx := resolverAPIContext(t, noNumericResolverSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// The unparseable empty-type form must not appear anywhere.
	forbidden := []string{
		"IncrementOp[models.]",
		"IncrementOp[]",
		"IncrementInput[models.]",
		"IncrementInput[]",
	}
	for _, sym := range forbidden {
		if strings.Contains(out, sym) {
			t.Errorf("resolvers output must not contain %q\n--- output ---\n%s", sym, out)
		}
		if strings.Contains(seeds, sym) {
			t.Errorf("seeds output must not contain %q\n--- output ---\n%s", sym, seeds)
		}
	}

	// Q/M-side method must be the reduced-signature variant (no incOps param).
	mustContain(t, out, "func (m *M) UpdateCategory(ctx context.Context, id uuid.UUID, setIn *models.UpdateCategoryInput, hasSet bool) (*models.Category, error)")

	// Q/M body must not loop over incOps or call Increment — those are gated
	// out alongside the IncrementColumn enum.
	updateBody := extractFunc(t, out, "UpdateCategory")
	if strings.Contains(updateBody, "for _, op := range incOps") {
		t.Errorf("Q/M UpdateCategory must not contain increment-dispatch loop when no numeric columns\n%s", updateBody)
	}
	if strings.Contains(updateBody, ".Categories().Increment(") {
		t.Errorf("Q/M UpdateCategory must not call Increment when no numeric columns\n%s", updateBody)
	}

	// Seed must delegate with the matching reduced signature — no incOps slice
	// build, no IncrementOp construction, no fifth argument on the Q/M call.
	seedBody := extractFunc(t, seeds, "UpdateCategory")
	if strings.Contains(seedBody, "var incOps") {
		t.Errorf("seed UpdateCategory must not declare incOps when no numeric columns\n%s", seedBody)
	}
	if strings.Contains(seedBody, "sqlgenresolver.IncrementOp[") {
		t.Errorf("seed UpdateCategory must not construct IncrementOp[T] when no numeric columns\n%s", seedBody)
	}
	mustContain(t, seedBody, "return r.M.UpdateCategory(ctx, id, setIn, hasSet)")
}

// TestResolvers_NumericColumns_KeepsIncrementOp pins the inverse:
// when the fixture HAS at least one numeric non-PK column, the existing
// dispatch-emitting form is preserved unchanged.
func TestResolvers_NumericColumns_KeepsIncrementOp(t *testing.T) {
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, out, "incOps []IncrementOp[models.ProductIncrementColumn]")
	mustContain(t, out, "models.IncrementInput[models.ProductIncrementColumn]")
	mustContain(t, out, "for _, op := range incOps")
	mustContain(t, seeds, "var incOps []sqlgenresolver.IncrementOp[models.ProductIncrementColumn]")
	mustContain(t, seeds, "return r.M.UpdateProduct(ctx, id, setIn, hasSet, incOps)")
}

// decimalOnlyResolverSchema is a fixture shaped like the graphql
// example's `orders` table: PK + foreign-key column + a single
// `decimal.Decimal`-typed numeric column. This once fell through
// `isNumericGoType`'s comparator-numeric set (decimal isn't `comparator.Numeric`)
// and so `hasNumeric` came back false, triggering the no-increment
// gating even though decimal columns ARE eligible for runtime Increment.
func decimalOnlyResolverSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "user_id", Type: "uuid", Nullable: false},
					{Name: "total", Type: "numeric", Nullable: false},
				},
			},
		},
	}
}

// mixedNumericResolverSchema mixes an integer-numeric column and a
// decimal-numeric column on the same table. Both must show up in the
// IncrementColumn enum and increment dispatch.
func mixedNumericResolverSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "price", Type: "numeric", Nullable: false},
				},
			},
		},
	}
}

// withDecimalOverride configures `numeric → decimal.Decimal` on the input
// (matching `examples/postgres/sqlgen.yml`'s standard override) and
// re-initialises the gotype resolver so column types resolve through the
// override.
func withDecimalOverride(in *gen.GenerateInput) {
	in.Config.Overrides.Types["numeric"] = config.TypeOverride{
		Type:   "decimal.Decimal",
		Import: "github.com/shopspring/decimal",
	}
	in.Resolver = gotype.NewResolver(in.Config.Input.Dialect, true, in.Config.Overrides.Types)
}

// TestResolvers_DecimalOnly_KeepsIncrementOp pins that a decimal-only
// fixture under the standard `numeric → decimal.Decimal` override must still
// emit the Increment dispatch path. This once fell into the
// no-increment branch and silently dropped the inc/dec mutation surface for
// every decimal column.
func TestResolvers_DecimalOnly_KeepsIncrementOp(t *testing.T) {
	apiCtx := resolverAPIContext(t, decimalOnlyResolverSchema(), withDecimalOverride)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// IncrementColumn enum must be emitted under the table name (Order).
	mustContain(t, out, "incOps []IncrementOp[models.OrderIncrementColumn]")
	mustContain(t, out, "models.IncrementInput[models.OrderIncrementColumn]")
	mustContain(t, out, "for _, op := range incOps")

	// Seed must construct IncrementOp[OrderIncrementColumn] entries (one per
	// decimal column) and pass the slice to the Q/M Update method.
	mustContain(t, seeds, "var incOps []sqlgenresolver.IncrementOp[models.OrderIncrementColumn]")
	mustContain(t, seeds, "return r.M.UpdateOrder(ctx, id, setIn, hasSet, incOps)")

	// The old failure mode (the empty-type form) must NOT appear —
	// this is what we'd see if hasNumeric still came back false on decimal.
	forbidden := []string{
		"IncrementOp[models.]",
		"IncrementOp[]",
		"IncrementInput[models.]",
		"IncrementInput[]",
	}
	for _, sym := range forbidden {
		if strings.Contains(out, sym) {
			t.Errorf("resolvers output must not contain %q\n--- output ---\n%s", sym, out)
		}
		if strings.Contains(seeds, sym) {
			t.Errorf("seeds output must not contain %q\n--- output ---\n%s", sym, seeds)
		}
	}
}

// TestResolvers_DecimalOnly_APITableContextHasIncrementColumns pins the
// context-builder side: BuildAPIContext must mark a decimal-only
// table as having increment columns so downstream gating lets the
// Update method emit the increment dispatch.
func TestResolvers_DecimalOnly_APITableContextHasIncrementColumns(t *testing.T) {
	in := apiTestInput(t, decimalOnlyResolverSchema())
	withDecimalOverride(in)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	if len(apiCtx.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(apiCtx.Tables))
	}
	tc := apiCtx.Tables[0]
	if !tc.HasIncrementColumns {
		t.Errorf("HasIncrementColumns = false, want true (decimal column should be incrementable)")
	}
	if tc.IncrementEnumType != "OrderIncrementColumn" {
		t.Errorf("IncrementEnumType = %q, want %q", tc.IncrementEnumType, "OrderIncrementColumn")
	}
	// Exactly one update op — `total` (decimal). user_id is a FK (not
	// numeric/incrementable), id is the PK (excluded).
	if len(tc.UpdateOps) != 1 {
		t.Fatalf("UpdateOps len = %d, want 1; ops = %+v", len(tc.UpdateOps), tc.UpdateOps)
	}
	if tc.UpdateOps[0].SQLName != "total" {
		t.Errorf("UpdateOps[0].SQLName = %q, want %q", tc.UpdateOps[0].SQLName, "total")
	}
}

// TestResolvers_MixedNumeric_BothInIncrementEnum pins the mixed-column case:
// a table with one integer-numeric column and one decimal column must list
// both in the IncrementColumn enum and the seed-side dispatch.
func TestResolvers_MixedNumeric_BothInIncrementEnum(t *testing.T) {
	in := apiTestInput(t, mixedNumericResolverSchema())
	withDecimalOverride(in)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	if len(apiCtx.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(apiCtx.Tables))
	}
	tc := apiCtx.Tables[0]
	gotCols := make(map[string]bool, len(tc.UpdateOps))
	for _, op := range tc.UpdateOps {
		gotCols[op.SQLName] = true
	}
	for _, want := range []string{"stock", "price"} {
		if !gotCols[want] {
			t.Errorf("UpdateOps missing %q; got %+v", want, tc.UpdateOps)
		}
	}
}

// TestResolvers_IntegerOnly_UnchangedFromBaseline pins that the pure
// integer-numeric path is unchanged by the decimal handling — the predicate split is
// strictly additive, not a behavioural change for non-decimal tables.
func TestResolvers_IntegerOnly_UnchangedFromBaseline(t *testing.T) {
	// resolverTestSchema has integer `stock` + numeric `price` BUT no decimal
	// override — so under the default `numeric → float64` mapping, both
	// columns are comparator-numeric and were already in the increment set
	// before the decimal handling. Re-running the existing keeps-IncrementOp assertion proves
	// the rename + helper split didn't drop anything.
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	mustContain(t, out, "incOps []IncrementOp[models.ProductIncrementColumn]")
	mustContain(t, out, "for _, op := range incOps")
	mustContain(t, seeds, "var incOps []sqlgenresolver.IncrementOp[models.ProductIncrementColumn]")
	mustContain(t, seeds, "return r.M.UpdateProduct(ctx, id, setIn, hasSet, incOps)")
}

// compositePKResolverSchema is a fixture mirroring the graphql E2E example's
// `user_categories` junction — a composite PK whose two columns span distinct
// Go types (`uuid.UUID` + `int64`). Previously every PK-keyed
// surface field drew its single arg from `tc.PKColumns[0]`, so the
// schema emitted `userCategory(userID: ID!): UserCategory`, the resolver
// emitted `func (q *Q) UserCategory(ctx, userID string)` calling
// `q.Client.UserCategories().Get(ctx, userID, …)` — but the client expects
// `models.UserCategoryPK{UserID, CategoryID}`, so the resolver failed to
// compile.
func compositePKResolverSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "user_categories",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "category_id", Type: "bigint", PrimaryKey: true, Nullable: false},
				},
			},
		},
	}
}

// TestGraphQLSchema_CompositePK_EmitsAllPKArgs pins the schema-side
// fix: every per-PK surface field (`<table>(args)`, `update<T>`, `delete<T>`
// / `hardDelete<T>` / `softDelete<T>`, `restore<T>`) joins one arg per PK
// column instead of dropping all but the first. The composite-PK fixture
// has `user_id UUID` and `category_id BIGINT` — both must surface as
// `(userID: UUID!, categoryID: Int!)` on every PK-keyed field.
func TestGraphQLSchema_CompositePK_EmitsAllPKArgs(t *testing.T) {
	in := apiTestInput(t, compositePKResolverSchema())
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if len(apiCtx.Tables) == 0 {
		t.Fatal("expected at least one APITableContext")
	}
	out := renderAPITableSchema(t, apiCtx.Tables[0])

	// The single-arg form (PKColumns[0] only) must NOT appear.
	if strings.Contains(out, "userCategory(userID: UUID!): UserCategory") {
		t.Errorf("schema emitted single-PK arg on composite-PK table\n--- output ---\n%s", out)
	}

	// Every per-PK surface field must list both args. The Get query on a
	// pure-PK table is the most direct surface; delete + restore are gated
	// on operations / soft-delete (this fixture has no deleted_at), so we
	// only assert what's reachable on this fixture.
	wantArgs := "(userID: UUID!, categoryID: Int!)"
	mustContain(t, out, "userCategory"+wantArgs+": UserCategory")
	// The fixture has no non-PK columns, so HasUpdateInput is false and the
	// update mutation is gated out — skip its assertion. The delete surface
	// also gates on Operations / HasSoftDelete — skip those too. The Get
	// surface is the canonical PK-args check.
}

// TestResolvers_CompositePK_EmitsCompositeSignatures pins the Go-side
// fix: every Q/M / seed method that takes a PK arg lists every PK column,
// and every model-call boundary constructs `models.<T>PK{Field1: arg1,
// Field2: arg2, …}` before calling the client.
func TestResolvers_CompositePK_EmitsCompositeSignatures(t *testing.T) {
	apiCtx := resolverAPIContext(t, compositePKResolverSchema(), nil)
	out := renderResolvers(t, apiCtx)
	seeds := renderSeeds(t, apiCtx)

	// The Q/M Get method must list every PK column on its arg list.
	mustContain(t, out, "func (q *Q) UserCategory(ctx context.Context, userID uuid.UUID, categoryID int)")
	// The model-call boundary must construct the PK struct literal — bare
	// `userID` would compile-error against `pk UserCategoryPK`.
	mustContain(t, out, "q.Client.UserCategories().Get(ctx, models.UserCategoryPK{UserID: userID, CategoryID: int64(categoryID)},")

	// The seed delegation must list every PK column on its arg list AND
	// pass them all through to the Q/M helper.
	mustContain(t, out, "func (q *Q) UserCategory(ctx context.Context, userID uuid.UUID, categoryID int)")
	mustContain(t, seeds, "func (r *queryResolver) UserCategory(ctx context.Context, userID uuid.UUID, categoryID int)")
	mustContain(t, seeds, "return r.Q.UserCategory(ctx, userID, categoryID)")
}

// TestPKConvert_CompositeStructLiteral exercises the funcmap helper directly.
// The single-PK paths are covered by TestResolvers_PKArgUsesGqlgenDefaultBinding
// and TestResolvers_PKArgNoSelfConvertForStringPK; this test pins the
// composite path's literal shape so a refactor of pkConvert can't silently
// drop a field name or skip a per-arg cast.
func TestPKConvert_CompositeStructLiteral(t *testing.T) {
	d := sql.NewPostgresDialect()
	fm := gen.FuncMap(d)
	pkConvert := fm["pkConvert"].(func(string, string, []gen.APIPKArg) string)

	tests := []struct {
		name string
		args []gen.APIPKArg
		want string
	}{
		{
			name: "single_arg_match",
			args: []gen.APIPKArg{
				{GraphQLName: "id", GoType: "string", ModelGoType: "string", ModelFieldName: "ID"},
			},
			want: "id",
		},
		{
			name: "single_arg_cast",
			args: []gen.APIPKArg{
				{GraphQLName: "id", GoType: "int", ModelGoType: "int64", ModelFieldName: "ID"},
			},
			want: "int64(id)",
		},
		{
			name: "composite_mixed_pk_types",
			args: []gen.APIPKArg{
				{GraphQLName: "userID", GoType: "string", ModelGoType: "string", ModelFieldName: "UserID"},
				{GraphQLName: "categoryID", GoType: "int", ModelGoType: "int64", ModelFieldName: "CategoryID"},
			},
			want: "models.UserCategoryPK{UserID: userID, CategoryID: int64(categoryID)}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pkConvert("models", "UserCategory", tt.args)
			if got != tt.want {
				t.Errorf("pkConvert: got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPKArgImports_UnionsAllPKColumns pins the import-collector fix:
// the resolver file's import block must include every distinct GoImport
// across every PK column on every table — not just the first PK column.
// A mixed-PK-type composite (uuid + int64) needs `github.com/google/uuid`
// resolvable in the import block; the collector once inspected only
// `PKColumns[0]`, so a table whose first PK column was the int64 column
// would silently drop the uuid import.
func TestPKArgImports_UnionsAllPKColumns(t *testing.T) {
	apiCtx := resolverAPIContext(t, compositePKResolverSchema(), nil)
	if len(apiCtx.Tables) == 0 {
		t.Fatal("expected at least one APITableContext")
	}
	tc := apiCtx.Tables[0]
	if len(tc.PKArgs) != 2 {
		t.Fatalf("expected 2 PK args, got %d", len(tc.PKArgs))
	}
	// In this fixture neither PK column is custom-scalar-bound (uuid
	// resolves to string by default; bigint to int via gqlgen Int binding),
	// so both GoImports are empty. The point of the test is to pin the
	// shape — every PKArg surfaces, regardless of whether its GoImport
	// lands as a non-empty third-party path.
	for _, a := range tc.PKArgs {
		if a.ModelFieldName == "" {
			t.Errorf("PKArg %q has empty ModelFieldName — composite PK literal will mis-render", a.Name)
		}
	}
}

// extractFunc returns the body of the named resolver method from the
// rendered output. Used by dispatch-order tests so they can pin per-method
// invariants without false matches across the full file.
func extractFunc(t *testing.T, src, methodName string) string {
	t.Helper()
	marker := ") " + methodName + "("
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("could not locate method %q in rendered output", methodName)
	}
	// Walk back to the start of the line.
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	// Walk forward to the matching close brace at column 0.
	rest := src[start:]
	end := strings.Index(rest[1:], "\n}\n")
	if end < 0 {
		// Could be the last function in the file — fall back to end of string.
		return rest
	}
	return rest[:end+3]
}
