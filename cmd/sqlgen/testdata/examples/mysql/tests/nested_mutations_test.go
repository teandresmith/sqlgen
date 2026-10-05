package tests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// Nested mutations on the one dialect without RETURNING (PRD §9.9).
//
// PostgreSQL and SQLite learn which rows a statement wrote from the statement
// that wrote them. MySQL cannot, and the generated client makes up for it in
// two ways the other dialects never use. `*Where` methods pre-SELECT the rows
// they are about to change (collectAffectedIDs), and `CreateMany` derives a
// batch's generated keys as LAST_INSERT_ID() + i instead of reading them back.
// The nested executor depended on both. The O2M adoption's shortfall check
// counted what UpdateWhere returned, and the M2M link step keys its junction
// rows on what CreateMany returned. So this is the dialect where the nested
// executor's correctness arguments rest on a different mechanism from the one
// they were written and tested against. This file tests that mechanism rather
// than re-running the PostgreSQL suite under another driver.
//
// Two defects were found here and fixed in the generator:
//
//   - The adoption's shortfall check could not see a lost race. The pre-SELECT
//     read from the transaction's REPEATABLE READ snapshot, so a row another
//     connection parented after that snapshot was counted as adopted while the
//     UPDATE's `fk IS NULL` guard skipped it. The call returned nil and the row
//     stayed with the other parent. The adoption no longer counts what
//     UpdateWhere reports: after each chunk's UPDATE it runs a verify read, the
//     chunk's ids that now carry `fk = parent`, and counts that. A plain read
//     inside the transaction sees the transaction's own writes, so it counts
//     what the UPDATE adopted, on every dialect and with no lock. Making the
//     pre-SELECT a locking read also fixed the race, and was withdrawn: FOR
//     UPDATE on an empty match takes a gap lock held until commit, which made
//     another connection's insert into the edge wait (PRD §9.8.6b).
//   - The link step's upsert reset every non-key junction column on a re-link.
//     `user_categories.slot` went from 7 to NULL on a `connect` of a pair that
//     was already linked. §9.9.4 now refuses a junction with any column beyond its
//     two foreign keys, so `users.Categories` is no longer nestable here, and
//     the M2M cases below run through `assets.Documents` instead.

// seedNestedUser inserts a bare parent through the flat surface. Its cleanup
// first deletes any event still on its edge, since the FK would otherwise
// refuse the delete.
func seedNestedUser(t *testing.T, client *models.Client, email string) int64 {
	t.Helper()
	ctx := context.Background()
	u, err := client.Users().Create(ctx, &models.CreateUserInput{Name: email, Email: email, Balance: 1})
	if err != nil {
		t.Fatalf("seeding user %s: %v", email, err)
	}
	t.Cleanup(func() {
		_ = client.UserEvents().HardDeleteWhere(ctx, &models.UserEventFilter{UserID: &comparator.NullableNumber[int64]{Eq: new(u.ID)}})
		_ = client.Users().HardDelete(ctx, u.ID)
	})
	return u.ID
}

// seedUserEvent inserts an event parented to owner, or unparented when owner
// is nil — the shape `connect` adopts.
func seedUserEvent(t *testing.T, client *models.Client, owner *int64, action string) int64 {
	t.Helper()
	in := &models.CreateUserEventInput{Action: action}
	if owner != nil {
		in.UserID = omittable.Set(owner)
	}
	ev, err := client.UserEvents().Create(context.Background(), in)
	if err != nil {
		t.Fatalf("seeding event %s: %v", action, err)
	}
	t.Cleanup(func() { _ = client.UserEvents().HardDelete(context.Background(), ev.ID) })
	return ev.ID
}

// seedDocument inserts a document with no asset link.
func seedDocument(t *testing.T, client *models.Client, name string) int64 {
	t.Helper()
	doc, err := client.Documents().Create(context.Background(), &models.CreateDocumentInput{
		EntityType: models.DocumentsEntityTypeEnumSpv,
		Name:       name,
	})
	if err != nil {
		t.Fatalf("seeding document %s: %v", name, err)
	}
	t.Cleanup(func() { _ = client.Documents().HardDelete(context.Background(), doc.ID) })
	return doc.ID
}

// userEventActions is the set of actions on a user's UserEvents edge, read
// through ctx so a caller inside a transaction sees its own writes. Actions
// rather than ids, so a mismatch names the row in the failure message.
func userEventActions(ctx context.Context, client *models.Client, user int64) ([]string, error) {
	rows, err := client.UserEvents().GetMany(ctx, &models.GetUserEventsInput{
		Filter: &models.UserEventFilter{UserID: &comparator.NullableNumber[int64]{Eq: new(user)}},
		Limit:  new(0),
	})
	if err != nil {
		return nil, fmt.Errorf("reading user %d's events: %w", user, err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action)
	}
	slices.Sort(out)
	return out, nil
}

// assetDocumentNames is the set of document names linked to an asset through
// the junction, read through ctx.
func assetDocumentNames(ctx context.Context, client *models.Client, asset int64) ([]string, error) {
	links, err := client.AssetDocumentLinks().GetMany(ctx, &models.GetAssetDocumentLinksInput{
		Filter: &models.AssetDocumentLinkFilter{AssetID: &comparator.Number[int64]{Eq: new(asset)}},
		Limit:  new(0),
	})
	if err != nil {
		return nil, fmt.Errorf("reading asset %d's links: %w", asset, err)
	}
	if len(links) == 0 {
		return []string{}, nil
	}
	ids := make([]int64, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.DocumentID)
	}
	docs, err := client.Documents().GetMany(ctx, &models.GetDocumentsInput{
		Filter: &models.DocumentFilter{ID: &comparator.Number[int64]{In: ids}},
		Limit:  new(0),
	})
	if err != nil {
		return nil, fmt.Errorf("reading asset %d's documents: %w", asset, err)
	}
	if len(docs) != len(ids) {
		return nil, fmt.Errorf("asset %d has %d links but %d of the documents they name exist", asset, len(ids), len(docs))
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Name)
	}
	slices.Sort(out)
	return out, nil
}

// TestNestedMutations_LifecycleInOneCallerTransaction runs the whole nested
// surface in order — CreateWithRelated, UpdateWithRelated, UpsertWithRelated,
// then the flat many-delete — inside one caller transaction, and asserts the
// database state after every step rather than only at the end.
//
// Because the caller already holds the transaction, every nested method runs
// as a savepoint (PRD §18.3). That also pins that each method's transaction
// name is a bare SQL identifier on MySQL, which renders it into SAVEPOINT
// unquoted, and not only on PostgreSQL.
//
// Two parents between them cover every shape MySQL can nest: `users` carries
// has-one (`Profile`), O2M on a NOT NULL FK (`Orders`) and O2M on a nullable FK
// (`UserEvents`), and `assets` carries M2M (`Documents`). No table here has a
// soft-delete column, so the §9.2 many-delete for all of them is
// HardDeleteMany.
func TestNestedMutations_LifecycleInOneCallerTransaction(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	orphanA := seedUserEvent(t, client, nil, "lifecycle-orphan-a")
	orphanB := seedUserEvent(t, client, nil, "lifecycle-orphan-b")
	orphanC := seedUserEvent(t, client, nil, "lifecycle-orphan-c")
	existingDoc := seedDocument(t, client, "lifecycle-existing.pdf")

	// Every assertion inside the transaction records and carries on; the body
	// returns only on a failed call. A t.Fatal here would unwind through
	// WithTx without rolling back and leave the connection holding the
	// transaction.
	expect := func(step, what string, got, want []string) {
		t.Helper()
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s: %s (-want +got):\n%s", step, what, diff)
		}
	}

	var user, asset, annex int64
	err := client.WithTx(ctx, "nested_lifecycle", func(txCtx context.Context) error {
		// Step 1 — CreateWithRelated, every verb the create side admits.
		u, err := client.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
			User:    models.CreateUserInput{Name: "Lifecycle", Email: "nested-lifecycle@example.com", Balance: 10},
			Profile: &models.UserProfileCreateNested{Create: &models.UserProfileCreateInput{Website: omittable.Set(new("https://lifecycle.example.com"))}},
			Orders:  &models.UserOrdersCreateNested{Create: []*models.UserOrdersCreateInput{{Total: 1}, {Total: 2}}},
			UserEvents: &models.UserUserEventsCreateNested{
				Create:  []*models.UserUserEventsCreateInput{{Action: "created-1"}, {Action: "created-2"}},
				Connect: []int64{orphanA},
			},
		})
		if err != nil {
			return fmt.Errorf("step 1, users: %w", err)
		}
		user = u.ID
		a, err := client.Assets().CreateWithRelated(txCtx, &models.CreateAssetWithRelatedInput{
			Asset: models.CreateAssetInput{Name: "Lifecycle Plant"},
			Documents: &models.AssetDocumentsCreateNested{
				Create: []*models.CreateDocumentInput{
					{EntityType: models.DocumentsEntityTypeEnumSpv, Name: "lifecycle-new-1.pdf"},
					{EntityType: models.DocumentsEntityTypeEnumSpv, Name: "lifecycle-new-2.pdf"},
				},
				Connect: []int64{existingDoc},
			},
		})
		if err != nil {
			return fmt.Errorf("step 1, assets: %w", err)
		}
		asset = a.ID

		events, err := userEventActions(txCtx, client, user)
		if err != nil {
			return err
		}
		expect("step 1", "UserEvents", events, []string{"created-1", "created-2", "lifecycle-orphan-a"})
		orders, err := client.Orders().GetMany(txCtx, &models.GetOrdersInput{Filter: &models.OrderFilter{UserID: &comparator.Number[int64]{Eq: new(user)}}, Limit: new(0)})
		if err != nil {
			return err
		}
		if len(orders) != 2 {
			t.Errorf("step 1: Orders has %d rows, want 2", len(orders))
		}
		profiles, err := client.Profiles().GetMany(txCtx, &models.GetProfilesInput{Filter: &models.ProfileFilter{UserID: &comparator.Number[int64]{Eq: new(user)}}, Limit: new(0)})
		if err != nil {
			return err
		}
		if len(profiles) != 1 || profiles[0].Website == nil || *profiles[0].Website != "https://lifecycle.example.com" {
			t.Errorf("step 1: Profile = %+v, want one row carrying the nested website", profiles)
		}
		docs, err := assetDocumentNames(txCtx, client, asset)
		if err != nil {
			return err
		}
		expect("step 1", "Documents", docs, []string{"lifecycle-existing.pdf", "lifecycle-new-1.pdf", "lifecycle-new-2.pdf"})

		// Step 2 — UpdateWithRelated. `connect` names orphanA again (already
		// ours: a no-op, not a conflict) and existingDoc again (already
		// linked: the link step's upsert takes its conflict branch on MySQL's
		// ON DUPLICATE KEY, which on a pure junction changes nothing).
		var created1 int64
		all, err := client.UserEvents().GetMany(txCtx, &models.GetUserEventsInput{
			Filter: &models.UserEventFilter{Action: &comparator.String{Eq: new("created-1")}, UserID: &comparator.NullableNumber[int64]{Eq: new(user)}},
			Limit:  new(0),
		})
		if err != nil || len(all) != 1 {
			return fmt.Errorf("step 2: locating created-1: %d rows, %w", len(all), err)
		}
		created1 = all[0].ID
		if _, err := client.Users().UpdateWithRelated(txCtx, user, &models.UpdateUserWithRelatedInput{
			User:   models.UpdateUserInput{Name: omittable.Set("Lifecycle Updated")},
			Orders: &models.UserOrdersUpdateNested{Create: []*models.UserOrdersCreateInput{{Total: 3}}},
			UserEvents: &models.UserUserEventsUpdateNested{
				Connect:    []int64{orphanA, orphanB},
				Disconnect: []int64{created1},
			},
		}); err != nil {
			return fmt.Errorf("step 2, users: %w", err)
		}
		newDocs, err := client.Documents().GetMany(txCtx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{Name: &comparator.String{Eq: new("lifecycle-new-1.pdf")}},
			Limit:  new(0),
		})
		if err != nil || len(newDocs) != 1 {
			return fmt.Errorf("step 2: locating lifecycle-new-1.pdf: %d rows, %w", len(newDocs), err)
		}
		if _, err := client.Assets().UpdateWithRelated(txCtx, asset, &models.UpdateAssetWithRelatedInput{
			Documents: &models.AssetDocumentsUpdateNested{
				Create:     []*models.CreateDocumentInput{{EntityType: models.DocumentsEntityTypeEnumSpv, Name: "lifecycle-new-3.pdf"}},
				Connect:    []int64{existingDoc},
				Disconnect: []int64{newDocs[0].ID},
			},
		}); err != nil {
			return fmt.Errorf("step 2, assets: %w", err)
		}

		events, err = userEventActions(txCtx, client, user)
		if err != nil {
			return err
		}
		expect("step 2", "UserEvents", events, []string{"created-2", "lifecycle-orphan-a", "lifecycle-orphan-b"})
		// The unlinked row still exists and its FK is NULL,
		// not left pointing at the parent.
		unlinked, err := client.UserEvents().Get(txCtx, created1)
		if err != nil {
			return fmt.Errorf("step 2: re-reading the disconnected event: %w", err)
		}
		if unlinked.UserID != nil {
			t.Errorf("step 2: the disconnected event's user_id = %d, want NULL", *unlinked.UserID)
		}
		reloaded, err := client.Users().Get(txCtx, user)
		if err != nil {
			return err
		}
		if reloaded.Name != "Lifecycle Updated" {
			t.Errorf("step 2: Name = %q, want the flat half of the wrapper applied", reloaded.Name)
		}
		orders, err = client.Orders().GetMany(txCtx, &models.GetOrdersInput{Filter: &models.OrderFilter{UserID: &comparator.Number[int64]{Eq: new(user)}}, Limit: new(0)})
		if err != nil {
			return err
		}
		if len(orders) != 3 {
			t.Errorf("step 2: Orders has %d rows, want 3", len(orders))
		}
		docs, err = assetDocumentNames(txCtx, client, asset)
		if err != nil {
			return err
		}
		expect("step 2", "Documents", docs, []string{"lifecycle-existing.pdf", "lifecycle-new-2.pdf", "lifecycle-new-3.pdf"})
		// `disconnect` deletes the junction row, never the target.
		if _, err := client.Documents().Get(txCtx, newDocs[0].ID); err != nil {
			t.Errorf("step 2: the disconnected document itself is gone: %v", err)
		}

		// Step 3 — UpsertWithRelated. `users` takes the conflict branch on
		// email; `clear` + `connect` is "the set is now exactly this".
		// `assets` can only take the insert branch — its one conflict target is
		// a generated key the input cannot carry — which is the branch where
		// `clear` matches nothing.
		up, err := client.Users().UpsertWithRelated(txCtx, &models.UpsertUserWithRelatedInput{
			User:       models.CreateUserInput{Name: "Lifecycle Upserted", Email: "nested-lifecycle@example.com", Balance: 20},
			UserEvents: &models.UserUserEventsUpdateNested{Clear: true, Connect: []int64{orphanC}},
		}, models.UserConflictEmail)
		if err != nil {
			return fmt.Errorf("step 3, users: %w", err)
		}
		if up.ID != user {
			t.Errorf("step 3: the upsert minted user %d, want the conflict branch to update %d", up.ID, user)
		}
		an, err := client.Assets().UpsertWithRelated(txCtx, &models.UpsertAssetWithRelatedInput{
			Asset:     models.CreateAssetInput{Name: "Lifecycle Annex"},
			Documents: &models.AssetDocumentsUpdateNested{Clear: true, Connect: []int64{existingDoc}},
		}, models.AssetConflictPK)
		if err != nil {
			return fmt.Errorf("step 3, assets: %w", err)
		}
		annex = an.ID

		events, err = userEventActions(txCtx, client, user)
		if err != nil {
			return err
		}
		expect("step 3", "UserEvents", events, []string{"lifecycle-orphan-c"})
		docs, err = assetDocumentNames(txCtx, client, annex)
		if err != nil {
			return err
		}
		expect("step 3", "the annex's Documents", docs, []string{"lifecycle-existing.pdf"})
		// The first asset is untouched: the annex's `clear` is scoped to its
		// own key.
		docs, err = assetDocumentNames(txCtx, client, asset)
		if err != nil {
			return err
		}
		expect("step 3", "the first asset's Documents", docs, []string{"lifecycle-existing.pdf", "lifecycle-new-2.pdf", "lifecycle-new-3.pdf"})

		// Step 4 — the flat many-delete over everything the lifecycle wrote,
		// children before the parents their NOT NULL FKs point at.
		var orderIDs []int64
		for _, o := range orders {
			orderIDs = append(orderIDs, o.ID)
		}
		if err := client.Orders().HardDeleteMany(txCtx, orderIDs); err != nil {
			return fmt.Errorf("step 4, orders: %w", err)
		}
		if err := client.Profiles().HardDeleteMany(txCtx, []int64{profiles[0].ID}); err != nil {
			return fmt.Errorf("step 4, profiles: %w", err)
		}
		mine, err := client.UserEvents().GetMany(txCtx, &models.GetUserEventsInput{
			Filter: &models.UserEventFilter{Action: &comparator.String{In: []string{"created-1", "created-2"}}},
			Limit:  new(0),
		})
		if err != nil {
			return err
		}
		eventIDs := []int64{orphanA, orphanB, orphanC}
		for _, e := range mine {
			eventIDs = append(eventIDs, e.ID)
		}
		if err := client.UserEvents().HardDeleteMany(txCtx, eventIDs); err != nil {
			return fmt.Errorf("step 4, user_events: %w", err)
		}
		if err := client.Users().HardDeleteMany(txCtx, []int64{user}); err != nil {
			return fmt.Errorf("step 4, users: %w", err)
		}
		lifecycleDocs, err := client.Documents().GetMany(txCtx, &models.GetDocumentsInput{
			Filter: &models.DocumentFilter{Name: &comparator.String{In: []string{"lifecycle-new-1.pdf", "lifecycle-new-2.pdf", "lifecycle-new-3.pdf"}}},
			Limit:  new(0),
		})
		if err != nil {
			return err
		}
		docIDs := make([]int64, 0, len(lifecycleDocs))
		for _, d := range lifecycleDocs {
			docIDs = append(docIDs, d.ID)
		}
		// The junction's FKs cascade, so deleting the assets takes the links.
		if err := client.Assets().HardDeleteMany(txCtx, []int64{asset, annex}); err != nil {
			return fmt.Errorf("step 4, assets: %w", err)
		}
		if err := client.Documents().HardDeleteMany(txCtx, docIDs); err != nil {
			return fmt.Errorf("step 4, documents: %w", err)
		}

		if _, err := client.Users().Get(txCtx, user); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("step 4: the user survived its HardDeleteMany: %v", err)
		}
		for _, id := range []int64{asset, annex} {
			docs, err := assetDocumentNames(txCtx, client, id)
			if err != nil {
				return err
			}
			expect("step 4", fmt.Sprintf("asset %d's links", id), docs, []string{})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("lifecycle transaction: %v", err)
	}

	// Committed: the deletes are the durable end state.
	if _, err := client.Users().Get(ctx, user); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("after commit: the user is still present: %v", err)
	}
	if _, err := client.Assets().Get(ctx, asset); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("after commit: the asset is still present: %v", err)
	}
}

// TestNestedMutations_M2MCreateLinksTheRowsItInserted pins the MySQL key
// derivation through the link step. It is the highest-consequence divergence
// this dialect has.
//
// A nested M2M `create` inserts the targets with CreateMany, and on MySQL
// CreateMany derives their keys as LAST_INSERT_ID() + i. The link step then
// writes one junction row per derived key. A derivation that is off by one
// links the parent to the wrong target row and reports success, and it still
// produces the right *number* of links. So this asserts which targets are
// linked, by name, and not how many.
//
// The decoy is inserted immediately before the call, so an off-by-one below
// lands on a row that exists instead of failing an FK check. The 401-target
// case spans three CreateMany statements at the default batch size of 200.
// Each statement has its own LAST_INSERT_ID, so a derivation that carried one
// batch's first key into the next would fail there and not at 3.
//
// Each case runs twice: on the default step, and from a pool whose sessions
// have auto_increment_increment=2, where the keys come out spaced and a
// derivation that assumed 1 links a subset. The nested call runs in a
// transaction, so the step is read on the session that ran the INSERT.
//
// Verified failing-first: with documentClient.multiInsertAndResolveIDs changed
// to `firstID + int64(i) - 1` in the generated models, both default-step cases
// link the decoy and drop the last inserted document, and the call returns nil.
// Against `firstID + int64(i)`, both step-2 cases link the wrong documents.
func TestNestedMutations_M2MCreateLinksTheRowsItInserted(t *testing.T) {
	pools := []struct {
		name, label string
		client      *models.Client
	}{
		{name: "step 1", label: "m2m-step1", client: newClient()},
		{name: "step 2", label: "m2m-step2", client: models.New(dbstdlib.New(openStepPool(t, 2)))},
	}
	for _, p := range pools {
		for _, n := range []int{3, 401} {
			t.Run(fmt.Sprintf("%s, %d targets", p.name, n), func(t *testing.T) {
				assertM2MCreateLinksTheRowsItInserted(t, p.client, fmt.Sprintf("%s-%d", p.label, n), n)
			})
		}
	}
}

// assertM2MCreateLinksTheRowsItInserted nests a create of n documents under a
// new asset, next to a decoy document, and asserts the junction rows name
// exactly the documents the call inserted.
func assertM2MCreateLinksTheRowsItInserted(t *testing.T, client *models.Client, label string, n int) {
	t.Helper()
	ctx := context.Background()
	seedDocument(t, client, label+"-decoy.pdf")

	creates := make([]*models.CreateDocumentInput, 0, n)
	want := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("%s-target-%03d.pdf", label, i)
		creates = append(creates, &models.CreateDocumentInput{EntityType: models.DocumentsEntityTypeEnumSpv, Name: name})
		want = append(want, name)
	}
	slices.Sort(want)

	a, err := client.Assets().CreateWithRelated(ctx, &models.CreateAssetWithRelatedInput{
		Asset:     models.CreateAssetInput{Name: "M2M identity " + label},
		Documents: &models.AssetDocumentsCreateNested{Create: creates},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Assets().HardDelete(ctx, a.ID)
		_ = client.Documents().HardDeleteWhere(ctx, &models.DocumentFilter{Name: &comparator.String{In: want}})
	})

	got, err := assetDocumentNames(ctx, client, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the junction rows name documents other than the ones the nested create inserted (-want +got):\n%s", diff)
	}
}

// TestNestedMutations_O2MAdoption walks the adoption path end to end on MySQL:
// §9.9.6's three-way `connect`, then `disconnect` and `clear`. Each unlink
// reads the column back rather than trusting the call: leaving the FK out
// of the SET list and setting it NULL are both expressible, both match their
// rows and both report success, but only the second unlinks anything.
func TestNestedMutations_O2MAdoption(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	owner := func(t *testing.T, id int64) *int64 {
		t.Helper()
		ev, err := client.UserEvents().Get(ctx, id)
		if err != nil {
			t.Fatalf("re-reading event %d: %v", id, err)
		}
		return ev.UserID
	}

	user := seedNestedUser(t, client, "adoption@example.com")
	other := seedNestedUser(t, client, "adoption-other@example.com")

	t.Run("an orphan is adopted", func(t *testing.T) {
		orphan := seedUserEvent(t, client, nil, "adoption-orphan")
		if _, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Connect: []int64{orphan}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		if got := owner(t, orphan); got == nil || *got != user {
			t.Errorf("user_id = %v, want %d", got, user)
		}
	})

	t.Run("already ours is a no-op", func(t *testing.T) {
		ours := seedUserEvent(t, client, &user, "adoption-ours")
		if _, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Connect: []int64{ours}},
		}); err != nil {
			t.Fatalf("connecting a row already on the edge: %v — the requested end state holds", err)
		}
		if got := owner(t, ours); got == nil || *got != user {
			t.Errorf("user_id = %v, want it still %d", got, user)
		}
	})

	t.Run("parented elsewhere is ErrAlreadyRelated", func(t *testing.T) {
		taken := seedUserEvent(t, client, &other, "adoption-taken")
		_, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Connect: []int64{taken}},
		})
		if !errors.Is(err, models.ErrAlreadyRelated) {
			t.Fatalf("err = %v, want ErrAlreadyRelated", err)
		}
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) || nested.Edge != "UserEvents" || nested.Verb != "connect" || nested.ID != taken {
			t.Errorf("attribution = %v, want UserEvents/connect naming %d", err, taken)
		}
		if got := owner(t, taken); got == nil || *got != other {
			t.Errorf("user_id = %v, want it still %d", got, other)
		}
	})

	t.Run("not visible is ErrNotFound", func(t *testing.T) {
		_, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Connect: []int64{9_000_000_000}},
		})
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound — never CONFLICT, which would confirm the row exists", err)
		}
	})

	t.Run("disconnect sets the FK NULL", func(t *testing.T) {
		child := seedUserEvent(t, client, &user, "adoption-disconnect")
		if _, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Disconnect: []int64{child}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		if got := owner(t, child); got != nil {
			t.Errorf("user_id = %d, want NULL", *got)
		}
	})

	t.Run("clear sets every FK on the edge NULL, and only on the edge", func(t *testing.T) {
		parent := seedNestedUser(t, client, "adoption-clear@example.com")
		a := seedUserEvent(t, client, &parent, "adoption-clear-a")
		b := seedUserEvent(t, client, &parent, "adoption-clear-b")
		bystander := seedUserEvent(t, client, &other, "adoption-clear-bystander")
		if _, err := client.Users().UpdateWithRelated(ctx, parent, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		for _, id := range []int64{a, b} {
			if got := owner(t, id); got != nil {
				t.Errorf("event %d: user_id = %d, want NULL", id, *got)
			}
		}
		if got := owner(t, bystander); got == nil || *got != other {
			t.Errorf("clear reached another parent's event: user_id = %v, want %d", got, other)
		}
	})

	// This is the case §9.9.6's `fk IS NULL` guard exists for: the row is
	// unparented when the visibility read looks at it and parented by another
	// connection before the adoption's UPDATE runs. The hook fires at the
	// start of the adoption's UpdateWhere, which is after the visibility read
	// and before the pre-SELECT, and commits the competing write on a
	// separate connection.
	//
	// Measured before the fix, under MySQL's default REPEATABLE READ: `err =
	// nil`, with the row parented to `other`. The adoption counted the rows
	// UpdateWhere reported, which on MySQL are the rows its pre-SELECT found.
	// That read answered from the transaction's snapshot, where the row was
	// still unparented. The UPDATE is a current read, so it skipped the row.
	// The shortfall check compared two numbers that agreed. PostgreSQL's
	// RETURNING has no such gap. Under READ COMMITTED the same interleaving was
	// already caught, because every read there takes a fresh snapshot.
	//
	// The adoption now counts a verify read issued after the UPDATE: the ids
	// that now carry `fk = parent`. It sees the transaction's own writes and
	// only those, so the row the UPDATE skipped is not counted. A throwaway
	// probe on a READ COMMITTED pool also returned ErrAlreadyRelated.
	//
	// Verified failing-first (2026-09-23): putting the old spelling back into
	// the generated userClient.applyUserUserEventsNested, so the adoption
	// selects `ID` from UpdateWhere and counts those rows, with no verify read
	// and no lock on collectAffectedIDs, restores `err = nil`.
	t.Run("a lost race is ErrAlreadyRelated", func(t *testing.T) {
		contested := seedUserEvent(t, client, nil, "adoption-race")

		var fired atomic.Bool
		interleave := func(next hook.MutationHandler) hook.MutationHandler {
			return func(ctx context.Context, m *hook.MutationContext) (any, error) {
				if m.Op == hook.OpUpdateWhere && m.Table == models.TableUserEvents && fired.CompareAndSwap(false, true) {
					if _, err := testDB.ExecContext(context.Background(), "UPDATE user_events SET user_id = ? WHERE id = ?", other, contested); err != nil {
						return nil, fmt.Errorf("the competing connect: %w", err)
					}
				}
				return next(ctx, m)
			}
		}
		racing := models.New(dbstdlib.New(testDB), models.WithMutationHook(interleave))

		_, err := racing.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			UserEvents: &models.UserUserEventsUpdateNested{Connect: []int64{contested}},
		})
		if !fired.Load() {
			t.Fatal("the interleaving hook never fired; the adoption did not route through UpdateWhere")
		}
		if !errors.Is(err, models.ErrAlreadyRelated) {
			t.Errorf("err = %v, want ErrAlreadyRelated — the adoption lost the race and must say so", err)
		}
		if got := owner(t, contested); got == nil || *got != other {
			t.Errorf("user_id = %v, want %d, the connection that won", got, other)
		}
	})
}

// TestNestedMutations_SavepointComposition runs the lifecycle through the
// explicit Begin / Commit spelling and fails its last nested step on purpose.
//
// The failing UpsertWithRelated runs its parent write and its `clear` before
// the `connect` that fails. Rolling back to its savepoint must undo both,
// leaving the first two steps and the rest of the caller's transaction intact.
// A nested method that opened its own transaction, or that released its
// savepoint before failing, would keep the `clear` and leave the edge empty.
func TestNestedMutations_SavepointComposition(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	orphan := seedUserEvent(t, client, nil, "savepoint-orphan")

	txCtx, err := client.Begin(ctx, "nested_savepoints")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			_ = client.Rollback(txCtx)
		}
	})

	u, err := client.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
		User:       models.CreateUserInput{Name: "Savepoint", Email: "nested-savepoint@example.com", Balance: 1},
		UserEvents: &models.UserUserEventsCreateNested{Create: []*models.UserUserEventsCreateInput{{Action: "savepoint-created"}}, Connect: []int64{orphan}},
	})
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	user := u.ID
	t.Cleanup(func() {
		// Cleanups run last-registered-first, so this one runs before the
		// Rollback registered above. On a failure between Begin and Commit the
		// transaction still holds its row locks, and a delete on another
		// connection would wait out innodb_lock_wait_timeout. Roll back first.
		if !committed {
			_ = client.Rollback(txCtx)
		}
		_ = client.UserEvents().HardDeleteWhere(ctx, &models.UserEventFilter{UserID: &comparator.NullableNumber[int64]{Eq: new(user)}})
		_ = client.Orders().HardDeleteWhere(ctx, &models.OrderFilter{UserID: &comparator.Number[int64]{Eq: new(user)}})
		_ = client.Users().HardDelete(ctx, user)
	})

	if _, err := client.Users().UpdateWithRelated(txCtx, user, &models.UpdateUserWithRelatedInput{
		User:   models.UpdateUserInput{Name: omittable.Set("Savepoint Updated")},
		Orders: &models.UserOrdersUpdateNested{Create: []*models.UserOrdersCreateInput{{Total: 5}}},
	}); err != nil {
		t.Fatalf("step 2: %v", err)
	}

	_, err = client.Users().UpsertWithRelated(txCtx, &models.UpsertUserWithRelatedInput{
		User:       models.CreateUserInput{Name: "should not land", Email: "nested-savepoint@example.com", Balance: 99},
		UserEvents: &models.UserUserEventsUpdateNested{Clear: true, Connect: []int64{9_000_000_000}},
	}, models.UserConflictEmail)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("step 3: err = %v, want the unresolvable connect's ErrNotFound", err)
	}

	// Inside the caller's transaction, after the savepoint rolled back.
	reloaded, err := client.Users().Get(txCtx, user)
	if err != nil {
		t.Fatalf("re-reading the parent: %v", err)
	}
	if reloaded.Name != "Savepoint Updated" {
		t.Errorf("Name = %q, want step 2's value — the failed upsert's parent write survived its savepoint", reloaded.Name)
	}
	events, err := userEventActions(txCtx, client, user)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"savepoint-created", "savepoint-orphan"}, events); diff != "" {
		t.Errorf("UserEvents after the failed step (-want +got):\n%s\nThe `clear` that ran before the failing `connect` was not rolled back.", diff)
	}

	// The caller's transaction is still usable.
	if _, err := client.UserEvents().Create(txCtx, &models.CreateUserEventInput{Action: "savepoint-after", UserID: omittable.Set(&user)}); err != nil {
		t.Fatalf("writing after the rolled-back savepoint: %v", err)
	}
	if err := client.Commit(txCtx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	committed = true

	events, err = userEventActions(ctx, client, user)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"savepoint-after", "savepoint-created", "savepoint-orphan"}, events); diff != "" {
		t.Errorf("UserEvents after commit (-want +got):\n%s", diff)
	}
	orders, err := client.Orders().GetMany(ctx, &models.GetOrdersInput{Filter: &models.OrderFilter{UserID: &comparator.Number[int64]{Eq: new(user)}}, Limit: new(0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Errorf("Orders after commit = %d rows, want step 2's one", len(orders))
	}
}
