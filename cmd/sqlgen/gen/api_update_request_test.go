package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// The update mutations' division of labor: the gqlgen-owned seed delegates in
// one line, the regenerated graph-package translator carries the GraphQL
// input across as an Update<T>Request, and the sqlgenresolver M methods own
// the `_inc` / `_dec` rules. A seed is written once, so anything it decided
// would be frozen at the columns the table had on that first run.

// upfSeedBody returns the statements of a rendered seed method, without its
// signature or braces, trimmed.
func upfSeedBody(t *testing.T, seeds, method string) string {
	t.Helper()
	fn := extractFunc(t, seeds, method)
	open := strings.Index(fn, "{\n")
	end := strings.LastIndex(fn, "\n}")
	if open < 0 || end < open {
		t.Fatalf("seed %s has no body:\n%s", method, fn)
	}
	return strings.TrimSpace(fn[open+2 : end])
}

// upfTopLevelFunc returns the rendered package-level function name, from its
// `func` line to its closing brace.
func upfTopLevelFunc(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func "+name+"(")
	if start < 0 {
		t.Fatalf("could not locate func %s in rendered output", name)
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return rest
	}
	return rest[:end+3]
}

// TestUpdateSeeds_DelegateInOneLine pins the seed shape for both update
// mutations, on a table with operators and one without: a single return that
// hands the translated request to M. The two shapes being the same is what
// lets a numeric column added later get its operators without touching the
// seed.
func TestUpdateSeeds_DelegateInOneLine(t *testing.T) {
	tests := []struct {
		name   string
		schema *parser.Schema
		single string
		bulk   string
	}{
		{
			name:   "table with incrementable columns",
			schema: resolverTestSchema(),
			single: "return r.M.UpdateProduct(ctx, id, translateUpdateProductRequest(input))",
			bulk:   "return r.M.UpdateProducts(ctx, translateProductFilter(&filter), translateUpdateProductRequest(input))",
		},
		{
			name:   "table without incrementable columns",
			schema: noNumericResolverSchema(),
			single: "return r.M.UpdateCategory(ctx, id, translateUpdateCategoryRequest(input))",
			bulk:   "return r.M.UpdateCategories(ctx, translateCategoryFilter(&filter), translateUpdateCategoryRequest(input))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiCtx := resolverAPIContext(t, tt.schema, nil)
			seeds := renderSeeds(t, apiCtx)
			table := apiCtx.Tables[0]
			for _, c := range []struct {
				method string
				want   string
			}{
				{method: "Update" + table.StructName, want: tt.single},
				{method: "Update" + table.StructNamePlural, want: tt.bulk},
			} {
				if got := upfSeedBody(t, seeds, c.method); got != c.want {
					t.Errorf("seed %s body = %q, want %q", c.method, got, c.want)
				}
			}
		})
	}
}

// TestBulkUpdateResolver_OperatorsRunInOneTransaction pins the bulk method's
// operator path: UpdateWhere reports the matched rows, every operator is
// applied to each of them by key, and the rows are re-read in ONE GetMany
// over their keys — so relationship loading stays batched — with the cache
// bypassed but query hooks left running, all inside one WithTx. An
// operator-free call stays UpdateWhere alone.
func TestBulkUpdateResolver_OperatorsRunInOneTransaction(t *testing.T) {
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	body := extractFunc(t, renderResolvers(t, apiCtx), "UpdateProducts")

	mustContain(t, body, "func (m *M) UpdateProducts(ctx context.Context, filter *models.ProductFilter, input UpdateProductRequest) ([]*models.Product, error)")
	mustContain(t, body, "if len(incOps) == 0 {")

	txIdx := strings.Index(body, `m.Client.WithTx(ctx, "update_product_where_inc", func(ctx context.Context) error {`)
	if txIdx < 0 {
		t.Fatalf("bulk method opens no transaction for the operator path:\n%s", body)
	}
	tx := body[txIdx:]
	steps := []string{
		"matched, err := m.Client.Products().UpdateWhere(ctx, filter, input.Set,",
		"for _, row := range matched {",
		"m.Client.Products().Increment(ctx, row.ID,",
		"keys = append(keys, row.ID.String())",
		"res, err = m.Client.Products().GetMany(ctx, &models.GetProductsInput{",
		"Filter: &models.ProductFilter{ID: &comparator.ID{In: keys}},",
		"o.FieldOptions = fo",
		"o.SkipCache = true",
	}
	last := -1
	for _, step := range steps {
		i := strings.Index(tx, step)
		if i < 0 {
			t.Fatalf("operator path is missing %q:\n%s", step, tx)
		}
		if i < last {
			t.Errorf("operator path runs %q out of order:\n%s", step, tx)
		}
		last = i
	}
	for _, bad := range []string{"o.SkipHooks = true", ".Get(ctx, row."} {
		if strings.Contains(tx, bad) {
			t.Errorf("operator path contains %q; the read-back must keep query hooks and read every row at once:\n%s", bad, tx)
		}
	}
}

// TestBulkUpdateResolver_RowKeysFollowTheKeyShape pins the key re-read for the
// three key shapes the client's own UpdateWhere read-back distinguishes: a
// string-compared single column, a numeric single column, and a composite
// key, which goes through the filter's PKs member. Asserted on the rendered
// bulk method, so the models alias is the template's own.
func TestBulkUpdateResolver_RowKeysFollowTheKeyShape(t *testing.T) {
	tests := []struct {
		name   string
		schema *parser.Schema
		method string
		want   []string
	}{
		{
			name:   "uuid key",
			schema: resolverTestSchema(),
			method: "UpdateProducts",
			want: []string{
				"keys := make([]string, 0, len(matched))",
				"keys = append(keys, row.ID.String())",
				"Filter: &models.ProductFilter{ID: &comparator.ID{In: keys}},",
			},
		},
		{
			name: "bigint key",
			schema: &parser.Schema{Tables: []parser.Table{{
				Name: "counters",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "hits", Type: "integer"},
				},
			}}},
			method: "UpdateCounters",
			want: []string{
				"keys := make([]int64, 0, len(matched))",
				"keys = append(keys, row.ID)",
				"Filter: &models.CounterFilter{ID: &comparator.Number[int64]{In: keys}},",
			},
		},
		{
			name: "composite key",
			schema: &parser.Schema{Tables: []parser.Table{{
				Name: "tallies",
				Columns: []parser.Column{
					{Name: "owner_id", Type: "uuid", PrimaryKey: true},
					{Name: "slot", Type: "bigint", PrimaryKey: true},
					{Name: "hits", Type: "integer"},
				},
			}}},
			method: "UpdateTallies",
			want: []string{
				"keys := make([]models.TallyPK, 0, len(matched))",
				"Increment(ctx, models.TallyPK{OwnerID: row.OwnerID, Slot: row.Slot},",
				"keys = append(keys, models.TallyPK{OwnerID: row.OwnerID, Slot: row.Slot})",
				"Filter: &models.TallyFilter{PKs: keys},",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := extractFunc(t, renderResolvers(t, resolverAPIContext(t, tt.schema, nil)), tt.method)
			for _, want := range tt.want {
				mustContain(t, body, want)
			}
		})
	}
}

// TestPKFromRow pins the key expression the bulk method reads off a matched
// row: the field itself for a single-column key, the model's PK struct for a
// composite one, with no conversion either way because the row already holds
// the model's types.
func TestPKFromRow(t *testing.T) {
	pkFromRow := gen.FuncMap(sql.NewPostgresDialect())["pkFromRow"].(func(string, string, string, []gen.APIPKArg) string)
	tests := []struct {
		name string
		args []gen.APIPKArg
		want string
	}{
		{
			name: "single column key",
			args: []gen.APIPKArg{{Name: "id", GoType: "int", ModelGoType: "int64", ModelFieldName: "ID"}},
			want: "row.ID",
		},
		{
			name: "composite key",
			args: []gen.APIPKArg{
				{Name: "workspace_id", GoType: "uuid.UUID", ModelGoType: "uuid.UUID", ModelFieldName: "WorkspaceID"},
				{Name: "key", GoType: "string", ModelGoType: "string", ModelFieldName: "Key"},
			},
			want: "models.WorkspaceSettingPK{WorkspaceID: row.WorkspaceID, Key: row.Key}",
		},
		{name: "no key", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pkFromRow("models", "WorkspaceSetting", "row", tt.args); got != tt.want {
				t.Errorf("pkFromRow(models, WorkspaceSetting, row, %+v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestUpdateInput_NullableColumnIsOmittable pins the explicit-null contract at
// both ends: sqlgen tells gqlgen to emit the update field over a nullable
// column as `graphql.Omittable`, and the translator turns a present `null`
// into a set model field holding its NULL while an absent field stays unset.
// A NOT NULL column and every create field keep the plain pointer.
func TestUpdateInput_NullableColumnIsOmittable(t *testing.T) {
	apiCtx := resolverAPIContext(t, resolverTestSchema(), nil)
	overrides := gen.APIGoFieldOverrides(apiCtx)

	tests := []struct {
		name          string
		typeName      string
		field         string
		wantOmittable bool
	}{
		{name: "nullable column on the update input", typeName: "UpdateProductInput", field: "description", wantOmittable: true},
		{name: "NOT NULL column on the update input", typeName: "UpdateProductInput", field: "name", wantOmittable: false},
		{name: "nullable column on the create input", typeName: "CreateProductInput", field: "description", wantOmittable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := overrides[tt.typeName][tt.field].Omittable; got != tt.wantOmittable {
				t.Errorf("APIGoFieldOverrides()[%s][%s].Omittable = %v, want %v", tt.typeName, tt.field, got, tt.wantOmittable)
			}
		})
	}

	apiCtx.ModelsPackage = "models"
	out := renderGqlmodelTemplate(t, "api/input-translate", apiCtx)
	update := upfTopLevelFunc(t, out, "translateUpdateProductInput")
	for _, want := range []string{
		"if val, ok := in.Description.ValueOK(); ok {",
		"zero, _ := out.Description.Get()\n\t\t\tout.Description = omittable.Set(zero)",
		"out.Description = omittable.Set(val)",
		"if in.Name != nil {",
	} {
		mustContain(t, update, want)
	}
	mustContain(t, upfTopLevelFunc(t, out, "hasUpdateProductSetFields"), "if in.Description.IsSet() {")
}
