package tests

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/types"
)

// TestJSONBArrayDefaultRoundTrip pins the production bug shape that
// motivated PRD §7.6. A `jsonb` column with a JSON-array DEFAULT
// (`'[{"year":1,"value":0.014}]'::jsonb`) produces array-shaped bytes on
// every fresh insert. The prior `types.JSONMap` (`map[string]any`) could
// not unmarshal those bytes — the polymorphic `types.JSON` (`json.RawMessage`)
// round-trips them unchanged and lets callers decode to a typed slice via
// `Decode`, or probe with `IsArray` and read with `AsSlice`.
//
// This test bypasses the example's generated models and the user-facing
// schema. It creates a single-purpose table in the shared test Postgres,
// inserts a row that exercises the DEFAULT path, and reads the column
// back through the same types.JSON Scanner/Valuer the generated models
// use.
func TestJSONBArrayDefaultRoundTrip(t *testing.T) {
	ctx := context.Background()

	const setupSQL = `
CREATE TABLE IF NOT EXISTS jsonb_default_assets (
    id           SERIAL PRIMARY KEY,
    property_tax_rate JSONB NOT NULL DEFAULT '[{"year":1,"value":0.014},{"year":2,"value":0.025}]'::jsonb
);
TRUNCATE TABLE jsonb_default_assets RESTART IDENTITY;
INSERT INTO jsonb_default_assets DEFAULT VALUES;
`
	if _, err := testPool.Exec(ctx, setupSQL); err != nil {
		t.Fatalf("setup table + insert: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `DROP TABLE IF EXISTS jsonb_default_assets`); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	var raw types.JSON
	if err := testPool.QueryRow(
		ctx,
		`SELECT property_tax_rate FROM jsonb_default_assets WHERE id = 1`,
	).Scan(&raw); err != nil {
		t.Fatalf("scan jsonb DEFAULT array into types.JSON: %v", err)
	}

	if !raw.IsArray() {
		t.Fatalf("expected IsArray=true, got %s", raw.String())
	}

	type yearValue struct {
		Year  int     `json:"year"`
		Value float64 `json:"value"`
	}
	var rows []yearValue
	if err := raw.Decode(&rows); err != nil {
		t.Fatalf("Decode []yearValue: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Year != 1 || rows[0].Value != 0.014 {
		t.Errorf("rows[0] = %+v, want {Year:1 Value:0.014}", rows[0])
	}
	if rows[1].Year != 2 || rows[1].Value != 0.025 {
		t.Errorf("rows[1] = %+v, want {Year:2 Value:0.025}", rows[1])
	}

	slice, err := raw.AsSlice()
	if err != nil {
		t.Fatalf("AsSlice: %v", err)
	}
	if len(slice) != 2 {
		t.Fatalf("AsSlice returned %d items, want 2", len(slice))
	}
}
