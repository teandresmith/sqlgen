package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/cache/models"
)

// TestView_SourceMutationInvalidatesView verifies that a mutation on products
// (listed in product_summary.invalidate_on) pattern-invalidates the view's
// cache (PRD §27.11).
func TestView_SourceMutationInvalidatesView(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)

	p, err := env.client.Products().Create(ctx, &models.CreateProductInput{
		Name: "ViewSrc", SKU: uniqueSku(t, "view-1"), Price: 1.0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = env.client.Products().HardDelete(ctx, p.ID) })

	env.backend.reset()
	if _, err := env.client.Products().Update(ctx, p.ID, &models.UpdateProductInput{
		Name: omittable.Set("ViewSrc2"),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Look for at least one invalidate_pattern call targeting product_summary.
	var sawViewPattern bool
	for _, c := range env.backend.filterCalls("invalidate_pattern") {
		if strings.Contains(c.pattern, ":product_summary:") {
			sawViewPattern = true
			break
		}
	}
	if !sawViewPattern {
		t.Errorf("view invalidation: expected an invalidate_pattern call for product_summary, got %+v",
			env.backend.filterCalls("invalidate_pattern"))
	}
}
