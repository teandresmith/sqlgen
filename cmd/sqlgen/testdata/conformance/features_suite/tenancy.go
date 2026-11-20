package featuressuite

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
)

// tenancy: the event tenant stamp (PRD §29.6), a tenant in the primary key
// (§29.7, §29.9), nested tenancy (§9.9.10) and a filter on the tenant column.
// MySQL captures each affected row's tenant and key by pre-read where
// PostgreSQL and SQLite use RETURNING, so every case runs on every target.
func tenancy(t *testing.T, h *Harness) {
	t.Run("Create", func(t *testing.T) { tenancyCreate(t, h) })
	t.Run("CreateMany", func(t *testing.T) { tenancyCreateMany(t, h) })
	t.Run("Update", func(t *testing.T) { tenancyUpdate(t, h) })
	t.Run("UpdateMany", func(t *testing.T) { tenancyUpdateMany(t, h) })
	t.Run("UpdateWhere", func(t *testing.T) { tenancyUpdateWhere(t, h) })
	t.Run("Upsert", func(t *testing.T) { tenancyUpsert(t, h) })
	t.Run("Increment", func(t *testing.T) { tenancyIncrement(t, h) })
	t.Run("SoftDelete", func(t *testing.T) { tenancySoftDelete(t, h) })
	t.Run("SoftDeleteMany", func(t *testing.T) { tenancySoftDeleteMany(t, h) })
	t.Run("SoftDeleteWhere", func(t *testing.T) { tenancySoftDeleteWhere(t, h) })
	t.Run("HardDelete", func(t *testing.T) { tenancyHardDelete(t, h) })
	t.Run("HardDeleteMany", func(t *testing.T) { tenancyHardDeleteMany(t, h) })
	t.Run("HardDeleteWhere", func(t *testing.T) { tenancyHardDeleteWhere(t, h) })
	t.Run("Restore", func(t *testing.T) { tenancyRestore(t, h) })
	t.Run("RestoreMany", func(t *testing.T) { tenancyRestoreMany(t, h) })
	t.Run("RestoreWhere", func(t *testing.T) { tenancyRestoreWhere(t, h) })
	t.Run("Get", func(t *testing.T) { tenancyGet(t, h) })
	t.Run("Exists", func(t *testing.T) { tenancyExists(t, h) })
	t.Run("GetMany", func(t *testing.T) { tenancyGetMany(t, h) })
	t.Run("UpdateWithRelated", func(t *testing.T) { tenancyUpdateWithRelated(t, h) })
	t.Run("UpsertWithRelated", func(t *testing.T) { tenancyUpsertWithRelated(t, h) })

	t.Run("a tenant-in-PK event is stamped from its PK", func(t *testing.T) {
		writes := []struct {
			name  string
			write func(r *stampRun) []any
		}{
			{"Create", func(r *stampRun) []any {
				in := &models.CreateMembershipInput{WorkspaceID: r.tenants[0], Member: "new", Role: "r"}
				if _, err := r.env.Client.Memberships().Create(r.ctx, in, r.membershipOpts()...); err != nil {
					r.t.Fatalf("Create() error = %v", err)
				}
				return []any{models.MembershipPK{WorkspaceID: in.WorkspaceID, Member: in.Member}}
			}},
			{"CreateMany", func(r *stampRun) []any {
				var inputs []*models.CreateMembershipInput
				var keys []any
				for i, tenant := range r.tenants {
					in := &models.CreateMembershipInput{WorkspaceID: tenant, Member: fmt.Sprintf("new-%d", i), Role: "r"}
					inputs = append(inputs, in)
					keys = append(keys, models.MembershipPK{WorkspaceID: tenant, Member: in.Member})
				}
				if _, err := r.env.Client.Memberships().CreateMany(r.ctx, inputs, r.membershipOpts()...); err != nil {
					r.t.Fatalf("CreateMany() error = %v", err)
				}
				return keys
			}},
			{"Update", func(r *stampRun) []any {
				pk := r.seedMemberships()[0]
				if _, err := r.env.Client.Memberships().Update(r.ctx, pk, &models.UpdateMembershipInput{Role: omittable.Set("admin")}, r.membershipOpts()...); err != nil {
					r.t.Fatalf("Update() error = %v", err)
				}
				return []any{pk}
			}},
			{"UpdateMany", func(r *stampRun) []any {
				pks := r.seedMemberships()
				var items []models.UpdateMembershipItem
				for _, pk := range pks {
					items = append(items, models.UpdateMembershipItem{PK: pk, Input: &models.UpdateMembershipInput{Role: omittable.Set("admin")}})
				}
				if _, err := r.env.Client.Memberships().UpdateMany(r.ctx, items, r.membershipOpts()...); err != nil {
					r.t.Fatalf("UpdateMany() error = %v", err)
				}
				return membershipKeys(pks)
			}},
			{"UpdateWhere", func(r *stampRun) []any {
				pks := r.seedMemberships()
				if _, err := r.env.Client.Memberships().UpdateWhere(r.ctx, &models.MembershipFilter{PKs: pks},
					&models.UpdateMembershipInput{Role: omittable.Set("admin")}, r.membershipOpts()...); err != nil {
					r.t.Fatalf("UpdateWhere() error = %v", err)
				}
				return membershipKeys(pks)
			}},
			{"Upsert", func(r *stampRun) []any {
				pk := r.seedMemberships()[0]
				in := &models.CreateMembershipInput{WorkspaceID: pk.WorkspaceID, Member: pk.Member, Role: "admin"}
				if _, err := r.env.Client.Memberships().Upsert(r.ctx, in, models.MembershipConflictPK, r.membershipOpts()...); err != nil {
					r.t.Fatalf("Upsert() error = %v", err)
				}
				return []any{pk}
			}},
			{"Increment", func(r *stampRun) []any {
				pk := r.seedMemberships()[0]
				in := models.IncrementInput[models.MembershipIncrementColumn]{Column: models.MembershipIncrementSeats, Amount: 1}
				if err := r.env.Client.Memberships().Increment(r.ctx, pk, in, r.membershipOpts()...); err != nil {
					r.t.Fatalf("Increment() error = %v", err)
				}
				return []any{pk}
			}},
			{"HardDelete", func(r *stampRun) []any {
				pk := r.seedMemberships()[0]
				if err := r.env.Client.Memberships().HardDelete(r.ctx, pk, r.membershipOpts()...); err != nil {
					r.t.Fatalf("HardDelete() error = %v", err)
				}
				return []any{pk}
			}},
			{"HardDeleteMany", func(r *stampRun) []any {
				pks := r.seedMemberships()
				if err := r.env.Client.Memberships().HardDeleteMany(r.ctx, pks, r.membershipOpts()...); err != nil {
					r.t.Fatalf("HardDeleteMany() error = %v", err)
				}
				return membershipKeys(pks)
			}},
			{"HardDeleteWhere", func(r *stampRun) []any {
				pks := r.seedMemberships()
				if err := r.env.Client.Memberships().HardDeleteWhere(r.ctx, &models.MembershipFilter{PKs: pks}, r.membershipOpts()...); err != nil {
					r.t.Fatalf("HardDeleteWhere() error = %v", err)
				}
				return membershipKeys(pks)
			}},
		}
		for _, w := range writes {
			checkStamps(t, h, "memberships "+w.name, w.write)
		}
	})

	t.Run("a required-false write under the zero tenant stamps its row's tenant", func(t *testing.T) {
		h.Reset(t)
		env := h.NewEnv(t, WithRecorder())
		owner := models.NewTenant()
		ownerCtx := WithTenant(context.Background(), owner)
		bulletins := env.Client.Bulletins()
		var ids []int64
		for _, body := range []string{"a", "b", "c"} {
			b, err := bulletins.Create(ownerCtx, &models.CreateBulletinInput{Body: body})
			if err != nil {
				t.Fatalf("Create(%s) error = %v", body, err)
			}
			ids = append(ids, b.ID)
		}
		// No tenant on the context: the resolver returns the zero tenant, so a
		// required: false table runs the call without tenancy.
		zero := context.Background()
		writes := []struct {
			name  string
			write func() ([]any, error)
		}{
			{"Create", func() ([]any, error) {
				b, err := bulletins.Create(zero, &models.CreateBulletinInput{WorkspaceID: omittable.Set(owner), Body: "d"})
				if err != nil {
					return nil, err
				}
				return []any{b.ID}, nil
			}},
			{"Update", func() ([]any, error) {
				_, err := bulletins.Update(zero, ids[0], &models.UpdateBulletinInput{Body: omittable.Set("a2")})
				return []any{ids[0]}, err
			}},
			{"UpdateMany", func() ([]any, error) {
				_, err := bulletins.UpdateMany(zero, []models.UpdateBulletinItem{
					{ID: ids[0], Input: &models.UpdateBulletinInput{Body: omittable.Set("a3")}},
					{ID: ids[1], Input: &models.UpdateBulletinInput{Body: omittable.Set("b3")}},
				})
				return []any{ids[0], ids[1]}, err
			}},
			{"UpdateWhere", func() ([]any, error) {
				_, err := bulletins.UpdateWhere(zero, &models.BulletinFilter{ID: &comparator.Number[int64]{In: ids[1:]}},
					&models.UpdateBulletinInput{Body: omittable.Set("x")})
				return []any{ids[1], ids[2]}, err
			}},
			{"HardDelete", func() ([]any, error) { return []any{ids[0]}, bulletins.HardDelete(zero, ids[0]) }},
			{"HardDeleteMany", func() ([]any, error) { return []any{ids[1]}, bulletins.HardDeleteMany(zero, ids[1:2]) }},
			{"HardDeleteWhere", func() ([]any, error) {
				return []any{ids[2]}, bulletins.HardDeleteWhere(zero, &models.BulletinFilter{ID: &comparator.Number[int64]{Eq: &ids[2]}})
			}},
		}
		for _, w := range writes {
			env.Recorder.Clear()
			keys, err := w.write()
			if err != nil {
				t.Fatalf("bulletins %s under the zero tenant: error = %v", w.name, err)
			}
			assertStamps(t, "bulletins "+w.name, env.Recorder, keys, slices.Repeat([]models.Tenant{owner}, len(keys)))
		}
	})

	t.Run("a listed key that matched no row publishes no event", func(t *testing.T) {
		writes := []struct {
			name  string
			write func(r *stampRun) []any
		}{
			{"notes UpdateMany", func(r *stampRun) []any {
				ids := noteIDs(r.seedNotes())
				items := []models.UpdateNoteItem{
					{ID: ids[0], Input: &models.UpdateNoteInput{Body: omittable.Set("x")}},
					{ID: slices.Max(ids) + 1000, Input: &models.UpdateNoteInput{Body: omittable.Set("x")}},
					{ID: ids[1], Input: &models.UpdateNoteInput{Body: omittable.Set("x")}},
				}
				if _, err := r.env.Client.Notes().UpdateMany(r.ctx, items, r.noteOpts()...); err != nil {
					r.t.Fatalf("UpdateMany() error = %v", err)
				}
				return int64Keys(ids)
			}},
			{"tasks SoftDeleteMany", func(r *stampRun) []any {
				ids := taskIDs(r.seedTasks())
				if _, err := r.env.Client.Tasks().SoftDeleteMany(r.ctx, withMissing(ids), r.taskOpts()...); err != nil {
					r.t.Fatalf("SoftDeleteMany() error = %v", err)
				}
				return int64Keys(ids)
			}},
			{"tasks RestoreMany", func(r *stampRun) []any {
				ids := taskIDs(r.seedTrashedTasks())
				if _, err := r.env.Client.Tasks().RestoreMany(r.ctx, withMissing(ids), r.taskOpts()...); err != nil {
					r.t.Fatalf("RestoreMany() error = %v", err)
				}
				return int64Keys(ids)
			}},
			{"notes HardDeleteMany", func(r *stampRun) []any {
				ids := noteIDs(r.seedNotes())
				if err := r.env.Client.Notes().HardDeleteMany(r.ctx, withMissing(ids), r.noteOpts()...); err != nil {
					r.t.Fatalf("HardDeleteMany() error = %v", err)
				}
				return int64Keys(ids)
			}},
			{"memberships UpdateMany", func(r *stampRun) []any {
				pks := r.seedMemberships()
				items := []models.UpdateMembershipItem{
					{PK: pks[0], Input: &models.UpdateMembershipInput{Role: omittable.Set("admin")}},
					{PK: models.MembershipPK{WorkspaceID: pks[0].WorkspaceID, Member: "missing"}, Input: &models.UpdateMembershipInput{Role: omittable.Set("admin")}},
					{PK: pks[1], Input: &models.UpdateMembershipInput{Role: omittable.Set("admin")}},
				}
				if _, err := r.env.Client.Memberships().UpdateMany(r.ctx, items, r.membershipOpts()...); err != nil {
					r.t.Fatalf("UpdateMany() error = %v", err)
				}
				return membershipKeys(pks)
			}},
			{"memberships HardDeleteMany", func(r *stampRun) []any {
				pks := r.seedMemberships()
				listed := []models.MembershipPK{pks[0], {WorkspaceID: pks[0].WorkspaceID, Member: "missing"}, pks[1]}
				if err := r.env.Client.Memberships().HardDeleteMany(r.ctx, listed, r.membershipOpts()...); err != nil {
					r.t.Fatalf("HardDeleteMany() error = %v", err)
				}
				return membershipKeys(pks)
			}},
		}
		for _, w := range writes {
			checkStamps(t, h, w.name, w.write)
		}
	})
}

func tenancyCreate(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes Create", func(r *stampRun) []any {
			n, err := r.env.Client.Notes().Create(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(0), Slug: "a", Body: "a"}, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("Create() error = %v", err)
			}
			return []any{n.ID}
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			pk := models.MembershipPK{WorkspaceID: seeded[0].WorkspaceID, Member: "new"}
			if _, err := c.Create(ctx, &models.CreateMembershipInput{WorkspaceID: pk.WorkspaceID, Member: pk.Member, Role: "r"}); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			return []models.MembershipPK{pk}
		})
	})
}

func tenancyCreateMany(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes CreateMany", func(r *stampRun) []any {
			var inputs []*models.CreateNoteInput
			for i := range r.tenants {
				s := fmt.Sprintf("new-%d", i)
				inputs = append(inputs, &models.CreateNoteInput{WorkspaceID: r.workspace(i), Slug: s, Body: s})
			}
			created, err := r.env.Client.Notes().CreateMany(r.ctx, inputs, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("CreateMany() error = %v", err)
			}
			return int64Keys(noteIDs(created))
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			tenant := seeded[0].WorkspaceID
			pks := []models.MembershipPK{{WorkspaceID: tenant, Member: "new-0"}, {WorkspaceID: tenant, Member: "new-1"}}
			if _, err := c.CreateMany(ctx, []*models.CreateMembershipInput{
				{WorkspaceID: tenant, Member: pks[0].Member, Role: "r"},
				{WorkspaceID: tenant, Member: pks[1].Member, Role: "r"},
			}); err != nil {
				t.Fatalf("CreateMany() error = %v", err)
			}
			return pks
		})
	})
}

func tenancyUpdate(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes Update", func(r *stampRun) []any {
			id := r.seedNotes()[0].ID
			if _, err := r.env.Client.Notes().Update(r.ctx, id, &models.UpdateNoteInput{Body: omittable.Set("changed")}, r.noteOpts()...); err != nil {
				r.t.Fatalf("Update() error = %v", err)
			}
			return []any{id}
		})
	})

	update := func(ctx context.Context, c models.MembershipClient, pk models.MembershipPK, opts ...func(*models.CallOptions[models.MembershipFieldOptions])) error {
		_, err := c.Update(ctx, pk, &models.UpdateMembershipInput{Role: omittable.Set("admin")}, opts...)
		return err
	}
	roleIsAdmin := func(t *testing.T, got *models.Membership, err error) {
		t.Helper()
		if err != nil || got.Role != "admin" {
			t.Errorf("Get() after Update = %+v, %v, want role admin", got, err)
		}
	}

	t.Run("a matching PK tenant proceeds", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		if err := update(ctx, env.Client.Memberships(), pk); err != nil {
			t.Fatalf("Update() with the matching tenant error = %v", err)
		}
		got, err := env.Client.Memberships().Get(ctx, pk)
		roleIsAdmin(t, got, err)
	})

	t.Run("SkipTenancy uses the caller's PK tenant", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		crossTenant(t, env, func(other context.Context) error {
			return update(other, env.Client.Memberships(), pk, skipTenancy)
		})
		got, err := env.Client.Memberships().Get(ctx, pk)
		roleIsAdmin(t, got, err)
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if err := update(ctx, c, seeded[0]); err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			return seeded[:1]
		})
	})
}

func tenancyUpdateMany(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes UpdateMany", func(r *stampRun) []any {
			ids := noteIDs(r.seedNotes())
			var items []models.UpdateNoteItem
			for _, id := range ids {
				items = append(items, models.UpdateNoteItem{ID: id, Input: &models.UpdateNoteInput{Body: omittable.Set("changed")}})
			}
			if _, err := r.env.Client.Notes().UpdateMany(r.ctx, items, r.noteOpts()...); err != nil {
				r.t.Fatalf("UpdateMany() error = %v", err)
			}
			return int64Keys(ids)
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			var items []models.UpdateMembershipItem
			for _, pk := range seeded {
				items = append(items, models.UpdateMembershipItem{PK: pk, Input: &models.UpdateMembershipInput{Role: omittable.Set("admin")}})
			}
			if _, err := c.UpdateMany(ctx, items); err != nil {
				t.Fatalf("UpdateMany() error = %v", err)
			}
			return seeded
		})
	})
}

func tenancyUpdateWhere(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes UpdateWhere", func(r *stampRun) []any {
			ids := noteIDs(r.seedNotes())
			if _, err := r.env.Client.Notes().UpdateWhere(r.ctx, &models.NoteFilter{ID: &comparator.Number[int64]{In: ids}},
				&models.UpdateNoteInput{Body: omittable.Set("changed")}, r.noteOpts()...); err != nil {
				r.t.Fatalf("UpdateWhere() error = %v", err)
			}
			return int64Keys(ids)
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if _, err := c.UpdateWhere(ctx, &models.MembershipFilter{PKs: seeded},
				&models.UpdateMembershipInput{Role: omittable.Set("admin")}); err != nil {
				t.Fatalf("UpdateWhere() error = %v", err)
			}
			return seeded
		})
	})
}

func tenancyUpsert(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes Upsert", func(r *stampRun) []any {
			notes := r.env.Client.Notes()
			seeded := r.seedNotes()[0]
			// The first takes the update branch on the seeded slug, the second
			// inserts.
			updated, err := notes.Upsert(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(0), Slug: seeded.Slug, Body: "changed"},
				models.NoteConflictWorkspaceIDSlug, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("Upsert() updating error = %v", err)
			}
			if updated.ID != seeded.ID {
				r.t.Fatalf("Upsert() updating returned key %d, want the seeded %d", updated.ID, seeded.ID)
			}
			inserted, err := notes.Upsert(r.ctx, &models.CreateNoteInput{WorkspaceID: r.workspace(1), Slug: "fresh", Body: "fresh"},
				models.NoteConflictWorkspaceIDSlug, r.noteOpts()...)
			if err != nil {
				r.t.Fatalf("Upsert() inserting error = %v", err)
			}
			return []any{updated.ID, inserted.ID}
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			pk := seeded[0]
			if _, err := c.Upsert(ctx, &models.CreateMembershipInput{WorkspaceID: pk.WorkspaceID, Member: pk.Member, Role: "admin"},
				models.MembershipConflictPK); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}
			return seeded[:1]
		})
	})
}

func tenancyIncrement(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes Increment", func(r *stampRun) []any {
			id := r.seedNotes()[0].ID
			if err := r.env.Client.Notes().Increment(r.ctx, id,
				models.IncrementInput[models.NoteIncrementColumn]{Column: models.NoteIncrementViews, Amount: 1}, r.noteOpts()...); err != nil {
				r.t.Fatalf("Increment() error = %v", err)
			}
			return []any{id}
		})
	})

	increment := func(ctx context.Context, c models.MembershipClient, pk models.MembershipPK, opts ...func(*models.CallOptions[models.MembershipFieldOptions])) error {
		in := models.IncrementInput[models.MembershipIncrementColumn]{Column: models.MembershipIncrementSeats, Amount: 3}
		return c.Increment(ctx, pk, in, opts...)
	}
	seatsAreFive := func(t *testing.T, got *models.Membership, err error) {
		t.Helper()
		if err != nil || got.Seats != 5 {
			t.Errorf("Get() after Increment = %+v, %v, want 5 seats", got, err)
		}
	}

	t.Run("a matching PK tenant proceeds", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		if err := increment(ctx, env.Client.Memberships(), pk); err != nil {
			t.Fatalf("Increment() with the matching tenant error = %v", err)
		}
		got, err := env.Client.Memberships().Get(ctx, pk)
		seatsAreFive(t, got, err)
	})

	t.Run("SkipTenancy uses the caller's PK tenant", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		crossTenant(t, env, func(other context.Context) error {
			return increment(other, env.Client.Memberships(), pk, skipTenancy)
		})
		got, err := env.Client.Memberships().Get(ctx, pk)
		seatsAreFive(t, got, err)
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if err := increment(ctx, c, seeded[0]); err != nil {
				t.Fatalf("Increment() error = %v", err)
			}
			return seeded[:1]
		})
	})
}

func tenancySoftDelete(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks SoftDelete", func(r *stampRun) []any {
			id := r.seedTasks()[0].ID
			if _, err := r.env.Client.Tasks().SoftDelete(r.ctx, id, r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDelete() error = %v", err)
			}
			return []any{id}
		})
	})
}

func tenancySoftDeleteMany(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks SoftDeleteMany", func(r *stampRun) []any {
			ids := taskIDs(r.seedTasks())
			if _, err := r.env.Client.Tasks().SoftDeleteMany(r.ctx, ids, r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDeleteMany() error = %v", err)
			}
			return int64Keys(ids)
		})
	})
}

func tenancySoftDeleteWhere(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks SoftDeleteWhere", func(r *stampRun) []any {
			ids := taskIDs(r.seedTasks())
			if _, err := r.env.Client.Tasks().SoftDeleteWhere(r.ctx, &models.TaskFilter{ID: &comparator.Number[int64]{In: ids}}, r.taskOpts()...); err != nil {
				r.t.Fatalf("SoftDeleteWhere() error = %v", err)
			}
			return int64Keys(ids)
		})
	})
}

func tenancyHardDelete(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes HardDelete", func(r *stampRun) []any {
			id := r.seedNotes()[0].ID
			if err := r.env.Client.Notes().HardDelete(r.ctx, id, r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDelete() error = %v", err)
			}
			return []any{id}
		})
	})

	gone := func(t *testing.T, ctx context.Context, env *Env, pk models.MembershipPK) {
		t.Helper()
		if _, err := env.Client.Memberships().Get(ctx, pk); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get() after HardDelete error = %v, want ErrNotFound", err)
		}
	}

	t.Run("a matching PK tenant proceeds", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		if err := env.Client.Memberships().HardDelete(ctx, pk); err != nil {
			t.Fatalf("HardDelete() with the matching tenant error = %v", err)
		}
		gone(t, ctx, env, pk)
	})

	t.Run("SkipTenancy uses the caller's PK tenant", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		crossTenant(t, env, func(other context.Context) error {
			return env.Client.Memberships().HardDelete(other, pk, skipTenancy)
		})
		gone(t, ctx, env, pk)
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if err := c.HardDelete(ctx, seeded[0]); err != nil {
				t.Fatalf("HardDelete() error = %v", err)
			}
			return seeded[:1]
		})
	})
}

func tenancyHardDeleteMany(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes HardDeleteMany", func(r *stampRun) []any {
			ids := noteIDs(r.seedNotes())
			if err := r.env.Client.Notes().HardDeleteMany(r.ctx, ids, r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDeleteMany() error = %v", err)
			}
			return int64Keys(ids)
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if err := c.HardDeleteMany(ctx, seeded); err != nil {
				t.Fatalf("HardDeleteMany() error = %v", err)
			}
			return seeded
		})
	})
}

func tenancyHardDeleteWhere(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "notes HardDeleteWhere", func(r *stampRun) []any {
			ids := noteIDs(r.seedNotes())
			if err := r.env.Client.Notes().HardDeleteWhere(r.ctx, &models.NoteFilter{ID: &comparator.Number[int64]{In: ids}}, r.noteOpts()...); err != nil {
				r.t.Fatalf("HardDeleteWhere() error = %v", err)
			}
			return int64Keys(ids)
		})
	})

	t.Run("AffectedPKs hold the full PK, tenant included", func(t *testing.T) {
		checkAffectedPKs(t, h, func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK {
			if err := c.HardDeleteWhere(ctx, &models.MembershipFilter{PKs: seeded}); err != nil {
				t.Fatalf("HardDeleteWhere() error = %v", err)
			}
			return seeded
		})
	})
}

func tenancyRestore(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks Restore", func(r *stampRun) []any {
			id := r.seedTrashedTasks()[0].ID
			if _, err := r.env.Client.Tasks().Restore(r.ctx, id, r.taskOpts()...); err != nil {
				r.t.Fatalf("Restore() error = %v", err)
			}
			return []any{id}
		})
	})
}

func tenancyRestoreMany(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks RestoreMany", func(r *stampRun) []any {
			ids := taskIDs(r.seedTrashedTasks())
			if _, err := r.env.Client.Tasks().RestoreMany(r.ctx, ids, r.taskOpts()...); err != nil {
				r.t.Fatalf("RestoreMany() error = %v", err)
			}
			return int64Keys(ids)
		})
	})
}

func tenancyRestoreWhere(t *testing.T, h *Harness) {
	t.Run("each event is stamped with its row's tenant", func(t *testing.T) {
		checkStamps(t, h, "tasks RestoreWhere", func(r *stampRun) []any {
			ids := taskIDs(r.seedTrashedTasks())
			if _, err := r.env.Client.Tasks().RestoreWhere(r.ctx, &models.TaskFilter{ID: &comparator.Number[int64]{In: ids}}, r.taskOpts()...); err != nil {
				r.t.Fatalf("RestoreWhere() error = %v", err)
			}
			return int64Keys(ids)
		})
	})
}

func tenancyGet(t *testing.T, h *Harness) {
	isRow := func(t *testing.T, got *models.Membership, err error, pk models.MembershipPK) {
		t.Helper()
		if err != nil || got.WorkspaceID != pk.WorkspaceID || got.Member != pk.Member {
			t.Errorf("Get(%v) = %+v, %v, want that row", pk, got, err)
		}
	}

	t.Run("a matching PK tenant proceeds", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		got, err := env.Client.Memberships().Get(ctx, pk)
		isRow(t, got, err, pk)
	})

	t.Run("SkipTenancy uses the caller's PK tenant", func(t *testing.T) {
		env, _, pk := membershipEnv(t, h)
		var got *models.Membership
		crossTenant(t, env, func(other context.Context) error {
			var err error
			got, err = env.Client.Memberships().Get(other, pk, skipTenancy)
			return err
		})
		isRow(t, got, nil, pk)
	})
}

func tenancyExists(t *testing.T, h *Harness) {
	t.Run("a matching PK tenant proceeds", func(t *testing.T) {
		env, ctx, pk := membershipEnv(t, h)
		if ok, err := env.Client.Memberships().Exists(ctx, pk); err != nil || !ok {
			t.Errorf("Exists() with the matching tenant = %v, %v, want true, nil", ok, err)
		}
	})

	t.Run("SkipTenancy uses the caller's PK tenant", func(t *testing.T) {
		env, _, pk := membershipEnv(t, h)
		var ok bool
		crossTenant(t, env, func(other context.Context) error {
			var err error
			ok, err = env.Client.Memberships().Exists(other, pk, skipTenancy)
			return err
		})
		if !ok {
			t.Error("Exists() of another tenant's PK under SkipTenancy = false, want true")
		}
	})
}

func tenancyGetMany(t *testing.T, h *Harness) {
	t.Run("a filter on the tenant column selects that tenant's rows", func(t *testing.T) {
		h.Reset(t)
		c := h.Client(t)
		mine, theirs := models.NewTenant(), models.NewTenant()
		want := map[int64]bool{}
		for _, n := range []struct {
			tenant models.Tenant
			slug   string
		}{{mine, "a"}, {theirs, "b"}, {theirs, "c"}} {
			created, err := c.Notes().Create(WithTenant(context.Background(), n.tenant), &models.CreateNoteInput{Slug: n.slug, Body: n.slug})
			if err != nil {
				t.Fatalf("Create(%s) error = %v", n.slug, err)
			}
			if n.tenant == theirs {
				want[created.ID] = true
			}
		}
		// models.TenantEq builds the comparator the generated filter declares
		// for the tenant column, so this compiles only when it follows the
		// tenant's Go type.
		got, err := c.Notes().GetMany(WithTenant(context.Background(), mine),
			&models.GetNotesInput{Filter: &models.NoteFilter{WorkspaceID: models.TenantEq(theirs)}}, skipTenancy)
		if err != nil {
			t.Fatalf("GetMany() error = %v", err)
		}
		if len(got) != len(want) {
			t.Errorf("GetMany() returned %d rows, want %d", len(got), len(want))
		}
		for _, n := range got {
			if !want[n.ID] || n.WorkspaceID != theirs {
				t.Errorf("GetMany() returned note %d of tenant %v, want only the filtered tenant's notes", n.ID, n.WorkspaceID)
			}
		}
	})
}

func tenancyUpdateWithRelated(t *testing.T, h *Harness) {
	t.Run("an unlink under a parent of another tenant is ErrAlreadyRelated", func(t *testing.T) {
		cases := []struct {
			name, edge, verb string
			input            func(note, tag int64) *models.UpdateTemplateWithRelatedInput
		}{
			{"o2m disconnect", "TemplateNotes", "disconnect", func(note, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{note}}}
			}},
			{"o2m disconnect naming no row", "TemplateNotes", "disconnect", func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{-1}}}
			}},
			{"o2m clear", "TemplateNotes", "clear", func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true}}
			}},
			{"m2m disconnect", "Tags", "disconnect", func(_, tag int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Tags: &models.TemplateTagsUpdateNested{Disconnect: []int64{tag}}}
			}},
			{"m2m disconnect naming no row", "Tags", "disconnect", func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Tags: &models.TemplateTagsUpdateNested{Disconnect: []int64{-1}}}
			}},
			{"m2m clear", "Tags", "clear", func(_, _ int64) *models.UpdateTemplateWithRelatedInput {
				return &models.UpdateTemplateWithRelatedInput{Tags: &models.TemplateTagsUpdateNested{Clear: true}}
			}},
		}
		for _, tc := range cases {
			h.Reset(t)
			c := h.Client(t)
			template, note, tag := linkedTemplate(t, c, models.NewTenant())
			resolved := WithTenant(context.Background(), models.NewTenant())
			_, err := c.Templates().UpdateWithRelated(resolved, template, tc.input(note, tag))
			assertNestedError(t, tc.name, err, models.ErrAlreadyRelated, tc.edge, tc.verb)
			if notes, tags := linkedRows(t, c, template); notes != 1 || tags != 1 {
				t.Errorf("%s: rows left = %d notes, %d tags, want 1 of each", tc.name, notes, tags)
			}
		}
	})

	t.Run("an unlink on the parent's tenant writes", func(t *testing.T) {
		type option = func(*models.CallOptions[models.TemplateFieldOptions])
		cases := []struct {
			name string
			// call returns the context and options of the unlinking call, given
			// the tenant that owns the template.
			call func(owner models.Tenant) (context.Context, []option)
		}{
			{"the resolved tenant", func(owner models.Tenant) (context.Context, []option) {
				return WithTenant(context.Background(), owner), nil
			}},
			{"SkipTenancy", func(models.Tenant) (context.Context, []option) {
				return WithTenant(context.Background(), models.NewTenant()), []option{skipTenancy}
			}},
			{"an explicit tenant", func(owner models.Tenant) (context.Context, []option) {
				explicit := func(o *models.CallOptions[models.TemplateFieldOptions]) { o.Tenant = new(owner) }
				return WithTenant(context.Background(), models.NewTenant()), []option{explicit}
			}},
		}
		for _, tc := range cases {
			h.Reset(t)
			c := h.Client(t)
			owner := models.NewTenant()
			ctx, opts := tc.call(owner)

			template, note, tag := linkedTemplate(t, c, owner)
			if _, err := c.Templates().UpdateWithRelated(ctx, template, &models.UpdateTemplateWithRelatedInput{
				TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{note}},
				Tags:          &models.TemplateTagsUpdateNested{Disconnect: []int64{tag}},
			}, opts...); err != nil {
				t.Fatalf("%s: disconnect error = %v", tc.name, err)
			}
			if notes, tags := linkedRows(t, c, template); notes != 0 || tags != 0 {
				t.Errorf("%s: rows left after disconnect = %d notes, %d tags, want none", tc.name, notes, tags)
			}

			template, _, _ = linkedTemplate(t, c, owner)
			if _, err := c.Templates().UpdateWithRelated(ctx, template, &models.UpdateTemplateWithRelatedInput{
				TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true},
				Tags:          &models.TemplateTagsUpdateNested{Clear: true},
			}, opts...); err != nil {
				t.Fatalf("%s: clear error = %v", tc.name, err)
			}
			if notes, tags := linkedRows(t, c, template); notes != 0 || tags != 0 {
				t.Errorf("%s: rows left after clear = %d notes, %d tags, want none", tc.name, notes, tags)
			}
		}
	})

	t.Run("a disconnect miss on the parent's tenant is a no-op", func(t *testing.T) {
		h.Reset(t)
		c := h.Client(t)
		owner := models.NewTenant()
		template, _, _ := linkedTemplate(t, c, owner)
		if _, err := c.Templates().UpdateWithRelated(WithTenant(context.Background(), owner), template, &models.UpdateTemplateWithRelatedInput{
			TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Disconnect: []int64{-1}},
			Tags:          &models.TemplateTagsUpdateNested{Disconnect: []int64{-1}},
		}); err != nil {
			t.Fatalf("disconnect error = %v", err)
		}
		if notes, tags := linkedRows(t, c, template); notes != 1 || tags != 1 {
			t.Errorf("rows left = %d notes, %d tags, want 1 of each", notes, tags)
		}
	})

	t.Run("an M2M create whose target differs on a shared junction column is ErrAlreadyRelated", func(t *testing.T) {
		h.Reset(t)
		c := h.Client(t)
		bg := context.Background()
		mine, other := models.NewTenant(), models.NewTenant()
		ctx := WithTenant(bg, mine)
		count := func(name string, n int64, err error) int64 {
			t.Helper()
			if err != nil {
				t.Fatalf("Count(%s) error = %v", name, err)
			}
			return n
		}
		tags := func() int64 { n, err := c.Tags().Count(bg, nil, skipTenancy); return count("tags", n, err) }
		links := func() int64 {
			n, err := c.BoardTags().Count(bg, nil, skipTenancy)
			m, err2 := c.TemplateTags().Count(bg, nil, skipTenancy)
			return count("board_tags", n, err) + count("template_tags", m, err2)
		}
		assertRefused := func(how string, err error) {
			t.Helper()
			assertNestedError(t, how, err, models.ErrAlreadyRelated, "Tags", "create")
			if nested, ok := errors.AsType[*models.NestedMutationError](err); ok && nested.ID != nil {
				t.Errorf("%s: error names %v, want no id", how, nested.ID)
			}
			if n := tags(); n != 0 {
				t.Errorf("%s: tags after the refusal = %d, want 0", how, n)
			}
			if n := links(); n != 0 {
				t.Errorf("%s: links after the refusal = %d, want 0", how, n)
			}
		}

		// Tenancy sets the created tag's workspace_id to the resolved tenant,
		// while the junction takes it from the template's owner.
		template, err := c.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: other, Name: "theirs"})
		if err != nil {
			t.Fatalf("Create(template) error = %v", err)
		}
		_, err = c.Templates().UpdateWithRelated(ctx, template.ID, &models.UpdateTemplateWithRelatedInput{
			Tags: &models.TemplateTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "set by tenancy"}}},
		})
		assertRefused("a tenancy-set tag under a shared parent", err)

		board, err := c.Boards().Create(ctx, &models.CreateBoardInput{Name: "mine"})
		if err != nil {
			t.Fatalf("Create(board) error = %v", err)
		}
		_, err = c.Boards().UpdateWithRelated(ctx, board.ID, &models.UpdateBoardWithRelatedInput{
			Tags: &models.BoardTagsUpdateNested{Create: []*models.CreateTagInput{{WorkspaceID: omittable.Set(other), Name: "set by the caller"}}},
		}, skipTenancy)
		assertRefused("a caller-set tag under SkipTenancy", err)

		// The check reads the created row's own value: a match still links.
		if _, err := c.Boards().UpdateWithRelated(ctx, board.ID, &models.UpdateBoardWithRelatedInput{
			Tags: &models.BoardTagsUpdateNested{Create: []*models.CreateTagInput{{Name: "matching"}}},
		}); err != nil {
			t.Fatalf("UpdateWithRelated() creating a matching tag error = %v", err)
		}
		if n := links(); n != 1 {
			t.Errorf("links after a matching create = %d, want 1", n)
		}
	})
}

func tenancyUpsertWithRelated(t *testing.T, h *Harness) {
	t.Run("an unlink under a parent of another tenant rolls back the insert", func(t *testing.T) {
		h.Reset(t)
		c := h.Client(t)
		ctx := WithTenant(context.Background(), models.NewTenant())
		_, err := c.Templates().UpsertWithRelated(ctx, &models.UpsertTemplateWithRelatedInput{
			Template:      models.CreateTemplateInput{OwnerWorkspace: models.NewTenant(), Name: "fresh"},
			TemplateNotes: &models.TemplateTemplateNotesUpdateNested{Clear: true},
		}, models.TemplateConflictPK)
		assertNestedError(t, "UpsertWithRelated", err, models.ErrAlreadyRelated, "TemplateNotes", "clear")
		if n, err := c.Templates().Count(ctx, nil); err != nil || n != 0 {
			t.Errorf("templates = %d, %v, want 0: the insert rolls back", n, err)
		}
	})
}

// stampRun is one pass of a stamp check: a write under ctx over one row of
// each tenant in tenants. Under skip the write passes SkipTenancy, and ctx
// resolves a tenant none of the rows has.
type stampRun struct {
	t       *testing.T
	env     *Env
	ctx     context.Context
	tenants []models.Tenant
	skip    bool
}

// checkStamps runs write twice: under the resolved tenant over rows of that
// tenant, then under SkipTenancy over rows of two other tenants. Each time the
// write returns the keys it affected, in tenants order, and every event must
// carry its own row's tenant.
func checkStamps(t *testing.T, h *Harness, label string, write func(r *stampRun) []any) {
	t.Helper()
	resolved := models.NewTenant()
	passes := []struct {
		how     string
		tenants []models.Tenant
		skip    bool
	}{
		{"under the resolved tenant", []models.Tenant{resolved, resolved}, false},
		{"under SkipTenancy", []models.Tenant{models.NewTenant(), models.NewTenant()}, true},
	}
	for _, p := range passes {
		h.Reset(t)
		env := h.NewEnv(t, WithRecorder())
		r := &stampRun{t: t, env: env, ctx: WithTenant(context.Background(), resolved), tenants: p.tenants, skip: p.skip}
		keys := write(r)
		assertStamps(t, label+" "+p.how, env.Recorder, keys, p.tenants[:len(keys)])
	}
}

// assertStamps checks that rec holds exactly one event per key, each with
// Metadata["tenant"] set to its row's tenant formatted with %v.
func assertStamps(t *testing.T, label string, rec *Recorder, keys []any, tenants []models.Tenant) {
	t.Helper()
	want := map[string]string{}
	for i, k := range keys {
		want[fmt.Sprintf("%T(%v)", k, k)] = fmt.Sprintf("%v", tenants[i])
	}
	got := rec.Events()
	if len(got) != len(keys) {
		t.Errorf("%s: got %d events, want %d (one per affected row)", label, len(got), len(keys))
	}
	for _, e := range got {
		key := fmt.Sprintf("%T(%v)", e.PK, e.PK)
		stamp, ok := want[key]
		if !ok {
			t.Errorf("%s: event for %s, want only %v", label, key, slices.Sorted(maps.Keys(want)))
			continue
		}
		delete(want, key)
		if v, ok := e.Metadata["tenant"]; !ok || v != stamp {
			t.Errorf("%s: event for %s: Metadata[tenant] = %q (present %v), want %q", label, key, v, ok, stamp)
		}
	}
}

// workspace is the tenant column of the i-th created row: set under
// SkipTenancy, which does not fill it, and left to tenancy otherwise.
func (r *stampRun) workspace(i int) omittable.Value[models.Tenant] {
	if r.skip {
		return omittable.Set(r.tenants[i])
	}
	return omittable.Value[models.Tenant]{}
}

func (r *stampRun) noteOpts() []func(*models.CallOptions[models.NoteFieldOptions]) {
	return callOpts[models.NoteFieldOptions](r.skip)
}

func (r *stampRun) taskOpts() []func(*models.CallOptions[models.TaskFieldOptions]) {
	return callOpts[models.TaskFieldOptions](r.skip)
}

func (r *stampRun) membershipOpts() []func(*models.CallOptions[models.MembershipFieldOptions]) {
	return callOpts[models.MembershipFieldOptions](r.skip)
}

func callOpts[T any](skip bool) []func(*models.CallOptions[T]) {
	if skip {
		return []func(*models.CallOptions[T]){skipTenancy[T]}
	}
	return nil
}

func skipTenancy[T any](o *models.CallOptions[T]) { o.SkipTenancy = true }

// seedNotes creates one note per run tenant, each under its own tenant, then
// forgets the events.
func (r *stampRun) seedNotes() []*models.Note {
	r.t.Helper()
	var out []*models.Note
	for i, tenant := range r.tenants {
		s := fmt.Sprintf("seed-%d", i)
		n, err := r.env.Client.Notes().Create(WithTenant(context.Background(), tenant), &models.CreateNoteInput{Slug: s, Body: s})
		if err != nil {
			r.t.Fatalf("seed note %d error = %v", i, err)
		}
		out = append(out, n)
	}
	r.env.Recorder.Clear()
	return out
}

// seedTasks creates one task per run tenant, each under its own tenant, then
// forgets the events.
func (r *stampRun) seedTasks() []*models.Task {
	r.t.Helper()
	var out []*models.Task
	for i, tenant := range r.tenants {
		task, err := r.env.Client.Tasks().Create(WithTenant(context.Background(), tenant), &models.CreateTaskInput{Title: fmt.Sprintf("seed-%d", i)})
		if err != nil {
			r.t.Fatalf("seed task %d error = %v", i, err)
		}
		out = append(out, task)
	}
	r.env.Recorder.Clear()
	return out
}

// seedTrashedTasks is seedTasks with each task soft-deleted.
func (r *stampRun) seedTrashedTasks() []*models.Task {
	r.t.Helper()
	tasks := r.seedTasks()
	for i, task := range tasks {
		if _, err := r.env.Client.Tasks().SoftDelete(WithTenant(context.Background(), r.tenants[i]), task.ID); err != nil {
			r.t.Fatalf("soft-delete seed task %d error = %v", i, err)
		}
	}
	r.env.Recorder.Clear()
	return tasks
}

// seedMemberships creates one membership per run tenant, each under its own
// tenant, then forgets the events.
func (r *stampRun) seedMemberships() []models.MembershipPK {
	r.t.Helper()
	var out []models.MembershipPK
	for i, tenant := range r.tenants {
		pk := models.MembershipPK{WorkspaceID: tenant, Member: fmt.Sprintf("seed-%d", i)}
		if _, err := r.env.Client.Memberships().Create(WithTenant(context.Background(), tenant),
			&models.CreateMembershipInput{WorkspaceID: tenant, Member: pk.Member, Role: "member"}); err != nil {
			r.t.Fatalf("seed membership %d error = %v", i, err)
		}
		out = append(out, pk)
	}
	r.env.Recorder.Clear()
	return out
}

// membershipEnv empties the tables and returns a client, a context resolving
// a fresh tenant, and the key of one membership of that tenant with 2 seats.
func membershipEnv(t *testing.T, h *Harness) (*Env, context.Context, models.MembershipPK) {
	t.Helper()
	h.Reset(t)
	env := h.NewEnv(t)
	tenant := models.NewTenant()
	ctx := WithTenant(context.Background(), tenant)
	pk := models.MembershipPK{WorkspaceID: tenant, Member: "m"}
	in := &models.CreateMembershipInput{WorkspaceID: tenant, Member: pk.Member, Role: "member", Seats: omittable.Set(int64(2))}
	if _, err := env.Client.Memberships().Create(ctx, in); err != nil {
		t.Fatalf("Create(membership) error = %v", err)
	}
	return env, ctx, pk
}

// crossTenant runs call under a context resolving another tenant and requires
// it to succeed without consulting the resolver.
func crossTenant(t *testing.T, env *Env, call func(other context.Context) error) {
	t.Helper()
	before := env.Resolver.Calls()
	if err := call(WithTenant(context.Background(), models.NewTenant())); err != nil {
		t.Fatalf("SkipTenancy call on another tenant's PK error = %v", err)
	}
	if n := env.Resolver.Calls() - before; n != 0 {
		t.Errorf("the resolver ran %d times under SkipTenancy, want 0", n)
	}
}

// checkAffectedPKs seeds two memberships of one tenant, runs write under that
// tenant and requires the one mutation it makes to record exactly the keys it
// returns as MutationContext.AffectedPKs, as full MembershipPK values.
func checkAffectedPKs(t *testing.T, h *Harness, write func(c models.MembershipClient, ctx context.Context, seeded []models.MembershipPK) []models.MembershipPK) {
	t.Helper()
	h.Reset(t)
	var recorded [][]any
	record := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, mc *hook.MutationContext) (any, error) {
			res, err := next(ctx, mc)
			if mc.Table == models.TableMemberships {
				recorded = append(recorded, mc.AffectedPKs)
			}
			return res, err
		}
	}
	c := h.NewEnv(t, WithMutationHook(record)).Client.Memberships()
	tenant := models.NewTenant()
	ctx := WithTenant(context.Background(), tenant)
	seeded := []models.MembershipPK{{WorkspaceID: tenant, Member: "seed-0"}, {WorkspaceID: tenant, Member: "seed-1"}}
	for _, pk := range seeded {
		if _, err := c.Create(ctx, &models.CreateMembershipInput{WorkspaceID: tenant, Member: pk.Member, Role: "member"}); err != nil {
			t.Fatalf("seed membership %s error = %v", pk.Member, err)
		}
	}
	recorded = nil

	want := write(c, ctx, seeded)
	if len(recorded) != 1 {
		t.Fatalf("recorded %d memberships mutations, want 1", len(recorded))
	}
	var got []models.MembershipPK
	for i, pk := range recorded[0] {
		typed, ok := pk.(models.MembershipPK)
		if !ok {
			t.Fatalf("AffectedPKs[%d] = %T(%v), want a MembershipPK", i, pk, pk)
		}
		got = append(got, typed)
	}
	byMember := func(a, b models.MembershipPK) int { return cmp.Compare(a.Member, b.Member) }
	slices.SortFunc(got, byMember)
	want = slices.SortedFunc(slices.Values(want), byMember)
	if !slices.Equal(got, want) {
		t.Errorf("AffectedPKs = %v, want %v, tenant included", got, want)
	}
}

// linkedTemplate creates a template owned by owner with one note and one tag
// on it, all under owner's tenant.
func linkedTemplate(t *testing.T, c *models.Client, owner models.Tenant) (template, note, tag int64) {
	t.Helper()
	ctx := WithTenant(context.Background(), owner)
	tmpl, err := c.Templates().Create(ctx, &models.CreateTemplateInput{OwnerWorkspace: owner, Name: "owned"})
	if err != nil {
		t.Fatalf("Create(template) error = %v", err)
	}
	n, err := c.TemplateNotes().Create(ctx, &models.CreateTemplateNoteInput{TemplateID: omittable.Set(new(tmpl.ID)), Body: "n"})
	if err != nil {
		t.Fatalf("Create(template note) error = %v", err)
	}
	tg, err := c.Tags().Create(ctx, &models.CreateTagInput{Name: "t"})
	if err != nil {
		t.Fatalf("Create(tag) error = %v", err)
	}
	if _, err := c.TemplateTags().Create(ctx, &models.CreateTemplateTagInput{WorkspaceID: owner, TemplateID: tmpl.ID, TagID: tg.ID}); err != nil {
		t.Fatalf("Create(template tag) error = %v", err)
	}
	return tmpl.ID, n.ID, tg.ID
}

// linkedRows counts the notes and tag links still on template, across tenants.
func linkedRows(t *testing.T, c *models.Client, template int64) (notes, tags int64) {
	t.Helper()
	ctx := context.Background()
	notes, err := c.TemplateNotes().Count(ctx, &models.TemplateNoteFilter{
		TemplateID: &comparator.NullableNumber[int64]{Number: comparator.Number[int64]{Eq: &template}},
	}, skipTenancy)
	if err != nil {
		t.Fatalf("Count(template notes) error = %v", err)
	}
	tags, err = c.TemplateTags().Count(ctx, &models.TemplateTagFilter{TemplateID: &comparator.Number[int64]{Eq: &template}}, skipTenancy)
	if err != nil {
		t.Fatalf("Count(template tags) error = %v", err)
	}
	return notes, tags
}

// assertNestedError requires err to wrap want and to be attributed to edge and
// verb.
func assertNestedError(t *testing.T, label string, err, want error, edge, verb string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: error = %v, want %v", label, err, want)
	}
	nested, ok := errors.AsType[*models.NestedMutationError](err)
	if !ok {
		t.Fatalf("%s: error %v carries no NestedMutationError", label, err)
	}
	if nested.Edge != edge || nested.Verb != verb {
		t.Errorf("%s: attribution = %s %s, want %s %s", label, nested.Edge, nested.Verb, edge, verb)
	}
}

func int64Keys(ids []int64) []any {
	keys := make([]any, len(ids))
	for i, id := range ids {
		keys[i] = id
	}
	return keys
}

func membershipKeys(pks []models.MembershipPK) []any {
	keys := make([]any, len(pks))
	for i, pk := range pks {
		keys[i] = pk
	}
	return keys
}

// withMissing returns ids with a key no row has between its first two.
func withMissing(ids []int64) []int64 {
	return []int64{ids[0], slices.Max(ids) + 1000, ids[1]}
}
