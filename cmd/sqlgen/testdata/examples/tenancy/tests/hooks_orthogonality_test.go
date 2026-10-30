package tests

import (
	"context"
	"errors"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// The tenant filter is applied by the SQL builders, not by a hook: a call that
// skips every hook is still scoped to the resolver's tenant.
func TestHooks_SkippingHooksKeepsTheTenantFilter(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	p := seedProduct(t) // tenant A's
	envB := newEnv(t, withResolver(staticResolver(tenantB)))
	skipHooks := func(o *models.CallOptions[models.ProductFieldOptions]) { o.SkipHooks = true }

	got, err := envB.client.Products().GetMany(ctx, nil, skipHooks)
	if err != nil {
		t.Fatalf("tenant B GetMany with SkipHooks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("tenant B GetMany with SkipHooks returned %d rows of tenant A's, want 0", len(got))
	}
	if _, err := envB.client.Products().Get(ctx, p.ID, skipHooks); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("tenant B Get of tenant A's product with SkipHooks error = %v, want ErrNotFound", err)
	}
}

// A query hook sees the caller's input as given: the tenant condition is added
// to the SQL, not to the Input struct.
func TestHooks_QueryInputCarriesNoTenantCondition(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	seedProduct(t)
	var seen []*models.GetProductsInput
	record := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			if in, ok := q.Input.(*models.GetProductsInput); ok {
				seen = append(seen, in)
			}
			return next(ctx, q)
		}
	}
	client := models.New(dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithQueryHook(record),
	)
	in := &models.GetProductsInput{Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("seed")}}}
	want := models.GetProductsInput{Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("seed")}}}

	got, err := client.Products().GetMany(ctx, in)
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetMany returned %d rows, want tenant A's 1", len(got))
	}
	if len(seen) != 1 {
		t.Fatalf("query hook saw %d GetProductsInput values, want 1", len(seen))
	}
	// Unexported fields included: an internal condition counts as a change.
	if diff := gocmp.Diff(want, *seen[0], gocmp.AllowUnexported(models.GetProductsInput{})); diff != "" {
		t.Errorf("input the query hook saw (-want +got):\n%s", diff)
	}
}

// On a table whose primary key holds the tenant column, a mutation hook's
// AffectedPKs are full PK values, tenant component included.
func TestHooks_AffectedPKsCarryTheTenantComponent(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	var affected [][]any
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if m.Table == models.TableOrderItems {
				affected = append(affected, m.AffectedPKs)
			}
			return res, err
		}
	}
	client := models.New(dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithMutationHook(record),
	)
	pk := models.OrderItemPK{WorkspaceID: tenantA, OrderID: 1, ProductID: 10}
	if _, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{WorkspaceID: tenantA, OrderID: 1, ProductID: 10, Quantity: 1, UnitPrice: 1}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := client.OrderItems().Update(ctx, pk, &models.UpdateOrderItemInput{Quantity: omittable.Set(int64(2))}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := client.OrderItems().HardDelete(ctx, pk); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	want := [][]any{{pk}, {pk}, {pk}}
	if diff := gocmp.Diff(want, affected); diff != "" {
		t.Errorf("AffectedPKs of Create, Update, HardDelete (-want +got):\n%s", diff)
	}
}
