package tests

// UpsertMany on MySQL (PRD §9.2, §9.5, §9.7, §25.1).
//
// MySQL forced Ticket Db's third decision, and it is the only dialect that can
// pin it: §6.3 measured that LAST_INSERT_ID() after a batch that mixed inserts
// with conflicts names the first *newly inserted* row, not the batch's first
// row — so CreateMany's `firstID + i` fabrication (create.go.tmpl) is unsound
// for an upsert. TestUpsertMany_LastInsertIDAfterMixedBatch_RawIsUnusable
// proves the server still behaves that way, and
// TestUpsertMany_DBGeneratedPK_ResolvesExistingIDs proves UpsertMany does not
// rely on it — a database-generated key is read back by the conflict target.
//
// MySQL is also the accepting side of §6.1: it takes an in-statement duplicate
// and keeps the last row, where PostgreSQL rejects the statement outright. The
// dedupe is what makes one spelling mean the same thing on both.

import (
	"context"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// --- §6.3: LAST_INSERT_ID after a mixed batch ---

// The raw shape a naive batched upsert would use to resolve keys. The claim
// under test is the one that makes it unusable: the ids MySQL actually assigned
// are not `LAST_INSERT_ID() + i` over the batch's rows.
func TestUpsertMany_LastInsertIDAfterMixedBatch_RawIsUnusable(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name LIKE 'RawLII-%'`)
	})

	// Seed one row so the batch below mixes a conflict with two inserts.
	if _, err := testDB.ExecContext(ctx,
		`INSERT INTO categories (name, description) VALUES (?, ?)`, "RawLII-a", "before"); err != nil {
		t.Fatalf("seeding RawLII-a: %v", err)
	}
	var seededID int64
	if err := testDB.QueryRowContext(ctx,
		`SELECT id FROM categories WHERE name = ?`, "RawLII-a").Scan(&seededID); err != nil {
		t.Fatalf("reading seeded id: %v", err)
	}

	if _, err := testDB.ExecContext(ctx,
		`INSERT INTO categories (name, description) VALUES (?, ?), (?, ?), (?, ?)
		 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), description = VALUES(description)`,
		"RawLII-a", "after", "RawLII-b", "b", "RawLII-c", "c"); err != nil {
		t.Fatalf("raw mixed batch: %v", err)
	}

	var reported int64
	if err := testDB.QueryRowContext(ctx, `SELECT LAST_INSERT_ID()`).Scan(&reported); err != nil {
		t.Fatalf("reading LAST_INSERT_ID: %v", err)
	}

	actual := make(map[string]int64, 3)
	for _, name := range []string{"RawLII-a", "RawLII-b", "RawLII-c"} {
		var id int64
		if err := testDB.QueryRowContext(ctx, `SELECT id FROM categories WHERE name = ?`, name).Scan(&id); err != nil {
			t.Fatalf("reading id for %s: %v", name, err)
		}
		actual[name] = id
	}

	// The fabrication CreateMany uses for a plain multi-row INSERT: ids are
	// consecutive from the reported one, in the batch's row order.
	fabricated := map[string]int64{
		"RawLII-a": reported,
		"RawLII-b": reported + 1,
		"RawLII-c": reported + 2,
	}
	if fabricated["RawLII-a"] == actual["RawLII-a"] &&
		fabricated["RawLII-b"] == actual["RawLII-b"] &&
		fabricated["RawLII-c"] == actual["RawLII-c"] {
		t.Fatalf("firstID+i reproduced the real ids (LAST_INSERT_ID=%d, actual=%v) — companion §6.3 measured that it does not, and UpsertMany's read-back exists because of it",
			reported, actual)
	}
	if reported == seededID {
		t.Errorf("LAST_INSERT_ID = %d, the batch's first row; §6.3 measured it naming the first *newly inserted* row instead", reported)
	}
	t.Logf("LAST_INSERT_ID=%d, seeded=%d, actual ids=%v", reported, seededID, actual)
}

// UpsertMany does not consult LAST_INSERT_ID at all: an AUTO_INCREMENT key is
// read back by the conflict target after each sub-batch, so a conflicting row
// resolves to the id it already had.
func TestUpsertMany_DBGeneratedPK_ResolvesExistingIDs(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name LIKE 'GenPKCat-%'`)
	})

	seeded, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name:        "GenPKCat-existing",
		Description: omittable.Set(new("before")),
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}

	got, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "GenPKCat-existing", Description: omittable.Set(new("after"))},
		{Name: "GenPKCat-new", Description: omittable.Set(new("fresh"))},
	}, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("UpsertMany on an AUTO_INCREMENT key: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("UpsertMany returned %d entities, want 2", len(got))
	}

	byName := make(map[string]*models.Category, len(got))
	for _, c := range got {
		byName[c.Name] = c
	}
	existing, ok := byName["GenPKCat-existing"]
	if !ok {
		t.Fatalf("result has no GenPKCat-existing; got %v", byName)
	}
	if existing.ID != seeded.ID {
		t.Errorf("conflicting row resolved to id %d, want the existing row's %d", existing.ID, seeded.ID)
	}
	if existing.Description == nil || *existing.Description != "after" {
		t.Errorf("conflicting row description = %v, want %q", existing.Description, "after")
	}
	fresh, ok := byName["GenPKCat-new"]
	if !ok || fresh.ID == 0 {
		t.Fatalf("newly inserted row missing or carries a zero id: %v", byName)
	}
	if fresh.ID == existing.ID {
		t.Errorf("both rows resolved to id %d — the read-back is not distinguishing them", fresh.ID)
	}
}

// --- §6.1: MySQL is the accepting side ---

// MySQL takes the in-statement duplicate PostgreSQL rejects and keeps the last
// row. UpsertMany dedupes first, so the outcome is the same one spelled the
// same way on every dialect.
func TestUpsertMany_DedupesLastWins(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name IN ('DedupeCat', 'OtherCat')`)
	})

	got, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "DedupeCat", Description: omittable.Set(new("first"))},
		{Name: "OtherCat", Description: omittable.Set(new("other"))},
		{Name: "DedupeCat", Description: omittable.Set(new("last"))},
	}, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("UpsertMany with an in-statement duplicate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("UpsertMany returned %d entities, want 2 (three inputs, one duplicate pair)", len(got))
	}

	var description *string
	if err := testDB.QueryRowContext(ctx,
		`SELECT description FROM categories WHERE name = ?`, "DedupeCat").Scan(&description); err != nil {
		t.Fatalf("reading DedupeCat: %v", err)
	}
	if description == nil || *description != "last" {
		t.Errorf("DedupeCat description = %v, want %q (last occurrence wins, as MySQL does natively)", description, "last")
	}
}

// --- The pure link table ---

// user_categories' key is its whole column set, so the conflict target covers
// every inserted column and MySQL emits the degenerate self-assignment that
// stands in for DO NOTHING. Repeating the batch changes nothing and errors not
// at all, and the result stays one entity per input.
func TestUpsertMany_LinkTableIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Name:    "LinkIdempotent",
		Email:   "link-idempotent@example.com",
		Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	var inputs []*models.CreateUserCategoryInput
	for _, suffix := range []string{"-a", "-b"} {
		cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "LinkIdempotent" + suffix})
		if err != nil {
			t.Fatalf("Create category %s: %v", suffix, err)
		}
		t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
		inputs = append(inputs, &models.CreateUserCategoryInput{UserID: user.ID, CategoryID: cat.ID})
	}
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM user_categories WHERE user_id = ?`, user.ID)
	})

	first, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (insert): %v", err)
	}
	if len(first) != len(inputs) {
		t.Fatalf("UpsertMany (insert) returned %d entities, want %d", len(first), len(inputs))
	}

	second, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (idempotent repeat): %v", err)
	}
	if len(second) != len(inputs) {
		t.Errorf("UpsertMany (idempotent repeat) returned %d entities, want %d", len(second), len(inputs))
	}

	var linkCount int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = ?`, user.ID).Scan(&linkCount); err != nil {
		t.Fatalf("counting links: %v", err)
	}
	if linkCount != len(inputs) {
		t.Errorf("link count after two UpsertMany calls = %d, want %d (idempotent)", linkCount, len(inputs))
	}
}

// PRD §9.4c: a nil element is a caller error, not a row to skip.
func TestUpsertMany_NilElement(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	if _, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "NilElementCat"},
		nil,
	}, models.CategoryConflictName); err == nil {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name = 'NilElementCat'`)
		t.Fatal("UpsertMany with a nil element = nil error, want ErrNilInput")
	}

	var count int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM categories WHERE name = ?`, "NilElementCat").Scan(&count); err != nil {
		t.Fatalf("counting categories: %v", err)
	}
	if count != 0 {
		t.Errorf("UpsertMany rejected for a nil element still wrote %d rows, want 0", count)
	}
}

// A conflict target the input cannot supply leaves the written rows unnameable.
// categories.id is AUTO_INCREMENT and absent from CreateCategoryInput entirely,
// so ConflictPK names a column no statement here carries; the operation refuses
// before writing rather than guessing a key.
func TestUpsertMany_UnsuppliedConflictTargetRefused(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	_, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "UnsuppliedTargetCat"},
	}, models.CategoryConflictPK)
	if err == nil {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name = 'UnsuppliedTargetCat'`)
		t.Fatal("UpsertMany with an unsupplied conflict target succeeded, want a refusal before any write")
	}
	if !strings.Contains(err.Error(), "does not supply") && !strings.Contains(err.Error(), "cannot be resolved") {
		t.Errorf("UpsertMany error = %v, want one naming the unsupplied conflict target", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM categories WHERE name = ?`, "UnsuppliedTargetCat").Scan(&count); err != nil {
		t.Fatalf("counting categories: %v", err)
	}
	if count != 0 {
		t.Errorf("refused UpsertMany wrote %d rows, want 0", count)
	}
}

// --- MySQL: a caller-known COMPOSITE key, on a non-covering target ---

// user_categories has a caller-known composite key (user_id, category_id) and a
// UNIQUE (user_id, slot) that does not cover it. Upserting on the slot target
// conflicts on columns that are not the key, so the conflicting row keeps the
// category_id it already had — the key columns are excluded from the update
// half of the clause. Taking the key from the input there names a row that does
// not exist.
//
// This is the only runtime coverage of two things at once: the composite
// read-back (resolveUpsertManyRows returning []UserCategoryPK, generalized for
// exactly this), and MySQL's own tuple-IN, which degenerates through
// buildExpandedOR where PostgreSQL and SQLite emit a row constructor. The
// postgres pin covers a single-column key on a single-column target, so neither
// of those paths is reached there.
//
// Failing-first: against the pre-fix template this returns 1 entity, not 2.
func TestUpsertMany_CallerKnownCompositeKey_NonCoveringTargetResolvesExistingKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Name:    "ReadbackComposite",
		Email:   "readback-composite@example.com",
		Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	var cats []int32
	for _, suffix := range []string{"-seeded", "-wanted", "-fresh"} {
		cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ReadbackComposite" + suffix})
		if err != nil {
			t.Fatalf("Create category %s: %v", suffix, err)
		}
		t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
		cats = append(cats, cat.ID)
	}
	seededCat, wantedCat, freshCat := cats[0], cats[1], cats[2]

	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM user_categories WHERE user_id = ?`, user.ID)
	})

	// Slot 1 is taken by seededCat.
	if _, err := client.UserCategories().UpsertMany(ctx, []*models.CreateUserCategoryInput{
		{UserID: user.ID, CategoryID: seededCat, Slot: omittable.Set(new(int32(1)))},
	}, models.UserCategoryConflictPK); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	got, err := client.UserCategories().UpsertMany(ctx, []*models.CreateUserCategoryInput{
		// Conflicts on (user_id, slot=1). The stored row keeps category_id =
		// seededCat; wantedCat is never written.
		{UserID: user.ID, CategoryID: wantedCat, Slot: omittable.Set(new(int32(1)))},
		// No conflict — inserted as given.
		{UserID: user.ID, CategoryID: freshCat, Slot: omittable.Set(new(int32(2)))},
	}, models.UserCategoryConflictUserIDSlot)
	if err != nil {
		t.Fatalf("UpsertMany on a non-covering composite target: %v", err)
	}

	// PRD §9.5 decision 2: one entity per deduped input, on every target.
	if len(got) != 2 {
		t.Fatalf("UpsertMany returned %d entities, want 2 — the conflicting row must be resolved by its own composite key, not by the one the input carried (PRD §9.5)", len(got))
	}

	byCat := make(map[int32]*models.UserCategory, len(got))
	for _, uc := range got {
		byCat[uc.CategoryID] = uc
	}
	if _, ok := byCat[wantedCat]; ok {
		t.Errorf("result carries category_id %d, which was never written: the conflicting row keeps its own key", wantedCat)
	}
	if _, ok := byCat[seededCat]; !ok {
		t.Errorf("result has no category_id %d (the seeded row); the composite read-back must resolve it", seededCat)
	}
	if _, ok := byCat[freshCat]; !ok {
		t.Errorf("result has no category_id %d; the non-conflicting row must still be inserted and returned", freshCat)
	}

	// The write landed on the seeded row rather than creating a third.
	var rows int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = ?`, user.ID).Scan(&rows); err != nil {
		t.Fatalf("counting links: %v", err)
	}
	if rows != 2 {
		t.Errorf("user_categories for the user = %d, want 2 (one conflicted, one inserted)", rows)
	}
}

// A NULL in a conflict column cannot be matched back, so on a target whose keys
// must be read back the call refuses before writing anything rather than
// returning a key it guessed (PRD §9.7). slot is nullable, which makes this
// reachable on the same table.
func TestUpsertMany_NonCoveringTarget_IndefiniteConflictValueRefused(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Name:    "ReadbackIndefinite",
		Email:   "readback-indefinite@example.com",
		Balance: 0,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })

	cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: "ReadbackIndefinite-a"})
	if err != nil {
		t.Fatalf("Create category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM user_categories WHERE user_id = ?`, user.ID)
	})

	// slot left unset: the conflict target (user_id, slot) has no definite value.
	_, err = client.UserCategories().UpsertMany(ctx, []*models.CreateUserCategoryInput{
		{UserID: user.ID, CategoryID: cat.ID},
	}, models.UserCategoryConflictUserIDSlot)
	if err == nil {
		t.Fatal("UpsertMany with an indefinite conflict value succeeded, want a refusal before any write")
	}
	if !strings.Contains(err.Error(), "cannot be resolved") {
		t.Errorf("UpsertMany error = %v, want one naming the unresolvable key", err)
	}

	var rows int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = ?`, user.ID).Scan(&rows); err != nil {
		t.Fatalf("counting links: %v", err)
	}
	if rows != 0 {
		t.Errorf("refused UpsertMany wrote %d rows, want 0", rows)
	}
}
