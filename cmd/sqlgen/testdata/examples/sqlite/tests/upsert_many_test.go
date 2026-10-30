package tests

// UpsertMany on SQLite (PRD §9.2, §9.5, §25.1).
//
// SQLite is the third leg of the two measurements Ticket Db turned into
// decisions (companion §6.1, §6.2):
//
//   - §6.1: like MySQL and unlike PostgreSQL, SQLite accepts an in-statement
//     duplicate conflict key on the DO UPDATE shape and keeps the last row.
//     UpsertMany's dedupe is what makes one spelling behave the same on all
//     three rather than erroring on one of them.
//   - §6.2: like PostgreSQL, DO NOTHING + RETURNING yields only the rows
//     actually inserted, so the batched form cannot source its keys there.
//
// SQLite is also where the bind-parameter ceiling is tightest (32766, §2.6),
// which is why the batching is at generation.batch_size rather than unbounded.

import (
	"context"
	"strings"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// --- §6.1: SQLite is the accepting side ---

// The raw statement PostgreSQL rejects. SQLite takes it and keeps the last row,
// which is the behavior UpsertMany's last-wins dedupe reproduces everywhere.
func TestUpsertMany_InStatementDuplicate_RawAccepted(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM categories WHERE name = 'RawDupCat'`)
	})

	if _, err := testDB.ExecContext(ctx,
		`INSERT INTO categories (name, description) VALUES (?, ?), (?, ?)
		 ON CONFLICT (name) DO UPDATE SET description = excluded.description`,
		"RawDupCat", "first", "RawDupCat", "second"); err != nil {
		t.Fatalf("raw duplicate ON CONFLICT DO UPDATE: %v — SQLite is expected to accept it (companion §6.1)", err)
	}

	var description *string
	if err := testDB.QueryRowContext(ctx,
		`SELECT description FROM categories WHERE name = ?`, "RawDupCat").Scan(&description); err != nil {
		t.Fatalf("reading RawDupCat: %v", err)
	}
	if description == nil || *description != "second" {
		t.Errorf("raw duplicate left description = %v, want %q (last row wins)", description, "second")
	}
}

// --- §6.2: DO NOTHING returns only the inserted rows ---

func TestUpsertMany_DoNothing_RawReturningIsShort(t *testing.T) {
	ctx := context.Background()
	userID, cats := seedUpsertManyLinks(ctx, t, "RawDoNothing", 3)

	if _, err := testDB.ExecContext(ctx,
		`INSERT INTO user_categories (user_id, category_id) VALUES (?, ?)`,
		userID, cats[0]); err != nil {
		t.Fatalf("seed existing link: %v", err)
	}

	rows, err := testDB.QueryContext(ctx,
		`INSERT INTO user_categories (user_id, category_id)
		 VALUES (?, ?), (?, ?), (?, ?)
		 ON CONFLICT (user_id, category_id) DO NOTHING
		 RETURNING category_id`,
		userID, cats[0], userID, cats[1], userID, cats[2])
	if err != nil {
		t.Fatalf("raw DO NOTHING insert: %v", err)
	}
	returned := 0
	for rows.Next() {
		returned++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatalf("iterating RETURNING rows: %v", err)
	}

	if returned != 2 {
		t.Errorf("DO NOTHING RETURNING yielded %d rows for 3 values, want 2 (companion §6.2)", returned)
	}
}

// --- The pure link table ---

func TestUpsertMany_LinkTableIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	userID, cats := seedUpsertManyLinks(ctx, t, "LinkIdempotent", 2)

	inputs := make([]*models.CreateUserCategoryInput, 0, len(cats))
	for _, catID := range cats {
		inputs = append(inputs, &models.CreateUserCategoryInput{UserID: userID, CategoryID: catID})
	}

	first, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (insert): %v", err)
	}
	if len(first) != len(inputs) {
		t.Fatalf("UpsertMany (insert) returned %d entities, want %d", len(first), len(inputs))
	}

	// Every row conflicts now, so the whole batch takes the DO NOTHING branch
	// and RETURNING would yield nothing at all.
	second, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (idempotent repeat): %v", err)
	}
	if len(second) != len(inputs) {
		t.Errorf("UpsertMany (idempotent repeat) returned %d entities, want %d — the DO NOTHING branch must not shorten the result (PRD §9.5)", len(second), len(inputs))
	}

	var linkCount int
	if err := testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = ?`, userID).Scan(&linkCount); err != nil {
		t.Fatalf("counting links: %v", err)
	}
	if linkCount != len(inputs) {
		t.Errorf("link count after two UpsertMany calls = %d, want %d (idempotent)", linkCount, len(inputs))
	}
}

// --- The database-generated key path ---

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

// seedUpsertManyLinks creates a user and n categories and returns their ids.
func seedUpsertManyLinks(ctx context.Context, t *testing.T, name string, n int) (int64, []int64) {
	t.Helper()
	client := newClient()

	user, err := client.Users().Create(ctx, &models.CreateUserInput{
		Email: name + "@example.com",
		Name:  name,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.Users().HardDelete(ctx, user.ID) })
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(ctx, `DELETE FROM user_categories WHERE user_id = ?`, user.ID)
	})

	cats := make([]int64, 0, n)
	for i := range n {
		cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
			Name: name + "-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("Create category %d: %v", i, err)
		}
		t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
		cats = append(cats, cat.ID)
	}
	return user.ID, cats
}
