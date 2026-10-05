package tests

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"uuid"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/tenancy"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// The polymorphic, self-referential and tenanted half of the nested create
// surface, all on `workspace_notes.DraftChildren`.
//
// That edge is the only one in any fixture that is polymorphic AND traverses a
// NULLABLE foreign key, which is what makes the discriminator check on a
// `connect` reachable at all: the `assets` discriminator edges run over
// `documents.entity_id`, which is NOT NULL, so §9.9.4 leaves them `create`-only
// and their visibility read is never emitted. Being self-referential and
// tenanted, the same edge carries the self-connect refusal and the §29.10
// propagation rule.

// nestedTenant is the workspace every note in this file belongs to.
var nestedTenant = uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

// nestedTenantedClient returns a Go client whose tenant resolver reads
// ctx — the same bridge tenancy_test.go wires for the HTTP surface — plus a
// ctx already carrying nestedTenant.
func nestedTenantedClient(t *testing.T) (*models.Client, context.Context) {
	t.Helper()
	client := models.New(dbpgx.New(testPool), models.WithTenantResolver(ctxTenantResolver()))
	return client, context.WithValue(context.Background(), tenantCtxKey{}, nestedTenant)
}

// seedNestedNote inserts one workspace note of the given kind with no parent — the
// unparented shape a `connect` can adopt.
func seedNestedNote(t *testing.T, client *models.Client, ctx context.Context, body string, kind models.WorkspaceNoteKindEnum) uuid.UUID {
	t.Helper()
	note, err := client.WorkspaceNotes().Create(ctx, &models.CreateWorkspaceNoteInput{
		Body: body,
		Kind: omittable.Set(kind),
	})
	if err != nil {
		t.Fatalf("seeding %s note: %v", kind, err)
	}
	return note.ID
}

// TestCreateWithRelated_DiscriminatorRoundTrip pins both halves of discriminator
// ownership against a real database.
//
// The create half is visible as a column value: the nested create never accepts
// `kind` and the row still comes back as a draft. The connect half is visible
// only as an error: a note of another kind is a row the FK constraint would
// happily accept, so without the discriminator on the visibility read the
// connect would silently relink it into an edge that can never read it back.
func TestCreateWithRelated_DiscriminatorRoundTrip(t *testing.T) {
	truncateAll(t)
	client, ctx := nestedTenantedClient(t)

	t.Run("a nested create carries the edge's declared value", func(t *testing.T) {
		parent, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "root", Kind: omittable.Set(models.WorkspaceNoteKindEnumPublished)},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "child"}},
			},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}

		reloaded, err := client.WorkspaceNotes().Get(ctx, parent.ID, func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
			o.FieldOptions = &models.WorkspaceNoteFieldOptions{
				ID: true,
				DraftChildren: &models.WorkspaceNoteRelationshipOptions{
					FieldOptions: &models.WorkspaceNoteFieldOptions{ID: true, Kind: true, WorkspaceID: true},
				},
			}
		})
		if err != nil {
			t.Fatalf("reloading parent: %v", err)
		}
		if len(reloaded.DraftChildren) != 1 {
			t.Fatalf("DraftChildren = %d rows, want 1 — the nested create did not land under this edge's predicate", len(reloaded.DraftChildren))
		}
		if got, want := reloaded.DraftChildren[0].Kind, models.WorkspaceNoteKindEnumDraft; got != want {
			t.Errorf("child kind = %q, want %q — the nested create sets the discriminator from the edge", got, want)
		}
		// §29.10 — the tenant column is auto-set by routing through the
		// target's own client, not assigned by the nested write.
		if got := reloaded.DraftChildren[0].WorkspaceID; got != nestedTenant {
			t.Errorf("child workspace_id = %v, want the resolved tenant %v", got, nestedTenant)
		}
	})

	t.Run("a connect whose discriminator differs is NOT_FOUND", func(t *testing.T) {
		published := seedNestedNote(t, client, ctx, "published-orphan", models.WorkspaceNoteKindEnumPublished)

		_, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "adopter"},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Connect: []uuid.UUID{published},
			},
		})
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound — the connect's visibility read carries the discriminator", err)
		}
		var nested *models.NestedMutationError
		if !errors.As(err, &nested) || nested.Edge != "DraftChildren" || nested.Verb != "connect" {
			t.Errorf("attribution = %v, want DraftChildren/connect", err)
		}

		// And the row is untouched: the mislink the guard prevents would have
		// set parent_id on a note this edge can never read back.
		still, err := client.WorkspaceNotes().Get(ctx, published)
		if err != nil {
			t.Fatalf("re-reading the published note: %v", err)
		}
		if still.ParentID != nil {
			t.Errorf("published note parent_id = %v, want NULL — the connect must not have relinked it", still.ParentID)
		}
	})

	t.Run("a connect of the matching kind is adopted", func(t *testing.T) {
		draft := seedNestedNote(t, client, ctx, "draft-orphan", models.WorkspaceNoteKindEnumDraft)

		parent, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "adopter-ok"},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Connect: []uuid.UUID{draft},
			},
		})
		if err != nil {
			t.Fatalf("CreateWithRelated: %v", err)
		}
		adopted, err := client.WorkspaceNotes().Get(ctx, draft)
		if err != nil {
			t.Fatalf("re-reading the adopted note: %v", err)
		}
		if adopted.ParentID == nil || *adopted.ParentID != parent.ID {
			t.Errorf("adopted note parent_id = %v, want %v", adopted.ParentID, parent.ID)
		}
	})
}

// TestCreateWithRelated_SelfConnectRefused pins the self-connect refusal. It is
// reachable on the create side precisely because the caller may supply the
// parent's own primary key: `CreateWorkspaceNoteInput.ID` is omittable, so a
// caller can name the row it is about to insert and then connect it to itself.
//
// No other guard catches it. The adopt-only-orphans rule does not — the
// parent's own parent_id is NULL, which looks like an adoptable orphan — and
// the two-verb check does not, since only one verb names the id. Left unguarded
// it writes a row that is its own parent, which every recursive read walks
// forever.
func TestCreateWithRelated_SelfConnectRefused(t *testing.T) {
	truncateAll(t)
	client, ctx := nestedTenantedClient(t)

	selfID := uuid.New()
	_, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
		WorkspaceNote: models.CreateWorkspaceNoteInput{ID: omittable.Set(selfID), Body: "ouroboros"},
		DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
			Connect: []uuid.UUID{selfID},
		},
	})
	if !errors.Is(err, models.ErrNestedVerbConflict) {
		t.Fatalf("err = %v, want ErrNestedVerbConflict — a self-connect is refused", err)
	}

	// The refusal happens inside the transaction, after the parent INSERT:
	// §9.9.6 puts the per-edge validation in step 2 and the parent's key is
	// not known until step 1 has run, so the refusal's "before any statement runs"
	// describes the statements on the *edge*, not the parent write. The
	// rollback is what makes the observable end state the same.
	notes, err := client.WorkspaceNotes().GetMany(ctx, &models.GetWorkspaceNotesInput{})
	if err != nil {
		t.Fatalf("counting notes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("%d note rows exist after a refused self-connect, want 0", len(notes))
	}
}

// countingTenantResolver wraps ctxTenantResolver and records how many times the
// consumer's resolver is actually invoked. §29.6's bound is a statement about
// that count, so nothing short of counting can assert it — the rows a pure
// resolver writes are identical however many times it ran, which is why a
// repeated-resolve regression would survive every other test in this package.
func countingTenantResolver(n *atomic.Int64) tenancy.TenantResolver[uuid.UUID] {
	inner := ctxTenantResolver()
	return func(ctx context.Context) (uuid.UUID, error) {
		n.Add(1)
		return inner(ctx)
	}
}

// TestCreateWithRelated_ResolvesTheTenantOnce pins PRD §29.6 across the nested
// surface: "the resolver is still invoked at most once per mutation hook
// entry", which §29.10 extends to nested children ("nested children inherit the
// ctx-cached tenant").
//
// Every inner write routes through another entity client, and the ctx stash
// each of those makes is scoped to its own hook-handler ctx — it never escapes
// back to the transaction body. Without resolving the tenant once up front, the
// same two-edge create measured three resolves with `create` alone and seven
// once both edges also carried a `connect`.
func TestCreateWithRelated_ResolvesTheTenantOnce(t *testing.T) {
	var calls atomic.Int64
	client := models.New(dbpgx.New(testPool), models.WithTenantResolver(countingTenantResolver(&calls)))
	ctx := context.WithValue(context.Background(), tenantCtxKey{}, nestedTenant)

	orphanChild := seedNestedNote(t, client, ctx, "once-orphan-child", models.WorkspaceNoteKindEnumPublished)
	orphanDraft := seedNestedNote(t, client, ctx, "once-orphan-draft", models.WorkspaceNoteKindEnumDraft)

	// A relationship in the selection is what makes CreateWithRelated issue its
	// terminal re-read, and that read resolves too — so the shape with the most
	// resolvers to collapse is the one that must also come back as 1. It
	// measured 10 before the fix.
	withRelationshipSelected := func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
		o.FieldOptions = &models.WorkspaceNoteFieldOptions{
			ID:   true,
			Body: true,
			DraftChildren: &models.WorkspaceNoteRelationshipOptions{
				FieldOptions: &models.WorkspaceNoteFieldOptions{ID: true},
			},
		}
	}

	cases := []struct {
		name  string
		opts  []func(*models.CallOptions[models.WorkspaceNoteFieldOptions])
		input *models.CreateWorkspaceNoteWithRelatedInput
	}{
		{
			name: "create on both edges",
			input: &models.CreateWorkspaceNoteWithRelatedInput{
				WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-create"},
				Children: &models.WorkspaceNoteChildrenCreateNested{
					Create: []*models.WorkspaceNoteChildrenCreateInput{{Body: "once-c"}},
				},
				DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
					Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "once-d"}},
				},
			},
		},
		{
			name: "create and connect on both edges",
			input: &models.CreateWorkspaceNoteWithRelatedInput{
				WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-connect"},
				Children: &models.WorkspaceNoteChildrenCreateNested{
					Create:  []*models.WorkspaceNoteChildrenCreateInput{{Body: "once-c2"}},
					Connect: []uuid.UUID{orphanChild},
				},
				DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
					Create:  []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "once-d2"}},
					Connect: []uuid.UUID{orphanDraft},
				},
			},
		},
		{
			name: "create and connect with a relationship selected",
			opts: []func(*models.CallOptions[models.WorkspaceNoteFieldOptions]){withRelationshipSelected},
			input: &models.CreateWorkspaceNoteWithRelatedInput{
				WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-reread"},
				Children: &models.WorkspaceNoteChildrenCreateNested{
					Create: []*models.WorkspaceNoteChildrenCreateInput{{Body: "once-c3"}},
				},
				DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
					Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "once-d3"}},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			calls.Store(0)
			if _, err := client.WorkspaceNotes().CreateWithRelated(ctx, tt.input, tt.opts...); err != nil {
				t.Fatalf("CreateWithRelated: %v", err)
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("tenant resolver invoked %d times, want 1 (PRD §29.6 / §29.10 — one resolve per logical mutation)", got)
			}
		})
	}

	t.Run("SkipTenancy resolves nothing", func(t *testing.T) {
		calls.Store(0)
		if _, err := client.WorkspaceNotes().CreateWithRelated(ctx, &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-skip", WorkspaceID: omittable.Set(nestedTenant)},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "once-skip-d", WorkspaceID: omittable.Set(nestedTenant)}},
			},
		}, func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
			o.SkipTenancy = true
		}); err != nil {
			t.Fatalf("CreateWithRelated(SkipTenancy): %v", err)
		}
		if got := calls.Load(); got != 0 {
			t.Errorf("tenant resolver invoked %d times under SkipTenancy, want 0", got)
		}
	})

	t.Run("the update and upsert families hoist the same way", func(t *testing.T) {
		// The hoist lives in one per-client helper that all three
		// methods call, so this is a pin on that sharing rather than on three
		// copies. A tenanted parent whose edges are also tenanted is the shape
		// with the most resolves to collapse.
		parent := seedNestedNote(t, client, ctx, "once-update-parent", models.WorkspaceNoteKindEnumPublished)
		orphan := seedNestedNote(t, client, ctx, "once-update-orphan", models.WorkspaceNoteKindEnumDraft)

		calls.Store(0)
		if _, err := client.WorkspaceNotes().UpdateWithRelated(ctx, parent, &models.UpdateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.UpdateWorkspaceNoteInput{Body: omittable.Set("once-update")},
			Children: &models.WorkspaceNoteChildrenUpdateNested{
				Create: []*models.WorkspaceNoteChildrenCreateInput{{Body: "once-update-c"}},
			},
			DraftChildren: &models.WorkspaceNoteDraftChildrenUpdateNested{
				Connect: []uuid.UUID{orphan},
			},
		}, withRelationshipSelected); err != nil {
			t.Fatalf("UpdateWithRelated: %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("UpdateWithRelated invoked the tenant resolver %d times, want 1", got)
		}

		calls.Store(0)
		if _, err := client.WorkspaceNotes().UpsertWithRelated(ctx, &models.UpsertWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-upsert"},
			Children: &models.WorkspaceNoteChildrenUpdateNested{
				Create: []*models.WorkspaceNoteChildrenCreateInput{{Body: "once-upsert-c"}},
			},
		}, models.WorkspaceNoteConflictPK, withRelationshipSelected); err != nil {
			t.Fatalf("UpsertWithRelated: %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("UpsertWithRelated invoked the tenant resolver %d times, want 1", got)
		}
	})

	t.Run("an explicit tenant resolves nothing", func(t *testing.T) {
		calls.Store(0)
		if _, err := client.WorkspaceNotes().CreateWithRelated(context.Background(), &models.CreateWorkspaceNoteWithRelatedInput{
			WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "once-explicit"},
			DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
				Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "once-explicit-d"}},
			},
		}, func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
			o.Tenant = new(nestedTenant)
		}); err != nil {
			t.Fatalf("CreateWithRelated(explicit tenant): %v", err)
		}
		// ctx carries no tenant at all, so a fallback to the resolver would
		// also have failed closed — the count distinguishes "honoured the
		// explicit tenant" from "never needed one".
		if got := calls.Load(); got != 0 {
			t.Errorf("tenant resolver invoked %d times under an explicit tenant, want 0 (PRD §29.4.4)", got)
		}
	})
}

// TestRelationshipLoad_HonoursAnExplicitTenant pins the §29.4.4 half:
// CallOptions.Tenant reaches the relationship loaders.
//
// The loader reads the target through the target's own client, which resolves
// its own tenant. Forwarding SkipTenancy but not Tenant left the parent scoped
// to the explicit tenant and the children scoped to the ctx resolver — two
// halves of one row set under two different tenants. On §29.4.4's stated
// bootstrap path, where the caller "cannot obtain it from request context", the
// child read failed with tenancy.ErrMissing while the parent succeeded.
//
// ctx deliberately carries no tenant, so only the explicit one can satisfy both
// halves.
func TestRelationshipLoad_HonoursAnExplicitTenant(t *testing.T) {
	client, seedCtx := nestedTenantedClient(t)

	parent, err := client.WorkspaceNotes().CreateWithRelated(seedCtx, &models.CreateWorkspaceNoteWithRelatedInput{
		WorkspaceNote: models.CreateWorkspaceNoteInput{Body: "explicit-tenant-parent"},
		DraftChildren: &models.WorkspaceNoteDraftChildrenCreateNested{
			Create: []*models.WorkspaceNoteDraftChildrenCreateInput{{Body: "explicit-tenant-child"}},
		},
	})
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}

	bare := context.Background()
	got, err := client.WorkspaceNotes().Get(bare, parent.ID, func(o *models.CallOptions[models.WorkspaceNoteFieldOptions]) {
		o.Tenant = new(nestedTenant)
		o.FieldOptions = &models.WorkspaceNoteFieldOptions{
			ID:   true,
			Body: true,
			DraftChildren: &models.WorkspaceNoteRelationshipOptions{
				FieldOptions: &models.WorkspaceNoteFieldOptions{ID: true, Body: true},
			},
		}
	})
	if err != nil {
		t.Fatalf("Get with an explicit tenant and a relationship selected: %v", err)
	}
	if len(got.DraftChildren) != 1 {
		t.Fatalf("relationship loaded %d children, want 1 — the explicit tenant did not reach the loader", len(got.DraftChildren))
	}
	if got.DraftChildren[0].Body != "explicit-tenant-child" {
		t.Errorf("child body = %q, want %q", got.DraftChildren[0].Body, "explicit-tenant-child")
	}
}
