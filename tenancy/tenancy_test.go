package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/teandresmith/sqlgen/tenancy"
)

// WorkspaceID is a named-wrapper type — the "custom comparable" path the
// generator must support in addition to the built-in integrations.
type WorkspaceID uuid.UUID

func TestTenantResolverInstantiations(t *testing.T) {
	t.Run("uuid.UUID", func(t *testing.T) {
		want := uuid.Must(uuid.NewRandom())
		var r tenancy.TenantResolver[uuid.UUID] = func(context.Context) (uuid.UUID, error) {
			return want, nil
		}
		got, err := r(context.Background())
		if err != nil {
			t.Fatalf("resolver returned unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("int64", func(t *testing.T) {
		var r tenancy.TenantResolver[int64] = func(context.Context) (int64, error) {
			return 42, nil
		}
		got, err := r(context.Background())
		if err != nil {
			t.Fatalf("resolver returned unexpected error: %v", err)
		}
		if got != 42 {
			t.Errorf("got %d, want 42", got)
		}
	})

	t.Run("string", func(t *testing.T) {
		var r tenancy.TenantResolver[string] = func(context.Context) (string, error) {
			return "workspace-42", nil
		}
		got, err := r(context.Background())
		if err != nil {
			t.Fatalf("resolver returned unexpected error: %v", err)
		}
		if got != "workspace-42" {
			t.Errorf("got %q, want %q", got, "workspace-42")
		}
	})

	t.Run("named wrapper over uuid.UUID", func(t *testing.T) {
		want := WorkspaceID(uuid.Must(uuid.NewRandom()))
		var r tenancy.TenantResolver[WorkspaceID] = func(context.Context) (WorkspaceID, error) {
			return want, nil
		}
		got, err := r(context.Background())
		if err != nil {
			t.Fatalf("resolver returned unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestTenantResolverPropagatesError(t *testing.T) {
	sentinel := errors.New("resolver blew up")
	var r tenancy.TenantResolver[string] = func(context.Context) (string, error) {
		return "", sentinel
	}
	_, err := r(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true")
	}
}

func TestTenantResolverReceivesContext(t *testing.T) {
	type ctxKey struct{}
	want := "from-context"
	var r tenancy.TenantResolver[string] = func(ctx context.Context) (string, error) {
		v, _ := ctx.Value(ctxKey{}).(string)
		return v, nil
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, want)
	got, err := r(ctx)
	if err != nil {
		t.Fatalf("resolver returned unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestErrMissing(t *testing.T) {
	const want = "tenancy: tenant missing from context"
	if got := tenancy.ErrMissing.Error(); got != want {
		t.Errorf("ErrMissing.Error() = %q, want %q", got, want)
	}

	wrapped := fmt.Errorf("get product: resolve tenant: %w", tenancy.ErrMissing)
	if !errors.Is(wrapped, tenancy.ErrMissing) {
		t.Errorf("errors.Is(wrapped, ErrMissing) = false, want true")
	}
}

func TestErrMismatch(t *testing.T) {
	const want = "tenancy: tenant on mutation input does not match resolved tenant; use CallOptions.SkipTenancy to override"
	if got := tenancy.ErrMismatch.Error(); got != want {
		t.Errorf("ErrMismatch.Error() = %q, want %q", got, want)
	}

	wrapped := fmt.Errorf("update product: %w", tenancy.ErrMismatch)
	if !errors.Is(wrapped, tenancy.ErrMismatch) {
		t.Errorf("errors.Is(wrapped, ErrMismatch) = false, want true")
	}
}

func TestErrMissingAndErrMismatchAreDistinct(t *testing.T) {
	if errors.Is(tenancy.ErrMissing, tenancy.ErrMismatch) {
		t.Error("ErrMissing should not match ErrMismatch")
	}
	if errors.Is(tenancy.ErrMismatch, tenancy.ErrMissing) {
		t.Error("ErrMismatch should not match ErrMissing")
	}
}

// TestCachedTenantRoundTrip covers the ctx cache the generated resolveTenant
// helpers short-circuit on.
//
// The zero-value case is the one that carries weight. A cached zero is a real
// answer — "the resolver ran and returned the zero value" — so presence has to
// be reported separately from the value; inferring absence from a zero would
// re-invoke the resolver on every hop of an operation whose tenant legitimately
// is the zero value, and would hide the case where a fail-closed table must see
// that zero and raise ErrMissing rather than inherit someone else's decision
// about it.
func TestCachedTenantRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		seed  func(context.Context) context.Context
		want  string
		wantO bool
	}{
		{
			name:  "absent",
			seed:  func(ctx context.Context) context.Context { return ctx },
			want:  "",
			wantO: false,
		},
		{
			name:  "present",
			seed:  func(ctx context.Context) context.Context { return tenancy.WithResolvedTenant(ctx, "acme") },
			want:  "acme",
			wantO: true,
		},
		{
			name:  "present and zero",
			seed:  func(ctx context.Context) context.Context { return tenancy.WithResolvedTenant(ctx, "") },
			want:  "",
			wantO: true,
		},
		{
			name: "last write wins",
			seed: func(ctx context.Context) context.Context {
				return tenancy.WithResolvedTenant(tenancy.WithResolvedTenant(ctx, "first"), "second")
			},
			want:  "second",
			wantO: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tenancy.CachedTenant[string](tt.seed(context.Background()))
			if got != tt.want || ok != tt.wantO {
				t.Errorf("CachedTenant = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantO)
			}
		})
	}
}

// TestCachedTenantIsKeyedByType pins the per-T slot: a package wiring two
// clients with different tenant types must not have one read the other's value.
func TestCachedTenantIsKeyedByType(t *testing.T) {
	ctx := tenancy.WithResolvedTenant(context.Background(), "acme")

	if got, ok := tenancy.CachedTenant[int64](ctx); ok || got != 0 {
		t.Errorf("tenancy.CachedTenant[int64] over a string entry = (%d, %v), want (0, false)", got, ok)
	}

	ctx = tenancy.WithResolvedTenant(ctx, int64(7))
	if got, ok := tenancy.CachedTenant[string](ctx); !ok || got != "acme" {
		t.Errorf("tenancy.CachedTenant[string] after an int64 write = (%q, %v), want (\"acme\", true)", got, ok)
	}
	if got, ok := tenancy.CachedTenant[int64](ctx); !ok || got != 7 {
		t.Errorf("tenancy.CachedTenant[int64] = (%d, %v), want (7, true)", got, ok)
	}
}

// TestWithResolvedTenantDerivesANewContext pins that the cache does not leak
// backwards into the caller's ctx. Two sequential operations on one
// caller-supplied ctx must resolve independently — a resolver may read ctx state
// that changed between them.
func TestWithResolvedTenantDerivesANewContext(t *testing.T) {
	parent := context.Background()
	child := tenancy.WithResolvedTenant(parent, "acme")

	if _, ok := tenancy.CachedTenant[string](parent); ok {
		t.Error("WithResolvedTenant mutated the ctx it was handed; the cache must not outlive the operation that set it")
	}
	if _, ok := tenancy.CachedTenant[string](child); !ok {
		t.Error("derived ctx does not carry the value")
	}
}
