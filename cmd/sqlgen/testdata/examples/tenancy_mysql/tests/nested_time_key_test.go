package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// TestNestedTimeKeyZones drives a nested connect and disconnect into a target
// keyed by a time, naming each key in another zone than the row the driver
// scans back. One instant names one row whatever its zone: alone, beside the
// same instant in UTC, and across the two verbs of one block.
func TestNestedTimeKeyZones(t *testing.T) {
	ctx := context.Background()
	resetDB(t)
	c := newTenantEnv(t).clientA
	ist := time.FixedZone("IST", 5*3600+30*60)
	base := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)

	moment := func(t *testing.T, minute int) time.Time {
		t.Helper()
		at := base.Add(time.Duration(minute) * time.Minute)
		if _, err := c.Moments().Create(ctx, &models.CreateMomentInput{At: at, Label: "m"}); err != nil {
			t.Fatalf("create moment: %v", err)
		}
		return at
	}
	owner := func(t *testing.T) int64 {
		t.Helper()
		o, err := c.MomentOwners().Create(ctx, &models.CreateMomentOwnerInput{Name: "o"})
		if err != nil {
			t.Fatalf("create owner: %v", err)
		}
		return o.ID
	}
	tag := func(t *testing.T) int64 {
		t.Helper()
		g, err := c.MomentTags().Create(ctx, &models.CreateMomentTagInput{Name: "g"})
		if err != nil {
			t.Fatalf("create tag: %v", err)
		}
		return g.ID
	}
	connectO2M := func(id int64, nested *models.MomentOwnerMomentsUpdateNested) error {
		_, err := c.MomentOwners().UpdateWithRelated(ctx, id, &models.UpdateMomentOwnerWithRelatedInput{
			MomentOwner: models.UpdateMomentOwnerInput{Name: omittable.Set("o")},
			Moments:     nested,
		})
		return err
	}
	connectM2M := func(id int64, nested *models.MomentTagMomentsUpdateNested) error {
		_, err := c.MomentTags().UpdateWithRelated(ctx, id, &models.UpdateMomentTagWithRelatedInput{
			MomentTag: models.UpdateMomentTagInput{Name: omittable.Set("g")},
			Moments:   nested,
		})
		return err
	}
	links := func(t *testing.T, tagID int64) int {
		t.Helper()
		rows, err := c.MomentTagLinks().GetMany(ctx, &models.GetMomentTagLinksInput{
			Filter: &models.MomentTagLinkFilter{TagID: &comparator.Number[int64]{Eq: new(tagID)}},
		})
		if err != nil {
			t.Fatalf("GetMany moment_tag_links: %v", err)
		}
		return len(rows)
	}

	cases := []struct {
		name string
		keys func(at time.Time) []time.Time
	}{
		// UTC is the foreign zone on pgx, which scans a timestamptz in time.Local.
		{"utc", func(at time.Time) []time.Time { return []time.Time{at} }},
		{"foreign zone", func(at time.Time) []time.Time { return []time.Time{at.In(ist)} }},
		{"local", func(at time.Time) []time.Time { return []time.Time{at.In(time.Local)} }},
		{"two zones", func(at time.Time) []time.Time { return []time.Time{at, at.In(ist)} }},
	}
	for i, tc := range cases {
		t.Run("o2m connect "+tc.name, func(t *testing.T) {
			at, id := moment(t, i), owner(t)
			if err := connectO2M(id, &models.MomentOwnerMomentsUpdateNested{Connect: tc.keys(at)}); err != nil {
				t.Fatalf("connect: %v", err)
			}
			// Through the cache: the nested write invalidated before it returned.
			got, err := c.Moments().Get(ctx, at)
			if err != nil {
				t.Fatalf("Get moment: %v", err)
			}
			if got.OwnerID == nil || *got.OwnerID != id {
				t.Errorf("owner_id = %v, want %d", got.OwnerID, id)
			}
		})
		t.Run("m2m connect and disconnect "+tc.name, func(t *testing.T) {
			at, id := moment(t, 10+i), tag(t)
			if err := connectM2M(id, &models.MomentTagMomentsUpdateNested{Connect: tc.keys(at)}); err != nil {
				t.Fatalf("connect: %v", err)
			}
			if n := links(t, id); n != 1 {
				t.Fatalf("links after connect = %d, want 1", n)
			}
			if err := connectM2M(id, &models.MomentTagMomentsUpdateNested{Disconnect: tc.keys(at)}); err != nil {
				t.Fatalf("disconnect: %v", err)
			}
			if n := links(t, id); n != 0 {
				t.Errorf("links after disconnect = %d, want 0", n)
			}
		})
	}

	t.Run("o2m verb conflict across zones", func(t *testing.T) {
		at := moment(t, 20)
		err := connectO2M(owner(t), &models.MomentOwnerMomentsUpdateNested{Connect: []time.Time{at}, Disconnect: []time.Time{at.In(ist)}})
		if !errors.Is(err, models.ErrNestedVerbConflict) {
			t.Errorf("error = %v, want ErrNestedVerbConflict", err)
		}
	})
	t.Run("m2m verb conflict across zones", func(t *testing.T) {
		at := moment(t, 21)
		err := connectM2M(tag(t), &models.MomentTagMomentsUpdateNested{Connect: []time.Time{at}, Disconnect: []time.Time{at.In(ist)}})
		if !errors.Is(err, models.ErrNestedVerbConflict) {
			t.Errorf("error = %v, want ErrNestedVerbConflict", err)
		}
	})
}
