package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// --- helpers ---

// testViewContext_tenanted returns the product_summary view context with a
// workspace_id tenant column attached, matching what attachTenancyToViews
// produces for a detected tenanted view (PRD §29.2.5).
func testViewContext_tenanted() gen.ViewContext {
	ctx := testViewContext_withPK()
	ctx.Columns = append(ctx.Columns, gen.ColumnContext{
		Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID",
		DBTag: "workspace_id", JSONTag: "workspace_id", Import: "github.com/gofrs/uuid/v5",
	})
	ctx.AllColumnNames = append(ctx.AllColumnNames, "workspace_id")
	ctx.ScanShapes = append(ctx.ScanShapes, gen.ScanShapeContext{
		ColumnName: "workspace_id", FieldName: "WorkspaceID", Shape: "direct", ScanExpr: "&p.WorkspaceID",
	})
	ctx.FilterFields = append(ctx.FilterFields, gen.FilterFieldContext{
		FieldName: "WorkspaceID", ComparatorType: "*comparator.ID", ColumnName: "workspace_id", Filterable: true,
	})
	ctx.Imports = gen.UniqueImports(append(ctx.Imports, "github.com/teandresmith/sqlgen/tenancy"))
	ctx.Tenancy = &gen.TableTenancyContext{
		Tenanted:  true,
		Column:    "workspace_id",
		FieldName: "WorkspaceID",
		GoType:    "uuid.UUID",
		Import:    "github.com/gofrs/uuid/v5",
		Required:  true,
	}
	return ctx
}

// testViewContext_shared returns the same view in a tenancy-enabled package
// that opted the view out — the shape attachTenancyToViews produces for a view
// with no tenant column or `views.<n>.tenancy.enabled: false`.
func testViewContext_shared() gen.ViewContext {
	ctx := testViewContext_withPK()
	ctx.Tenancy = &gen.TableTenancyContext{Tenanted: false}
	return ctx
}

// tenantPredicate is the exact emission the read templates must produce.
const tenantPredicate = `conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`

// --- tests ---

// TestViewTenancy_readMethodsEmitPredicate pins view tenant scoping: every one
// of the five view read methods is tenant-scoped (PRD §29.4.1 / §29.2.5).
// GetMany and Count inject directly; Get, Paginate and Connection funnel
// through them, so the assertion for those three is that they reach the
// injecting method with the caller's CallOptions intact.
func TestViewTenancy_readMethodsEmitPredicate(t *testing.T) {
	ctx := testViewContext_tenanted()

	getOut := executeViewGetTemplate(t, ctx)
	if !strings.Contains(getOut, tenantPredicate) {
		t.Errorf("GetMany does not emit the tenant predicate:\n%s", getOut)
	}
	if !strings.Contains(getOut, `if !options.SkipTenancy {`) {
		t.Errorf("GetMany tenancy block is not gated on SkipTenancy:\n%s", getOut)
	}
	if !strings.Contains(getOut, `c.resolveTenant(ctx, options.Tenant)`) {
		t.Errorf("GetMany does not pass CallOptions.Tenant to resolveTenant:\n%s", getOut)
	}
	if !strings.Contains(getOut, `fmt.Errorf("get product_summary: resolve tenant: %w", err)`) {
		t.Errorf("GetMany resolver error is not wrapped with the view name:\n%s", getOut)
	}
	// Get delegates to GetMany with the caller's options copied verbatim, so
	// the predicate reaches the PK read too.
	if !strings.Contains(getOut, "*o = options") {
		t.Errorf("Get does not forward CallOptions to the internal GetMany:\n%s", getOut)
	}

	countOut := executeViewCountTemplate(t, ctx)
	if !strings.Contains(countOut, tenantPredicate) {
		t.Errorf("Count does not emit the tenant predicate:\n%s", countOut)
	}
	if !strings.Contains(countOut, `fmt.Errorf("count product_summary: resolve tenant: %w", err)`) {
		t.Errorf("Count resolver error is not wrapped with the view name:\n%s", countOut)
	}

	pageOut := executeViewPaginationTemplate(t, ctx)
	// Paginate and Connection compose Count + GetMany. A second injection here
	// would double the predicate and add a redundant bind, so the correct
	// emission is none — the table path (templates/table/pagination.go.tmpl)
	// has no tenancy block for the same reason.
	if strings.Contains(pageOut, tenantPredicate) {
		t.Errorf("Paginate/Connection duplicate the tenant predicate already applied by Count/GetMany:\n%s", pageOut)
	}
	if got := strings.Count(pageOut, "*o = options"); got != 2 {
		t.Errorf("internalOpts forwarding = %d occurrences, want 2 (Paginate + Connection)", got)
	}
}

// TestViewTenancy_clientEmitsResolver pins the resolver field and the ported
// resolveTenant method, and asserts none of the mutation-only half of the
// table client (captureAffectedTenants) came along (PRD §29.2.5).
func TestViewTenancy_clientEmitsResolver(t *testing.T) {
	out := executeViewClientTemplate(t, testViewContext_tenanted())

	if !strings.Contains(out, "tenantResolver tenancy.TenantResolver[uuid.UUID]") {
		t.Errorf("view client struct is missing the tenantResolver field:\n%s", out)
	}
	if !strings.Contains(out, "func (c *productSummaryClient) resolveTenant(ctx context.Context, explicit *uuid.UUID) (uuid.UUID, bool, error)") {
		t.Errorf("view client is missing resolveTenant:\n%s", out)
	}
	if !strings.Contains(out, "tenancy.CachedTenant[uuid.UUID](ctx)") {
		t.Errorf("resolveTenant does not honor the §29.6 resolved-tenant cache:\n%s", out)
	}
	if strings.Contains(out, "captureAffectedTenants") {
		t.Errorf("view client emitted the mutation-only captureAffectedTenants:\n%s", out)
	}
}

// TestViewTenancy_sharedViewOmitsPredicate asserts a view that is not tenanted
// regenerates exactly as it did before tenancy existed — both the
// tenancy-globally-disabled shape (Tenancy nil) and the opted-out shape
// (Tenancy non-nil, Tenanted false).
func TestViewTenancy_sharedViewOmitsPredicate(t *testing.T) {
	tests := []struct {
		name string
		ctx  gen.ViewContext
	}{
		{"tenancy disabled", testViewContext_withPK()},
		{"view opted out", testViewContext_shared()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, out := range []string{
				executeViewClientTemplate(t, tt.ctx),
				executeViewGetTemplate(t, tt.ctx),
				executeViewCountTemplate(t, tt.ctx),
				executeViewPaginationTemplate(t, tt.ctx),
			} {
				for _, token := range []string{"resolveTenant", "tenantResolver", "SkipTenancy", "Tenancy"} {
					if strings.Contains(out, token) {
						t.Errorf("shared view emitted %q:\n%s", token, out)
					}
				}
			}
		})
	}
}

// TestViewTenancy_sharedViewOutputUnchanged is the byte-identical guarantee:
// attaching a non-tenanted TableTenancyContext to a view must not perturb its
// generated output at all.
func TestViewTenancy_sharedViewOutputUnchanged(t *testing.T) {
	before := testViewContext_withPK()
	after := testViewContext_shared()

	for _, tt := range []struct {
		name string
		exec func(*testing.T, gen.ViewContext) string
	}{
		{"client", executeViewClientTemplate},
		{"get", executeViewGetTemplate},
		{"count", executeViewCountTemplate},
		{"pagination", executeViewPaginationTemplate},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := tt.exec(t, after), tt.exec(t, before); got != want {
				t.Errorf("opted-out view output differs from tenancy-disabled output:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestViewTenancy_refreshEmitsNoPredicate pins PRD §29.2.5: REFRESH
// MATERIALIZED VIEW recomputes the whole relation and is never scoped.
func TestViewTenancy_refreshEmitsNoPredicate(t *testing.T) {
	ctx := testViewContext_tenanted()
	ctx.Materialized = true
	ctx.ConcurrentlyRefreshable = true

	out := executeViewRefreshTemplate(t, ctx)
	if !strings.Contains(out, "func (c *productSummaryClient) Refresh(") ||
		!strings.Contains(out, "func (c *productSummaryClient) RefreshConcurrently(") {
		t.Fatalf("refresh template did not emit both refresh methods:\n%s", out)
	}
	for _, token := range []string{"resolveTenant", "SkipTenancy", "workspace_id", "tenancy."} {
		if strings.Contains(out, token) {
			t.Errorf("matview refresh emitted %q — refresh is never tenant-scoped:\n%s", token, out)
		}
	}
}

// TestViewTenancy_requiredBranch pins the fail-closed / fall-open split, which
// is chosen at template-expansion time from tenancy.required (PRD §29.3.1).
func TestViewTenancy_requiredBranch(t *testing.T) {
	tests := []struct {
		name     string
		required bool
		want     string
		notWant  string
	}{
		{"required true fails closed", true, "return zero, false, tenancy.ErrMissing", ""},
		{"required false falls open", false, "return zero, false, nil", "return zero, false, tenancy.ErrMissing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testViewContext_tenanted()
			ctx.Tenancy.Required = tt.required
			out := executeViewClientTemplate(t, ctx)
			if !strings.Contains(out, tt.want) {
				t.Errorf("resolveTenant missing %q:\n%s", tt.want, out)
			}
			if tt.notWant != "" && strings.Contains(out, tt.notWant) {
				t.Errorf("resolveTenant emitted %q under required:false:\n%s", tt.notWant, out)
			}
		})
	}
}

// TestViewTenancy_pkTenantColumnStaysAutoFilter guards a trap: a view's @pk
// annotation selects a Get signature, it does not make the column a DDL
// primary key, so the table path's §29.7 verify-match must not be ported. The view stays plain-filtered (PRD §30.4.2).
func TestViewTenancy_pkTenantColumnStaysAutoFilter(t *testing.T) {
	ctx := testViewContext_tenanted()
	ctx.CompositePK = true
	ctx.CompositePKStructName = "ProductSummaryPK"
	ctx.PKColumns = append(ctx.PKColumns, gen.ColumnContext{
		Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID",
		DBTag: "workspace_id", JSONTag: "workspace_id", Import: "github.com/gofrs/uuid/v5",
	})
	// attachTenancyToViews never sets InPrimaryKey, but pin the emission
	// against a context that does — the templates must not read it.
	ctx.Tenancy.InPrimaryKey = true

	out := executeViewGetTemplate(t, ctx)
	if strings.Contains(out, "tenancy.ErrMismatch") {
		t.Errorf("view Get emitted the §29.7 verify-match, which has no read-only analogue:\n%s", out)
	}
	if !strings.Contains(out, tenantPredicate) {
		t.Errorf("view with a @pk tenant column lost its auto-filter:\n%s", out)
	}
}

// TestViewTenancy_predicateOrderIsStable pins the placeholder-numbering
// property Connection depends on: the tenant condition is appended after the
// filter conditions and before input.conditions (the keyset predicate), the
// same order the table path uses.
func TestViewTenancy_predicateOrderIsStable(t *testing.T) {
	out := executeViewGetTemplate(t, testViewContext_tenanted())

	filterIdx := strings.Index(out, "conds = input.Filter.ToConditions(c.dialect)")
	tenantIdx := strings.Index(out, tenantPredicate)
	keysetIdx := strings.Index(out, "conds = append(conds, input.conditions...)")

	if filterIdx < 0 || tenantIdx < 0 || keysetIdx < 0 {
		t.Fatalf("expected all three condition sources in GetMany:\n%s", out)
	}
	if filterIdx >= tenantIdx || tenantIdx >= keysetIdx {
		t.Errorf("condition order = filter:%d tenant:%d keyset:%d, want filter < tenant < keyset", filterIdx, tenantIdx, keysetIdx)
	}
}
