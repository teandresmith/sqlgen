package tests

import (
	"context"
	"fmt"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// The tenanted half of OpUpsertMany's consumer arms.
//
// UpsertMany has a producer (AffectedPKs + AffectedTenants) and three
// consumer arms; these tests exercise the two together on a tenanted table,
// pinning the §29.6 event stamp and the cache's tenanted eviction path
// (invalidateAffectedTenanted) for this op.
//
// products is the shape that makes this worth pinning rather than assuming:
// tenanted with workspace_id NOT in the primary key, an AUTOINCREMENT key
// (so the written rows' keys cannot come from the inputs), and a conflict
// target that does not cover the key. That combination is the one UpsertMany
// path where both the keys and the tenants come from a read-back — the branch
// whose index alignment no committed golden exercises.

// upsertManyProducts is the two-tenant batch these tests share. It runs under
// SkipTenancy with an explicit workspace_id per row, so a resolver-derived
// tenant could not produce the per-row answers asserted below.
func upsertManyProducts(t *testing.T, skuSuffix string) []*models.CreateProductInput {
	t.Helper()
	return []*models.CreateProductInput{
		{WorkspaceID: omittable.Set(tenantA), Name: "A-one", SKU: "um-a1-" + skuSuffix, Price: 1},
		{WorkspaceID: omittable.Set(tenantB), Name: "B-one", SKU: "um-b1-" + skuSuffix, Price: 2},
		{WorkspaceID: omittable.Set(tenantA), Name: "A-two", SKU: "um-a2-" + skuSuffix, Price: 3},
	}
}

// TestUpsertManyTenancy_EventsStampPerRowTenant pins that a tenanted
// UpsertMany publishes one event per written row, each carrying that row's own
// input (§28.6) and that row's own tenant stamp (§29.6) — not the resolver's,
// and not the batch's first.
func TestUpsertManyTenancy_EventsStampPerRowTenant(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	got := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"products"}})

	inputs := upsertManyProducts(t, "stamp")
	written, err := env.client.Products().UpsertMany(ctx, inputs, models.ProductConflictWorkspaceIDSKU,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}
	if len(written) != len(inputs) {
		t.Fatalf("written rows = %d, want %d", len(written), len(inputs))
	}

	events := got.waitFor(t, len(inputs))
	if len(events) != len(inputs) {
		t.Fatalf("event count = %d, want %d", len(events), len(inputs))
	}

	wantTenant := []uuid.UUID{tenantA, tenantB, tenantA}
	for i, e := range events {
		if e.Action != event.Upsert {
			t.Errorf("event[%d].Action = %q, want %q", i, e.Action, event.Upsert)
		}
		in, ok := e.Input.(*models.CreateProductInput)
		if !ok {
			t.Fatalf("event[%d].Input type = %T, want *models.CreateProductInput (per-row, not the batch slice)", i, e.Input)
		}
		if in.SKU != inputs[i].SKU {
			t.Errorf("event[%d].Input.SKU = %q, want %q — event i must carry input i", i, in.SKU, inputs[i].SKU)
		}
		// §29.6: stamped from the row's captured tenant, in the §29.5 key
		// grammar. A batch-shared or resolver-derived value could not give
		// row 1 tenantB while the resolver returns tenantA.
		want := fmt.Sprintf("%v", wantTenant[i])
		if got := e.Metadata["tenant"]; got != want {
			t.Errorf("event[%d].Metadata[tenant] = %q, want %q", i, got, want)
		}
	}
}

// TestUpsertManyTenancy_CacheEvictsPerRowTenant pins the UpsertMany cache arm
// on a tenanted table: §27.7 gives UpsertMany "Invalidate many", and on a
// tenanted table that routes through invalidateAffectedTenanted, which evicts
// one tenant-scoped key per affected row. A *short* AffectedTenants is loud —
// invalidateTenantedFallback pattern-wipes and emits the cache error signal.
// The quiet failure is a same-length carrier: a nil entry skips that row's
// eviction outright, and a wrong-tenant entry evicts some other tenant's key
// while the real one stays stale. Neither surfaces an error, so this asserts
// the row-keyed path actually evicted the row it was supposed to.
func TestUpsertManyTenancy_CacheEvictsPerRowTenant(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)), withCache())
	ctx := context.Background()

	inputs := upsertManyProducts(t, "cache")
	seeded, err := env.client.Products().UpsertMany(ctx, inputs, models.ProductConflictWorkspaceIDSKU,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("UpsertMany seed: %v", err)
	}
	if len(seeded) != len(inputs) {
		t.Fatalf("written rows = %d, want %d", len(seeded), len(inputs))
	}

	// Pick a tenant-A row and warm its cache entry through the read-through
	// path, confirming it is cached by observing the second Get hit.
	var warmed int64
	for _, p := range seeded {
		if p.WorkspaceID == tenantA {
			warmed = p.ID
			break
		}
	}
	if warmed == 0 {
		t.Fatal("no tenant-A row among the written products")
	}
	if _, err := env.client.Products().Get(ctx, warmed); err != nil {
		t.Fatalf("Get (warm): %v", err)
	}
	hitsBefore := env.metrics.hitCount()
	if _, err := env.client.Products().Get(ctx, warmed); err != nil {
		t.Fatalf("Get (confirm warm): %v", err)
	}
	if got := env.metrics.hitCount() - hitsBefore; got != 1 {
		t.Fatalf("Get after warm: hits delta = %d, want 1 (row was not cached, so the eviction assertion below would be vacuous)", got)
	}

	// Re-upsert the same three rows: every one takes the conflict branch, so
	// the keys and the tenants all come from the read-back.
	if _, err := env.client.Products().UpsertMany(ctx, inputs, models.ProductConflictWorkspaceIDSKU,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("UpsertMany conflict: %v", err)
	}

	// The warmed entry must be gone: the next Get is a miss, not a hit.
	// Without the OpUpsertMany arm the op would fall out of
	// dispatchMutation's switch to a bare `return nil` and the stale entry
	// would survive.
	hits, misses := env.metrics.hitCount(), env.metrics.missCount()
	if _, err := env.client.Products().Get(ctx, warmed); err != nil {
		t.Fatalf("Get after UpsertMany: %v", err)
	}
	if got := env.metrics.hitCount() - hits; got != 0 {
		t.Errorf("Get after UpsertMany: hits delta = %d, want 0 — the entry was not evicted (§27.7 Invalidate many)", got)
	}
	if got := env.metrics.missCount() - misses; got != 1 {
		t.Errorf("Get after UpsertMany: misses delta = %d, want 1 — the tenant-scoped key was not evicted", got)
	}
}
