package tests

import (
	"context"
	"errors"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"

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
