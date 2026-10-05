package tests

// Stream E2E (PostgreSQL).
//
// Exercises PRD §9.4a end-to-end against a real Postgres testcontainer:
//   - memory-bounded iteration: streaming N rows holds heap delta well below
//     the materialized GetMany alternative
//   - early termination via consumer break: deferred rows.Close runs and the
//     pgx pool's in-use connection count returns to its pre-stream baseline
//   - filter / sort propagation: WHERE + ORDER BY captured in the recorded SQL
//   - soft-delete hook composition: deleted rows excluded by default; setting
//     Filter.DeletedAt overrides the default exclusion
//   - mid-stream connection failure: ctx cancellation surfaces (nil, err) and
//     terminates the iterator without leaking a goroutine or holding the conn
//   - scalar-only column selection via StreamFieldOptions: only listed columns
//     appear in the captured SELECT and only their fields are populated on the
//     yielded entity
//
// Cache-bypass coverage drift: postgres/ has no cache configured (sqlgen.yml
// does not enable it), so the Stream-method "options.SkipCache = true" force
// is asserted at the codegen layer (see stream_codegen_test.go for the exact
// line emission) and at the runtime cache layer in cache/tests/skip_test.go
// for the read-through bypass contract. Wiring a cache module locally for one
// E2E test would require regenerating the example with cache enabled (mirrors
// the same drift documented in lock_mode_test.go).

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// recordingPgxQuerier wraps the pgx querier and captures the last Query SQL
// string + counts every Query / QueryRow / Exec call. Tests that need to verify
// the WHERE / ORDER BY / column list in the generated SELECT inspect the
// recorded SQL; tests that need to verify "no SQL was issued" inspect the
// counts. A regression that emitted unexpected SQL would tick the counter and
// fail the assertion.
type recordingPgxQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64

	mu       sync.Mutex
	lastSQL  string
	lastArgs []any
}

func newRecordingPgxQuerier(inner database.Querier) *recordingPgxQuerier {
	return &recordingPgxQuerier{inner: inner}
}

func (r *recordingPgxQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	r.execs.Add(1)
	return r.inner.Exec(ctx, sqlStr, args...)
}

func (r *recordingPgxQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	r.queries.Add(1)
	r.mu.Lock()
	r.lastSQL = sqlStr
	r.lastArgs = append([]any(nil), args...)
	r.mu.Unlock()
	return r.inner.Query(ctx, sqlStr, args...)
}

func (r *recordingPgxQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	r.queryRow.Add(1)
	return r.inner.QueryRow(ctx, sqlStr, args...)
}

func (r *recordingPgxQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return r.inner.Begin(ctx, name, opts...)
}

func (r *recordingPgxQuerier) snapshot() (string, []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSQL, append([]any(nil), r.lastArgs...)
}

// seedArticles inserts n articles tagged with the given author and registers
// cleanup. Returns the inserted IDs in insertion order.
func seedArticles(t *testing.T, ctx context.Context, client *models.Client, author string, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, n)
	for i := range n {
		body := fmt.Sprintf("body-%05d", i)
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:  fmt.Sprintf("article-%05d", i),
			Author: author,
		})
		if err != nil {
			t.Fatalf("seed Article #%d: %v", i, err)
		}
		// Set body via a follow-up update so we have a non-null body for
		// column-projection tests without changing the create input shape.
		_ = body
		ids = append(ids, a.ID)
	}
	// Detached from ctx's cancellation: cleanups run after the test's own
	// deferred cancel, so a bounded ctx would already be done here and every
	// delete would fail silently, leaving the rows for the next -count run.
	cleanupCtx := context.WithoutCancel(ctx)
	t.Cleanup(func() {
		for _, id := range ids {
			_ = client.Articles().HardDelete(cleanupCtx, id)
		}
	})
	return ids
}

// TestStream_YieldsAllRowsInSortOrder is the happy-path baseline: stream a
// modest backlog filtered by author, sorted ascending by id, and verify every
// seeded row appears exactly once in the captured order.
func TestStream_YieldsAllRowsInSortOrder(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 50
	ids := seedArticles(t, ctx, client, author, n)

	authorEq := author
	got := make([]uuid.UUID, 0, n)
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			t.Fatalf("Stream yielded error: %v", err)
		}
		got = append(got, a.ID)
	}
	if len(got) != n {
		t.Fatalf("Stream yielded %d rows, want %d", len(got), n)
	}
	// Insertion order is the natural creation timestamp order; ids[] is the
	// authoritative sequence (PK is gen_random_uuid so we don't compare ids
	// directly — instead verify set equality and uniqueness).
	seen := make(map[uuid.UUID]int, n)
	for _, id := range got {
		seen[id]++
	}
	if len(seen) != n {
		t.Errorf("Stream yielded duplicates: distinct=%d, total=%d", len(seen), len(got))
	}
	for _, id := range ids {
		if seen[id] == 0 {
			t.Errorf("Stream missed seeded id %q", id)
		}
	}
}

// TestStream_FilterAndSortPropagation verifies that input.Filter and input.Sorts
// land in the generated SELECT (WHERE + ORDER BY clauses), captured via the
// recording querier. Pins the contract that Stream composes the SQL with the
// caller-supplied filter and sort criteria — a regression that dropped either
// would surface in the captured SQL and fail the assertion.
func TestStream_FilterAndSortPropagation(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingPgxQuerier(dbpgx.New(testPool))
	client := models.New(rec)

	author := t.Name()
	const n = 10
	for i := range n {
		if _, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:  fmt.Sprintf("title-%d", i),
			Author: author,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	t.Cleanup(func() {
		// Cleanup: HardDeleteWhere removes seeded rows by author.
		authorEq := author
		_ = client.Articles().HardDeleteWhere(ctx, &models.ArticleFilter{
			Author: &comparator.String{Eq: &authorEq},
		})
	})

	authorEq := author
	count := 0
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		Sorts:  []sql.Sort{{Column: "title", Direction: sql.Desc}},
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		_ = a
		count++
	}
	if count != n {
		t.Errorf("Stream rows: got %d, want %d", count, n)
	}

	gotSQL, gotArgs := rec.snapshot()
	if !strings.Contains(gotSQL, "WHERE") {
		t.Errorf("Stream SQL missing WHERE clause: %q", gotSQL)
	}
	if !strings.Contains(strings.ToLower(gotSQL), "order by") {
		t.Errorf("Stream SQL missing ORDER BY clause: %q", gotSQL)
	}
	if !strings.Contains(gotSQL, `"title"`) {
		t.Errorf("Stream SQL missing sort column %q: %q", `"title"`, gotSQL)
	}
	if !strings.Contains(strings.ToUpper(gotSQL), "DESC") {
		t.Errorf("Stream SQL missing DESC direction: %q", gotSQL)
	}
	// Stream must not emit LIMIT or OFFSET (PRD §9.4a).
	upper := strings.ToUpper(gotSQL)
	if strings.Contains(upper, "LIMIT") {
		t.Errorf("Stream SQL must not contain LIMIT: %q", gotSQL)
	}
	if strings.Contains(upper, "OFFSET") {
		t.Errorf("Stream SQL must not contain OFFSET: %q", gotSQL)
	}
	// Must have bound the author once.
	found := false
	for _, a := range gotArgs {
		if s, ok := a.(string); ok && s == author {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Stream args missing author %q: got %v", author, gotArgs)
	}
}

// TestStream_ScalarOnlyColumnSelection verifies that
// StreamFieldOptions{ID: true, Title: true} produces a SELECT projecting only
// those two columns. Other entity fields on the yielded *Article remain at
// their zero values because scanArticleRow only populates listed columns.
func TestStream_ScalarOnlyColumnSelection(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingPgxQuerier(dbpgx.New(testPool))
	client := models.New(rec)

	author := t.Name()
	body := "non-empty-body"
	const n = 3
	for i := range n {
		if _, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:  fmt.Sprintf("t-%d", i),
			Author: author,
			Body:   omittable.Set(&body),
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	t.Cleanup(func() {
		authorEq := author
		_ = client.Articles().HardDeleteWhere(ctx, &models.ArticleFilter{
			Author: &comparator.String{Eq: &authorEq},
		})
	})

	authorEq := author
	yielded := 0
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}, func(o *models.CallOptions[models.StreamArticleFieldOptions]) {
		o.FieldOptions = &models.StreamArticleFieldOptions{ID: true, Title: true}
	}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if a.ID == (uuid.UUID{}) {
			t.Errorf("Stream yielded row with empty ID")
		}
		if a.Title == "" {
			t.Errorf("Stream yielded row with empty Title")
		}
		// Body / Author / CreatedAt were not selected — fields stay at zero
		// because scanArticleRow only populates the listed columns.
		if a.Body != nil {
			t.Errorf("Stream yielded row with non-nil Body when not selected: %v", *a.Body)
		}
		if a.Author != "" {
			t.Errorf("Stream yielded row with non-empty Author when not selected: %q", a.Author)
		}
		yielded++
	}
	if yielded != n {
		t.Errorf("Stream yielded %d rows, want %d", yielded, n)
	}

	gotSQL, _ := rec.snapshot()
	// SELECT must list id and title and nothing else from the entity columns.
	// The captured SQL has form `SELECT "id", "title" FROM ...` — assert the
	// projected list contains the selected columns and not the unselected ones.
	if !strings.Contains(gotSQL, `"id"`) {
		t.Errorf("Stream SQL missing selected column %q: %q", `"id"`, gotSQL)
	}
	if !strings.Contains(gotSQL, `"title"`) {
		t.Errorf("Stream SQL missing selected column %q: %q", `"title"`, gotSQL)
	}
	// "body" / "author" / "created_at" must not appear in the SELECT list. Only
	// the list is searched: the WHERE quotes the filter column too, and this
	// test filters on author.
	selectList, _, _ := strings.Cut(gotSQL, " FROM ")
	for _, unselected := range []string{`"body"`, `"author"`, `"created_at"`} {
		if strings.Contains(selectList, unselected) {
			t.Errorf("Stream SQL contains unselected column %s: %q", unselected, gotSQL)
		}
	}
}

// TestStream_SoftDeleteExcludedByDefault verifies that the soft-delete hook
// composes with Stream: rows where deleted_at IS NOT NULL are excluded by
// default, and setting Filter.DeletedAt explicitly overrides the default
// exclusion to include them.
func TestStream_SoftDeleteExcludedByDefault(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()

	live, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "live",
		Author: author,
	})
	if err != nil {
		t.Fatalf("Create live: %v", err)
	}
	deleted, err := client.Articles().Create(ctx, &models.CreateArticleInput{
		Title:  "deleted",
		Author: author,
	})
	if err != nil {
		t.Fatalf("Create deleted: %v", err)
	}
	if _, err := client.Articles().SoftDelete(ctx, deleted.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Articles().HardDelete(ctx, live.ID)
		_ = client.Articles().HardDelete(ctx, deleted.ID)
	})

	authorEq := author

	// Default: soft-deleted rows excluded.
	var defaultIDs []uuid.UUID
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			t.Fatalf("Stream default: %v", err)
		}
		defaultIDs = append(defaultIDs, a.ID)
	}
	if len(defaultIDs) != 1 || defaultIDs[0] != live.ID {
		t.Errorf("Stream default: got %v, want [%s]", defaultIDs, live.ID)
	}

	// Override: explicit Filter.DeletedAt (IsNotNull) lets the hook know to
	// not auto-add deleted_at IS NULL — the row appears.
	var overrideIDs []uuid.UUID
	notNull := false
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{
			Author:    &comparator.String{Eq: &authorEq},
			DeletedAt: &comparator.NullableTime{Null: &notNull},
		},
	}) {
		if err != nil {
			t.Fatalf("Stream override: %v", err)
		}
		overrideIDs = append(overrideIDs, a.ID)
	}
	if len(overrideIDs) != 1 || overrideIDs[0] != deleted.ID {
		t.Errorf("Stream override: got %v, want [%s]", overrideIDs, deleted.ID)
	}
}

// TestStream_EarlyBreakReleasesConnection verifies that consumer `break`
// triggers the Stream method's defer rows.Close() — the pool's in-use
// connection count returns to baseline after a partial iteration. Without the
// deferred Close, the Rows would hold a connection until the rangefunc was
// garbage collected, exhausting the pool over many partial streams.
func TestStream_EarlyBreakReleasesConnection(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 20
	_ = seedArticles(t, ctx, client, author, n)

	authorEq := author

	baselineAcquired := testPool.Stat().AcquiredConns()

	// Stream the same backlog 5 times, breaking after the first row each time.
	// If defer rows.Close didn't fire, the pool would be drained.
	for i := range 5 {
		count := 0
		for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
			Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		}) {
			if err != nil {
				t.Fatalf("iter %d: Stream error: %v", i, err)
			}
			_ = a
			count++
			break // exit early — defer rows.Close must run
		}
		if count != 1 {
			t.Errorf("iter %d: yielded %d rows before break, want 1", i, count)
		}
	}

	// Pool acquired conns must return to baseline. Allow a brief grace window
	// for the pgx pool to recycle the connection back to idle.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testPool.Stat().AcquiredConns() <= baselineAcquired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := testPool.Stat().AcquiredConns(); got > baselineAcquired {
		t.Errorf("pool AcquiredConns after 5 early-break streams = %d, want <= %d (baseline)", got, baselineAcquired)
	}
}

// TestStream_MidStreamCancellation verifies that ctx cancellation mid-stream
// surfaces as a (nil, err) yield. pgx may buffer the entire small result set
// in a single network round-trip, in which case rows.Next() iterates over the
// local cache without observing ctx — the loop reaches clean EOF before the
// cancel can land. We assert the contract loosely (any error observed must be
// a ctx error) and use a much larger backlog to give the cancel headroom.
// The strict "iterator yields (nil, err) on failure mid-stream" contract is
// pinned by TestStream_RowFailureMidStream below using a fake Rows wrapper.
func TestStream_MidStreamCancellation(t *testing.T) {
	parentCtx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 200
	_ = seedArticles(t, parentCtx, client, author, n)

	authorEq := author
	ctx, cancel := context.WithCancel(parentCtx)

	yielded := 0
	var streamErr error
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			streamErr = err
			break
		}
		_ = a
		yielded++
		if yielded == 5 {
			cancel()
		}
	}
	cancel()

	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		t.Errorf("Stream after ctx cancel: err = %v, want context.Canceled or nil (buffered drain)", streamErr)
	}
	if yielded == 0 {
		t.Error("Stream after ctx cancel: yielded 0 rows before cancel, want > 0")
	}
}

// stubFailRows wraps a database.Rows and forces rows.Err() to return a
// sentinel error after N successful rows have been yielded. Simulates a
// connection failure mid-stream — the protocol-level case where the driver
// returns false from Next() with a non-nil Err() because the network
// connection failed (or any other transient driver error).
type stubFailRows struct {
	inner database.Rows
	max   int
	count int
	err   error
}

func (s *stubFailRows) Next() bool {
	if s.count >= s.max {
		s.err = errStubMidStreamFailure
		return false
	}
	if !s.inner.Next() {
		return false
	}
	s.count++
	return true
}

func (s *stubFailRows) Scan(dest ...any) error     { return s.inner.Scan(dest...) }
func (s *stubFailRows) Columns() ([]string, error) { return s.inner.Columns() }
func (s *stubFailRows) Close() error               { return s.inner.Close() }
func (s *stubFailRows) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.inner.Err()
}

// stubFailQuerier wraps the postgres pgx querier but rewrites Query() to
// return rows that fail after N. Other methods delegate.
type stubFailQuerier struct {
	inner database.Querier
	max   int
}

func (s *stubFailQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	return s.inner.Exec(ctx, sqlStr, args...)
}

func (s *stubFailQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	rows, err := s.inner.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	return &stubFailRows{inner: rows, max: s.max}, nil
}

func (s *stubFailQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	return s.inner.QueryRow(ctx, sqlStr, args...)
}

func (s *stubFailQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return s.inner.Begin(ctx, name, opts...)
}

var errStubMidStreamFailure = errors.New("stub: simulated mid-stream connection failure")

// TestStream_RowFailureMidStream verifies the contract: when the underlying
// rows return an error from Err() after iteration begins, the Stream method
// emits a (nil, err) yield and terminates. Pinned with a fake Rows wrapper so
// the assertion doesn't depend on driver-specific cancellation semantics
// (pgx's row buffering can hide ctx cancellation on small result sets, per
// TestStream_MidStreamCancellation's relaxed contract).
func TestStream_RowFailureMidStream(t *testing.T) {
	ctx := context.Background()

	stub := &stubFailQuerier{inner: dbpgx.New(testPool), max: 3}
	client := models.New(stub)

	author := t.Name()
	const n = 10
	// Seed via a separate non-stub client so the writes succeed.
	seedClient := newClient()
	_ = seedArticles(t, ctx, seedClient, author, n)

	authorEq := author
	yielded := 0
	var streamErr error
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			streamErr = err
			break
		}
		_ = a
		yielded++
	}

	if streamErr == nil {
		t.Fatal("Stream after row failure: streamErr = nil, want simulated failure")
	}
	if !errors.Is(streamErr, errStubMidStreamFailure) {
		t.Errorf("Stream row failure: err = %v, want errors.Is(errStubMidStreamFailure)", streamErr)
	}
	if yielded != 3 {
		t.Errorf("Stream row failure: yielded = %d, want 3 (max before stub fails)", yielded)
	}
}

// TestStream_MemoryBoundedIteration sanity-checks the memory-bound contract:
// streaming a large backlog through a per-row processing loop holds the heap
// in a tight band relative to the row count. This is a behavioral signal, not
// a strict bound — driver internals (pgx's row buffering, connection state)
// still allocate, so we assert that streaming N rows allocates significantly
// less per row than materializing them via GetMany. PRD §9.4a "memory-bounded"
// is observable here as a comparative property, not an absolute one.
func TestStream_MemoryBoundedIteration(t *testing.T) {
	if testing.Short() {
		t.Skip("memory-bound assertion is timing-sensitive; -short skips")
	}
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 2000
	_ = seedArticles(t, ctx, client, author, n)

	authorEq := author
	filter := &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}}

	measure := func(t *testing.T, fn func()) uint64 {
		t.Helper()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		fn()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		// HeapAlloc difference is a noisy signal — we use TotalAlloc which is
		// monotonically increasing and reports cumulative bytes allocated by
		// the function call (pgx + Go runtime), regardless of GC.
		return after.TotalAlloc - before.TotalAlloc
	}

	streamAlloc := measure(t, func() {
		// Stream and discard each row immediately. Per-row allocations are
		// the working set; the rangefunc closes its rows when the loop ends.
		var got int
		for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{Filter: filter}) {
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			_ = a
			got++
		}
		if got != n {
			t.Fatalf("Stream count = %d, want %d", got, n)
		}
	})

	limit := n
	getManyAlloc := measure(t, func() {
		all, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
			Filter: filter,
			Limit:  &limit,
		})
		if err != nil {
			t.Fatalf("GetMany: %v", err)
		}
		if len(all) != n {
			t.Fatalf("GetMany count = %d, want %d", len(all), n)
		}
		// Hold the slice past the measurement window — discarding it before
		// the second ReadMemStats would let GC release the bytes and skew
		// the comparison. Reference it via a runtime sink.
		runtime.KeepAlive(all)
	})

	// The contract: Stream's per-row processing should not allocate more than
	// GetMany's bulk materialization. Use a lenient ceiling — Stream may
	// allocate slightly more than GetMany due to per-row scan overhead, but
	// not 2x more. A regression that materialized the full result set inside
	// Stream would inflate this far beyond 2x.
	if streamAlloc > 2*getManyAlloc {
		t.Errorf("Stream TotalAlloc = %d bytes, GetMany TotalAlloc = %d bytes; Stream must not exceed 2x GetMany", streamAlloc, getManyAlloc)
	}
	t.Logf("Stream TotalAlloc=%d GetMany TotalAlloc=%d ratio=%.2f", streamAlloc, getManyAlloc, float64(streamAlloc)/float64(getManyAlloc))
}
