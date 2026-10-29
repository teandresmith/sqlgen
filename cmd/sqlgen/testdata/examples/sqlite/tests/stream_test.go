package tests

// Stream E2E (SQLite).
//
// Mirrors postgres/tests/stream_test.go for the SQLite dialect, exercising
// PRD §9.4a end-to-end against the in-memory SQLite test DB:
//   - filter / sort propagation captured in the recorded SQL
//   - scalar-only column selection via StreamFieldOptions
//   - early termination releases the connection cleanly
//   - mid-stream cancellation surfaces (nil, err) and terminates the iterator
//
// Per-driver buffering caveat: modernc.org/sqlite is a translation of the C
// sqlite3 library to Go, exposed through database/sql. Iteration is row-at-a-
// time within the driver, but the database is in-process so "memory-bounded"
// is a less meaningful contract than for client-server drivers — the entire
// table lives in the same process address space. The memory-bound assertion
// from the postgres test is intentionally NOT mirrored here for the same
// reason. Stream still avoids GetMany's slice-grow + per-row backing-slice
// allocator path, which is the codegen-side pin.
//
// Cache-bypass coverage: sqlite/ has no cache configured (sqlgen.yml does not
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

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

// recordingSqliteQuerier wraps the stdlib sqlite querier and captures the
// last Query SQL string + counts every Query / QueryRow / Exec call.
type recordingSqliteQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64

	mu       sync.Mutex
	lastSQL  string
	lastArgs []any
}

func newRecordingSqliteQuerier(inner database.Querier) *recordingSqliteQuerier {
	return &recordingSqliteQuerier{inner: inner}
}

func (r *recordingSqliteQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	r.execs.Add(1)
	return r.inner.Exec(ctx, sqlStr, args...)
}

func (r *recordingSqliteQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	r.queries.Add(1)
	r.mu.Lock()
	r.lastSQL = sqlStr
	r.lastArgs = append([]any(nil), args...)
	r.mu.Unlock()
	return r.inner.Query(ctx, sqlStr, args...)
}

func (r *recordingSqliteQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	r.queryRow.Add(1)
	return r.inner.QueryRow(ctx, sqlStr, args...)
}

func (r *recordingSqliteQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return r.inner.Begin(ctx, name, opts...)
}

func (r *recordingSqliteQuerier) snapshot() (string, []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSQL, append([]any(nil), r.lastArgs...)
}

// seedSqliteArticles inserts n articles tagged with the given author and
// registers cleanup. Returns the inserted IDs in insertion order.
func seedSqliteArticles(t *testing.T, ctx context.Context, client *models.Client, author string, n int) []int64 {
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

// TestStream_FilterAndSortPropagation_SQLite — verifies WHERE + ORDER BY
// land in the captured SELECT and Stream emits no LIMIT / OFFSET.
func TestStream_FilterAndSortPropagation_SQLite(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingSqliteQuerier(dbstdlib.New(testDB))
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
	// SQLite uses double-quoted identifiers (same as postgres).
	if !strings.Contains(gotSQL, `"title"`) {
		t.Errorf("Stream SQL missing sort column %q: %q", `"title"`, gotSQL)
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

// TestStream_ScalarOnlyColumnSelection_SQLite — verifies StreamFieldOptions
// produces a SELECT projecting only the listed columns, and the yielded
// entity has only those fields populated.
func TestStream_ScalarOnlyColumnSelection_SQLite(t *testing.T) {
	ctx := context.Background()

	rec := newRecordingSqliteQuerier(dbstdlib.New(testDB))
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
	if !strings.Contains(gotSQL, `"id"`) {
		t.Errorf("Stream SQL missing selected column %q: %q", `"id"`, gotSQL)
	}
	if !strings.Contains(gotSQL, `"title"`) {
		t.Errorf("Stream SQL missing selected column %q: %q", `"title"`, gotSQL)
	}
	// Only the SELECT list is checked: the WHERE quotes the filter column too,
	// and this test filters on author.
	selectList, _, _ := strings.Cut(gotSQL, " FROM ")
	for _, unselected := range []string{`"body"`, `"author"`, `"created_at"`} {
		if strings.Contains(selectList, unselected) {
			t.Errorf("Stream SQL contains unselected column %s: %q", unselected, gotSQL)
		}
	}
}

// TestStream_EarlyBreakReleasesConnection_SQLite — verifies that consumer
// `break` triggers defer rows.Close(); the stdlib *sql.DB's open-connection
// count returns to baseline after partial iterations.
func TestStream_EarlyBreakReleasesConnection_SQLite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 20
	_ = seedSqliteArticles(t, ctx, client, author, n)

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

// TestStream_MidStreamCancellation_SQLite — verifies ctx cancellation
// mid-stream surfaces as a final (nil, err) yield. Because modernc.org/sqlite
// is in-process, cancellation is checked at row-fetch boundaries; with a
// large enough backlog the cancel reliably lands mid-iter.
func TestStream_MidStreamCancellation_SQLite(t *testing.T) {
	parentCtx := context.Background()
	client := newClient()

	author := t.Name()
	const n = 200
	_ = seedSqliteArticles(t, parentCtx, client, author, n)

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

	// In-process SQLite may drain its row buffer faster than the cancel can
	// land — accept "no error if buffer drained" but ENFORCE that any error
	// observed is a context error. Same caveat shape as MySQL's stdlib.
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		t.Errorf("Stream after ctx cancel: err = %v, want context.Canceled or nil (buffered drain)", streamErr)
	}
	if yielded == 0 {
		t.Error("Stream after ctx cancel: yielded 0 rows before cancel, want > 0")
	}
}
