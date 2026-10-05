package tests

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// Go `UpdateWithRelated` + `UpsertWithRelated` (PRD §9.9).
//
// The create half is pinned next door; this file is the two verbs that only
// exist once the parent does. `disconnect` and `clear` are where the emitted
// SQL can be wrong in ways a compiler cannot see: an unlink that omits the
// column instead of setting it NULL matches its rows and reports success,
// and a `clear` on one polymorphic edge that carries no discriminator
// term unlinks every sibling edge's rows over the same foreign key.
//
// `users` is the three-shape parent, `workspace_notes` the polymorphic +
// self-referential + tenanted one.

// seedParentedEvent inserts an event already parented to owner — the shape a
// `disconnect` unlinks and a `connect` refuses as already related.
func seedParentedEvent(t *testing.T, owner uuid.UUID, action string) uuid.UUID {
	t.Helper()
	ev, err := testClient.Events().Create(nestedCtx(), &models.CreateEventInput{
		Action:     action,
		OccurredAt: nestedOccurredAt(),
		UserID:     omittable.Set(&owner),
	})
	if err != nil {
		t.Fatalf("seeding parented event: %v", err)
	}
	return ev.ID
}

// seedUserRow inserts a bare parent for the update-side calls to act on. It is
// the Go-client sibling of curated_surface_test.go's seedUser, which goes
// through the GraphQL surface.
func seedUserRow(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := testClient.Users().Create(nestedCtx(), &models.CreateUserInput{Email: email, Name: email})
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return u.ID
}

// eventIDs reads the ids currently on a user's Events edge, which is the only
// way to assert on a set rather than on a row the method chose to return.
func eventIDs(t *testing.T, user uuid.UUID) map[uuid.UUID]bool {
	t.Helper()
	rows, err := testClient.Events().GetMany(nestedCtx(), &models.GetEventsInput{Limit: new(0)})
	if err != nil {
		t.Fatalf("reading events: %v", err)
	}
	out := make(map[uuid.UUID]bool, len(rows))
	for _, ev := range rows {
		if ev.UserID != nil && *ev.UserID == user {
			out[ev.ID] = true
		}
	}
	return out
}

// TestUpdateWithRelated_EveryMatrixCell walks §9.9.3 on the update side: the
// three verb-complete shapes each take `create`, `connect`, `disconnect` and
// `clear`, and the NOT NULL edge takes `create` alone.
//
// The NOT NULL edge's missing verbs are compile-time facts —
// `UserOrdersUpdateNested` declares no `Disconnect` or `Clear` member at all,
// which is what makes the row in the matrix a type rather than a runtime check
// — so the reference to it below is the assertion.
//
// The edges are read back through a separate Get *outside* the nested call, the
// way TestCreateWithRelated_AllFourShapes reads its own: the assertion is about
// rows in the database rather than about what the method chose to hand back.
// Selecting the edges on the nested call itself is now a supported spelling too
// — §18.5's connection reservation serializes §9.9.6's terminal re-read against
// the transaction's single connection, so it may fan out freely — and
// relationship_tx_fanout_test.go covers that surface. This test stays with the
// outside read for its own reason: it asserts sets of rows, not a returned
// object.
func TestUpdateWithRelated_EveryMatrixCell(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "matrix@example.com")
	linked := seedParentedEvent(t, user, "linked")
	orphan := seedOrphanEvent(t, "matrix-orphan")
	existingCat := seedCategoryRow(t, "matrix-topic")

	linkedCat, err := testClient.Categories().Create(ctx, &models.CreateCategoryInput{Name: "matrix-linked"})
	if err != nil {
		t.Fatalf("seeding category: %v", err)
	}
	if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		Categories: &models.UserCategoriesUpdateNested{Connect: []int64{linkedCat.ID}},
	}); err != nil {
		t.Fatalf("seeding the M2M link: %v", err)
	}

	updated, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		User: models.UpdateUserInput{Name: omittable.Set("Matrix")},
		Events: &models.UserEventsUpdateNested{
			Create:     []*models.UserEventsCreateInput{{Action: "fresh", OccurredAt: nestedOccurredAt()}},
			Connect:    []uuid.UUID{orphan},
			Disconnect: []uuid.UUID{linked},
		},
		// The NOT NULL edge's block carries `create` and nothing else.
		Orders: &models.UserOrdersUpdateNested{
			Create: []*models.UserOrdersCreateInput{{Notes: omittable.Set(new("matrix"))}},
		},
		Categories: &models.UserCategoriesUpdateNested{
			Create:     []*models.CreateCategoryInput{{Name: "matrix-new"}},
			Connect:    []int64{existingCat},
			Disconnect: []int64{linkedCat.ID},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWithRelated: %v", err)
	}

	// The parent's own columns updated alongside the nested rows.
	if updated.Name != "Matrix" {
		t.Errorf("Name = %q, want %q — the flat half of the wrapper did not reach Update", updated.Name, "Matrix")
	}

	reloaded, err := testClient.Users().Get(ctx, user, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:         true,
			Events:     &models.EventRelationshipOptions{FieldOptions: &models.EventFieldOptions{ID: true}},
			Orders:     &models.OrderRelationshipOptions{FieldOptions: &models.OrderFieldOptions{ID: true}},
			Categories: &models.CategoryRelationshipOptions{FieldOptions: &models.CategoryFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("reloading the parent: %v", err)
	}

	// Events: one created, one adopted, the pre-existing one unlinked.
	if len(reloaded.Events) != 2 {
		t.Errorf("Events = %d rows, want 2 (1 created + 1 connected, 1 disconnected)", len(reloaded.Events))
	}
	for _, ev := range reloaded.Events {
		if ev.ID == linked {
			t.Errorf("the disconnected event %v is still on the edge", linked)
		}
	}
	if !eventIDs(t, user)[orphan] {
		t.Errorf("the connected event %v is not parented to the user", orphan)
	}

	if len(reloaded.Orders) != 1 {
		t.Errorf("Orders = %d rows, want 1", len(updated.Orders))
	}

	// Categories: one created, one connected, the pre-existing link deleted.
	if len(reloaded.Categories) != 2 {
		t.Errorf("Categories = %d rows, want 2 (1 created + 1 connected, 1 disconnected)", len(reloaded.Categories))
	}
	for _, c := range reloaded.Categories {
		if c.ID == linkedCat.ID {
			t.Errorf("the disconnected category %d still has a junction row", linkedCat.ID)
		}
	}
	// The unlink deleted the junction row, not the target: `disconnect` means
	// unlink and never destroy.
	if _, err := testClient.Categories().Get(ctx, linkedCat.ID); err != nil {
		t.Errorf("the disconnected category row itself was destroyed: %v", err)
	}
}

// TestUpdateWithRelated_UnlinkActuallyNullsTheFK is a runtime pin, and it
// is the one assertion in this file that cannot be made any other way.
//
// `omittable.Set[*uuid.UUID](nil)` sets the column NULL; the zero
// `omittable.Value[*uuid.UUID]{}` omits it from the SET list. Because
// nullable UUIDs bind to `*uuid.UUID`, the compiler accepts both in that
// position, and the wrong one matches its rows, reports success and unlinks
// nothing — so this reads the column back rather than trusting the call.
//
// Verified failing-first: with the assignment swapped for the zero value in
// nested.go.tmpl, both subtests report the FK still pointing at the parent
// while UpdateWithRelated returns nil.
func TestUpdateWithRelated_UnlinkActuallyNullsTheFK(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	assertNull := func(t *testing.T, id uuid.UUID) {
		t.Helper()
		row, err := testClient.Events().Get(ctx, id)
		if err != nil {
			t.Fatalf("re-reading the unlinked event: %v", err)
		}
		if row.UserID != nil {
			t.Errorf("events.user_id = %v, want NULL — the unlink omitted the column from the SET list instead of setting it", *row.UserID)
		}
	}

	t.Run("disconnect", func(t *testing.T) {
		user := seedUserRow(t, "unlink-disconnect@example.com")
		child := seedParentedEvent(t, user, "unlink-disconnect")
		if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{Disconnect: []uuid.UUID{child}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		assertNull(t, child)
	})

	t.Run("clear", func(t *testing.T) {
		user := seedUserRow(t, "unlink-clear@example.com")
		child := seedParentedEvent(t, user, "unlink-clear")
		if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		assertNull(t, child)
	})
}

// TestUpdateWithRelated_ClearReplacesTheSet pins the `clear` verb's payoff:
// `clear` plus `connect` is exactly "the set is now this", which is what makes
// a `set` verb unnecessary. It only holds because `clear` runs FIRST — running
// it after the linking verbs would unlink what they had just linked and leave
// the edge empty.
func TestUpdateWithRelated_ClearReplacesTheSet(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "clear-set@example.com")
	old1 := seedParentedEvent(t, user, "old-1")
	old2 := seedParentedEvent(t, user, "old-2")
	fresh := seedOrphanEvent(t, "fresh")

	if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		Events: &models.UserEventsUpdateNested{
			Clear:   true,
			Connect: []uuid.UUID{fresh},
			Create:  []*models.UserEventsCreateInput{{Action: "minted", OccurredAt: nestedOccurredAt()}},
		},
	}); err != nil {
		t.Fatalf("UpdateWithRelated: %v", err)
	}

	got := eventIDs(t, user)
	if len(got) != 2 {
		t.Errorf("the edge holds %d rows after clear + connect + create, want exactly 2", len(got))
	}
	if !got[fresh] {
		t.Error("the connected event is absent; `clear` ran after the linking verbs and unlinked it")
	}
	for _, gone := range []uuid.UUID{old1, old2} {
		if got[gone] {
			t.Errorf("the pre-existing event %v survived the clear", gone)
		}
	}
}

// TestUpdateWithRelated_ClearSupersedesDisconnect pins the `clear` rule's
// second half: with `clear` set, `disconnect` on the same edge is ignored
// rather than rejected. Every id it names is already unlinked, so the caller's
// requested end state holds — the same reasoning as `connect`'s already-ours
// no-op.
//
// The id it names is one the verb-conflict check would otherwise refuse, which
// is what makes this a test of the skip rather than of the statement.
func TestUpdateWithRelated_ClearSupersedesDisconnect(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "clear-supersedes@example.com")
	orphan := seedOrphanEvent(t, "clear-supersedes")

	if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		Events: &models.UserEventsUpdateNested{
			Clear:      true,
			Connect:    []uuid.UUID{orphan},
			Disconnect: []uuid.UUID{orphan},
		},
	}); err != nil {
		t.Fatalf("UpdateWithRelated: %v — under `clear` a disconnect is ignored, so it cannot contradict the connect", err)
	}
	if !eventIDs(t, user)[orphan] {
		t.Error("the connected event is absent; the ignored disconnect ran anyway")
	}
}

// TestUpdateWithRelated_DisconnectIsParentScoped pins the asymmetry the
// visibility read rests on: a `disconnect` performs no visibility read because
// it cannot need one — the filter carries `fk = parent`, so it is structurally
// incapable of touching another parent's rows, and a miss is a no-op rather
// than an error.
func TestUpdateWithRelated_DisconnectIsParentScoped(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	mine := seedUserRow(t, "scoped-mine@example.com")
	theirs := seedUserRow(t, "scoped-theirs@example.com")
	theirChild := seedParentedEvent(t, theirs, "not-mine")

	if _, err := testClient.Users().UpdateWithRelated(ctx, mine, &models.UpdateUserWithRelatedInput{
		Events: &models.UserEventsUpdateNested{Disconnect: []uuid.UUID{theirChild, uuid.New()}},
	}); err != nil {
		t.Fatalf("UpdateWithRelated: %v — a disconnect that matches nothing is a no-op, not an error", err)
	}

	row, err := testClient.Events().Get(ctx, theirChild)
	if err != nil {
		t.Fatalf("re-reading the other parent's event: %v", err)
	}
	if row.UserID == nil || *row.UserID != theirs {
		t.Errorf("events.user_id = %v, want it still parented to %v — a disconnect detached another parent's row", row.UserID, theirs)
	}
}

// TestUpdateWithRelated_ConnectThreeWaySplit pins O2M `connect`'s three-way
// outcome split on the update side, where all three are reachable on one parent.
//
// Collapsing the split to "fewer rows updated than requested → conflict"
// reports a typo'd or cross-tenant id as an existing relationship, and
// disagrees with M2M `connect`, which maps the identical caller mistake to
// NOT_FOUND.
func TestUpdateWithRelated_ConnectThreeWaySplit(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "three-way@example.com")
	other := seedUserRow(t, "three-way-other@example.com")

	t.Run("not visible is ErrNotFound", func(t *testing.T) {
		missing := uuid.New()
		_, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{missing}},
		})
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) || nested.Edge != "Events" || nested.Verb != "connect" {
			t.Errorf("attribution = %v, want Events/connect naming the id", err)
		}
	})

	t.Run("parented elsewhere is ErrAlreadyRelated", func(t *testing.T) {
		taken := seedParentedEvent(t, other, "three-way-taken")
		_, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{taken}},
		})
		if !errors.Is(err, models.ErrAlreadyRelated) {
			t.Fatalf("err = %v, want ErrAlreadyRelated", err)
		}
		row, err := testClient.Events().Get(ctx, taken)
		if err != nil {
			t.Fatalf("re-reading the contested event: %v", err)
		}
		if row.UserID == nil || *row.UserID != other {
			t.Errorf("events.user_id = %v, want it still parented to %v", row.UserID, other)
		}
	})

	t.Run("already ours is a no-op", func(t *testing.T) {
		ours := seedParentedEvent(t, user, "three-way-ours")
		if _, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{ours}},
		}); err != nil {
			t.Fatalf("connecting a row already on the edge: %v — the requested end state holds, so this is a no-op", err)
		}
		if !eventIDs(t, user)[ours] {
			t.Error("the already-ours event left the edge")
		}
	})
}

// TestUpdateWithRelated_VerbConflictIsRefusedBeforeAnyStatement pins the
// verb-conflict check. The order would decide the outcome and there is no
// defensible default, so the contradiction is refused rather than resolved —
// and refused before anything is written, which the untouched parent column
// proves.
//
// Verified failing-first: with the check removed from nested.go.tmpl, both
// subtests return `err = nil` — execution order resolves the contradiction
// silently, which is exactly what the check exists to stop.
func TestUpdateWithRelated_VerbConflictIsRefusedBeforeAnyStatement(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user := seedUserRow(t, "verb-conflict@example.com")
	child := seedParentedEvent(t, user, "verb-conflict-child")

	t.Run("connect and disconnect naming one target", func(t *testing.T) {
		_, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			User:   models.UpdateUserInput{Name: omittable.Set("should not land")},
			Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{child}, Disconnect: []uuid.UUID{child}},
		})
		if !errors.Is(err, models.ErrNestedVerbConflict) {
			t.Fatalf("err = %v, want ErrNestedVerbConflict", err)
		}
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) {
			t.Fatalf("err = %v, want a *NestedMutationError", err)
		}
		if nested.Edge != "Events" || nested.Verb != "disconnect" {
			t.Errorf("attribution = %s/%s, want Events/disconnect", nested.Edge, nested.Verb)
		}
		if got, ok := nested.ID.(uuid.UUID); !ok || got != child {
			t.Errorf("NestedMutationError.ID = %v, want the contested id %v", nested.ID, child)
		}

		// Nothing ran: the parent's own column is unchanged, and the child is
		// still linked.
		row, err := testClient.Users().Get(ctx, user)
		if err != nil {
			t.Fatalf("re-reading the parent: %v", err)
		}
		if row.Name == "should not land" {
			t.Error("the parent write ran before the verb-conflict check; §9.9.6 refuses before any statement")
		}
		if !eventIDs(t, user)[child] {
			t.Error("the child was unlinked by a call that returned a verb conflict")
		}
	})

	t.Run("a create carrying an explicit key that disconnect also names", func(t *testing.T) {
		id := uuid.New()
		_, err := testClient.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
			Events: &models.UserEventsUpdateNested{
				Create:     []*models.UserEventsCreateInput{{ID: omittable.Set(id), Action: "verb-conflict-create", OccurredAt: nestedOccurredAt()}},
				Disconnect: []uuid.UUID{id},
			},
		})
		if !errors.Is(err, models.ErrNestedVerbConflict) {
			t.Fatalf("err = %v, want ErrNestedVerbConflict — a create entry with an explicit key can contradict a disconnect", err)
		}
		if _, err := testClient.Events().Get(ctx, id); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("the nested create ran before the check: %v", err)
		}
	})
}

// TestUpdateWithRelated_SelfReferenceIsRefused pins the self-reference refusal
// on both linking verbs.
//
// Left unguarded, `connect: [self]` writes a row that is its own parent, which
// every recursive read walks forever. No other guard catches it: an unparented
// self is an adoptable orphan under `connect`'s own rule, and the verb-conflict
// check sees one verb naming one target.
//
// Verified failing-first on the disconnect half: with the guard removed the
// call returns `err = nil`, because the unlink's own `fk = parent` term matches
// nothing on a row that is not yet its own parent — so the refusal is the only
// thing that reports the caller's mistake.
func TestUpdateWithRelated_SelfReferenceIsRefused(t *testing.T) {
	truncateAll(t)
	client, ctx := nestedTenantedClient(t)

	self, err := client.WorkspaceNotes().Create(ctx, &models.CreateWorkspaceNoteInput{Body: "self-ref"})
	if err != nil {
		t.Fatalf("seeding note: %v", err)
	}

	for _, tt := range []struct {
		verb  string
		input *models.UpdateWorkspaceNoteWithRelatedInput
	}{
		{verb: "connect", input: &models.UpdateWorkspaceNoteWithRelatedInput{
			Children: &models.WorkspaceNoteChildrenUpdateNested{Connect: []uuid.UUID{self.ID}},
		}},
		{verb: "disconnect", input: &models.UpdateWorkspaceNoteWithRelatedInput{
			Children: &models.WorkspaceNoteChildrenUpdateNested{Disconnect: []uuid.UUID{self.ID}},
		}},
	} {
		t.Run(tt.verb, func(t *testing.T) {
			_, err := client.WorkspaceNotes().UpdateWithRelated(ctx, self.ID, tt.input)
			if !errors.Is(err, models.ErrNestedVerbConflict) {
				t.Fatalf("err = %v, want ErrNestedVerbConflict", err)
			}
			row, err := client.WorkspaceNotes().Get(ctx, self.ID)
			if err != nil {
				t.Fatalf("re-reading the note: %v", err)
			}
			if row.ParentID != nil {
				t.Errorf("workspace_notes.parent_id = %v, want NULL — the row became its own parent", *row.ParentID)
			}
		})
	}
}

// TestUpdateWithRelated_UnlinkScopesToTheEdgeDiscriminator is the runtime pin
// for discriminator ownership on the unlink half, and it is the sharpest
// failure the discriminator scoping guards against.
//
// `workspace_notes` carries two edges over one foreign key: `Children`, which
// is every child, and `DraftChildren`, which is the children whose `kind` is
// `draft`. A `clear` on `DraftChildren` must unlink the drafts and leave the
// published children where they are. Without the discriminator term in the
// filter it unlinks both — a silent over-unlink, reported as success, on rows
// the caller never named.
//
// Verified failing-first: with the `Kind:` term removed from the clear and the
// disconnect in nested.go.tmpl, both subtests report the published child
// detached.
func TestUpdateWithRelated_UnlinkScopesToTheEdgeDiscriminator(t *testing.T) {
	truncateAll(t)
	client, ctx := nestedTenantedClient(t)

	setup := func(t *testing.T, label string) (parent, draft, published uuid.UUID) {
		t.Helper()
		p, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: label},
			Children: &models.WorkspaceNoteChildrenCreateNested{
				Create: []*models.WorkspaceNoteChildrenCreateInput{
					{Body: label + "-published", Kind: omittable.Set(models.WorkspaceNoteKindEnumPublished)},
				},
			},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: label + "-draft"}},
			},
		}, func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
			o.FieldOptions = &models.WorkspaceNoteFieldOptions{
				ID: true,
				Children: &models.WorkspaceNoteRelationshipOptions{
					FieldOptions: &models.WorkspaceNoteFieldOptions{ID: true, Kind: true},
				},
			}
		})
		if err != nil {
			t.Fatalf("seeding the two-edge parent: %v", err)
		}
		if len(p.Children) != 2 {
			t.Fatalf("seeded %d children, want 2 (one of each kind)", len(p.Children))
		}
		for _, c := range p.Children {
			if c.Kind == models.WorkspaceNoteKindEnumDraft {
				draft = c.ID
			} else {
				published = c.ID
			}
		}
		return p.ID, draft, published
	}

	assertSplit := func(t *testing.T, parent, draft, published uuid.UUID) {
		t.Helper()
		for _, tt := range []struct {
			id       uuid.UUID
			label    string
			stillOur bool
		}{
			{id: draft, label: "draft", stillOur: false},
			{id: published, label: "published", stillOur: true},
		} {
			row, err := client.WorkspaceNotes().Get(ctx, tt.id)
			if err != nil {
				t.Fatalf("re-reading the %s child: %v", tt.label, err)
			}
			linked := row.ParentID != nil && *row.ParentID == parent
			if linked != tt.stillOur {
				t.Errorf("%s child: linked = %v, want %v — the unlink is scoped to the edge's own discriminator value",
					tt.label, linked, tt.stillOur)
			}
		}
	}

	t.Run("clear", func(t *testing.T) {
		parent, draft, published := setup(t, "discriminator-clear")
		if _, err := client.WorkspaceNotes().UpdateWithRelated(ctx, parent, &models.UpdateWorkspaceNoteWithRelatedInput{
			DraftChildren: &models.WorkspaceNoteDraftChildrenUpdateNested{Clear: true},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(clear): %v", err)
		}
		assertSplit(t, parent, draft, published)
	})

	t.Run("disconnect naming a row of another kind", func(t *testing.T) {
		parent, draft, published := setup(t, "discriminator-disconnect")
		// Both ids are named; only the one this edge can see may be unlinked.
		if _, err := client.WorkspaceNotes().UpdateWithRelated(ctx, parent, &models.UpdateWorkspaceNoteWithRelatedInput{
			DraftChildren: &models.WorkspaceNoteDraftChildrenUpdateNested{Disconnect: []uuid.UUID{draft, published}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated(disconnect): %v — an id this edge cannot see is a miss, and a miss is a no-op", err)
		}
		assertSplit(t, parent, draft, published)
	})
}

// TestUpdateWithRelated_BatchesPastTheSQLiteBindCeiling pins that `connect` and
// `disconnect` batch at `batchSize`.
//
// The bind-parameter ceiling is dialect-dependent and lowest on SQLite, which
// rejects an `IN` list at 32766 parameters with an error naming no table,
// column, edge or verb — so §9.9.8 cannot attribute it and batching removes the
// ceiling rather than relocating it. This example is PostgreSQL, whose pgx path
// binds the whole list as ONE array parameter (`= ANY($1)`), so the ceiling is
// not reachable here in either implementation: what discriminates is the
// statement count, which an unbatched disconnect would leave at one.
//
// The list is sized past 32766 anyway, so the same shape is the ceiling case on
// the dialects that expand their placeholders, MySQL included.
func TestUpdateWithRelated_BatchesPastTheSQLiteBindCeiling(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	const (
		n         = 32_767
		batchSize = 200
	)
	want := (n + batchSize - 1) / batchSize

	client, counter := tracedClient(t)
	user := seedUserRow(t, "batch-ceiling@example.com")
	linked := seedParentedEvent(t, user, "batch-ceiling-linked")

	// Every id but one names no row at all: a disconnect matching nothing is a
	// no-op, so the list costs nothing to build and the one real row proves
	// the statements ran rather than being skipped.
	ids := make([]uuid.UUID, 0, n)
	ids = append(ids, linked)
	for len(ids) < n {
		ids = append(ids, uuid.New())
	}

	counter.reset()
	if _, err := client.Users().UpdateWithRelated(ctx, user, &models.UpdateUserWithRelatedInput{
		Events: &models.UserEventsUpdateNested{Disconnect: ids},
	}); err != nil {
		t.Fatalf("UpdateWithRelated with %d disconnect ids: %v", n, err)
	}

	got := counter.count()
	if got < want {
		t.Errorf("issued %d statements for %d ids, want at least ceil(%d/%d) = %d — an unbatched disconnect is one statement over the whole list",
			got, n, n, batchSize, want)
	}
	// BEGIN, COMMIT and the parent write are a small constant on top; anything
	// much larger means a per-row statement.
	if got > want+10 {
		t.Errorf("issued %d statements for %d ids, want about %d — the batch size is not being honoured", got, n, want)
	}

	row, err := testClient.Events().Get(ctx, linked)
	if err != nil {
		t.Fatalf("re-reading the unlinked event: %v", err)
	}
	if row.UserID != nil {
		t.Error("the one real id in the batch was not unlinked; the chunks did not all run")
	}
}

// TestUpsertWithRelated_RunsBothBranchesOnOneBlock pins that UpsertWithRelated
// reuses the update-side block. Every verb is an idempotent set operation, so
// that block is correct on a freshly inserted parent as well as on an existing
// one, and the method needs no insert-vs-update detection.
//
// The same payload runs twice: the first call inserts the parent, the second
// takes the conflict branch. `disconnect` and `clear` match zero rows on the
// insert branch and are no-ops there, which is what makes one block correct on
// both.
func TestUpsertWithRelated_RunsBothBranchesOnOneBlock(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	orphan := seedOrphanEvent(t, "upsert-orphan")
	existingCat := seedCategoryRow(t, "upsert-topic")

	payload := func() *models.UpsertUserWithRelatedInput {
		return &models.UpsertUserWithRelatedInput{
			User: models.CreateUserInput{Email: "upsert@example.com", Name: "Upsert"},
			Events: &models.UserEventsUpdateNested{
				Clear:   true,
				Connect: []uuid.UUID{orphan},
			},
			Categories: &models.UserCategoriesUpdateNested{
				Clear:   true,
				Connect: []int64{existingCat},
			},
		}
	}

	first, err := testClient.Users().UpsertWithRelated(ctx, payload(), models.UserConflictEmail)
	if err != nil {
		t.Fatalf("UpsertWithRelated (insert branch): %v", err)
	}
	// On a parent that did not exist a moment ago, `clear` matched nothing and
	// the connect still landed.
	if !eventIDs(t, first.ID)[orphan] {
		t.Fatal("the connected event is absent after the insert branch")
	}

	second, err := testClient.Users().UpsertWithRelated(ctx, payload(), models.UserConflictEmail)
	if err != nil {
		t.Fatalf("UpsertWithRelated (conflict branch): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("the second upsert minted a new parent (%v vs %v); the conflict target did not fire", second.ID, first.ID)
	}

	// Idempotence: the same payload twice yields the same end state, which is
	// the property `clear` + `connect` is supposed to have.
	got := eventIDs(t, second.ID)
	if len(got) != 1 || !got[orphan] {
		t.Errorf("Events = %v after running the same payload twice, want exactly the connected id %v", got, orphan)
	}
	cats, err := testClient.Users().Get(ctx, second.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{
			ID:         true,
			Categories: &models.CategoryRelationshipOptions{FieldOptions: &models.CategoryFieldOptions{ID: true}},
		}
	})
	if err != nil {
		t.Fatalf("re-reading the parent: %v", err)
	}
	if len(cats.Categories) != 1 || cats.Categories[0].ID != existingCat {
		t.Errorf("Categories = %v, want exactly the connected id %d — the link step is not idempotent", cats.Categories, existingCat)
	}
}

// TestNestedWithRelated_ComposeWithACallerTransaction extends the
// CreateWithRelated savepoint pin to UpdateWithRelated and UpsertWithRelated.
//
// The name each hands database.WithTransaction doubles as the SAVEPOINT
// identifier the moment ctx already carries a transaction, so a prose name with
// spaces in it works at the top level — where it is only a label — and
// hard-errors on every nested call. Nothing else catches that: the top-level
// path, which every other test in these files takes, never renders the name
// into SQL.
func TestNestedWithRelated_ComposeWithACallerTransaction(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	t.Run("UpdateWithRelated commits with the caller's transaction", func(t *testing.T) {
		user := seedUserRow(t, "tx-update@example.com")
		orphan := seedOrphanEvent(t, "tx-update-orphan")
		if err := testClient.WithTx(ctx, "outer_update", func(txCtx context.Context) error {
			_, err := testClient.Users().UpdateWithRelated(txCtx, user, &models.UpdateUserWithRelatedInput{
				Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{orphan}},
			})
			return err
		}); err != nil {
			t.Fatalf("WithTx(UpdateWithRelated): %v", err)
		}
		if !eventIDs(t, user)[orphan] {
			t.Error("the adoption did not survive the caller's commit")
		}
	})

	t.Run("UpsertWithRelated rolls back to the savepoint only", func(t *testing.T) {
		var survivor *models.User
		err := testClient.WithTx(ctx, "outer_upsert", func(txCtx context.Context) error {
			_, err := testClient.Users().UpsertWithRelated(txCtx, &models.UpsertUserWithRelatedInput{
				User:   models.CreateUserInput{Email: "tx-upsert-doomed@example.com", Name: "doomed"},
				Events: &models.UserEventsUpdateNested{Connect: []uuid.UUID{uuid.New()}},
			}, models.UserConflictEmail)
			if !errors.Is(err, models.ErrNotFound) {
				return errors.New("expected the unresolvable connect to fail the nested write")
			}
			// The savepoint absorbed the failure; the outer transaction is
			// still usable, which is the half a new-transaction-per-call
			// implementation cannot provide.
			survivor, err = testClient.Users().Create(txCtx, &models.CreateUserInput{
				Email: "tx-upsert-survivor@example.com", Name: "survivor",
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
}
