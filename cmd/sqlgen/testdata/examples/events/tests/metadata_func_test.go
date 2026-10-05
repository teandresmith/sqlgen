package tests

import (
	"context"
	"testing"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// event.Config.MetadataFunc (PRD §28.8) — the producer-side hook
// that injects consumer metadata (actor, request_id, traceparent, …) into
// every published Event.Metadata. These tests exercise the non-tenanted path
// on the events example; the tenanted reserved-key + multi-tenant-batch
// behavior lives in the tenancy example (event_metadata_test.go).

type metaCtxKey struct{}

// metadataFromCtx mirrors a real MetadataFunc: it pulls request-scoped values
// out of the context. Returns nil when nothing was stashed, so the nil-merge
// path is exercised too.
func metadataFromCtx(ctx context.Context) map[string]string {
	m, _ := ctx.Value(metaCtxKey{}).(map[string]string)
	return m
}

// newClientWithMeta builds a client whose event publisher is configured with
// the given MetadataFunc, returning the client and its bus.
func newClientWithMeta(t *testing.T, fn func(context.Context) map[string]string) (*models.Client, *memorybus.Bus) {
	t.Helper()
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	client := models.New(
		dbstdlib.New(testDB),
		models.WithEventPublisher(bus, func(c *event.Config) { c.MetadataFunc = fn }),
	)
	return client, bus
}

// TestMetadataFunc_MergedIntoEveryEvent verifies the MetadataFunc result is
// merged into every fanned-out event of a batch, and that each event carries
// its OWN metadata map (per-event shallow clone) so mutating one does not
// bleed into a sibling.
func TestMetadataFunc_MergedIntoEveryEvent(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithMeta(t, func(context.Context) map[string]string {
		return map[string]string{"actor": "alice", "request_id": "req-1"}
	})
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	inputs := []*models.CreateProductInput{
		{Name: "a", SKU: "MF-MERGE-1", Price: 1},
		{Name: "b", SKU: "MF-MERGE-2", Price: 2},
		{Name: "c", SKU: "MF-MERGE-3", Price: 3},
	}
	created, err := client.Products().CreateMany(ctx, inputs)
	if err != nil {
		t.Fatalf("CreateMany: %v", err)
	}
	t.Cleanup(func() {
		for _, p := range created {
			_ = client.Products().HardDelete(ctx, p.ID)
		}
	})

	events := got.waitFor(t, 3)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	for i, e := range events {
		if e.Metadata == nil {
			t.Fatalf("event[%d].Metadata = nil, want actor+request_id", i)
		}
		if e.Metadata["actor"] != "alice" || e.Metadata["request_id"] != "req-1" {
			t.Errorf("event[%d].Metadata = %v, want actor=alice request_id=req-1", i, e.Metadata)
		}
	}

	// Per-event isolation: each event owns a distinct map. Mutating one must
	// not affect the others (proves the base map is cloned per event, not
	// shared across the fanout).
	events[0].Metadata["actor"] = "MUTATED"
	if events[1].Metadata["actor"] != "alice" {
		t.Errorf("mutating event[0].Metadata bled into event[1]: %v", events[1].Metadata)
	}
	if events[2].Metadata["actor"] != "alice" {
		t.Errorf("mutating event[0].Metadata bled into event[2]: %v", events[2].Metadata)
	}
}

// TestMetadataFunc_CapturedAtHookEntry_UnderTx pins the load-bearing timing
// guarantee (§28.8): MetadataFunc runs at hook entry where the
// request ctx is live — NOT from the deferred Tx.OnCommit callback, which
// fires on a detached context.Background(). We stash the actor only in the
// request ctx and run the mutation inside a transaction (default async
// callback mode); the value can only reach the event if it was captured at
// hook entry.
func TestMetadataFunc_CapturedAtHookEntry_UnderTx(t *testing.T) {
	client, bus := newClientWithMeta(t, metadataFromCtx)
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	// Actor lives ONLY in the request ctx. The deferred OnCommit publish will
	// run on Background, so if MetadataFunc were called there it would see nil.
	reqCtx := context.WithValue(context.Background(), metaCtxKey{}, map[string]string{"actor": "carol"})

	var createdID int64
	err := client.WithTx(reqCtx, "meta-hook-entry", func(txCtx context.Context) error {
		p, err := client.Products().Create(txCtx, &models.CreateProductInput{
			Name: "TxMeta", SKU: "MF-TX-1", Price: 1,
		})
		if err != nil {
			return err
		}
		createdID = p.ID
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(context.Background(), createdID) })

	events := got.waitFor(t, 1)
	if events[0].Metadata == nil || events[0].Metadata["actor"] != "carol" {
		t.Errorf("event.Metadata = %v, want actor=carol — MetadataFunc must run at hook entry (request ctx), not from the async OnCommit callback",
			events[0].Metadata)
	}
}

// TestMetadataFunc_NonTenanted_NoTenantStamp confirms the system adds no
// "tenant" key on a non-tenanted table: a normal MetadataFunc (actor only)
// yields an event carrying actor and no tenant key. The events example has no
// tenanted tables, so this is the structural non-tenanted guarantee.
func TestMetadataFunc_NonTenanted_NoTenantStamp(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithMeta(t, func(context.Context) map[string]string {
		return map[string]string{"actor": "dave"}
	})
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "NoTenant", SKU: "MF-NT-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	events := got.waitFor(t, 1)
	if events[0].Metadata["actor"] != "dave" {
		t.Errorf("Metadata[actor] = %q, want dave", events[0].Metadata["actor"])
	}
	if v, ok := events[0].Metadata["tenant"]; ok {
		t.Errorf("non-tenanted event carries tenant=%q, want no tenant key", v)
	}
}

// TestMetadataFunc_Nil_NoMetadata pins the byte-identical no-op path: with no
// MetadataFunc configured, a non-tenanted event's Metadata is nil — exactly
// the behavior before MetadataFunc existed.
func TestMetadataFunc_Nil_NoMetadata(t *testing.T) {
	ctx := context.Background()
	client, bus := newClientWithBus(t) // no MetadataFunc
	got := subscribe(t, bus, event.SubscribeOptions{Actions: []event.Action{event.Create}})

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "NilMeta", SKU: "MF-NIL-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	events := got.waitFor(t, 1)
	if events[0].Metadata != nil {
		t.Errorf("Metadata = %v, want nil (no MetadataFunc, non-tenanted → byte-identical no-op)", events[0].Metadata)
	}
}
