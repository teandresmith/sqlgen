package tests

// A many-to-many loader scans the junction's keys into their own Go types, so
// a BLOB key matches on both sides of the loader's maps.

import (
	"context"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestM2MLoad_BinaryKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	keys := [][]byte{{0x00, 0xff, 0x10}, {0x41, 0x42}}
	for i, id := range keys {
		if _, err := client.BinaryKeys().Create(ctx, &models.CreateBinaryKeyInput{ID: id, Name: []string{"bk-a", "bk-b"}[i]}); err != nil {
			t.Fatalf("Create binary key %x: %v", id, err)
		}
		t.Cleanup(func() { _ = client.BinaryKeys().HardDelete(ctx, id) })
	}
	codes := []string{"bin-key-red", "bin-key-blue", "bin-key-green"}
	for _, code := range codes {
		if _, err := client.Badges().Create(ctx, &models.CreateBadgeInput{Code: code, Label: code}); err != nil {
			t.Fatalf("Create badge %s: %v", code, err)
		}
		t.Cleanup(func() { _ = client.Badges().HardDelete(ctx, code) })
	}

	// bk-a holds red and blue, bk-b holds blue; green has no binary key.
	links := []models.CreateBinaryKeyBadgeInput{
		{BinaryKeyID: keys[0], BadgeCode: codes[0]},
		{BinaryKeyID: keys[0], BadgeCode: codes[1]},
		{BinaryKeyID: keys[1], BadgeCode: codes[1]},
	}
	for _, in := range links {
		if _, err := client.BinaryKeyBadges().Create(ctx, &in); err != nil {
			t.Fatalf("Create binary key badge: %v", err)
		}
		// Registered after the keys and badges, so it runs before them.
		t.Cleanup(func() {
			_ = client.BinaryKeyBadges().HardDelete(ctx, models.BinaryKeyBadgePK{BinaryKeyID: in.BinaryKeyID, BadgeCode: in.BadgeCode})
		})
	}

	t.Run("binary key parent", func(t *testing.T) {
		got, err := client.BinaryKeys().GetMany(ctx, &models.GetBinaryKeysInput{
			Filter: &models.BinaryKeyFilter{ID: &comparator.Opaque[[]byte]{In: keys}},
		}, func(o *models.CallOptions[models.BinaryKeyFieldOptions]) {
			o.FieldOptions = &models.BinaryKeyFieldOptions{
				Name:   true,
				Badges: &models.BadgeRelationshipOptions{FieldOptions: &models.BadgeFieldOptions{Label: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany binary keys with Badges: %v", err)
		}
		want := map[string][]string{"bk-a": {codes[1], codes[0]}, "bk-b": {codes[1]}}
		if len(got) != len(want) {
			t.Fatalf("GetMany returned %d binary keys, want %d", len(got), len(want))
		}
		for _, bk := range got {
			var badges []string
			for _, b := range bk.Badges {
				badges = append(badges, b.Code)
			}
			slices.Sort(badges)
			if !slices.Equal(badges, want[bk.Name]) {
				t.Errorf("%s: Badges = %v, want %v", bk.Name, badges, want[bk.Name])
			}
		}
	})

	t.Run("binary key target", func(t *testing.T) {
		got, err := client.Badges().GetMany(ctx, &models.GetBadgesInput{
			Filter: &models.BadgeFilter{Code: &comparator.ID{In: codes}},
		}, func(o *models.CallOptions[models.BadgeFieldOptions]) {
			o.FieldOptions = &models.BadgeFieldOptions{
				Code:       true,
				BinaryKeys: &models.BinaryKeyRelationshipOptions{FieldOptions: &models.BinaryKeyFieldOptions{Name: true}},
			}
		})
		if err != nil {
			t.Fatalf("GetMany badges with BinaryKeys: %v", err)
		}
		want := map[string][]string{codes[0]: {"bk-a"}, codes[1]: {"bk-a", "bk-b"}, codes[2]: nil}
		if len(got) != len(want) {
			t.Fatalf("GetMany returned %d badges, want %d", len(got), len(want))
		}
		for _, b := range got {
			var names []string
			for _, bk := range b.BinaryKeys {
				names = append(names, bk.Name)
			}
			slices.Sort(names)
			if !slices.Equal(names, want[b.Code]) {
				t.Errorf("%s: BinaryKeys = %v, want %v", b.Code, names, want[b.Code])
			}
		}
	})
}
