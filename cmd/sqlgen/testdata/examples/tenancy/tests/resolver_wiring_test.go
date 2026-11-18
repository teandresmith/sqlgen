package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// pin compiles only when its argument has exactly type T, so each call states a
// signature the build checks.
func pin[T any](T) {}

// TestWithTenantResolver_TakesTheTenantColumnType pins the generated option to a
// resolver of the tenant column's Go type, and the model and create-input tenant
// fields to that same type. A resolver returning any other type does not build.
func TestWithTenantResolver_TakesTheTenantColumnType(_ *testing.T) {
	pin[func(tenancy.TenantResolver[uuid.UUID]) models.ClientOption](models.WithTenantResolver)
	pin[uuid.UUID](models.Product{}.WorkspaceID)
	pin[omittable.Value[uuid.UUID]](models.CreateProductInput{}.WorkspaceID)
}

// TestResolverWiring_ComposesWithEventsAndCache runs one long-lived client built
// with a resolver that reads the tenant from ctx and returns tenancy.ErrMissing
// when it is absent, an event publisher and a cache.
// Each option still does its job alongside the others.
func TestResolverWiring_ComposesWithEventsAndCache(t *testing.T) {
	resetDB(t)

	env := newEnv(t, withResolver(ctxResolver()), withCache(), withEvents())
	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"products"}})
	ctxA := withTenantCtx(context.Background(), tenantA)

	created, err := env.client.Products().Create(ctxA, &models.CreateProductInput{
		Name: "wired", SKU: "WIRED-1", Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.WorkspaceID != tenantA {
		t.Errorf("created.WorkspaceID = %v, want the resolved tenant %v", created.WorkspaceID, tenantA)
	}

	events := collector.waitFor(t, 1)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if got := events[0]; got.Action != event.Create || got.Metadata["tenant"] != fmt.Sprintf("%v", tenantA) {
		t.Errorf("event = {Action: %s, tenant: %q}, want {Action: %s, tenant: %q}", got.Action, got.Metadata["tenant"], event.Create, tenantA)
	}

	if _, err := env.client.Products().Get(ctxA, created.ID); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	assertCacheHit(t, env.metrics, "second Get under the same tenant", func() {
		if _, err := env.client.Products().Get(ctxA, created.ID); err != nil {
			t.Fatalf("second Get: %v", err)
		}
	})

	env.counter.reset()
	if _, err := env.client.Products().Get(context.Background(), created.ID); !errors.Is(err, tenancy.ErrMissing) {
		t.Errorf("Get with no tenant on ctx: err = %v, want tenancy.ErrMissing", err)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("DB ops issued for a ctx with no tenant: %d, want 0", got)
	}
}
