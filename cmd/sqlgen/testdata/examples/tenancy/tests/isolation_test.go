package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestIsolation_Get verifies that Get on a tenanted table returns sql.ErrNoRows
// when called with a different tenant than the row was created with — the row
// exists in the database, but the auto-injected `WHERE workspace_id = $resolved`
// filter scopes it out (PRD §29.4.1). The B-resolver test isn't an
// authorization error, it's a "no row matches" — distinguishing those two cases
// is critical because most callers `errors.Is(err, models.ErrNotFound)` and expect
// the row simply to not exist.
func TestIsolation_Get(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name:  "widget",
		SKU:   "WIDGET-A-1",
		Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create product (tenant A): %v", err)
	}

	gotA, err := envA.client.Products().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get from tenant A: %v", err)
	}
	if gotA == nil || gotA.ID != created.ID {
		t.Fatalf("Get from tenant A: got %+v, want id=%d", gotA, created.ID)
	}
	if gotA.WorkspaceID != tenantA {
		t.Errorf("Get from tenant A: workspace_id = %v, want %v", gotA.WorkspaceID, tenantA)
	}

	if _, err := envB.client.Products().Get(ctx, created.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get from tenant B: err = %v, want models.ErrNotFound", err)
	}
}

// TestIsolation_GetMany verifies GetMany returns ONLY the resolver's tenant's
// rows even when the underlying table has rows for multiple tenants. This is
// the more common workflow — most reads are list reads, and the auto-filter has
// to reliably scope every one.
func TestIsolation_GetMany(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	for i, sku := range []string{"A1", "A2", "A3"} {
		_, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			Name:  "tenant A product",
			SKU:   sku,
			Price: float64(i),
		})
		if err != nil {
			t.Fatalf("create A %s: %v", sku, err)
		}
	}
	for i, sku := range []string{"B1", "B2"} {
		_, err := envB.client.Products().Create(ctx, &models.CreateProductInput{
			Name:  "tenant B product",
			SKU:   sku,
			Price: float64(i),
		})
		if err != nil {
			t.Fatalf("create B %s: %v", sku, err)
		}
	}

	listA, err := envA.client.Products().GetMany(ctx, &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("GetMany A: %v", err)
	}
	if len(listA) != 3 {
		t.Errorf("GetMany A: got %d rows, want 3", len(listA))
	}
	for _, p := range listA {
		if p.WorkspaceID != tenantA {
			t.Errorf("GetMany A: row workspace_id = %v, want %v", p.WorkspaceID, tenantA)
		}
	}

	listB, err := envB.client.Products().GetMany(ctx, &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("GetMany B: %v", err)
	}
	if len(listB) != 2 {
		t.Errorf("GetMany B: got %d rows, want 2", len(listB))
	}
	for _, p := range listB {
		if p.WorkspaceID != tenantB {
			t.Errorf("GetMany B: row workspace_id = %v, want %v", p.WorkspaceID, tenantB)
		}
	}
}

// TestIsolation_LegacyColumnOverride verifies the per-table tenancy.column
// override (org_id instead of workspace_id) participates in the auto-filter
// identically — same isolation contract, different column name.
func TestIsolation_LegacyColumnOverride(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	createdA, err := envA.client.LegacyWidgets().Create(ctx, &models.CreateLegacyWidgetInput{
		Label: "A widget",
	})
	if err != nil {
		t.Fatalf("create legacy widget A: %v", err)
	}
	if createdA.OrgID != tenantA {
		t.Errorf("created legacy widget org_id = %v, want %v", createdA.OrgID, tenantA)
	}

	if _, err := envB.client.LegacyWidgets().Get(ctx, createdA.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get legacy widget from tenant B: err = %v, want models.ErrNotFound", err)
	}
}

// TestIsolation_SharedTable_AuditLogs verifies that opted-out tables behave
// like non-tenant tables — no auto-filter, both tenants see all rows. Audit
// logs are the canonical "shared / system-wide" table in this schema.
func TestIsolation_SharedTable_AuditLogs(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()

	for _, msg := range []string{"system event 1", "system event 2"} {
		if _, err := envA.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{Message: msg}); err != nil {
			t.Fatalf("create audit log: %v", err)
		}
	}

	listA, err := envA.client.AuditLogs().GetMany(ctx, &models.GetAuditLogsInput{})
	if err != nil {
		t.Fatalf("audit GetMany A: %v", err)
	}
	listB, err := envB.client.AuditLogs().GetMany(ctx, &models.GetAuditLogsInput{})
	if err != nil {
		t.Fatalf("audit GetMany B: %v", err)
	}
	if len(listA) != 2 || len(listB) != 2 {
		t.Errorf("audit logs are shared: A=%d B=%d, want 2 and 2", len(listA), len(listB))
	}
}

// TestIsolation_TenantValueViaCtxResolver verifies a realistic resolver pattern:
// the tenant lives on the request context, the resolver pulls it out. Each call
// can carry a different tenant, so the same client instance serves every
// tenant. This is the §29.3.2 happy path.
func TestIsolation_TenantValueViaCtxResolver(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(ctxResolver()))

	ctxA := withTenantCtx(context.Background(), tenantA)
	ctxB := withTenantCtx(context.Background(), tenantB)

	createdA, err := env.client.Products().Create(ctxA, &models.CreateProductInput{
		Name: "ctx A", SKU: "CTX-A", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("create ctx A: %v", err)
	}
	if createdA.WorkspaceID != tenantA {
		t.Errorf("createdA.WorkspaceID = %v, want %v", createdA.WorkspaceID, tenantA)
	}

	if _, err := env.client.Products().Get(ctxB, createdA.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Get from ctx B: err = %v, want models.ErrNotFound", err)
	}

	gotA, err := env.client.Products().Get(ctxA, createdA.ID)
	if err != nil {
		t.Fatalf("Get from ctx A: %v", err)
	}
	if gotA.ID != createdA.ID {
		t.Errorf("Get from ctx A: id = %d, want %d", gotA.ID, createdA.ID)
	}
}

// TestIsolation_NilResolverFailsClosed is a guard: the Nil UUID must never
// be confused with a real tenant. Under tenancy.required:true (the default,
// and the setting in this example), a resolver returning uuid.Nil is
// treated as "tenant missing" per PRD §29.3.1 — "Returning the zero value
// for T with required: true causes every tenanted operation to error with
// ErrMissing before hitting the DB."
//
// The generator enforces this contract itself: the resolver need not return
// tenancy.ErrMissing explicitly for the fail-closed branch to trigger, since
// the generator upgrades a zero-value + nil-error return to
// tenancy.ErrMissing under required:true.
// The defensive property the original test asserted ("if a Nil-tenant row
// somehow gets created, other tenants can't see it") is now satisfied by
// construction — Nil-tenant rows can't be created at all under required:true.
func TestIsolation_NilResolverFailsClosed(t *testing.T) {
	resetDB(t)

	envNil := newEnv(t, withResolver(staticResolver(uuid.Nil())))
	ctx := context.Background()

	envNil.counter.reset()
	_, err := envNil.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "nil-tenant", SKU: "NIL-1", Price: 0.5,
	})
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("create with nil-returning resolver under required:true: err = %v, want tenancy.ErrMissing", err)
	}
	if got := envNil.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued on nil-resolver Create: %d, want 0 (fail-closed before round-trip)", got)
	}
}
