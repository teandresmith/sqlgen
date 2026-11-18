package tests

import (
	"context"
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
// can answer — that the rows land with the right foreign keys, and that the
// query count does not scale with the number of children.
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
