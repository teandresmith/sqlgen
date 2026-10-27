package tests

import (
	"context"
	"database/sql"
	"testing"
)

// The GraphQL update surface's operator and null contracts, exercised against
// the events table.
//
// Every `<col>_inc` / `<col>_dec` field the UpdateEventInput schema advertises
// is a promise: an operator the schema accepts and the executor then drops is
// an accepted-and-ignored input, which reports success while doing nothing.
// The bulk `updateEvents(filter, input)` mutation takes the same input type and
// therefore owes the same translation. And GraphQL distinguishes an explicit
// `null` from an omitted field, so `{ col: null }` must clear a nullable column
// while omitting it must leave the column alone.

// uopEventRow is the subset of an events row these tests read back. Every
// column is nullable, so each is scanned into its sql.Null form.
type uopEventRow struct {
	adjustment  sql.NullString
	retryCount  sql.NullInt64
	ioCount     sql.NullInt64
	tcpPort     sql.NullInt64
	processedAt sql.NullTime
	mac         sql.NullString
}

// uopSeedEvent inserts one events row with every column under test holding a
// non-NULL value, and returns its id. It writes through SQL rather than
// createEvent so the starting state does not depend on the surface under test.
func uopSeedEvent(t *testing.T, action string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO events (action, occurred_at, processed_at, adjustment, retry_count, io_count, tcp_port, mac)
		VALUES ($1, '2024-01-01T00:00:00Z', '2024-01-02T00:00:00Z', 10.50, 3, 5, 8080, 'aa:bb')
		RETURNING id::text
	`, action).Scan(&id)
	if err != nil {
		t.Fatalf("seed events row %q: %v", action, err)
	}
	return id
}

// uopReadEvent reads the row back straight from Postgres, so the assertion
// sees what was persisted rather than what the mutation chose to return.
func uopReadEvent(t *testing.T, id string) uopEventRow {
	t.Helper()
	var r uopEventRow
	err := testPool.QueryRow(context.Background(), `
		SELECT adjustment::text, retry_count, io_count, tcp_port, processed_at, mac
		FROM events WHERE id = $1
	`, id).Scan(&r.adjustment, &r.retryCount, &r.ioCount, &r.tcpPort, &r.processedAt, &r.mac)
	if err != nil {
		t.Fatalf("read back events row %s: %v", id, err)
	}
	return r
}

// uopMustExec runs a mutation and fails the test on any GraphQL error.
func uopMustExec(t *testing.T, query string, vars map[string]any) {
	t.Helper()
	resp := gqlExec(t, query, vars, nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
}

// TestUpdateOperators_BulkIncrementIsApplied pins that updateEvents translates
// `_inc` exactly as updateEvent does: the filter selects one row and the
// operator moves that row's adjustment by +2, leaving its sibling untouched.
func TestUpdateOperators_BulkIncrementIsApplied(t *testing.T) {
	truncateAll(t)
	target := uopSeedEvent(t, "uop-bulk-target")
	other := uopSeedEvent(t, "uop-bulk-other")

	uopMustExec(t, `
		mutation {
			updateEvents(filter: { action: { eq: "uop-bulk-target" } }, input: { adjustment_inc: 2 }) { id }
		}
	`, nil)

	if got := uopReadEvent(t, target).adjustment; !got.Valid || got.String != "12.50" {
		t.Errorf("updateEvents(adjustment_inc: 2): target adjustment = %+v, want 12.50 (was 10.50)", got)
	}
	if got := uopReadEvent(t, other).adjustment; !got.Valid || got.String != "10.50" {
		t.Errorf("updateEvents(adjustment_inc: 2): row outside the filter has adjustment = %+v, want 10.50 unchanged", got)
	}
}

// TestUpdateOperators_EveryAdvertisedIncrementIsApplied walks every numeric
// column for which UpdateEventInput advertises `_inc` / `_dec`, and checks the
// single-row update moves the stored value by the requested amount.
func TestUpdateOperators_EveryAdvertisedIncrementIsApplied(t *testing.T) {
	tests := []struct {
		name  string
		field string
		read  func(uopEventRow) (int64, bool)
		start int64
		delta int64
	}{
		{
			name: "retryCount_inc", field: "retryCount_inc", start: 3, delta: 1,
			read: func(r uopEventRow) (int64, bool) { return r.retryCount.Int64, r.retryCount.Valid },
		},
		{
			name: "retryCount_dec", field: "retryCount_dec", start: 3, delta: -1,
			read: func(r uopEventRow) (int64, bool) { return r.retryCount.Int64, r.retryCount.Valid },
		},
		{
			name: "ioCount_inc", field: "ioCount_inc", start: 5, delta: 1,
			read: func(r uopEventRow) (int64, bool) { return r.ioCount.Int64, r.ioCount.Valid },
		},
		{
			name: "ioCount_dec", field: "ioCount_dec", start: 5, delta: -1,
			read: func(r uopEventRow) (int64, bool) { return r.ioCount.Int64, r.ioCount.Valid },
		},
		{
			name: "tcpPort_inc", field: "tcpPort_inc", start: 8080, delta: 1,
			read: func(r uopEventRow) (int64, bool) { return r.tcpPort.Int64, r.tcpPort.Valid },
		},
		{
			name: "tcpPort_dec", field: "tcpPort_dec", start: 8080, delta: -1,
			read: func(r uopEventRow) (int64, bool) { return r.tcpPort.Int64, r.tcpPort.Valid },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			id := uopSeedEvent(t, "uop-inc")

			uopMustExec(t, `
				mutation ($id: UUID!) {
					updateEvent(id: $id, input: { `+tt.field+`: 1 }) { id }
				}
			`, map[string]any{"id": id})

			got, valid := tt.read(uopReadEvent(t, id))
			if want := tt.start + tt.delta; !valid || got != want {
				t.Errorf("updateEvent(%s: 1): stored value = %d (valid=%v), want %d (was %d)",
					tt.field, got, valid, want, tt.start)
			}
		})
	}
}

// TestUpdateOperators_ExplicitNullClearsColumn pins the null/omitted
// distinction on nullable update fields: `{ col: null }` writes NULL, and an
// update that omits the field leaves the stored value as it was.
func TestUpdateOperators_ExplicitNullClearsColumn(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		isNull func(uopEventRow) bool
	}{
		{name: "Int column", field: "retryCount", isNull: func(r uopEventRow) bool { return !r.retryCount.Valid }},
		{name: "String column", field: "mac", isNull: func(r uopEventRow) bool { return !r.mac.Valid }},
		{name: "NullDateTime column", field: "processedAt", isNull: func(r uopEventRow) bool { return !r.processedAt.Valid }},
		{name: "NullDecimal column", field: "adjustment", isNull: func(r uopEventRow) bool { return !r.adjustment.Valid }},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/explicit null clears", func(t *testing.T) {
			truncateAll(t)
			id := uopSeedEvent(t, "uop-null")

			uopMustExec(t, `
				mutation ($id: UUID!) {
					updateEvent(id: $id, input: { `+tt.field+`: null }) { id }
				}
			`, map[string]any{"id": id})

			if !tt.isNull(uopReadEvent(t, id)) {
				t.Errorf("updateEvent(%s: null): column still holds a value, want NULL", tt.field)
			}
		})

		t.Run(tt.name+"/omitted leaves unchanged", func(t *testing.T) {
			truncateAll(t)
			id := uopSeedEvent(t, "uop-null")

			uopMustExec(t, `
				mutation ($id: UUID!) {
					updateEvent(id: $id, input: { action: "renamed" }) { id }
				}
			`, map[string]any{"id": id})

			if tt.isNull(uopReadEvent(t, id)) {
				t.Errorf("updateEvent without %s: column became NULL, want it unchanged", tt.field)
			}
		})
	}
}
