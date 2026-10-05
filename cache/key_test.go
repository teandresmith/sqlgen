package cache_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cache"
)

func TestBuildKey(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		schema      string
		table       string
		fingerprint string
		pk          any
		want        string
	}{
		{
			name:        "postgres with schema and fingerprint",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "products",
			fingerprint: "1a2b3c",
			pk:          42,
			want:        "sqlgen:public.products:fingerprint:v1a2b3c:pk:42",
		},
		{
			name:        "postgres uuid pk",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "products",
			fingerprint: "1a2b3c",
			pk:          "550e8400-e29b-41d4-a716-446655440000",
			want:        "sqlgen:public.products:fingerprint:v1a2b3c:pk:550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "mysql empty schema",
			prefix:      "sqlgen",
			schema:      "",
			table:       "products",
			fingerprint: "1a2b3c",
			pk:          42,
			want:        "sqlgen:products:fingerprint:v1a2b3c:pk:42",
		},
		{
			name:        "sqlite empty schema",
			prefix:      "sqlgen",
			schema:      "",
			table:       "users",
			fingerprint: "9z8y7x",
			pk:          "admin",
			want:        "sqlgen:users:fingerprint:v9z8y7x:pk:admin",
		},
		{
			name:        "custom prefix",
			prefix:      "myapp",
			schema:      "public",
			table:       "products",
			fingerprint: "1a2b3c",
			pk:          42,
			want:        "myapp:public.products:fingerprint:v1a2b3c:pk:42",
		},
		{
			name:        "empty fingerprint drops fingerprint segment",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "products",
			fingerprint: "",
			pk:          42,
			want:        "sqlgen:public.products:pk:42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildKey(tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.pk)
			if got != tt.want {
				t.Errorf("BuildKey(%q, %q, %q, %q, %v) = %q, want %q", tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.pk, got, tt.want)
			}
		})
	}
}

func TestBuildKeyLabelOrdering(t *testing.T) {
	got := cache.BuildKey("sqlgen", "public", "products", "1a2b3c", 42)
	// fingerprint: must appear before pk:
	fpIdx := strings.Index(got, "fingerprint:")
	pkIdx := strings.Index(got, "pk:")
	if fpIdx < 0 || pkIdx < 0 {
		t.Fatalf("BuildKey() missing labels, got %q", got)
	}
	if fpIdx >= pkIdx {
		t.Errorf("BuildKey() label ordering wrong: fingerprint at %d, pk at %d in %q", fpIdx, pkIdx, got)
	}
}

func TestBuildKeyFingerprintPresence(t *testing.T) {
	withFp := cache.BuildKey("sqlgen", "public", "products", "1a2b3c4d", 42)
	wantWith := "sqlgen:public.products:fingerprint:v1a2b3c4d:pk:42"
	if withFp != wantWith {
		t.Errorf("BuildKey with fingerprint = %q, want %q", withFp, wantWith)
	}

	withoutFp := cache.BuildKey("sqlgen", "public", "products", "", 42)
	wantWithout := "sqlgen:public.products:pk:42"
	if withoutFp != wantWithout {
		t.Errorf("BuildKey without fingerprint = %q, want %q", withoutFp, wantWithout)
	}
	if strings.Contains(withoutFp, "fingerprint:") {
		t.Errorf("BuildKey without fingerprint = %q, still contains fingerprint: segment", withoutFp)
	}
}

func TestBuildCompositeKey(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		schema      string
		table       string
		fingerprint string
		pks         []any
		want        string
	}{
		{
			name:        "two-column composite",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "order_items",
			fingerprint: "4e5f6g",
			pks:         []any{"order123", "product456"},
			want:        "sqlgen:public.order_items:fingerprint:v4e5f6g:pk:order123:product456",
		},
		{
			name:        "three-column composite",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "ledger",
			fingerprint: "aabbcc",
			pks:         []any{"tenant1", 2025, "acct-7"},
			want:        "sqlgen:public.ledger:fingerprint:vaabbcc:pk:tenant1:2025:acct-7",
		},
		{
			name:        "mysql two-column composite no schema",
			prefix:      "sqlgen",
			schema:      "",
			table:       "order_items",
			fingerprint: "4e5f6g",
			pks:         []any{"order123", "product456"},
			want:        "sqlgen:order_items:fingerprint:v4e5f6g:pk:order123:product456",
		},
		{
			name:        "empty fingerprint two-column composite",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "order_items",
			fingerprint: "",
			pks:         []any{"order123", "product456"},
			want:        "sqlgen:public.order_items:pk:order123:product456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildCompositeKey(tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.pks)
			if got != tt.want {
				t.Errorf("BuildCompositeKey(%q, %q, %q, %q, %v) = %q, want %q", tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.pks, got, tt.want)
			}
		})
	}
}

func TestBuildTenantKey(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		schema      string
		table       string
		fingerprint string
		tenant      any
		pk          any
		want        string
	}{
		{
			name:        "postgres tenanted with schema",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "products",
			fingerprint: "1a2b3c",
			tenant:      "ws-abc",
			pk:          42,
			want:        "sqlgen:public.products:tenant:ws-abc:fingerprint:v1a2b3c:pk:42",
		},
		{
			name:        "mysql tenanted empty schema",
			prefix:      "sqlgen",
			schema:      "",
			table:       "products",
			fingerprint: "1a2b3c",
			tenant:      "ws-abc",
			pk:          42,
			want:        "sqlgen:products:tenant:ws-abc:fingerprint:v1a2b3c:pk:42",
		},
		{
			name:        "sqlite tenanted empty schema uuid pk",
			prefix:      "sqlgen",
			schema:      "",
			table:       "users",
			fingerprint: "9z8y7x",
			tenant:      "tenant-7",
			pk:          "550e8400-e29b-41d4-a716-446655440000",
			want:        "sqlgen:users:tenant:tenant-7:fingerprint:v9z8y7x:pk:550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "integer tenant",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "orders",
			fingerprint: "abcd12",
			tenant:      42,
			pk:          "ord-1",
			want:        "sqlgen:public.orders:tenant:42:fingerprint:vabcd12:pk:ord-1",
		},
		{
			name:        "empty fingerprint preserves tenant segment",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "products",
			fingerprint: "",
			tenant:      "ws-abc",
			pk:          42,
			want:        "sqlgen:public.products:tenant:ws-abc:pk:42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildTenantKey(tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.tenant, tt.pk)
			if got != tt.want {
				t.Errorf("BuildTenantKey(%q, %q, %q, %q, %v, %v) = %q, want %q", tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.tenant, tt.pk, got, tt.want)
			}
		})
	}
}

func TestBuildTenantKeyLabelOrdering(t *testing.T) {
	got := cache.BuildTenantKey("sqlgen", "public", "products", "1a2b3c", "ws-abc", 42)
	tIdx := strings.Index(got, "tenant:")
	fpIdx := strings.Index(got, "fingerprint:")
	pkIdx := strings.Index(got, "pk:")
	if tIdx < 0 || fpIdx < 0 || pkIdx < 0 {
		t.Fatalf("BuildTenantKey() missing labels, got %q", got)
	}
	if tIdx >= fpIdx || fpIdx >= pkIdx {
		t.Errorf("BuildTenantKey() label ordering wrong: tenant=%d fingerprint=%d pk=%d in %q (want tenant<fingerprint<pk)", tIdx, fpIdx, pkIdx, got)
	}
}

func TestBuildCompositeTenantKey(t *testing.T) {
	tests := []struct {
		name        string
		prefix      string
		schema      string
		table       string
		fingerprint string
		tenant      any
		pks         []any
		want        string
	}{
		{
			name:        "postgres two-column composite tenanted",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "order_items",
			fingerprint: "4e5f6g",
			tenant:      "ws-abc",
			pks:         []any{"order123", "product456"},
			want:        "sqlgen:public.order_items:tenant:ws-abc:fingerprint:v4e5f6g:pk:order123:product456",
		},
		{
			name:        "mysql two-column composite tenanted no schema",
			prefix:      "sqlgen",
			schema:      "",
			table:       "order_items",
			fingerprint: "4e5f6g",
			tenant:      "ws-abc",
			pks:         []any{"order123", "product456"},
			want:        "sqlgen:order_items:tenant:ws-abc:fingerprint:v4e5f6g:pk:order123:product456",
		},
		{
			name:        "three-column composite tenanted",
			prefix:      "sqlgen",
			schema:      "public",
			table:       "ledger",
			fingerprint: "aabbcc",
			tenant:      "tenant1",
			pks:         []any{"acct-7", 2025, "txn-9"},
			want:        "sqlgen:public.ledger:tenant:tenant1:fingerprint:vaabbcc:pk:acct-7:2025:txn-9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildCompositeTenantKey(tt.prefix, tt.schema, tt.table, tt.fingerprint, tt.tenant, tt.pks)
			if got != tt.want {
				t.Errorf("BuildCompositeTenantKey(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTenantKeyDifferentTenantsDistinctKeys(t *testing.T) {
	a := cache.BuildTenantKey("sqlgen", "public", "products", "1a2b3c", "ws-A", 42)
	b := cache.BuildTenantKey("sqlgen", "public", "products", "1a2b3c", "ws-B", 42)
	if a == b {
		t.Errorf("BuildTenantKey() returned same key for different tenants: %q", a)
	}
}

func TestBuildTenantTablePattern(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		schema string
		table  string
		tenant any
		want   string
	}{
		{
			name:   "postgres tenanted with schema",
			prefix: "sqlgen",
			schema: "public",
			table:  "products",
			tenant: "ws-abc",
			want:   "sqlgen:public.products:tenant:ws-abc:*",
		},
		{
			name:   "mysql tenanted empty schema",
			prefix: "sqlgen",
			schema: "",
			table:  "products",
			tenant: "ws-abc",
			want:   "sqlgen:products:tenant:ws-abc:*",
		},
		{
			name:   "integer tenant",
			prefix: "sqlgen",
			schema: "public",
			table:  "orders",
			tenant: 7,
			want:   "sqlgen:public.orders:tenant:7:*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildTenantTablePattern(tt.prefix, tt.schema, tt.table, tt.tenant)
			if got != tt.want {
				t.Errorf("BuildTenantTablePattern(%q, %q, %q, %v) = %q, want %q", tt.prefix, tt.schema, tt.table, tt.tenant, got, tt.want)
			}
			if strings.Contains(got, "fingerprint:") {
				t.Errorf("BuildTenantTablePattern() = %q, must not contain fingerprint: segment", got)
			}
			if strings.Contains(got, ":pk:") {
				t.Errorf("BuildTenantTablePattern() = %q, must not contain :pk: segment", got)
			}
			// Single-wildcard requirement (PRD §29.5): exactly one trailing "*",
			// no intermediate wildcards in the pattern.
			if !strings.HasSuffix(got, ":*") {
				t.Errorf("BuildTenantTablePattern() = %q, must end with :*", got)
			}
			if strings.Count(got, "*") != 1 {
				t.Errorf("BuildTenantTablePattern() = %q, must contain exactly one wildcard", got)
			}
		})
	}
}

func TestBuildTablePattern(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		schema string
		table  string
		want   string
	}{
		{
			name:   "postgres",
			prefix: "sqlgen",
			schema: "public",
			table:  "products",
			want:   "sqlgen:public.products:*",
		},
		{
			name:   "mysql empty schema",
			prefix: "sqlgen",
			schema: "",
			table:  "products",
			want:   "sqlgen:products:*",
		},
		{
			name:   "custom prefix",
			prefix: "myapp",
			schema: "public",
			table:  "orders",
			want:   "myapp:public.orders:*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.BuildTablePattern(tt.prefix, tt.schema, tt.table)
			if got != tt.want {
				t.Errorf("BuildTablePattern(%q, %q, %q) = %q, want %q", tt.prefix, tt.schema, tt.table, got, tt.want)
			}
			if strings.Contains(got, "fingerprint:") {
				t.Errorf("BuildTablePattern() = %q, must not contain fingerprint: segment", got)
			}
			if strings.Contains(got, ":pk:") {
				t.Errorf("BuildTablePattern() = %q, must not contain :pk: segment", got)
			}
		})
	}
}
