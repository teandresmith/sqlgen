package gen_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// apiTestInput builds a GenerateInput plus a RootConfig with api.graphql
// enabled. Callers may further mutate the returned config before calling
// BuildAPIContext.
func apiTestInput(t *testing.T, schema *parser.Schema) *gen.GenerateInput {
	t.Helper()
	in := testInput(schema)
	in.Config.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{
			Enabled:     true,
			SchemaDir:   "./graph",
			ResolverDir: "./graph",
			Package:     "graph",
			FieldCasing: config.FieldCasingCamel,
		},
	}
	return in
}

// apiTestSchema returns a small fixture covering: PK + nullable column +
// JSONB + numeric + timestamp.
//
// Postgres SQL types are spelled out (int4 / integer instead of int) so the
// gotype resolver picks them up — the resolver's default mapping does not
// include the bare alias `int`.
func apiTestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false, Comment: "human-readable product name"},
					{Name: "description", Type: "text", Nullable: true},
					{Name: "price", Type: "numeric", Nullable: false},
					{Name: "stock", Type: "integer", Nullable: false},
					{Name: "metadata", Type: "jsonb", Nullable: true},
					{Name: "created_at", Type: "timestamp", Nullable: false},
				},
			},
		},
	}
}

func loadAPISchemaTemplate(t *testing.T) *template.Template {
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

func renderAPITableSchema(t *testing.T, ctx gen.APITableContext) string {
	t.Helper()
	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/table-schema", ctx); err != nil {
		t.Fatalf("rendering api/table-schema: %v", err)
	}
	return buf.String()
}

func renderAPISharedSchema(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/shared-schema", ctx); err != nil {
		t.Fatalf("rendering api/shared-schema: %v", err)
	}
	return buf.String()
}

func TestGraphQLSchema_emitsExpectedTypesPerTable(t *testing.T) {
	in := apiTestInput(t, apiTestSchema())

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}

	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext returned nil")
	}
	if len(apiCtx.Tables) != 1 {
		t.Fatalf("Tables: got %d, want 1", len(apiCtx.Tables))
	}

	out := renderAPITableSchema(t, apiCtx.Tables[0])

	mustContain(t, out, "type Product {")
	// `uuid` resolves to `uuid.UUID` with no configuration (PRD §7.2), which
	// the registry binds to the UUID scalar. `ID!` is the PK rendering for a
	// PK that resolves to a Go `string` — see TestGraphQLSchema_PKByGoType.
	mustContain(t, out, "id: UUID!")
	mustContain(t, out, "name: String!")
	mustContain(t, out, "description: String")
	// numeric/decimal default binding is float64 → Float; the Decimal binding
	// requires the consumer to opt in via overrides.types pointing at
	// shopspring/decimal — outside the scope of this fixture.
	mustContain(t, out, "price: Float!")
	mustContain(t, out, "stock: Int!")
	mustContain(t, out, "metadata: JSON") // jsonb → JSON (sqlgen-shipped)
	// time.Time is the default timestamp binding; the gqlgen-bundled `Time`
	// scalar covers it. `DateTime` only surfaces when the consumer overrides
	// the binding to types.DateTime / types.NullDateTime.
	mustContain(t, out, "createdAt: Time!")
	mustContain(t, out, "type ProductConnection {")
	mustContain(t, out, "type ProductEdge {")
	mustContain(t, out, "type ProductListResult {")
	// <T>ListResult mirrors runtime PaginateResult[T] field-for-field
	// (items, totalCount, offset, limit, hasMore) so the gqlgen.yml `models:`
	// merge (PRD §26.5.6) can bind ProductListResult → PaginateResult[Product]
	// directly without translate helpers.
	mustContain(t, out, "items:      [Product!]!")
	mustContain(t, out, "totalCount: Int!")
	mustContain(t, out, "offset:     Int!")
	mustContain(t, out, "limit:      Int!")
	mustContain(t, out, "hasMore:    Boolean!")
	mustContain(t, out, "input ProductFilter {")
	mustContain(t, out, "input CreateProductInput {")
	mustContain(t, out, "input UpdateProductInput {")
	mustContain(t, out, "extend type Query {")
	mustContain(t, out, "product(id: UUID!): Product")
	mustContain(t, out, "products(filter: ProductFilter")
	mustContain(t, out, "productList(")
	mustContain(t, out, "extend type Mutation {")
	mustContain(t, out, "createProduct(input: CreateProductInput!): Product!")
	mustContain(t, out, "updateProduct(id: UUID!, input: UpdateProductInput!): Product!")

	// Update input must include _inc / _dec for numeric columns. The
	// paired-field type is always `Int` regardless of the column's GraphQL
	// type so the seed cast `int(*input.<IncGoField>)` compiles uniformly
	// across int / float / decimal columns (IncrementOp[T].Amount is `int`).
	mustContain(t, out, "stock_inc: Int")
	mustContain(t, out, "stock_dec: Int")
	mustContain(t, out, "price_inc: Int")
	mustContain(t, out, "price_dec: Int")
}

func TestGraphQLSchema_respectsFieldCasing(t *testing.T) {
	tests := []struct {
		name      string
		casing    string
		wantField string
		wantPK    string
	}{
		{name: "camel_case", casing: config.FieldCasingCamel, wantField: "createdAt: Time!", wantPK: "id: UUID!"},
		{name: "snake_case", casing: config.FieldCasingSnake, wantField: "created_at: Time!", wantPK: "id: UUID!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, apiTestSchema())
			in.Config.API.GraphQL.FieldCasing = tt.casing

			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			out := renderAPITableSchema(t, apiCtx.Tables[0])

			mustContain(t, out, tt.wantField)
			mustContain(t, out, tt.wantPK)
		})
	}
}

// TestGraphQLSchema_digitLeadingColumns pins PRD §8.5 "Digit-Leading
// Handling" against both `field_casing` modes. GraphQL identifiers carry the
// same `/[_A-Za-z][_0-9A-Za-z]*/` constraint as Go, so a column like
// `2010_revenue` cannot appear in the emitted schema with its raw SQL name
// regardless of casing — the camel branch produces `col2010Revenue` via
// toCamelCase's digit-leading guard, and the snake branch produces
// `col_2010_revenue` via the matching guard in graphQLFieldName itself.
// Without both guards, the snake-cased schema would emit
// `2010_revenue: Float!`, which gqlgen would reject at parse time.
func TestGraphQLSchema_digitLeadingColumns(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "reports",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, Nullable: false},
					{Name: "2010_revenue", Type: "numeric", Nullable: false},
					{Name: "1st_place", Type: "text", Nullable: false},
				},
			},
		},
	}

	tests := []struct {
		name       string
		casing     string
		wantFields []string
	}{
		{
			name:   "camel_case",
			casing: config.FieldCasingCamel,
			wantFields: []string{
				// Field name in object/filter/input/sort types.
				"col2010Revenue: Float!",
				"col1stPlace: String!",
				// Sort field enum value — SCREAMING_SNAKE_CASE via the
				// digit-leading guard in screamingSnakeCase.
				"COL_2010_REVENUE",
				"COL_1ST_PLACE",
			},
		},
		{
			name:   "snake_case",
			casing: config.FieldCasingSnake,
			wantFields: []string{
				"col_2010_revenue: Float!",
				"col_1st_place: String!",
				"COL_2010_REVENUE",
				"COL_1ST_PLACE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := apiTestInput(t, schema)
			in.Config.API.GraphQL.FieldCasing = tt.casing

			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			out := renderAPITableSchema(t, apiCtx.Tables[0])

			for _, want := range tt.wantFields {
				mustContain(t, out, want)
			}
			// The raw digit-leading name must NEVER appear as a bare GraphQL
			// identifier — gqlgen would reject it. The `db:` / `json:` struct
			// tags and doc-comment string literals (`"2010_revenue"`)
			// preserve the SQL name, but the GraphQL identifier surface
			// (field position `<name>:`, enum-value position `\n  <NAME>\n`)
			// must always carry the prefix.
			//
			// Anchor each leak pattern against a non-identifier byte so the
			// check is not satisfied by the prefixed form as a substring
			// (e.g., `col_2010_revenue:` would falsely satisfy a plain
			// `Contains` check on `2010_revenue:`).
			leakPatterns := []*regexp.Regexp{
				regexp.MustCompile(`(?:^|[^A-Za-z0-9_])2010_revenue:`),
				regexp.MustCompile(`(?:^|[^A-Za-z0-9_])1st_place:`),
				regexp.MustCompile(`(?:^|[^A-Za-z0-9_])2010_REVENUE(?:[^A-Za-z0-9_]|$)`),
				regexp.MustCompile(`(?:^|[^A-Za-z0-9_])1ST_PLACE(?:[^A-Za-z0-9_]|$)`),
			}
			for _, leak := range leakPatterns {
				if leak.MatchString(out) {
					t.Errorf("rendered GraphQL schema leaked raw digit-leading identifier %q (casing=%s):\n%s",
						leak.String(), tt.casing, out)
				}
			}
		})
	}
}

func TestGraphQLSchema_excludesDisabledOperations(t *testing.T) {
	t.Run("api.enabled false on table", func(t *testing.T) {
		in := apiTestInput(t, apiTestSchema())
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
		if len(apiCtx.Tables) != 0 {
			t.Fatalf("expected 0 tables when api.enabled=false, got %d", len(apiCtx.Tables))
		}
	})

	t.Run("api.operations: read_only excludes mutations", func(t *testing.T) {
		in := apiTestInput(t, apiTestSchema())
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
		out := renderAPITableSchema(t, apiCtx.Tables[0])

		// No mutation extension should be emitted (gqlgen rejects empty extensions).
		if strings.Contains(out, "extend type Mutation") {
			t.Fatalf("read_only preset must not emit Mutation extension. Got:\n%s", out)
		}
		mustContain(t, out, "extend type Query {")
	})
}

func TestGraphQLSchema_dedupesScalarDeclarations(t *testing.T) {
	// Schema with two jsonb columns -> JSON referenced twice but declared once.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "metadata", Type: "jsonb", Nullable: true},
					{Name: "extra", Type: "jsonb", Nullable: true},
				},
			},
		},
	}
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	// Count JSON entries in UsedScalars (should be exactly 1).
	count := 0
	for _, s := range apiCtx.UsedScalars {
		if s.Name == "JSON" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("JSON declared %d times in UsedScalars, want 1", count)
	}

	// And the rendered shared schema declares it once.
	shared := renderAPISharedSchema(t, apiCtx)
	if got := strings.Count(shared, "scalar JSON"); got != 1 {
		t.Fatalf("scalar JSON declared %d times in shared schema, want 1", got)
	}
}

// TestGraphQLSchema_DescriptionWiring pins the comment-description wiring: the
// type-level `"""..."""` block reads Table.Comment when non-empty, falling
// back to the existing template-rendered placeholder when empty. Manifest
// and `.graphqls` files agree on descriptions after this lands (PRD §26.4,
// PRD §30.7).
func TestGraphQLSchema_DescriptionWiring(t *testing.T) {
	t.Run("with_comment_renders_comment", func(t *testing.T) {
		schema := apiTestSchema()
		schema.Tables[0].Comment = "Storefront products. Soft-deleted on retirement."
		in := apiTestInput(t, schema)
		tables, err := gen.BuildTableContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts: %v", err)
		}
		apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
		if err != nil {
			t.Fatalf("BuildAPIContext: %v", err)
		}
		out := renderAPITableSchema(t, apiCtx.Tables[0])
		mustContain(t, out, "Storefront products. Soft-deleted on retirement.")
		if strings.Contains(out, "Product corresponds to the products table.") {
			t.Errorf("unexpected fallback placeholder present alongside comment:\n%s", out)
		}
	})

	t.Run("empty_comment_falls_back_to_placeholder", func(t *testing.T) {
		// apiTestSchema's first table has no Comment set.
		in := apiTestInput(t, apiTestSchema())
		tables, err := gen.BuildTableContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts: %v", err)
		}
		apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
		if err != nil {
			t.Fatalf("BuildAPIContext: %v", err)
		}
		out := renderAPITableSchema(t, apiCtx.Tables[0])
		mustContain(t, out, "Product corresponds to the products table.")
	})
}

// mustContain fails with a diagnostic dump of `out` when `want` is missing.
func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q\n--- output ---\n%s", want, out)
	}
}

// TestGraphQLSchema_PKByGoType pins the PRD §26.4 SQL→GraphQL mapping for
// primary keys: integer PKs surface as `Int!`, string PKs as `ID!`, uuid as
// `UUID!`. Flattening these all to `ID!` was rejected — the schema rendering follows the PRD
// row-by-row, and the resolver template handles the int/int64 binding gap
// via `pkConvert` at the model-call boundary (see TestResolvers_PKArgUses
// GqlgenDefaultBinding for the resolver-side guard).
func TestGraphQLSchema_PKByGoType(t *testing.T) {
	tests := []struct {
		name       string
		sqlType    string
		wantPKType string // expected `id: <Type>!` rendering
	}{
		// `uuid` resolves to `uuid.UUID` with no configuration (PRD §7.2), so
		// the default binding takes the registry's UUID scalar rather than the
		// spec ID. Only a PK that resolves to a Go `string` reaches `ID!`,
		// which `text_string_pk` below still covers.
		{name: "uuid_default_uuid_scalar", sqlType: "uuid", wantPKType: "UUID"},
		// PRD §26.4 row "id (PK) — Go int / int64 → Int!". Postgres maps
		// `bigint` to int64 and `integer` to int32 — both surface as `Int!`.
		{name: "bigint_int64_pk", sqlType: "bigint", wantPKType: "Int"},
		{name: "integer_int32_pk", sqlType: "integer", wantPKType: "Int"},
		// `text` PK column maps to Go `string`; PrimaryKey + string → `ID!`
		// per PRD §26.4 row "id (PK) — Go string → ID!".
		{name: "text_string_pk", sqlType: "text", wantPKType: "ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{
				Tables: []parser.Table{
					{
						Name: "items",
						Columns: []parser.Column{
							{Name: "id", Type: tt.sqlType, PrimaryKey: true, Nullable: false},
							{Name: "name", Type: "text", Nullable: false},
						},
					},
				},
			}
			in := apiTestInput(t, schema)
			tables, err := gen.BuildTableContexts(in, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts: %v", err)
			}
			apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
			if err != nil {
				t.Fatalf("BuildAPIContext: %v", err)
			}
			out := renderAPITableSchema(t, apiCtx.Tables[0])

			// Type field shape on the object type.
			mustContain(t, out, "id: "+tt.wantPKType+"!")
			// Query / mutation arg types must agree with the type field.
			mustContain(t, out, "item(id: "+tt.wantPKType+"!): Item")
			mustContain(t, out, "updateItem(id: "+tt.wantPKType+"!,")
		})
	}
}

// renderAPIComparatorTranslators renders graph/comparator_translate_gen.go's
// body so a test can assert on what the emitted functions actually reference,
// rather than on what the context says they will.
func renderAPIComparatorTranslators(t *testing.T, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/comparator-translate", ctx); err != nil {
		t.Fatalf("rendering api/comparator-translate: %v", err)
	}
	return buf.String()
}
