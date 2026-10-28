package tests

// Runtime properties of the event and tenancy config that only a generated
// client can show: how often the consumer's MetadataFunc runs, that a
// consumer-chosen metadata key passes through untouched, that sqlgen does not
// police a tenant switch inside one transaction, and what the fail-closed
// error says at the first tenanted call.

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/event"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestMetadataFunc_CalledOncePerHookEntry pins that MetadataFunc runs once per
// mutation-hook entry, not once per event: a CreateMany of three rows is one
// hook entry that fans out to three events.
func TestMetadataFunc_CalledOncePerHookEntry(t *testing.T) {
	resetDB(t)
	var calls atomic.Int64
	env := newEnv(t, withMetadataFunc(func(context.Context) map[string]string {
		calls.Add(1)
		return map[string]string{"actor": "once"}
	}))
	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	inputs := []*models.CreateProductInput{
		{Name: "once-1", SKU: "MF-ONCE-1", Price: 1},
		{Name: "once-2", SKU: "MF-ONCE-2", Price: 2},
		{Name: "once-3", SKU: "MF-ONCE-3", Price: 3},
	}
	if _, err := env.client.Products().CreateMany(context.Background(), inputs); err != nil {
		t.Fatalf("CreateMany: %v", err)
	}

	events := collector.waitFor(t, len(inputs))
	if len(events) != len(inputs) {
		t.Fatalf("got %d events, want %d", len(events), len(inputs))
	}
	for _, e := range events {
		if got := e.Metadata["actor"]; got != "once" {
			t.Errorf("event %s Metadata[actor] = %q, want %q", e.ID, got, "once")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("MetadataFunc calls for one CreateMany of %d rows = %d, want 1", len(inputs), got)
	}
}

// TestMetadataFunc_TraceparentPassesThrough pins that "traceparent" is the
// consumer's key: the value MetadataFunc returns reaches the event verbatim,
// beside the system tenant stamp.
func TestMetadataFunc_TraceparentPassesThrough(t *testing.T) {
	resetDB(t)
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	env := newEnv(t, withMetadataFunc(func(context.Context) map[string]string {
		return map[string]string{"traceparent": traceparent}
	}))
	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	if _, err := env.client.Products().Create(context.Background(), &models.CreateProductInput{
		Name: "trace", SKU: "MF-TRACE-1", Price: 1,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	e := collector.waitFor(t, 1)[0]
	want := map[string]string{"traceparent": traceparent, "tenant": tenantA.String()}
	if len(e.Metadata) != len(want) || e.Metadata["traceparent"] != want["traceparent"] || e.Metadata["tenant"] != want["tenant"] {
		t.Errorf("event Metadata = %v, want %v", e.Metadata, want)
	}
}

// TestTx_MidTransactionTenantSwitchIsNotDetected pins the weak sense of
// "cross-tenant transactions are not supported": a child ctx carrying another
// tenant between two writes in one transaction writes rows in two tenants,
// and the transaction commits without error.
func TestTx_MidTransactionTenantSwitchIsNotDetected(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(ctxResolver()))
	ctx := withTenantCtx(context.Background(), tenantA)

	err := env.client.WithTx(ctx, "mid-tx-switch", func(txCtx context.Context) error {
		if _, err := env.client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "in-a", SKU: "SW-A", Price: 1,
		}); err != nil {
			return err
		}
		switched := withTenantCtx(txCtx, tenantB)
		_, err := env.client.Products().Create(switched, &models.CreateProductInput{
			Name: "in-b", SKU: "SW-B", Price: 2,
		})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx with a mid-transaction tenant switch: err = %v, want nil (no switch detection)", err)
	}

	rows, err := env.client.Products().GetMany(ctx, &models.GetProductsInput{},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("GetMany (skip-tenancy): %v", err)
	}
	got := make(map[string]string, len(rows))
	for _, p := range rows {
		got[p.SKU] = p.WorkspaceID.String()
	}
	want := map[string]string{"SW-A": tenantA.String(), "SW-B": tenantB.String()}
	if len(got) != len(want) || got["SW-A"] != want["SW-A"] || got["SW-B"] != want["SW-B"] {
		t.Errorf("committed rows by tenant = %v, want %v (one row in each tenant)", got, want)
	}
}

// TestErrMissing_FirstTenantedCallNamesTableAndResolver pins that ErrMissing
// surfaces at the first tenanted call, before any database round trip, with
// the table and the resolve step named in the error.
func TestErrMissing_FirstTenantedCallNamesTableAndResolver(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(missingResolver()))

	_, err := env.client.Products().Create(context.Background(), &models.CreateProductInput{
		Name: "missing", SKU: "EM-1", Price: 1,
	})
	const want = "create product: resolve tenant: tenancy: tenant missing from context"
	if err == nil || err.Error() != want {
		t.Errorf("Create with a missing tenant: err = %v, want %q", err, want)
	}
	if got := env.counter.totalDBOps(); got != 0 {
		t.Errorf("database operations before ErrMissing = %d, want 0", got)
	}
}
