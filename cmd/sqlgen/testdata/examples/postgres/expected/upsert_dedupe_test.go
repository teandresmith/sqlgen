package models

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/sql"
)

// upsertConflictPositions and upsertDedupeKey are emitted as Go source strings
// from the generator (cmd/sqlgen/gen/context_shared.go, upsertDedupeHelpers) and
// rendered by shared_types.go.tmpl, so nothing inside the gen module runs them
// — the tests there assert the definitions, not the behavior. These supply the
// behavior half, in the generated package, against the emitted helper rather
// than a copy of it. Same arrangement as union_columns_test.go.
//
// What is load-bearing (PRD §9.2, §9.5): UpsertMany dedupes its inputs by
// conflict target before building a statement, because PostgreSQL alone rejects
// an ON CONFLICT ... DO UPDATE that would affect one row twice while MySQL and
// SQLite accept it and keep the last row. The key decides which rows collapse,
// so a key that is too coarse silently drops a row the caller asked for, and one
// that is too fine lets the PostgreSQL error through. The same key matches a
// written row against the row read back for it, so both halves must agree.

func TestUpsertConflictPositions(t *testing.T) {
	tests := []struct {
		name            string
		columns         []string
		conflictColumns []string
		want            []int
		wantOK          bool
	}{
		{
			name:            "single_column",
			columns:         []string{"id", "name", "price"},
			conflictColumns: []string{"name"},
			want:            []int{1},
			wantOK:          true,
		},
		{
			name:            "composite_keeps_target_order_not_column_order",
			columns:         []string{"a", "b", "c"},
			conflictColumns: []string{"c", "a"},
			want:            []int{2, 0},
			wantOK:          true,
		},
		{
			// An AUTO_INCREMENT key is absent from CreateInput entirely, so a
			// ConflictPK target on such a table names a column the statement
			// never supplies. Nothing can be deduped or read back on it.
			name:            "column_not_supplied",
			columns:         []string{"name", "payload"},
			conflictColumns: []string{"id"},
			want:            nil,
			wantOK:          false,
		},
		{
			name:            "empty_target",
			columns:         []string{"name"},
			conflictColumns: nil,
			want:            nil,
			wantOK:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := upsertConflictPositions(tt.columns, tt.conflictColumns)
			if ok != tt.wantOK {
				t.Fatalf("upsertConflictPositions ok = %v, want %v", ok, tt.wantOK)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("positions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Rows that must key the same, and rows that must not.
func TestUpsertDedupeKey_identity(t *testing.T) {
	ts := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		left  []any
		right []any
		same  bool
	}{
		{
			name:  "same_scalar_values",
			left:  []any{"abc", int64(7)},
			right: []any{"abc", int64(7)},
			same:  true,
		},
		{
			name:  "different_scalar_values",
			left:  []any{"abc"},
			right: []any{"abd"},
			same:  false,
		},
		{
			// %v on a *string prints an address, so an unconverted key would
			// make two pointers to the same text look like different rows —
			// and would never match a row read back from the database.
			name:  "pointer_keys_by_value_not_address",
			left:  []any{new("abc")},
			right: []any{new("abc")},
			same:  true,
		},
		{
			// int and int64 both bind as int64.
			name:  "integer_widths_normalize",
			left:  []any{7},
			right: []any{int64(7)},
			same:  true,
		},
		{
			// The monotonic reading a time.Now() carries is not stored by any
			// dialect, so it must not reach the key.
			name:  "timestamps_ignore_monotonic_and_location",
			left:  []any{ts},
			right: []any{ts.In(time.FixedZone("x", 3600))},
			same:  true,
		},
		{
			// Length-prefixed components: ("ab","c") and ("a","bc") are
			// different rows and must not collapse into one key.
			name:  "component_split_is_unambiguous",
			left:  []any{"ab", "c"},
			right: []any{"a", "bc"},
			same:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positions := make([]int, len(tt.left))
			for i := range positions {
				positions[i] = i
			}

			leftKey, ok := upsertDedupeKey(tt.left, positions)
			if !ok {
				t.Fatalf("upsertDedupeKey(left) reported no definite key")
			}
			rightKey, ok := upsertDedupeKey(tt.right, positions)
			if !ok {
				t.Fatalf("upsertDedupeKey(right) reported no definite key")
			}

			if (leftKey == rightKey) != tt.same {
				t.Errorf("keys equal = %v, want %v (left=%q right=%q)", leftKey == rightKey, tt.same, leftKey, rightKey)
			}
		})
	}
}

// A row with no definite conflict value is reported, not keyed. Such a row
// cannot be shown to collide in-statement, so UpsertMany keeps it rather than
// deduping it away — and where the key is also needed to name the written row,
// the operation refuses instead of returning a wrong primary key.
func TestUpsertDedupeKey_indefiniteValues(t *testing.T) {
	tests := []struct {
		name string
		row  []any
	}{
		{
			// The column's value is whatever the database's DEFAULT evaluates
			// to, which is not known until the statement runs.
			name: "default_sentinel",
			row:  []any{sql.Default},
		},
		{
			// SQLite's form of the same thing: the default expression is
			// inlined rather than spelled DEFAULT.
			name: "default_expr_sentinel",
			row:  []any{sql.NewDefaultExpr("CURRENT_TIMESTAMP")},
		},
		{
			// NULL never matches under a unique index on PostgreSQL, MySQL or
			// SQLite, so two NULL-keyed rows are two rows.
			name: "nil_any",
			row:  []any{nil},
		},
		{
			name: "typed_nil_pointer",
			row:  []any{(*string)(nil)},
		},
		{
			// One indefinite component is enough — the row's identity is the
			// whole target.
			name: "definite_then_indefinite",
			row:  []any{"abc", sql.Default},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positions := make([]int, len(tt.row))
			for i := range positions {
				positions[i] = i
			}

			key, ok := upsertDedupeKey(tt.row, positions)
			if ok {
				t.Errorf("upsertDedupeKey reported a definite key %q, want none", key)
			}
			if key != "" {
				t.Errorf("upsertDedupeKey key = %q, want empty on the indefinite path", key)
			}
		})
	}
}
