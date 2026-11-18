package tests

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/omittable"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/tenancy_mysql/models"
)

// articlesBatchSize is the sub-batch size of the generated articles client:
// sqlgen.yml sets no batch_size, so the generator's default applies.
const articlesBatchSize = 200

// A SkipTenancy UpdateMany whose first item moves an article from tenant A to
// tenant B, and which repeats that article's key past the first sub-batch.
// The second sub-batch's pre-read finds the row already under tenant B, but
// the tenant the operation affected for that key is the one the row had
// before the call: every event for the key carries tenant A, and the tenant
// A cache entry warmed before the call is evicted.
func TestMySQLCapture_UpdateManyRepeatedKeyKeepsPreMutationTenant(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	backend, err := cachememory.New(cachememory.Options{MaxSize: 10_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	metrics := &spyMetrics{}
	cache, err := models.NewCache(backend, models.WithMetricsRecorder(metrics))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	client := models.New(
		dbstdlib.New(testDB),
		models.WithTenantResolver(staticResolver(tenantA)),
		models.WithCache(cache),
		models.WithEventPublisher(bus),
	)

	moved := mkArticle(t, client, "moved", "moved-original")

	var (
		evMu   sync.Mutex
		events []event.Event
	)
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{"articles"}}, func(_ context.Context, e event.Event) error {
		evMu.Lock()
		events = append(events, e)
		evMu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// One item more than a sub-batch holds puts the last repeat of the key in
	// a second sub-batch, which pre-reads the row after the first item moved
	// it.
	items := make([]models.UpdateArticleItem, 0, articlesBatchSize+1)
	items = append(items, models.UpdateArticleItem{ID: moved.ID, Input: &models.UpdateArticleInput{
		WorkspaceID: omittable.Set(tenantB),
		Title:       omittable.Set("moved-0"),
	}})
	for i := 1; i <= articlesBatchSize; i++ {
		items = append(items, models.UpdateArticleItem{ID: moved.ID, Input: &models.UpdateArticleInput{
			Title: omittable.Set(fmt.Sprintf("moved-%d", i)),
		}})
	}
	if _, err := client.Articles().UpdateMany(ctx, items, func(o *models.CallOptions[models.ArticleFieldOptions]) {
		o.SkipTenancy = true
	}); err != nil {
		t.Fatalf("SkipTenancy UpdateMany: %v", err)
	}

	evMu.Lock()
	got := append([]event.Event(nil), events...)
	evMu.Unlock()
	if len(got) != len(items) {
		t.Fatalf("events = %d, want %d (one per written item)", len(got), len(items))
	}
	var wrong []int
	for i, e := range got {
		if e.PK != any(moved.ID) {
			t.Fatalf("event %d PK = %v, want %d", i, e.PK, moved.ID)
		}
		if e.Metadata == nil || e.Metadata["tenant"] != tenantA {
			wrong = append(wrong, i)
		}
	}
	if len(wrong) > 0 {
		t.Errorf("%d of %d events carry a tenant other than %q, the tenant the row had before the call; first is event %d with Metadata %v",
			len(wrong), len(got), tenantA, wrong[0], got[wrong[0]].Metadata)
	}

	// The row now belongs to tenant B, so a tenant A read finds nothing; a
	// surviving tenant A cache entry would serve the pre-call row instead.
	if a, err := client.Articles().Get(ctx, moved.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("tenant A Get after the move = (%+v, %v), want ErrNotFound; the pre-call tenant's cache entry was not evicted", a, err)
	}
	assertNoFallback(t, metrics)
}
