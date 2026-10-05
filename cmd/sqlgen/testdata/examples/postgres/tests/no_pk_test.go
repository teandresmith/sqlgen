package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// E2E coverage for the no-PK + primary_key.columns override path
// (PRD §9.4b / §8.6). Both `counters` and `rate_limits` lack inline PRIMARY
// KEY clauses — uniqueness is added post-CREATE via ALTER TABLE — and would
// be skipped from generation entirely without an override. The example
// sqlgen.yml supplies `primary_key.columns` for both, exercising the
// SINGLE-column (counters.key) and COMPOSITE (rate_limits.{org_id, bucket})
// shapes against a live Postgres container.

// TestCounters_SingleColumnOverride exercises the single-column override:
// counters.key is promoted to PK via tables.counters.primary_key.columns.
// Create / Get / Update / Upsert / HardDelete all key on the bare `key`
// string argument, matching the auto-detected single-column PK shape.
func TestCounters_SingleColumnOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	key := "no-pk-counter-" + t.Name()
	t.Cleanup(func() { _ = client.Counters().HardDelete(ctx, key) })

	created, err := client.Counters().Create(ctx, &models.CreateCounterInput{
		Key:   key,
		Count: omittable.Set(int64(5)),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Key != key {
		t.Errorf("Create key = %q, want %q", created.Key, key)
	}
	if created.Count != 5 {
		t.Errorf("Create count = %d, want 5", created.Count)
	}

	got, err := client.Counters().Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Count != 5 {
		t.Errorf("Get count = %d, want 5", got.Count)
	}

	updated, err := client.Counters().Update(ctx, key, &models.UpdateCounterInput{
		Count: omittable.Set(int64(42)),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Count != 42 {
		t.Errorf("Update count = %d, want 42", updated.Count)
	}

	// Upsert via the auto-emitted CounterConflictKey target (the override
	// matches the existing UNIQUE (key) constraint, so the conflict target
	// names the column directly).
	upserted, err := client.Counters().Upsert(ctx, &models.CreateCounterInput{
		Key:   key,
		Count: omittable.Set(int64(100)),
	}, models.CounterConflictKey)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted.Count != 100 {
		t.Errorf("Upsert count = %d, want 100", upserted.Count)
	}
}

// TestCounters_FilterKeyedOps covers the filter-keyed surface that the
// override unlocks alongside the PK-keyed methods: GetMany / Count /
// UpdateWhere against a real Postgres binding. A no-PK table once panicked
// during generation, so this is also the regression coverage that future
// template refactors won't silently break the path.
func TestCounters_FilterKeyedOps(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	prefix := "no-pk-filter-" + t.Name() + "-"
	keys := []string{prefix + "a", prefix + "b", prefix + "c"}
	t.Cleanup(func() {
		for _, k := range keys {
			_ = client.Counters().HardDelete(ctx, k)
		}
	})

	inputs := []*models.CreateCounterInput{
		{Key: keys[0], Count: omittable.Set(int64(1))},
		{Key: keys[1], Count: omittable.Set(int64(2))},
		{Key: keys[2], Count: omittable.Set(int64(3))},
	}
	if _, err := client.Counters().CreateMany(ctx, inputs); err != nil {
		t.Fatalf("CreateMany: %v", err)
	}

	filter := &models.CounterFilter{
		Key: &comparator.ID{In: keys},
	}

	gotMany, err := client.Counters().GetMany(ctx, &models.GetCountersInput{
		Filter: filter,
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(gotMany) != 3 {
		t.Errorf("GetMany len = %d, want 3", len(gotMany))
	}

	count, err := client.Counters().Count(ctx, filter)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}

	bumped, err := client.Counters().UpdateWhere(ctx, filter, &models.UpdateCounterInput{
		Count: omittable.Set(int64(99)),
	})
	if err != nil {
		t.Fatalf("UpdateWhere: %v", err)
	}
	if len(bumped) != 3 {
		t.Errorf("UpdateWhere len = %d, want 3", len(bumped))
	}
	for _, c := range bumped {
		if c.Count != 99 {
			t.Errorf("UpdateWhere %q count = %d, want 99", c.Key, c.Count)
		}
	}
}

// TestRateLimits_CompositeOverride exercises the composite override:
// rate_limits.{org_id, bucket} is promoted to PK via
// tables.rate_limits.primary_key.columns. The resulting client surface is
// keyed on the generated `RateLimitPK` struct, matching the shape of a
// natively-declared composite PRIMARY KEY.
func TestRateLimits_CompositeOverride(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	pk := models.RateLimitPK{OrgID: 4242, Bucket: "no-pk-composite-" + t.Name()}
	t.Cleanup(func() { _ = client.RateLimits().HardDelete(ctx, pk) })

	created, err := client.RateLimits().Create(ctx, &models.CreateRateLimitInput{
		OrgID:  pk.OrgID,
		Bucket: pk.Bucket,
		Count:  omittable.Set(int32(7)),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.OrgID != pk.OrgID || created.Bucket != pk.Bucket {
		t.Errorf("Create PK = (%d,%q), want (%d,%q)", created.OrgID, created.Bucket, pk.OrgID, pk.Bucket)
	}
	if created.Count != 7 {
		t.Errorf("Create count = %d, want 7", created.Count)
	}

	got, err := client.RateLimits().Get(ctx, pk)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Count != 7 {
		t.Errorf("Get count = %d, want 7", got.Count)
	}

	updated, err := client.RateLimits().Update(ctx, pk, &models.UpdateRateLimitInput{
		Count: omittable.Set(int32(11)),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Count != 11 {
		t.Errorf("Update count = %d, want 11", updated.Count)
	}

	// Upsert via the auto-emitted RateLimitConflictOrgIDBucket target (the
	// composite override matches the existing UNIQUE (org_id, bucket)).
	upserted, err := client.RateLimits().Upsert(ctx, &models.CreateRateLimitInput{
		OrgID:  pk.OrgID,
		Bucket: pk.Bucket,
		Count:  omittable.Set(int32(50)),
	}, models.RateLimitConflictOrgIDBucket)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if upserted.Count != 50 {
		t.Errorf("Upsert count = %d, want 50", upserted.Count)
	}
}
