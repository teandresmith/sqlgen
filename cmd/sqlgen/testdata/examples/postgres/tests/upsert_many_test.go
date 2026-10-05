package tests

// UpsertMany on PostgreSQL (PRD §9.2, §9.5, §25.1).
//
// PostgreSQL is the dialect that forced two of Ticket Db's three decisions, so
// both are pinned here against the real server rather than against the template
// text:
//
//   - §6.1: PostgreSQL alone rejects an ON CONFLICT ... DO UPDATE that would
//     affect one row twice. TestUpsertMany_InStatementDuplicate_RawRejected
//     proves the server still does, so the dedupe is load-bearing rather than
//     defensive, and TestUpsertMany_DedupesLastWins proves UpsertMany survives
//     the same input and keeps the last occurrence.
//   - §6.2: DO NOTHING returns only the rows actually inserted, so RETURNING
//     comes back short after a partial conflict.
//     TestUpsertMany_DoNothing_RawReturningIsShort proves that, and
//     TestUpsertMany_LinkTableIdempotent proves UpsertMany returns one entity
//     per input anyway — it re-fetches by key instead of reading RETURNING.
//
// §6.3 (MySQL's LAST_INSERT_ID after a mixed batch) has no PostgreSQL analogue
// and is pinned in the mysql example.

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// seedUpsertManyLinks creates a user and two categories and returns their ids.
func seedUpsertManyLinks(ctx context.Context, t *testing.T, name string) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	client := newClient()

	user, err := client.PublicUsers().Create(ctx, &models.CreatePublicUserInput{
		Email: name + "@example.com",
		Name:  name,
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	t.Cleanup(func() { _ = client.PublicUsers().HardDelete(ctx, user.ID) })

	cats := make([]uuid.UUID, 0, 2)
	for _, suffix := range []string{"-a", "-b"} {
		cat, err := client.Categories().Create(ctx, &models.CreateCategoryInput{Name: name + suffix})
		if err != nil {
			t.Fatalf("Create category %s: %v", suffix, err)
		}
		t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, cat.ID) })
		cats = append(cats, cat.ID)
	}
	return user.ID, cats
}

// --- §6.1: the measurement the dedupe exists for ---

// The raw statement UpsertMany would issue without its dedupe pass. PostgreSQL
// rejects it: "ON CONFLICT DO UPDATE command cannot affect row a second time".
// If this ever stops failing, the dedupe has become optional and this file's
// premise needs rewriting rather than its assertions relaxing.
func TestUpsertMany_InStatementDuplicate_RawRejected(t *testing.T) {
	ctx := context.Background()

	_, err := testPool.Exec(ctx,
		`INSERT INTO categories (name, description) VALUES ($1, $2), ($3, $4)
		 ON CONFLICT (name) DO UPDATE SET description = excluded.description`,
		"RawDupCat", "first", "RawDupCat", "second")
	if err == nil {
		_, _ = testPool.Exec(ctx, `DELETE FROM categories WHERE name = $1`, "RawDupCat")
		t.Fatal("raw duplicate ON CONFLICT DO UPDATE succeeded; PostgreSQL is expected to reject it (companion §6.1)")
	}
	if !strings.Contains(err.Error(), "affect row a second time") {
		t.Errorf("raw duplicate rejected with %v, want the 'affect row a second time' cardinality error", err)
	}
}

// The same input through UpsertMany succeeds on all three dialects, and the
// last occurrence is the one that lands — matching what MySQL and SQLite do
// natively with the un-deduped statement.
func TestUpsertMany_DedupesLastWins(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	inputs := []*models.CreateCategoryInput{
		{Name: "DedupeCat", Description: omittable.Set(new("first"))},
		{Name: "OtherCat", Description: omittable.Set(new("other"))},
		{Name: "DedupeCat", Description: omittable.Set(new("last"))},
	}

	got, err := client.Categories().UpsertMany(ctx, inputs, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("UpsertMany with an in-statement duplicate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM categories WHERE name = ANY($1)`, []string{"DedupeCat", "OtherCat"})
	})

	if len(got) != 2 {
		t.Fatalf("UpsertMany returned %d entities, want 2 (three inputs, one duplicate pair)", len(got))
	}

	byName := make(map[string]*models.Category, len(got))
	for _, c := range got {
		byName[c.Name] = c
	}
	dedup, ok := byName["DedupeCat"]
	if !ok {
		t.Fatalf("UpsertMany result has no DedupeCat; got %v", byName)
	}
	if dedup.Description == nil || *dedup.Description != "last" {
		t.Errorf("DedupeCat description = %v, want %q (last occurrence wins)", dedup.Description, "last")
	}
}

// --- §6.2: DO NOTHING returns only the inserted rows ---

// The raw statement, on the shape that produces DO NOTHING: a pure link table
// whose conflict target covers every column it inserts. One of the three rows
// already exists, so RETURNING yields two.
func TestUpsertMany_DoNothing_RawReturningIsShort(t *testing.T) {
	ctx := context.Background()
	userID, cats := seedUpsertManyLinks(ctx, t, "RawDoNothing")

	third, err := newClient().Categories().Create(ctx, &models.CreateCategoryInput{Name: "RawDoNothing-c"})
	if err != nil {
		t.Fatalf("Create third category: %v", err)
	}
	t.Cleanup(func() { _ = newClient().Categories().HardDelete(ctx, third.ID) })

	if _, err := testPool.Exec(ctx,
		`INSERT INTO user_categories (user_id, category_id) VALUES ($1, $2)`,
		userID, cats[0]); err != nil {
		t.Fatalf("seed existing link: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM user_categories WHERE user_id = $1`, userID)
	})

	rows, err := testPool.Query(ctx,
		`INSERT INTO user_categories (user_id, category_id)
		 VALUES ($1, $2), ($3, $4), ($5, $6)
		 ON CONFLICT (user_id, category_id) DO NOTHING
		 RETURNING category_id`,
		userID, cats[0], userID, cats[1], userID, third.ID)
	if err != nil {
		t.Fatalf("raw DO NOTHING insert: %v", err)
	}
	returned := 0
	for rows.Next() {
		returned++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating RETURNING rows: %v", err)
	}

	if returned != 2 {
		t.Errorf("DO NOTHING RETURNING yielded %d rows for 3 values, want 2 (companion §6.2)", returned)
	}
}

// UpsertMany on the same shape returns one entity per input regardless, because
// it never reads its keys out of RETURNING: a composite key is caller-known, so
// the entities come from a re-fetch on the keys the inputs carry.
func TestUpsertMany_LinkTableIdempotent(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	userID, cats := seedUpsertManyLinks(ctx, t, "LinkIdempotent")

	inputs := make([]*models.CreateUserCategoryInput, 0, len(cats))
	for _, catID := range cats {
		inputs = append(inputs, &models.CreateUserCategoryInput{UserID: userID, CategoryID: catID})
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM user_categories WHERE user_id = $1`, userID)
	})

	first, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (insert): %v", err)
	}
	if len(first) != len(inputs) {
		t.Fatalf("UpsertMany (insert) returned %d entities, want %d", len(first), len(inputs))
	}

	// Every row now conflicts, so the whole batch takes the DO NOTHING branch
	// and RETURNING would yield nothing at all.
	second, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK)
	if err != nil {
		t.Fatalf("UpsertMany (idempotent repeat): %v", err)
	}
	if len(second) != len(inputs) {
		t.Errorf("UpsertMany (idempotent repeat) returned %d entities, want %d — the DO NOTHING branch must not shorten the result (PRD §9.5)", len(second), len(inputs))
	}

	var linkCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM user_categories WHERE user_id = $1`, userID).Scan(&linkCount); err != nil {
		t.Fatalf("counting links: %v", err)
	}
	if linkCount != len(inputs) {
		t.Errorf("link count after two UpsertMany calls = %d, want %d (idempotent)", linkCount, len(inputs))
	}
}

// --- §25.1: the statement count ---

// A pure link table's key is caller-known, so nothing is read back: the write
// bound is ceil(N/batchSize) INSERTs, and the only other statement is the
// terminal re-fetch every entity-returning mutation issues.
func TestUpsertMany_LinkTableStatementCount(t *testing.T) {
	ctx := context.Background()
	userID, cats := seedUpsertManyLinks(ctx, t, "LinkStmtCount")

	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	inputs := make([]*models.CreateUserCategoryInput, 0, len(cats))
	for _, catID := range cats {
		inputs = append(inputs, &models.CreateUserCategoryInput{UserID: userID, CategoryID: catID})
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM user_categories WHERE user_id = $1`, userID)
	})

	cap.reset()
	if _, err := client.UserCategories().UpsertMany(ctx, inputs, models.UserCategoryConflictPK); err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}

	sqls := cap.snapshot()
	var inserts, others int
	for _, sqlStr := range sqls {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqlStr)), "INSERT ") {
			inserts++
			continue
		}
		others++
	}
	// batch_size defaults to 200 and there are two inputs, so one statement.
	if inserts != 1 {
		t.Errorf("UpsertMany issued %d INSERT statements, want 1 (one per sub-batch): %v", inserts, sqls)
	}
	if others != 1 {
		t.Errorf("UpsertMany issued %d non-INSERT statements, want 1 (the terminal re-fetch): %v", others, sqls)
	}
}

// --- The database-generated key path ---

// categories.id is a DEFAULT-backed uuid, so the caller need not supply it and
// UpsertMany cannot name the written rows from the inputs. It reads them back
// by the conflict target instead — which must produce the *existing* row's id
// on the conflict branch, not a freshly generated one.
func TestUpsertMany_DBGeneratedPK_ResolvesExistingIDs(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seeded, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name:        "GenPKCat-existing",
		Description: omittable.Set(new("before")),
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM categories WHERE name LIKE $1`, "GenPKCat-%")
	})

	got, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "GenPKCat-existing", Description: omittable.Set(new("after"))},
		{Name: "GenPKCat-new", Description: omittable.Set(new("fresh"))},
	}, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("UpsertMany on a db-generated key: %v", err)
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
		t.Errorf("conflicting row resolved to id %v, want the existing row's %v", existing.ID, seeded.ID)
	}
	if existing.Description == nil || *existing.Description != "after" {
		t.Errorf("conflicting row description = %v, want %q", existing.Description, "after")
	}
	if fresh, ok := byName["GenPKCat-new"]; !ok || fresh.ID == (uuid.UUID{}) {
		t.Errorf("newly inserted row missing or carries a zero id: %v", byName)
	}
}

// A conflict target the input cannot supply leaves the written rows unnameable,
// so the operation refuses before it writes anything rather than returning a
// primary key it guessed. categories.id is DEFAULT-backed and absent from these
// inputs, so ConflictPK names a column the statement does not carry.
func TestUpsertMany_DBGeneratedPK_UnsuppliedConflictTargetRefused(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	_, err := client.Categories().UpsertMany(ctx, []*models.CreateCategoryInput{
		{Name: "UnsuppliedTargetCat"},
	}, models.CategoryConflictPK)
	if err == nil {
		_, _ = testPool.Exec(ctx, `DELETE FROM categories WHERE name = $1`, "UnsuppliedTargetCat")
		t.Fatal("UpsertMany with an unsupplied conflict target succeeded, want a refusal before any write")
	}
	if !strings.Contains(err.Error(), "cannot be resolved") && !strings.Contains(err.Error(), "does not supply") {
		t.Errorf("UpsertMany error = %v, want one naming the unsupplied conflict target", err)
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM categories WHERE name = $1`, "UnsuppliedTargetCat").Scan(&count); err != nil {
		t.Fatalf("counting categories: %v", err)
	}
	if count != 0 {
		t.Errorf("refused UpsertMany wrote %d rows, want 0", count)
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
		t.Fatal("UpsertMany with a nil element = nil error, want ErrNilInput")
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM categories WHERE name = $1`, "NilElementCat").Scan(&count); err != nil {
		t.Fatalf("counting categories: %v", err)
	}
	if count != 0 {
		t.Errorf("UpsertMany rejected for a nil element still wrote %d rows, want 0", count)
	}
}

// An empty call writes nothing and — the part that is easy to get wrong — reads
// nothing: the terminal re-fetch filters on the written keys, and a key filter
// built from an empty set contributes no condition at all.
func TestUpsertMany_EmptyInput(t *testing.T) {
	ctx := context.Background()
	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	cap.reset()
	got, err := client.Categories().UpsertMany(ctx, nil, models.CategoryConflictName)
	if err != nil {
		t.Fatalf("UpsertMany(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("UpsertMany(nil) returned %d entities, want 0", len(got))
	}
	if sqls := cap.snapshot(); len(sqls) != 0 {
		t.Errorf("UpsertMany(nil) issued %d statements, want 0: %v", len(sqls), sqls)
	}
}

// --- A caller-known key is not the written row's key on every target ---

// counters.key is the primary key, supplied by the caller (promoted from a
// UNIQUE by primary_key.columns), and counters.slot is a second UNIQUE that
// does NOT cover it. Upserting on CounterConflictSlot therefore conflicts on a
// column that is not the key, and the conflicting row keeps the key it already
// had — the key column is excluded from the update half of the clause.
//
// Taking the key from the input there names a row that does not exist: it would
// put a phantom key on AffectedPKs (the cache eviction key and Event.PK) and
// make the terminal GetMany return short. This pins that UpsertMany reads the
// written rows back on such a target instead.
//
// Failing-first: against the pre-fix template this returns 1 entity, not 2, and
// the surviving entity carries "readback-wanted" rather than the seeded key.
func TestUpsertMany_CallerKnownKey_NonCoveringTargetResolvesExistingKey(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	const seededKey = "readback-seeded"
	const wantedKey = "readback-wanted"
	const freshKey = "readback-fresh"

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM counters WHERE key LIKE $1`, "readback-%")
	})

	// A row that already owns slot "readback-slot-a", under a key the upsert
	// below will NOT supply.
	if _, err := client.Counters().Create(ctx, &models.CreateCounterInput{
		Key:  seededKey,
		Slot: omittable.Set(new("readback-slot-a")),
	}); err != nil {
		t.Fatalf("Create seed counter: %v", err)
	}

	got, err := client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
		// Conflicts on slot. The stored row keeps key=seededKey; wantedKey is
		// never written anywhere.
		{Key: wantedKey, Slot: omittable.Set(new("readback-slot-a")), Count: omittable.Set(int64(7))},
		// No conflict — inserted as given.
		{Key: freshKey, Slot: omittable.Set(new("readback-slot-b")), Count: omittable.Set(int64(3))},
	}, models.CounterConflictSlot)
	if err != nil {
		t.Fatalf("UpsertMany on a non-covering conflict target: %v", err)
	}

	// PRD §9.5 decision 2: one entity per deduped input, on every target.
	if len(got) != 2 {
		t.Fatalf("UpsertMany returned %d entities, want 2 — a conflicting row must be resolved by its own key, not by the one the input carried (PRD §9.5)", len(got))
	}

	byKey := make(map[string]*models.Counter, len(got))
	for _, c := range got {
		byKey[c.Key] = c
	}
	if _, ok := byKey[wantedKey]; ok {
		t.Errorf("result carries key %q, which was never written: the conflicting row keeps its own key", wantedKey)
	}
	existing, ok := byKey[seededKey]
	if !ok {
		t.Fatalf("result has no %q; got keys %v", seededKey, slices.Sorted(maps.Keys(byKey)))
	}
	if existing.Count != 7 {
		t.Errorf("conflicting row count = %d, want 7 (the upsert's value must have landed on the existing row)", existing.Count)
	}
	if _, ok := byKey[freshKey]; !ok {
		t.Errorf("result has no %q; the non-conflicting row must still be inserted and returned", freshKey)
	}

	// The write landed on the seeded row rather than creating a second one.
	var rows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM counters WHERE key LIKE $1`, "readback-%").Scan(&rows); err != nil {
		t.Fatalf("counting counters: %v", err)
	}
	if rows != 2 {
		t.Errorf("counters matching readback-%% = %d, want 2 (one conflicted, one inserted)", rows)
	}
}

// The same table on a PK-covering target keeps the one-statement fast path:
// the key is caller-known there, so nothing is read back.
func TestUpsertMany_CallerKnownKey_CoveringTargetIssuesNoReadBack(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM counters WHERE key LIKE $1`, "readbackcov-%")
	})

	cap := newCapturingQuerier(dbpgx.New(testPool))
	client := models.New(cap)

	cap.reset()
	if _, err := client.Counters().UpsertMany(ctx, []*models.CreateCounterInput{
		{Key: "readbackcov-a", Count: omittable.Set(int64(1))},
		{Key: "readbackcov-b", Count: omittable.Set(int64(2))},
	}, models.CounterConflictPK); err != nil {
		t.Fatalf("UpsertMany on the PK target: %v", err)
	}

	sqls := cap.snapshot()
	var inserts, selects int
	for _, sqlStr := range sqls {
		switch {
		case strings.HasPrefix(strings.TrimSpace(strings.ToUpper(sqlStr)), "INSERT "):
			inserts++
		default:
			selects++
		}
	}
	if inserts != 1 {
		t.Errorf("UpsertMany issued %d INSERT statements, want 1: %v", inserts, sqls)
	}
	// Only the terminal re-fetch. A read-back would make this 2.
	if selects != 1 {
		t.Errorf("UpsertMany issued %d non-INSERT statements, want 1 (the terminal re-fetch only — a PK-covering target needs no read-back): %v", selects, sqls)
	}
}
