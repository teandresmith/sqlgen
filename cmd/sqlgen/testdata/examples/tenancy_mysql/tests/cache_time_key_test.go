package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// TestCacheTimeKeyZones caches a time-keyed row read in UTC, then writes it
// with the key named in another zone. One instant is one cache key whatever
// its zone, so the write evicts the cached entry and the next read is fresh:
// for a single time PK (update) and for a composite PK with a time member
// (delete).
func TestCacheTimeKeyZones(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	c := newTenantEnv(t).clientA
	ist := time.FixedZone("IST", 5*3600+30*60)
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)

	t.Run("single pk update", func(t *testing.T) {
		o, err := c.MomentOwners().Create(ctx, &models.CreateMomentOwnerInput{Name: "o"})
		if err != nil {
			t.Fatalf("create owner: %v", err)
		}
		if _, err := c.Moments().Create(ctx, &models.CreateMomentInput{At: at, Label: "m"}); err != nil {
			t.Fatalf("create moment: %v", err)
		}
		if _, err := c.Moments().Get(ctx, at); err != nil {
			t.Fatalf("warm Get: %v", err)
		}
		if _, err := c.Moments().Update(ctx, at.In(ist), &models.UpdateMomentInput{OwnerID: omittable.Set(new(o.ID))}); err != nil {
			t.Fatalf("Update in IST: %v", err)
		}
		got, err := c.Moments().Get(ctx, at)
		if err != nil {
			t.Fatalf("Get after update: %v", err)
		}
		if got.OwnerID == nil || *got.OwnerID != o.ID {
			t.Errorf("Get after an IST-keyed update: OwnerID = %v, want %d (stale cache entry)", got.OwnerID, o.ID)
		}
	})

	t.Run("composite pk delete", func(t *testing.T) {
		g, err := c.MomentTags().Create(ctx, &models.CreateMomentTagInput{Name: "g"})
		if err != nil {
			t.Fatalf("create tag: %v", err)
		}
		pk := models.MomentTagLinkPK{TagID: g.ID, MomentAt: at}
		if _, err := c.MomentTagLinks().Create(ctx, &models.CreateMomentTagLinkInput{TagID: g.ID, MomentAt: at}); err != nil {
			t.Fatalf("create link: %v", err)
		}
		if _, err := c.MomentTagLinks().Get(ctx, pk); err != nil {
			t.Fatalf("warm Get: %v", err)
		}
		if err := c.MomentTagLinks().HardDelete(ctx, models.MomentTagLinkPK{TagID: g.ID, MomentAt: at.In(ist)}); err != nil {
			t.Fatalf("HardDelete in IST: %v", err)
		}
		got, err := c.MomentTagLinks().Get(ctx, pk)
		if !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get after an IST-keyed delete = (%v, %v), want ErrNotFound (stale cache entry)", got, err)
		}
	})
}
