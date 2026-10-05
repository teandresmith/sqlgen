package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// TestReservedWordColumns_RoundTrip exercises every reserved-word column on
// the reserved_word_columns table through Create → Get → Update → Get → Delete
// against a real SQLite database. The point of this test is NOT to verify SQL
// correctness in isolation (the property test in
// cmd/sqlgen/gen/safety_property_test.go already pins parseability for every
// reserved word). It's to confirm the safeGoIdent escape ALSO works at
// runtime — the generated code compiles, the renamed locals correctly flow
// through CreateMany's value-row assembly, and round-tripped values match
// what the caller supplied.
//
// Every non-PK column on reserved_word_columns is intentionally named after a
// Go keyword (`type`, `interface`, `func`, `map`), a predeclared identifier
// (`new`, `make`, `len`, `error`), or a generator-reserved local (`ctx`,
// `err`). See PRD §8.5 (Reserved Word Handling) for the design.
func TestReservedWordColumns_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Create — caller supplies values for every reserved-word column. The
	// generated Create body builds a per-column local via the safeGoIdent
	// pipe (e.g., `var typeVal any = sql.NewDefaultExpr("'kind'")`), so the
	// escape branch is exercised end-to-end.
	row, err := client.ReservedWordColumns().Create(ctx, &models.CreateReservedWordColumnInput{
		Type:      omittable.Set("trade"),
		Interface: omittable.Set("rpc"),
		Func:      omittable.Set("compute"),
		Map:       omittable.Set("index"),
		New:       omittable.Set("seed"),
		Make:      omittable.Set("forge"),
		Len:       omittable.Set(int64(42)),
		Error:     omittable.Set("none"),
		Ctx:       omittable.Set("session"),
		Err:       omittable.Set("ok"),
	})
	if err != nil {
		t.Fatalf("Create reserved_word_columns: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("Create reserved_word_columns: expected non-zero ID")
	}

	// Get — verify every reserved-word column round-tripped its supplied
	// value. The exposed struct fields are PascalCase (Type, Interface, ...)
	// which never collide with Go keywords; the runtime risk was in the
	// generated *Create* body, which this assertion indirectly pins.
	got, err := client.ReservedWordColumns().Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get reserved_word_columns: %v", err)
	}
	cases := []struct {
		field, got, want string
	}{
		{"Type", got.Type, "trade"},
		{"Interface", got.Interface, "rpc"},
		{"Func", got.Func, "compute"},
		{"Map", got.Map, "index"},
		{"New", got.New, "seed"},
		{"Make", got.Make, "forge"},
		{"Error", got.Error, "none"},
		{"Ctx", got.Ctx, "session"},
		{"Err", got.Err, "ok"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Get(%d).%s = %q, want %q", row.ID, c.field, c.got, c.want)
		}
	}
	if got.Len != 42 {
		t.Errorf("Get(%d).Len = %d, want 42", row.ID, got.Len)
	}

	// Update — partial update touching three of the reserved-word columns.
	// The Update body uses a different codegen path (setClause assembly,
	// not per-column locals), but the round-trip pins that the renamed
	// fields exposed in UpdateReservedWordColumnInput continue to work.
	updated, err := client.ReservedWordColumns().Update(ctx, row.ID, &models.UpdateReservedWordColumnInput{
		Type:  omittable.Set("equity"),
		Error: omittable.Set("retry"),
		Len:   omittable.Set(int64(99)),
	})
	if err != nil {
		t.Fatalf("Update reserved_word_columns: %v", err)
	}
	if updated.Type != "equity" {
		t.Errorf("Update Type = %q, want %q", updated.Type, "equity")
	}
	if updated.Error != "retry" {
		t.Errorf("Update Error = %q, want %q", updated.Error, "retry")
	}
	if updated.Len != 99 {
		t.Errorf("Update Len = %d, want 99", updated.Len)
	}
	// Untouched columns retain their prior values.
	if updated.Interface != "rpc" {
		t.Errorf("Update preserved Interface = %q, want %q", updated.Interface, "rpc")
	}
	if updated.Ctx != "session" {
		t.Errorf("Update preserved Ctx = %q, want %q", updated.Ctx, "session")
	}

	// CreateMany — exercises the batch path that originally surfaced the
	// `var type any` bug in production. Two rows with default values for
	// every reserved-word column drive the per-batch valueRows assembly.
	many, err := client.ReservedWordColumns().CreateMany(ctx, []*models.CreateReservedWordColumnInput{
		{Type: omittable.Set("batch-a")},
		{Type: omittable.Set("batch-b")},
	})
	if err != nil {
		t.Fatalf("CreateMany reserved_word_columns: %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("CreateMany returned %d rows, want 2", len(many))
	}
	if many[0].Type != "batch-a" || many[1].Type != "batch-b" {
		t.Errorf("CreateMany Types = [%q, %q], want [batch-a, batch-b]", many[0].Type, many[1].Type)
	}
	// Other reserved-word columns fall back to their DB defaults.
	if many[0].Interface != "iface" {
		t.Errorf("CreateMany[0].Interface = %q, want %q (DB default)", many[0].Interface, "iface")
	}

	// HardDelete — the final round-trip step confirms the renamed locals
	// do not leak into delete-path codegen (delete uses PK only, no
	// column-derived locals — verified by spec, exercised here for
	// completeness).
	if err := client.ReservedWordColumns().HardDelete(ctx, row.ID); err != nil {
		t.Fatalf("HardDelete reserved_word_columns: %v", err)
	}
}
