package tests

// Single-row Update honours the empty-FieldOptions skip (PRD §9.2, §9.6).
//
// `Update` was once the only entity-returning mutation that returned
// `c.Get(...)` unconditionally: `Create`, `CreateMany`, `Upsert`, `UpdateMany`,
// `UpdateWhere` and every delete variant already guarded their trailing read
// with `FieldOptions != nil && !HasSelectedColumns()`. A caller passing an
// all-false `&ProductFieldOptions{}` therefore paid for a SELECT whose result
// it had explicitly said it did not want.
//
// Coverage:
//   - Normal path: an Update with fields set and an all-false FieldOptions
//     issues exactly one statement (the UPDATE) and returns nil, nil. The write
//     still lands — asserted by an independent read on an unwrapped client, so
//     the verification read is not counted against the escape.
//   - Empty-update path: an Update with nothing set AND an all-false
//     FieldOptions issues zero statements and returns nil, nil. §9.2 names this
//     path explicitly ("on the empty-update path as well as the normal one").
//   - Regression, nil FieldOptions: the full entity still comes back and the
//     re-fetch still happens (UPDATE + SELECT).
//   - Regression, populated FieldOptions: a selection that asks for columns
//     still re-fetches and returns those columns.

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// seedSkipRefetchProduct creates a category + product on an unwrapped client and
// returns the product along with a client whose querier records every statement.
func seedSkipRefetchProduct(ctx context.Context, t *testing.T, name string) (*models.Product, *models.Client, *capturingQuerier) {
	t.Helper()

	plain := models.New(dbpgx.New(testPool))

	cat, err := plain.Categories().Create(ctx, &models.CreateCategoryInput{Name: name + "-cat"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = plain.Categories().HardDelete(ctx, cat.ID) })

	seed, err := plain.Products().Create(ctx, &models.CreateProductInput{
		Name:       name,
		Price:      10.00,
		CategoryID: cat.ID,
	})
	if err != nil {
		t.Fatalf("Create product: %v", err)
	}
	t.Cleanup(func() { _ = plain.Products().HardDelete(ctx, seed.ID) })

	cap := newCapturingQuerier(dbpgx.New(testPool))
	return seed, models.New(cap), cap
}

// TestUpdate_EmptyFieldOptions_SkipsRefetch_Postgres pins the query count: one
// statement, not two. This is the assertion that fails without the template's
// skip guard — the entity return value would be non-nil and a SELECT would follow
// the UPDATE.
func TestUpdate_EmptyFieldOptions_SkipsRefetch_Postgres(t *testing.T) {
	ctx := context.Background()
	seed, client, cap := seedSkipRefetchProduct(ctx, t, "SkipRefetch-27.3")

	cap.reset()
	got, err := client.Products().Update(ctx, seed.ID, &models.UpdateProductInput{
		Name: omittable.Set("SkipRefetch-27.3-updated"),
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.FieldOptions = &models.ProductFieldOptions{} // all false — caller wants no result
	})
	if err != nil {
		t.Fatalf("Update(empty FieldOptions): %v", err)
	}
	if got != nil {
		t.Errorf("Update(empty FieldOptions) = %+v, want nil (skip-refetch)", got)
	}

	sqls := cap.snapshot()
	if len(sqls) != 1 {
		t.Errorf("Update(empty FieldOptions) issued %d statements, want 1 (the UPDATE): %v", len(sqls), sqls)
	}
	for _, sqlStr := range sqls {
		if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqlStr)), "UPDATE ") {
			t.Errorf("Update(empty FieldOptions) issued a non-UPDATE statement: %q", sqlStr)
		}
	}

	// The write still landed — read it back on an unwrapped client so this
	// verification read is not counted against the escape above.
	plain := models.New(dbpgx.New(testPool))
	after, err := plain.Products().Get(ctx, seed.ID)
	if err != nil {
		t.Fatalf("Get after skip-refetch Update: %v", err)
	}
	if after.Name != "SkipRefetch-27.3-updated" {
		t.Errorf("post-Update name = %q, want %q (the UPDATE must still execute)", after.Name, "SkipRefetch-27.3-updated")
	}
}

// TestUpdate_EmptyFieldOptions_EmptyUpdate_NoStatements_Postgres covers the
// second guarded site: the §9.5 empty-update short-circuit. With nothing set
// there is no UPDATE to issue, and with an all-false FieldOptions there is no
// entity to read — so the whole call touches the database zero times.
func TestUpdate_EmptyFieldOptions_EmptyUpdate_NoStatements_Postgres(t *testing.T) {
	ctx := context.Background()
	seed, client, cap := seedSkipRefetchProduct(ctx, t, "SkipRefetchEmpty-27.3")

	cap.reset()
	got, err := client.Products().Update(ctx, seed.ID, &models.UpdateProductInput{},
		func(o *models.CallOptions[models.ProductFieldOptions]) {
			o.FieldOptions = &models.ProductFieldOptions{}
		})
	if err != nil {
		t.Fatalf("Update(empty input, empty FieldOptions): %v", err)
	}
	if got != nil {
		t.Errorf("Update(empty input, empty FieldOptions) = %+v, want nil (skip-refetch)", got)
	}
	if sqls := cap.snapshot(); len(sqls) != 0 {
		t.Errorf("Update(empty input, empty FieldOptions) issued %d statements, want 0: %v", len(sqls), sqls)
	}
}

// TestUpdate_NilFieldOptions_StillReturnsEntity_Postgres is the regression pin:
// the escape must be inert unless FieldOptions is non-nil and selects nothing.
func TestUpdate_NilFieldOptions_StillReturnsEntity_Postgres(t *testing.T) {
	ctx := context.Background()
	seed, client, cap := seedSkipRefetchProduct(ctx, t, "NilFieldOptions-27.3")

	cap.reset()
	got, err := client.Products().Update(ctx, seed.ID, &models.UpdateProductInput{
		Name: omittable.Set("NilFieldOptions-27.3-updated"),
	})
	if err != nil {
		t.Fatalf("Update(nil FieldOptions): %v", err)
	}
	if got == nil {
		t.Fatalf("Update(nil FieldOptions) = nil, want the full entity")
	}
	if got.ID != seed.ID {
		t.Errorf("Update(nil FieldOptions) ID = %s, want %s", got.ID, seed.ID)
	}
	if got.Name != "NilFieldOptions-27.3-updated" {
		t.Errorf("Update(nil FieldOptions) Name = %q, want %q", got.Name, "NilFieldOptions-27.3-updated")
	}
	// Every non-selected column is populated too — this is the full entity,
	// not a projection.
	if got.Price != seed.Price || got.CategoryID != seed.CategoryID || got.Quantity != seed.Quantity {
		t.Errorf("Update(nil FieldOptions) returned a partial entity: got=%+v want price=%v category=%v quantity=%v",
			got, seed.Price, seed.CategoryID, seed.Quantity)
	}

	// The re-fetch is still issued: UPDATE followed by SELECT.
	sqls := cap.snapshot()
	if len(sqls) != 2 {
		t.Fatalf("Update(nil FieldOptions) issued %d statements, want 2 (UPDATE + SELECT): %v", len(sqls), sqls)
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[0])), "UPDATE ") {
		t.Errorf("first statement = %q, want an UPDATE", sqls[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqls[1])), "SELECT ") {
		t.Errorf("second statement = %q, want a SELECT", sqls[1])
	}
}

// TestUpdate_PopulatedFieldOptions_StillRefetches_Postgres pins the other inert
// case: a selection that asks for columns is not an empty selection.
func TestUpdate_PopulatedFieldOptions_StillRefetches_Postgres(t *testing.T) {
	ctx := context.Background()
	seed, client, cap := seedSkipRefetchProduct(ctx, t, "PopulatedFieldOptions-27.3")

	cap.reset()
	got, err := client.Products().Update(ctx, seed.ID, &models.UpdateProductInput{
		Name: omittable.Set("PopulatedFieldOptions-27.3-updated"),
	}, func(o *models.CallOptions[models.ProductFieldOptions]) {
		o.FieldOptions = &models.ProductFieldOptions{ID: true, Name: true}
	})
	if err != nil {
		t.Fatalf("Update(populated FieldOptions): %v", err)
	}
	if got == nil {
		t.Fatalf("Update(populated FieldOptions) = nil, want the projected entity")
	}
	if got.ID != seed.ID || got.Name != "PopulatedFieldOptions-27.3-updated" {
		t.Errorf("Update(populated FieldOptions) = {ID:%s Name:%q}, want {ID:%s Name:%q}",
			got.ID, got.Name, seed.ID, "PopulatedFieldOptions-27.3-updated")
	}
	if sqls := cap.snapshot(); len(sqls) != 2 {
		t.Errorf("Update(populated FieldOptions) issued %d statements, want 2 (UPDATE + SELECT): %v", len(sqls), sqls)
	}
}
