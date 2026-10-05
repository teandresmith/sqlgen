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

func TestExists_PresentAndAbsent_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "MyExistsCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID,
		Title:      "MyExistsProduct",
		Price:      10.00,
		SKU:        "MYEXISTS-001",
		Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, product.ID) })

	exists, err := client.Products().Exists(ctx, product.ID)
	if err != nil {
		t.Fatalf("Exists present: %v", err)
	}
	if !exists {
		t.Error("Exists(present PK) = false, want true")
	}

	exists, err = client.Products().Exists(ctx, int64(999999999))
	if err != nil {
		t.Fatalf("Exists absent: %v", err)
	}
	if exists {
		t.Error("Exists(absent PK) = true, want false")
	}
}

func TestExistsWhere_InOperator_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "MyEW-In-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "MyEW-In-A", Price: 5.00, SKU: "MYEW-IN-A", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "MyEW-In-B", Price: 5.00, SKU: "MYEW-IN-B", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID: &comparator.Number[int64]{In: []int64{p1.ID, p2.ID}},
	})
	if err != nil {
		t.Fatalf("ExistsWhere In matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(In=[p1,p2]) = false, want true")
	}

	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID: &comparator.Number[int64]{In: []int64{999999999}},
	})
	if err != nil {
		t.Fatalf("ExistsWhere In absent: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(In=[absent]) = true, want false")
	}
}

func TestExistsWhere_GteOperator_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "MyEW-Gte-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		CategoryID: cat.ID, Title: "MyEW-Gte-Product", Price: 99.99,
		SKU: "MYEW-GTE-001", Attributes: types.JSON([]byte(`{}`)),
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	threshold := 50.00
	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.Number[int64]{Eq: &p.ID},
		Price: &comparator.Number[float64]{Gte: &threshold},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Gte matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(id=p, price>=50) = false, want true")
	}

	over := 999.99
	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.Number[int64]{Eq: &p.ID},
		Price: &comparator.Number[float64]{Gte: &over},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Gte non-matching: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(id=p, price>=999.99) = true, want false")
	}
}

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

func TestExistsWhere_NilFilter_MySQL(t *testing.T) {
	// ExistsWhere(ctx, nil) is a legal unfiltered probe per PRD §22. Returns
	// true iff the table has at least one non-soft-deleted row. Does NOT return
	// ErrEmptyFilter (that sentinel is mutation-only).
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "MyEW-Nil-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	exists, err := client.Categories().ExistsWhere(ctx, nil)
	if err != nil {
		t.Fatalf("ExistsWhere(nil) with data: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(nil) with data = false, want true")
	}
}

func TestExists_SoftDelete_ExcludedByDefault_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "MySoftDeleteExists", Author: "my-exists-sd",
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, article.ID) })

	exists, err := client.Articles().Exists(ctx, article.ID)
	if err != nil {
		t.Fatalf("Exists before soft-delete: %v", err)
	}
	if !exists {
		t.Fatal("Exists before soft-delete = false, want true")
	}

	if _, err := client.Articles().SoftDelete(ctx, article.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	exists, err = client.Articles().Exists(ctx, article.ID)
	if err != nil {
		t.Fatalf("Exists after soft-delete: %v", err)
	}
	if exists {
		t.Error("Exists(soft-deleted) = true, want false (default scoping excludes)")
	}
}

func TestExistsWhere_SoftDelete_FilterOverride_MySQL(t *testing.T) {
	// Setting Filter.DeletedAt overrides the default soft-delete scoping —
	// project-equivalent of the "SkipSoftDelete" mechanism per §9.8.10.
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "MySDFilterOverride", Author: "my-ew-sd-override",
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, article.ID) })

	if _, err := client.Articles().SoftDelete(ctx, article.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	exists, err := client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		ID: &comparator.Number[int64]{Eq: &article.ID},
	})
	if err != nil {
		t.Fatalf("ExistsWhere default scoping: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(id=soft-deleted) default = true, want false")
	}

	isNotNull := false
	exists, err = client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		ID:        &comparator.Number[int64]{Eq: &article.ID},
		DeletedAt: &comparator.NullableTime{Null: &isNotNull},
	})
	if err != nil {
		t.Fatalf("ExistsWhere DeletedAt override: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(id=soft-deleted, DeletedAt=NotNull) = false, want true")
	}
}
