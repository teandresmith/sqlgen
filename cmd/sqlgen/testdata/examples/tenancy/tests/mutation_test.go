package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestMutation_Create_TenantAutoSet verifies that Create on a tenanted table
// auto-fills the tenant column from the resolver — the caller doesn't have to
// (and shouldn't need to) think about workspace_id at all (PRD §29.4.2). This
// is the most common write path; getting it wrong silently is the whole class
// of bug tenancy is meant to prevent.
func TestMutation_Create_TenantAutoSet(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "auto-set", SKU: "AUTO-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.WorkspaceID != tenantA {
		t.Errorf("created.WorkspaceID = %v, want %v", created.WorkspaceID, tenantA)
	}
}

// TestMutation_Create_RedundantMatchSucceeds verifies that a caller who DOES
// set the tenant field — and sets it to the value the resolver returns — gets
// the same successful Create. The §29.4.2 tolerance is the practical "read,
// edit, write back" pattern: a handler reads a row, mutates a couple of fields
// while preserving everything else, and writes it back; the workspace_id will
// still be set on the input. We don't punish that.
func TestMutation_Create_RedundantMatchSucceeds(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		WorkspaceID: omittable.Set(tenantA),
		Name:        "redundant-match",
		SKU:         "MATCH-1",
		Price:       1.0,
	})
	if err != nil {
		t.Fatalf("Create with matching tenant: %v", err)
	}
	if created.WorkspaceID != tenantA {
		t.Errorf("created.WorkspaceID = %v, want %v", created.WorkspaceID, tenantA)
	}
}

// TestMutation_Create_MismatchReturnsErrMismatch verifies the §29.4.2 mismatch
// guard: caller sets tenant X, resolver says tenant Y → ErrMismatch BEFORE the
// DB round trip. The "before" part is critical — we shouldn't insert the row
// then realize it was wrong; we shouldn't insert at all.
func TestMutation_Create_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	envA.counter.reset()

	_, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		WorkspaceID: omittable.Set(tenantB), // mismatched
		Name:        "mismatched",
		SKU:         "MISMATCH-1",
		Price:       1.0,
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Create with mismatched tenant: err = %v, want tenancy.ErrMismatch", err)
	}

	// PRD §29.4.2: ErrMismatch is returned BEFORE the DB round-trip — no INSERT
	// hits the wire. Counting issued operations is a black-box way to assert
	// the contract without poking at internals.
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during mismatch path: %d, want 0", got)
	}
}

// TestMutation_Update_MismatchReturnsErrMismatch is the Update analogue. Update
// is interesting because the user could be trying to MOVE a row from tenant A
// to tenant B; the mismatch guard catches that and forces an explicit
// SkipTenancy opt-in for any cross-tenant write.
func TestMutation_Update_MismatchReturnsErrMismatch(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "to-move", SKU: "MOVE-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	envA.counter.reset()
	_, err = envA.client.Products().Update(ctx, created.ID, &models.UpdateProductInput{
		WorkspaceID: omittable.Set(tenantB),
		Name:        omittable.Set("renamed"),
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("Update with mismatched tenant: err = %v, want tenancy.ErrMismatch", err)
	}
	if got := envA.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during Update mismatch path: %d, want 0", got)
	}

	// Belt-and-suspenders: the row is still on tenant A.
	got, err := envA.client.Products().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get post-mismatch: %v", err)
	}
	if got.WorkspaceID != tenantA {
		t.Errorf("post-mismatch row workspace_id = %v, want %v (mismatch must not partially-write)", got.WorkspaceID, tenantA)
	}
}

// TestMutation_MissingTenantFailsClosed verifies the §29.3.1 fail-closed
// contract: tenancy.required:true (the safe default) + a resolver that returns
// ErrMissing → the operation errors with ErrMissing BEFORE the DB round-trip.
// Same "before" requirement as mismatch — nothing partial writes.
func TestMutation_MissingTenantFailsClosed(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(missingResolver()))

	ctx := context.Background()
	env.counter.reset()

	_, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "missing", SKU: "MISS-1", Price: 1.0,
	})
	if !errors.Is(err, tenancy.ErrMissing) {
		t.Fatalf("Create with missing tenant: err = %v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during missing-tenant path: %d, want 0", got)
	}
}

// TestRead_MissingTenantFailsClosed mirrors the mutation test for the read
// path. A missing tenant on Get / GetMany must also fail closed — silently
// returning "no rows" would be the worst-case footgun (the row exists, the
// caller thinks it doesn't, they retry or duplicate-create).
func TestRead_MissingTenantFailsClosed(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envMissing := newEnv(t, withResolver(missingResolver()))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "exists", SKU: "EXISTS-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	envMissing.counter.reset()
	if _, err := envMissing.client.Products().Get(ctx, created.ID); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Get with missing tenant: err = %v, want tenancy.ErrMissing", err)
	}
	if got := envMissing.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during missing-tenant Get: %d, want 0", got)
	}

	envMissing.counter.reset()
	if _, err := envMissing.client.Products().GetMany(ctx, &models.GetProductsInput{}); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("GetMany with missing tenant: err = %v, want tenancy.ErrMissing", err)
	}
	if got := envMissing.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued during missing-tenant GetMany: %d, want 0", got)
	}
}

// TestMutation_Delete_TenantScoped verifies that Delete on a row owned by
// another tenant doesn't reach the row — same auto-filter as Get. Without this
// scoping, a tenant could (by guessing/scanning IDs) delete another tenant's
// data.
func TestMutation_Delete_TenantScoped(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "owned-by-A", SKU: "DEL-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	// Tenant B attempts hard delete of A's row → no row matches the
	// tenant-scoped WHERE; the row survives.
	if err := envB.client.Products().HardDelete(ctx, created.ID); err != nil {
		t.Logf("HardDelete from B (expected to succeed silently with 0 affected): %v", err)
	}

	got, err := envA.client.Products().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get from A after B's delete attempt: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("row missing after cross-tenant delete attempt: got %+v", got)
	}
}

// TestMutation_TenantUUIDIsConcrete is a guard: the WorkspaceID field on the
// generated CreateInput is uuid.UUID (the resolved Go type), not []byte. If the
// override stops applying for any reason, this test is the canary.
func TestMutation_TenantUUIDIsConcrete(t *testing.T) {
	var input models.CreateProductInput
	input.WorkspaceID = omittable.Set(uuid.New())
	if v, ok := input.WorkspaceID.Get(); !ok || v == uuid.Nil() {
		t.Errorf("WorkspaceID set/get round-trip broken: ok=%v v=%v", ok, v)
	}
}
