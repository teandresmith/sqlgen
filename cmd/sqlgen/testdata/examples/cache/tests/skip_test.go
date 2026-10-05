package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestSkip_CacheOnGet verifies SkipCache: true on Get bypasses the read-through
// entirely — no backend Get and no Set (PRD §27.7).
func TestSkip_CacheOnGet(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "Skip", SKU: uniqueSku(t, "skip-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	env.backend.reset()
	if _, err := env.client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipCache = true
	}); err != nil {
		t.Fatalf("Get (SkipCache): %v", err)
	}

	if got := len(env.backend.filterCalls("get", "set")); got != 0 {
		t.Errorf("SkipCache on Get: want 0 cache calls, got %d", got)
	}
}

// TestSkip_CacheOnMutation verifies SkipCache: true on a mutation bypasses
// both the cache set (Create) and invalidation (Update) — no backend calls.
func TestSkip_CacheOnMutation(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "SkipM", SKU: uniqueSku(t, "skipm-1"), Price: 1.0,
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipCache = true
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	if got := len(env.backend.filterCalls("set")); got != 0 {
		t.Errorf("SkipCache on Create: want 0 Set calls, got %d", got)
	}

	env.backend.reset()
	if _, err := env.client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
		Name: omittable.Set("SkipM2"),
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipCache = true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := len(env.backend.filterCalls("invalidate", "invalidate_many", "invalidate_pattern")); got != 0 {
		t.Errorf("SkipCache on Update: want 0 invalidate calls, got %d", got)
	}
}

// TestSkip_HooksImpliesSkipCache verifies SkipHooks: true also disables cache
// read-through AND cache-write-through for mutations (PRD §27.7).
func TestSkip_HooksImpliesSkipCache(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "SkipH", SKU: uniqueSku(t, "skiph-1"), Price: 1.0,
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipHooks = true
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	if got := len(env.backend.snapshot()); got != 0 {
		t.Errorf("SkipHooks: want 0 cache calls, got %d", got)
	}

	env.backend.reset()
	if _, err := env.client.Products().Get(ctx, p.ID, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.SkipHooks = true
	}); err != nil {
		t.Fatalf("Get (SkipHooks): %v", err)
	}
	if got := len(env.backend.snapshot()); got != 0 {
		t.Errorf("SkipHooks Get: want 0 cache calls, got %d", got)
	}
}

// TestSkip_PerTableCacheDisabled verifies audit_logs (cache.enabled: false in
// config) produces zero cache activity.
func TestSkip_PerTableCacheDisabled(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	env.backend.reset()
	a, err := env.client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message: "log-one",
	})
	if err != nil {
		t.Fatalf("Create audit: %v", err)
	}
	t.Cleanup(func() { _ = env.client.AuditLogs().HardDelete(ctx, a.ID) })

	// audit_logs-keyed calls only — the generated audit log table has no
	// cache code, so any recorded calls here would indicate a generator
	// leak.
	if got := len(env.backend.callsForTable("audit_logs")); got != 0 {
		t.Errorf("audit_logs (cache disabled): want 0 cache calls, got %d", got)
	}

	// Reads are also no-ops.
	env.backend.reset()
	if _, err := env.client.AuditLogs().Get(ctx, a.ID); err != nil {
		t.Fatalf("Get audit: %v", err)
	}
	if got := len(env.backend.callsForTable("audit_logs")); got != 0 {
		t.Errorf("audit_logs Get: want 0 cache calls, got %d", got)
	}

	// Update / HardDelete similarly.
	env.backend.reset()
	if _, err := env.client.AuditLogs().Update(ctx, a.ID, &models.UpdateAuditLogInput{
		Message: omittable.Set("log-two"),
	}); err != nil {
		t.Fatalf("Update audit: %v", err)
	}
	if got := len(env.backend.callsForTable("audit_logs")); got != 0 {
		t.Errorf("audit_logs Update: want 0 cache calls, got %d", got)
	}
}

// TestSkip_RelationshipBypass verifies that a Get with a relationship flag on
// FieldOptions bypasses the cache entirely — no read, no write (PRD §27.6).
func TestSkip_RelationshipBypass(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	u, err := env.client.Users().Create(ctx, &models.CreateUserInput{
		Name: "Rel", Email: uniqueSku(t, "rel-1") + "@ex.com",
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	if _, err := env.client.Profiles().Create(ctx, &models.CreateProfileInput{
		UserID: u.ID, Bio: omittable.Set(new("hi")),
	}); err != nil {
		t.Fatalf("Create profile: %v", err)
	}
	t.Cleanup(func() {
		_ = env.client.Profiles().HardDeleteWhere(ctx, &models.ProfileFilter{
			UserID: &comparator.Number[int64]{Eq: new(u.ID)},
		})
		_ = env.client.Users().HardDelete(ctx, u.ID)
	})

	env.backend.reset()
	got, err := env.client.Users().Get(ctx, u.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{ID: true, Name: true, Email: true, Profile: &models.ProfileFieldOptions{ID: true, UserID: true, Bio: true}}
	})
	if err != nil {
		t.Fatalf("Get (with rel): %v", err)
	}
	if got.Profile == nil {
		t.Fatal("Profile not loaded — test setup wrong")
	}

	// With the relationship flag set, cache must be bypassed entirely.
	if len(env.backend.snapshot()) != 0 {
		t.Errorf("relationship Get: want 0 cache calls, got %d", len(env.backend.snapshot()))
	}
}
