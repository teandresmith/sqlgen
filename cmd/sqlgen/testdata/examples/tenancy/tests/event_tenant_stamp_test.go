package tests

import (
	"context"
	"testing"
	"time"
	"uuid"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Event tenant publish (PRD §29.6).
// Every event for a tenanted table is stamped from the mutated row itself —
// the AffectedTenants capture (tenant ∉ PK) or the PK struct (tenant ∈ PK) —
// so SkipTenancy and required:false-zero mutations publish tenant-stamped
// events, per-row, with the %v form that is byte-identical to the §29.5
// cache-key grammar. A row the mutation never materialized soft-degrades to
// an absent key, never "<nil>".

// A SkipTenancy batch spanning two tenants publishes one event per
// row, each stamped with THAT row's tenant. A single resolver-derived (or
// batch-shared) value could never produce two distinct stamps.
func TestEventTenantStamp_SkipTenancyManyStampsPerRowTenants(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	mk := func(tenant uuid.UUID, title string) int64 {
		t.Helper()
		a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:       title,
			WorkspaceID: omittable.Set(tenant),
		}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true })
		if err != nil {
			t.Fatalf("create %s (%s): %v", title, tenant, err)
		}
		return a.ID
	}
	rID := mk(tenantA, "stamp-r")
	sID := mk(tenantB, "stamp-s")

	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"articles"}})

	if _, err := env.client.Articles().UpdateMany(ctx, []models.UpdateArticleItem{
		{ID: rID, Input: &models.UpdateArticleInput{Title: omittable.Set("stamp-r2")}},
		{ID: sID, Input: &models.UpdateArticleInput{Title: omittable.Set("stamp-s2")}},
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}

	events := collector.waitFor(t, 2)
	wantByPK := map[int64]string{rID: tenantA.String(), sID: tenantB.String()}
	for _, e := range events {
		pk, ok := e.PK.(int64)
		if !ok {
			t.Fatalf("event PK = %T, want int64", e.PK)
		}
		want := wantByPK[pk]
		if e.Metadata == nil || e.Metadata["tenant"] != want {
			t.Errorf("event for pk %d: Metadata[tenant] = %v, want %q (per-row stamp)", pk, e.Metadata, want)
		}
	}
}

// Tenant-in-PK tables stamp from the PK struct: a SkipTenancy
// mutation through a disagreeing resolver publishes the PK-carried tenant.
func TestEventTenantStamp_TenantInPKStampsFromPKStruct(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	pkB := models.OrderItemPK{WorkspaceID: tenantB, OrderID: 9, ProductID: 9}
	if _, err := env.client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantB, OrderID: 9, ProductID: 9, Quantity: 1, UnitPrice: 1,
	}, func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true }); err != nil {
		t.Fatalf("seed order item: %v", err)
	}

	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"order_items"}})

	if _, err := env.client.OrderItems().Update(
		ctx, pkB,
		&models.UpdateOrderItemInput{Quantity: omittable.Set(int64(5))},
		func(o *models.CallOptions[models.OrderItemFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SkipTenancy update: %v", err)
	}

	events := collector.waitFor(t, 1)
	if got := events[0].Metadata["tenant"]; got != tenantB.String() {
		t.Errorf("tenant-in-PK event Metadata[tenant] = %q, want %q (from OrderItemPK.WorkspaceID)", got, tenantB.String())
	}
}

// A batch member the mutation never materialized (missing PK) wrote nothing,
// so it publishes no event at all; the materialized member's event carries
// its row's tenant.
func TestEventTenantStamp_UnmaterializedRowPublishesNothing(t *testing.T) {
	resetDB(t)
	env := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	a, err := env.client.Articles().Create(ctx, &models.CreateArticleInput{Title: "stamp-nil"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	collector := subscribeEvents(t, env.bus, event.SubscribeOptions{Tables: []string{"articles"}})

	const missingID int64 = 424_242
	if _, err := env.client.Articles().SoftDeleteMany(
		ctx, []int64{a.ID, missingID},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SoftDeleteMany with missing PK: %v", err)
	}

	collector.waitFor(t, 1)
	// Give a stray event for the missing key time to arrive.
	time.Sleep(100 * time.Millisecond)
	events := collector.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (the missing key wrote nothing): %+v", len(events), events)
	}
	if pk, _ := events[0].PK.(int64); pk != a.ID {
		t.Errorf("event PK = %v, want %d", events[0].PK, a.ID)
	}
	if got := events[0].Metadata["tenant"]; got != tenantA.String() {
		t.Errorf("event Metadata[tenant] = %q, want %q", got, tenantA.String())
	}
}

// The second nil path: tenancy.required:false with a zero resolver leaves
// mc.Tenant nil, yet the event carries the row's actual org (row-derived).
func TestEventTenantStamp_RequiredFalseZeroTenantStampsRowOrg(t *testing.T) {
	resetDB(t)
	envReal := newEnv(t, withResolver(staticResolver(tenantA)), withEvents())
	ctx := context.Background()

	w, err := envReal.client.LegacyWidgets().Create(ctx, &models.CreateLegacyWidgetInput{Label: "stamp-w"})
	if err != nil {
		t.Fatalf("create widget: %v", err)
	}

	// Zero-returning resolver publishing to the same bus: legacy_widgets has
	// tenancy.required:false, so the zero resolve opts the call out of
	// tenancy (apply=false, mc.Tenant nil) — no SkipTenancy anywhere.
	zeroClient := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(uuid.UUID{})),
		models.WithEventPublisher(envReal.bus),
	)

	collector := subscribeEvents(t, envReal.bus, event.SubscribeOptions{Tables: []string{"legacy_widgets"}})

	if _, err := zeroClient.LegacyWidgets().Update(
		ctx, w.ID,
		&models.UpdateLegacyWidgetInput{Label: omittable.Set("stamp-w2")},
	); err != nil {
		t.Fatalf("zero-resolver update: %v", err)
	}

	events := collector.waitFor(t, 1)
	if got := events[0].Metadata["tenant"]; got != tenantA.String() {
		t.Errorf("required:false zero-tenant event Metadata[tenant] = %q, want %q (row-derived org)", got, tenantA.String())
	}
}
