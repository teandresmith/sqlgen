package tests

import (
	"context"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// TestCache_NestedWriteInvalidatesBeforeReturn pins that each *WithRelated
// method called outside a transaction invalidates the rows its inner calls
// wrote before it returns, as a flat write does: a cached Get issued next
// sees the adoption, under the client's default async callback mode. The
// method opens the root transaction itself, so with async callbacks the
// invalidation would run in a goroutine after Commit and the next Get would
// read the entry Create wrote through (owner_id nil).
func TestCache_NestedWriteInvalidatesBeforeReturn(t *testing.T) {
	tests := []struct {
		name    string
		connect func(ctx context.Context, c *models.Client, at types.DateTime) (int64, error)
	}{
		{
			name: "CreateWithRelated",
			connect: func(ctx context.Context, c *models.Client, at types.DateTime) (int64, error) {
				o, err := c.MomentOwners().CreateWithRelated(ctx, &models.CreateMomentOwnerWithRelatedInput{
					MomentOwner: models.CreateMomentOwnerInput{Name: "o"},
					Moments:     &models.MomentOwnerMomentsCreateNested{Connect: []types.DateTime{at}},
				})
				if err != nil {
					return 0, err
				}
				return o.ID, nil
			},
		},
		{
			name: "UpdateWithRelated",
			connect: func(ctx context.Context, c *models.Client, at types.DateTime) (int64, error) {
				o, err := c.MomentOwners().Create(ctx, &models.CreateMomentOwnerInput{Name: "o"})
				if err != nil {
					return 0, err
				}
				if _, err := c.MomentOwners().UpdateWithRelated(ctx, o.ID, &models.UpdateMomentOwnerWithRelatedInput{
					MomentOwner: models.UpdateMomentOwnerInput{Name: omittable.Set("o")},
					Moments:     &models.MomentOwnerMomentsUpdateNested{Connect: []types.DateTime{at}},
				}); err != nil {
					return 0, err
				}
				return o.ID, nil
			},
		},
		{
			name: "UpdateWithRelated with a committed transaction in ctx",
			connect: func(ctx context.Context, c *models.Client, at types.DateTime) (int64, error) {
				o, err := c.MomentOwners().Create(ctx, &models.CreateMomentOwnerInput{Name: "o"})
				if err != nil {
					return 0, err
				}
				txCtx, err := c.Begin(ctx, "done")
				if err != nil {
					return 0, err
				}
				if err := c.Commit(txCtx); err != nil {
					return 0, err
				}
				if _, err := c.MomentOwners().UpdateWithRelated(txCtx, o.ID, &models.UpdateMomentOwnerWithRelatedInput{
					MomentOwner: models.UpdateMomentOwnerInput{Name: omittable.Set("o")},
					Moments:     &models.MomentOwnerMomentsUpdateNested{Connect: []types.DateTime{at}},
				}); err != nil {
					return 0, err
				}
				return o.ID, nil
			},
		},
		{
			name: "UpsertWithRelated",
			connect: func(ctx context.Context, c *models.Client, at types.DateTime) (int64, error) {
				o, err := c.MomentOwners().UpsertWithRelated(ctx, &models.UpsertMomentOwnerWithRelatedInput{
					MomentOwner: models.CreateMomentOwnerInput{Name: "o"},
					Moments:     &models.MomentOwnerMomentsUpdateNested{Connect: []types.DateTime{at}},
				}, models.MomentOwnerConflictPK)
				if err != nil {
					return 0, err
				}
				return o.ID, nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			resetDB(t)
			c := newEnv(t, withCache()).client
			base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

			for i := range 20 {
				at := types.DateTime{Time: base.Add(time.Duration(i) * time.Minute)}
				if _, err := c.Moments().Create(ctx, &models.CreateMomentInput{At: at, Label: "m"}); err != nil {
					t.Fatalf("create moment: %v", err)
				}
				ownerID, err := tt.connect(ctx, c, at)
				if err != nil {
					t.Fatalf("connect: %v", err)
				}
				got, err := c.Moments().Get(ctx, at)
				if err != nil {
					t.Fatalf("Get moment: %v", err)
				}
				if got.OwnerID == nil || *got.OwnerID != ownerID {
					t.Fatalf("iteration %d: cached owner_id = %v, want %d", i, got.OwnerID, ownerID)
				}
			}
		})
	}
}
