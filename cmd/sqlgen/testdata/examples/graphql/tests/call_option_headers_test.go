package tests

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cache"
	cachememory "github.com/teandresmith/sqlgen/cache/memory"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/event"
	"github.com/teandresmith/sqlgen/event/memorybus"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
)

// These tests drive the per-call option headers through a real HTTP request
// to the generated GraphQL server wrapped in WithCallOptionsMiddleware, and
// observe the effect on the client the resolvers wrap: the cache backend it
// writes to, the event bus it publishes on, and the hooks registered on it.
// Every case runs against a control request without the header that does
// produce the effect, so an absent effect is attributable to the header.

// thdBackend wraps a cache.Backend and counts every call that reaches it, so
// a test can tell "the cache was never touched" from "the cache was probed
// and missed" and from "the cache was written".
type thdBackend struct {
	inner cache.Backend

	mu          sync.Mutex
	gets        int
	sets        int
	invalidates int
}

func (b *thdBackend) Get(ctx context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	b.gets++
	b.mu.Unlock()
	return b.inner.Get(ctx, key)
}

func (b *thdBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	b.mu.Lock()
	b.sets++
	b.mu.Unlock()
	return b.inner.Set(ctx, key, value, ttl)
}

func (b *thdBackend) Invalidate(ctx context.Context, key string) error {
	b.mu.Lock()
	b.invalidates++
	b.mu.Unlock()
	return b.inner.Invalidate(ctx, key)
}

func (b *thdBackend) InvalidateMany(ctx context.Context, keys []string) error {
	b.mu.Lock()
	b.invalidates++
	b.mu.Unlock()
	return b.inner.InvalidateMany(ctx, keys)
}

func (b *thdBackend) InvalidatePattern(ctx context.Context, pattern string) error {
	b.mu.Lock()
	b.invalidates++
	b.mu.Unlock()
	return b.inner.InvalidatePattern(ctx, pattern)
}

// thdCounts is a snapshot of the calls a thdBackend has seen.
type thdCounts struct {
	gets, sets, invalidates int
}

func (b *thdBackend) counts() thdCounts {
	b.mu.Lock()
	defer b.mu.Unlock()
	return thdCounts{gets: b.gets, sets: b.sets, invalidates: b.invalidates}
}

// waitForSets polls until the backend has seen at least want Set calls.
// Hydration writes the full entity on a background goroutine, so the Set a
// control read produces lands after the response does.
func (b *thdBackend) waitForSets(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.counts().sets >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited 2s for %d cache Set calls, observed %d", want, b.counts().sets)
}

// thdCacheOption builds an in-memory cache wrapped in a counting backend and
// returns the client option that wires it in.
func thdCacheOption(t *testing.T) (models.ClientOption, *thdBackend) {
	t.Helper()
	inner, err := cachememory.New(cachememory.Options{MaxSize: 1_000})
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })
	backend := &thdBackend{inner: inner}
	c, err := models.NewCache(backend)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return models.WithCache(c), backend
}

// thdEventOption builds a memory bus subscribed to table and returns the
// client option that publishes to it, plus a snapshot of what was published.
func thdEventOption(t *testing.T, table string) (models.ClientOption, func() []event.Event) {
	t.Helper()
	bus := memorybus.New()
	t.Cleanup(func() { _ = bus.Close() })
	var (
		mu     sync.Mutex
		events []event.Event
	)
	sub, err := bus.Subscribe(event.SubscribeOptions{Tables: []string{table}}, func(_ context.Context, e event.Event) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return models.WithEventPublisher(bus), func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(events)
	}
}

// thdHookCall is one invocation a recording hook observed.
type thdHookCall struct {
	kind  string // "mutation" or "query"
	table hook.TableName
	op    string
}

// thdHookOptions returns a recording mutation hook and query hook as client
// options, plus a snapshot of every invocation they saw.
func thdHookOptions() ([]models.ClientOption, func() []thdHookCall) {
	var (
		mu    sync.Mutex
		calls []thdHookCall
	)
	record := func(c thdHookCall) {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
	}
	mh := func(next hook.MutationHandler) hook.MutationHandler {
		return func(ctx context.Context, m *hook.MutationContext) (any, error) {
			record(thdHookCall{kind: "mutation", table: m.Table, op: string(m.Op)})
			return next(ctx, m)
		}
	}
	qh := func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			record(thdHookCall{kind: "query", table: q.Table, op: string(q.Op)})
			return next(ctx, q)
		}
	}
	return []models.ClientOption{models.WithMutationHook(mh), models.WithQueryHook(qh)}, func() []thdHookCall {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

// thdServe stands up the generated GraphQL stack, wrapped in the generated
// call-options middleware, over a fresh client built with opts.
func thdServe(t *testing.T, opts ...models.ClientOption) string {
	t.Helper()
	return tscServe(t, models.New(dbpgx.New(testPool), opts...))
}

const (
	thdUpdateCategory = `mutation ($id: Int!, $name: String!) { updateCategory(id: $id, input: { name: $name }) { id name } }`
	thdGetCategory    = `query ($id: Int!) { category(id: $id) { id name } }`
)

// thdMustName runs a category query or mutation through url with headers,
// fails on any GraphQL error, and returns the name the response carried
// under field.
func thdMustName(t *testing.T, url, query, field string, vars map[string]any, headers map[string]string) string {
	t.Helper()
	resp := postGQLAt(t, url, query, vars, headers)
	if len(resp.Errors) != 0 {
		t.Fatalf("%s errors: %+v", field, resp.Errors)
	}
	var out map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("decoding %s (raw=%s): %v", field, resp.Data, err)
	}
	return out[field].Name
}

// thdCategoryName reads a category's name straight from the database.
func thdCategoryName(t *testing.T, id int64) string {
	t.Helper()
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM categories WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("reading category %d: %v", id, err)
	}
	return name
}

// TestCallOptionHeaders_NoCacheBypassesWriteThrough pins that
// `Cache-Control: no-cache` bypasses write-through as well as read-through:
// a request carrying it writes nothing to the cache — no hydration Set after
// a read, no invalidation after a mutation — while the same request without
// the header does both.
func TestCallOptionHeaders_NoCacheBypassesWriteThrough(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		field   string
		headers map[string]string
		// wantWrites: the control path. A read misses, then hydration Sets the
		// full entity; a mutation invalidates the row and its read-back hydrates.
		wantWrites      bool
		wantInvalidates bool
	}{
		{
			name:       "a read without the header hydrates the cache",
			query:      thdGetCategory,
			field:      "category",
			wantWrites: true,
		},
		{
			name:    "a read with Cache-Control no-cache writes nothing to the cache",
			query:   thdGetCategory,
			field:   "category",
			headers: map[string]string{"Cache-Control": "no-cache"},
		},
		{
			name:            "a mutation without the header invalidates and rehydrates the cache",
			query:           thdUpdateCategory,
			field:           "updateCategory",
			wantWrites:      true,
			wantInvalidates: true,
		},
		{
			name:    "a mutation with Cache-Control no-cache writes nothing to the cache",
			query:   thdUpdateCategory,
			field:   "updateCategory",
			headers: map[string]string{"Cache-Control": "no-cache"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			id := tscCreateCategoryID(t, testServer.URL, "thd-cache-seed")
			cacheOpt, backend := thdCacheOption(t)
			url := thdServe(t, cacheOpt)

			got := thdMustName(t, url, tt.query, tt.field, map[string]any{"id": id, "name": "thd-cache-renamed"}, tt.headers)
			want := "thd-cache-seed"
			if tt.query == thdUpdateCategory {
				want = "thd-cache-renamed"
				if db := thdCategoryName(t, id); db != want {
					t.Fatalf("database name after updateCategory = %q, want %q", db, want)
				}
			}
			if got != want {
				t.Fatalf("%s returned name %q, want %q", tt.field, got, want)
			}

			if !tt.wantWrites {
				// Hydration is only ever scheduled from a read-through miss, which
				// probes the backend first: zero calls of any kind proves the
				// request neither wrote nor scheduled a write.
				if c := backend.counts(); c != (thdCounts{}) {
					t.Errorf("cache backend calls = %+v, want none", c)
				}
				return
			}
			backend.waitForSets(t, 1)
			c := backend.counts()
			if c.gets == 0 {
				t.Errorf("cache backend gets = 0, want the read-through probe")
			}
			if tt.wantInvalidates && c.invalidates == 0 {
				t.Errorf("cache backend invalidations = 0, want the mutation's invalidation")
			}
		})
	}
}

// TestCallOptionHeaders_SkipEventsSuppressesPublishing pins that
// `X-Skip-Events: true` suppresses event publishing for the mutation while the
// write itself still happens; the same mutation without it publishes.
func TestCallOptionHeaders_SkipEventsSuppressesPublishing(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantEvents int
	}{
		{
			name:       "a mutation without the header publishes its event",
			wantEvents: 1,
		},
		{
			name:       "a mutation with X-Skip-Events true publishes nothing",
			headers:    map[string]string{"X-Skip-Events": "true"},
			wantEvents: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			eventOpt, published := thdEventOption(t, "categories")
			url := thdServe(t, eventOpt)

			const name = "thd-skip-events"
			resp := postGQLAt(t, url, tscCreateCategory, map[string]any{"name": name}, tt.headers)
			if len(resp.Errors) != 0 {
				t.Fatalf("createCategory errors: %+v", resp.Errors)
			}
			if n := tscCategoryCount(t, name); n != 1 {
				t.Fatalf("categories named %q = %d, want 1 (the write must still happen)", name, n)
			}
			if got := published(); len(got) != tt.wantEvents {
				t.Errorf("published events = %d, want %d: %+v", len(got), tt.wantEvents, got)
			}
		})
	}
}

// TestCallOptionHeaders_SkipHooksBypassesHooks pins that `X-Skip-Hooks: true`
// bypasses the hooks registered on the client: neither the mutation hook
// (the update) nor the query hook (the read-back) runs, and the write still
// lands. Without the header both run.
func TestCallOptionHeaders_SkipHooksBypassesHooks(t *testing.T) {
	tests := []struct {
		name      string
		headers   map[string]string
		wantCalls []thdHookCall
	}{
		{
			name: "a mutation without the header runs the mutation and query hooks",
			wantCalls: []thdHookCall{
				{kind: "mutation", table: models.TableCategories, op: string(hook.OpUpdate)},
				{kind: "query", table: models.TableCategories, op: string(hook.OpGet)},
			},
		},
		{
			name:    "a mutation with X-Skip-Hooks true runs no hook",
			headers: map[string]string{"X-Skip-Hooks": "true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			id := tscCreateCategoryID(t, testServer.URL, "thd-skip-hooks-seed")
			hookOpts, calls := thdHookOptions()
			url := thdServe(t, hookOpts...)

			const renamed = "thd-skip-hooks-renamed"
			if got := thdMustName(t, url, thdUpdateCategory, "updateCategory", map[string]any{"id": id, "name": renamed}, tt.headers); got != renamed {
				t.Fatalf("updateCategory returned name %q, want %q", got, renamed)
			}
			if db := thdCategoryName(t, id); db != renamed {
				t.Fatalf("database name after updateCategory = %q, want %q", db, renamed)
			}
			if got := calls(); !slices.Equal(got, tt.wantCalls) {
				t.Errorf("hook calls = %+v, want %+v", got, tt.wantCalls)
			}
		})
	}
}

// TestCallOptionHeaders_SkipHooksImpliesSkipCacheAndEvents pins that
// `X-Skip-Hooks: true` on its own, with neither `Cache-Control: no-cache` nor
// `X-Skip-Events: true`, also skips the cache and suppresses events: the
// mutation and its read-back leave the cache backend untouched and publish
// nothing. Without the header the same mutation does both.
func TestCallOptionHeaders_SkipHooksImpliesSkipCacheAndEvents(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantCache  bool
		wantEvents int
	}{
		{
			name:       "a mutation without the header touches the cache and publishes",
			wantCache:  true,
			wantEvents: 1,
		},
		{
			name:       "a mutation with only X-Skip-Hooks true touches no cache and publishes nothing",
			headers:    map[string]string{"X-Skip-Hooks": "true"},
			wantCache:  false,
			wantEvents: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			id := tscCreateCategoryID(t, testServer.URL, "thd-implies-seed")
			cacheOpt, backend := thdCacheOption(t)
			eventOpt, published := thdEventOption(t, "categories")
			url := thdServe(t, cacheOpt, eventOpt)

			const renamed = "thd-implies-renamed"
			if got := thdMustName(t, url, thdUpdateCategory, "updateCategory", map[string]any{"id": id, "name": renamed}, tt.headers); got != renamed {
				t.Fatalf("updateCategory returned name %q, want %q", got, renamed)
			}

			if got := published(); len(got) != tt.wantEvents {
				t.Errorf("published events = %d, want %d: %+v", len(got), tt.wantEvents, got)
			}
			if !tt.wantCache {
				if c := backend.counts(); c != (thdCounts{}) {
					t.Errorf("cache backend calls = %+v, want none", c)
				}
				return
			}
			backend.waitForSets(t, 1)
			if c := backend.counts(); c.invalidates == 0 || c.gets == 0 {
				t.Errorf("cache backend calls = %+v, want an invalidation and a read-through probe", c)
			}
		})
	}
}

// TestCallOptionHeaders_SkipHooksCoversRelationshipLoads pins that the header
// overrides cover the relationship loads the FieldOptions tree triggers, not
// only the primary client call: a `user` query selecting its `events`
// relationship with `X-Skip-Hooks: true` runs no query hook, for the users
// read or for the events load. The control request shows both reads are
// hook-observable without the header.
func TestCallOptionHeaders_SkipHooksCoversRelationshipLoads(t *testing.T) {
	tests := []struct {
		name      string
		headers   map[string]string
		wantCalls []thdHookCall
	}{
		{
			name: "a relationship query without the header runs the hook for both reads",
			wantCalls: []thdHookCall{
				{kind: "query", table: models.TableUsers, op: string(hook.OpGet)},
				{kind: "query", table: models.TableEvents, op: string(hook.OpGetMany)},
			},
		},
		{
			name:    "a relationship query with X-Skip-Hooks true runs no hook for either read",
			headers: map[string]string{"X-Skip-Hooks": "true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			u := seedUser(t, "thd-relationship@example.com", "ThdRelationship")
			hookOpts, calls := thdHookOptions()
			url := thdServe(t, hookOpts...)

			resp := postGQLAt(t, url, `query ($id: UUID!) { user(id: $id) { id events { id } } }`, map[string]any{"id": u.ID}, tt.headers)
			if len(resp.Errors) != 0 {
				t.Fatalf("user query errors: %+v", resp.Errors)
			}
			var out struct {
				User *struct {
					ID string `json:"id"`
				} `json:"user"`
			}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				t.Fatalf("decoding user (raw=%s): %v", resp.Data, err)
			}
			if out.User == nil || out.User.ID != u.ID {
				t.Fatalf("user query returned %s, want user %s", resp.Data, u.ID)
			}
			if got := calls(); !slices.Equal(got, tt.wantCalls) {
				t.Errorf("hook calls = %+v, want %+v", got, tt.wantCalls)
			}
		})
	}
}
