package gen_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// loadAllTableTemplates parses every *.tmpl file used by the tenancy
// integration so tests can render a full per-table body.
func loadAllTableTemplates(t *testing.T, dialect sql.Dialect) *template.Template {
	t.Helper()
	tmpl := template.New("").Funcs(gen.FuncMap(dialect))
	var err error
	tmpl, err = tmpl.ParseGlob("templates/shared/_*.tmpl")
	if err != nil {
		t.Fatalf("parsing shared fragments: %v", err)
	}
	tmpl, err = tmpl.ParseGlob(filepath.Join("templates", "table", "*.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing table templates: %v", err)
	}
	tmpl, err = tmpl.ParseFiles(filepath.Join("templates", "client.go.tmpl"))
	if err != nil {
		t.Fatalf("parsing client template: %v", err)
	}
	return tmpl
}

// tenantedProductContext returns a TableContext for a tenanted products
// table — simple single-PK schema mirroring the PRD §29.4 example, used
// across multiple tenancy template tests.
func tenantedProductContext() gen.TableContext {
	return gen.TableContext{
		StructName:            "Product",
		TableName:             "products",
		TableNameConstant:     "TableProducts",
		Schema:                "public",
		Package:               "db",
		VarName:               "p",
		Dialect:               "postgres",
		Driver:                "pgx",
		PKStrategy:            config.PKStrategyApp,
		UUIDVersion:           "v4",
		PKAutoGenExpr:         "uuid.New()",
		BatchSize:             100,
		QueryLimit:            100,
		CompositePK:           false,
		CompositePKStructName: "",
		Imports: []string{
			"context",
			"fmt",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"github.com/teandresmith/sqlgen/tenancy",
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", ZeroValue: "uuid.UUID{}", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/google/uuid"},
		},
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/google/uuid"},
			{Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID", DBTag: "workspace_id", JSONTag: "workspace_id", Import: "github.com/google/uuid"},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "string", ColumnName: "name", Required: true, JSONTag: "name"},
			{FieldName: "ID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "id", Omittable: true, JSONTag: "id"},
			// Tenant column promoted from required → omittable by attachTenancyToTables.
			{FieldName: "WorkspaceID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "workspace_id", Omittable: true, JSONTag: "workspace_id"},
		},
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Name", GoType: "omittable.Value[string]", ColumnName: "name", Omittable: true, JSONTag: "name"},
			{FieldName: "WorkspaceID", GoType: "omittable.Value[uuid.UUID]", ColumnName: "workspace_id", Omittable: true, JSONTag: "workspace_id"},
		},
		AllColumnNames: []string{"id", "name", "workspace_id"},
		Operations: gen.ResolvedOperations{
			Get:        true,
			GetMany:    true,
			Create:     true,
			CreateMany: true,
			Update:     true,
			HardDelete: true,
			Exists:     true,
			Count:      true,
		},
		Tenancy: &gen.TableTenancyContext{
			Tenanted:     true,
			Column:       "workspace_id",
			FieldName:    "WorkspaceID",
			GoType:       "uuid.UUID",
			Import:       "github.com/google/uuid",
			Required:     true,
			InPrimaryKey: false,
		},
	}
}

func renderTenancyTableBody(t *testing.T, tc gen.TableContext, templateName string) string {
	t.Helper()
	tmpl := loadAllTableTemplates(t, sql.NewPostgresDialect())
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, templateName, tc); err != nil {
		t.Fatalf("executing %s: %v", templateName, err)
	}
	return buf.String()
}

// Asserts the emitted get template contains both the tenant resolution and
// the append to conds. Exercises the happy path (PRD §29.4.1).
func TestTenancyTemplate_getMany_emitsTenantFilter(t *testing.T) {
	tc := tenantedProductContext()
	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		"if !options.SkipTenancy {",
		"resolvedTenant, apply, err := c.resolveTenant(ctx, options.Tenant)",
		`return nil, fmt.Errorf("get products: resolve tenant: %w", err)`,
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`,
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("GetMany output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// Confirms the Create template emits the auto-set block and mismatch check
// before the INSERT build (PRD §29.4.2 mutation path).
func TestTenancyTemplate_create_emitsMismatchAndAutoSet(t *testing.T) {
	tc := tenantedProductContext()
	out := renderTenancyTableBody(t, tc, "table/create")

	wants := []string{
		"if !options.SkipTenancy {",
		"resolvedTenant, apply, err := c.resolveTenant(ctx, options.Tenant)",
		`return nil, fmt.Errorf("create product: resolve tenant: %w", err)`,
		`if v, ok := input.WorkspaceID.Get(); ok && v != resolvedTenant {`,
		"return nil, tenancy.ErrMismatch",
		`columns = append(columns, "workspace_id")`,
		"args = append(args, resolvedTenant)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("Create output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// PRD §29.4.2 requires the tenant column to be excluded from the SET clause
// under normal operation. Only when SkipTenancy is set may the caller change
// the tenant row ownership.
func TestTenancyTemplate_update_tenantNeverInSetUnderNormalPath(t *testing.T) {
	tc := tenantedProductContext()
	out := renderTenancyTableBody(t, tc, "table/update")

	// The structural guard must be present. `if !applyTenancy` (not `if
	// options.SkipTenancy`) means the tenant SET clause fires under
	// SkipTenancy:true OR under required:false + zero resolver — either
	// explicit opt-out or implicit cross-tenant.
	wantGuard := "if !applyTenancy {"
	if !strings.Contains(out, wantGuard) {
		t.Errorf("Update output missing applyTenancy guard around tenant SET clause\nfull output:\n%s", out)
	}

	// The tenant column must be rolled into Conditions, not SET, on the happy path.
	wantCond := `updateConds = append(updateConds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`
	if !strings.Contains(out, wantCond) {
		t.Errorf("Update output missing tenant condition append\nfull output:\n%s", out)
	}

	// ErrMismatch must surface before any SQL is built.
	wantMismatch := "return nil, tenancy.ErrMismatch"
	if !strings.Contains(out, wantMismatch) {
		t.Errorf("Update output missing ErrMismatch check\nfull output:\n%s", out)
	}
}

// PRD §29.4.3: the generator emits an explicit bypass comment above Raw and
// RawExec when tenancy is enabled globally. Verifies the comment is present
// for a tenancy-enabled client.
func TestTenancyTemplate_clientRawHasBypassComment(t *testing.T) {
	ctx := gen.ClientContext{
		Entities: []gen.EntityClientContext{
			{StructName: "Product", InterfaceName: "ProductClient", FieldName: "product", AccessorName: "Products"},
		},
		ClientName:       "Client",
		Package:          "db",
		TenancyEnabled:   true,
		TenancyGoType:    "uuid.UUID",
		TenancyImport:    "github.com/google/uuid",
		TenantedEntities: []string{"product"},
		Imports: []string{
			"context",
			"errors",
			"io",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/hook",
			"github.com/teandresmith/sqlgen/tenancy",
		},
	}

	tmpl := loadAllTableTemplates(t, sql.NewPostgresDialect())
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "client", ctx); err != nil {
		t.Fatalf("executing client template: %v", err)
	}

	out := buf.String()
	wants := []string{
		"// sqlgen: raw query bypasses tenancy",
		"func WithTenantResolver(r tenancy.TenantResolver[uuid.UUID])",
		"tenantResolver tenancy.TenantResolver[uuid.UUID]",
		"c.product.tenantResolver = options.tenantResolver",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("client output missing %q\nfull output:\n%s", want, out)
		}
	}

	// The comment must appear twice — once for Raw, once for RawExec.
	if got := strings.Count(out, "// sqlgen: raw query bypasses tenancy"); got != 2 {
		t.Errorf("expected 2 bypass comments, got %d", got)
	}
}

// Tenancy-disabled projects must regenerate byte-identically — the client
// template emits no tenancy references when TenancyEnabled is false.
func TestTenancyTemplate_clientOmitsTenancyWhenDisabled(t *testing.T) {
	ctx := gen.ClientContext{
		Entities: []gen.EntityClientContext{
			{StructName: "Product", InterfaceName: "ProductClient", FieldName: "product", AccessorName: "Products"},
		},
		ClientName:     "Client",
		Package:        "db",
		TenancyEnabled: false,
		Imports: []string{
			"context",
			"errors",
			"io",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/hook",
		},
	}

	tmpl := loadAllTableTemplates(t, sql.NewPostgresDialect())
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "client", ctx); err != nil {
		t.Fatalf("executing client template: %v", err)
	}

	out := buf.String()
	mustNotContain := []string{
		"sqlgen: raw query bypasses tenancy",
		"WithTenantResolver",
		"tenantResolver",
		"tenancy.TenantResolver",
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("tenancy-disabled client output unexpectedly contains %q", s)
		}
	}
}

// Shared (non-tenanted) tables in a tenancy-enabled project must emit
// per-entity method bodies byte-identical to the tenancy-disabled baseline
// — only the resolver wiring at client construction differs. This test
// renders the GetMany template for a Tenancy-set but not-tenanted table
// and asserts no tenancy references appear.
func TestTenancyTemplate_sharedTableInTenancyProjectHasNoFilter(t *testing.T) {
	tc := tenantedProductContext()
	// Flip to shared: Tenancy present but not tenanted.
	tc.Tenancy = &gen.TableTenancyContext{Tenanted: false}

	out := renderTenancyTableBody(t, tc, "table/get")

	mustNotContain := []string{
		"resolvedTenant",
		"SkipTenancy",
		"tenancy.ErrMissing",
		"tenancy.ErrMismatch",
		`Where("workspace_id")`,
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("shared-table output unexpectedly contains %q", s)
		}
	}
}

// Verifies BuildClientContext wires TenancyEnabled/GoType/Entities correctly
// when tenancy is enabled and one of the tables is tenanted.
func TestBuildClientContext_tenancyEnabledProducesResolverWiring(t *testing.T) {
	enabledTrue := true
	cfg := &config.RootConfig{
		Tenancy: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: &enabledTrue},
	}
	tables := []gen.TableContext{
		{
			StructName: "Product",
			Tenancy:    &gen.TableTenancyContext{Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		},
		{
			StructName: "AuditLog",
			Tenancy:    &gen.TableTenancyContext{Tenanted: false},
		},
	}
	tenancyMap := map[string]gen.TenancyContext{
		"public.products":   {Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		"public.audit_logs": {Tenanted: false},
	}

	ctx := gen.BuildClientContext(tables, nil, "db", "Client", false, nil, cfg, tenancyMap)

	if !ctx.TenancyEnabled {
		t.Fatalf("TenancyEnabled = false, want true")
	}
	if ctx.TenancyGoType != "uuid.UUID" {
		t.Errorf("TenancyGoType = %q, want uuid.UUID", ctx.TenancyGoType)
	}
	if ctx.TenancyImport != "github.com/google/uuid" {
		t.Errorf("TenancyImport = %q, want github.com/google/uuid", ctx.TenancyImport)
	}
	if len(ctx.TenantedEntities) != 1 || ctx.TenantedEntities[0] != "product" {
		t.Errorf("TenantedEntities = %v, want [product]", ctx.TenantedEntities)
	}
	// Imports must include tenancy + the tenant type's package.
	wantImports := []string{"github.com/teandresmith/sqlgen/tenancy", "github.com/google/uuid"}
	for _, want := range wantImports {
		if !slices.Contains(ctx.Imports, want) {
			t.Errorf("Imports missing %q; got %v", want, ctx.Imports)
		}
	}
}

// tenantedO2OParentContext builds a parent TableContext with one O2O target.
// tenantedParent / tenantedChild toggle whether each side carries a tenanted
// TableTenancyContext plus the child-tenant annotation normally produced by
// annotateO2OChildTenancy. Used by the o2o-propagation tests.
func tenantedO2OParentContext(tenantedParent, tenantedChild bool) gen.TableContext {
	tc := gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Package:           "db",
		VarName:           "p",
		Dialect:           "postgres",
		Driver:            "pgx",
		QueryLimit:        100,
		Imports: []string{
			"context",
			"fmt",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"github.com/teandresmith/sqlgen/tenancy",
		},
		Columns: []gen.ColumnContext{
			{Name: "company_id", FieldName: "CompanyID", GoType: "uuid.UUID", DBTag: "company_id", JSONTag: "company_id", Import: "github.com/google/uuid"},
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/google/uuid"},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true, Import: "github.com/google/uuid"},
		},
		AllColumnNames: []string{"company_id", "id", "name"},
		ScanShapes: []gen.ScanShapeContext{
			{ColumnName: "company_id", FieldName: "CompanyID", Shape: "direct", ScanExpr: "&p.CompanyID"},
			{ColumnName: "id", FieldName: "ID", Shape: "direct", ScanExpr: "&p.ID"},
			{ColumnName: "name", FieldName: "Name", Shape: "direct", ScanExpr: "&p.Name"},
		},
		Operations:          gen.ResolvedOperations{Get: true, GetMany: true},
		HasO2ORelationships: true,
		O2ORelationships: []gen.RelationshipContext{
			{
				Name: "company", Type: parser.OneToOne, TargetTable: "companies", TargetSchema: "public",
				TargetStructName: "Company", FKColumn: "company_id", FKFieldName: "CompanyID",
				FieldName: "Company", GoType: "*Company", JSONTag: "company",
			},
		},
		O2OJoinDetails: []gen.O2OJoinDetail{
			{
				FieldOptionsCheck: "fo.Company != nil", FieldOptionsColumns: "fo.Company.Columns()",
				PKColumn:    "id",
				TargetTable: "companies", TargetSchema: "public",
				Alias: "c", OnLocalAlias: "p", OnLocal: "company_id", OnRemote: "id",
				StructName: "Company", VarName: "c", HasDataVar: "cHasData",
				PKFieldName: "ID", PKZeroValue: "uuid.Nil", ParentVarName: "p", FieldName: "Company",
				ScanCases: []gen.O2OScanCase{
					{PrefixedColumn: "c.id", ScanExpr: "&c.ID", IsPK: true, Shape: "direct", FieldName: "ID", VarName: "c"},
					{PrefixedColumn: "c.name", ScanExpr: "&c.Name", Shape: "direct", FieldName: "Name", VarName: "c"},
				},
			},
		},
		O2OAllTargets: []gen.O2OJoinDetail{
			{StructName: "Company", VarName: "c", HasDataVar: "cHasData", PKFieldName: "ID", PKZeroValue: "uuid.Nil", ParentVarName: "p", FieldName: "Company"},
		},
		O2OAssignOrder: []gen.O2OJoinDetail{
			{StructName: "Company", VarName: "c", HasDataVar: "cHasData", PKFieldName: "ID", PKZeroValue: "uuid.Nil", ParentVarName: "p", FieldName: "Company"},
		},
		O2OParentScanCases: []gen.O2OScanCase{
			{PrefixedColumn: "p.company_id", ScanExpr: "&p.CompanyID", Shape: "direct", FieldName: "CompanyID", VarName: "p"},
			{PrefixedColumn: "p.id", ScanExpr: "&p.ID", Shape: "direct", FieldName: "ID", VarName: "p"},
			{PrefixedColumn: "p.name", ScanExpr: "&p.Name", Shape: "direct", FieldName: "Name", VarName: "p"},
		},
	}
	if tenantedParent {
		tc.Tenancy = &gen.TableTenancyContext{
			Tenanted: true, Column: "workspace_id", FieldName: "WorkspaceID",
			GoType: "uuid.UUID", Import: "github.com/google/uuid", Required: true,
		}
	} else if tenantedChild {
		// Parent not tenanted but needs resolver + canonical tenant type to
		// emit a working resolveTenant method.
		tc.Tenancy = &gen.TableTenancyContext{
			Tenanted: false, GoType: "uuid.UUID", Import: "github.com/google/uuid",
		}
	}
	if tenantedChild {
		tc.TenantedO2OChildren = []gen.TenantedO2OChild{
			{Alias: "c", TenantColumn: "workspace_id"},
		}
		tc.HasTenantedO2OChild = true
	}
	return tc
}

// o2o golden #1: tenanted parent + tenanted child → outer WHERE carries
// both the parent and child tenant filters (belt-and-suspenders §29.10). The
// child filter goes through the tenantedAliases map + runtime o2oJoins loop so
// only aliases actually JOINed contribute a WHERE.
func TestTenancyTemplate_o2o_bothTenanted_emitsBothFilters(t *testing.T) {
	tc := tenantedO2OParentContext(true, true)
	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		// Parent tenant filter — appended to `conds` BEFORE PrefixConditions.
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`,
		// o2o branch resolves tenant again for the child filter.
		"prefixedConds := sql.PrefixConditions(\"p\", conds)",
		"if !options.SkipTenancy {",
		`return nil, fmt.Errorf("get products: resolve tenant: %w", err)`,
		// Static map from alias → tenant column (tenantedAliases literal).
		"tenantedAliases := map[string]string{",
		`"c": "workspace_id",`,
		// Runtime loop intersects the map against o2oJoins and appends only
		// for aliases that are actually JOINed.
		"for _, j := range o2oJoins {",
		"if col, ok := tenantedAliases[j.Alias]; ok {",
		"prefixedConds = append(prefixedConds, sql.Where(j.Alias+\".\"+c.dialect.QuoteIdentifier(col)).Eq(resolvedTenant))",
		"Conditions: prefixedConds,",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("o2o both-tenanted output missing %q\nfull output:\n%s", want, out)
		}
	}

	// The unconditional per-child append pattern from the old template
	// must not appear — it's the exact bug the map form eliminates.
	mustNotContain := []string{
		`prefixedConds = append(prefixedConds, sql.Where("c.workspace_id").Eq(resolvedTenant))`,
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("o2o both-tenanted output unexpectedly contains unconditional per-child append %q\nfull output:\n%s", s, out)
		}
	}
}

// o2o golden #2: tenanted parent + non-tenanted child → parent filter
// only, no child-side filter in the outer WHERE (§29.10 non-tenanted-child
// non-error).
func TestTenancyTemplate_o2o_parentOnlyTenanted_noChildFilter(t *testing.T) {
	tc := tenantedO2OParentContext(true, false)
	out := renderTenancyTableBody(t, tc, "table/get")

	if !strings.Contains(out, `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`) {
		t.Errorf("parent-only tenanted output missing parent tenant filter\nfull output:\n%s", out)
	}
	// The o2o branch must not emit a child-side tenant filter.
	mustNotContain := []string{
		`sql.Where("c.workspace_id")`,
		"prefixedConds = append(prefixedConds, sql.Where",
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("parent-only tenanted output unexpectedly contains %q\nfull output:\n%s", s, out)
		}
	}
}

// o2o golden #3: non-tenanted parent + tenanted child → child filter
// is applied in the outer WHERE, parent has no own tenant filter (§29.10).
// Parent client still gets a resolveTenant method since it needs to resolve
// tenant at call time to filter the child side.
func TestTenancyTemplate_o2o_childOnlyTenanted_emitsChildFilter(t *testing.T) {
	tc := tenantedO2OParentContext(false, true)
	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		"prefixedConds := sql.PrefixConditions(\"p\", conds)",
		"if !options.SkipTenancy {",
		"resolvedTenant, apply, err := c.resolveTenant(ctx, options.Tenant)",
		// tenantedAliases map + runtime o2oJoins intersection.
		"tenantedAliases := map[string]string{",
		`"c": "workspace_id",`,
		"for _, j := range o2oJoins {",
		"if col, ok := tenantedAliases[j.Alias]; ok {",
		"prefixedConds = append(prefixedConds, sql.Where(j.Alias+\".\"+c.dialect.QuoteIdentifier(col)).Eq(resolvedTenant))",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("child-only tenanted output missing %q\nfull output:\n%s", want, out)
		}
	}
	// The parent itself is not tenanted — no `sql.Where(c.dialect.QuoteIdentifier("workspace_id"))`
	// (bare parent filter) should appear.
	if strings.Contains(out, `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id"))`) {
		t.Errorf("child-only tenanted output unexpectedly contains parent-side tenant filter\nfull output:\n%s", out)
	}
	// Per-child unconditional append (the old pattern) must not appear.
	if strings.Contains(out, `prefixedConds = append(prefixedConds, sql.Where("c.workspace_id").Eq(resolvedTenant))`) {
		t.Errorf("child-only tenanted output unexpectedly contains the old per-child append\nfull output:\n%s", out)
	}
}

// Parent with multiple tenanted o2o targets (direct + chained)
// renders every alias in the tenantedAliases map literal so the runtime
// o2oJoins loop can pick up whichever subset FieldOptions actually JOINed.
// Critically, the template must NOT emit a per-child unconditional
// `prefixedConds = append(...)` sequence (the old pattern).
func TestTenancyTemplate_o2o_multipleTenantedChildren_rendersMap(t *testing.T) {
	tc := tenantedO2OParentContext(true, true)
	// Simulate what annotateO2OChildTenancy produces for a chained o2o
	// tree: the direct child "c" plus a chained alias "o" that is
	// tenanted but only in the JOIN list when the caller selects Company
	// AND its nested Owner.
	tc.TenantedO2OChildren = []gen.TenantedO2OChild{
		{Alias: "c", TenantColumn: "workspace_id"},
		{Alias: "o", TenantColumn: "workspace_id"},
	}

	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		"tenantedAliases := map[string]string{",
		`"c": "workspace_id",`,
		`"o": "workspace_id",`,
		"for _, j := range o2oJoins {",
		"if col, ok := tenantedAliases[j.Alias]; ok {",
		"prefixedConds = append(prefixedConds, sql.Where(j.Alias+\".\"+c.dialect.QuoteIdentifier(col)).Eq(resolvedTenant))",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("multi-child o2o output missing %q\nfull output:\n%s", want, out)
		}
	}

	// The old per-child unconditional appends must not appear — the
	// whole point of the map is that the runtime loop decides per-call.
	mustNotContain := []string{
		`prefixedConds = append(prefixedConds, sql.Where("c.workspace_id").Eq(resolvedTenant))`,
		`prefixedConds = append(prefixedConds, sql.Where("o.workspace_id").Eq(resolvedTenant))`,
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("multi-child o2o output unexpectedly contains unconditional per-child append %q\nfull output:\n%s", s, out)
		}
	}
}

// Parent client.go.tmpl emits tenantResolver + resolveTenant even when
// the parent table itself is not tenanted, as long as an o2o child is.
func TestTenancyTemplate_client_emitsResolverForNonTenantedParentWithTenantedChild(t *testing.T) {
	tc := tenantedO2OParentContext(false, true)
	out := renderTenancyTableBody(t, tc, "table/client")

	wants := []string{
		"tenantResolver tenancy.TenantResolver[uuid.UUID]",
		"func (c *productClient) resolveTenant(ctx context.Context, explicit *uuid.UUID) (uuid.UUID, bool, error)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("non-tenanted-parent client output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// annotateO2OChildTenancy populates TenantedO2OChildren + the matching
// bool flag on parents with tenanted O2O children (including chained targets)
// and leaves parents with no tenanted children untouched.
func TestAttachTenancyToTables_annotatesO2OChildTenancy(t *testing.T) {
	tables := []gen.TableContext{
		{
			StructName: "Product", TableName: "products", Schema: "public",
			O2OJoinDetails: []gen.O2OJoinDetail{
				{Alias: "c", TargetTable: "companies", TargetSchema: "public"},
			},
		},
		{
			StructName: "Order", TableName: "orders", Schema: "public",
			O2OJoinDetails: []gen.O2OJoinDetail{
				{Alias: "u", TargetTable: "users", TargetSchema: "public"},
			},
		},
		{StructName: "Company", TableName: "companies", Schema: "public"},
		{StructName: "User", TableName: "users", Schema: "public"},
	}
	tenancyMap := map[string]gen.TenancyContext{
		"public.products":  {Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		"public.companies": {Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		"public.orders":    {Tenanted: false},
		"public.users":     {Tenanted: false},
	}

	gen.AttachTenancyToTablesForTest(tables, tenancyMap)

	// Product → Company (tenanted): must have the annotation.
	if !tables[0].HasTenantedO2OChild {
		t.Errorf("Product HasTenantedO2OChild = false, want true")
	}
	if len(tables[0].TenantedO2OChildren) != 1 ||
		tables[0].TenantedO2OChildren[0].Alias != "c" ||
		tables[0].TenantedO2OChildren[0].TenantColumn != "workspace_id" {
		t.Errorf("Product TenantedO2OChildren = %+v, want [{Alias:c TenantColumn:workspace_id}]", tables[0].TenantedO2OChildren)
	}

	// Order → User (NOT tenanted): no annotation.
	if tables[1].HasTenantedO2OChild {
		t.Errorf("Order HasTenantedO2OChild = true, want false")
	}
	if len(tables[1].TenantedO2OChildren) != 0 {
		t.Errorf("Order TenantedO2OChildren = %v, want empty", tables[1].TenantedO2OChildren)
	}
}

// BuildClientContext's TenantedEntities must include parents with
// tenanted O2O children so the unified client wires the resolver into them
// even when the parent table is not itself tenanted.
func TestBuildClientContext_tenantedO2OParentReceivesResolverWiring(t *testing.T) {
	enabledTrue := true
	cfg := &config.RootConfig{
		Tenancy: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: &enabledTrue},
	}
	tables := []gen.TableContext{
		// Parent table is NOT tenanted but has a tenanted o2o child.
		{
			StructName:          "Order",
			Tenancy:             &gen.TableTenancyContext{Tenanted: false, GoType: "uuid.UUID", Import: "github.com/google/uuid"},
			HasTenantedO2OChild: true,
			TenantedO2OChildren: []gen.TenantedO2OChild{{Alias: "u", TenantColumn: "workspace_id"}},
		},
		{
			StructName: "User",
			Tenancy:    &gen.TableTenancyContext{Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		},
	}
	tenancyMap := map[string]gen.TenancyContext{
		"public.orders": {Tenanted: false},
		"public.users":  {Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
	}

	ctx := gen.BuildClientContext(tables, nil, "db", "Client", false, nil, cfg, tenancyMap)

	// Both Order and User must receive the resolver wiring: Order because of
	// its tenanted o2o child, User because it is itself tenanted.
	want := []string{"order", "user"}
	if len(ctx.TenantedEntities) != len(want) {
		t.Fatalf("TenantedEntities = %v, want %v", ctx.TenantedEntities, want)
	}
	for i, name := range want {
		if ctx.TenantedEntities[i] != name {
			t.Errorf("TenantedEntities[%d] = %q, want %q", i, ctx.TenantedEntities[i], name)
		}
	}
}

// loadRelationships propagates SkipTenancy from the parent's
// CallOptions into the child's GetMany, so `SkipTenancy: true` at the parent
// call drops the auto-filter on the child too.
func TestTenancyTemplate_loadRelationships_propagatesSkipTenancy(t *testing.T) {
	tc := tenantedProductContext()
	tc.HasO2MRelationships = true
	tc.O2MRelationships = []gen.RelationshipContext{
		{
			Name: "reviews", Type: parser.OneToMany, TargetTable: "reviews", TargetSchema: "public",
			TargetStructName: "Review", FKColumn: "product_id", FKFieldName: "ProductID",
			FieldName: "Reviews", GoType: "[]*Review", JSONTag: "reviews", FKGoType: "uuid.UUID",
		},
	}

	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		// loadRelationships signature carries skipTenancy under tenancy.
		"func (c *productClient) loadRelationships(ctx context.Context, conn database.Querier, products []*Product, fo *ProductFieldOptions, skipTenancy bool) error {",
		// Call site passes options.SkipTenancy through.
		"if err := c.loadRelationships(ctx, conn, products, options.FieldOptions, options.SkipTenancy); err != nil {",
		// Child GetMany callback forwards the flag.
		"co.SkipTenancy = skipTenancy",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("loadRelationships SkipTenancy propagation missing %q\nfull output:\n%s", want, out)
		}
	}
}

// renderUpsertTenancy renders the upsert template under the given dialect.
// Upsert surfaces dialect-specific interactions (RETURNING vs LastInsertId,
// ON CONFLICT vs ON DUPLICATE KEY) so the single-dialect renderTenancyTableBody
// helper isn't sufficient for tenancy-upsert coverage.
func renderUpsertTenancy(t *testing.T, tc gen.TableContext, dialect sql.Dialect) string {
	t.Helper()
	tmpl := loadAllTableTemplates(t, dialect)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "table/upsert", tc); err != nil {
		t.Fatalf("executing table/upsert: %v", err)
	}
	return buf.String()
}

// tenantedProductUpsertContext extends tenantedProductContext with the Upsert
// operation + a PK conflict target so upsert-template tests can render.
func tenantedProductUpsertContext() gen.TableContext {
	tc := tenantedProductContext()
	tc.Operations.Upsert = true
	tc.ConflictTargets = []gen.ConflictTargetContext{
		{ConstantName: "ProductConflictPK", Columns: []string{"id"}, CoversPK: true, Comment: "PRIMARY KEY (id)"},
	}
	return tc
}

// Cross-dialect tenancy auto-set on Upsert: the resolve + mismatch + auto-set
// block is dialect-agnostic and must render identically for every dialect. The
// Create template covers this for INSERT; Upsert is a distinct path because it
// has its own tenant-block emission (see upsert.go.tmpl).
func TestTenancyTemplate_upsert_autoSetAcrossDialects(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
	}{
		{"postgres", sql.NewPostgresDialect()},
		{"mysql", sql.NewMySQLDialect()},
		{"sqlite", sql.NewSQLiteDialect()},
	}
	wants := []string{
		"if !options.SkipTenancy {",
		"resolvedTenant, apply, err := c.resolveTenant(ctx, options.Tenant)",
		`return nil, fmt.Errorf("upsert product: resolve tenant: %w", err)`,
		`if v, ok := input.WorkspaceID.Get(); ok && v != resolvedTenant {`,
		"return nil, tenancy.ErrMismatch",
		`columns = append(columns, "workspace_id")`,
		"args = append(args, resolvedTenant)",
		"m.Tenant = resolvedTenant",
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tc := tenantedProductUpsertContext()
			out := renderUpsertTenancy(t, tc, tt.dialect)
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s upsert output missing %q\nfull output:\n%s", tt.name, want, out)
				}
			}
		})
	}
}

// PRD §29.4.2: the SET clause of Upsert must exclude the tenant column — a
// row's tenant is not a mutable attribute under normal operation. The
// emission needs to name the tenant column in the excludeColumns exclusion
// list alongside the PK columns, across every dialect (the ON CONFLICT /
// ON DUPLICATE KEY surface differs, but the Go-level updateColumns variable
// is what drives both).
func TestTenancyTemplate_upsert_tenantExcludedFromUpdateSet(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
	}{
		{"postgres", sql.NewPostgresDialect()},
		{"mysql", sql.NewMySQLDialect()},
		{"sqlite", sql.NewSQLiteDialect()},
	}
	// The exclusion list literal must carry "workspace_id" alongside the PK
	// column "id". We assert the substring rather than the full line so the
	// test survives trivial formatting changes while still pinning the
	// dialect-invariant invariant.
	wantExclusion := `append(conflictColumns, "id", "workspace_id")`
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tc := tenantedProductUpsertContext()
			out := renderUpsertTenancy(t, tc, tt.dialect)
			if !strings.Contains(out, wantExclusion) {
				t.Errorf("%s upsert missing tenant from exclusion list; want substring %q\nfull output:\n%s",
					tt.name, wantExclusion, out)
			}
		})
	}
}

// Shared-table upsert in a tenancy-enabled project must continue to emit the
// plain exclusion list — no tenancy-block resolve/mismatch code path, no
// widening of the exclusion literal. Guards against a template regression
// where .Tenancy being non-nil but .Tenancy.Tenanted=false accidentally opens
// the tenancy branch.
func TestTenancyTemplate_upsert_sharedTableOmitsTenancy(t *testing.T) {
	tc := tenantedProductUpsertContext()
	tc.Tenancy = &gen.TableTenancyContext{Tenanted: false}

	out := renderUpsertTenancy(t, tc, sql.NewPostgresDialect())

	// Only the tenancy-block emissions are under test; generic omittable-field
	// iteration on a `workspace_id` input column is unrelated (it would not
	// appear on a real shared table, which would not carry the field at all).
	mustNotContain := []string{
		"resolvedTenant",
		"tenancy.ErrMismatch",
		`, "workspace_id"))`,
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("shared-table upsert unexpectedly contains %q\nfull output:\n%s", s, out)
		}
	}
	if !strings.Contains(out, `append(conflictColumns, "id")`) {
		t.Errorf("shared-table upsert missing plain PK-only exclusion list\nfull output:\n%s", out)
	}
}

// SkipTenancy on Upsert must take the caller-supplied tenant value verbatim
// — the resolver is not consulted, and no mismatch check runs. Mirrors the
// Create-path SkipTenancy branch.
func TestTenancyTemplate_upsert_skipTenancyUsesExplicitTenant(t *testing.T) {
	tc := tenantedProductUpsertContext()
	out := renderUpsertTenancy(t, tc, sql.NewPostgresDialect())

	wants := []string{
		"} else if v, ok := input.WorkspaceID.Get(); ok {",
		`columns = append(columns, "workspace_id")`,
		"args = append(args, v)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("SkipTenancy upsert branch missing %q\nfull output:\n%s", want, out)
		}
	}
}

// When tenancy is not configured, BuildClientContext must emit no tenancy
// surface — non-tenancy projects regenerate byte-identically (§29 regression).
func TestBuildClientContext_tenancyDisabledProducesNoWiring(t *testing.T) {
	tables := []gen.TableContext{{StructName: "Product"}}
	ctx := gen.BuildClientContext(tables, nil, "db", "Client", false, nil, nil, nil)

	if ctx.TenancyEnabled {
		t.Errorf("TenancyEnabled = true, want false")
	}
	if ctx.TenancyGoType != "" {
		t.Errorf("TenancyGoType = %q, want empty", ctx.TenancyGoType)
	}
	if ctx.TenantedEntities != nil {
		t.Errorf("TenantedEntities = %v, want nil", ctx.TenantedEntities)
	}
	for _, imp := range ctx.Imports {
		if strings.Contains(imp, "tenancy") {
			t.Errorf("Imports unexpectedly contain tenancy: %v", ctx.Imports)
			break
		}
	}
}

// tenantInPKOrderItemContext returns a TableContext for order_items whose
// workspace_id column participates in the composite primary key — the PRD §29.7
// shape. Used by the tenant-in-PK verify-match template tests.
func tenantInPKOrderItemContext() gen.TableContext {
	wsCol := gen.ColumnContext{Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID", DBTag: "workspace_id", JSONTag: "workspace_id", PrimaryKey: true, Import: "github.com/google/uuid"}
	orderCol := gen.ColumnContext{Name: "order_id", FieldName: "OrderID", GoType: "int64", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true}
	productCol := gen.ColumnContext{Name: "product_id", FieldName: "ProductID", GoType: "int64", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true}
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "",
		Package:               "models",
		VarName:               "oi",
		Dialect:               "sqlite",
		Driver:                "stdlib",
		PKStrategy:            config.PKStrategyDB,
		BatchSize:             100,
		QueryLimit:            100,
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Imports: []string{
			"context",
			"fmt",
			"github.com/google/uuid",
			"github.com/teandresmith/sqlgen/database",
			"github.com/teandresmith/sqlgen/sql",
			"github.com/teandresmith/sqlgen/tenancy",
		},
		PKColumns: []gen.ColumnContext{wsCol, orderCol, productCol},
		Columns: []gen.ColumnContext{
			wsCol, orderCol, productCol,
			{Name: "quantity", FieldName: "Quantity", GoType: "int64", DBTag: "quantity", JSONTag: "quantity"},
			{Name: "unit_price", FieldName: "UnitPrice", GoType: "float64", DBTag: "unit_price", JSONTag: "unit_price"},
		},
		// Tenant-in-PK: the tenant column stays required on CreateInput (no
		// omittable promotion) because the caller always supplies it.
		CreateInputFields: []gen.InputFieldContext{
			{FieldName: "WorkspaceID", GoType: "uuid.UUID", ColumnName: "workspace_id", Required: true, JSONTag: "workspace_id"},
			{FieldName: "OrderID", GoType: "int64", ColumnName: "order_id", Required: true, JSONTag: "order_id"},
			{FieldName: "ProductID", GoType: "int64", ColumnName: "product_id", Required: true, JSONTag: "product_id"},
			{FieldName: "Quantity", GoType: "int64", ColumnName: "quantity", Required: true, JSONTag: "quantity"},
			{FieldName: "UnitPrice", GoType: "float64", ColumnName: "unit_price", Required: true, JSONTag: "unit_price"},
		},
		// UpdateInputFields excludes PK columns.
		UpdateInputFields: []gen.InputFieldContext{
			{FieldName: "Quantity", GoType: "omittable.Value[int64]", ColumnName: "quantity", Omittable: true, JSONTag: "quantity"},
			{FieldName: "UnitPrice", GoType: "omittable.Value[float64]", ColumnName: "unit_price", Omittable: true, JSONTag: "unit_price"},
		},
		AllColumnNames: []string{"order_id", "product_id", "quantity", "unit_price", "workspace_id"},
		Operations: gen.ResolvedOperations{
			Get:        true,
			GetMany:    true,
			Create:     true,
			CreateMany: true,
			Update:     true,
			UpdateMany: true,
			HardDelete: true,
			Exists:     true,
			Count:      true,
		},
		Tenancy: &gen.TableTenancyContext{
			Tenanted:     true,
			Column:       "workspace_id",
			FieldName:    "WorkspaceID",
			GoType:       "uuid.UUID",
			Import:       "github.com/google/uuid",
			Required:     true,
			InPrimaryKey: true,
		},
	}
}

// TestTenancyTemplate_tenantInPK_getEmitsVerifyMatch asserts Get on a
// composite-PK table whose tenant column lives inside the PK emits the
// verify-match block (`pk.WorkspaceID != resolvedTenant`) — PRD §29.7
// Option-2 rule.
func TestTenancyTemplate_tenantInPK_getEmitsVerifyMatch(t *testing.T) {
	tc := tenantInPKOrderItemContext()
	out := renderTenancyTableBody(t, tc, "table/get")

	wants := []string{
		"if !options.SkipTenancy {",
		"resolvedTenant, apply, err := c.resolveTenant(ctx, options.Tenant)",
		"if pk.WorkspaceID != resolvedTenant {",
		"return nil, tenancy.ErrMismatch",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("tenant-in-PK Get output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// TestTenancyTemplate_tenantInPK_updateEmitsPKVerifyOnly asserts Update on a
// tenant-in-PK table verifies pk.WorkspaceID (not input.WorkspaceID — which
// doesn't exist because UpdateInput excludes PK columns).
func TestTenancyTemplate_tenantInPK_updateEmitsPKVerifyOnly(t *testing.T) {
	tc := tenantInPKOrderItemContext()
	out := renderTenancyTableBody(t, tc, "table/update")

	wants := []string{
		"if pk.WorkspaceID != resolvedTenant {",
		"return nil, tenancy.ErrMismatch",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("tenant-in-PK Update missing %q\nfull output:\n%s", want, out)
		}
	}
	// The input-side check must NOT appear — the tenant field doesn't exist
	// on UpdateInput when it's in the PK.
	if strings.Contains(out, "input.WorkspaceID.Get()") {
		t.Errorf("tenant-in-PK Update must not reference input.WorkspaceID.Get() (field excluded from UpdateInput)\nfull output:\n%s", out)
	}
}

// TestTenancyTemplate_tenantInPK_createVerifiesRequiredField asserts Create on
// a tenant-in-PK table treats WorkspaceID as a required plain field (not
// omittable) and emits a direct equality check against the resolver.
func TestTenancyTemplate_tenantInPK_createVerifiesRequiredField(t *testing.T) {
	tc := tenantInPKOrderItemContext()
	out := renderTenancyTableBody(t, tc, "table/create")

	wants := []string{
		"if input.WorkspaceID != resolvedTenant {",
		"return nil, tenancy.ErrMismatch",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("tenant-in-PK Create missing %q\nfull output:\n%s", want, out)
		}
	}
	// The omittable .Get() path must NOT appear on the tenant-in-PK Create —
	// the field is plain, not omittable.
	if strings.Contains(out, "input.WorkspaceID.Get()") {
		t.Errorf("tenant-in-PK Create must not reference input.WorkspaceID.Get() (field is required, not omittable)\nfull output:\n%s", out)
	}
}

// TestTenancyTemplate_tenantInPK_deleteVariantsEmitVerify asserts each PK-
// scoped delete variant emits the verify-match block. order_items has no
// soft-delete so only HardDelete / HardDeleteMany apply.
func TestTenancyTemplate_tenantInPK_deleteVariantsEmitVerify(t *testing.T) {
	tc := tenantInPKOrderItemContext()
	out := renderTenancyTableBody(t, tc, "table/delete")

	wants := []string{
		// HardDelete (single PK).
		"if pk.WorkspaceID != resolvedTenant {",
		// HardDeleteMany (batch, per-item loop).
		"for i := range pks {",
		"if pks[i].WorkspaceID != resolvedTenant {",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("tenant-in-PK delete variants missing %q\nfull output:\n%s", want, out)
		}
	}
}

// Every chaining op template stashes the resolved tenant on ctx
// via tenancy.WithResolvedTenant immediately after its own resolve, so
// the chained terminal Get / GetMany short-circuits via tenancy.CachedTenant
// instead of re-invoking the user resolver. The corresponding short-circuit
// in resolveTenant itself is asserted by
// TestTenancyTemplate_client_resolveTenantShortCircuitsOnCachedValue below.
func TestTenancyTemplate_chainCachesResolvedTenant(t *testing.T) {
	tc := tenantedProductUpsertContext()
	tc.Operations.UpdateMany = true
	tc.Operations.UpdateWhere = true
	tc.Operations.SoftDelete = true
	tc.Operations.Restore = true
	tc.SoftDelete = &gen.SoftDeleteContext{
		Column:    "deleted_at",
		FieldName: "DeletedAt",
		Strategy:  "timestamp",
	}
	tc.FilterFields = append(tc.FilterFields, gen.FilterFieldContext{
		FieldName:          "DeletedAt",
		IsSoftDeleteColumn: true,
		Filterable:         true,
	})

	cases := []struct {
		template string
	}{
		{"table/create"},
		{"table/upsert"},
		{"table/update"},
		{"table/delete"},
	}
	// The stashed value is the resolver's
	// raw answer, with no `apply` — that bool is the table's reading of the
	// answer and cannot be shared across tables that set `required`
	// differently — and an explicit CallOptions.Tenant is not stashed at all,
	// because it rides on CallOptions and resolveTenant honours it first.
	wants := []string{
		"if options.Tenant == nil {",
		"ctx = tenancy.WithResolvedTenant(ctx, resolvedTenant)",
	}
	for _, tt := range cases {
		t.Run(tt.template, func(t *testing.T) {
			out := renderTenancyTableBody(t, tc, tt.template)
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s output missing cache attach %q\nfull output:\n%s", tt.template, want, out)
				}
			}
			if bad := "tenancy.WithResolvedTenant(ctx, resolvedTenant, apply)"; strings.Contains(out, bad) {
				t.Errorf("%s stashes the per-table apply decision %q; only the resolver's answer may be cached", tt.template, bad)
			}
		})
	}
}

// resolveTenant on every tenanted client begins with a
// tenancy.CachedTenant lookup so a chained terminal call (Create → Get →
// GetMany) reuses the parent's resolved value rather than re-invoking the
// user resolver.
func TestTenancyTemplate_client_resolveTenantShortCircuitsOnCachedValue(t *testing.T) {
	tc := tenantedProductContext()
	out := renderTenancyTableBody(t, tc, "table/client")

	// The lookup lives in tenantValue: resolveTenant asks it
	// what the resolver said, then applies this table's own required branch.
	// Keeping the two apart is the fix — a cached `apply` is one table's
	// reading of the answer, and the ctx slot is shared by the whole package.
	wants := []string{
		"func (c *productClient) tenantValue(ctx context.Context) (uuid.UUID, error) {",
		"if v, ok := tenancy.CachedTenant[uuid.UUID](ctx); ok {",
		"return v, nil",
		"v, err := c.tenantValue(ctx)",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("resolveTenant output missing cache short-circuit %q\nfull output:\n%s", want, out)
		}
	}
	if bad := "tenancy.CachedTenant[uuid.UUID](ctx); ok {\n\t\treturn v, apply, nil"; strings.Contains(out, bad) {
		t.Errorf("resolveTenant returns a cached apply decision; each table must apply its own required branch")
	}
}

// TestTenancyTemplate_tenantInPK_existsEmitsVerify asserts Exists emits the
// verify-match block for tenant-in-PK tables.
func TestTenancyTemplate_tenantInPK_existsEmitsVerify(t *testing.T) {
	tc := tenantInPKOrderItemContext()
	out := renderTenancyTableBody(t, tc, "table/exists")

	wants := []string{
		"if pk.WorkspaceID != resolvedTenant {",
		"return false, tenancy.ErrMismatch",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("tenant-in-PK Exists missing %q\nfull output:\n%s", want, out)
		}
	}
}
