package tests

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// TestO2OJoinMiss_ReadsAsNil pins that an O2O LEFT JOIN miss reads as nil on
// pgx, and it is the arm that matters most even though the defect is
// driver-independent.
//
// The joined read tells a real row from a LEFT JOIN miss by testing the
// target's PK against its zero value, which happens *after* the scan — but a
// miss returns NULL for every one of the target's columns, and a NOT NULL one
// resolves to a Go type that refuses NULL ("cannot scan NULL into *[16]byte"
// here, "converting NULL to int64 is unsupported" on database/sql). Every O2O
// edge therefore errored for a parent with no matching row.
//
// The fix wraps those destinations in database.NullScan, which changes how pgx
// resolves the scan plan — a sql.Scanner destination leaves pgx's native codec
// path — so the hit case below is as load-bearing as the miss. `profiles`
// covers both sides of that choice in one row: a NOT NULL uuid and timestamptz
// that are wrapped, and a nullable jsonb and int4[] that are not.
func TestO2OJoinMiss_ReadsAsNil(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	newUser := func(email, name string) *models.PublicUser {
		t.Helper()
		u, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{Email: email, Name: name})
		if err != nil {
			t.Fatalf("Create user %q: %v", email, err)
		}
		t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, u.ID) })
		return u
	}

	load := func(t *testing.T, u *models.PublicUser) *models.PublicUser {
		t.Helper()
		got, err := client.PublicUsers().Get(ctx, u.ID, func(o *models.CallOptions[models.PublicUserFieldOptions]) {
			o.FieldOptions = &models.PublicUserFieldOptions{
				ID:   true,
				Name: true,
				Profile: &models.ProfileFieldOptions{
					ID:     true,
					UserID: true,
					Bio:    true,
					Scores: true,
				},
			}
		})
		if err != nil {
			t.Fatalf("Get user with the o2o edge selected: %v", err)
		}
		return got
	}

	t.Run("miss reads as nil", func(t *testing.T) {
		bare := newUser("o2omiss-bare@example.com", "O2OMissBare")
		if got := load(t, bare); got.Profile != nil {
			t.Errorf("Profile = %+v, want nil", got.Profile)
		}
	})

	t.Run("hit still scans every column shape", func(t *testing.T) {
		owner := newUser("o2omiss-hit@example.com", "O2OMissHit")
		profile, err := client.Profiles().Create(ctx, &models.CreateProfileInput{
			UserID: owner.ID,
			Bio:    omittable.Set(new("Developer")),
			Scores: omittable.Set([]int32{90, 85}),
		})
		if err != nil {
			t.Fatalf("Create profile: %v", err)
		}
		t.Cleanup(func() { _ = client.Profiles().HardDelete(ctx, profile.ID) })

		got := load(t, owner)
		if got.Profile == nil {
			t.Fatal("Profile = nil, want the seeded profile")
		}
		if got.Profile.ID != profile.ID {
			t.Errorf("Profile.ID = %q, want %q", got.Profile.ID, profile.ID)
		}
		// The array is read through pgx's native codec; wrapping it would have
		// dropped it onto convertAssign and failed here, not on the miss.
		if diff := cmp.Diff([]int32{90, 85}, got.Profile.Scores); diff != "" {
			t.Errorf("Profile.Scores mismatch (-want +got):\n%s", diff)
		}
	})
}
