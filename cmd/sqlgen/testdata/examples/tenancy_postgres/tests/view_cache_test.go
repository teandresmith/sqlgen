package tests

// The PostgreSQL leg of the schema-qualified cache paths
// (PRD §5.5, §27.11, §29.5): workspace_line_totals is the only cached view on
// a PostgreSQL example, so its source-table switch and refresh clear run here
// against `public.*` names, and the tenant capture-gap fallback's
// cross-tenant pattern is forced here as the sqlite `tenancy` example forces
// it with a bare name.

import (
	"context"
	"testing"
	"uuid"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	sqlgenpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_postgres/models"
)

// viewCacheEnv is newTenantEnv with the backend exposed, so a test can seed
// and probe raw keys. Views have no read-through path (PRD §29.2.5
// "Caching"), so a seeded key is the only entry a view cache can hold.
type viewCacheEnv struct {
	backend *cachememory.Backend
	cache   *models.Cache
	metrics *spyMetrics
	clientA *models.Client
	clientB *models.Client
}

func newViewCacheEnv(t *testing.T) *viewCacheEnv {
	t.Helper()
	backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	metrics := &spyMetrics{}
	c, err := models.NewCache(backend, models.WithMetricsRecorder(metrics))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	mk := func(tenant uuid.UUID) *models.Client {
		return models.New(
			sqlgenpgx.New(testPool),
			models.WithTenantResolver(staticResolver(tenant)),
			models.WithCache(c),
		)
	}
	return &viewCacheEnv{backend: backend, cache: c, metrics: metrics, clientA: mk(tenantA), clientB: mk(tenantB)}
}

// The view's cache keys live under "sqlgen:public.workspace_line_totals" —
// the schema and bare name its key grammar joins (PRD §27.5). A control key
// under another relation shows each clear is scoped to the view.
var (
	viewKey    = cache.BuildKey("sqlgen", "public", "workspace_line_totals", "0", tenantA)
	controlKey = cache.BuildKey("sqlgen", "public", "article_stats", "0", tenantA)
)

func (e *viewCacheEnv) seed(t *testing.T) {
	t.Helper()
	for _, key := range []string{viewKey, controlKey} {
		if err := e.backend.Set(context.Background(), key, []byte("{}"), 0); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
}

func (e *viewCacheEnv) present(t *testing.T, key string) bool {
	t.Helper()
	v, err := e.backend.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	return v != nil
}

// TestViewCache_SourceWriteClearsView pins the view-invalidate switch on
// PostgreSQL: it is keyed by the source table's hook.TableName value
// ("public.line_items"), and the pattern it fires must match the view's
// schema-qualified keys. A write to a table the view does not list leaves
// them alone.
func TestViewCache_SourceWriteClearsView(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		write     func(t *testing.T, c *models.Client)
		wantClear bool
	}{
		{
			name: "line_items write (invalidate_on source)",
			write: func(t *testing.T, c *models.Client) {
				if _, err := c.LineItems().Create(ctx, &models.CreateLineItemInput{
					OrderID: 1, ProductID: 1, Quantity: 1,
				}); err != nil {
					t.Fatalf("create line item: %v", err)
				}
			},
			wantClear: true,
		},
		{
			name: "articles write (not a source)",
			write: func(t *testing.T, c *models.Client) {
				if _, err := c.Articles().Create(ctx, &models.CreateArticleInput{Title: "unrelated"}); err != nil {
					t.Fatalf("create article: %v", err)
				}
			},
			wantClear: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetDB(t)
			env := newViewCacheEnv(t)
			env.seed(t)

			tt.write(t, env.clientA)

			if got := !env.present(t, viewKey); got != tt.wantClear {
				t.Errorf("view key %q cleared = %t, want %t", viewKey, got, tt.wantClear)
			}
			if !env.present(t, controlKey) {
				t.Errorf("control key %q was cleared; the view pattern must reach only the view's keys", controlKey)
			}
		})
	}
}

// TestViewCache_RefreshClearsView pins the refresh-driven clear (PRD §27.11
// "Materialized views"): Refresh resolves the view's key segments from its
// schema-qualified hook.TableName value.
func TestViewCache_RefreshClearsView(t *testing.T) {
	resetDB(t)
	env := newViewCacheEnv(t)
	env.seed(t)

	if err := env.clientA.WorkspaceLineTotal().Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if env.present(t, viewKey) {
		t.Errorf("view key %q survived Refresh", viewKey)
	}
	if !env.present(t, controlKey) {
		t.Errorf("control key %q was cleared by Refresh", controlKey)
	}
}

// TestTenantedFallback_PatternEvictsEveryTenant is the PostgreSQL leg of the
// sqlite `tenancy` example's forced-fallback test. With the tenant carriers
// suppressed, the cache degrades to the cross-tenant table pattern (PRD
// §29.5). On PostgreSQL that pattern must be built from the schema and the
// bare name ("sqlgen:public.articles:*"), not from the qualified
// hook.TableName value, or it matches no key and tenant B's entry for an
// unrelated row survives where it must be evicted.
func TestTenantedFallback_PatternEvictsEveryTenant(t *testing.T) {
	resetDB(t)
	env := newViewCacheEnv(t)
	ctx := context.Background()

	r := mkArticle(t, env.clientA, "fallback-r")
	s := mkArticle(t, env.clientB, "fallback-s")

	// The cache hook runs outside user hooks, so nil-ing the carriers after
	// the terminal returns is what an unwired mutation shape would hand it.
	suppressCapture := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			res, err := next(ctx, m)
			if err == nil && m.Table == models.TableArticles {
				m.Tenant = nil
				m.AffectedTenants = nil
			}
			return res, err
		}
	}
	suppressed := models.New(
		sqlgenpgx.New(testPool),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithCache(env.cache),
		models.WithMutationHook(suppressCapture),
	)
	if _, err := suppressed.Articles().Update(ctx, r.ID, &models.UpdateArticleInput{
		Title: omittable.Set("fallback-r-updated"),
	}); err != nil {
		t.Fatalf("Update with suppressed capture: %v (the fallback must not fail a committed write)", err)
	}

	if n := env.metrics.fallbackCount(); n != 1 {
		t.Errorf("fallback signals = %d, want 1", n)
	}
	misses := env.metrics.missCount()
	if _, err := env.clientB.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("Get s: %v", err)
	}
	if got := env.metrics.missCount() - misses; got != 1 {
		t.Errorf("Get s after the forced fallback: misses delta = %d, want 1 (the table pattern should evict every tenant's entries)", got)
	}
}
