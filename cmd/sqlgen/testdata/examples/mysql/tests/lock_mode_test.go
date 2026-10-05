package tests

// LockMode E2E (MySQL).
//
// Mirrors postgres/tests/lock_mode_test.go for the MySQL dialect, exercising
// PRD §9.6a end-to-end against a real mysql:8.0 testcontainer:
//   - read-modify-write under FOR UPDATE inside WithTx
//   - LOCK IN SHARE MODE coexists with FOR SHARE on Postgres semantics:
//     a separate concurrent FOR UPDATE NOWAIT must surface ER_LOCK_NOWAIT
//   - FOR UPDATE NOWAIT immediately surfaces ER_LOCK_NOWAIT (3572) when
//     contended (MySQL 8.0+ only — server version is checked at the runtime
//     guard before NOWAIT/SkipLocked SQL is issued)
//   - FOR UPDATE SKIP LOCKED job-queue: two concurrent workers split a
//     20-row backlog with no overlap
//   - outside-tx call returns the sqlgen guard error before any SQL
//     roundtrip (counter pinned at 0)
//   - savepoint rollback does not release the outer-tx lock
//
// Cache-bypass coverage: mysql/ has no cache configured (sqlgen.yml does not
// enable it). The SkipCache force is asserted at the codegen layer
// (lock_mode_codegen_test.go) and at the runtime cache layer in
// cache/tests/skip_test.go. Same rationale as the postgres test header.

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// MySQL ER_LOCK_NOWAIT — surfaces from `FOR UPDATE NOWAIT` when the targeted
// row is locked by another transaction. Pinned by error number, not message,
// so a server-version locale change doesn't break the test.
const errLockNoWait = 3572

// countingMysqlQuerier wraps the stdlib MySQL querier and counts every Query
// / QueryRow / Exec call. The outside-tx guard test pins query count = 0.
type countingMysqlQuerier struct {
	inner    database.Querier
	queries  atomic.Int64
	queryRow atomic.Int64
	execs    atomic.Int64
}

func newCountingMysqlQuerier(inner database.Querier) *countingMysqlQuerier {
	return &countingMysqlQuerier{inner: inner}
}

func (c *countingMysqlQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	c.execs.Add(1)
	return c.inner.Exec(ctx, sqlStr, args...)
}

func (c *countingMysqlQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	c.queries.Add(1)
	return c.inner.Query(ctx, sqlStr, args...)
}

func (c *countingMysqlQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	c.queryRow.Add(1)
	return c.inner.QueryRow(ctx, sqlStr, args...)
}

func (c *countingMysqlQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingMysqlQuerier) totalDBOps() int64 {
	return c.queries.Load() + c.queryRow.Load() + c.execs.Load()
}

// TestLockMode_ReadModifyWrite_MySQL — read-modify-write inside WithTx with a
// concurrent contender that must block until the outer commit. Mirrors the
// postgres analogue.
func TestLockMode_ReadModifyWrite_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	seed, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name:        "lock-rmw-seed",
		Description: omittable.Set(strPtrMy("initial")),
	})
	if err != nil {
		t.Fatalf("Create seed category: %v", err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, seed.ID) })

	blockerStarted := make(chan struct{})
	contenderQueued := make(chan struct{})
	contenderDone := make(chan *models.Category, 1)
	contenderErr := make(chan error, 1)

	go func() {
		<-blockerStarted
		close(contenderQueued)
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

		close(blockerStarted)
		<-contenderQueued
		time.Sleep(200 * time.Millisecond)

		select {
		case got := <-contenderDone:
			t.Errorf("contender returned before outer commit: got %+v, want still blocked", got)
		case err := <-contenderErr:
			t.Errorf("contender errored before outer commit: %v", err)
		default:
		}

		_, err = client.Categories().Update(txCtx, seed.ID, &models.UpdateCategoryInput{
			Description: omittable.Set(strPtrMy("updated")),
		})
		return err
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}

	select {
	case got := <-contenderDone:
		if got.Description == nil || *got.Description != "updated" {
			t.Errorf("contender description after outer commit = %v, want %q", got.Description, "updated")
		}
	case err := <-contenderErr:
		t.Fatalf("contender errored: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("contender did not return within 15s of outer commit")
	}
}

// TestLockMode_NoWaitReturnsLockNoWait_MySQL — second tx attempts FOR UPDATE
// NOWAIT against a row already locked by the first tx; must surface
// ER_LOCK_NOWAIT (3572). Asserted via errors.As against *mysql.MySQLError so
// a message-format change doesn't break the pin.
func TestLockMode_NoWaitReturnsLockNoWait_MySQL(t *testing.T) {
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

	contenderErr := client.WithTx(ctx, "lock_nowait_contender", func(txCtx context.Context) error {
		_, err := client.Categories().Get(txCtx, seed.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		return err
	})
	if contenderErr == nil {
		t.Fatal("LockForUpdateNoWait against held row: err = nil, want ER_LOCK_NOWAIT")
	}

	var myErr *mysql.MySQLError
	if !errors.As(contenderErr, &myErr) {
		t.Fatalf("LockForUpdateNoWait error not a *mysql.MySQLError: %T: %v", contenderErr, contenderErr)
	}
	if myErr.Number != errLockNoWait {
		t.Errorf("LockForUpdateNoWait MySQL error number = %d, want %d (ER_LOCK_NOWAIT)", myErr.Number, errLockNoWait)
	}
}

// TestLockMode_SkipLockedJobQueue_MySQL — same job-queue pattern as postgres,
// adapted for MySQL. Worker A claims 10 rows + holds; worker B's GetMany sees
// those rows as locked and skips them, claiming the remaining 10. Total
// processed = 20 with no overlap.
func TestLockMode_SkipLockedJobQueue_MySQL(t *testing.T) {
	ctx := context.Background()
	client := newClient()

	const total = 20
	const claimSize = 10

	authorTag := t.Name()
	ids := make([]int64, 0, total)
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
		ids []int64
		err error
	}

	workerAReady := make(chan struct{})
	workerARelease := make(chan struct{})
	results := make(chan claim, 2)

	worker := func(name string, gate <-chan struct{}, signal chan<- struct{}) {
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
			return nil
		})
		results <- c
	}

	go worker("skip_locked_a", workerARelease, workerAReady)
	<-workerAReady
	go worker("skip_locked_b", nil, nil)

	first := <-results
	close(workerARelease)
	second := <-results

	if first.err != nil {
		t.Fatalf("first worker tx: %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second worker tx: %v", second.err)
	}

	merged := append([]int64{}, first.ids...)
	merged = append(merged, second.ids...)
	if len(merged) != total {
		t.Errorf("SKIP LOCKED total claimed = %d, want %d (first=%d second=%d)",
			len(merged), total, len(first.ids), len(second.ids))
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
	for i := 1; i < len(merged); i++ {
		if merged[i] == merged[i-1] {
			t.Errorf("SKIP LOCKED duplicate row %d claimed by both workers", merged[i])
		}
	}
}

// TestLockMode_OutsideTxReturnsGuardError_MySQL pins the runtime guard for
// every non-LockNone mode against MySQL: the call must short-circuit with
// the sqlgen guard error before any SQL roundtrip. The countingMysqlQuerier
// confirms zero queries were issued.
func TestLockMode_OutsideTxReturnsGuardError_MySQL(t *testing.T) {
	ctx := context.Background()

	counter := newCountingMysqlQuerier(dbstdlib.New(testDB))
	client := models.New(counter)

	seedClient := newClient()
	seed, err := seedClient.Categories().Create(ctx, &models.CreateCategoryInput{
		Name: "lock-outside-tx-seed",
	})
	if err != nil {
		t.Fatalf("Create seed: %v", err)
	}
	t.Cleanup(func() { _ = seedClient.Categories().HardDelete(ctx, seed.ID) })

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
				t.Errorf("Get(LockMode=%s) error = %q, want substring %q",
					mode, err.Error(), "LockMode requires an active transaction")
			}
			if got := counter.totalDBOps(); got != 0 {
				t.Errorf("Get(LockMode=%s) outside tx: db ops = %d, want 0 (guard must short-circuit before SQL)", mode, got)
			}
		})
	}
}

// TestLockMode_SavepointDoesNotReleaseLock_MySQL pins PRD §9.6a "Lock release
// semantics" on MySQL: outer-tx FOR UPDATE survives an inner savepoint
// rollback. After the inner savepoint is rolled back, a concurrent NOWAIT
// contender must still hit ER_LOCK_NOWAIT.
func TestLockMode_SavepointDoesNotReleaseLock_MySQL(t *testing.T) {
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

		innerErr := client.WithTx(outerCtx, "sp_lock_inner", func(innerCtx context.Context) error {
			return sentinel
		})
		if !errors.Is(innerErr, sentinel) {
			t.Errorf("inner WithTx err = %v, want sentinel", innerErr)
		}
		close(innerRolledBack)

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
		case <-time.After(15 * time.Second):
			t.Fatal("contender did not return within 15s — expected immediate NOWAIT error")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("outer WithTx: %v", err)
	}
	<-innerRolledBack

	probeErr := <-contenderResult
	if probeErr == nil {
		t.Fatal("post-savepoint NOWAIT contender: err = nil, want ER_LOCK_NOWAIT (lock survived savepoint rollback)")
	}
	var myErr *mysql.MySQLError
	if !errors.As(probeErr, &myErr) {
		t.Fatalf("post-savepoint NOWAIT contender: error not *mysql.MySQLError: %T: %v", probeErr, probeErr)
	}
	if myErr.Number != errLockNoWait {
		t.Errorf("post-savepoint NOWAIT contender error number = %d, want %d", myErr.Number, errLockNoWait)
	}
}

func strPtrMy(s string) *string { return &s }
