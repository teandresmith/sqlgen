package tests

import (
	"context"
	"database/sql"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// TestDateTimeOverrideRoundTrip covers a MySQL column overridden to
// types.DateTime / types.NullDateTime (warehouses.created_at TIMESTAMP and
// inspected_at DATETIME(6)). The SQL builders bind such a value as the
// time.Time it wraps, so it round-trips like a time.Time column under every
// driver loc and session time zone: a UTC time is written (MySQL rejects the
// RFC 3339 "Z" that types.DateTime.Value writes), read back as the same
// instant, matched by a filter, and paged through by a cursor whose keyset
// holds a types.DateTime.
func TestDateTimeOverrideRoundTrip(t *testing.T) {
	inst := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	frac := inst.Add(500 * time.Millisecond)
	est := time.FixedZone("EST", -5*3600)

	for _, loc := range []string{"UTC", "Local"} {
		for _, tz := range []string{"+00:00", "-05:00"} {
			t.Run("loc="+loc+"/time_zone="+tz, func(t *testing.T) {
				ctx := context.Background()
				db, err := sql.Open("mysql", testConnStr+"&loc="+loc+"&time_zone="+url.QueryEscape("'"+tz+"'"))
				if err != nil {
					t.Fatalf("open: %v", err)
				}
				t.Cleanup(func() { _ = db.Close() })
				client := models.New(dbstdlib.New(db))
				wh := client.Warehouses()

				create := func(name string, createdAt time.Time, inspectedAt types.NullDateTime) *models.Warehouse {
					t.Helper()
					row, err := wh.Create(ctx, &models.CreateWarehouseInput{
						ExternalID:  ksuid.New(),
						Name:        name,
						Price:       decimal.NewFromInt(1),
						InspectedAt: omittable.Set(inspectedAt),
						CreatedAt:   omittable.Set(types.DateTime{Time: createdAt}),
					})
					if err != nil {
						t.Fatalf("Create %s: %v", name, err)
					}
					t.Cleanup(func() { _ = newClient().Warehouses().HardDelete(context.Background(), row.ID) })
					return row
				}
				a := create("dt-a", inst, types.NullDateTime{Time: frac, Valid: true})
				b := create("dt-b", inst.In(est), types.NullDateTime{Time: frac.In(est), Valid: true})
				c := create("dt-c", inst.Add(time.Hour), types.NullDateTime{})
				ids := []int64{a.ID, b.ID, c.ID}

				for _, w := range []struct {
					row         *models.Warehouse
					createdAt   time.Time
					inspectedAt bool
				}{{a, inst, true}, {b, inst, true}, {c, inst.Add(time.Hour), false}} {
					got, err := wh.Get(ctx, w.row.ID)
					if err != nil {
						t.Fatalf("Get %s: %v", w.row.Name, err)
					}
					if !got.CreatedAt.Equal(w.createdAt) {
						t.Errorf("%s created_at = %s, want %s", got.Name, got.CreatedAt.Time, w.createdAt)
					}
					if got.InspectedAt.Valid != w.inspectedAt || (w.inspectedAt && !got.InspectedAt.Time.Equal(frac)) {
						t.Errorf("%s inspected_at = %+v, want valid=%v %s", got.Name, got.InspectedAt, w.inspectedAt, frac)
					}
				}

				later := frac.Add(time.Minute).In(est)
				if _, err := wh.Update(ctx, c.ID, &models.UpdateWarehouseInput{
					InspectedAt: omittable.Set(types.NullDateTime{Time: later, Valid: true}),
				}); err != nil {
					t.Fatalf("Update: %v", err)
				}
				got, err := wh.Get(ctx, c.ID)
				if err != nil {
					t.Fatalf("Get after Update: %v", err)
				}
				if !got.InspectedAt.Valid || !got.InspectedAt.Time.Equal(later) {
					t.Errorf("updated inspected_at = %+v, want %s", got.InspectedAt, later)
				}

				matched, err := wh.GetMany(ctx, &models.GetWarehousesInput{Filter: &models.WarehouseFilter{
					ID:          &comparator.Number[int64]{In: ids},
					CreatedAt:   &comparator.Time{Eq: &inst},
					InspectedAt: &comparator.NullableTime{Time: comparator.Time{Eq: &frac}},
				}})
				if err != nil {
					t.Fatalf("GetMany: %v", err)
				}
				if diff := cmp.Diff([]int64{a.ID, b.ID}, warehouseIDs(matched)); diff != "" {
					t.Errorf("filter matched (-want +got):\n%s", diff)
				}

				filter := &models.WarehouseFilter{ID: &comparator.Number[int64]{In: ids}}
				var forward []int64
				var after *string
				for range len(ids) + 2 {
					page, err := wh.Connection(ctx, models.ConnectionInput[models.WarehouseFilter]{Filter: filter, First: new(1), After: after})
					if err != nil {
						t.Fatalf("Connection forward: %v", err)
					}
					for _, e := range page.Edges {
						forward = append(forward, e.Node.ID)
					}
					if !page.PageInfo.HasNextPage {
						break
					}
					after = page.PageInfo.EndCursor
				}
				if diff := cmp.Diff(ids, forward); diff != "" {
					t.Errorf("forward pages (-want +got):\n%s", diff)
				}

				var backward []int64
				var before *string
				for range len(ids) + 2 {
					page, err := wh.Connection(ctx, models.ConnectionInput[models.WarehouseFilter]{Filter: filter, Last: new(1), Before: before})
					if err != nil {
						t.Fatalf("Connection backward: %v", err)
					}
					for _, e := range page.Edges {
						backward = append(backward, e.Node.ID)
					}
					if !page.PageInfo.HasPreviousPage {
						break
					}
					before = page.PageInfo.StartCursor
				}
				slices.Reverse(backward)
				if diff := cmp.Diff(ids, backward); diff != "" {
					t.Errorf("backward pages, reversed (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func warehouseIDs(rows []*models.Warehouse) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	slices.Sort(out)
	return out
}
