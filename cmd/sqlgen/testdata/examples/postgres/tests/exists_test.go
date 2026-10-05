package tests

import (
	"context"
	"testing"
	"uuid"

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

func TestExists_PresentAndAbsent_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ExistsCat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	product, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name:       "ExistsProduct",
		Price:      10.00,
		Quantity:   omittable.Set[int32](1),
		CategoryID: cat.ID,
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

	exists, err = client.Products().Exists(ctx, (uuid.UUID{}))
	if err != nil {
		t.Fatalf("Exists absent: %v", err)
	}
	if exists {
		t.Error("Exists(absent PK) = true, want false")
	}
}

func TestExistsWhere_InOperator_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "EW-In-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p1, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "EW-In-A", Price: 5.00, CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p1.ID) })

	p2, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "EW-In-B", Price: 5.00, CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p2.ID) })

	// In with a matching set → true
	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID: &comparator.ID{In: []string{p1.ID.String(), p2.ID.String()}},
	})
	if err != nil {
		t.Fatalf("ExistsWhere In matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(In=[p1,p2]) = false, want true")
	}

	// In with an all-absent set → false
	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID: &comparator.ID{In: idStrings([]uuid.UUID{{}})},
	})
	if err != nil {
		t.Fatalf("ExistsWhere In absent: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(In=[absent]) = true, want false")
	}
}

func TestExistsWhere_GteOperator_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "EW-Gte-Cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })

	p, err := client.Products().Create(ctx, &models.CreateProductInput{
		Name: "EW-Gte-Product", Price: 99.99, CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = client.Products().HardDelete(ctx, p.ID) })

	// Scope the probe to this product's ID so sibling test rows don't pollute.
	threshold := 50.00
	exists, err := client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.ID{Eq: new(p.ID.String())},
		Price: &comparator.Number[float64]{Gte: &threshold},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Gte matching: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(id=p, price>=50) = false, want true")
	}

	overThreshold := 999.99
	exists, err = client.Products().ExistsWhere(ctx, &models.ProductFilter{
		ID:    &comparator.ID{Eq: new(p.ID.String())},
		Price: &comparator.Number[float64]{Gte: &overThreshold},
	})
	if err != nil {
		t.Fatalf("ExistsWhere Gte non-matching: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(id=p, price>=999.99) = true, want false")
	}
}

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

func TestExistsWhere_NilFilter_Postgres(t *testing.T) {
	// ExistsWhere(ctx, nil) is a legal unfiltered probe per PRD §22 — it returns
	// true iff the table has at least one non-soft-deleted row. It does NOT
	// return ErrEmptyFilter (that sentinel is mutation-only).
	ctx := context.Background()
	client := newClient()

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "EW-Nil-Cat"})
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

func TestExists_SoftDelete_ExcludedByDefault_Postgres(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "SoftDeleteExists", Author: "exists-sd",
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

	// Default Exists excludes soft-deleted rows (§9.8.10).
	exists, err = client.Articles().Exists(ctx, article.ID)
	if err != nil {
		t.Fatalf("Exists after soft-delete: %v", err)
	}
	if exists {
		t.Error("Exists(soft-deleted) = true, want false (default scoping excludes)")
	}
}

func TestExistsWhere_SoftDelete_FilterOverride_Postgres(t *testing.T) {
	// Setting Filter.DeletedAt overrides the default soft-delete scoping — this
	// is the project-equivalent of the "SkipSoftDelete" mechanism; see §9.8.10
	// (godoc: "Use ExistsWhere with Filter.DeletedAt comparator for
	// soft-deleted rows").
	ctx := context.Background()
	client := newClient()

	article, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title: "SDFilterOverride", Author: "ew-sd-override",
	})
	if err != nil {
		t.Fatalf("Create article: %v", err)
	}
	t.Cleanup(func() { _ = client.Articles().HardDelete(ctx, article.ID) })

	if _, err := client.Articles().SoftDelete(ctx, article.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	// Without override: ExistsWhere honors the default IS NULL filter and
	// returns false for the soft-deleted row.
	exists, err := client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		ID: &comparator.ID{Eq: new(article.ID.String())},
	})
	if err != nil {
		t.Fatalf("ExistsWhere default scoping: %v", err)
	}
	if exists {
		t.Error("ExistsWhere(id=soft-deleted) default = true, want false")
	}

	// With DeletedAt comparator set to IS NOT NULL, the default injection is
	// suppressed and the soft-deleted row becomes visible.
	isNotNull := false
	exists, err = client.Articles().ExistsWhere(ctx, &models.ArticleFilter{
		ID:        &comparator.ID{Eq: new(article.ID.String())},
		DeletedAt: &comparator.NullableTime{Null: &isNotNull},
	})
	if err != nil {
		t.Fatalf("ExistsWhere DeletedAt override: %v", err)
	}
	if !exists {
		t.Error("ExistsWhere(id=soft-deleted, DeletedAt=NotNull) = false, want true")
	}
}
