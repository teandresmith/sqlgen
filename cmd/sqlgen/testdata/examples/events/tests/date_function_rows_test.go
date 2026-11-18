package tests

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// SQLite's date() and CURRENT_DATE write a bare date ("2026-01-01"), while
// sqlgen writes and binds every time value, a date included, as datetime()
// text ("2026-01-01 00:00:00"). The tests below write rows through date() and
// CURRENT_DATE into audit_logs.created_at, a types.DateTime column like a
// SQLite `date` column, and check what that mismatch does to reads.

// gtwInsertAuditLogAt inserts an audit_logs row tagged with the test's name
// whose created_at is the SQL expression expr, and returns its id.
func gtwInsertAuditLogAt(t *testing.T, client *models.Client, expr string) int64 {
	t.Helper()
	ctx := context.Background()
	res, err := testDB.ExecContext(ctx, `INSERT INTO audit_logs (message, created_at) VALUES (?, `+expr+`)`, t.Name())
	if err != nil {
		t.Fatalf("insert audit log at %s: %v", expr, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	t.Cleanup(func() { _ = client.AuditLogs().HardDelete(context.Background(), id) })
	return id
}

// gtwAuditLogIDs returns the ids of this test's audit_logs rows that match
// createdAt, in id order.
func gtwAuditLogIDs(t *testing.T, client *models.Client, createdAt comparator.Time) []int64 {
	t.Helper()
	msg := t.Name()
	rows, err := client.AuditLogs().GetMany(context.Background(), &models.GetAuditLogsInput{
		Filter: &models.AuditLogFilter{
			Message:   &comparator.String{Eq: &msg},
			CreatedAt: &createdAt,
		},
		Sorts: []sql.Sort{{Column: "id", Direction: sql.Asc}},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func TestDateFunctionRows_CompareAsDifferentText(t *testing.T) {
	ctx := context.Background()
	client, _ := newClientWithBus(t)
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan2 := jan1.AddDate(0, 0, 1)

	t.Run("scans to the same time as sqlgen's text", func(t *testing.T) {
		dated := gtwInsertAuditLogAt(t, client, "date('2026-01-01')")
		datetimed := gtwInsertAuditLogAt(t, client, "datetime('2026-01-01 00:00:00')")
		for _, id := range []int64{dated, datetimed} {
			row, err := client.AuditLogs().Get(ctx, id)
			if err != nil {
				t.Fatalf("Get(%d): %v", id, err)
			}
			if !row.CreatedAt.Time.Equal(jan1) {
				t.Errorf("Get(%d).CreatedAt = %v, want %v", id, row.CreatedAt.Time, jan1)
			}
		}
	})

	t.Run("Eq and In miss a date() row", func(t *testing.T) {
		gtwInsertAuditLogAt(t, client, "date('2026-01-01')")
		datetimed := gtwInsertAuditLogAt(t, client, "datetime('2026-01-01 00:00:00')")
		want := []int64{datetimed}
		if got := gtwAuditLogIDs(t, client, comparator.Time{Eq: &jan1}); !slices.Equal(got, want) {
			t.Errorf("Eq %v matched %v, want %v", jan1, got, want)
		}
		if got := gtwAuditLogIDs(t, client, comparator.Time{In: []time.Time{jan1}}); !slices.Equal(got, want) {
			t.Errorf("In [%v] matched %v, want %v", jan1, got, want)
		}
	})

	t.Run("Eq misses a CURRENT_DATE row", func(t *testing.T) {
		gtwInsertAuditLogAt(t, client, "CURRENT_DATE")
		now := time.Now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if got := gtwAuditLogIDs(t, client, comparator.Time{Eq: &today}); len(got) != 0 {
			// The UTC date can turn over between the insert and the filter.
			if time.Now().UTC().Day() == now.Day() {
				t.Errorf("Eq %v matched CURRENT_DATE rows %v, want none", today, got)
			}
		}
	})

	t.Run("sorts before every bound value of its day", func(t *testing.T) {
		jan1Date := gtwInsertAuditLogAt(t, client, "date('2026-01-01')")
		jan1Time := gtwInsertAuditLogAt(t, client, "datetime('2026-01-01 00:00:00')")
		jan2Date := gtwInsertAuditLogAt(t, client, "date('2026-01-02')")
		if got, want := gtwAuditLogIDs(t, client, comparator.Time{Gte: &jan1}), []int64{jan1Time, jan2Date}; !slices.Equal(got, want) {
			t.Errorf("Gte %v matched %v, want %v (the date() row of that day excluded)", jan1, got, want)
		}
		if got, want := gtwAuditLogIDs(t, client, comparator.Time{Lt: &jan2}), []int64{jan1Date, jan1Time, jan2Date}; !slices.Equal(got, want) {
			t.Errorf("Lt %v matched %v, want %v (the next day's date() row included)", jan2, got, want)
		}
	})

	t.Run("cursor skips forward and repeats backward", func(t *testing.T) {
		// The keyset binds a date() row's created_at as datetime() text, which
		// sorts after the row's own text: paging forward passes over the next
		// date() row of that day, and paging backward finds the row itself
		// again.
		first := gtwInsertAuditLogAt(t, client, "date('2026-01-01')")
		gtwInsertAuditLogAt(t, client, "date('2026-01-01')")
		last := gtwInsertAuditLogAt(t, client, "date('2026-01-02')")
		msg := t.Name()
		filter := &models.AuditLogFilter{Message: &comparator.String{Eq: &msg}}
		one := 1
		const pages = 4

		var forward []int64
		var after *string
		for range pages {
			conn, err := client.AuditLogs().Connection(ctx, models.ConnectionInput[models.AuditLogFilter]{Filter: filter, First: &one, After: after})
			if err != nil {
				t.Fatalf("Connection forward: %v", err)
			}
			for _, e := range conn.Edges {
				forward = append(forward, e.Node.ID)
			}
			if !conn.PageInfo.HasNextPage {
				break
			}
			after = conn.PageInfo.EndCursor
		}
		if want := []int64{first, last}; !slices.Equal(forward, want) {
			t.Errorf("forward pages = %v, want %v (the second date() row of a day skipped)", forward, want)
		}

		var backward []int64
		var before *string
		for range pages {
			conn, err := client.AuditLogs().Connection(ctx, models.ConnectionInput[models.AuditLogFilter]{Filter: filter, Last: &one, Before: before})
			if err != nil {
				t.Fatalf("Connection backward: %v", err)
			}
			for _, e := range conn.Edges {
				backward = append(backward, e.Node.ID)
			}
			if !conn.PageInfo.HasPreviousPage {
				break
			}
			before = conn.PageInfo.StartCursor
		}
		if want := []int64{last, last, last, last}; !slices.Equal(backward, want) {
			t.Errorf("backward pages = %v, want %v (the same date() row on every page)", backward, want)
		}
	})
}
