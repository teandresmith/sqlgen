package tests

// A many-to-many loader fetches its targets with a filter on the target's own
// key field. badges is keyed on the TEXT column `code` and owners on the
// INTEGER column `owner_key`, so neither target has an `ID` filter field.

import (
	"context"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestM2MLoad_TargetsKeyedOnNonIDColumns(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	var ownerKeys []int64
	for _, name := range []string{"badge-owner-a", "badge-owner-b"} {
		o, err := client.Owners().Create(ctx, &models.CreateOwnerInput{Name: name})
		if err != nil {
			t.Fatalf("Create owner %s: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Owners().HardDelete(ctx, o.OwnerKey) })
		ownerKeys = append(ownerKeys, o.OwnerKey)
	}
	codes := []string{"m2m-key-gold", "m2m-key-silver", "m2m-key-bronze"}
	for _, code := range codes {
		if _, err := client.Badges().Create(ctx, &models.CreateBadgeInput{Code: code, Label: code}); err != nil {
			t.Fatalf("Create badge %s: %v", code, err)
		}
		t.Cleanup(func() { _ = client.Badges().HardDelete(ctx, code) })
	}

	// Owner a holds gold and silver, owner b holds silver; bronze has no owner.
	links := []models.CreateOwnerBadgeInput{
		{OwnerID: ownerKeys[0], BadgeCode: codes[0]},
		{OwnerID: ownerKeys[0], BadgeCode: codes[1]},
		{OwnerID: ownerKeys[1], BadgeCode: codes[1]},
	}
	for _, in := range links {
		if _, err := client.OwnerBadges().Create(ctx, &in); err != nil {
			t.Fatalf("Create owner badge %+v: %v", in, err)
		}
		// Registered after the owners and badges, so it runs before them.
		t.Cleanup(func() {
			_ = client.OwnerBadges().HardDelete(ctx, models.OwnerBadgePK{OwnerID: in.OwnerID, BadgeCode: in.BadgeCode})
		})
	}

	t.Run("owner badges by text key", func(t *testing.T) {
		owners, err := client.Owners().GetMany(ctx, &models.GetOwnersInput{
			Filter: &models.OwnerFilter{OwnerKey: &comparator.Number[int64]{In: ownerKeys}},
		}, func(o *models.CallOptions[models.OwnerFieldOptions]) {
			o.FieldOptions = &models.OwnerFieldOptions{
				OwnerKey: true,
				Badges:   &models.BadgeRelationshipOptions{FieldOptions: &models.BadgeFieldOptions{Label: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany owners with Badges: %v", err)
		}
		want := map[int64][]string{
			ownerKeys[0]: {codes[0], codes[1]},
			ownerKeys[1]: {codes[1]},
		}
		if len(owners) != len(want) {
			t.Fatalf("GetMany returned %d owners, want %d", len(owners), len(want))
		}
		for _, o := range owners {
			var got []string
			for _, b := range o.Badges {
				got = append(got, b.Code)
			}
			slices.Sort(got)
			wantCodes := slices.Sorted(slices.Values(want[o.OwnerKey]))
			if !slices.Equal(got, wantCodes) {
				t.Errorf("owner %d: Badges = %v, want %v", o.OwnerKey, got, wantCodes)
			}
		}
	})

	t.Run("badge owners by integer key", func(t *testing.T) {
		badges, err := client.Badges().GetMany(ctx, &models.GetBadgesInput{
			Filter: &models.BadgeFilter{Code: &comparator.ID{In: codes}},
		}, func(o *models.CallOptions[models.BadgeFieldOptions]) {
			o.FieldOptions = &models.BadgeFieldOptions{
				Code:   true,
				Owners: &models.OwnerRelationshipOptions{FieldOptions: &models.OwnerFieldOptions{Name: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany badges with Owners: %v", err)
		}
		want := map[string][]int64{
			codes[0]: {ownerKeys[0]},
			codes[1]: {ownerKeys[0], ownerKeys[1]},
			codes[2]: nil,
		}
		if len(badges) != len(want) {
			t.Fatalf("GetMany returned %d badges, want %d", len(badges), len(want))
		}
		for _, b := range badges {
			var got []int64
			for _, o := range b.Owners {
				got = append(got, o.OwnerKey)
			}
			slices.Sort(got)
			wantKeys := slices.Sorted(slices.Values(want[b.Code]))
			if !slices.Equal(got, wantKeys) {
				t.Errorf("badge %s: Owners = %v, want %v", b.Code, got, wantKeys)
			}
		}
	})
}
