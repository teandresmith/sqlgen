package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// Go `CreateWithRelated` (PRD §9.9).
//
// The eligibility matrix (§9.9.3) is asserted at codegen time in
// cmd/sqlgen/gen/nested_test.go; this file asserts what only a real database
// can answer — that the rows land with the right foreign keys, that the
// visibility read is what separates NOT_FOUND from CONFLICT, that the whole
// set is one transaction, and that the query count does not scale with the
// number of children.
//
// `users` carries the three list shapes (O2M on a nullable FK, O2M on a NOT
// NULL FK, M2M on an int64-PK target), `assets` carries the has-one edge over a
// NOT NULL FK and the UUID-PK M2M, and `workspace_notes` is the polymorphic +
// self-referential + tenanted one. `users.Badge`, the has-one over a nullable
// FK, is exercised through the API in nested_mutations_graphql_test.go.

func nestedCtx() context.Context { return context.Background() }

// nestedOccurredAt is a fixed timestamp so nested event rows are comparable
// across runs. `events.occurred_at` is NOT NULL with no default, so it is a
// required field on the nested child input too.
func nestedOccurredAt() types.DateTime {
	return types.DateTime{Time: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
}

// seedOrphanEvent inserts an event with a NULL user_id — the only shape an
// O2M `connect` can adopt (PRD §9.9.3: connect adopts unparented rows).
func seedOrphanEvent(t *testing.T, action string) uuid.UUID {
	t.Helper()
	ev, err := testClient.Events().Create(nestedCtx(), &models.CreateEventInput{
		Action:     action,
		OccurredAt: nestedOccurredAt(),
	})
	if err != nil {
		t.Fatalf("seeding orphan event: %v", err)
	}
	return ev.ID
}

func seedCategoryRow(t *testing.T, name string) int64 {
	t.Helper()
	cat, err := testClient.Categories().Create(nestedCtx(), &models.CreateCategoryInput{Name: name})
	if err != nil {
		t.Fatalf("seeding category: %v", err)
	}
	return cat.ID
}

func seedDocumentRow(t *testing.T, name string, entity uuid.UUID, kind models.DocumentEntityTypeEnum) uuid.UUID {
	t.Helper()
	doc, err := testClient.Documents().Create(nestedCtx(), &models.CreateDocumentInput{
		EntityID:   entity,
		EntityType: kind,
		Name:       name,
	})
	if err != nil {
		t.Fatalf("seeding document: %v", err)
	}
	return doc.ID
}

// TestCreateWithRelated_AllFourShapes writes one parent and every §9.9.1 write
// shape that nests, in a single call, and reads each side back through the
// relationship loaders rather than through the method's own return value — so
// the assertion is about rows in the database, not about what the method chose
// to hand back.
func TestCreateWithRelated_AllFourShapes(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	orphan := seedOrphanEvent(t, "adopted")
	existingCat := seedCategoryRow(t, "existing-topic")

	created, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User: models.CreateUserInput{Email: "shapes@example.com", Name: "Shapes"},
		Events: &models.UserEventsCreateNested{
			Create: []*models.UserEventsCreateInput{
				{Action: "signup", OccurredAt: nestedOccurredAt()},
				{Action: "login", OccurredAt: nestedOccurredAt()},
			},
			Connect: []uuid.UUID{orphan},
		},
		Orders: &models.UserOrdersCreateNested{
			Create: []*models.UserOrdersCreateInput{
				{Notes: omittable.Set(new("first"))},
				{Notes: omittable.Set(new("second"))},
			},
		},
		Categories: &models.UserCategoriesCreateNested{
			Create:  []*models.CreateCategoryInput{{Name: "new-topic"}},
			Connect: []int64{existingCat},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}

	reloaded, err := testClient.Users().Get(ctx, created.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:         true,
			Events:     &models.EventRelationshipOptions{FieldOptions: &models.EventFieldOptions{ID: true, Action: true}},
			Orders:     &models.OrderRelationshipOptions{FieldOptions: &models.OrderFieldOptions{ID: true, Notes: true}},
			Categories: &models.CategoryRelationshipOptions{FieldOptions: &models.CategoryFieldOptions{ID: true, Name: true}},
		}
	})
	if err != nil {
		t.Fatalf("reloading parent: %v", err)
	}

	// O2M on a NULLABLE FK — two created plus the adopted orphan.
	if got := len(reloaded.Events); got != 3 {
		t.Errorf("Events = %d rows, want 3 (2 created + 1 connected)", got)
	}
	var sawAdopted bool
	for _, ev := range reloaded.Events {
		if ev.ID == orphan {
			sawAdopted = true
		}
	}
	if !sawAdopted {
		t.Errorf("the connected event %v is not parented to the new user; the adoption UPDATE did not land", orphan)
	}

	// O2M on a NOT NULL FK — create only, the FK assigned bare.
	if got := len(reloaded.Orders); got != 2 {
		t.Errorf("Orders = %d rows, want 2", got)
	}

	// M2M — one created target plus one connected, both linked through the
	// junction by the §9.9.6 link step.
	if got := len(reloaded.Categories); got != 2 {
		t.Errorf("Categories = %d rows, want 2 (1 created + 1 connected)", got)
	}
	var sawExisting bool
	for _, c := range reloaded.Categories {
		if c.ID == existingCat {
			sawExisting = true
		}
	}
	if !sawExisting {
		t.Errorf("the connected category %d has no junction row; the link step did not run", existingCat)
	}
}

// TestCreateWithRelated_HasOneAndUUIDManyToMany covers the two shapes `users`
// cannot reach: the has-one edge, whose `create` takes a pointer and writes one
// child through the target's own Create, and the M2M edges with a UUID-PK
// target — the only ones in any fixture, which is why the ticket names them.
func TestCreateWithRelated_HasOneAndUUIDManyToMany(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	standalone := seedDocumentRow(t, "standalone", uuid.New(), models.DocumentEntityTypeEnumSpv)

	asset, err := testClient.Assets().CreateWithRelated(ctx, &models.CreateAssetWithRelatedInput{
		Asset: models.CreateAssetInput{Name: "asset-with-related"},
		PrimaryDocument: &models.AssetPrimaryDocumentCreateNested{
			Create: &models.AssetPrimaryDocumentCreateInput{Name: "the-primary"},
		},
		Attachments: &models.AssetAttachmentsCreateNested{
			Create: []*models.AssetAttachmentsCreateInput{{Name: "attachment-a"}},
		},
		Documents: &models.AssetDocumentsCreateNested{
			Create: []*models.CreateDocumentInput{{
				EntityID:   uuid.New(),
				EntityType: models.DocumentEntityTypeEnumSpv,
				Name:       "linked-new",
			}},
			Connect: []uuid.UUID{standalone},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}

	reloaded, err := testClient.Assets().Get(ctx, asset.ID, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{
			ID:              true,
			PrimaryDocument: &models.DocumentFieldOptions{ID: true, Name: true, EntityType: true},
			Attachments:     &models.DocumentRelationshipOptions{FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true, EntityType: true}},
			Documents:       &models.DocumentRelationshipOptions{FieldOptions: &models.DocumentFieldOptions{ID: true, Name: true}},
		}
	})
	if err != nil {
		t.Fatalf("reloading asset: %v", err)
	}

	// has-one — one child, reachable only because the executor set entity_id
	// from the parent's key and entity_type from the edge's discriminator.
	if reloaded.PrimaryDocument == nil {
		t.Fatal("PrimaryDocument is nil; the has-one nested create did not land, or the discriminator was not set")
	}
	if reloaded.PrimaryDocument.Name != "the-primary" {
		t.Errorf("PrimaryDocument.Name = %q, want %q", reloaded.PrimaryDocument.Name, "the-primary")
	}
	// The discriminator carries the edge's declared value.
	if got, want := reloaded.PrimaryDocument.EntityType, models.DocumentEntityTypeEnumAssetprimary; got != want {
		t.Errorf("PrimaryDocument.EntityType = %q, want %q — the edge sets it", got, want)
	}
	if len(reloaded.Attachments) != 1 {
		t.Fatalf("Attachments = %d rows, want 1", len(reloaded.Attachments))
	}
	if got, want := reloaded.Attachments[0].EntityType, models.DocumentEntityTypeEnumAssetattachment; got != want {
		t.Errorf("Attachments[0].EntityType = %q, want %q — two edges into one table get two discriminator values", got, want)
	}

	// M2M on a UUID-PK target — one created, one connected.
	if got := len(reloaded.Documents); got != 2 {
		t.Errorf("Documents = %d rows, want 2 (1 created + 1 connected)", got)
	}

	// The mirror edge, Document.Assets, links the same junction from the other
	// side and is the second UUID-PK M2M the ticket names.
	doc, err := testClient.Documents().CreateWithRelated(ctx, &models.CreateDocumentWithRelatedInput{
		Document: models.CreateDocumentInput{
			EntityID:   uuid.New(),
			EntityType: models.DocumentEntityTypeEnumSpv,
			Name:       "reverse-linked",
		},
		Assets: &models.DocumentAssetsCreateNested{
			Connect: []uuid.UUID{asset.ID},
		},
	})
	if err != nil {
		t.Fatalf("Documents().CreateWithRelated: %v", err)
	}
	reloadedDoc, err := testClient.Documents().Get(ctx, doc.ID, func(o *models.CallOptions[models.DocumentFieldOptions]) {
		o.FieldOptions = &models.DocumentFieldOptions{
			ID:     true,
			Assets: &models.AssetRelationshipOptions{FieldOptions: &models.AssetFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("reloading document: %v", err)
	}
	if len(reloadedDoc.Assets) != 1 || reloadedDoc.Assets[0].ID != asset.ID {
		t.Errorf("Document.Assets = %v, want exactly the asset %v", reloadedDoc.Assets, asset.ID)
	}
}

// TestCreateWithRelated_ConnectAttribution pins §9.9.8's two connect outcomes
// and the structured error that carries them. The visibility read is what makes
// the split possible at all: without it the UPDATE's row count cannot separate
// "no such row" from "already parented".
func TestCreateWithRelated_ConnectAttribution(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	t.Run("a target that is not visible is NOT_FOUND, never CONFLICT", func(t *testing.T) {
		missing := uuid.New()
		_, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User:   models.CreateUserInput{Email: "miss@example.com", Name: "Miss"},
			Events: &models.UserEventsCreateNested{Connect: []uuid.UUID{missing}},
		})
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound — mapping a visibility miss to a conflict would confirm the row exists", err)
		}
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) {
			t.Fatalf("err = %v, want a *NestedMutationError carrying the edge and verb", err)
		}
		if nested.Edge != "Events" || nested.Verb != "connect" {
			t.Errorf("attribution = %s/%s, want Events/connect", nested.Edge, nested.Verb)
		}
		if got, ok := nested.ID.(uuid.UUID); !ok || got != missing {
			t.Errorf("NestedMutationError.ID = %v, want the offending target id %v", nested.ID, missing)
		}
	})

	t.Run("a target parented elsewhere is ErrAlreadyRelated", func(t *testing.T) {
		owner, err := testClient.Users().Create(ctx, &models.CreateUserInput{Email: "owner@example.com", Name: "Owner"})
		if err != nil {
			t.Fatalf("seeding owner: %v", err)
		}
		taken, err := testClient.Events().Create(ctx, &models.CreateEventInput{
			Action:     "taken",
			OccurredAt: nestedOccurredAt(),
			UserID:     omittable.Set(&owner.ID),
		})
		if err != nil {
			t.Fatalf("seeding parented event: %v", err)
		}

		_, err = testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User:   models.CreateUserInput{Email: "thief@example.com", Name: "Thief"},
			Events: &models.UserEventsCreateNested{Connect: []uuid.UUID{taken.ID}},
		})
		if !errors.Is(err, models.ErrAlreadyRelated) {
			t.Fatalf("err = %v, want ErrAlreadyRelated — connect adopts only unparented rows", err)
		}

		// And the row stayed where it was: the whole call rolled back.
		still, err := testClient.Events().Get(ctx, taken.ID)
		if err != nil {
			t.Fatalf("re-reading the contested event: %v", err)
		}
		if still.UserID == nil || *still.UserID != owner.ID {
			t.Errorf("events.user_id = %v, want it still parented to %v", still.UserID, owner.ID)
		}
	})
}

// TestCreateWithRelated_RepeatedConnectIDIsOneAdoption pins that naming one
// target twice on a `connect` is one adoption rather than two.
//
// `connect` is a set operation — naming a target twice states the same end
// state twice — but the adoption is verified by comparing the rows its verify
// read finds under the parent against the queued id count, and the UPDATE and
// that read each match the row once however many times the caller named it
// (the check counted the UPDATE's own rows when this pin was written, with the
// same result). Without a dedupe the shortfall check
// fires and reports ErrAlreadyRelated against a row that has no other parent:
// the "fewer rows updated than requested → conflict" collapse PRD §9.9.6
// forbids, arriving through the caller's input instead of through the guard.
//
// Verified failing-first: with the `queued` dedupe removed from nested.go.tmpl,
// the first subtest returns ErrAlreadyRelated naming no id.
func TestCreateWithRelated_RepeatedConnectIDIsOneAdoption(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	t.Run("a repeated id adopts once and does not report a conflict", func(t *testing.T) {
		orphan := seedOrphanEvent(t, "repeated")

		parent, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User:   models.CreateUserInput{Email: "repeat@example.com", Name: "Repeat"},
			Events: &models.UserEventsCreateNested{Connect: []uuid.UUID{orphan, orphan}},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated: %v — a repeated connect id is the same end state stated twice, not a conflict", err)
		}

		adopted, err := testClient.Events().Get(ctx, orphan)
		if err != nil {
			t.Fatalf("re-reading the adopted event: %v", err)
		}
		if adopted.UserID == nil || *adopted.UserID != parent.ID {
			t.Errorf("events.user_id = %v, want %v", adopted.UserID, parent.ID)
		}
	})

	t.Run("a repeated id still conflicts when the row is parented elsewhere", func(t *testing.T) {
		owner, err := testClient.Users().Create(ctx, &models.CreateUserInput{Email: "dupowner@example.com", Name: "DupOwner"})
		if err != nil {
			t.Fatalf("seeding owner: %v", err)
		}
		taken, err := testClient.Events().Create(ctx, &models.CreateEventInput{
			Action:     "dup-taken",
			OccurredAt: nestedOccurredAt(),
			UserID:     omittable.Set(&owner.ID),
		})
		if err != nil {
			t.Fatalf("seeding parented event: %v", err)
		}

		_, err = testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User:   models.CreateUserInput{Email: "dupthief@example.com", Name: "DupThief"},
			Events: &models.UserEventsCreateNested{Connect: []uuid.UUID{taken.ID, taken.ID}},
		})
		if !errors.Is(err, models.ErrAlreadyRelated) {
			t.Fatalf("err = %v, want ErrAlreadyRelated — the dedupe must not swallow a real conflict", err)
		}
		// And it names the offending id, which the shortfall branch cannot do.
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) || nested.ID == nil {
			t.Errorf("err = %v, want a *NestedMutationError naming the offending target", err)
		}
	})
}

// TestCreateWithRelated_RollsBackTheWholeSet pins the §9.9 transaction
// property. The parent row and the children that did land before the failure
// are both gone — the whole set runs in one transaction, so a nested failure
// cannot leave a half-written parent behind.
func TestCreateWithRelated_RollsBackTheWholeSet(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	_, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User: models.CreateUserInput{Email: "rollback@example.com", Name: "Rollback"},
		Events: &models.UserEventsCreateNested{
			Create:  []*models.UserEventsCreateInput{{Action: "before-the-failure", OccurredAt: nestedOccurredAt()}},
			Connect: []uuid.UUID{uuid.New()}, // not visible → the call fails after the create landed
		},
	})
	if err == nil {
		t.Fatal("CreateWithRelated succeeded with an unresolvable connect")
	}

	users, err := testClient.Users().GetMany(ctx, &models.GetUsersInput{})
	if err != nil {
		t.Fatalf("counting users: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("%d user rows survived a failed nested create; the parent write was not rolled back", len(users))
	}
	events, err := testClient.Events().GetMany(ctx, &models.GetEventsInput{})
	if err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("%d event rows survived a failed nested create; the child write was not rolled back", len(events))
	}
}

// TestCreateWithRelated_NilFieldOptionsLeavesRelationshipsUnpopulated pins that
// a nil FieldOptions leaves the relationship members unpopulated.
// "nil means everything" is the right mental model for *columns* and the wrong
// one here: the terminal re-read is gated on the caller having selected a
// relationship, and a nil selection selects none — so the parent comes back
// with its members unpopulated, on the edges the call just wrote as well as
// every other.
func TestCreateWithRelated_NilFieldOptionsLeavesRelationshipsUnpopulated(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	got, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User: models.CreateUserInput{Email: "nil-field-options@example.com", Name: "Nil Field Options"},
		Events: &models.UserEventsCreateNested{
			Create: []*models.UserEventsCreateInput{{Action: "written", OccurredAt: nestedOccurredAt()}},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}
	if got.Events != nil {
		t.Errorf("Events = %v with a nil FieldOptions, want nil", got.Events)
	}
	// Columns, by contrast, are all selected: nil means "every column".
	if got.Email != "nil-field-options@example.com" {
		t.Errorf("Email = %q, want the created value — a nil FieldOptions still selects every column", got.Email)
	}

	// Selecting a relationship earns the terminal re-read, and then the member
	// is populated with the rows the call just wrote.
	withRel, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User: models.CreateUserInput{Email: "selected-field-options@example.com", Name: "Selected Field Options"},
		Events: &models.UserEventsCreateNested{
			Create: []*models.UserEventsCreateInput{{Action: "written", OccurredAt: nestedOccurredAt()}},
		},
	}, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:     true,
			Events: &models.EventRelationshipOptions{FieldOptions: &models.EventFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("CreateWithRelated with a relationship selected: %v", err)
	}
	if len(withRel.Events) != 1 {
		t.Errorf("Events = %d rows after selecting the relationship, want 1", len(withRel.Events))
	}
}

// TestCreateWithRelated_QueryCountDoesNotScaleWithChildren pins §9.9.7:
// `ceil(N / batch_size)` statements per participating table per verb, plus a
// bounded set of reads — never a per-row read.
//
// The assertion is the invariant rather than a fixed total, because the total
// is a sum over whatever the inner clients happen to issue and would move on an
// unrelated change to any of them. Two runs of the same call shape with ten
// times the children must cost the same, and only a per-row read breaks that.
//
// The counter is a pgx tracer rather than a `countingQuerier` shim. A
// database.Querier shim cannot see inside a transaction — database.Conn hands
// the body the *database.Tx, which holds the driver connection the shim
// delegated to — so both runs would measure exactly 1 (the Begin) and the
// comparison would hold vacuously. See statement_counter_test.go.
func TestCreateWithRelated_QueryCountDoesNotScaleWithChildren(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	client, counter := tracedClient(t)

	run := func(t *testing.T, label string, n int) int {
		t.Helper()
		creates := make([]*models.UserEventsCreateInput, 0, n)
		orders := make([]*models.UserOrdersCreateInput, 0, n)
		cats := make([]*models.CreateCategoryInput, 0, n)
		for i := range n {
			creates = append(creates, &models.UserEventsCreateInput{Action: "bulk", OccurredAt: nestedOccurredAt()})
			orders = append(orders, &models.UserOrdersCreateInput{})
			cats = append(cats, &models.CreateCategoryInput{Name: label + "-cat-" + string(rune('a'+i))})
		}
		connect := make([]uuid.UUID, 0, n)
		for range n {
			connect = append(connect, seedOrphanEvent(t, "bulk-orphan"))
		}

		counter.reset()
		if _, err := client.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
			User:       models.CreateUserInput{Email: label + "@example.com", Name: label},
			Events:     &models.UserEventsCreateNested{Create: creates, Connect: connect},
			Orders:     &models.UserOrdersCreateNested{Create: orders},
			Categories: &models.UserCategoriesCreateNested{Create: cats},
		}); err != nil {
			t.Fatalf("CreateWithRelated(%s): %v", label, err)
		}
		return counter.count()
	}

	small := run(t, "small", 2)
	large := run(t, "large", 20)
	if small != large {
		t.Errorf("query count scaled with the child count: %d for 2 children, %d for 20. §9.9.7 forbids a per-row read", small, large)
	}
	// A loose ceiling, so a regression that adds a whole extra round-trip per
	// verb is still caught even though it would keep the two runs equal. The
	// count now includes the transaction's own BEGIN and COMMIT, which the
	// Querier-level shim never saw.
	if small > 25 {
		t.Errorf("a nested create over three edges cost %d statements; the §9.9.7 bound is a handful per verb", small)
	}
	if small < 5 {
		t.Fatalf("the counter saw only %d statements for a three-edge nested create; it is not observing the transaction body", small)
	}
}

// TestCreateWithRelated_ComposesWithACallerTransaction pins PRD §18.3 for the
// nested surface: a consumer may call `…WithRelated` inside their own
// `Begin` / `WithTx` block, and it nests as a savepoint rather than opening a
// second transaction.
//
// The name `CreateWithRelated` hands database.WithTransaction doubles as the
// SAVEPOINT identifier the moment ctx already carries a transaction, so a prose
// name with spaces in it works at the top level — where it is only a label —
// and hard-errors on every nested call, before any statement runs. Nothing else
// catches that: the top-level path, which every other test in this file takes,
// never renders the name into SQL.
func TestCreateWithRelated_ComposesWithACallerTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("commits with the caller's transaction", func(t *testing.T) {
		var created *models.User
		err := testClient.WithTx(ctx, "outer_commit", func(txCtx context.Context) error {
			var err error
			created, err = testClient.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
				User:   models.CreateUserInput{Email: "nested-tx-commit@example.com", Name: "nested tx"},
				Orders: &models.UserOrdersCreateNested{Create: []*models.UserOrdersCreateInput{{}}},
			})
			return err
		})
		if err != nil {
			t.Fatalf("WithTx(CreateWithRelated): %v", err)
		}
		if _, err := testClient.Users().Get(ctx, created.ID); err != nil {
			t.Errorf("parent absent after the caller's transaction committed: %v", err)
		}
	})

	t.Run("rolls back with the caller's transaction", func(t *testing.T) {
		sentinel := errors.New("caller aborted")
		var created *models.User
		err := testClient.WithTx(ctx, "outer_rollback", func(txCtx context.Context) error {
			var err error
			created, err = testClient.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
				User:   models.CreateUserInput{Email: "nested-tx-rollback@example.com", Name: "nested tx"},
				Orders: &models.UserOrdersCreateNested{Create: []*models.UserOrdersCreateInput{{}}},
			})
			if err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx returned %v, want the caller's sentinel", err)
		}
		if _, err := testClient.Users().Get(ctx, created.ID); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("parent survived the caller's rollback: got %v, want ErrNotFound", err)
		}
	})

	t.Run("a failed nested write rolls back to the savepoint only", func(t *testing.T) {
		var survivor *models.User
		err := testClient.WithTx(ctx, "outer_savepoint", func(txCtx context.Context) error {
			_, err := testClient.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
				User: models.CreateUserInput{Email: "nested-tx-doomed@example.com", Name: "doomed"},
				Events: &models.UserEventsCreateNested{
					Connect: []uuid.UUID{uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")},
				},
			})
			if err == nil {
				return errors.New("expected the unresolvable connect to fail the nested write")
			}
			if !errors.Is(err, models.ErrNotFound) {
				return fmt.Errorf("nested write failed for the wrong reason: %w", err)
			}
			// The savepoint absorbed the failure; the outer transaction is
			// still usable, which is the half a new-transaction-per-call
			// implementation cannot provide.
			survivor, err = testClient.Users().Create(txCtx, &models.CreateUserInput{
				Email: "nested-tx-survivor@example.com", Name: "survivor",
			})
			return err
		})
		if err != nil {
			t.Fatalf("outer transaction did not survive the nested failure: %v", err)
		}
		if _, err := testClient.Users().Get(ctx, survivor.ID); err != nil {
			t.Errorf("row written after the savepoint rollback is absent: %v", err)
		}
	})

	t.Run("two nested writes nest sequentially in one transaction", func(t *testing.T) {
		err := testClient.WithTx(ctx, "outer_two", func(txCtx context.Context) error {
			for _, label := range []string{"first", "second"} {
				if _, err := testClient.Users().CreateWithRelated(txCtx, &models.CreateUserWithRelatedInput{
					User:   models.CreateUserInput{Email: "nested-tx-" + label + "@example.com", Name: label},
					Orders: &models.UserOrdersCreateNested{Create: []*models.UserOrdersCreateInput{{}}},
				}); err != nil {
					return fmt.Errorf("%s: %w", label, err)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("two sequential nested writes in one transaction: %v", err)
		}
	})
}
