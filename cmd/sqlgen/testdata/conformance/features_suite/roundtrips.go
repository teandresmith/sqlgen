package featuressuite

import (
	"context"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
)

// RunRoundTrips measures the database round trips the cache, tenancy and an
// event publisher add, on the shapes docs/round-trips.md reports, and checks
// that report. h.Base must sit on a pool whose driver reports every statement
// to rec. It is not part of Run: a round-trip count is a measurement the
// report publishes, not a PRD requirement.
func RunRoundTrips(t *testing.T, h *Harness, rec *roundtrip.Recorder) {
	roundtrip.Run(t, roundtrip.Config{
		Suite:    roundtrip.SuiteFeatures,
		Dialect:  h.Dialect,
		Driver:   h.Driver,
		Recorder: rec,
		Reset:    h.Reset,
	}, roundTripCases(h))
}

type op = func(ctx context.Context) error

const (
	groupCache   = "With a cache"
	groupTenancy = "With tenancy"
	groupEvents  = "With an event publisher"
)

// rtRows is how many rows a *Where case matches.
const rtRows = 10

func roundTripCases(h *Harness) []roundtrip.Case {
	c := func(group, method, shape, grows string, setup func(t *testing.T) op) roundtrip.Case {
		return roundtrip.Case{Group: group, Op: method, Shape: shape, Grows: grows, Setup: setup}
	}
	cached := func(t *testing.T) *models.Client { return h.NewEnv(t, WithCache()).Client }
	published := func(t *testing.T) *models.Client { return h.NewEnv(t, WithRecorder()).Client }
	return []roundtrip.Case{
		// The cache, on an untenanted table.
		c(groupCache, "Get", "cold cache", "", func(t *testing.T) op {
			client := cached(t)
			id := rtSeedRegions(t, h, 1)[0]
			return func(ctx context.Context) error { _, err := client.Regions().Get(ctx, id); return err }
		}),
		c(groupCache, "Get", "warm cache (same call made before)", "", func(t *testing.T) op {
			client := cached(t)
			id := rtSeedRegions(t, h, 1)[0]
			if _, err := client.Regions().Get(context.Background(), id); err != nil {
				t.Fatalf("warm Get: %v", err)
			}
			return func(ctx context.Context) error { _, err := client.Regions().Get(ctx, id); return err }
		}),
		c(groupCache, "GetMany", "cold cache, 10 rows", "", func(t *testing.T) op {
			client := cached(t)
			rtSeedRegions(t, h, rtRows)
			return func(ctx context.Context) error { _, err := client.Regions().GetMany(ctx, nil); return err }
		}),
		c(groupCache, "GetMany", "warm cache (same call made before)", "", func(t *testing.T) op {
			client := cached(t)
			rtSeedRegions(t, h, rtRows)
			if _, err := client.Regions().GetMany(context.Background(), nil); err != nil {
				t.Fatalf("warm GetMany: %v", err)
			}
			return func(ctx context.Context) error { _, err := client.Regions().GetMany(ctx, nil); return err }
		}),
		c(groupCache, "Count", "warm cache (same call made before)", "", func(t *testing.T) op {
			client := cached(t)
			rtSeedRegions(t, h, rtRows)
			if _, err := client.Regions().Count(context.Background(), nil); err != nil {
				t.Fatalf("warm Count: %v", err)
			}
			return func(ctx context.Context) error { _, err := client.Regions().Count(ctx, nil); return err }
		}),
		c(groupCache, "Create", "write-through", "", func(t *testing.T) op {
			client := cached(t)
			return func(ctx context.Context) error {
				_, err := client.Regions().Create(ctx, &models.CreateRegionInput{Name: "r"})
				return err
			}
		}),
		c(groupCache, "Update", "evicts the row", "", func(t *testing.T) op {
			client := cached(t)
			id := rtSeedRegions(t, h, 1)[0]
			return func(ctx context.Context) error {
				_, err := client.Regions().Update(ctx, id, &models.UpdateRegionInput{Name: omittable.Set("renamed")})
				return err
			}
		}),
		c(groupCache, "UpdateWhere", "evicts 10 matching rows", "", func(t *testing.T) op {
			client := cached(t)
			rtSeedRegions(t, h, rtRows)
			return func(ctx context.Context) error {
				_, err := client.Regions().UpdateWhere(ctx, rtRegionFilter(), &models.UpdateRegionInput{Name: omittable.Set("renamed")})
				return err
			}
		}),
		c(groupCache, "HardDelete", "evicts the row", "", func(t *testing.T) op {
			client := cached(t)
			id := rtSeedRegions(t, h, 1)[0]
			return func(ctx context.Context) error { return client.Regions().HardDelete(ctx, id) }
		}),
		c(groupCache, "HardDeleteWhere", "evicts 10 matching rows", "", func(t *testing.T) op {
			client := cached(t)
			rtSeedRegions(t, h, rtRows)
			return func(ctx context.Context) error { return client.Regions().HardDeleteWhere(ctx, rtRegionFilter()) }
		}),

		// Tenancy, with no cache and no publisher.
		c(groupTenancy, "Get", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			id := rtSeedNotes(t, client, tenant, 1)[0]
			return tenantOp(tenant, func(ctx context.Context) error { _, err := client.Notes().Get(ctx, id); return err })
		}),
		c(groupTenancy, "GetMany", "tenant-scoped, 10 rows", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			rtSeedNotes(t, client, tenant, rtRows)
			return tenantOp(tenant, func(ctx context.Context) error { _, err := client.Notes().GetMany(ctx, nil); return err })
		}),
		c(groupTenancy, "Get", "O2M (Cards) and M2M (Tags)", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			id := rtSeedBoard(t, client, tenant)
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Boards().Get(ctx, id, func(o *models.CallOptions[models.BoardFieldOptions]) {
					o.FieldOptions = &models.BoardFieldOptions{ID: true, Name: true, Cards: &models.CardRelationshipOptions{}, Tags: &models.TagRelationshipOptions{}}
				})
				return err
			})
		}),
		c(groupTenancy, "Create", "tenant from the resolver", "", func(t *testing.T) op {
			client := h.Client(t)
			return tenantOp(models.NewTenant(), func(ctx context.Context) error {
				_, err := client.Notes().Create(ctx, &models.CreateNoteInput{Slug: "n", Body: "n"})
				return err
			})
		}),
		c(groupTenancy, "Update", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			id := rtSeedNotes(t, client, tenant, 1)[0]
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Notes().Update(ctx, id, &models.UpdateNoteInput{Body: omittable.Set("changed")})
				return err
			})
		}),
		c(groupTenancy, "Update", "tenant in the composite key", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			ctx := WithTenant(context.Background(), tenant)
			if _, err := client.Memberships().Create(ctx, &models.CreateMembershipInput{WorkspaceID: tenant, Member: "m", Role: "member"}); err != nil {
				t.Fatalf("Create membership: %v", err)
			}
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Memberships().Update(ctx, models.MembershipPK{WorkspaceID: tenant, Member: "m"}, &models.UpdateMembershipInput{Role: omittable.Set("owner")})
				return err
			})
		}),
		c(groupTenancy, "UpdateWhere", "tenant-scoped, 10 matching rows", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			rtSeedNotes(t, client, tenant, rtRows)
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Notes().UpdateWhere(ctx, rtNoteFilter(), &models.UpdateNoteInput{Body: omittable.Set("changed")})
				return err
			})
		}),
		c(groupTenancy, "Upsert", "tenant-scoped, update (conflict)", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			rtSeedNotes(t, client, tenant, 1)
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Notes().Upsert(ctx, &models.CreateNoteInput{Slug: "n-0", Body: "changed"}, models.NoteConflictWorkspaceIDSlug)
				return err
			})
		}),
		c(groupTenancy, "HardDelete", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			id := rtSeedNotes(t, client, tenant, 1)[0]
			return tenantOp(tenant, func(ctx context.Context) error { return client.Notes().HardDelete(ctx, id) })
		}),
		c(groupTenancy, "SoftDelete", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := h.Client(t), models.NewTenant()
			task, err := client.Tasks().Create(WithTenant(context.Background(), tenant), &models.CreateTaskInput{Title: "t"})
			if err != nil {
				t.Fatalf("Create task: %v", err)
			}
			return tenantOp(tenant, func(ctx context.Context) error { _, err := client.Tasks().SoftDelete(ctx, task.ID); return err })
		}),
		c(groupTenancy, "CreateWithRelated", "O2M create 2 rows", "", func(t *testing.T) op {
			client := h.Client(t)
			return tenantOp(models.NewTenant(), func(ctx context.Context) error {
				_, err := client.Boards().CreateWithRelated(ctx, &models.CreateBoardWithRelatedInput{
					Board: models.CreateBoardInput{Name: "b"},
					Cards: &models.BoardCardsCreateNested{Create: []*models.BoardCardsCreateInput{{Title: "x"}, {Title: "y"}}},
				})
				return err
			})
		}),

		// An event publisher, on a tenanted table: compare each row with its
		// tenancy row above, which has no publisher.
		c(groupEvents, "Create", "tenant-scoped", "", func(t *testing.T) op {
			client := published(t)
			return tenantOp(models.NewTenant(), func(ctx context.Context) error {
				_, err := client.Notes().Create(ctx, &models.CreateNoteInput{Slug: "n", Body: "n"})
				return err
			})
		}),
		c(groupEvents, "Update", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := published(t), models.NewTenant()
			id := rtSeedNotes(t, client, tenant, 1)[0]
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Notes().Update(ctx, id, &models.UpdateNoteInput{Body: omittable.Set("changed")})
				return err
			})
		}),
		c(groupEvents, "UpdateWhere", "tenant-scoped, 10 matching rows", "", func(t *testing.T) op {
			client, tenant := published(t), models.NewTenant()
			rtSeedNotes(t, client, tenant, rtRows)
			return tenantOp(tenant, func(ctx context.Context) error {
				_, err := client.Notes().UpdateWhere(ctx, rtNoteFilter(), &models.UpdateNoteInput{Body: omittable.Set("changed")})
				return err
			})
		}),
		c(groupEvents, "HardDelete", "tenant-scoped", "", func(t *testing.T) op {
			client, tenant := published(t), models.NewTenant()
			id := rtSeedNotes(t, client, tenant, 1)[0]
			return tenantOp(tenant, func(ctx context.Context) error { return client.Notes().HardDelete(ctx, id) })
		}),
		c(groupEvents, "HardDeleteWhere", "tenant-scoped, 10 matching rows", "", func(t *testing.T) op {
			client, tenant := published(t), models.NewTenant()
			rtSeedNotes(t, client, tenant, rtRows)
			return tenantOp(tenant, func(ctx context.Context) error { return client.Notes().HardDeleteWhere(ctx, rtNoteFilter()) })
		}),
	}
}

// tenantOp runs fn with tenant on its context.
func tenantOp(tenant models.Tenant, fn op) op {
	return func(ctx context.Context) error { return fn(WithTenant(ctx, tenant)) }
}

func rtRegionFilter() *models.RegionFilter {
	return &models.RegionFilter{Name: &comparator.String{StartsWith: new("r-")}}
}

func rtNoteFilter() *models.NoteFilter {
	return &models.NoteFilter{Slug: &comparator.String{StartsWith: new("n-")}}
}

func rtSeedRegions(t *testing.T, h *Harness, n int) []int64 {
	t.Helper()
	client := h.Client(t)
	ids := make([]int64, n)
	for i := range ids {
		r, err := client.Regions().Create(context.Background(), &models.CreateRegionInput{Name: fmt.Sprintf("r-%02d", i)})
		if err != nil {
			t.Fatalf("Create region: %v", err)
		}
		ids[i] = r.ID
	}
	return ids
}

func rtSeedNotes(t *testing.T, client *models.Client, tenant models.Tenant, n int) []int64 {
	t.Helper()
	ctx := WithTenant(context.Background(), tenant)
	ids := make([]int64, n)
	for i := range ids {
		s := fmt.Sprintf("n-%d", i)
		note, err := client.Notes().Create(ctx, &models.CreateNoteInput{Slug: s, Body: s})
		if err != nil {
			t.Fatalf("Create note: %v", err)
		}
		ids[i] = note.ID
	}
	return ids
}

// rtSeedBoard creates a board with two cards and two tags in tenant's
// workspace and returns its key.
func rtSeedBoard(t *testing.T, client *models.Client, tenant models.Tenant) int64 {
	t.Helper()
	ctx := WithTenant(context.Background(), tenant)
	b, err := client.Boards().CreateWithRelated(ctx, &models.CreateBoardWithRelatedInput{
		Board: models.CreateBoardInput{Name: "b"},
		Cards: &models.BoardCardsCreateNested{Create: []*models.BoardCardsCreateInput{{Title: "x"}, {Title: "y"}}},
		Tags:  &models.BoardTagsCreateNested{Create: []*models.CreateTagInput{{Name: "x"}, {Name: "y"}}},
	})
	if err != nil {
		t.Fatalf("Create board: %v", err)
	}
	return b.ID
}
