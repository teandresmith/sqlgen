package tests

import (
	"context"
	"errors"
	"testing"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Idempotent upsert on a tenanted table: tags has a single-column AUTOINCREMENT PK and
// UNIQUE (workspace_id, name), and the tenant column is excluded from the
// UPDATE-set half per PRD §29.4.2. Upserting on that unique key therefore
// leaves no column to assign at all, so the dialect emits DO NOTHING — which
// returns no row on conflict, leaving RETURNING with no PK and no tenant.
//
// The tenant assertion is the second half: the conflicting row keeps its own
// tenant, and cache invalidation and event stamping rebuild the tenant-scoped
// key from the value the write path captured (PRD §29.5).
func TestUpsert_IdempotentEmptyUpdateColumns(t *testing.T) {
	resetDB(t)

	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	ctx := context.Background()

	first, err := envA.client.Tags().Upsert(ctx, &models.CreateTagInput{Name: "idempotent"}, models.TagConflictWorkspaceIDName)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	if first.WorkspaceID != tenantA {
		t.Fatalf("Upsert (insert) WorkspaceID = %v, want %v", first.WorkspaceID, tenantA)
	}

	second, err := envA.client.Tags().Upsert(ctx, &models.CreateTagInput{Name: "idempotent"}, models.TagConflictWorkspaceIDName)
	if err != nil {
		t.Fatalf("Upsert (idempotent) = %v, want nil (ErrNotFound=%v)", err, errors.Is(err, models.ErrNotFound))
	}
	if second.ID != first.ID {
		t.Errorf("Upsert (idempotent) ID = %d, want %d", second.ID, first.ID)
	}
	if second.Name != "idempotent" {
		t.Errorf("Upsert (idempotent) name = %q, want %q", second.Name, "idempotent")
	}
	if second.WorkspaceID != tenantA {
		t.Errorf("Upsert (idempotent) WorkspaceID = %v, want %v", second.WorkspaceID, tenantA)
	}
}

// The conflict target is tenant-scoped, so the same name under another tenant
// is a fresh insert rather than a conflict. This pins that the lookup the
// fallback performs is bounded by the conflict columns it was handed — a
// lookup on `name` alone would resolve tenant B's upsert to tenant A's row.
func TestUpsert_IdempotentEmptyUpdateColumnsPerTenant(t *testing.T) {
	resetDB(t)

	ctx := context.Background()
	envA := newEnv(t, withResolver(staticResolver(tenantA)))
	envB := newEnv(t, withResolver(staticResolver(tenantB)))

	tagA, err := envA.client.Tags().Upsert(ctx, &models.CreateTagInput{Name: "shared"}, models.TagConflictWorkspaceIDName)
	if err != nil {
		t.Fatalf("Upsert tenant A: %v", err)
	}
	tagB, err := envB.client.Tags().Upsert(ctx, &models.CreateTagInput{Name: "shared"}, models.TagConflictWorkspaceIDName)
	if err != nil {
		t.Fatalf("Upsert tenant B: %v", err)
	}
	if tagB.ID == tagA.ID {
		t.Fatalf("Upsert tenant B resolved to tenant A's row (ID = %d)", tagB.ID)
	}
	if tagB.WorkspaceID != tenantB {
		t.Errorf("Upsert tenant B WorkspaceID = %v, want %v", tagB.WorkspaceID, tenantB)
	}

	// Re-upserting under B is now the idempotent conflict, and must stay on B's row.
	again, err := envB.client.Tags().Upsert(ctx, &models.CreateTagInput{Name: "shared"}, models.TagConflictWorkspaceIDName)
	if err != nil {
		t.Fatalf("Upsert tenant B (idempotent): %v", err)
	}
	if again.ID != tagB.ID {
		t.Errorf("Upsert tenant B (idempotent) ID = %d, want %d", again.ID, tagB.ID)
	}
	if again.WorkspaceID != tenantB {
		t.Errorf("Upsert tenant B (idempotent) WorkspaceID = %v, want %v", again.WorkspaceID, tenantB)
	}
}

// Idempotent upsert on the caller-known-PK branch: tag_links is a tenanted
// pure junction, so an upsert on its composite PK has nothing to SET and the
// dialect emits DO NOTHING — which returns no row. The PK is known from the
// input here, but the tenant is not: it has to come back from the row so cache
// invalidation and event stamping can rebuild the tenant-scoped key (PRD
// §29.5). Scanning the empty result set directly would surface the driver's
// no-rows error instead.
func TestUpsert_IdempotentCompositePKTenantCapture(t *testing.T) {
	resetDB(t)

	ctx := context.Background()
	envA := newEnv(t, withResolver(staticResolver(tenantA)))

	first, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "first"})
	if err != nil {
		t.Fatalf("Create tag: %v", err)
	}
	second, err := envA.client.Tags().Create(ctx, &models.CreateTagInput{Name: "second"})
	if err != nil {
		t.Fatalf("Create tag: %v", err)
	}

	input := func() *models.CreateTagLinkInput {
		return &models.CreateTagLinkInput{TagID: first.ID, RelatedTagID: second.ID}
	}

	link, err := envA.client.TagLinks().Upsert(ctx, input(), models.TagLinkConflictPK)
	if err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}
	if link.WorkspaceID != tenantA {
		t.Fatalf("Upsert (insert) WorkspaceID = %v, want %v", link.WorkspaceID, tenantA)
	}

	// The idempotent conflict — nothing to SET, so no row comes back.
	again, err := envA.client.TagLinks().Upsert(ctx, input(), models.TagLinkConflictPK)
	if err != nil {
		t.Fatalf("Upsert (idempotent) = %v, want nil (ErrNotFound=%v)", err, errors.Is(err, models.ErrNotFound))
	}
	if again.TagID != first.ID || again.RelatedTagID != second.ID {
		t.Errorf("Upsert (idempotent) PK = (%d, %d), want (%d, %d)", again.TagID, again.RelatedTagID, first.ID, second.ID)
	}
	if again.WorkspaceID != tenantA {
		t.Errorf("Upsert (idempotent) WorkspaceID = %v, want %v", again.WorkspaceID, tenantA)
	}
}
