package models

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// unionColumns is emitted as a Go source string from the generator
// (cmd/sqlgen/gen/context_shared.go, sharedHelperDefinitions) and rendered by
// shared_types.go.tmpl, so nothing inside the gen module type-checks or runs
// it — the tests there assert the definition, not the behavior. These tests
// supply the behavior half, and they live in the generated package so they
// exercise the emitted helper rather than a copy of it. That follows the
// mysql_version_test.go precedent
// (cmd/sqlgen/testdata/examples/mysql/models) and needs no drift guard: the
// golden trees already pin the helper's text, so a body change lands here on
// the next `make update-golden-e2e` and these tests run against the new body.
//
// Why the ordering is load-bearing (PRD §9.6): sql.BuildSelect emits
// the SELECT list through sortedCopy, while scan<T>(rows, columns) fills
// targets[idx] positionally by walking `columns` in the order it was given.
// The two line up only while the slice reaching the scanner is sorted.
// unionColumns holds that up by re-sorting whenever it adds a column, and by
// handing the argument back untouched when it does not — safe only because
// every call site passes an already-sorted slice (FieldOptions.Columns ends in
// slices.Sort, <T>AllColumns is emitted sorted). A regression that appended
// without re-sorting would not fail loudly; it would scan values into the
// wrong fields.
func TestUnionColumns(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		required []string
		want     []string
	}{
		{
			name:     "no_required_returns_input",
			columns:  []string{"id", "name"},
			required: nil,
			want:     []string{"id", "name"},
		},
		{
			name:     "empty_required_returns_input",
			columns:  []string{"id"},
			required: []string{},
			want:     []string{"id"},
		},
		{
			name:     "required_already_selected",
			columns:  []string{"created_at", "id", "name"},
			required: []string{"id"},
			want:     []string{"created_at", "id", "name"},
		},
		{
			name:     "single_missing_column_resorts",
			columns:  []string{"name", "price"},
			required: []string{"id"},
			want:     []string{"id", "name", "price"},
		},
		{
			// A composite cursor key hands two columns to one call (PRD §12).
			name:     "composite_cursor_key_adds_both",
			columns:  []string{"name"},
			required: []string{"created_at", "id"},
			want:     []string{"created_at", "id", "name"},
		},
		{
			name:     "duplicate_required_added_once",
			columns:  []string{"name"},
			required: []string{"id", "id"},
			want:     []string{"id", "name"},
		},
		{
			name:     "mixed_present_and_repeated_missing",
			columns:  []string{"id", "name"},
			required: []string{"id", "created_at", "created_at", "name"},
			want:     []string{"created_at", "id", "name"},
		},
		{
			name:     "nil_columns_yields_required",
			columns:  nil,
			required: []string{"id"},
			want:     []string{"id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := unionColumns(tc.columns, tc.required...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unionColumns(%v, %v) mismatch (-want +got):\n%s", tc.columns, tc.required, diff)
			}
		})
	}
}

// TestUnionColumns_doesNotMutateInput pins that the union never writes through
// the argument's backing array. The slice a call site hands in can be shared —
// <T>AllColumns is a package-level var, and table/get.go.tmpl assigns it to
// the same variable the union writes back to — so an in-place append would
// corrupt every later read in the process rather than just the one call.
func TestUnionColumns_doesNotMutateInput(t *testing.T) {
	// Spare capacity is the trap: `append(columns, ...)` writes into the
	// caller's array instead of allocating, and only a slice with room to grow
	// can observe the difference.
	backing := make([]string, 2, 6)
	copy(backing, []string{"id", "name"})
	columns := backing[:2]

	got := unionColumns(columns, "created_at")

	if diff := cmp.Diff([]string{"created_at", "id", "name"}, got); diff != "" {
		t.Errorf("unionColumns() mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"id", "name"}, columns); diff != "" {
		t.Errorf("argument slice was mutated (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"id", "name", "", "", "", ""}, backing[:cap(backing)]); diff != "" {
		t.Errorf("argument backing array was written through (-want +got):\n%s", diff)
	}
}

// TestUnionColumns_fastPathAliasesInput records that the "nothing missing"
// path returns the argument itself rather than a copy. PRD §9.6 spells the
// body out verbatim, so this is the specified shape rather than an accident,
// and it is the reason the sorted-projection invariant above is a joint
// contract with the call sites: a caller that sorted or appended to a returned
// slice in place would reach back into whatever it passed in.
func TestUnionColumns_fastPathAliasesInput(t *testing.T) {
	columns := []string{"id", "name"}

	got := unionColumns(columns, "id")

	if len(got) != len(columns) || &got[0] != &columns[0] {
		t.Errorf("unionColumns() with nothing missing returned a copy (%v); the argument slice itself is the contract", got)
	}
}
