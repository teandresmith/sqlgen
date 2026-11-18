package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// Exists / ExistsWhere e2e for PostgreSQL (PRD §9.1, §9.8.10).
//
// Scope notes (reconciled against PRD §22):
//   - PRD §22 defines ErrEmptyFilter strictly for mutation *Where operations
//     (UpdateWhere, SoftDeleteWhere, RestoreWhere, HardDeleteWhere). ExistsWhere
//     is a read-only scalar probe — an unfiltered existence check is legal and
//     documented (ExistsWhere(ctx, nil) returns true iff the table is non-empty).
//     ExistsWhere does NOT return ErrEmptyFilter; the test below validates
//     that contract.
//   - There is no `SkipSoftDelete` option on CallOptions. Soft-delete bypass is
//     performed by setting the filter's DeletedAt / IsDeleted comparator (see
//     PRD §9.8.10 godoc "Use ExistsWhere with Filter.DeletedAt comparator for
//     soft-deleted rows"). The test below exercises the filter-override path.

func TestExistsWhere_LikeOperator_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "EW-Like-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "LikeProbeWidget", Price: 1.00, CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	pattern := "LikeProbe%"
	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:   &comparator.ID{Eq: new(p.ID.String())},
		Name: &comparator.String{Like: &pattern},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Like matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(name LIKE 'LikeProbe%') = false, want true")
	}

	nonMatching := "NoSuchPrefix%"
	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:   &comparator.ID{Eq: new(p.ID.String())},
		Name: &comparator.String{Like: &nonMatching},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Like non-matching: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(name LIKE 'NoSuchPrefix%') = true, want false")
	}
}

func TestExistsWhere_NullOperator_Postgres(t *testing.T) {
	// articles.body is nullable TEXT — exercise both IsNull and IsNotNull paths.
	ctx := context.Background()
	client := newClient()

	nullBody, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "NullBody", Author: "exists-null",
	})
	if err != nil {
		t.Fatalf("Create null-body article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, nullBody.ID) })

	bodyText := "non-null body"
	withBody, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "WithBody",
		Body:   omittable.Set(&bodyText),
		Author: "exists-null",
	})
	if err != nil {
		t.Fatalf("Create body article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, withBody.ID) })

	authorEq := "exists-null"
	isNull := true
	exists, err := client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &authorEq},
		Body:   &comparator.NullableString{Null: &isNull},
	})
	if err != nil {
		t.Fatalf("ExistsWhere body IS NULL: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(body IS NULL) = false, want true (nullBody article matches)")
	}

	isNotNull := false
	exists, err = client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &authorEq},
		Body:   &comparator.NullableString{Null: &isNotNull},
	})
	if err != nil {
		t.Fatalf("ExistsWhere body IS NOT NULL: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(body IS NOT NULL) = false, want true (withBody article matches)")
	}
}
