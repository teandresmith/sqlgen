package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestDigitLeadingColumns_RoundTrip exercises every digit-leading column on
// the digit_leading_columns table through Create → Get → Update → CreateMany →
// HardDelete against a real SQLite database. The point of this test is NOT
// to verify SQL correctness in isolation (the property test in
// cmd/sqlgen/gen/safety_property_test.go already pins parseability for every
// digit-leading fixture). It's to confirm the PRD §8.5 "Digit-Leading
// Handling" rule ALSO works at runtime — the generated `Col`/`col` prefix
// produces compilable Go, the renamed locals flow through CreateMany's
// value-row assembly, and round-tripped values match what the caller
// supplied while the `db:` tag preserves the original SQL column name.
//
// Every non-PK column on digit_leading_columns is intentionally named with a
// leading digit (`2010_revenue`, `2024_quota`, `1st_place`, `3d_model_url`).
// The exposed struct fields are `Col2010Revenue`, `Col2024Quota`,
// `Col1stPlace`, `Col3dModelURL`; the generated CreateMany body uses the
// camel-form locals `col2010Revenue`, `col2024Quota`, etc.
func TestDigitLeadingColumns_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Create — caller supplies values for every digit-leading column. The
	// generated Create body builds a per-column local via the
	// prefixIfDigitLeading guard threaded through toCamelCase (e.g.,
	// `var col2010Revenue any = sql.NewDefaultExpr("0.0")`), so the
	// prefix branch is exercised end-to-end.
	row, err := client.DigitLeadingColumns().Create(ctx, &models.CreateDigitLeadingColumnInput{
		Col2010Revenue: omittable.Set(1234.56),
		Col2024Quota:   omittable.Set(int64(500)),
		Col1stPlace:    omittable.Set("gold"),
		Col3dModelURL:  omittable.Set("https://cdn.example.com/model.glb"),
	})
	if err != nil {
		t.Fatalf("Create digit_leading_columns: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("Create digit_leading_columns: expected non-zero ID")
	}

	// Get — verify every digit-leading column round-tripped its supplied
	// value. The exposed struct fields are `Col<N>...` (PascalCase) but the
	// `db:` and `json:` tags carry the original SQL/wire names — the
	// generated scanner reads from "2010_revenue" / "2024_quota" / etc.
	got, err := client.DigitLeadingColumns().Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get digit_leading_columns: %v", err)
	}
	if got.Col2010Revenue != 1234.56 {
		t.Errorf("Get(%d).Col2010Revenue = %v, want 1234.56", row.ID, got.Col2010Revenue)
	}
	if got.Col2024Quota != 500 {
		t.Errorf("Get(%d).Col2024Quota = %d, want 500", row.ID, got.Col2024Quota)
	}
	if got.Col1stPlace != "gold" {
		t.Errorf("Get(%d).Col1stPlace = %q, want %q", row.ID, got.Col1stPlace, "gold")
	}
	if got.Col3dModelURL != "https://cdn.example.com/model.glb" {
		t.Errorf("Get(%d).Col3dModelURL = %q, want CDN URL", row.ID, got.Col3dModelURL)
	}

	// Update — partial update touching two digit-leading columns. The
	// Update body uses a different codegen path (setClause assembly,
	// not per-column locals), but the round-trip pins that the renamed
	// fields exposed in UpdateDigitLeadingColumnInput continue to work.
	updated, err := client.DigitLeadingColumns().Update(ctx, row.ID, &models.UpdateDigitLeadingColumnInput{
		Col2010Revenue: omittable.Set(9999.99),
		Col1stPlace:    omittable.Set("platinum"),
	})
	if err != nil {
		t.Fatalf("Update digit_leading_columns: %v", err)
	}
	if updated.Col2010Revenue != 9999.99 {
		t.Errorf("Update Col2010Revenue = %v, want 9999.99", updated.Col2010Revenue)
	}
	if updated.Col1stPlace != "platinum" {
		t.Errorf("Update Col1stPlace = %q, want %q", updated.Col1stPlace, "platinum")
	}
	// Untouched columns retain their prior values.
	if updated.Col2024Quota != 500 {
		t.Errorf("Update preserved Col2024Quota = %d, want 500", updated.Col2024Quota)
	}
	if updated.Col3dModelURL != "https://cdn.example.com/model.glb" {
		t.Errorf("Update preserved Col3dModelURL = %q, want CDN URL", updated.Col3dModelURL)
	}

	// CreateMany — exercises the batch path. The bare-local emission site
	// in create.go.tmpl renders `var col2010Revenue any = ...` for each
	// digit-leading column; without the prefix guard the template would
	// have emitted `var 2010Revenue any = ...` and failed to compile.
	// Driving two rows confirms the renamed locals correctly drive the
	// per-batch valueRows assembly.
	many, err := client.DigitLeadingColumns().CreateMany(ctx, []*models.CreateDigitLeadingColumnInput{
		{Col2010Revenue: omittable.Set(100.0), Col2024Quota: omittable.Set(int64(1))},
		{Col2010Revenue: omittable.Set(200.0), Col2024Quota: omittable.Set(int64(2))},
	})
	if err != nil {
		t.Fatalf("CreateMany digit_leading_columns: %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("CreateMany returned %d rows, want 2", len(many))
	}
	if many[0].Col2010Revenue != 100.0 || many[1].Col2010Revenue != 200.0 {
		t.Errorf("CreateMany Col2010Revenue = [%v, %v], want [100.0, 200.0]",
			many[0].Col2010Revenue, many[1].Col2010Revenue)
	}
	// Columns left unset fall back to their SQL DEFAULT values.
	if many[0].Col1stPlace != "unranked" {
		t.Errorf("CreateMany[0].Col1stPlace = %q, want %q (SQL DEFAULT)", many[0].Col1stPlace, "unranked")
	}
	if many[0].Col3dModelURL != "" {
		t.Errorf("CreateMany[0].Col3dModelURL = %q, want %q (SQL DEFAULT)", many[0].Col3dModelURL, "")
	}

	// HardDelete — confirms the renamed locals do not leak into delete-path
	// codegen (delete uses PK only, no column-derived locals — verified by
	// spec, exercised here for completeness).
	if err := client.DigitLeadingColumns().HardDelete(ctx, row.ID); err != nil {
		t.Fatalf("HardDelete digit_leading_columns: %v", err)
	}
}

// TestDigitLeadingColumns_ZeroInput: every writable column of
// digit_leading_columns has a DEFAULT, so a zero input supplies no column and
// the INSERT must take SQLite's all-defaults form. `INSERT INTO t () VALUES ()`
// is a syntax error on SQLite; the builder emits `INSERT INTO t DEFAULT VALUES`,
// and drops the conflict clause on Upsert because SQLite accepts none after
// DEFAULT VALUES.
func TestDigitLeadingColumns_ZeroInput(t *testing.T) {
	ctx := context.Background()
	c := newClient().DigitLeadingColumns()

	want := func(t *testing.T, op string, got *models.DigitLeadingColumn) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s returned a nil row", op)
		}
		if got.Col2010Revenue != 0 || got.Col2024Quota != 0 || got.Col1stPlace != "unranked" || got.Col3dModelURL != "" {
			t.Errorf("%s row = %+v, want the column defaults {0 0 unranked \"\"}", op, *got)
		}
	}

	created, err := c.Create(ctx, &models.CreateDigitLeadingColumnInput{})
	if err != nil {
		t.Fatalf("Create(zero input): %v", err)
	}
	want(t, "Create", created)

	upserted, err := c.Upsert(ctx, &models.CreateDigitLeadingColumnInput{}, models.DigitLeadingColumnConflictPK)
	if err != nil {
		t.Fatalf("Upsert(zero input): %v", err)
	}
	want(t, "Upsert", upserted)
	if upserted.ID == created.ID {
		t.Errorf("Upsert(zero input) returned id %d, the row Create wrote; want a new row", upserted.ID)
	}

	many, err := c.CreateMany(ctx, []*models.CreateDigitLeadingColumnInput{{}, {}})
	if err != nil {
		t.Fatalf("CreateMany(zero inputs): %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("CreateMany(zero inputs) returned %d rows, want 2", len(many))
	}
	for _, row := range many {
		want(t, "CreateMany", row)
	}
}
