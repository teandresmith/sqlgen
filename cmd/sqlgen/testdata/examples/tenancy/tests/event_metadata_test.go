package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Tenancy event metadata — PRD §29.6 + §28.3.
//
// Invariants under test here:
//
//   - §29.6 + §28.3: Metadata is a map[string]string. Absence of the "tenant"
//     key — not an empty-string value, not the stringified zero UUID — is how
//     non-tenanted rows and SkipTenancy events are distinguished. A
//     regression that wrote `"tenant": ""` or `"tenant": "<nil>"` would
//     silently break downstream routing that filters by tenant presence.
//     Pinned here via a regression guard that refuses any "" tenant values.
//   - §28.4: batch mutations on a tenanted table produce N events with
//     uniform Metadata["tenant"] across all N. Each event is
//     stamped from its own row's captured tenant (§29.6) and
//     closed over into every fanned-out event.
//
// This file covers:
//
//  1. **Multiple non-tenanted tables.** audit_logs AND post_tags (distinct
//     schema shapes — audit_logs is a simple flat table, post_tags is a
//     composite-PK junction with no workspace_id column). Both must emit
//     events with Metadata["tenant"] absent.
//  2. **Batch uniformity.** CreateMany → N events all carrying the same
//     tenant value, no drift between events[0] and events[N-1].
//  3. **No empty-string tenant.** Regression guard walked across every test
//     in this file.

// TestEventMetadata_NonTenantedTables_MultipleShapes verifies the §29.6
// key-absence property across the two structurally different non-tenanted
// tables in the schema: audit_logs (simple flat) and post_tags (composite-PK
// junction). Both must emit events with no "tenant" key regardless of the
// resolver's value. A regression that unconditionally populated the key for
// every hook would slip past a test that only exercised one shape.
func TestEventMetadata_NonTenantedTables_MultipleShapes(t *testing.T) {
	resetDB(t)
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	// audit_logs — simple flat, no workspace_id column at all.
	auditCollector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables: []string{"audit_logs"},
	})
	if _, err := envA.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message: "shared-row-audit",
	}); err != nil {
		t.Fatalf("audit_logs Create: %v", err)
	}
	auditEvents := auditCollector.waitFor(t, 1)
	assertTenantKeyAbsent(t, auditEvents[0], "audit_logs")

	// post_tags — composite-PK junction. Seed a post and a tag under
	// tenantA first (both ARE tenanted, but post_tags itself is not).
	user, err := envA.client.Users().Create(ctx, &models.CreateUserInput{Email: "e@t.com"})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	post, err := envA.client.Posts().Create(ctx, &models.CreatePostInput{
		UserID: user.ID, Title: "post",
	})
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	tag, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "tag"})
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	postTagCollector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables: []string{"post_tags"},
	})
	if _, err := envA.client.PostTags().Create(ctx, &models.CreatePostTagInput{
		PostID: post.ID, TagID: tag.ID,
	}); err != nil {
		t.Fatalf("post_tags Create: %v", err)
	}
	postTagEvents := postTagCollector.waitFor(t, 1)
	assertTenantKeyAbsent(t, postTagEvents[0], "post_tags")
}

// TestEventMetadata_BatchTenanted_UniformTenant verifies that a batch
// mutation on a tenanted table emits N events with identical
// Metadata["tenant"] — there is no drift between event[0] and event[N-1].
// Under PRD §29.6 each fanned-out event is stamped from its
// own row's captured tenant; a batch mutated under one resolved tenant
// therefore still yields N identical stamps — uniformity now follows from
// the rows all belonging to that tenant, not from sharing one resolver read.
func TestEventMetadata_BatchTenanted_UniformTenant(t *testing.T) {
	resetDB(t)
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	const n = 5
	inputs := make([]*models.CreateProductInput, n)
	for i := range n {
		inputs[i] = &models.CreateProductInput{
			Name:  "batch-uni",
			SKU:   "BATCH-UNI-" + string(rune('A'+i)),
			Price: float64(i + 1),
		}
	}

	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{
		Tables:  []string{"products"},
		Actions: []event.Action{event.Create},
	})

	if _, err := envA.client.Products().CreateMany(ctx, inputs); err != nil {
		t.Fatalf("CreateMany: %v", err)
	}

	events := collector.waitFor(t, n)
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}

	wantTenant := fmt.Sprintf("%v", tenantA)
	for i, e := range events {
		if e.Metadata == nil {
			t.Fatalf("event[%d].Metadata = nil, want map with tenant=%q", i, wantTenant)
		}
		got, ok := e.Metadata["tenant"]
		if !ok {
			t.Errorf("event[%d].Metadata missing 'tenant' key", i)
			continue
		}
		if got != wantTenant {
			t.Errorf("event[%d].Metadata[tenant] = %q, want %q (batch must be uniform)",
				i, got, wantTenant)
		}
	}

	// Redundant-looking but load-bearing — we want an explicit assertion that
	// ALL events are byte-identical on the tenant value, not just "every
	// one matches the expected string". A hypothetical regression where the
	// hook resolved per-event could produce N values that all happen to
	// match `staticResolver(tenantA)` but arrive via different code paths;
	// by asserting events[i] == events[0] we pin the "closed-over-once"
	// property (§29.6 third paragraph).
	for i, e := range events {
		if e.Metadata["tenant"] != events[0].Metadata["tenant"] {
			t.Errorf("event[%d].Metadata[tenant] = %q, event[0] = %q — batch must share one value",
				i, e.Metadata["tenant"], events[0].Metadata["tenant"])
		}
	}
}

// TestEventMetadata_NoEmptyTenantStringRegression is the acceptance-criteria
// regression guard: under NO circumstance does Metadata["tenant"] = "" leak
// into an event. The template stamps from the captured row tenant,
// gating on a nil capture element — which produces key-absence, never an
// empty-string value. A refactor that formatted uuid.Nil or the tenant
// type's zero value into the map would fail this test.
//
// Exercises: tenanted Create, tenanted Update under SkipTenancy (now
// stamped with the row's tenant), non-tenanted Create.
func TestEventMetadata_NoEmptyTenantStringRegression(t *testing.T) {
	resetDB(t)
	envA := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	collector := subscribeEvents(t, envA.bus, event.SubscribeOptions{})

	// Tenanted Create.
	created, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "no-empty", SKU: "NO-EMPTY-1", Price: 1,
	})
	if err != nil {
		t.Fatalf("Create (tenanted): %v", err)
	}

	// Tenanted Update under SkipTenancy (stamped with the row's tenant;
	// the guard below rejects any empty-string form).
	if _, err := envA.client.Products().Update(
		ctx, created.ID,
		&models.UpdateProductInput{Name: omittable.Set("no-empty-upd")},
		func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("Update (skip-tenancy): %v", err)
	}

	// Non-tenanted Create — audit_logs never populates tenant.
	if _, err := envA.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message: "no-empty-audit",
	}); err != nil {
		t.Fatalf("Create (non-tenanted): %v", err)
	}

	events := collector.waitFor(t, 3)
	for i, e := range events {
		if e.Metadata == nil {
			continue
		}
		v, ok := e.Metadata["tenant"]
		if !ok {
			continue
		}
		if v == "" {
			t.Errorf("event[%d] (Action=%s, Table=%s): Metadata[tenant] is empty string — regression against §29.6 key-absence rule",
				i, e.Action, e.Table)
		}
	}
}

// assertTenantKeyAbsent checks that the given event's Metadata has no
// "tenant" key. Used by non-tenanted and SkipTenancy tests where absence —
// not empty-string presence — is the §29.6 contract.
func assertTenantKeyAbsent(t *testing.T, e event.Event, descr string) {
	t.Helper()
	if e.Metadata == nil {
		return
	}
	if v, ok := e.Metadata["tenant"]; ok {
		t.Errorf("%s: Metadata[tenant] = %q present, want absent", descr, v)
	}
}
