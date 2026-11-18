package tests

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestWithCache_OutermostWhateverTheOptionOrder registers a user query hook
// before and after WithCache. Either way the cache hook runs outside it, so a
// cache hit returns before the user hook runs.
func TestWithCache_OutermostWhateverTheOptionOrder(t *testing.T) {
	tests := []struct {
		name       string
		cacheFirst bool
	}{
		{name: "WithCache before the user hook", cacheFirst: true},
		{name: "WithCache after the user hook", cacheFirst: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			env := newEnv(t)

			var userGets atomic.Int32
			userHook := hook.OnQuery(func(next hook.QueryHandler) hook.QueryHandler {
				return func(ctx context.Context, q *hook.QueryContext) (any, error) {
					userGets.Add(1)
					return next(ctx, q)
				}
			}, hook.OpGet)
			opts := []models.ClientOption{models.WithQueryHook(userHook), models.WithCache(env.cache)}
			if tt.cacheFirst {
				opts = []models.ClientOption{models.WithCache(env.cache), models.WithQueryHook(userHook)}
			}
			client := models.New(env.counter, opts...)

			p, err := client.Products().Create(ctx, &models.CreateProductInput{
				Name: "Outer", SKU: uniqueSku(t, "outer"), Price: 1.0,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

			// Create wrote the entity through, so this Get is a hit.
			if _, err := client.Products().Get(ctx, p.ID); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got := userGets.Load(); got != 0 {
				t.Errorf("user query hook ran %d times on a cache hit, want 0 (cache hook outermost)", got)
			}
		})
	}
}
