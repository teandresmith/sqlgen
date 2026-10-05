package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// renderGqlmodelTemplate executes a named API template against the shared
// API template set used by all api_*_test.go files. Mirrors the per-file
// helpers (renderFilterTranslate, renderComparatorTranslate, …) but is
// generic over the template name so the gqlgen-alias tests can exercise every
// template in a single test file.
func renderGqlmodelTemplate(t *testing.T, name string, ctx *gen.APIContext) string {
	t.Helper()
	tmpl := loadAPIAllTemplates(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, name, ctx); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	return buf.String()
}

// TestSeedTemplate_GqlgenAliasPrefix pins the gqlgen model alias: when
// APIContext.GqlgenModelAlias is set, every gqlgen-emitted input type
// reference in the seed body is prefixed with the alias. The reverse —
// alias unset ⇒ bare identifier — is the legacy path covered by every
// other seed test in this package.
func TestSeedTemplate_GqlgenAliasPrefix(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
	apiCtx.GqlgenModelAlias = gen.GqlgenModelImportAlias
	apiCtx.GqlgenModelImportPath = "example.com/foo/graph/model"

	out := renderGqlmodelTemplate(t, "api/seeds", apiCtx)

	// Filter input on the connection field — gqlgen-emitted, must be
	// qualified by the alias.
	mustContain(t, out, "filter *gqlmodel.ProductFilter")
	// Sort input on the list field — gqlgen-emitted slice element type.
	mustContain(t, out, "sort []*gqlmodel.ProductSort")
	// Create / Update input parameter types in the mutation seed bodies.
	mustContain(t, out, "input gqlmodel.CreateProductInput")
	mustContain(t, out, "input gqlmodel.UpdateProductInput")
	// CreateMany passes a slice of pointers to the gqlgen-emitted type.
	mustContain(t, out, "inputs []*gqlmodel.CreateProductInput")

	// Sanity: the consumer's models package is still referenced under the
	// `models` alias for the omittable-wrapped Create<T>Input the unified
	// client accepts. Both packages co-exist in the same seed file with
	// distinct aliases, which is the whole point of the sqlgen-controlled
	// gqlmodel name.
	mustContain(t, out, "*models.Product")
	mustContain(t, out, "*models.Create"+"ProductInput")
}

// TestSeedTemplate_NoAliasFallback pins the inverse: when
// GqlgenModelAlias is empty (no gqlgen.yml on disk, or module path
// unresolvable), the seed template emits bare identifier references —
// matching the golden snapshots generated without a gqlgen.yml.
func TestSeedTemplate_NoAliasFallback(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.SqlgenResolverPkgName = "sqlgenresolver"
	// Intentionally leave GqlgenModelAlias / ImportPath empty.

	out := renderGqlmodelTemplate(t, "api/seeds", apiCtx)

	if strings.Contains(out, "gqlmodel.") {
		t.Errorf("seed template must emit bare identifiers when alias is unset; got:\n%s", out)
	}
	mustContain(t, out, "filter *ProductFilter")
	mustContain(t, out, "input CreateProductInput")
}

// TestInputTranslate_GqlgenAliasPrefix pins the gqlgen model alias in the input
// translator body: the gqlgen-emitted `Create<T>Input` / `Update<T>Input`
// types passed to translateCreate/Update<T>Input are alias-qualified when
// GqlgenModelAlias is set.
func TestInputTranslate_GqlgenAliasPrefix(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.GqlgenModelAlias = gen.GqlgenModelImportAlias
	apiCtx.GqlgenModelImportPath = "example.com/foo/graph/model"

	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)

	mustContain(t, out, "func translateCreateProductInput(in gqlmodel.CreateProductInput) *models.CreateProductInput")
	mustContain(t, out, "func translateUpdateProductInput(in gqlmodel.UpdateProductInput) *models.UpdateProductInput")
	mustContain(t, out, "func hasUpdateProductSetFields(in gqlmodel.UpdateProductInput) bool")
}

// TestFilterTranslate_GqlgenAliasPrefix pins the gqlgen model alias in the filter
// translator body: `<T>Filter` is alias-qualified when set.
func TestFilterTranslate_GqlgenAliasPrefix(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.GqlgenModelAlias = gen.GqlgenModelImportAlias
	apiCtx.GqlgenModelImportPath = "example.com/foo/graph/model"

	out := renderGqlmodelTemplate(t, "api/filter-translate", apiCtx)

	mustContain(t, out, "func translateProductFilter(in *gqlmodel.ProductFilter) *models.ProductFilter")
}

// TestSortTranslate_GqlgenAliasPrefix pins the gqlgen model alias in the sort
// translator: `<T>Sort`, `<T>SortField`, and the shared `SortDirection` enum
// all live in the gqlgen model package and are alias-qualified when set.
func TestSortTranslate_GqlgenAliasPrefix(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.GqlgenModelAlias = gen.GqlgenModelImportAlias
	apiCtx.GqlgenModelImportPath = "example.com/foo/graph/model"

	out := renderGqlmodelTemplate(t, "api/sort-translate", apiCtx)

	mustContain(t, out, "func directionToSortDirection(d gqlmodel.SortDirection) sql.SortDirection")
	mustContain(t, out, "func translateProductSort(in []*gqlmodel.ProductSort) []sql.Sort")
	mustContain(t, out, "func productSortFieldToColumn(f gqlmodel.ProductSortField) string")
}

// TestComparatorTranslate_GqlgenAliasPrefix pins the gqlgen model alias in
// the comparator translator: every gqlgen-emitted scalar comparator input
// (StringComparator, IDComparator, …) is alias-qualified when set.
func TestComparatorTranslate_GqlgenAliasPrefix(t *testing.T) {
	apiCtx := translatorAPIContext(t, config.DialectPostgres)
	apiCtx.GqlgenModelAlias = gen.GqlgenModelImportAlias
	apiCtx.GqlgenModelImportPath = "example.com/foo/graph/model"

	out := renderGqlmodelTemplate(t, "api/comparator-translate", apiCtx)

	mustContain(t, out, "func translateStringComparator(in *gqlmodel.StringComparator) *comparator.String")
	mustContain(t, out, "func translateBooleanComparator(in *gqlmodel.BooleanComparator) *comparator.Bool")
	mustContain(t, out, "func translateTimeComparator(in *gqlmodel.TimeComparator) *comparator.Time")
}

// TestPopulateAPIContextResolverFields_ReadsGqlgenModelLayout pins the gqlgen
// model alias's integration point: when api.graphql is enabled and gqlgen.yml
// is on disk,
// `PopulateAPIContextResolverFields` parses the model block and writes the
// derived import path + alias onto the APIContext so the orchestrator's
// `buildAPI*File` helpers and the seeds renderer pick them up. With a
// missing gqlgen.yml the fields stay empty (fail-soft) — the resulting
// rendered files emit bare identifiers, matching the legacy path.
func TestPopulateAPIContextResolverFields_ReadsGqlgenModelLayout(t *testing.T) {
	dir := t.TempDir()
	gqlgenPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(gqlgenPath, []byte("model:\n  filename: api/types/types_gen.go\n  package: types\n"), 0o600); err != nil {
		t.Fatalf("writing gqlgen.yml: %v", err)
	}

	// Stage a temp go.mod so ReadModulePath returns a non-empty module path
	// and PopulateAPIContextResolverFields takes the gqlgen-yml branch.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	cfg := &config.RootConfig{
		Output: config.OutputConfig{
			Package: "models",
			Dir:     ".",
			Client:  &config.ClientOutputConfig{Name: "Client"},
		},
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled:      true,
				GqlgenConfig: gqlgenPath,
				ResolverDir:  "graph",
			},
		},
	}
	apiCtx := &gen.APIContext{}
	gen.PopulateAPIContextResolverFields(apiCtx, cfg, ".")

	if apiCtx.GqlgenModelImportPath != "example.com/foo/api/types" {
		t.Errorf("GqlgenModelImportPath: got %q, want %q", apiCtx.GqlgenModelImportPath, "example.com/foo/api/types")
	}
	if apiCtx.GqlgenModelAlias != gen.GqlgenModelImportAlias {
		t.Errorf("GqlgenModelAlias: got %q, want %q", apiCtx.GqlgenModelAlias, gen.GqlgenModelImportAlias)
	}
	// ResolverImportPath drives the gqlgen.yml `models:` binding for
	// category-4 scalars (UUID/NullUUID/Decimal/NullDecimal/JSON) — the
	// discovery anchor must point at <module>/<output.dir>/<resolver_dir>.
	if apiCtx.ResolverImportPath != "example.com/foo/graph" {
		t.Errorf("ResolverImportPath: got %q, want %q", apiCtx.ResolverImportPath, "example.com/foo/graph")
	}
}

func TestPopulateAPIContextResolverFields_FailsSoftWhenGqlgenYmlMissing(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	cfg := &config.RootConfig{
		Output: config.OutputConfig{
			Package: "models",
			Dir:     ".",
			Client:  &config.ClientOutputConfig{Name: "Client"},
		},
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled:      true,
				GqlgenConfig: filepath.Join(dir, "absent.yml"),
			},
		},
	}
	apiCtx := &gen.APIContext{}
	gen.PopulateAPIContextResolverFields(apiCtx, cfg, ".")

	if apiCtx.GqlgenModelImportPath != "" {
		t.Errorf("expected empty GqlgenModelImportPath when gqlgen.yml is missing, got %q", apiCtx.GqlgenModelImportPath)
	}
	if apiCtx.GqlgenModelAlias != "" {
		t.Errorf("expected empty GqlgenModelAlias when gqlgen.yml is missing, got %q", apiCtx.GqlgenModelAlias)
	}
}
