package tests

import (
	"errors"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// PRD §9.4c — no generated method may panic on a nil input pointer, and none
// may silently do the wrong thing. Reads normalize nil to the zero-value input;
// writes return ErrNilInput rather than writing a zero-valued row.

// TestNilInput_ReadsAndWrites covers every nil-able method on the entity
// client. Each case asserts the documented outcome; a panic fails the subtest
// outright, since the executor's recovery hook turns one into an error whose
// message names the nil dereference.
func TestNilInput_ReadsAndWrites(t *testing.T) {
	truncateAll(t)
	c := testClient
	seeded, err := c.Users().Create(ctx(), &models.CreateUserInput{
		Email: "nil-input@example.com", Name: "Nil Input", PasswordHash: omittable.Set("h"),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name    string
		call    func() error
		wantErr error // nil means "must succeed"
	}{
		// Reads — nil is the zero-value input.
		{"GetMany", func() error { _, err := c.Users().GetMany(ctx(), nil); return err }, nil},
		{"view GetMany", func() error { _, err := c.CategoryPriceTotal().GetMany(ctx(), nil); return err }, nil},
		// A tenanted view reaches its tenant resolver instead of panicking —
		// the guard removes the nil deref, not the §29 scoping that follows it.
		{"tenanted view GetMany", func() error {
			_, err := c.WorkspaceNoteSummary().GetMany(ctx(), nil)
			return err
		}, tenancy.ErrMissing},
		{"Count", func() error { _, err := c.Users().Count(ctx(), nil); return err }, nil},
		{"ExistsWhere", func() error { _, err := c.Users().ExistsWhere(ctx(), nil); return err }, nil},
		// Writes — nil is a caller error, never a zero-valued row.
		{"Create", func() error { _, err := c.Users().Create(ctx(), nil); return err }, models.ErrNilInput},
		{"Update", func() error { _, err := c.Users().Update(ctx(), seeded.ID, nil); return err }, models.ErrNilInput},
		{"Upsert", func() error {
			_, err := c.Users().Upsert(ctx(), nil, models.UserConflictEmail)
			return err
		}, models.ErrNilInput},
		// Bulk writes: a nil *slice* is no rows, but a nil *element* is the same
		// caller error a nil single-row input is — skipping it would silently
		// drop a row the caller asked to write (§9.4c).
		{"CreateMany nil slice", func() error { _, err := c.Users().CreateMany(ctx(), nil); return err }, nil},
		{"UpdateMany nil slice", func() error { _, err := c.Users().UpdateMany(ctx(), nil); return err }, nil},
		{"CreateMany nil element", func() error {
			_, err := c.Users().CreateMany(ctx(), []*models.CreateUserInput{nil})
			return err
		}, models.ErrNilInput},
		{"UpdateMany zero item", func() error {
			// Update<T>Item.Input is a pointer, so a zero-valued item carries a
			// nil one — a shape that compiles naturally.
			_, err := c.Users().UpdateMany(ctx(), []models.UpdateUserItem{{ID: seeded.ID}})
			return err
		}, models.ErrNilInput},
		// UpdateWhere takes both, and checks the input first (§9.4c).
		{"UpdateWhere nil input", func() error { _, err := c.Users().UpdateWhere(ctx(), nil, nil); return err }, models.ErrNilInput},
		{"UpdateWhere nil filter", func() error {
			_, err := c.Users().UpdateWhere(ctx(), nil, &models.UpdateUserInput{})
			return err
		}, models.ErrEmptyFilter},
		// A valid filter with a nil input must not reach the executor — this is
		// the combination a both-nil probe masks, since the filter guard fires
		// first and hides the input dereference behind it.
		{"UpdateWhere valid filter, nil input", func() error {
			name := "Nil Input"
			_, err := c.Users().UpdateWhere(ctx(), &models.UserFilter{Name: &comparator.String{Eq: &name}}, nil)
			return err
		}, models.ErrNilInput},
		// *Where deletes keep their own guard.
		{"SoftDeleteWhere", func() error { _, err := c.Users().SoftDeleteWhere(ctx(), nil); return err }, models.ErrEmptyFilter},
		{"RestoreWhere", func() error { _, err := c.Users().RestoreWhere(ctx(), nil); return err }, models.ErrEmptyFilter},
		{"HardDeleteWhere", func() error { return c.Users().HardDeleteWhere(ctx(), nil) }, models.ErrEmptyFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			switch {
			case tt.wantErr == nil && err != nil:
				t.Errorf("err = %v, want nil", err)
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// The write guards run before the executor, so a nil input must not have
	// inserted the zero-valued row that normalizing to &CreateUserInput{} would.
	total, err := c.Users().Count(ctx(), nil)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 1 {
		t.Errorf("users after nil writes = %d, want 1 — a nil input wrote a row", total)
	}
}

// TestNilInput_ReadsMatchZeroValue pins the §9.4c read rule as an equivalence:
// nil and the zero-value input are the same call. Stream is the load-bearing
// case — without the guard, a nil input panics inside its handler and the
// iterator yields zero rows on seeded data, reporting the recovered panic only
// as its final error.
func TestNilInput_ReadsMatchZeroValue(t *testing.T) {
	truncateAll(t)
	c := testClient
	const seedCount = 3
	for i := range seedCount {
		if _, err := c.Users().Create(ctx(), &models.CreateUserInput{
			Email: string(rune('a'+i)) + "@nil-input.example.com", Name: "Nil Input",
			PasswordHash: omittable.Set("h"),
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	nilRows, err := c.Users().GetMany(ctx(), nil)
	if err != nil {
		t.Fatalf("GetMany(nil): %v", err)
	}
	zeroRows, err := c.Users().GetMany(ctx(), &models.GetUsersInput{})
	if err != nil {
		t.Fatalf("GetMany(&GetUsersInput{}): %v", err)
	}
	if len(nilRows) != seedCount || len(zeroRows) != seedCount {
		t.Errorf("GetMany rows: nil=%d zero-value=%d, want %d each", len(nilRows), len(zeroRows), seedCount)
	}

	count := func(t *testing.T, label string, in *models.StreamUsersInput) int {
		t.Helper()
		n := 0
		for _, err := range c.Users().Stream(ctx(), in) {
			if err != nil {
				t.Fatalf("Stream(%s): %v", label, err)
			}
			n++
		}
		return n
	}
	nilStream := count(t, "nil", nil)
	zeroStream := count(t, "&StreamUsersInput{}", &models.StreamUsersInput{})
	if nilStream != seedCount || zeroStream != seedCount {
		t.Errorf("Stream rows: nil=%d zero-value=%d, want %d each — a swallowed panic reads as an empty table",
			nilStream, zeroStream, seedCount)
	}
}
