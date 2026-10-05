package tests

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy/models"
)

// Event-driven invalidation for tenanted tables (PRD §28.11/§27.9).
// Previously the FromEventSubscriber receive path dropped
// Event.Metadata["tenant"] and routed tenanted tables into InvalidateMany's tenanted-table refusal — a
// deterministic hard error on every event, redelivered forever, so
// cross-instance invalidation never happened for any tenanted table. This
// suite was landed FAILING against that defect first, then made green by the
// InvalidationSignal rewrite.

// remoteInstance builds the "other process": a cache fed only by the bus via
// FromEventSubscriber, plus per-tenant clients reading through it.
type remoteInstance struct {
	cacheA  *models.Client // resolver A, shares the remote cache
	cacheB  *models.Client // resolver B, shares the remote cache
	metrics *spyMetrics
}

func newRemoteInstance(t *testing.T, bus *memorybus.Bus) *remoteInstance {
	t.Helper()
	backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	metrics := newSpyMetrics()
	remoteCache, err := models.NewCache(
		backend,
		models.WithMetricsRecorder(metrics),
		models.WithInvalidationSource(cache.FromEventSubscriber(bus)),
	)
	if err != nil {
		t.Fatalf("NewCache (remote): %v", err)
	}
	t.Cleanup(func() { _ = remoteCache.Close() })

	mk := func(tenant uuid.UUID) *models.Client {
		return models.New(
			dbstdlib.New(testDB),
			models.WithTenantResolver(staticResolver(tenant)),
			models.WithCache(remoteCache),
		)
	}
	return &remoteInstance{cacheA: mk(tenantA), cacheB: mk(tenantB), metrics: metrics}
}

// waitForTitle polls a cached Get until the title matches (the event-driven
// eviction is asynchronous relative to the publisher's return on some
// transports) or the deadline passes.
func waitForTitle(t *testing.T, client *models.Client, id int64, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := client.Articles().Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get %d: %v", id, err)
		}
		if got.Title == want {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("cached title for %d = %q, want %q — event-driven invalidation never evicted the remote entry (§28.11 defect)", id, got.Title, want)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A publish→subscribe round-trip on a tenanted table evicts the
// receiving cache's precise per-PK tenant-scoped key (rebuilt from the event's
// tenant stamp — byte-identical to the key the receiver itself wrote), returns
// no error, and leaves other tenants' entries untouched. The mutation runs
// under SkipTenancy so the tenant can only come from the row-derived stamp.
func TestEventDrivenInvalidation_TenantedPreciseReceive(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	// "Local" instance publishes mutation events; it carries no cache so
	// every effect observed on the remote side came through the bus.
	local := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithEventPublisher(bus),
	)
	remote := newRemoteInstance(t, bus)

	r, err := local.Articles().Create(ctx, &models.CreateArticleInput{Title: "recv-r"})
	if err != nil {
		t.Fatalf("create R: %v", err)
	}
	s, err := local.Articles().Create(ctx, &models.CreateArticleInput{
		Title:       "recv-s",
		WorkspaceID: omittable.Set(tenantB),
	}, func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true })
	if err != nil {
		t.Fatalf("create S: %v", err)
	}

	// Warm the REMOTE cache under each tenant's key.
	if _, err := remote.cacheA.Articles().Get(ctx, r.ID); err != nil {
		t.Fatalf("warm remote R: %v", err)
	}
	if _, err := remote.cacheB.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm remote S: %v", err)
	}

	// (a) SkipTenancy update on the local instance → tenant-stamped event →
	// the remote cache must evict its own …:tenant:A:…:pk:r key.
	if _, err := local.Articles().Update(
		ctx, r.ID,
		&models.UpdateArticleInput{Title: omittable.Set("recv-r2")},
		func(o *models.CallOptions[models.ArticleFieldOptions]) { o.SkipTenancy = true },
	); err != nil {
		t.Fatalf("local SkipTenancy update: %v", err)
	}
	waitForTitle(t, remote.cacheA, r.ID, "recv-r2")

	// (c) tenant B's entry survived the precise eviction.
	assertCacheHit(t, remote.metrics, "remote Get S after precise event-driven eviction", func() {
		if _, err := remote.cacheB.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("remote Get S: %v", err)
		}
	})

	// The precise path must not have taken the defensive table pattern.
	assertNoFallbackSignal(t, remote.metrics)
}

// A tenant-less event on a tenanted table (an out-of-band publisher, or one
// that predates per-row tenant stamping) degrades to the full-table pattern on the receive side:
// safe over-eviction, no error back to the transport.
func TestEventDrivenInvalidation_TenantAbsentFallsBackToTablePattern(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })

	local := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantB)),
		models.WithEventPublisher(bus),
	)
	remote := newRemoteInstance(t, bus)

	s, err := local.Articles().Create(ctx, &models.CreateArticleInput{Title: "absent-s"})
	if err != nil {
		t.Fatalf("create S: %v", err)
	}
	if _, err := remote.cacheB.Articles().Get(ctx, s.ID); err != nil {
		t.Fatalf("warm remote S: %v", err)
	}

	// Hand-crafted tenant-less event (no Metadata at all) for the tenanted
	// table — the receiver cannot build a precise key and must clear the
	// table cross-tenant instead of erroring or going stale.
	if err := bus.Publish(ctx, event.Event{
		ID:     "absent-manual",
		Table:  "articles",
		Action: event.Update,
		PK:     s.ID,
	}); err != nil {
		t.Fatalf("publish tenant-less event: %v", err)
	}

	// S's entry is gone (over-evicted by the table pattern).
	deadline := time.Now().Add(2 * time.Second)
	for {
		missesBefore := remote.metrics.missCount()
		if _, err := remote.cacheB.Articles().Get(ctx, s.ID); err != nil {
			t.Fatalf("remote Get S: %v", err)
		}
		if remote.metrics.missCount() > missesBefore {
			break // the entry had been evicted — this Get missed
		}
		if time.Now().After(deadline) {
			t.Errorf("remote entry for S still cached after a tenant-less event — full-table fallback never fired")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// capturingSource is an InvalidationSource that hands the generated
// handleInvalidation back to the test, so the handler's return value can be
// asserted directly (memorybus swallows handler errors, making the no-error
// half of this suite only indirectly observable through eviction otherwise).
type capturingSource struct {
	handler cache.InvalidationHandler
}

func (s *capturingSource) Subscribe(h cache.InvalidationHandler) (cache.InvalidationSubscription, error) {
	s.handler = h
	return noopSubscription{}, nil
}
func (s *capturingSource) Close() error { return nil }

type noopSubscription struct{}

func (noopSubscription) Unsubscribe() error { return nil }

// The error contract, asserted directly: a tenanted signal with a tenant
// stamp returns nil (precise path), and a tenant-less tenanted signal ALSO
// returns nil (fallback path — the transport must ACK, not redeliver
// forever, which was the §28.11 failure loop).
func TestEventDrivenInvalidation_HandlerReturnsNoError(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	backend, err := cachememory.New(cachememory.Options{MaxSize: 100})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	src := &capturingSource{}
	metrics := newSpyMetrics()
	c, err := models.NewCache(
		backend,
		models.WithMetricsRecorder(metrics),
		models.WithInvalidationSource(src),
	)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if src.handler == nil {
		t.Fatalf("InvalidationSource.Subscribe never received the handler")
	}

	// Precise path: tenant-stamped signal on a tenanted table.
	if err := src.handler(ctx, cache.InvalidationSignal{
		Table:  models.TableArticles,
		Tenant: tenantA.String(),
		PKs:    []any{int64(1)},
	}); err != nil {
		t.Errorf("handler(tenant-stamped signal) = %v, want nil", err)
	}

	// Fallback path: tenant-less signal on the same table — before the fix
	// this returned the deterministic "table is tenanted" hard error and
	// redelivered forever.
	if err := src.handler(ctx, cache.InvalidationSignal{
		Table: models.TableArticles,
		PKs:   []any{int64(1)},
	}); err != nil {
		t.Errorf("handler(tenant-less signal) = %v, want nil (fallback must ACK, not redeliver)", err)
	}
	if n := metrics.errorOpCount("invalidate_tenant_fallback"); n != 1 {
		t.Errorf("fallback signals = %d, want 1 (only the tenant-less signal degrades)", n)
	}

	// Tenant-in-PK table: precise from the PK struct even without a stamp.
	if err := src.handler(ctx, cache.InvalidationSignal{
		Table: models.TableOrderItems,
		PKs:   []any{models.OrderItemPK{WorkspaceID: tenantA, OrderID: 1, ProductID: 1}},
	}); err != nil {
		t.Errorf("handler(tenant-in-PK signal without stamp) = %v, want nil (PK carries the tenant)", err)
	}
	if n := metrics.errorOpCount("invalidate_tenant_fallback"); n != 1 {
		t.Errorf("fallback signals after ∈-PK signal = %d, want still 1 (PK-derived path is precise)", n)
	}
}
