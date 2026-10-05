package gen_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// relFilterSchema is the fixture for the relationship-filter tests (PRD §11.1).
// It carries one of each shape the compiler has to distinguish:
//
//   - users → orders: O2M (FK without UNIQUE), the plain correlated shape
//   - users ↔ categories via user_categories: M2M, the junction shape
//   - users → profiles: O2O (FK + UNIQUE), which contributes NO filter member
//   - orders → order_items: a second hop, so a nested relationship filter has
//     somewhere to go
//   - notes: soft-deleted O2M target, for the §17.3 injection
func relFilterSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid"},
					{Name: "email", Type: "text"},
				},
			},
			{
				Name: "profiles",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Unique: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
					{Name: "bio", Type: "text"},
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid"},
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
					{Name: "status", Type: "text"},
				},
			},
			{
				Name: "order_items",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "order_id", Type: "uuid", FKReference: &parser.FKReference{Table: "orders", Column: "id"}},
					{Name: "sku", Type: "text"},
				},
			},
			{
				Name: "notes",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
					{Name: "body", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
			{
				Name: "categories",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name: "user_categories",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
					{Name: "category_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "categories", Column: "id"}},
				},
				// M2M detection keys on a composite PK/UNIQUE constraint over
				// both FKs, not on the per-column PrimaryKey flags.
				Constraints: []parser.Constraint{
					{Type: parser.PrimaryKey, Columns: []string{"user_id", "category_id"}},
				},
			},
		},
	}
}

// relFilterTables builds the table contexts for relFilterSchema, keyed by SQL
// name. tenantColumn enables tenancy on that column when non-empty.
func relFilterTables(t *testing.T, tenantColumn string) map[string]gen.TableContext {
	t.Helper()
	in := testInput(relFilterSchema())
	// Relationship detection is a parser step; the context builders consume its
	// output rather than re-deriving it.
	parser.DetectRelationships(in.Schema)
	if tenantColumn != "" {
		in.Config.Tenancy = &config.TenancyConfig{
			Enabled:  true,
			Column:   tenantColumn,
			Required: new(true),
		}
	}
	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(tables))
	for _, tc := range tables {
		byName[tc.TableName] = tc
	}
	return byName
}

func relFilterNames(rfs []gen.RelationshipFilterContext) []string {
	names := make([]string, len(rfs))
	for i, rf := range rfs {
		names[i] = rf.FieldName
	}
	return names
}

func findRelFilter(t *testing.T, tc gen.TableContext, field string) gen.RelationshipFilterContext {
	t.Helper()
	for _, rf := range tc.RelationshipFilters {
		if rf.FieldName == field {
			return rf
		}
	}
	t.Fatalf("table %s has no relationship filter %q, has %v", tc.TableName, field, relFilterNames(tc.RelationshipFilters))
	return gen.RelationshipFilterContext{}
}

// TestRelationshipFilters_ListRelationshipsOnly is the PRD §11.1 scoping rule:
// a list relationship contributes a member, a to-one relationship does not. The
// O2O's FK is directly filterable on the parent and the inverse list direction
// expresses the same predicate, so an EXISTS for it would be a second spelling
// of an existing capability.
func TestRelationshipFilters_ListRelationshipsOnly(t *testing.T) {
	tables := relFilterTables(t, "")

	got := relFilterNames(tables["users"].RelationshipFilters)
	want := []string{"Categories", "Notes", "Orders"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("users relationship filters (-want +got):\n%s", diff)
	}

	// The O2O edge is emitted on the FK holder — profiles → users. Assert it
	// exists, so the exclusion is a real observation rather than a vacuous pass
	// on an undetected edge, and assert profiles gained no member for it.
	var hasO2O bool
	for _, rel := range tables["profiles"].O2ORelationships {
		if rel.TargetTable == "users" {
			hasO2O = true
		}
	}
	if !hasO2O {
		t.Fatal("fixture no longer produces a profiles→users O2O relationship; the exclusion assertion below is vacuous")
	}
	if names := relFilterNames(tables["profiles"].RelationshipFilters); len(names) != 0 {
		t.Errorf("profiles relationship filters = %v, want none (its only relationship is to-one)", names)
	}
}

// TestRelationshipFilters_O2MJoinMetadata pins the correlated shape: the FK
// lives on the target, and the correlation is the parent's own PK column,
// carried without a qualifier for the builder to resolve.
func TestRelationshipFilters_O2MJoinMetadata(t *testing.T) {
	tables := relFilterTables(t, "")
	rf := findRelFilter(t, tables["users"], "Orders")

	if rf.IsM2M {
		t.Error("O2M relationship filter marked as M2M")
	}
	if rf.FKColumn != "user_id" {
		t.Errorf("FKColumn = %q, want %q", rf.FKColumn, "user_id")
	}
	if rf.CorrelationColumn != "id" {
		t.Errorf("CorrelationColumn = %q, want %q", rf.CorrelationColumn, "id")
	}
	if rf.TargetStructName != "Order" {
		t.Errorf("TargetStructName = %q, want %q", rf.TargetStructName, "Order")
	}
	if rf.JunctionTable != "" {
		t.Errorf("JunctionTable = %q, want empty for O2M", rf.JunctionTable)
	}
}

// TestRelationshipFilters_M2MJoinMetadata pins the junction shape: one EXISTS
// over the junction joined to the target, correlating on the junction's local
// FK. This is a WHERE-clause predicate, distinct from the two-query M2M loader.
func TestRelationshipFilters_M2MJoinMetadata(t *testing.T) {
	tables := relFilterTables(t, "")
	rf := findRelFilter(t, tables["users"], "Categories")

	if !rf.IsM2M {
		t.Fatal("M2M relationship filter not marked as M2M")
	}
	if rf.JunctionTable != "user_categories" {
		t.Errorf("JunctionTable = %q, want %q", rf.JunctionTable, "user_categories")
	}
	if rf.JunctionLocalFK != "user_id" {
		t.Errorf("JunctionLocalFK = %q, want %q", rf.JunctionLocalFK, "user_id")
	}
	if rf.JunctionReferenceFK != "category_id" {
		t.Errorf("JunctionReferenceFK = %q, want %q", rf.JunctionReferenceFK, "category_id")
	}
	if rf.TargetPKColumn != "id" {
		t.Errorf("TargetPKColumn = %q, want %q", rf.TargetPKColumn, "id")
	}
	if rf.CorrelationColumn != "id" {
		t.Errorf("CorrelationColumn = %q, want %q", rf.CorrelationColumn, "id")
	}
}

// TestRelationshipFilters_TargetSoftDeleteAttached checks that the injection
// decision is read off the *target*, not the parent: notes carries deleted_at,
// orders does not, and users itself has no soft delete at all.
func TestRelationshipFilters_TargetSoftDeleteAttached(t *testing.T) {
	tables := relFilterTables(t, "")

	notes := findRelFilter(t, tables["users"], "Notes")
	if notes.SoftDelete == nil {
		t.Fatal("Notes relationship filter has no SoftDelete, want the target's deleted_at")
	}
	if notes.SoftDelete.Column != "deleted_at" {
		t.Errorf("SoftDelete.Column = %q, want %q", notes.SoftDelete.Column, "deleted_at")
	}

	if orders := findRelFilter(t, tables["users"], "Orders"); orders.SoftDelete != nil {
		t.Errorf("Orders relationship filter has SoftDelete %+v, want nil", orders.SoftDelete)
	}
}

// TestRelationshipFilters_TargetSoftDeleteRespectsExcludeDeleted covers the
// §17.3 opt-out: with exclude_deleted off, the target's own read path injects no
// default, so neither does the subquery.
func TestRelationshipFilters_TargetSoftDeleteRespectsExcludeDeleted(t *testing.T) {
	in := testInput(relFilterSchema())
	parser.DetectRelationships(in.Schema)
	in.Config.Generation.ExcludeDeleted = new(false)
	tables, err := gen.BuildTableContextsFromSchema(in.Schema, in.Config)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	for _, tc := range tables {
		if tc.TableName != "users" {
			continue
		}
		if rf := findRelFilter(t, tc, "Notes"); rf.SoftDelete != nil {
			t.Errorf("SoftDelete = %+v with exclude_deleted:false, want nil", rf.SoftDelete)
		}
		return
	}
	t.Fatal("users table context not built")
}

// TestRelationshipFilters_TargetTenantColumnAttached is the invariant-1 wiring:
// the tenant predicate the subquery injects comes from the target's own tenancy
// config. categories has no workspace_id, so a filter through it stays unscoped
// — the un-tenanted-child pattern §29.10 explicitly allows.
func TestRelationshipFilters_TargetTenantColumnAttached(t *testing.T) {
	tables := relFilterTables(t, "workspace_id")

	if got := findRelFilter(t, tables["users"], "Orders").TenantColumn; got != "workspace_id" {
		t.Errorf("Orders TenantColumn = %q, want %q", got, "workspace_id")
	}
	if got := findRelFilter(t, tables["users"], "Categories").TenantColumn; got != "" {
		t.Errorf("Categories TenantColumn = %q, want empty (categories is not tenanted)", got)
	}
}

// TestRelationshipFilters_TenantedFlagIsTransitive covers the reason the flag is
// a closure rather than a direct lookup: order_items is not tenanted and neither
// is anything it points at, but orders reaches a tenanted table through its own
// members, so a parent filtering by orders still has to resolve a tenant.
func TestRelationshipFilters_TenantedFlagIsTransitive(t *testing.T) {
	tables := relFilterTables(t, "workspace_id")

	if !tables["users"].HasTenantedRelationshipFilter {
		t.Error("users.HasTenantedRelationshipFilter = false, want true (orders is tenanted)")
	}
	// order_items has no relationship filters of its own and reaches nothing.
	if tables["order_items"].HasTenantedRelationshipFilter {
		t.Error("order_items.HasTenantedRelationshipFilter = true, want false")
	}
	// categories reaches users (M2M inverse), which reaches tenanted orders.
	if !tables["categories"].HasTenantedRelationshipFilter {
		t.Error("categories.HasTenantedRelationshipFilter = false, want true via users → orders")
	}
}

// TestRelationshipFilters_NoneWithoutTenancy keeps the untenanted path honest:
// relationship filters still compile, but nothing resolves a tenant.
func TestRelationshipFilters_NoneWithoutTenancy(t *testing.T) {
	tables := relFilterTables(t, "")
	for name, tc := range tables {
		if tc.HasTenantedRelationshipFilter {
			t.Errorf("%s.HasTenantedRelationshipFilter = true with tenancy disabled", name)
		}
		if !tc.HasFilterOptions {
			t.Errorf("%s.HasFilterOptions = false, want true (the package has relationship filters)", name)
		}
	}
}

// TestRelationshipFilters_CompositePKParentEmitsNone covers the shape
// sql.Exists cannot express: one correlation column, so a parent whose identity
// spans several columns has nothing single to correlate on. The member is
// dropped rather than emitted against an arbitrary PK column.
func TestRelationshipFilters_CompositePKParentEmitsNone(t *testing.T) {
	tables := relFilterTables(t, "")
	uc := tables["user_categories"]
	if !uc.CompositePK {
		t.Fatal("fixture no longer produces a composite-PK user_categories; the assertion below is vacuous")
	}
	if len(uc.RelationshipFilters) != 0 {
		t.Errorf("user_categories relationship filters = %v, want none (composite PK)", relFilterNames(uc.RelationshipFilters))
	}
}

// --- Emitted code ---

func renderRelFilter(t *testing.T, tc gen.TableContext) string {
	t.Helper()
	return executeFilterTemplate(t, tc)
}

// TestFilterTemplate_O2MEmitsCorrelatedExists pins the emitted O2M subquery: a
// bare correlation token, no baked-in qualifier, and the target's own
// ToConditions supplying the inner WHERE one nesting level down.
func TestFilterTemplate_O2MEmitsCorrelatedExists(t *testing.T) {
	tables := relFilterTables(t, "")
	got := renderRelFilter(t, tables["users"])

	for _, want := range []string{
		"Orders *OrderFilter `json:\"orders\"`",
		"func (f *UserFilter) ToConditions(dialect sql.Dialect, opts ...FilterOption) []sql.Condition {",
		"conds = append(conds, userOrdersFilterExists(f.Orders, dialect, opts))",
		`func userOrdersFilterExists(f *OrderFilter, dialect sql.Dialect, opts []FilterOption) sql.Condition {`,
		`tgt := filterSubqueryAlias("tgt", scope.depth)`,
		`dialect.QuoteIdentifier("user_id") + " = " + sql.CorrelationToken`,
		`sql.SubqueryWhere(dialect, tgt, f.ToConditions(dialect, nestFilterScope(opts, scope.depth+1)...))`,
		`return sql.Exists("id", sql.Subquery{SQL: subSQL, Args: subArgs})`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered filter missing %q\n---\n%s", want, got)
		}
	}
}

// TestFilterTemplate_M2MEmitsJunctionJoin pins the single-EXISTS junction shape
// required by §11.1 — FROM junction JOIN target, correlating on the junction's
// local FK — as opposed to the two-query M2M loader.
func TestFilterTemplate_M2MEmitsJunctionJoin(t *testing.T) {
	tables := relFilterTables(t, "")
	got := renderRelFilter(t, tables["users"])

	for _, want := range []string{
		"Categories *CategoryFilter `json:\"categories\"`",
		`sql.Table{Schema: "", Name: "user_categories"}`,
		`" JOIN " + dialect.FormatTable(sql.Table{Schema: "", Name: "categories"}) + " " + tgt`,
		`dialect.QuoteIdentifier("id") + " = " + jn + "." + dialect.QuoteIdentifier("category_id")`,
		`" WHERE " + jn + "." + dialect.QuoteIdentifier("user_id") + " = " + sql.CorrelationToken`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered filter missing %q\n---\n%s", want, got)
		}
	}
	if n := strings.Count(got, "func userCategoriesFilterExists("); n != 1 {
		t.Errorf("userCategoriesFilterExists declared %d times, want 1", n)
	}
}

// TestFilterTemplate_SoftDeleteAndTenantInsideSubquery is the invariant-1
// assertion at the emission layer: both predicates are appended to subSQL, so
// they land inside the EXISTS rather than beside it in the outer WHERE.
func TestFilterTemplate_SoftDeleteAndTenantInsideSubquery(t *testing.T) {
	tables := relFilterTables(t, "workspace_id")
	got := renderRelFilter(t, tables["users"])

	for _, want := range []string{
		"if f.DeletedAt == nil {",
		`subSQL += " AND " + tgt + "." + dialect.QuoteIdentifier("deleted_at") + " IS NULL"`,
		"if scope.applyTenant {",
		`subSQL += " AND " + tgt + "." + dialect.QuoteIdentifier("workspace_id") + " = $"`,
		"subArgs = append(subArgs, scope.tenant)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered filter missing %q\n---\n%s", want, got)
		}
	}
	// The tenant predicate must never be appended to the outer condition slice.
	if strings.Contains(got, `conds = append(conds, sql.Where(`) {
		t.Error("tenant predicate appended to the outer conditions instead of the subquery")
	}
}

// TestFilterTemplate_NoRelationshipsKeepsBareSignature is the byte-identity
// guard for projects that gain nothing from this feature: with no relationship
// filter anywhere in the package, ToConditions keeps its bare signature and no
// FilterOption plumbing is threaded.
func TestFilterTemplate_NoRelationshipsKeepsBareSignature(t *testing.T) {
	got := executeFilterTemplate(t, testFilterTableContext())

	if !strings.Contains(got, "func (f *ProductFilter) ToConditions(dialect sql.Dialect) []sql.Condition {") {
		t.Errorf("filter without relationships changed its ToConditions signature\n---\n%s", got)
	}
	for _, unwanted := range []string{"FilterOption", "filterSubqueryAlias", "sql.Exists"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("filter without relationships emitted %q\n---\n%s", unwanted, got)
		}
	}
}

// TestRelationshipFilters_SharedParentRendersTenantedClient covers the shape a
// row-set test cannot reach: a table with no tenant column of its own whose
// list relationship points at a tenanted one. Compiling its relationship filter
// injects the *target's* tenant predicate, so the client has to resolve a
// tenant despite carrying none — and the emitted `tenancy.TenantResolver[T]`
// then needs a concrete T that the table's own (absent) tenancy config cannot
// supply. Rendering the client template is the assertion, because an empty T
// produces `tenancy.TenantResolver[]`, which does not compile and which no
// example schema currently exercises.
func TestRelationshipFilters_SharedParentRendersTenantedClient(t *testing.T) {
	tables := relFilterTables(t, "workspace_id")
	categories := tables["categories"]

	if categories.Tenancy != nil && categories.Tenancy.Tenanted {
		t.Fatal("fixture no longer keeps categories untenanted; the assertions below are vacuous")
	}
	if !categories.HasTenantedRelationshipFilter {
		t.Fatal("categories does not reach a tenanted target; the assertions below are vacuous")
	}
	if categories.Tenancy == nil || categories.Tenancy.GoType == "" {
		t.Fatalf("untenanted parent has no tenant Go type to render TenantResolver[T] with: %+v", categories.Tenancy)
	}
	if !slices.Contains(categories.Imports, "github.com/teandresmith/sqlgen/tenancy") {
		t.Errorf("untenanted parent is missing the tenancy import: %v", categories.Imports)
	}

	got := executeClientTemplate(t, categories)
	if strings.Contains(got, "TenantResolver[]") {
		t.Errorf("client renders an empty tenant type parameter:\n%s", got)
	}
	for _, want := range []string{
		// The tenant column is `uuid` with no override, so it resolves to the
		// standard library's uuid.UUID (PRD §7.2).
		"tenantResolver tenancy.TenantResolver[uuid.UUID]",
		"func (c *categoryClient) filterOptions(ctx context.Context, filter *CategoryFilter, skipTenancy bool, explicit *uuid.UUID) ([]FilterOption, error) {",
		// The gate is what keeps a shared table readable before a tenant is
		// selected. Its outer WHERE is never tenant-scoped, so resolving on
		// every call hands a fail-closed resolver a veto over reads that have
		// no tenanted component at all.
		"if skipTenancy || !filter.usesRelationshipFilter() {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered client missing %q\n---\n%s", want, got)
		}
	}
}

// TestRelationshipFilters_SharedParentGatesTenantResolve pins the runtime half
// of the gate the client asserts above: the predicate filterOptions calls has
// to be emitted on the same shared parent, and it has to answer for members
// reached through and / or, not only for those set at the top level.
func TestRelationshipFilters_SharedParentGatesTenantResolve(t *testing.T) {
	tables := relFilterTables(t, "workspace_id")
	categories := tables["categories"]

	if !categories.HasTenantedRelationshipFilter {
		t.Fatal("categories does not reach a tenanted target; the assertions below are vacuous")
	}
	if len(categories.RelationshipFilters) == 0 {
		t.Fatal("categories has no relationship filter members to gate on")
	}

	got := executeFilterTemplate(t, categories)
	want := make([]string, 0, len(categories.RelationshipFilters)+4)
	want = append(
		want,
		"func (f *CategoryFilter) usesRelationshipFilter() bool {",
		"\tfor _, sub := range f.And {",
		"\tfor _, sub := range f.Or {",
		"\t\tif sub.usesRelationshipFilter() {",
	)
	for _, rf := range categories.RelationshipFilters {
		want = append(want, fmt.Sprintf("\tif f.%s != nil {", rf.FieldName))
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("rendered filter missing %q\n---\n%s", w, got)
		}
	}
}

// TestRelationshipFilters_UntenantedFilterOmitsGate keeps the gate off the
// filters that never reach a tenanted target. Those packages have no
// filterOptions to call it, so emitting it would be dead code — and, with
// tenancy disabled entirely, an unused method on every filter in the package.
func TestRelationshipFilters_UntenantedFilterOmitsGate(t *testing.T) {
	tables := relFilterTables(t, "")
	for name, tc := range tables {
		if tc.HasTenantedRelationshipFilter {
			t.Fatalf("%s reaches a tenanted target with tenancy disabled", name)
		}
		if got := executeFilterTemplate(t, tc); strings.Contains(got, "usesRelationshipFilter") {
			t.Errorf("%s emits the gate without a tenanted relationship filter\n---\n%s", name, got)
		}
	}
}
