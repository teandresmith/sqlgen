package tests

// A has-one edge holds its FK on the target, so the LEFT JOIN's
// parent side is the parent's own PK. owners spells it owner_key and
// owner_profiles spells its own key id; joining on the target's PK name read
// "o.id", a column owners does not have, and every read that selected
// OwnerProfile failed with "no such column: o.id".

import (
	"context"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

func TestO2OJoin_HasOneDifferingPKNames(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	// Two owners, so a join that matched the wrong parent row would hand one
	// owner the other's profile.
	bios := map[int64]string{}
	var keys []int64
	for _, name := range []string{"pkname-a", "pkname-b"} {
		o, err := client.Owners().Create(ctx, &models.CreateOwnerInput{Name: name})
		if err != nil {
			t.Fatalf("Create owner %s: %v", name, err)
		}
		t.Cleanup(func() { _ = client.Owners().HardDelete(ctx, o.OwnerKey) })
		bio := name + "-bio"
		p, err := client.OwnerProfiles().Create(ctx, &models.CreateOwnerProfileInput{OwnerID: o.OwnerKey, Bio: omittable.Set(&bio)})
		if err != nil {
			t.Fatalf("Create owner profile %s: %v", name, err)
		}
		// Profiles first: the FK constraint holds the owner while one exists.
		t.Cleanup(func() { _ = client.OwnerProfiles().HardDelete(ctx, p.ID) })
		bios[o.OwnerKey] = bio
		keys = append(keys, o.OwnerKey)
	}

	for _, key := range keys {
		got, err := client.Owners().Get(ctx, key, func(o *models.CallOptions[models.OwnerFieldOptions]) {
			o.FieldOptions = &models.OwnerFieldOptions{
				OwnerKey: true, Name: true,
				OwnerProfile: &models.OwnerProfileFieldOptions{ID: true, Bio: true},
			}
		})
		if err != nil {
			t.Fatalf("Get owner %d with OwnerProfile: %v", key, err)
		}
		if got.OwnerProfile == nil || got.OwnerProfile.Bio == nil {
			t.Fatalf("owner %d: OwnerProfile not loaded: %+v", key, got.OwnerProfile)
		}
		if *got.OwnerProfile.Bio != bios[key] {
			t.Errorf("owner %d: OwnerProfile.Bio = %q, want %q", key, *got.OwnerProfile.Bio, bios[key])
		}
	}
}
