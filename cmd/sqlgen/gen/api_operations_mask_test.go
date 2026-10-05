package gen_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestAPIOperationsMask_PerTable pins the headline case: a junction whose
// create/upsert mutations are suppressed from GraphQL while the Go client
// keeps generating them for a hand-written resolver to call.
func TestAPIOperationsMask_PerTable(t *testing.T) {
	in := apiTestInput(t, junctionAPISchema())
	in.Config.Tables = map[string]config.TableConfig{
		"task_labels": {
			API: &config.TableAPIConfig{
				Operations: &config.Operations{
					Create:     new(false),
					CreateMany: new(false),
					Upsert:     new(false),
				},
			},
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

	byName := map[string]gen.APITableContext{}
	for _, tc := range apiCtx.Tables {
		byName[tc.SQLTable] = tc
	}

	// Masked table: create surface gone from the schema, reads intact.
	masked := renderAPITableSchema(t, byName["task_labels"])
	mustNotContain(
		t, masked,
		"createTaskLabel(",
		"createTaskLabels(",
		"upsertTaskLabel(",
		"input CreateTaskLabelInput",
	)
	mustContainAll(
		t, masked,
		"taskLabel(taskID: UUID!, labelID: UUID!): TaskLabel",
		"deleteTaskLabel(",
	)

	// The Go client is untouched — the blessed resolver still has a method.
	for _, tc := range tables {
		if tc.TableName != "task_labels" {
			continue
		}
		if !tc.Operations.Create {
			t.Error("client Operations.Create was masked; the API mask must not reach the Go client")
		}
		if !tc.Operations.Upsert {
			t.Error("client Operations.Upsert was masked; the API mask must not reach the Go client")
		}
	}

	// A sibling table with no mask keeps its full surface.
	sibling := renderAPITableSchema(t, byName["task_assignees"])
	mustContainAll(
		t, sibling,
		"createTaskAssignee(",
		"upsertTaskAssignee(",
		"input CreateTaskAssigneeInput",
	)
}

// TestAPIOperationsMask_GlobalAndPresetAndOverride covers the resolution
// order: a global mask applies everywhere, and a table's own mask replaces
// it wholesale rather than merging.
func TestAPIOperationsMask_GlobalAndPresetAndOverride(t *testing.T) {
	in := apiTestInput(t, junctionAPISchema())
	in.Config.API.Operations = &config.Operations{Preset: config.PresetReadOnly}
	in.Config.Tables = map[string]config.TableConfig{
		"task_assignees": {
			API: &config.TableAPIConfig{
				Operations: &config.Operations{Create: new(true)},
			},
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
	byName := map[string]gen.APITableContext{}
	for _, tc := range apiCtx.Tables {
		byName[tc.SQLTable] = tc
	}

	// Global read_only mask: labels has queries, no mutations at all.
	labels := renderAPITableSchema(t, byName["labels"])
	mustNotContain(t, labels, "extend type Mutation")
	mustContain(t, labels, "extend type Query")

	// task_assignees replaces the global mask with its own, which expands
	// from the `all` preset — so it regains the full mutation surface, not
	// just `create`.
	assignees := renderAPITableSchema(t, byName["task_assignees"])
	mustContainAll(t, assignees, "createTaskAssignee(", "deleteTaskAssignee(")
}

// TestAPIOperationsMask_CannotExceedClient pins the subtractive invariant: a
// mask key set true for a method the schema does not give the client exposes
// nothing. labels has no soft-delete column, so it has no SoftDelete or
// Restore to delegate to.
func TestAPIOperationsMask_CannotExceedClient(t *testing.T) {
	in := apiTestInput(t, junctionAPISchema())
	in.Config.Tables = map[string]config.TableConfig{
		"labels": {
			API: &config.TableAPIConfig{
				Operations: &config.Operations{Preset: config.PresetReadOnly, SoftDelete: new(true), Restore: new(true)},
			},
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
	for _, tc := range apiCtx.Tables {
		if tc.SQLTable != "labels" {
			continue
		}
		out := renderAPITableSchema(t, tc)
		for _, field := range []string{"deleteLabel(", "softDeleteLabel(", "restoreLabel("} {
			if strings.Contains(out, field) {
				t.Errorf("API mask exposed %s on a table with no soft-delete column:\n%s", field, out)
			}
		}
	}
}

// TestAPIOperationsMask_UpdateTsFollowsUpdateWhere pins the update gate:
// `update<T>s(filter, input)` is backed by UpdateWhere, so the schema field,
// the *M resolver and the seed delegation are all gated on the `update_where`
// mask key that names it. Masking `update` alone leaves it in place.
func TestAPIOperationsMask_UpdateTsFollowsUpdateWhere(t *testing.T) {
	tests := []struct {
		name  string
		table config.TableConfig
		want  bool
	}{
		{
			name:  "no mask keeps the mutation",
			table: config.TableConfig{},
			want:  true,
		},
		{
			name: "API update off keeps the filter-scoped mutation",
			table: config.TableConfig{API: &config.TableAPIConfig{
				Operations: &config.Operations{Update: new(false)},
			}},
			want: true,
		},
		{
			name: "API update_where off masks the mutation",
			table: config.TableConfig{API: &config.TableAPIConfig{
				Operations: &config.Operations{UpdateWhere: new(false)},
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx := resolverAPIContext(t, apiTestSchema(), func(in *gen.GenerateInput) {
				in.Config.Tables = map[string]config.TableConfig{"products": tt.table}
			})
			for _, s := range []struct{ surface, out, marker string }{
				{"schema", renderAPITableSchema(t, apiCtx.Tables[0]), "updateProducts(filter: ProductFilter!"},
				{"resolvers", renderResolvers(t, apiCtx), ") UpdateProducts(ctx context.Context"},
				{"seeds", renderSeeds(t, apiCtx), "func (r *mutationResolver) UpdateProducts("},
			} {
				if got := strings.Contains(s.out, s.marker); got != tt.want {
					t.Errorf("%s contains %q = %v, want %v", s.surface, s.marker, got, tt.want)
				}
			}
		})
	}
}

// TestAPIOperationsMask_ListFollowsPaginate pins the `<t>List` half of Phase
// 29 decision 1: the offset query is backed by Paginate, so the schema field,
// the *Q resolver and the seed delegation are gated on the `paginate` mask key
// alone. `get_many` no longer reaches it (naming it is a config error), and
// masking the other two reads leaves it in place.
func TestAPIOperationsMask_ListFollowsPaginate(t *testing.T) {
	tests := []struct {
		name string
		mask *config.Operations
		want bool
	}{
		{name: "no mask keeps the list query", want: true},
		{name: "API get and connection off keep the list query", mask: &config.Operations{Get: new(false), Connection: new(false)}, want: true},
		{name: "API paginate off masks the list query", mask: &config.Operations{Paginate: new(false)}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx := resolverAPIContext(t, apiTestSchema(), func(in *gen.GenerateInput) {
				in.Config.Tables = map[string]config.TableConfig{
					"products": {API: &config.TableAPIConfig{Operations: tt.mask}},
				}
			})
			for _, s := range []struct{ surface, out, marker string }{
				{"schema", renderAPITableSchema(t, apiCtx.Tables[0]), "productList("},
				{"resolvers", renderResolvers(t, apiCtx), ") ProductList(ctx context.Context"},
				{"seeds", renderSeeds(t, apiCtx), "func (r *queryResolver) ProductList("},
			} {
				if got := strings.Contains(s.out, s.marker); got != tt.want {
					t.Errorf("%s contains %q = %v, want %v", s.surface, s.marker, got, tt.want)
				}
			}
		})
	}
}

// TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface pins the
// readers behind the `update<T>s` gate. A mask that keeps
// `update_where` and takes `update` and every other query and mutation away
// leaves `update<T>s(filter, input)` as the table's only API operation. It
// still needs the update input, its translator, the flat Mutation block, the
// resolver file's `context` import and the stub-rewriter root field, and
// nothing else holds those surfaces open. A set mask turns the client's
// batch-of-items UpdateMany off (fail closed), so a reader left on it drops
// the surface here rather than riding on the client's own UpdateMany.
func TestAPIOperationsMask_UpdateWhereAloneCarriesTheUpdateSurface(t *testing.T) {
	const updateWhereOnlyMask = `
    api:
      operations:
        create: false
        create_many: false
        update: false
        upsert: false
        soft_delete: false
        hard_delete: false
        restore: false
        get: false
        paginate: false
        connection: false
`
	loadConfig := func(t *testing.T, products string) *config.RootConfig {
		t.Helper()
		body := `version: v1
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
tables:
  products:` + products
		cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}
		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return cfg
	}
	schema := func(extra ...parser.Column) *parser.Schema {
		return &parser.Schema{Tables: []parser.Table{{
			Name: "products", Schema: "public",
			Columns: append([]parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
				{Name: "name", Type: "text"},
				{Name: "stock", Type: "integer"},
			}, extra...),
		}}}
	}
	buildAPI := func(t *testing.T, s *parser.Schema, products string) (*gen.APIContext, error) {
		t.Helper()
		in := apiTestInput(t, s)
		in.Config.Tables = loadConfig(t, products).Tables
		tables, err := gen.BuildTableContexts(in, nil)
		if err != nil {
			t.Fatalf("BuildTableContexts: %v", err)
		}
		return gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	}

	t.Run("generated files", func(t *testing.T) {
		// The resolver and input-translator files are emitted only when the
		// consumer's module path resolves, which is read from the working
		// directory's go.mod.
		mod := t.TempDir()
		if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/probe\n\ngo 1.27\n"), 0o600); err != nil {
			t.Fatalf("writing go.mod: %v", err)
		}
		t.Chdir(mod)
		root := t.TempDir()
		if _, err := gen.GenerateInto(schema(), loadConfig(t, updateWhereOnlyMask), "test", root); err != nil {
			t.Fatalf("GenerateInto() error: %v", err)
		}
		for _, f := range []struct {
			path string
			want []string
		}{
			{"models/graph/product_gen.graphqls", []string{
				"extend type Mutation",
				"updateProducts(filter: ProductFilter!, input: UpdateProductInput!)",
				"input UpdateProductInput",
			}},
			{"models/graph/input_translate_gen.go", []string{"func translateUpdateProductInput("}},
			{"models/graph/sqlgenresolver/resolvers_gen.go", []string{") UpdateProducts(ctx context.Context", "\t\"context\"\n"}},
		} {
			b, err := os.ReadFile(filepath.Join(root, f.path)) //nolint:gosec // reading back a t.TempDir the test just generated into
			if err != nil {
				t.Errorf("reading %s: %v", f.path, err)
				continue
			}
			for _, want := range f.want {
				if !strings.Contains(string(b), want) {
					t.Errorf("%s does not contain %q", f.path, want)
				}
			}
		}
	})

	t.Run("stub-rewriter root field", func(t *testing.T) {
		apiCtx, err := buildAPI(t, schema(), updateWhereOnlyMask)
		if err != nil {
			t.Fatalf("BuildAPIContext: %v", err)
		}
		_, mutations := gen.CollectSqlgenRootFields(apiCtx)
		if !slices.ContainsFunc(mutations, func(f gen.APIRootField) bool { return f.GoName == "UpdateProducts" }) {
			t.Errorf("CollectSqlgenRootFields() mutations = %v, want UpdateProducts", mutations)
		}
	})

	t.Run("masking update_where too takes the update surface", func(t *testing.T) {
		apiCtx, err := buildAPI(t, schema(), updateWhereOnlyMask+"        update_where: false\n")
		if err != nil {
			t.Fatalf("BuildAPIContext: %v", err)
		}
		at := apiCtx.Tables[0]
		if at.Operations.UpdateWhere || at.Operations.UpdateMany {
			t.Errorf("Operations.UpdateWhere, UpdateMany = %v, %v, want false, false", at.Operations.UpdateWhere, at.Operations.UpdateMany)
		}
		if at.HasUpdateInput {
			t.Error("HasUpdateInput = true, want false: no mutation consumes the update input")
		}
		if out := renderAPITableSchema(t, at); strings.Contains(out, "updateProducts(") || strings.Contains(out, "input UpdateProductInput") {
			t.Errorf("schema still carries the masked update surface:\n%s", out)
		}
	})
}
