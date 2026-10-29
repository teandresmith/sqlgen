package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// Exists / ExistsWhere e2e for MySQL (PRD §9.1, §9.8.10). Dialect-adapted
// mirror of the postgres exists suite.
//
// Scope notes (reconciled against PRD §22):
//   - ExistsWhere does NOT return ErrEmptyFilter — that sentinel is defined in
//     PRD §22 for mutation *Where operations only (UpdateWhere, SoftDeleteWhere,
//     RestoreWhere, HardDeleteWhere). ExistsWhere is a read-only scalar probe
//     and `ExistsWhere(ctx, nil)` is legal (returns true iff the table has a
//     non-soft-deleted row).
//   - There is no `SkipSoftDelete` CallOption. The project-equivalent bypass
//     is setting `Filter.DeletedAt` (or `Filter.IsDeleted` for boolean-strategy
//     tables) — see PRD §9.8.10.

func TestExistsWhere_LikeOperator_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "MyEW-Like-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "MyLikeProbeWidget", Price: 1.00,
		SKU: "MYLIKEPROBE-001", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	pattern := "MyLikeProbe%"
	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.Number[int64]{Eq: &p.ID},
		Title: &comparator.String{Like: &pattern},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Like matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(title LIKE 'MyLikeProbe%') = false, want true")
	}

	noMatch := "MyNoSuchPrefix%"
	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.Number[int64]{Eq: &p.ID},
		Title: &comparator.String{Like: &noMatch},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Like non-matching: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(title LIKE 'MyNoSuchPrefix%') = true, want false")
	}
}

func TestExistsWhere_NullOperator_MySQL(t *testing.T) {
	// articles.body is nullable TEXT — exercise both IsNull and IsNotNull paths.
	ctx := context.Background()
	client := newClient()

	nullBody, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "MyNullBody", Author: "my-exists-null",
	})
	if err != nil {
		t.Fatalf("Create null-body article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, nullBody.ID) })

	bodyText := "non-null body"
	withBody, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "MyWithBody",
		Body:   omittable.Set(&bodyText),
		Author: "my-exists-null",
	})
	if err != nil {
		t.Fatalf("Create body article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, withBody.ID) })

	authorEq := "my-exists-null"
	isNull := true
	exists, err := client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		Author: &comparator.String{Eq: &authorEq},
		Body:   &comparator.NullableString{Null: &isNull},
	})
	if err != nil {
		t.Fatalf("ExistsWhere body IS NULL: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(body IS NULL) = false, want true")
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
		t.Error("ExistsWhere(body IS NOT NULL) = false, want true")
	}
}
