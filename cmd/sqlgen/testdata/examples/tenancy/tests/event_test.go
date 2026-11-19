package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/event"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

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
