package tests

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/events/models"
)

// SQLite compares DATETIME values as text, so a value sqlgen writes or binds
// has to be in the text datetime() writes. seedAuditLogTimes inserts three
// audit_logs rows at one instant (two written by datetime(), one by Create with
// the instant in a non-UTC zone) and a fourth a day later, all tagged with the
// test's name. It returns their ids in (created_at, id) order.
func seedAuditLogTimes(t *testing.T, client *models.Client) []int64 {
	t.Helper()
	ctx := context.Background()
	var ids []int64
	insert := func(at string) {
		res, err := testDB.ExecContext(ctx, `INSERT INTO audit_logs (message, created_at) VALUES (?, datetime(?))`, t.Name(), at)
		if err != nil {
			t.Fatalf("insert audit log at %s: %v", at, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last insert id: %v", err)
		}
		ids = append(ids, id)
	}
	insert("2026-01-01 00:00:00")
	insert("2026-01-01 00:00:00")
	est := time.FixedZone("EST", -5*3600)
	row, err := client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message:   t.Name(),
		CreatedAt: omittable.Set(types.DateTime{Time: time.Date(2025, 12, 31, 19, 0, 0, 0, est)}),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ids = append(ids, row.ID)
	insert("2026-01-02 00:00:00")
	t.Cleanup(func() { _ = client.AuditLogs().HardDeleteMany(context.Background(), ids) })
	return ids
}

// TestDateTime_ConnectionPagesAcrossEqualTimestamps pages one row at a time
// over a (created_at, id) keyset in both directions. A cursor that binds a
// different text format from the stored one skips or repeats the rows that
// share a timestamp.
func TestDateTime_ConnectionPagesAcrossEqualTimestamps(t *testing.T) {
	ctx := context.Background()
	client, _ := newClientWithBus(t)
	want := seedAuditLogTimes(t, client)
	msg := t.Name()
	filter := &models.AuditLogFilter{Message: &comparator.String{Eq: &msg}}
	one := 1

	var forward []int64
	var after *string
	for range len(want) + 1 {
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
	if !slices.Equal(forward, want) {
		t.Errorf("forward pages = %v, want %v", forward, want)
	}

	var backward []int64
	var before *string
	for range len(want) + 1 {
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
	slices.Reverse(backward)
	if !slices.Equal(backward, want) {
		t.Errorf("backward pages (reversed) = %v, want %v", backward, want)
	}
}

// TestDateTime_EqFilterMatchesScannedTime filters on the time scanned from a
// datetime()-written row. It must match that row, its twin, and the row Create
// wrote at the same instant.
func TestDateTime_EqFilterMatchesScannedTime(t *testing.T) {
	ctx := context.Background()
	client, _ := newClientWithBus(t)
	ids := seedAuditLogTimes(t, client)
	first, err := client.AuditLogs().Get(ctx, ids[0])
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	msg := t.Name()
	got, err := client.AuditLogs().GetMany(ctx, &models.GetAuditLogsInput{
		Filter: &models.AuditLogFilter{
			Message:   &comparator.String{Eq: &msg},
			CreatedAt: &comparator.Time{Eq: &first.CreatedAt.Time},
		},
		Sorts: []sql.Sort{{Column: "id", Direction: sql.Asc}},
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	gotIDs := make([]int64, len(got))
	for i, row := range got {
		gotIDs[i] = row.ID
	}
	if want := ids[:3]; !slices.Equal(gotIDs, want) {
		t.Errorf("created_at = %v matched %v, want %v", first.CreatedAt.Time, gotIDs, want)
	}
}
