package tests

import (
	"strings"
	"testing"
)

// The single-row `updateEvent(id, input)` mutation rejects a set field paired
// with an operator on the same column, before writing anything.

// uprUpdateEvent runs updateEvent against id with the given input literal and
// returns the response, errors included.
func uprUpdateEvent(t *testing.T, id, input string) gqlResponse {
	t.Helper()
	return gqlExec(t, `
		mutation ($id: UUID!) {
			updateEvent(id: $id, input: `+input+`) { id }
		}
	`, map[string]any{"id": id}, nil)
}

// TestUpdatePairing_SetAndDecrementIsInvalidInput pins that `<f>` together
// with `<f>_dec` is INVALID_INPUT on the single-row mutation, exactly as
// `<f>` with `<f>_inc` is, and that the row is left as it was: neither the
// set value nor the decrement lands.
func TestUpdatePairing_SetAndDecrementIsInvalidInput(t *testing.T) {
	truncateAll(t)
	id := uopSeedEvent(t, "upr-set-dec")

	resp := uprUpdateEvent(t, id, `{ retryCount: 9, retryCount_dec: 1 }`)

	if len(resp.Errors) != 1 {
		t.Fatalf("updateEvent(retryCount: 9, retryCount_dec: 1): got %d errors, want 1: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "INVALID_INPUT"; got != want {
		t.Errorf("updateEvent(retryCount: 9, retryCount_dec: 1): code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "cannot be both set and decremented") {
		t.Errorf("updateEvent(retryCount: 9, retryCount_dec: 1): message = %q, want it to name the set-and-decrement pairing", resp.Errors[0].Message)
	}
	if got := uopReadEvent(t, id).retryCount; !got.Valid || got.Int64 != 3 {
		t.Errorf("updateEvent(retryCount: 9, retryCount_dec: 1): retry_count = %+v, want 3 unchanged", got)
	}
}
