package tests

// Fingerprint kill-switch coverage (PRD §27.5).
//
// The fingerprint segment scopes every cached entry to the exact struct shape
// that produced it. Any migration that regenerates the Go struct produces a
// new fingerprint → new key namespace. PRD §27.5 calls out two safety
// properties this file guards:
//
//  1. After the fingerprint changes, no cache key generated under the old
//     fingerprint is reachable via the new client. Old entries orphan; the
//     new client builds keys with the new fingerprint and issues a fresh DB
//     query on miss instead of serving stale-shape data.
//
//  2. Old entries are eventually cleaned up by backend LRU / TTL OR by the
//     fingerprint-agnostic BuildTablePattern invalidation that already fires
//     on every *Where mutation and from InvalidateTable. The test exercises
//     the deterministic path (InvalidateTable / BuildTablePattern) — LRU is
//     a backend-implementation property unit-tested in cache/memory/.
//
// **Approach.** We can't bump the codegen fingerprint constant from a test —
// it's package-private and re-deriving it would require schema edit + regen.
// Instead, we synthesize the "old fingerprint" scenario by writing a
// payload directly into the backend under a key built with a fake old
// fingerprint via cache.BuildKey. The active client's Get for the same PK
// produces a key with the live fingerprint, so the old payload is
// unreachable from the client; the new key is populated on miss; and a
// fingerprint-agnostic InvalidateTable clears both generations.

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cache"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestFingerprint_OldKeyUnreachableFromClient seeds the cache backend with a
// payload under an old/fake fingerprint for an existing PK. The client's
// next Get for that PK constructs a key with the LIVE fingerprint — the old
// payload remains in the backend but is unreachable through the client
// path; the Get goes to the DB on miss and populates the new-fingerprint
// key. The two keys coexist.
func TestFingerprint_OldKeyUnreachableFromClient(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "FP", SKU: uniqueSku(t, "fp-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Force cold cache so the post-Create cache write is gone.
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}

	// Fabricate a key with a stale fingerprint and push a bogus payload at
	// it. cache.BuildKey is the same function the codegen uses — so the
	// only thing distinguishing this from a "real prior-generation" entry
	// is the fingerprint string.
	oldFingerprint := "old00000"
	oldKey := cache.BuildKey("sqlgen", "", "products", oldFingerprint, p.ID)
	const oldPayload = `{"id":1,"name":"OLD-SHAPE","sku":"old","price":0,"stock":0,"created_at":"2000-01-01T00:00:00Z"}`
	if err := env.backend.Set(ctx, oldKey, []byte(oldPayload), 0); err != nil {
		t.Fatalf("seed old-fingerprint entry: %v", err)
	}

	// Sanity: old key is readable directly.
	if got, err := env.backend.Get(ctx, oldKey); err != nil || string(got) != oldPayload {
		t.Fatalf("backend.Get(oldKey): got=%q err=%v, want stored payload", got, err)
	}

	env.counter.reset()
	env.backend.reset()
	missesBefore := env.metrics.missCount()
	hitsBefore := env.metrics.hitCount()

	// Client Get with the LIVE fingerprint must NOT see the old-fingerprint
	// payload — it should miss the cache and round-trip to the DB.
	got, err := env.client.Products().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("client.Get: %v", err)
	}
	if got.Name != "FP" {
		t.Errorf("Name = %q, want %q (the bogus old payload would have produced %q)", got.Name, "FP", "OLD-SHAPE")
	}
	if env.counter.selectQueries() == 0 {
		t.Error("client.Get hit the old fingerprint cache entry (cross-fingerprint cache leak)")
	}
	if env.metrics.missCount()-missesBefore != 1 {
		t.Errorf("misses delta = %d, want 1 (cache miss expected)",
			env.metrics.missCount()-missesBefore)
	}
	if env.metrics.hitCount() != hitsBefore {
		t.Errorf("hits delta = %d, want 0", env.metrics.hitCount()-hitsBefore)
	}

	// Old payload still sits in the backend (orphaned, awaiting LRU/TTL/pattern eviction).
	if got, err := env.backend.Get(ctx, oldKey); err != nil || string(got) != oldPayload {
		t.Errorf("old key evicted by client.Get? got=%q err=%v — old keys must orphan, not be reaped silently",
			got, err)
	}

	// Sanity: a SECOND Get hits the new-fingerprint key.
	hitsBefore = env.metrics.hitCount()
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("Get warm: %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Error("second Get did not hit the new-fingerprint cache entry")
	}
}

// TestFingerprint_TablePatternEvictsBothGenerations verifies that
// BuildTablePattern (the fingerprint-agnostic pattern) clears BOTH
// current-generation and orphaned-prior-generation entries in one pass —
// the kill-switch property from PRD §27.5. InvalidateTable is the only
// path that reaches it — mutations, the *Where family included, evict by
// key and leave prior-generation entries to TTL.
func TestFingerprint_TablePatternEvictsBothGenerations(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "FPP", SKU: uniqueSku(t, "fpp-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	// Seed an orphaned prior-generation entry.
	oldKey := cache.BuildKey("sqlgen", "", "products", "old00000", p.ID)
	if err := env.backend.Set(ctx, oldKey, []byte(`"orphan"`), 0); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	// Warm the live-fingerprint cache so we have an entry on each side.
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("warm: %v", err)
	}

	// Sanity: both keys are present in the backend before invalidation.
	if got, err := env.backend.Get(ctx, oldKey); err != nil || got == nil {
		t.Fatalf("pre-invalidate orphan: got=%q err=%v, want present", got, err)
	}
	// We can't reference the live key directly (private fingerprint const), but
	// the second Get-as-hit smoke covers it indirectly: a hit means the entry
	// is in the backend.
	hitsBefore := env.metrics.hitCount()
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("warm sanity: %v", err)
	}
	if env.metrics.hitCount()-hitsBefore != 1 {
		t.Fatal("live entry not hot before invalidation — test setup invalid")
	}

	// Pattern invalidation — the kill-switch path. Goes through
	// BuildTablePattern (fingerprint-agnostic per §27.5).
	env.backend.reset()
	if err := env.cache.InvalidateTable(ctx, models.TableProducts); err != nil {
		t.Fatalf("InvalidateTable: %v", err)
	}

	// Verify the pattern shape is fingerprint-and-pk-agnostic. The §27.5
	// invariant: BuildTablePattern omits BOTH segments so the single sweep
	// clears every generation.
	patternCalls := env.backend.filterCalls("invalidate_pattern")
	if len(patternCalls) != 1 {
		t.Fatalf("InvalidateTable: want 1 invalidate_pattern, got %d", len(patternCalls))
	}
	pat := patternCalls[0].pattern
	if !strings.HasSuffix(pat, ":*") {
		t.Errorf("pattern %q: want trailing :*", pat)
	}
	if strings.Contains(pat, ":fingerprint:") {
		t.Errorf("pattern %q: must NOT contain :fingerprint: (PRD §27.5 — pattern is fingerprint-agnostic)", pat)
	}
	if strings.Contains(pat, ":pk:") {
		t.Errorf("pattern %q: must NOT contain :pk:", pat)
	}

	// Both the orphan and the live entry are now gone.
	if got, err := env.backend.Get(ctx, oldKey); err != nil {
		t.Errorf("post-invalidate orphan: backend.Get err=%v, want nil (key gone)", err)
	} else if got != nil {
		t.Errorf("post-invalidate orphan: got=%q, want nil — pattern eviction must clear prior-generation entries", got)
	}

	// And: the next live Get is a MISS, proving the live-fingerprint entry was also reaped.
	missesBefore := env.metrics.missCount()
	if _, err := env.client.Products().Get(ctx, p.ID); err != nil {
		t.Fatalf("post-invalidate Get: %v", err)
	}
	if env.metrics.missCount()-missesBefore != 1 {
		t.Errorf("post-invalidate Get: misses delta = %d, want 1 (live entry should be gone)",
			env.metrics.missCount()-missesBefore)
	}
}
