package tests

import (
	"context"
	"testing"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestHookNilResultDoesNotPanic pins that a hook short-circuiting with
// (nil, nil) (PRD §21.5) returns the method's zero value and a nil error
// instead of panicking on the result type assertion, as Get / Create do.
func TestHookNilResultDoesNotPanic(t *testing.T) {
	q := func(hook.QueryHandler) hook.QueryHandler {
		return func(context.Context, *hook.QueryContext) (any, error) { return nil, nil }
	}
	m := func(hook.MutationHandler) hook.MutationHandler {
		return func(context.Context, *hook.MutationContext) (any, error) { return nil, nil }
	}
	client := models.New(dbstdlib.New(testDB), models.WithQueryHook(q), models.WithMutationHook(m))
	ctx := context.Background()

	tests := []struct {
		name string
		call func() (zero bool, err error)
	}{
		{"table Paginate", func() (bool, error) {
			r, err := client.Products().Paginate(ctx, models.PaginateInput[models.ProductFilter]{Limit: 1})
			return r == nil, err
		}},
		{"table Connection", func() (bool, error) {
			r, err := client.Products().Connection(ctx, models.ConnectionInput[models.ProductFilter]{})
			return r == nil, err
		}},
		{"table Count", func() (bool, error) {
			n, err := client.Products().Count(ctx, nil)
			return n == 0, err
		}},
		{"table Exists", func() (bool, error) {
			ok, err := client.Products().Exists(ctx, 1)
			return !ok, err
		}},
		{"table ExistsWhere", func() (bool, error) {
			ok, err := client.Products().ExistsWhere(ctx, nil)
			return !ok, err
		}},
		{"view Paginate", func() (bool, error) {
			r, err := client.ProductSummary().Paginate(ctx, models.PaginateInput[models.ProductSummaryFilter]{Limit: 1})
			return r == nil, err
		}},
		{"view Connection", func() (bool, error) {
			r, err := client.ProductSummary().Connection(ctx, models.ConnectionInput[models.ProductSummaryFilter]{})
			return r == nil, err
		}},
		{"view Count", func() (bool, error) {
			n, err := client.ProductSummary().Count(ctx, nil)
			return n == 0, err
		}},
		{"RawExec", func() (bool, error) {
			r, err := client.RawExec(ctx, "SELECT 1")
			return r == nil, err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s with a (nil, nil) hook panicked: %v", tt.name, r)
				}
			}()
			zero, err := tt.call()
			if err != nil {
				t.Fatalf("%s with a (nil, nil) hook: err = %v, want nil", tt.name, err)
			}
			if !zero {
				t.Errorf("%s with a (nil, nil) hook returned a non-zero value", tt.name)
			}
		})
	}
}
