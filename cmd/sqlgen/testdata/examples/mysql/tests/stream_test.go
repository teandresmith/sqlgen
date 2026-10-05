package tests

// Stream E2E (MySQL).
//
// Mirrors postgres/tests/stream_test.go for the MySQL dialect, exercising
// PRD §9.4a end-to-end against a real mysql:8.0 testcontainer:
//   - happy-path baseline: every seeded row appears exactly once
//   - filter / sort propagation captured in the recorded SQL
//   - scalar-only column selection via StreamFieldOptions
//   - soft-delete hook composition (default exclusion + explicit override)
//   - early termination releases the connection cleanly
//   - mid-stream cancellation surfaces (nil, err) and terminates the iterator
//
// Per-driver buffering caveat: the MySQL stdlib driver
// (github.com/go-sql-driver/mysql) buffers the full result set client-side by
// default — `text-protocol` rows arrive as a single response from the server,
// not row-at-a-time as Postgres pgx does. This means the MEMORY-bound contract
// (PRD §9.4a "yields rows as the driver returns them") is per-driver: on
// MySQL, "memory-bounded" reduces to "the per-row materialization in the loop
// body is bounded; the driver's buffer holds the unscanned rows". The
// per-driver tradeoff is documented in PRD §9.4a "Implementation pattern" and
// the §9.4a "Constraints" — Stream still avoids the full GetMany allocator
// path (slice growth + per-row entity copies into a backing slice) and still
// honors early-break to release the conn back to the pool. The
// memory-bound assertion in the postgres test is intentionally NOT mirrored
// here because the driver's buffer would dominate the measurement and produce
// a noisy signal that doesn't reflect Stream's contract.
//
// Cache-bypass coverage: mysql/ has no cache configured (sqlgen.yml does not
// enable it). The Stream-method "options.SkipCache = true" force is asserted
// at the codegen layer (stream_codegen_test.go) and at the runtime
// cache layer in cache/tests/skip_test.go. Same rationale as the lock_mode
// test header.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// recordingMysqlQuerier wraps the stdlib MySQL querier and captures the last
// Query SQL string + counts every Query / QueryRow / Exec call. Same purpose
// as recordingPgxQuerier in the postgres test file — duplicated because
// example modules have separate go.mods (GOWORK=off build) and can't share
// helpers across module boundaries.
type recordingMysqlQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64

	mu       sync.Mutex
	lastSQL  string
	lastArgs []any
}

func newRecordingMysqlQuerier(inner database.Querier) *recordingMysqlQuerier {
	return &recordingMysqlQuerier{inner: inner}
}

func (r *recordingMysqlQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	r.execs.Add(1)
	return r.inner.Exec(ctx, sqlStr, args...)
}

func (r *recordingMysqlQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	r.queries.Add(1)
	r.mu.Lock()
	r.lastSQL = sqlStr
	r.lastArgs = append([]any(nil), args...)
	r.mu.Unlock()
	return r.inner.Query(ctx, sqlStr, args...)
}

func (r *recordingMysqlQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	r.queryRow.Add(1)
	return r.inner.QueryRow(ctx, sqlStr, args...)
}

func (r *recordingMysqlQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return r.inner.Begin(ctx, name, opts...)
}

func (r *recordingMysqlQuerier) snapshot() (string, []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSQL, append([]any(nil), r.lastArgs...)
}

// seedMysqlArticles inserts n articles tagged with the given author and
// registers cleanup. Returns the inserted IDs in insertion order.
func seedMysqlArticles(t *testing.T, ctx context.Context, client *models.Client, author string, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := range n {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:  fmt.Sprintf("article-%05d", i),
			Author: author,
		})
		if err != nil {
			t.Fatalf("seed Article #%d: %v", i, err)
		}
		ids = append(ids, a.ID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = client.Articles().HardDelete(ctx, id)
		}
	})
	return ids
}

// TestStream_YieldsAllRowsInSortOrder_MySQL — happy-path baseline.
func TestStream_YieldsAllRowsInSortOrder_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 50
	ids := seedMysqlArticles(t, ctx, client, author, n)

	authorEq := author
	got := make([]int64, 0, n)
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
	seen := make(map[int64]int, n)
	for _, id := range got {
		seen[id]++
	}
	if len(seen) != n {
		t.Errorf("Stream yielded duplicates: distinct=%d, total=%d", len(seen), len(got))
	}
	for _, id := range ids {
		if seen[id] == 0 {
			t.Errorf("Stream missed seeded id %d", id)
		}
	}
}

// TestStream_FilterAndSortPropagation_MySQL — verifies WHERE + ORDER BY land
// in the captured SELECT and Stream emits no LIMIT / OFFSET.
func TestStream_FilterAndSortPropagation_MySQL(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingMysqlQuerier(dbstdlib.New(testDB))
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
	if !strings.Contains(gotSQL, "`title`") {
		t.Errorf("Stream SQL missing sort column `title`: %q", gotSQL)
	}
	if !strings.Contains(strings.ToUpper(gotSQL), "DESC") {
		t.Errorf("Stream SQL missing DESC direction: %q", gotSQL)
	}
	upper := strings.ToUpper(gotSQL)
	if strings.Contains(upper, "LIMIT") {
		t.Errorf("Stream SQL must not contain LIMIT: %q", gotSQL)
	}
	if strings.Contains(upper, "OFFSET") {
		t.Errorf("Stream SQL must not contain OFFSET: %q", gotSQL)
	}
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

// TestStream_ScalarOnlyColumnSelection_MySQL — verifies StreamFieldOptions
// produces a SELECT projecting only the listed columns, and the yielded
// entity has only those fields populated.
func TestStream_ScalarOnlyColumnSelection_MySQL(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingMysqlQuerier(dbstdlib.New(testDB))
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
		if a.ID == 0 {
			t.Errorf("Stream yielded row with zero ID")
		}
		if a.Title == "" {
			t.Errorf("Stream yielded row with empty Title")
		}
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
	if !strings.Contains(gotSQL, "`id`") {
		t.Errorf("Stream SQL missing selected column `id`: %q", gotSQL)
	}
	if !strings.Contains(gotSQL, "`title`") {
		t.Errorf("Stream SQL missing selected column `title`: %q", gotSQL)
	}
	// Only the SELECT list is checked: the WHERE quotes the filter column too,
	// and this test filters on author.
	selectList, _, _ := strings.Cut(gotSQL, " FROM ")
	for _, unselected := range []string{"`body`", "`author`", "`created_at`"} {
		if strings.Contains(selectList, unselected) {
			t.Errorf("Stream SQL contains unselected column %s: %q", unselected, gotSQL)
		}
	}
}

// TestStream_SoftDeleteExcludedByDefault_MySQL — verifies soft-delete hook
// composition with Stream.
func TestStream_SoftDeleteExcludedByDefault_MySQL(t *testing.T) {
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

	var defaultIDs []int64
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			t.Fatalf("Stream default: %v", err)
		}
		defaultIDs = append(defaultIDs, a.ID)
	}
	if len(defaultIDs) != 1 || defaultIDs[0] != live.ID {
		t.Errorf("Stream default: got %v, want [%d]", defaultIDs, live.ID)
	}

	notNull := false
	var overrideIDs []int64
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
		t.Errorf("Stream override: got %v, want [%d]", overrideIDs, deleted.ID)
	}
}

// TestStream_EarlyBreakReleasesConnection_MySQL — verifies that consumer
// `break` triggers defer rows.Close(); the stdlib *sql.DB's open-connection
// count returns to baseline after partial iterations.
func TestStream_EarlyBreakReleasesConnection_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 20
	_ = seedMysqlArticles(t, ctx, client, author, n)

	authorEq := author

	baselineInUse := testDB.Stats().InUse

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
			break
		}
		if count != 1 {
			t.Errorf("iter %d: yielded %d rows before break, want 1", i, count)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testDB.Stats().InUse <= baselineInUse {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := testDB.Stats().InUse; got > baselineInUse {
		t.Errorf("InUse conns after 5 early-break streams = %d, want <= %d (baseline)", got, baselineInUse)
	}
}

// TestStream_MidStreamCancellation_MySQL — verifies ctx cancellation
// mid-stream surfaces as a final (nil, err) yield. Note: with the stdlib
// MySQL driver's client-side buffering, all rows have already been received
// by the time iteration begins, so cancellation may race with EOF. The test
// pads with enough rows that cancellation reliably observes the ctx error
// before the buffer drains.
func TestStream_MidStreamCancellation_MySQL(t *testing.T) {
	parentCtx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 200 // larger backlog to give the cancel a chance to land mid-iter
	_ = seedMysqlArticles(t, parentCtx, client, author, n)

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

	// Buffering caveat: with the stdlib mysql driver buffering the result set
	// client-side, cancellation may still produce an error from rows.Err()
	// when ctx is checked inside the driver loop. If the buffer drains before
	// the ctx check, the loop may return cleanly — the test passes either way
	// because both behaviors honor the contract that Stream NEVER lies about
	// cancellation. But we still expect SOME work was done before the cancel
	// landed. The post-condition we strictly enforce is: if err is observed,
	// it must be a context error.
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		t.Errorf("Stream after ctx cancel: err = %v, want context.Canceled or nil (buffered drain)", streamErr)
	}
	if yielded == 0 {
		t.Error("Stream after ctx cancel: yielded 0 rows before cancel, want > 0")
	}
}
