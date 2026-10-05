package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// event.Config.MetadataFunc on tenanted tables (PRD §28.8 + §29.6).
// The non-tenanted merge/isolation/timing paths are covered in the events
// example; here we pin the two behaviors that only surface with tenancy:
//
//  1. Reserved-key precedence — the system row-derived "tenant" stamp is
//     applied last, so a MetadataFunc that tries to set "tenant" cannot
//     override it (§28.8), while the consumer's other keys are carried.
//  2. Per-event tenant + shared consumer keys — a SkipTenancy batch spanning
//     two tenants stamps each event with its own row's tenant, alongside the
//     one shared MetadataFunc payload.

// TestMetadataFunc_TenantReservedKeyWins verifies that on a tenanted table the
// system "tenant" stamp beats a consumer MetadataFunc that maliciously (or
// accidentally) sets "tenant" — while the consumer's non-reserved keys still
// flow through.
func TestMetadataFunc_TenantReservedKeyWins(t *testing.T) {
	resetDB(t)
	envA := newEnv(
		t,
		withResolver(staticResolver(tenantA)),
		withMetadataFunc(func(context.Context) map[string]string {
			return map[string]string{"tenant": "HACKED", "actor": "bob"}
		}),
	)
	ctx := context.Background()

	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	p, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "reserved", SKU: "MF-RES-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = p

	events := collector.waitFor(t, 1)
	e := events[0]
	if e.Metadata == nil {
		t.Fatalf("Metadata = nil, want tenant + actor")
	}
	// System wins: tenant is the resolved row tenant, NOT "HACKED".
	if got := e.Metadata["tenant"]; got != tenantA.String() {
		t.Errorf("Metadata[tenant] = %q, want %q (system stamp must override consumer MetadataFunc)", got, tenantA.String())
	}
	// Consumer's non-reserved key survives.
	if got := e.Metadata["actor"]; got != "bob" {
		t.Errorf("Metadata[actor] = %q, want bob (consumer key must be carried)", got)
	}
}

// TestMetadataFunc_MultiTenantBatch_PerRowTenantSharedKeys verifies that a
// SkipTenancy batch spanning two tenants stamps each fanned-out event with its
// OWN row's tenant (§29.6 per-row) while carrying the single shared
// MetadataFunc payload on every event — proving the per-event clone (tenant
// does not bleed) plus the merge.
func TestMetadataFunc_MultiTenantBatch_PerRowTenantSharedKeys(t *testing.T) {
	resetDB(t)
	envA := newEnv(
		t,
		withResolver(staticResolver(tenantA)),
		withMetadataFunc(func(context.Context) map[string]string {
			return map[string]string{"actor": "batch-bob", "request_id": "req-batch"}
		}),
	)
	ctx := context.Background()

	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	// One batch, two tenants, under SkipTenancy so the caller-supplied
	// WorkspaceID on each row is honored verbatim (resolver A is ignored).
	inputs := []*models.CreateProductInput{
		{WorkspaceID: omittable.Set(tenantA), Name: "row-a", SKU: "MF-MTB-A", Price: 1},
		{WorkspaceID: omittable.Set(tenantB), Name: "row-b", SKU: "MF-MTB-B", Price: 2},
	}
	if _, err := envA.client.Products().CreateMany(
		ctx, inputs,
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("CreateMany (skip-tenancy): %v", err)
	}

	events := collector.waitFor(t, 2)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	// Map each event to its row's tenant via the SKU carried in the input.
	byTenant := map[string]event.Event{}
	for _, e := range events {
		tenantVal := e.Metadata["tenant"]
		byTenant[tenantVal] = e
		// Shared consumer keys present on every event.
		if e.Metadata["actor"] != "batch-bob" || e.Metadata["request_id"] != "req-batch" {
			t.Errorf("event (tenant=%s).Metadata = %v, want shared actor=batch-bob request_id=req-batch", tenantVal, e.Metadata)
		}
	}

	if _, ok := byTenant[tenantA.String()]; !ok {
		t.Errorf("no event stamped with tenant A (%s); got tenants %v", tenantA, tenantKeys(byTenant))
	}
	if _, ok := byTenant[tenantB.String()]; !ok {
		t.Errorf("no event stamped with tenant B (%s); got tenants %v", tenantB, tenantKeys(byTenant))
	}
	// Two distinct tenants ⇒ two distinct stamps: the per-row tenant did not
	// bleed into a single shared value across the batch.
	if len(byTenant) != 2 {
		t.Errorf("expected 2 distinct per-row tenant stamps, got %d: %v", len(byTenant), tenantKeys(byTenant))
	}
}

func tenantKeys(m map[string]event.Event) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
