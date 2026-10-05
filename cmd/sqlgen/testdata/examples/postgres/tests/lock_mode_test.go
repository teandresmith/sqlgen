package tests

// LockMode E2E (PostgreSQL).
//
// Exercises PRD §9.6a end-to-end against a real Postgres testcontainer:
//   - read-modify-write under FOR UPDATE inside WithTx
//   - FOR UPDATE NOWAIT immediately surfaces the dialect lock-not-available
//     error (SQLSTATE 55P03) when contended
//   - FOR UPDATE SKIP LOCKED job-queue: two concurrent workers split a 20-row
//     backlog with no overlap, no row left behind
//   - outside-tx call returns the sqlgen "LockMode requires an active
//     transaction" guard error before any SQL roundtrip (counter pinned at 0)
//   - savepoint rollback does not release the outer-tx lock
//
// Cache-bypass coverage: postgres/ has no cache configured (sqlgen.yml does
// not enable it), so the SkipCache force is asserted at the codegen layer
// (see lock_mode_codegen_test.go) and at the runtime cache layer in
// cache/tests/skip_test.go. PRD §9.6a's SkipCache invariant is pinned by the
// guard's emitted line at the top of every read method body — verifying that
// pinning here would require wiring a cache module the example does not
// generate.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// countingPgxQuerier wraps the pgx querier and counts every Query / QueryRow
// / Exec call. The outside-tx guard test uses it to assert that the LockMode
// runtime guard short-circuits BEFORE any SQL round-trip — a regression that
// emitted SQL first would tick the counter and fail the test.
type countingPgxQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64
}

func newCountingPgxQuerier(inner database.Querier) *countingPgxQuerier {
	return &countingPgxQuerier{inner: inner}
}

func (c *countingPgxQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.execs.Add(1)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *countingPgxQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.queries.Add(1)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *countingPgxQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.queryRow.Add(1)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *countingPgxQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingPgxQuerier) totalDBOps() int64 {
	return c.queries.Load() + c.queryRow.Load() + c.execs.Load()
}

// TestLockMode_ReadModifyWrite covers the canonical read-modify-write pattern:
// inside a single WithTx, Get with LockForUpdate, mutate, then Update. The
// commit releases the lock. A concurrent goroutine that tries to acquire the
// same FOR UPDATE lock blocks until the outer commit returns.
//
// PRD §9.6a "Pattern 1 — WithTx (recommended)".
func TestLockMode_ReadModifyWrite(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seed, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name:        "lock-rmw-seed",
		Description: omittable.Set(strPtr("initial")),
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, seed.ID) })

	// Coordinate the contended goroutine via three channels:
	//   blockerStarted: outer tx has Get'd with FOR UPDATE
	//   contenderQueued: contender goroutine has been scheduled
	//   contenderDone: contender finished its blocked Get + saw the new value
	blockerStarted := make(chan struct{})
	contenderQueued := make(chan struct{})
	contenderDone := make(chan *models.Category, 1)
	contenderErr := make(chan error, 1)

	go func() {
		<-blockerStarted
		close(contenderQueued)
		// Contender enters its own tx; the Get will block on the outer tx's
		// row lock until that tx commits. Once it returns, the contender
		// observes the post-update value.
		var got *models.Category
		err := client.WithTx(context.Background(), "lock_rmw_contender", func(txCtx context.Context) error {
			c, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = sql.LockForUpdate
			})
			if err != nil {
				return err
			}
			got = c
			return nil
		})
		if err != nil {
			contenderErr <- err
			return
		}
		contenderDone <- got
	}()

	err = client.WithTx(ctx, "lock_rmw_outer", func(txCtx context.Context) error {
		row, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		})
		if err != nil {
			return err
		}
		if row.Description == nil || *row.Description != "initial" {
			t.Errorf("locked Get: description = %v, want %q", row.Description, "initial")
		}

		// Release the contender — it will try to take the same lock and block
		// because the outer tx still holds it.
		close(blockerStarted)
		<-contenderQueued

		// Give the contender a moment to actually issue its locked Get and
		// queue behind the lock. 200ms is enough on every supported runner;
		// the assertion that the contender sees the post-update value is the
		// load-bearing check, not the timing.
		time.Sleep(200 * time.Millisecond)

		// Verify the contender hasn't returned yet — the lock is held.
		select {
		case got := <-contenderDone:
			t.Errorf("contender returned before outer commit: got %+v, want still blocked", got)
		case err := <-contenderErr:
			t.Errorf("contender errored before outer commit: %v", err)
		default:
		}

		_, err = client.Categories().Update(txCtx, seed.ID, &models.UpdateCategoryInput{
			Description: omittable.Set(strPtr("updated")),
		})
		return err
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}

	// Outer commit released the lock — the contender must now finish and
	// observe the post-commit value.
	select {
	case got := <-contenderDone:
		if got.Description == nil || *got.Description != "updated" {
			t.Errorf("contender description after outer commit = %v, want %q", got.Description, "updated")
		}
	case err := <-contenderErr:
		t.Fatalf("contender errored: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("contender did not return within 5s of outer commit")
	}
}

// TestLockMode_NoWaitReturnsLockNotAvailable verifies that LockForUpdateNoWait
// against a row already locked by another transaction returns the dialect's
// lock-not-available error immediately, surfaced through sqlgen's normal error
// chain. PostgreSQL emits SQLSTATE 55P03 (lock_not_available); we assert via
// errors.As against *pgconn.PgError so a string-format change in the driver
// or in sqlgen's wrapping doesn't break the pin.
func TestLockMode_NoWaitReturnsLockNotAvailable(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seed, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-nowait-seed",
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, seed.ID) })

	holderReady := make(chan struct{})
	holderRelease := make(chan struct{})
	holderDone := make(chan error, 1)

	// Holder takes FOR UPDATE and waits for the test to release it.
	go func() {
		holderDone <- client.WithTx(context.Background(), "lock_nowait_holder", func(txCtx context.Context) error {
			if _, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = sql.LockForUpdate
			}); err != nil {
				return err
			}
			close(holderReady)
			<-holderRelease
			return nil
		})
	}()

	<-holderReady
	defer func() {
		close(holderRelease)
		<-holderDone
	}()

	// Contender attempts NOWAIT — must surface SQLSTATE 55P03 immediately.
	contenderErr := client.WithTx(ctx, "lock_nowait_contender", func(txCtx context.Context) error {
		_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		return err
	})
	if contenderErr == nil {
		t.Fatal("LockForUpdateNoWait against held row: err = nil, want lock_not_available")
	}

	var pgErr *pgconn.PgError
	if !errors.As(contenderErr, &pgErr) {
		t.Fatalf("LockForUpdateNoWait error not a *pgconn.PgError: %T: %v", contenderErr, contenderErr)
	}
	if pgErr.Code != "55P03" {
		t.Errorf("LockForUpdateNoWait SQLSTATE = %q, want %q (lock_not_available)", pgErr.Code, "55P03")
	}
}

// TestLockMode_SkipLockedJobQueue exercises PRD §9.6a "Pattern 3 — job queue
// with LockForUpdateSkipLocked": two concurrent workers each take a 10-row
// limit out of a 20-row backlog. Worker A claims its 10 first and holds the
// lock; worker B's GetMany sees those rows as locked and skips them, claiming
// the remaining 10. Both commit. Total processed must equal 20 with no
// overlap and no row left behind.
func TestLockMode_SkipLockedJobQueue(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	const total = 20
	const claimSize = 10

	authorTag := t.Name()
	ids := make([]uuid.UUID, 0, total)
	for i := 0; i < total; i++ {
		a, err := client.Articles().Create(ctx, &models.CreateArticleInput{
			Title:  "queue-job",
			Author: authorTag,
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

	authorEq := authorTag
	filter := &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}}

	type claim struct {
		ids []uuid.UUID
		err error
	}

	workerAReady := make(chan struct{})
	workerARelease := make(chan struct{})
	results := make(chan claim, 2)

	worker := func(name string, gate <-chan struct{}, signal chan<- struct{}, hold bool) {
		var c claim
		c.err = client.WithTx(context.Background(), name, func(txCtx context.Context) error {
			limit := claimSize
			rows, err := client.Articles().GetMany(txCtx, &models.GetArticlesInput{
				Filter: filter,
				Limit:  &limit,
				Sorts:  []sql.Sort{{Column: "id", Direction: sql.Asc}},
			}, func(o *models.CallOptions[models.ArticleFieldOptions]) {
				o.LockMode = sql.LockForUpdateSkipLocked
			})
			if err != nil {
				return err
			}
			for _, r := range rows {
				c.ids = append(c.ids, r.ID)
			}
			if signal != nil {
				close(signal)
			}
			if gate != nil {
				<-gate
			}
			if !hold {
				return nil
			}
			// hold=true workers wait for an external release before commit.
			return nil
		})
		results <- c
	}

	// Worker A: signals once it has its 10 rows locked, then waits for the
	// test to release before committing. Worker B starts only after that
	// signal so its GetMany sees A's 10 rows as already-locked.
	go worker("skip_locked_a", workerARelease, workerAReady, true)
	<-workerAReady
	go worker("skip_locked_b", nil, nil, false)

	// Wait for B to publish its claim, then release A.
	var first, second claim
	first = <-results
	close(workerARelease)
	second = <-results

	if first.err != nil {
		t.Fatalf("first worker tx: %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second worker tx: %v", second.err)
	}

	merged := append([]uuid.UUID{}, first.ids...)
	merged = append(merged, second.ids...)
	if len(merged) != total {
		t.Errorf("SKIP LOCKED total claimed = %d, want %d (first=%d second=%d)",
			len(merged), total, len(first.ids), len(second.ids))
	}
	// uuid.UUID is an array type, so it is comparable but not ordered — the
	// adjacent-duplicate scan below needs an explicit comparison.
	slices.SortFunc(merged, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for i := 1; i < len(merged); i++ {
		if merged[i] == merged[i-1] {
			t.Errorf("SKIP LOCKED duplicate row %v claimed by both workers", merged[i])
		}
	}
}

// TestLockMode_OutsideTxReturnsGuardError verifies that calling Get with a
// non-LockNone mode outside a transaction returns the sqlgen-flavored guard
// error BEFORE any SQL is issued. The countingPgxQuerier pins this — a
// regression that emitted SQL first would tick the counter.
//
// PRD §9.6a "Validation rules" row 1: "LockMode != LockNone outside a
// transaction → Returns sqlgen error before SQL roundtrip".
func TestLockMode_OutsideTxReturnsGuardError(t *testing.T) {
	ctx := context.Background()

	counter := newCountingPgxQuerier(dbpgx.New(testPool))
	client := models.New(counter)

	// Seed via the standard client so the row exists. Then swap to the
	// counting client and reset the counter before exercising the guard.
	seedClient := newClient()
	seed, err := seedClient.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-outside-tx-seed",
	})
	if err != nil {
		t.Fatalf("Create seed: %v", err)
	}
	t.Cleanup(func() { _ = seedClient.Categories().HardDelete(ctx, seed.ID) })

	// Reset counter — only the guarded call must contribute.
	counter.queries.Store(0)
	counter.queryRow.Store(0)
	counter.execs.Store(0)

	for _, mode := range []sql.LockMode{
		sql.LockForUpdate,
		sql.LockForShare,
		sql.LockForUpdateNoWait,
		sql.LockForUpdateSkipLocked,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			counter.queries.Store(0)
			counter.queryRow.Store(0)
			counter.execs.Store(0)

			_, err := client.Categories().Get(ctx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
				o.LockMode = mode
			})
			if err == nil {
				t.Fatalf("Get(LockMode=%s) outside tx: err = nil, want guard error", mode)
			}
			if !strings.Contains(err.Error(), "LockMode requires an active transaction") {
				t.Errorf("Get(LockMode=%s) error = %q, want substring %q", mode, err.Error(), "LockMode requires an active transaction")
			}
			if got := counter.totalDBOps(); got != 0 {
				t.Errorf("Get(LockMode=%s) outside tx: db ops = %d, want 0 (guard must short-circuit before SQL)", mode, got)
			}
		})
	}
}

// TestLockMode_SavepointDoesNotReleaseLock pins PRD §9.6a "Lock release
// semantics": locks acquired before a savepoint survive an inner-savepoint
// rollback and are released only by the outermost commit/rollback. After the
// inner savepoint rolls back, a concurrent contender attempting NOWAIT must
// still hit the lock-not-available error — proving the lock is still held.
func TestLockMode_SavepointDoesNotReleaseLock(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seed, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-savepoint-seed",
	})
	if err != nil {
		t.Fatalf("Create seed: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, seed.ID) })

	innerRolledBack := make(chan struct{})
	contenderProbed := make(chan struct{})
	contenderResult := make(chan error, 1)
	sentinel := errors.New("force inner rollback")

	err = client.WithTx(ctx, "lock_savepoint_outer", func(outerCtx context.Context) error {
		if _, err := client.Categories().Get(outerCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdate
		}); err != nil {
			return err
		}

		// Inner savepoint that rolls back. The PRD §9.6a "Lock release
		// semantics" rule says the outer-tx lock survives this rollback.
		innerErr := client.WithTx(outerCtx, "sp_lock_inner", func(innerCtx context.Context) error {
			return sentinel
		})
		if !errors.Is(innerErr, sentinel) {
			t.Errorf("inner WithTx err = %v, want sentinel", innerErr)
		}
		close(innerRolledBack)

		// Spawn a contender on a separate connection that probes the lock
		// with NOWAIT — must fail with 55P03 because the outer-tx lock is
		// still held even though the savepoint rolled back.
		go func() {
			contenderResult <- client.WithTx(context.Background(), "sp_lock_contender", func(txCtx context.Context) error {
				_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
					o.LockMode = sql.LockForUpdateNoWait
				})
				return err
			})
			close(contenderProbed)
		}()

		select {
		case <-contenderProbed:
		case <-time.After(5 * time.Second):
			t.Fatal("contender did not return within 5s — expected immediate NOWAIT error")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	<-innerRolledBack

	probeErr := <-contenderResult
	if probeErr == nil {
		t.Fatal("post-savepoint NOWAIT contender: err = nil, want 55P03 (lock survived savepoint rollback)")
	}
	var pgErr *pgconn.PgError
	if !errors.As(probeErr, &pgErr) {
		t.Fatalf("post-savepoint NOWAIT contender: error not *pgconn.PgError: %T: %v", probeErr, probeErr)
	}
	if pgErr.Code != "55P03" {
		t.Errorf("post-savepoint NOWAIT contender SQLSTATE = %q, want %q", pgErr.Code, "55P03")
	}
}

func strPtr(s string) *string { return &s }
