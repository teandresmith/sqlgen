package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestEvent_TenantedMutationCarriesTenantMetadata verifies §29.6: every
// mutation event from a tenanted table carries Metadata["tenant"] as the
// %v-stringified resolved tenant. This is the contract subscribers (audit
// logs, downstream caches, projections) rely on to attribute events to the
// right tenant.
func TestEvent_TenantedMutationCarriesTenantMetadata(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables: []string{"products"},
	})

	ctx := context.Background()
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "evented", SKU: "EVT-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := collector.waitFor(t, 1)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	got := events[0]
	if got.Action != event.Create {
		t.Errorf("event.Action = %s, want %s", got.Action, event.Create)
	}
	if got.Table != "products" {
		t.Errorf("event.Table = %q, want %q", got.Table, "products")
	}
	if got.Metadata == nil {
		t.Fatalf("event.Metadata = nil, want map with %q key", "tenant")
	}
	wantTenant := fmt.Sprintf("%v", tenantA)
	if got.Metadata["tenant"] != wantTenant {
		t.Errorf("event.Metadata[tenant] = %q, want %q", got.Metadata["tenant"], wantTenant)
	}
	_ = created
}

// TestEvent_NonTenantedMutationOmitsTenantKey verifies §29.6 second invariant:
// shared / opted-out tables (audit_logs in our schema) emit events with NO
// "tenant" key — not an empty string, not the zero UUID, just absent.
// Subscribers checking `if v, ok := meta["tenant"]; ok { ... }` should be able
// to distinguish "this row is shared" from "this row's tenant is X".
func TestEvent_NonTenantedMutationOmitsTenantKey(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables: []string{"audit_logs"},
	})

	ctx := context.Background()
	if _, err := envA.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message: "shared row",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	events := collector.waitFor(t, 1)
	if got := events[0]; got.Metadata != nil {
		if _, ok := got.Metadata["tenant"]; ok {
			t.Errorf("non-tenanted event has Metadata[tenant] = %q, want absent", got.Metadata["tenant"])
		}
	}
}

// TestEvent_SkipTenancyStampsRowTenant pins the §29.6
// SkipTenancy behavior: mc.Tenant stays nil (no resolver invocation), but the
// event hook stamps Metadata["tenant"] from the structurally captured row
// tenant — here the tenant column the admin write explicitly supplied. An
// admin write for tenant B therefore publishes a B-stamped event even though
// the resolver would have said A; the resolver value is never used.
func TestEvent_SkipTenancyStampsRowTenant(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables: []string{"products"},
	})

	ctx := context.Background()
	// Admin path: resolver would return A, but caller explicitly writes for B.
	if _, err := envA.client.Products().Create(
		ctx, &models.CreateProductInput{
			WorkspaceID: omittable.Set(tenantB),
			Name:        "admin-write", SKU: "ADM-1", Price: 1.0,
		},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("admin Create: %v", err)
	}

	events := collector.waitFor(t, 1)
	got := events[0]
	if got.Metadata == nil {
		t.Fatalf("SkipTenancy event Metadata = nil, want Metadata[tenant] = %q (row-derived stamp)", tenantB)
	}
	if v := got.Metadata["tenant"]; v != tenantB.String() {
		t.Errorf("SkipTenancy event Metadata[tenant] = %q, want %q (the row's tenant, not the resolver's %q)",
			v, tenantB.String(), tenantA.String())
	}
}
