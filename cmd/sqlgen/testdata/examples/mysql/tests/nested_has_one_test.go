package tests

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// The MySQL half of the has-one pin. `documents.entity_id` carries no
// UNIQUE constraint, so nothing in the database refuses a second
// `asset.primary` row; the executor's existing-child read has to. The read is
// a plain SELECT, so unlike the `*Where` verbs it costs the same one statement
// here as on the RETURNING dialects.
func TestNestedMutations_HasOneSecondCreateIsRefused(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	asset, err := client.Assets().CreateWithRelated(ctx, &models.CreateAssetWithRelatedInput{
		Asset:           models.CreateAssetInput{Name: "Has-one"},
		PrimaryDocument: &models.AssetPrimaryDocumentCreateNested{Create: &models.AssetPrimaryDocumentCreateInput{Name: "first"}},
	})
	if err != nil {
		t.Fatalf("CreateWithRelated: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.ExecContext(context.Background(), "DELETE FROM documents WHERE entity_id = ? AND entity_type = 'asset.primary'", asset.ID)
		_ = client.Assets().HardDelete(context.Background(), asset.ID)
	})

	_, err = client.Assets().UpdateWithRelated(ctx, asset.ID, &models.UpdateAssetWithRelatedInput{
		PrimaryDocument: &models.AssetPrimaryDocumentUpdateNested{Create: &models.AssetPrimaryDocumentCreateInput{Name: "second"}},
	})
	if !errors.Is(err, models.ErrAlreadyRelated) {
		t.Fatalf("second nested create: err = %v, want ErrAlreadyRelated", err)
	}
	nested, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok || nested.Edge != "PrimaryDocument" || nested.Verb != "create" {
		t.Errorf("err = %v, want a *NestedMutationError on PrimaryDocument / create", err)
	}

	var n int
	if err := testDB.QueryRowContext(ctx,
		"SELECT count(*) FROM documents WHERE entity_id = ? AND entity_type = 'asset.primary'", asset.ID).Scan(&n); err != nil {
		t.Fatalf("counting primary documents: %v", err)
	}
	if n != 1 {
		t.Errorf("primary documents after the refused update = %d, want 1", n)
	}
}
