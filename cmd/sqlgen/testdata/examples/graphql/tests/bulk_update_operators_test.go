package tests

import (
	"testing"
)

// The bulk `updateEvents(filter, input)` mutation beyond the single-operator
// case: set fields and operators in one call, the pairing rules the single-row
// mutation enforces, the mutation result, and a filter that matches nothing.

// upfBulkRow is the slice of an updateEvents result these tests select.
type upfBulkRow struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	RetryCount *int   `json:"retryCount"`
	IOCount    *int   `json:"ioCount"`
}

// upfUpdateEvents runs updateEvents and returns its rows, failing the test on
// any GraphQL error.
func upfUpdateEvents(t *testing.T, filter, input string) []upfBulkRow {
	t.Helper()
	var out struct {
		UpdateEvents []upfBulkRow `json:"updateEvents"`
	}
	gqlExecData(t, `
		mutation {
			updateEvents(filter: `+filter+`, input: `+input+`) { id action retryCount ioCount }
		}
	`, nil, &out)
	return out.UpdateEvents
}

// TestBulkUpdateOperators_SetFieldAndIncrementApplyToTheSameRows pins that the
// operators reach the rows the filter matched, even when a set field in the
// same call moves those rows out of the filter: the filter selects on
// `action`, the set field renames it, and the increment must still land.
func TestBulkUpdateOperators_SetFieldAndIncrementApplyToTheSameRows(t *testing.T) {
	truncateAll(t)
	target := uopSeedEvent(t, "upf-rename-me")
	other := uopSeedEvent(t, "upf-untouched")

	rows := upfUpdateEvents(t, `{ action: { eq: "upf-rename-me" } }`, `{ action: "upf-renamed", retryCount_inc: 2, ioCount_dec: 1 }`)

	if len(rows) != 1 || rows[0].ID != target {
		t.Fatalf("updateEvents returned %+v, want exactly the target row %s", rows, target)
	}
	got := uopReadEvent(t, target)
	if !got.retryCount.Valid || got.retryCount.Int64 != 5 {
		t.Errorf("target retry_count = %+v, want 5 (was 3, +2)", got.retryCount)
	}
	if !got.ioCount.Valid || got.ioCount.Int64 != 4 {
		t.Errorf("target io_count = %+v, want 4 (was 5, -1)", got.ioCount)
	}
	if untouched := uopReadEvent(t, other); untouched.retryCount.Int64 != 3 || untouched.ioCount.Int64 != 5 {
		t.Errorf("row outside the filter changed: retry_count = %+v, io_count = %+v, want 3 and 5", untouched.retryCount, untouched.ioCount)
	}
}

// TestBulkUpdateOperators_ResultReflectsEveryOperator pins that the mutation
// answers with the rows as they stand after the operators, for every matched
// row, rather than with the state UpdateWhere alone produced.
func TestBulkUpdateOperators_ResultReflectsEveryOperator(t *testing.T) {
	truncateAll(t)
	uopSeedEvent(t, "upf-many")
	uopSeedEvent(t, "upf-many")

	rows := upfUpdateEvents(t, `{ action: { eq: "upf-many" } }`, `{ retryCount_inc: 4 }`)

	if len(rows) != 2 {
		t.Fatalf("updateEvents returned %d rows, want 2: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.RetryCount == nil || *r.RetryCount != 7 {
			t.Errorf("row %s retryCount in the result = %v, want 7 (was 3, +4)", r.ID, r.RetryCount)
		}
	}
}

// TestBulkUpdateOperators_NoMatchedRowIsAnEmptyResult pins that an
// operator-only call whose filter matches nothing succeeds with no rows.
func TestBulkUpdateOperators_NoMatchedRowIsAnEmptyResult(t *testing.T) {
	truncateAll(t)
	seeded := uopSeedEvent(t, "upf-present")

	rows := upfUpdateEvents(t, `{ action: { eq: "upf-absent" } }`, `{ retryCount_inc: 1 }`)

	if len(rows) != 0 {
		t.Errorf("updateEvents matching nothing returned %+v, want no rows", rows)
	}
	if got := uopReadEvent(t, seeded).retryCount; got.Int64 != 3 {
		t.Errorf("unmatched row retry_count = %+v, want 3 unchanged", got)
	}
}

// TestBulkUpdateOperators_PairingIsInvalidInput pins that the bulk mutation
// rejects the pairings the single-row one does, before writing anything.
func TestBulkUpdateOperators_PairingIsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "set and increment on one column", input: `{ retryCount: 9, retryCount_inc: 1 }`},
		{name: "set and decrement on one column", input: `{ retryCount: 9, retryCount_dec: 1 }`},
		{name: "increment and decrement on one column", input: `{ retryCount_inc: 1, retryCount_dec: 1 }`},
		{name: "explicit null and increment on one column", input: `{ retryCount: null, retryCount_inc: 1 }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			id := uopSeedEvent(t, "upf-pairing")

			resp := gqlExec(t, `
				mutation {
					updateEvents(filter: { action: { eq: "upf-pairing" } }, input: `+tt.input+`) { id }
				}
			`, nil, nil)

			if len(resp.Errors) != 1 {
				t.Fatalf("updateEvents(%s): got %d errors, want 1: %+v", tt.input, len(resp.Errors), resp.Errors)
			}
			if got, want := extensionsCode(resp.Errors[0]), "INVALID_INPUT"; got != want {
				t.Errorf("updateEvents(%s): code = %q, want %q", tt.input, got, want)
			}
			if got := uopReadEvent(t, id).retryCount; got.Int64 != 3 {
				t.Errorf("updateEvents(%s): retry_count = %+v, want 3 unchanged", tt.input, got)
			}
		})
	}
}
