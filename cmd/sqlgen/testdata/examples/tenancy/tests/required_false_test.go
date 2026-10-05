package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// required:false regression guard.
//
// The tenancy example configures `legacy_widgets.tenancy.required: false`
// (see sqlgen.yml). Per PRD §29.2 line 10933:
//
//   "When false, missing tenant skips the filter — effectively becomes a
//   cross-tenant query."
//
// A generator that ignored the Required flag would unconditionally call
// resolveTenant and append the tenant filter, so a zero-value resolver would
// produce WHERE org_id = '00000000-...-0' instead of the intended no-filter.
// resolveTenant therefore returns (value, apply bool, err) and the generator
// gates the filter/verify-match/auto-set on `apply`. Under required:false + zero resolver, apply=false and err=nil.
//
// These tests cover the three paths:
//   1. Zero resolver + required:false → no filter in SQL, call succeeds
//      (cross-tenant by design).
//   2. Non-zero resolver + required:false → filter applied normally
//      (isolation preserved).
//   3. Non-zero-resolver-returning-error + required:false → error propagates
//      (required:false does NOT silently swallow resolver errors).

// seedWidget creates one legacy widget under the given org_id via an env
// with a static resolver returning that value. The seeding env is discarded
// after the helper returns.
func seedWidget(t *testing.T, org uuid.UUID, label string) *models.LegacyWidget {
	t.Helper()
	env := newEnv(t, withResolver(staticResolver(org)))
	w, err := env.client.LegacyWidgets().Create(context.Background(), &models.CreateLegacyWidgetInput{
		Label: label,
	})
	if err != nil {
		t.Fatalf("seed legacy widget (org %v): %v", org, err)
	}
	return w
}

// newCapturingLegacyClient is a small wrapper around newCapturingClient that
// returns the client already registered against the capturing querier. Reused
// across the required_false tests.
func newCapturingLegacyClient(t *testing.T, resolver tenancy.TenantResolver[uuid.UUID]) (*models.Client, *capturingQuerier) {
	t.Helper()
	return newCapturingClient(t, resolver)
}

// TestRequiredFalse_ZeroResolver_SkipsFilterOnGetMany is the headline
// regression guard: under required:false, a resolver returning (uuid.Nil(), nil)
// causes GetMany to omit the org_id predicate from the generated SELECT. The
// captured SQL is inspected directly — a builder regression that re-introduces
// a `WHERE org_id = ?` predicate against uuid.Nil (ignoring required:false) would
// fail the assertion.
func TestRequiredFalse_ZeroResolver_SkipsFilterOnGetMany(t *testing.T) {
	resetDB(t)

	// Seed one widget per tenant so a genuinely cross-tenant query has rows
	// on both sides to discover.
	_ = seedWidget(t, tenantA, "A widget")
	_ = seedWidget(t, tenantB, "B widget")

	// Zero-returning resolver under required:false → filter skipped.
	client, cap := newCapturingLegacyClient(t, func(context.Context) (uuid.UUID, error) {
		return uuid.Nil(), nil
	})

	cap.reset()
	widgets, err := client.LegacyWidgets().GetMany(context.Background(), &models.GetLegacyWidgetsInput{})
	if err != nil {
		t.Fatalf("GetMany under required:false + zero resolver: %v", err)
	}
	if len(widgets) != 2 {
		t.Errorf("cross-tenant GetMany: got %d widgets, want 2 (both tenants' rows)", len(widgets))
	}

	// Captured SELECT must not contain an org_id predicate in its WHERE.
	// Identifier quoting stripped so both `"org_id" = ?` and `org_id = ?`
	// collapse to the same token (same rule as countWhereToken in
	// composite_pk_mismatch_test.go).
	lastSQL := lastCapturedSelect(t, cap)
	if strings.Contains(strings.ReplaceAll(strings.ToLower(lastSQL), "\"", ""), "org_id = ") {
		t.Errorf("captured SELECT contains org_id predicate; expected none under required:false + zero resolver\nSQL: %s", lastSQL)
	}
}

// TestRequiredFalse_NonZeroResolver_AppliesFilter is the companion to the
// zero-resolver guard: when the resolver returns a real tenant value,
// required:false does NOT drop the filter. Both halves of required:false —
// the zero-value branch and the non-zero branch — must be correct for isolation
// to survive.
func TestRequiredFalse_NonZeroResolver_AppliesFilter(t *testing.T) {
	resetDB(t)

	_ = seedWidget(t, tenantA, "A widget")
	_ = seedWidget(t, tenantB, "B widget")

	// Non-zero resolver under required:false → filter still applied.
	client, cap := newCapturingLegacyClient(t, staticResolver(tenantA))

	cap.reset()
	widgets, err := client.LegacyWidgets().GetMany(context.Background(), &models.GetLegacyWidgetsInput{})
	if err != nil {
		t.Fatalf("GetMany under required:false + tenant A resolver: %v", err)
	}
	if len(widgets) != 1 {
		t.Errorf("tenant-scoped GetMany: got %d widgets, want 1 (tenant A only)", len(widgets))
	}
	if len(widgets) == 1 && widgets[0].OrgID != tenantA {
		t.Errorf("tenant-scoped GetMany: org_id = %v, want %v", widgets[0].OrgID, tenantA)
	}

	// Captured SELECT MUST contain the org_id predicate.
	lastSQL := lastCapturedSelect(t, cap)
	if !strings.Contains(strings.ReplaceAll(strings.ToLower(lastSQL), "\"", ""), "org_id = ") {
		t.Errorf("captured SELECT missing org_id predicate; expected it under required:false + non-zero resolver\nSQL: %s", lastSQL)
	}
}

// TestRequiredFalse_ResolverError_Propagates verifies required:false doesn't
// silently swallow errors returned by the resolver — only zero-value-with-
// nil-error is treated as "skip filter". A resolver returning a non-nil
// error (e.g., ctx missing) still propagates up wrapped with the per-op
// "resolve tenant:" prefix.
func TestRequiredFalse_ResolverError_Propagates(t *testing.T) {
	resetDB(t)

	wantErr := errors.New("resolver failed")
	client, _ := newCapturingLegacyClient(t, func(context.Context) (uuid.UUID, error) {
		return uuid.Nil(), wantErr
	})

	_, err := client.LegacyWidgets().GetMany(context.Background(), &models.GetLegacyWidgetsInput{})
	if err == nil {
		t.Fatalf("GetMany with erroring resolver: err = nil, want wrap of %v", wantErr)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("GetMany with erroring resolver: err = %v, want wraps %v", err, wantErr)
	}
}

// TestRequiredFalse_CreateUsesCallerValueWhenResolverZero covers the
// Create path under required:false + zero resolver: the auto-set block
// doesn't fire, so the caller-supplied OrgID on the input is used as-is.
// When the caller also omits OrgID, the DB's NOT NULL constraint surfaces —
// tested via a separate subtest to distinguish "caller forgot" from "caller
// supplied".
func TestRequiredFalse_CreateUsesCallerValueWhenResolverZero(t *testing.T) {
	resetDB(t)

	// Zero-returning resolver + caller-supplied OrgID → row created under
	// the caller's supplied tenant.
	envZero := newEnv(t, withResolver(func(context.Context) (uuid.UUID, error) {
		return uuid.Nil(), nil
	}))
	w, err := envZero.client.LegacyWidgets().Create(context.Background(), &models.CreateLegacyWidgetInput{
		OrgID: omittable.Set(tenantA),
		Label: "caller-supplied",
	})
	if err != nil {
		t.Fatalf("Create under required:false + zero resolver + caller OrgID: %v", err)
	}
	if w.OrgID != tenantA {
		t.Errorf("caller-supplied OrgID not honored: got %v, want %v", w.OrgID, tenantA)
	}

	t.Run("ForgettingOrgIDErrors", func(t *testing.T) {
		// No caller-supplied OrgID, zero resolver → the generator omits
		// org_id from the INSERT (tenancy is opted-out for this call), and
		// the DB's NOT NULL constraint surfaces as the error. This is the
		// documented required:false semantic — caller is responsible for
		// the tenant when the resolver bows out, and "forgot to set it"
		// becomes a loud DB-level error (not a silent zero-tenant write,
		// since uuid.Nil wasn't written either).
		_, err := envZero.client.LegacyWidgets().Create(context.Background(), &models.CreateLegacyWidgetInput{
			Label: "no-org-supplied",
		})
		if err == nil {
			t.Fatalf("expected NOT NULL constraint violation, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "org_id") {
			t.Errorf("error message = %q, want to contain 'org_id'", err.Error())
		}
	})
}

// TestRequiredTrue_ZeroResolver_StillErrMissing is a negative-control
// regression guard: other tables in the tenancy example inherit the global
// `tenancy.required: true`, so their fail-closed behavior must be
// unchanged by the required:false handling. products (workspace_id,
// required:true) under a zero resolver still returns tenancy.ErrMissing.
func TestRequiredTrue_ZeroResolver_StillErrMissing(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(func(context.Context) (uuid.UUID, error) {
		return uuid.Nil(), nil
	}))

	env.counter.reset()
	_, err := env.client.Products().GetMany(context.Background(), &models.GetProductsInput{})
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("products GetMany under required:true + zero resolver: err = %v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on required:true fail-closed path: %d, want 0", got)
	}
}

// lastCapturedSelect returns the last SELECT recorded by the capturing
// querier. Fails the test if no SELECT was captured.
func lastCapturedSelect(t *testing.T, c *capturingQuerier) string {
	t.Helper()
	sqls := c.snapshot()
	for i := len(sqls) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[i])), "SELECT") {
			return sqls[i]
		}
	}
	t.Fatalf("no SELECT captured; captured SQL = %v", sqls)
	return ""
}

// TestCachedZeroTenant_DoesNotSuppressAFailClosedTable pins the
// property that makes the nested-mutation hoist safe: the shared ctx cache
// carries the resolver's answer, not one table's reading of it.
//
// The ctx cache is keyed on the tenant type, so it is one slot shared by every
// entity client in the package. It used to carry the generated resolveTenant's
// `apply` bool — this table's *reading* of the resolver's answer, which folds in
// its own tenancy.required setting. Sharing a reading rather than an answer let
// whichever table resolved first impose its `required` on the rest: a
// required:false table that resolved to the zero value stashed apply=false, and
// every required:true table downstream read it and skipped the ErrMissing it
// owed — writing rows with a caller-supplied tenant that nothing checked.
//
// `legacy_widgets` is the package's one required:false table and `products` is
// required:true, which is the divergence the slot has to survive. The cache is
// seeded directly because the two tables share no edge to chain over; that is
// exactly the value a nested hoist, or any chaining entry point, would leave on
// ctx after a zero resolve.
func TestCachedZeroTenant_DoesNotSuppressAFailClosedTable(t *testing.T) {
	resetDB(t)

	var zero uuid.UUID
	env := newEnv(t, withResolver(staticResolver(zero)))
	ctx := tenancy.WithResolvedTenant(context.Background(), zero)

	t.Run("required:true still fails closed", func(t *testing.T) {
		_, err := env.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "cached-zero", SKU: "CZ-00", Price: 1,
		})
		if !errors.Is(err, tenancy.ErrMissing) {
			t.Errorf("Create on a required:true table over a cached zero tenant = %v, want tenancy.ErrMissing", err)
		}
	})

	t.Run("required:false still skips the filter", func(t *testing.T) {
		if _, err := env.client.LegacyWidgets().Create(ctx, &models.CreateLegacyWidgetInput{
			Label: "cached-zero-widget", OrgID: omittable.Set(uuid.MustParse("11111111-1111-1111-1111-111111111111")),
		}); err != nil {
			t.Errorf("Create on a required:false table over a cached zero tenant = %v, want success", err)
		}
	})
}

// TestCachedTenant_IsReusedWithoutReResolving is the other half: a cached
// NON-zero value short-circuits the resolver on any client, which is what makes
// PRD §29.6's once-per-operation bound reachable across entity boundaries.
func TestCachedTenant_IsReusedWithoutReResolving(t *testing.T) {
	resetDB(t)

	cr := newCountingResolver(staticResolver(tenantA))
	env := newEnv(t, withResolver(cr.resolve))
	ctx := tenancy.WithResolvedTenant(context.Background(), tenantA)

	if _, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "cached-reuse", SKU: "CR-10", Price: 1,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := cr.count.Load(); got != 0 {
		t.Errorf("resolver invoked %d times over a pre-seeded cache, want 0", got)
	}
}
