package tests

import (
	"context"
	"slices"
	"sync"
	"testing"
	"uuid"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// mutationRecord captures a MutationContext's carriers after the terminal
// handler populated them.
type mutationRecord struct {
	op      hook.MutationOp
	pks     []any
	tenants []any
}

// mutationRecorder is a MutationHook that snapshots AffectedPKs and
// AffectedTenants for every successful articles mutation. Snapshots are
// deep-copied because the context is not retained past the hook chain.
type mutationRecorder struct {
	mu      sync.Mutex
	records []mutationRecord
}

func (r *mutationRecorder) hook(next hook.MutationHandler) hook.MutationHandler {
	return func(ctx context.Context, m *hook.MutationContext) (any, error) {
		res, err := next(ctx, m)
		if err == nil && m.Table == models.TableArticles {
			r.mu.Lock()
			r.records = append(r.records, mutationRecord{
				op:      m.Op,
				pks:     slices.Clone(m.AffectedPKs),
				tenants: slices.Clone(m.AffectedTenants),
			})
			r.mu.Unlock()
		}
		return res, err
	}
}

func (r *mutationRecorder) byOp(op hook.MutationOp) []mutationRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []mutationRecord
	for _, rec := range r.records {
		if rec.op == op {
			out = append(out, rec)
		}
	}
	return out
}

// TestAffectedTenants_indexAlignedWithAffectedPKs pins the PRD §29.5
// alignment invariant: for a tenanted table whose tenant column is
// NOT part of the primary key, every mutation fills m.AffectedTenants in
// lockstep with m.AffectedPKs — AffectedTenants[i] is the tenant of the row
// AffectedPKs[i] — captured structurally from the mutated rows themselves
// (SQLite RETURNING here), not from the resolver. The batch runs under
// SkipTenancy across rows of two tenants, so a resolver-derived value could
// not produce the per-row answer this test demands.
func TestAffectedTenants_indexAlignedWithAffectedPKs(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	recorder := &mutationRecorder{}
	client := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithMutationHook(recorder.hook),
	)

	// One article per tenant, created under SkipTenancy with an explicit
	// tenant column so a single client can seed both tenants.
	tenantByArticle := make(map[int64]uuid.UUID)
	for _, tt := range []struct {
		title  string
		tenant uuid.UUID
	}{
		{"a-article", tenantA},
		{"b-article", tenantB},
	} {
		created, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:       tt.title,
			WorkspaceID: omittable.Set(tt.tenant),
		}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true })
		if err != nil {
			t.Fatalf("create %s: %v", tt.title, err)
		}
		tenantByArticle[created.ID] = tt.tenant
	}

	// assertAligned checks one record against the seeded tenant map.
	assertAligned := func(t *testing.T, rec mutationRecord, wantLen int) {
		t.Helper()
		if len(rec.pks) != wantLen {
			t.Fatalf("%s AffectedPKs length = %d, want %d", rec.op, len(rec.pks), wantLen)
		}
		if len(rec.tenants) != len(rec.pks) {
			t.Fatalf("%s AffectedTenants length = %d, want %d (index-aligned with AffectedPKs)",
				rec.op, len(rec.tenants), len(rec.pks))
		}
		for i, pk := range rec.pks {
			id, ok := pk.(int64)
			if !ok {
				t.Fatalf("%s AffectedPKs[%d] = %T, want int64", rec.op, i, pk)
			}
			got, ok := rec.tenants[i].(uuid.UUID)
			if !ok {
				t.Fatalf("%s AffectedTenants[%d] = %T (%v), want uuid.UUID", rec.op, i, rec.tenants[i], rec.tenants[i])
			}
			if want := tenantByArticle[id]; got != want {
				t.Errorf("%s AffectedTenants[%d] = %s, want %s (tenant of article %d)", rec.op, i, got, want, id)
			}
		}
	}

	// Create: one carrier element per created row, matching the explicit
	// tenant that was written.
	creates := recorder.byOp(hook.OpCreate)
	if len(creates) != 2 {
		t.Fatalf("recorded %d OpCreate mutations, want 2", len(creates))
	}
	for _, rec := range creates {
		assertAligned(t, rec, 1)
	}

	// UpdateMany across both tenants under SkipTenancy — the multi-row
	// alignment pin. Per-item RETURNING captures each row's own tenant.
	ids := make([]int64, 0, len(tenantByArticle))
	for id := range tenantByArticle {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	items := make([]models.UpdateArticleItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, models.UpdateArticleItem{
			ID:    id,
			Input: &models.UpdateArticleInput{Body: omittable.Set(new("updated"))},
		})
	}
	if _, err := client.Articles().UpdateMany(
		ctx, items,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("UpdateMany under SkipTenancy: %v", err)
	}
	updates := recorder.byOp(hook.OpUpdateMany)
	if len(updates) != 1 {
		t.Fatalf("recorded %d OpUpdateMany mutations, want 1", len(updates))
	}
	assertAligned(t, updates[0], 2)

	// SoftDeleteMany exercises the single-batched-statement RETURNING shape
	// (PK + tenant projected together and re-aligned to the input order).
	if _, err := client.Articles().SoftDeleteMany(
		ctx, ids,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("SoftDeleteMany under SkipTenancy: %v", err)
	}
	softDeletes := recorder.byOp(hook.OpSoftDeleteMany)
	if len(softDeletes) != 1 {
		t.Fatalf("recorded %d OpSoftDeleteMany mutations, want 1", len(softDeletes))
	}
	assertAligned(t, softDeletes[0], 2)

	// Single-row update on tenant B's article under SkipTenancy — the
	// resolver would say tenant A; the structural capture must say B.
	var bArticle int64
	for id, tn := range tenantByArticle {
		if tn == tenantB {
			bArticle = id
		}
	}
	if _, err := client.Articles().Restore(
		ctx, bArticle,
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("Restore under SkipTenancy: %v", err)
	}
	restores := recorder.byOp(hook.OpRestore)
	if len(restores) != 1 {
		t.Fatalf("recorded %d OpRestore mutations, want 1", len(restores))
	}
	assertAligned(t, restores[0], 1)
}

// TestAffectedTenants_absentForTenantInPKAndSharedTables pins the additive
// contract's other half: tenant-in-PK tables (order_items) and non-tenanted
// tables (audit_logs) never populate the carrier — consumers read the tenant
// from the PK struct, or there is no tenant at all.
func TestAffectedTenants_absentForTenantInPKAndSharedTables(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	var mu sync.Mutex
	tenantsByTable := make(map[hook.TableName][][]any)
	spy := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if err == nil {
				mu.Lock()
				tenantsByTable[m.Table] = append(tenantsByTable[m.Table], m.AffectedTenants)
				mu.Unlock()
			}
			return res, err
		}
	}
	client := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithMutationHook(spy),
	)

	if _, err := client.OrderItems().Create(ctx, &models.CreateOrderItemInput{
		WorkspaceID: tenantA, OrderID: 1, ProductID: 1, Quantity: 2, UnitPrice: 9.5,
	}); err != nil {
		t.Fatalf("create order item: %v", err)
	}
	if _, err := client.AuditLogs().Create(ctx, &models.CreateAuditLogInput{
		Message: "hello",
	}); err != nil {
		t.Fatalf("create audit log: %v", err)
	}

	for _, table := range []hook.TableName{models.TableOrderItems, models.TableAuditLogs} {
		for i, tenants := range tenantsByTable[table] {
			if tenants != nil {
				t.Errorf("%s mutation %d: AffectedTenants = %v, want nil (no carrier for tenant-in-PK / shared tables)",
					table, i, tenants)
			}
		}
	}
}
