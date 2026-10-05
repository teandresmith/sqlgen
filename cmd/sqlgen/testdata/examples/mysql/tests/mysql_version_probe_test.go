package tests

// MySQL version probe placement.
//
// mysqlVersionCache.get runs "SELECT VERSION()" once per client, behind a
// sync.Once, and is reached only from the §9.6a lock-mode guard — which runs it
// *after* database.InTransaction(ctx) has passed. So every probe was issued
// from inside a transaction, and once the transaction reservation (§18.5)
// existed that gave the probe two failure modes it never had: a first probe
// queued behind another goroutine's live read blocks inside once.Do, and every
// get() caller in the process blocks with it — including callers on unrelated
// transactions and on the pool.
//
// The probe now runs on the pool querier, not on database.Conn(ctx, ...):
// SELECT VERSION() is a server property, not transaction state, and the cache
// is already client-wide.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/omittable"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
)

// versionProbeQuerier is the querier the client is constructed with, and it
// closes probed on the first SELECT VERSION() it is asked to run.
//
// That single channel is load-bearing twice over. It is the *property* under
// test — the probe must reach the client's own querier rather than whatever
// connection the calling context happens to carry — and it is the *sequencing
// edge* the scenario needs, replacing a sleep that could only guess when the
// probe had run. Against database.Conn(ctx, c.querier) the probe is handed the
// caller's *Tx instead and this querier never sees it, so the channel never
// closes and the test fails on a bounded wait rather than on a race.
type versionProbeQuerier struct {
	inner  database.Querier
	once   sync.Once
	probed chan struct{}
}

func newVersionProbeQuerier(inner database.Querier) *versionProbeQuerier {
	return &versionProbeQuerier{inner: inner, probed: make(chan struct{})}
}

func (q *versionProbeQuerier) Exec(ctx context.Context, sqlStr string, args ...any) (database.Result, error) {
	return q.inner.Exec(ctx, sqlStr, args...)
}

func (q *versionProbeQuerier) Query(ctx context.Context, sqlStr string, args ...any) (database.Rows, error) {
	return q.inner.Query(ctx, sqlStr, args...)
}

func (q *versionProbeQuerier) QueryRow(ctx context.Context, sqlStr string, args ...any) database.Row {
	if strings.Contains(sqlStr, "VERSION()") {
		q.once.Do(func() { close(q.probed) })
	}
	return q.inner.QueryRow(ctx, sqlStr, args...)
}

func (q *versionProbeQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return q.inner.Begin(ctx, name, opts...)
}

// TestMySQLVersionProbe_NotBlockedByAnotherTxReservation pins the probe onto
// the pool, and with it the client-wide stall the old placement created.
//
//	tx1, goroutine A: holds tx1's reservation on an undrained result set.
//	tx1, goroutine B: issues the client's first-ever version-gated read, so it
//	                  is the goroutine that enters once.Do.
//	tx2, main:        issues a version-gated read on its own, free connection.
//
// With the probe on tx1's connection, B parks inside once.Do waiting for a
// reservation A holds, and main's get() parks behind B even though tx2's
// connection is idle — a stall with no relationship to either transaction.
// With the probe on the pool, once.Do completes while A is still holding, so
// main's read runs immediately.
//
// tx2 is only opened once the probe has been observed, so B is necessarily the
// goroutine that ran it; nothing here depends on winning a race.
func TestMySQLVersionProbe_NotBlockedByAnotherTxReservation(t *testing.T) {
	ctx := context.Background()
	// A fresh client means a fresh, unprobed mysqlVersionCache — the whole
	// scenario is about the very first get() in a client's life. The scenario
	// also needs three connections live at once (tx1, tx2, and the pool probe),
	// which testDB allows: main_test.go sets no SetMaxOpenConns, so the pool is
	// unbounded. A bounded pool would deadlock here rather than fail.
	probe := newVersionProbeQuerier(dbstdlib.New(testDB))
	client := models.New(probe)

	// Two rows, one per version-gated reader. They must be distinct: both reads
	// are FOR UPDATE NOWAIT, so pointing them at the same row would make the
	// queued one fail with ER_LOCK_NOWAIT against a lock the other still holds —
	// row contention impersonating the reservation contention under test.
	seedHeld := seedProbeCategory(t, ctx, client, "version-probe-held")
	seedFree := seedProbeCategory(t, ctx, client, "version-probe-free")

	tx1Ctx, err := client.Begin(ctx, "version_probe_holder")
	if err != nil {
		t.Fatalf("Begin tx1: %v", err)
	}
	defer func() { _ = client.Rollback(tx1Ctx) }()

	// A: take tx1's reservation and keep it. A plain SELECT, deliberately left
	// undrained and unclosed — it locks no row, so nothing below contends on
	// the server side; the only contention is the reservation itself.
	holdRows, err := client.Querier(tx1Ctx).Query(tx1Ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatalf("holding query on tx1: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = holdRows.Close()
		}
	}
	defer release()

	// B: the first version-gated read in this client's life. Its SELECT blocks
	// on the reservation A holds, so it is not waited on until the end — but its
	// probe must get through first.
	firstProbeDone := make(chan error, 1)
	go func() {
		_, err := client.Categories().Get(tx1Ctx, seedHeld.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		firstProbeDone <- err
	}()

	select {
	case <-probe.probed:
	case err := <-firstProbeDone:
		t.Fatalf("the held transaction's read returned before the version probe reached the pool: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the version probe never reached the client's querier within 10s while another goroutine " +
			"held that transaction's reservation — the probe is running on the transaction's connection, " +
			"so it is parked inside sync.Once and every other get() caller parks behind it")
	}

	// The probe is through while the reservation is still held, which is the
	// whole claim. B cannot have completed: its SELECT is behind A.
	select {
	case err := <-firstProbeDone:
		t.Fatalf("the held transaction's read completed while its reservation was held: %v", err)
	default:
	}

	// main, on its own transaction and its own connection. Bounded, because the
	// failure this gates is an unbounded wait: it is what the assertion is.
	tx2Ctx, err := client.Begin(ctx, "version_probe_reader")
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	defer func() { _ = client.Rollback(tx2Ctx) }()

	secondDone := make(chan error, 1)
	go func() {
		_, err := client.Categories().Get(tx2Ctx, seedFree.ID, func(o *models.CallOptions[models.CategoryFieldOptions]) {
			o.LockMode = sql.LockForUpdateNoWait
		})
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("version-gated read on an unrelated transaction failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("version-gated read on an unrelated transaction did not complete within 10s " +
			"while another transaction held its own reservation")
	}

	// Let the holder go and confirm the queued read completes too, so the test
	// leaves no goroutine parked on a reservation.
	release()
	select {
	case err := <-firstProbeDone:
		if err != nil {
			t.Errorf("queued read on tx1 failed after the reservation was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("queued read on tx1 did not complete within 10s of the reservation being released")
	}
}

// seedProbeCategory inserts one category and registers its cleanup.
func seedProbeCategory(t *testing.T, ctx context.Context, client *models.Client, name string) *models.Category {
	t.Helper()
	c, err := client.Categories().Create(ctx, &models.CreateCategoryInput{
		Name:        name,
		Description: omittable.Set(strPtrMy("initial")),
	})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	t.Cleanup(func() { _ = client.Categories().HardDelete(ctx, c.ID) })
	return c
}
