package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/segmentio/ksuid"

	"github.com/teandresmith/sqlgen/cache"
	"github.com/teandresmith/sqlgen/tenancy"
)

// TestKSUID_TenantResolverInstantiates verifies the §29.2.4 contract that
// `TenantResolver[T]` accepts ANY `comparable` type — not just uuid.UUID. KSUID
// is the canonical "custom comparable wrapper" example: a fixed-size byte
// array with String() returning a base62 form. The smoke is compile-time
// (the type assignment) plus a real call: round-trip ctx → resolver → value
// works the same way it would for any tenant Go type.
func TestKSUID_TenantResolverInstantiates(t *testing.T) {
	want := ksuid.New()
	var r tenancy.TenantResolver[ksuid.KSUID] = func(context.Context) (ksuid.KSUID, error) {
		return want, nil
	}
	got, err := r(context.Background())
	if err != nil {
		t.Fatalf("KSUID resolver: %v", err)
	}
	if got != want {
		t.Errorf("KSUID resolver: got %v, want %v", got, want)
	}
}

// TestKSUID_CacheKeyStringification verifies the §29.5 cache-key path for
// KSUID tenants: BuildTenantKey + BuildCompositeTenantKey + BuildTenantTablePattern
// all stringify the tenant via %v, which calls KSUID.String() (base62). This
// is the contract Redis / Memcached backends rely on — the key is opaque text,
// not a binary blob.
func TestKSUID_CacheKeyStringification(t *testing.T) {
	tA := ksuid.New()
	tB := ksuid.New()

	tests := []struct {
		name string
		got  string
	}{
		{"BuildTenantKey simple PK", cache.BuildTenantKey("sqlgen", "", "items", "v1", tA, int64(42))},
		{"BuildCompositeTenantKey", cache.BuildCompositeTenantKey("sqlgen", "", "items", "v1", tA, []any{int64(1), "alpha"})},
		{"BuildTenantTablePattern", cache.BuildTenantTablePattern("sqlgen", "", "items", tA)},
	}

	wantTenant := tA.String()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.got, wantTenant) {
				t.Errorf("key %q does not contain KSUID string %q", tt.got, wantTenant)
			}
			if !strings.Contains(tt.got, "tenant:") {
				t.Errorf("key %q missing tenant: label segment", tt.got)
			}
		})
	}

	// Distinct KSUID tenants → distinct keys.
	keyA := cache.BuildTenantKey("sqlgen", "", "items", "v1", tA, int64(42))
	keyB := cache.BuildTenantKey("sqlgen", "", "items", "v1", tB, int64(42))
	if keyA == keyB {
		t.Errorf("KSUID tenants A and B produce the same cache key: %q", keyA)
	}
}
