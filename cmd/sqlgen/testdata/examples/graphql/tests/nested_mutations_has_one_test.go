package tests

import (
	"errors"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// A has-one edge holds at most one child (PRD §13.1), and only a
// UNIQUE constraint on the foreign key makes the database refuse a second one.
// `assets.PrimaryDocument` has none: `documents.entity_id` is shared by three
// polymorphic edges, so a second nested `create` used to insert a second
// `asset.primary` row, report success, and leave GetMany returning the asset
// once per child. `users.Badge` is the UNIQUE-FK contrast, whose second child
// the database used to refuse with a constraint error instead.

func primaryDocumentCount(t *testing.T, asset uuid.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(nestedCtx(),
		`SELECT count(*) FROM documents WHERE entity_id = $1 AND entity_type = 'asset.primary'`, asset).Scan(&n); err != nil {
		t.Fatalf("counting primary documents: %v", err)
	}
	return n
}

// wantNestedRefusal asserts err is the per-edge refusal §9.9.8 describes.
func wantNestedRefusal(t *testing.T, err error, sentinel error, edge, verb string) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	nested, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("err = %v, want a *NestedMutationError", err)
	}
	if nested.Edge != edge || nested.Verb != verb {
		t.Errorf("NestedMutationError = {Edge: %q, Verb: %q}, want {%q, %q}", nested.Edge, nested.Verb, edge, verb)
	}
}

func TestNestedHasOne_SecondCreateIsRefused(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	asset, err := testClient.Assets().CreateWithRelated(ctx, &models.CreateAssetWithRelatedInput{
		Asset:           models.CreateAssetInput{Name: "has-one"},
		PrimaryDocument: &models.AssetPrimaryDocumentCreateNested{Create: &models.AssetPrimaryDocumentCreateInput{Name: "first"}},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}
	if _, err := testClient.Assets().Create(ctx, &models.CreateAssetInput{Name: "bystander"}); err != nil {
		t.Fatalf("seeding a second asset: %v", err)
	}

	_, err = testClient.Assets().UpdateWithRelated(ctx, asset.ID, &models.UpdateAssetWithRelatedInput{
		PrimaryDocument: &models.AssetPrimaryDocumentUpdateNested{Create: &models.AssetPrimaryDocumentCreateInput{Name: "second"}},
	})
	wantNestedRefusal(t, err, models.ErrAlreadyRelated, "PrimaryDocument", "create")
	if got := primaryDocumentCount(t, asset.ID); got != 1 {
		t.Errorf("primary documents after the refused update = %d, want 1", got)
	}

	// Upsert reuses the update-side block (PRD §9.9.5), so the conflict
	// branch is the same refusal.
	_, err = testClient.Assets().UpsertWithRelated(ctx, &models.UpsertAssetWithRelatedInput{
		Asset:           models.CreateAssetInput{ID: omittable.Set(asset.ID), Name: "has-one"},
		PrimaryDocument: &models.AssetPrimaryDocumentUpdateNested{Create: &models.AssetPrimaryDocumentCreateInput{Name: "third"}},
	}, models.AssetConflictPK)
	wantNestedRefusal(t, err, models.ErrAlreadyRelated, "PrimaryDocument", "create")
	if got := primaryDocumentCount(t, asset.ID); got != 1 {
		t.Errorf("primary documents after the refused upsert = %d, want 1", got)
	}

	// The read side never saw a second child, so it returns each asset once.
	rows, err := testClient.Assets().GetMany(ctx, &models.GetAssetsInput{}, func(o *models.CallOptions[models.AssetFieldOptions]) {
		o.FieldOptions = &models.AssetFieldOptions{ID: true, PrimaryDocument: &models.DocumentFieldOptions{ID: true, Name: true}}
	})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("GetMany = %d rows for 2 assets, want 2", len(rows))
	}
}

func TestNestedHasOne_ConnectAndReplace(t *testing.T) {
	truncateAll(t)
	ctx := nestedCtx()

	user, err := testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User:  models.CreateUserInput{Email: "has-one@example.com", Name: "HasOne"},
		Badge: &models.UserBadgeCreateNested{Create: &models.UserBadgeCreateInput{Label: "first"}},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}
	current, err := testClient.Users().Get(ctx, user.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{ID: true, Badge: &models.UserBadgeFieldOptions{ID: true}}
	})
	if err != nil || current.Badge == nil {
		t.Fatalf("reading the badge back: %v (badge %v)", err, current.Badge)
	}
	orphan, err := testClient.UserBadges().Create(ctx, &models.CreateUserBadgeInput{Label: "orphan"})
	if err != nil {
		t.Fatalf("seeding an orphan badge: %v", err)
	}

	tests := []struct {
		name     string
		block    *models.UserBadgeUpdateNested
		sentinel error
		verb     string
	}{
		{
			name:     "connect on an occupied edge",
			block:    &models.UserBadgeUpdateNested{Connect: &orphan.ID},
			sentinel: models.ErrAlreadyRelated,
			verb:     "connect",
		},
		{
			name:     "create and connect in one block",
			block:    &models.UserBadgeUpdateNested{Create: &models.UserBadgeCreateInput{Label: "both"}, Connect: &orphan.ID},
			sentinel: models.ErrNestedVerbConflict,
			verb:     "connect",
		},
		{
			// disconnect runs after create (PRD §9.9.6), so this asks for two
			// children for a moment; `clear` is the replace spelling.
			name:     "disconnect the current child and create another",
			block:    &models.UserBadgeUpdateNested{Disconnect: &current.Badge.ID, Create: &models.UserBadgeCreateInput{Label: "swap"}},
			sentinel: models.ErrAlreadyRelated,
			verb:     "create",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testClient.Users().UpdateWithRelated(ctx, user.ID, &models.UpdateUserWithRelatedInput{Badge: tt.block})
			wantNestedRefusal(t, err, tt.sentinel, "Badge", tt.verb)
		})
	}

	// On a parent that does not exist yet the edge is empty, so the
	// validation step is the only thing that stops a block asking for two.
	_, err = testClient.Users().CreateWithRelated(ctx, &models.CreateUserWithRelatedInput{
		User:  models.CreateUserInput{Email: "has-one-both@example.com", Name: "Both"},
		Badge: &models.UserBadgeCreateNested{Create: &models.UserBadgeCreateInput{Label: "both"}, Connect: &orphan.ID},
	})
	wantNestedRefusal(t, err, models.ErrNestedVerbConflict, "Badge", "connect")

	// Connecting the child the edge already holds is connect's no-op.
	if _, err := testClient.Users().UpdateWithRelated(ctx, user.ID, &models.UpdateUserWithRelatedInput{
		Badge: &models.UserBadgeUpdateNested{Connect: &current.Badge.ID},
	}); err != nil {
		t.Errorf("connect naming the current badge: %v, want nil", err)
	}

	// clear runs first, so clear + connect replaces the child.
	if _, err := testClient.Users().UpdateWithRelated(ctx, user.ID, &models.UpdateUserWithRelatedInput{
		Badge: &models.UserBadgeUpdateNested{Clear: true, Connect: &orphan.ID},
	}); err != nil {
		t.Fatalf("clear + connect: %v", err)
	}
	after, err := testClient.Users().Get(ctx, user.ID, func(o *models.CallOptions[models.UserFieldOptions]) {
		o.FieldOptions = &models.UserFieldOptions{ID: true, Badge: &models.UserBadgeFieldOptions{ID: true}}
	})
	if err != nil {
		t.Fatalf("re-reading the badge: %v", err)
	}
	if after.Badge == nil || after.Badge.ID != orphan.ID {
		t.Errorf("Badge after clear + connect = %v, want the connected badge %d", after.Badge, orphan.ID)
	}
}
