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
)

// pprGraphQLConfig is a minimal sqlgen.yml with the GraphQL API on.
// extraGraphQL is appended under `api.graphql` (indent its keys four spaces),
// then extraTop as top-level sections at column 0.
func pprGraphQLConfig(extraGraphQL, extraTop string) string {
	return `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    field_casing: camel_case
` + extraGraphQL + extraTop
}

// pprLoadConfig writes body to a temp sqlgen.yml and loads it through the
// production loader, so defaults are applied the way the CLI applies them.
func pprLoadConfig(t *testing.T, body string) *config.RootConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(%s) error: %v", path, err)
	}
	return cfg
}

// pprReadGenerated returns the content of the generated file whose path ends
// in suffix, failing when the run did not write one.
func pprReadGenerated(t *testing.T, res *gen.GenerateResult, suffix string) string {
	t.Helper()
	for _, f := range res.Files {
		if strings.HasSuffix(filepath.ToSlash(f), suffix) {
			b, err := os.ReadFile(f) //nolint:gosec // a file this test run just generated under its temp dir
			if err != nil {
				t.Fatalf("reading generated %s: %v", f, err)
			}
			return string(b)
		}
	}
	t.Fatalf("generate wrote no file ending in %q; files:\n%s", suffix, strings.Join(res.Files, "\n"))
	return ""
}

// pprHasLine reports whether out has a line that is exactly want once
// surrounding whitespace is trimmed, so `coupons: Coupon` does not match
// `coupons: Coupon!`.
func pprHasLine(out, want string) bool {
	return slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
		return strings.TrimSpace(l) == want
	})
}

// PRD §26.4: the GraphQL create input mirrors the Go `Create<T>Input`, with
// omittable Go fields becoming optional GraphQL fields. A NOT NULL column with
// a DB default is omittable in Go, so a client must be able to leave it out.
func TestGraphQLCreateInput_DefaultedColumnIsOptional(t *testing.T) {
	schema := parseDDLForTest(t, config.DialectPostgres, `CREATE TABLE widgets (
	id uuid PRIMARY KEY,
	name text NOT NULL,
	is_active boolean NOT NULL DEFAULT true,
	created_at timestamptz NOT NULL DEFAULT now(),
	stock integer NOT NULL
);`)
	in := apiTestInput(t, schema)
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() error: %v", err)
	}
	createInput := inputBlock(t, renderAPITableSchema(t, apiTableByName(t, apiCtx, "Widget")), "CreateWidgetInput")

	tests := []struct {
		name          string
		column        string
		wantOmittable bool
		wantField     string
	}{
		{name: "boolean with a literal default is optional", column: "is_active", wantOmittable: true, wantField: "isActive: Boolean"},
		{name: "timestamp with a function default is optional", column: "created_at", wantOmittable: true, wantField: "createdAt: Time"},
		{name: "text without a default stays required", column: "name", wantOmittable: false, wantField: "name: String!"},
		{name: "integer without a default stays required", column: "stock", wantOmittable: false, wantField: "stock: Int!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The Go side is the reference the PRD names; if it moved, the
			// GraphQL expectation below would be pinning the wrong thing.
			i := slices.IndexFunc(tables[0].CreateInputFields, func(f gen.InputFieldContext) bool {
				return f.ColumnName == tt.column
			})
			if i < 0 {
				t.Fatalf("CreateWidgetInput (Go) has no field for column %q", tt.column)
			}
			if got := tables[0].CreateInputFields[i].Omittable; got != tt.wantOmittable {
				t.Fatalf("CreateWidgetInput (Go) %s Omittable = %v, want %v", tt.column, got, tt.wantOmittable)
			}
			if !pprHasLine(createInput, tt.wantField) {
				t.Errorf("input CreateWidgetInput has no line %q:\n%s", tt.wantField, createInput)
			}
		})
	}
}

// PRD §26.5.1 / §26.5.4: `update<T>s` takes the same Update<T>Input as the
// single-row mutation, and bulk variants share its `_inc` / `_dec`
// translation. The operator values reach the sqlgenresolver M methods
// untranslated (the seeds are one-line delegations), so each M method is the
// place they must be read. The pairing guard only nil-checks `<Col>Inc`;
// dereferencing `*input.<Col>Inc` is what reading the amount looks like, and
// is exactly how the single-row method dispatches it.
func TestResolvers_BulkUpdateDispatchesIncrementOperators(t *testing.T) {
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	products := apiTableByName(t, apiCtx, "Product")
	if len(products.UpdateOps) == 0 {
		t.Fatalf("Product.UpdateOps is empty; the fixture needs an incrementable column")
	}
	resolvers := renderResolvers(t, apiCtx)
	single := extractFunc(t, resolvers, "UpdateProduct")
	bulk := extractFunc(t, resolvers, "UpdateProducts")

	for _, op := range products.UpdateOps {
		for _, field := range []struct {
			name   string
			goName string
		}{
			{name: op.SQLName + " inc", goName: op.IncGoField},
			{name: op.SQLName + " dec", goName: op.DecGoField},
		} {
			t.Run(field.name, func(t *testing.T) {
				read := "*input." + field.goName
				if !strings.Contains(single, read) {
					t.Fatalf("single-row M method does not read %s; the reference shape moved:\n%s", read, single)
				}
				if !strings.Contains(bulk, read) {
					t.Errorf("bulk update M method never reads %s, so the operator is dropped:\n%s", read, bulk)
				}
			})
		}
	}
}

// PRD §26.6 / §26.5.6: `api.graphql.max_depth` and `max_complexity` are
// generated as gqlgen server configuration. graph/server.go is one-shot, so it
// cannot follow a later config edit; the values live in the regenerated
// sqlgenresolver/limits_gen.go that server.go applies.
func TestGenerate_GraphQLServerCarriesQueryLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	t.Chdir(root)

	schema := parseDDLForTest(t, config.DialectPostgres, `CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);`)
	cfg := pprLoadConfig(t, pprGraphQLConfig("    max_depth: 3\n    max_complexity: 77\n", ""))
	if cfg.API.GraphQL.MaxDepth != 3 || cfg.API.GraphQL.MaxComplexity != 77 {
		t.Fatalf("loaded max_depth = %d, max_complexity = %d, want 3 and 77", cfg.API.GraphQL.MaxDepth, cfg.API.GraphQL.MaxComplexity)
	}

	res, err := gen.Generate(schema, cfg, "test")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	limits := pprReadGenerated(t, res, "models/graph/sqlgenresolver/limits_gen.go")
	for _, want := range []struct {
		name  string
		value string
	}{
		{name: "max_depth", value: "3"},
		{name: "max_complexity", value: "77"},
	} {
		if !regexp.MustCompile(`\b` + want.value + `\b`).MatchString(limits) {
			t.Errorf("limits_gen.go does not carry %s = %s:\n%s", want.name, want.value, limits)
		}
	}
	server := pprReadGenerated(t, res, "models/graph/server.go")
	if !strings.Contains(server, "sqlgenresolver.ApplyQueryLimits(srv)") {
		t.Errorf("server.go does not apply the generated limits:\n%s", server)
	}
}
