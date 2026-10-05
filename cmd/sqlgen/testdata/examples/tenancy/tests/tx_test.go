package tests

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// omittableSetTenantB wraps tenantB in omittable so the tx-test reads cleanly.
func omittableSetTenantB() omittable.Value[uuid.UUID] {
	return omittable.Set(tenantB)
}

// TestTx_TenantIsConsistentAcrossOpsInATx verifies §29.10 transaction
// behavior: a transaction opened with a context that resolves to tenant A
// applies that tenant to every read and write inside the tx.
//
// The consistency comes from the ctx, not from a per-transaction cache. The
// resolver is invoked once per operation (PRD §29.6, §29.11) and the answers
// agree because each call is handed the same ctx — which is why the assertion
// below is a lower bound on the count rather than 1.
//
// We use a counting resolver to assert "called per op" — the resolver is the
// integration point and being silently bypassed would be a bug class on its
// own.
func TestTx_TenantIsConsistentAcrossOpsInATx(t *testing.T) {
	resetDB(t)

	var resolveCount atomic.Int64
	resolver := func(ctx context.Context) (uuid.UUID, error) {
		resolveCount.Add(1)
		v, ok := ctx.Value(tenantCtxKey{}).(uuid.UUID)
		if !ok {
			return uuid.Nil(), tenancy.ErrMissing
		}
		return v, nil
	}

	env := newEnv(t, withResolver(resolver))
	ctx := withTenantCtx(context.Background(), tenantA)

	err := env.client.WithTx(ctx, "tenant-tx", func(ctx context.Context) error {
		// Two writes inside the tx; both should auto-fill workspace_id from
		// tenant A.
		_, err := env.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "tx-1", SKU: "TX-1", Price: 1.0,
		})
		if err != nil {
			return err
		}
		_, err = env.client.Products().Create(ctx, &models.CreateProductInput{
			Name: "tx-2", SKU: "TX-2", Price: 2.0,
		})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	// Both rows visible to A, none to B.
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	listA, err := env.client.Products().GetMany(ctx, &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("post-tx GetMany A: %v", err)
	}
	if len(listA) != 2 {
		t.Errorf("post-tx GetMany A: %d rows, want 2", len(listA))
	}
	listB, err := envB.client.Products().GetMany(context.Background(), &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("post-tx GetMany B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("post-tx GetMany B: %d rows, want 0 (cross-tenant leak)", len(listB))
	}

	// At least 2 resolves (one per Create). The current implementation
	// resolves per operation; if the future ever moves to "resolve once at
	// tx-open", this assertion guards that we don't regress to "never
	// resolve".
	if resolveCount.Load() < 2 {
		t.Errorf("resolveCount = %d, want >= 2", resolveCount.Load())
	}
}

// TestTx_CrossTenantWriteRejected verifies §29.11 "cross-tenant transactions
// not supported": when the tx is open under tenant A and a Create inside the
// tx supplies tenant B as the input value (without SkipTenancy), the mismatch
// guard kicks in and the tx must be rolled back. The driver layer's tx state
// stays consistent with the tenancy guarantee.
func TestTx_CrossTenantWriteRejected(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	ctx := context.Background()
	err := envA.client.WithTx(ctx, "cross-tenant", func(ctx context.Context) error {
		_, err := envA.client.Products().Create(ctx, &models.CreateProductInput{
			WorkspaceID: omittableSetTenantB(), // helper to keep the import shape clean
			Name:        "cross-tenant-write",
			SKU:         "X-1", Price: 1.0,
		})
		return err
	})
	if !errors.Is(err, tenancy.ErrMismatch) {
		t.Fatalf("WithTx with cross-tenant write: err = %v, want tenancy.ErrMismatch", err)
	}

	// The rolled-back tx left no row.
	listA, err := envA.client.Products().GetMany(ctx, &models.GetProductsInput{})
	if err != nil {
		t.Fatalf("post-rollback GetMany: %v", err)
	}
	if len(listA) != 0 {
		t.Errorf("post-rollback rows on A: %d, want 0 (rollback failed)", len(listA))
	}
}
