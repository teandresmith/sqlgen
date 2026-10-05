package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// slugged_rows.slug is UNIQUE with the constant DEFAULT 'x' and `read_only` on
// the API, so every insert that leaves it unset collides on the same value.
// These tests are the SQLite half of the cross-dialect upsert-collision pins;
// the mysql example carries the dialect where the outcome is not automatic.

// resetSluggedRows empties slugged_rows now and when the test ends.
func resetSluggedRows(t *testing.T) {
	t.Helper()
	wipe := func() {
		if _, err := testDB.ExecContext(context.Background(), "DELETE FROM slugged_rows"); err != nil {
			t.Fatalf("clearing slugged_rows: %v", err)
		}
	}
	wipe()
	t.Cleanup(wipe)
}

// wantUniqueViolation fails unless err is a unique ConstraintError.
func wantUniqueViolation(t *testing.T, op string, err error) {
	t.Helper()
	ce, ok := errors.AsType[*models.ConstraintError](err)
	if !ok || ce.Type != models.ConstraintUnique {
		t.Fatalf("%s error = %v, want a %q ConstraintError", op, err, models.ConstraintUnique)
	}
}

// wantSluggedRow fails unless the row with id has the given name.
func wantSluggedRow(t *testing.T, id int64, name string) {
	t.Helper()
	got, err := newClient().SluggedRows().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	if got.Name != name {
		t.Errorf("Get(%d).Name = %q, want %q", id, got.Name, name)
	}
}

// TestSluggedRows_PKTargetDoesNotFireOnAnotherUnique: an upsert on the PK
// target whose insert collides on a different unique column is a unique
// violation (2067), through the Go client and through the flat GraphQL upsert,
// and the row holding that value is untouched.
func TestSluggedRows_PKTargetDoesNotFireOnAnotherUnique(t *testing.T) {
	resetSluggedRows(t)
	ctx := context.Background()
	c := newClient().SluggedRows()

	victim, err := c.Create(ctx, &models.CreateSluggedRowInput{Name: omittable.Set("victim")})
	if err != nil {
		t.Fatalf("Create(victim): %v", err)
	}

	got, err := c.Upsert(ctx, &models.CreateSluggedRowInput{Name: omittable.Set("other")}, models.SluggedRowConflictPK)
	if err == nil {
		t.Errorf("Upsert(PK target, colliding slug) = %+v, want a unique violation", got)
	}
	wantUniqueViolation(t, "Upsert(PK target, colliding slug)", err)
	wantSluggedRow(t, victim.ID, "victim")

	resp := gqlPost(t, newGraphQLServer(t), `mutation { upsertSluggedRow(input: {name: "other"}) { id name } }`)
	if !strings.Contains(resp, `"code":"CONFLICT"`) {
		t.Errorf("upsertSluggedRow(colliding slug) = %s, want a CONFLICT error", resp)
	}
	wantSluggedRow(t, victim.ID, "victim")
}

// TestSluggedRows_ZeroUpsertIsPlainInsert pins the zero-Upsert rule end to end
// (PRD 9.5): a zero Upsert emits no conflict clause, so the second one
// collides on the default slug as a unique violation (2067).
func TestSluggedRows_ZeroUpsertIsPlainInsert(t *testing.T) {
	resetSluggedRows(t)
	ctx := context.Background()
	c := newClient().SluggedRows()

	first, err := c.Upsert(ctx, &models.CreateSluggedRowInput{}, models.SluggedRowConflictPK)
	if err != nil {
		t.Fatalf("Upsert(zero input) #1: %v", err)
	}
	if first.Name != "" || first.Slug != "x" {
		t.Errorf("Upsert(zero input) #1 = {Name:%q Slug:%q}, want the defaults {%q %q}", first.Name, first.Slug, "", "x")
	}

	second, err := c.Upsert(ctx, &models.CreateSluggedRowInput{}, models.SluggedRowConflictPK)
	if err == nil {
		t.Errorf("Upsert(zero input) #2 = %+v, want a unique violation", second)
	}
	wantUniqueViolation(t, "Upsert(zero input) #2", err)
}

// TestSluggedRows_DefaultedTargetIsPlainInsert pins the other half of the
// same rule (PRD 9.5): a unique target whose column the input leaves to its
// DEFAULT names no value the caller chose, so the upsert is a plain insert and
// the collision on the default is a unique violation, not an update of the
// row that holds it — the outcome a zero input already has.
func TestSluggedRows_DefaultedTargetIsPlainInsert(t *testing.T) {
	resetSluggedRows(t)
	ctx := context.Background()
	c := newClient().SluggedRows()

	victim, err := c.Create(ctx, &models.CreateSluggedRowInput{Name: omittable.Set("victim")})
	if err != nil {
		t.Fatalf("Create(victim): %v", err)
	}

	got, err := c.Upsert(ctx, &models.CreateSluggedRowInput{Name: omittable.Set("other")}, models.SluggedRowConflictSlug)
	if err == nil {
		t.Errorf("Upsert(slug target, slug unset) = %+v, want a unique violation", got)
	}
	wantUniqueViolation(t, "Upsert(slug target, slug unset)", err)

	row, err := c.Get(ctx, victim.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", victim.ID, err)
	}
	if row.Name != "victim" {
		t.Errorf("Get(%d).Name = %q, want %q", victim.ID, row.Name, "victim")
	}
}
