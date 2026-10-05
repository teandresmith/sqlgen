package tests

// Stream inside a transaction (PostgreSQL).
//
// PRD §9.4a: Stream is the one generated method that runs consumer code inside
// an open result set, so inside a transaction it holds the transaction's only
// connection — and its reservation (§18.5) — for the whole iteration. The two
// contracts cannot both hold, so Stream refuses a transaction unless the caller
// sets CallOptions.AllowInTransaction.
//
// The refusal has to be observable as "no SQL at all", not merely "an error":
// the guard runs before the hook chain is built, so a refused Stream fires no
// hooks either, global authorization included. A querier wrapper cannot see
// that — inside a transaction the generated method reaches the *Tx through
// database.Conn and never touches the pool querier again — so these tests
// observe at the driver, through a pgx QueryTracer on a dedicated pool.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teandresmith/sqlgen/comparator"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// streamTxDeadline bounds every test in this file. Each one exercises a path
// that either holds or queues on the transaction's reservation, and the failure
// mode a reservation bug produces is a hang — so an unbounded context would
// spend the module's whole `go test -timeout` and report nothing useful.
const streamTxDeadline = 30 * time.Second

// statementTracer counts every statement pgx puts on the wire and keeps the
// SQL of each, so a test can assert both "nothing was issued" and — when
// something was — exactly what.
type statementTracer struct {
	count atomic.Int64

	mu  sync.Mutex
	sql []string
}

func (s *statementTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	s.count.Add(1)
	s.mu.Lock()
	s.sql = append(s.sql, data.SQL)
	s.mu.Unlock()
	return ctx
}

func (s *statementTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// since returns the statements traced after mark, for error messages.
func (s *statementTracer) since(mark int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mark >= int64(len(s.sql)) {
		return nil
	}
	return append([]string(nil), s.sql[mark:]...)
}

// newTracedClient builds a second pool over the same database with a query
// tracer attached, plus a client on it. The shared testPool is left alone so
// tracing costs nothing for every other test in the package.
func newTracedClient(t *testing.T) (*models.Client, *statementTracer) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testPool.Config().ConnString())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	tracer := &statementTracer{}
	cfg.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return models.New(dbpgx.New(pool)), tracer
}

// TestStream_RefusedInsideTransactionIssuesNoSQL is the §9.4a default: without
// the opt-in, a Stream on a txCtx yields exactly one (nil, error) and puts
// nothing on the wire. The error names both the alternative and the obligation
// the opt-in would commit the caller to, since it is the only place a consumer
// who hit this learns what to do next.
func TestStream_RefusedInsideTransactionIssuesNoSQL(t *testing.T) {
	// Bounded: every assertion below is about a call that must not park on the
	// transaction's reservation, so a regression should surface here with the
	// test's own message rather than as the module's 5m go test timeout.
	ctx, cancel := context.WithTimeout(context.Background(), streamTxDeadline)
	defer cancel()
	client, tracer := newTracedClient(t)

	author := t.Name()
	seedArticles(t, ctx, client, author, 3)

	authorEq := author
	var yields int
	var gotErr error
	var mark, traced int64

	if err := client.WithTx(ctx, "stream-refusal", func(txCtx context.Context) error {
		mark = tracer.count.Load()
		for a, err := range client.Articles().Stream(txCtx, &models.StreamArticlesInput{
			Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		}) {
			yields++
			gotErr = err
			if a != nil {
				t.Errorf("refused Stream yielded a non-nil entity %v", a.ID)
			}
		}
		traced = tracer.count.Load() - mark
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if yields != 1 {
		t.Errorf("refused Stream yielded %d times, want exactly 1", yields)
	}
	if gotErr == nil {
		t.Fatal("refused Stream yielded no error")
	}
	for _, want := range []string{
		"stream articles",
		"unsupported inside a transaction",
		"use Connection in a loop",
		"AllowInTransaction",
		"issues nothing else on this txCtx",
	} {
		if !strings.Contains(gotErr.Error(), want) {
			t.Errorf("refusal error %q does not mention %q", gotErr.Error(), want)
		}
	}
	if traced != 0 {
		t.Errorf("refused Stream issued %d statements, want 0: %v", traced, tracer.since(mark))
	}
}

// TestStream_AllowInTransactionStreamsUnderTheReservation is §2.10 probe B: an
// opted-in Stream is the ordinary read of §18.5, holding the reservation for
// the whole iteration rather than bypassing it. A GetMany issued concurrently
// on the same txCtx therefore queues behind it and completes safely instead of
// racing the driver — which is why the opt-in must not opt out of the
// reservation. Run under -race, this is the probe that measured 13 data races
// on the bypass variant.
func TestStream_AllowInTransactionStreamsUnderTheReservation(t *testing.T) {
	// Bounded for the same reason, and it matters most here: the contending
	// GetMany queues on the reservation the stream holds, so a reservation that
	// is never released would hang rather than fail.
	ctx, cancel := context.WithTimeout(context.Background(), streamTxDeadline)
	defer cancel()
	client := newClient()

	author := t.Name()
	const n = 40
	ids := seedArticles(t, ctx, client, author, n)

	authorEq := author
	var concurrentRows int
	var concurrentErr error

	if err := client.WithTx(ctx, "stream-allowed", func(txCtx context.Context) error {
		// The contending reader starts before the stream does and stays on the
		// same txCtx, so it either wins the reservation outright or waits for
		// the stream to drain. Both orders are correct; neither may race.
		done := make(chan struct{})
		go func() {
			defer close(done)
			got, err := client.Articles().GetMany(txCtx, &models.GetArticlesInput{
				Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
			})
			concurrentRows, concurrentErr = len(got), err
		}()

		streamed := 0
		for a, err := range client.Articles().Stream(txCtx, &models.StreamArticlesInput{
			Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		}, func(o *models.CallOptions[models.StreamArticleFieldOptions]) {
			o.AllowInTransaction = true
		}) {
			if err != nil {
				t.Errorf("opted-in Stream yielded error: %v", err)
				break
			}
			if a == nil {
				t.Error("opted-in Stream yielded a nil entity with no error")
				break
			}
			streamed++
		}

		// The reader must finish inside the transaction: a statement that
		// outlived fn would run against a committed Tx.
		<-done

		if streamed != n {
			t.Errorf("opted-in Stream yielded %d rows, want %d", streamed, n)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if concurrentErr != nil {
		t.Errorf("concurrent GetMany on the same txCtx failed: %v", concurrentErr)
	}
	if concurrentRows != len(ids) {
		t.Errorf("concurrent GetMany returned %d rows, want %d", concurrentRows, len(ids))
	}
}

// TestStream_OutsideTransactionUnaffectedByTheGuard pins the other half of the
// acceptance criterion: the guard is scoped to database.InTransaction, so a
// Stream on a plain ctx is byte-identical in behavior to before — no opt-in
// needed, no refusal, and no extra statement on the wire beyond the one SELECT.
func TestStream_OutsideTransactionUnaffectedByTheGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), streamTxDeadline)
	defer cancel()
	client, tracer := newTracedClient(t)

	author := t.Name()
	const n = 5
	seedArticles(t, ctx, client, author, n)

	authorEq := author
	mark := tracer.count.Load()
	streamed := 0
	for a, err := range client.Articles().Stream(ctx, &models.StreamArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}) {
		if err != nil {
			t.Fatalf("Stream outside a transaction yielded error: %v", err)
		}
		if a == nil {
			t.Fatal("Stream outside a transaction yielded a nil entity")
		}
		streamed++
	}
	if streamed != n {
		t.Errorf("Stream outside a transaction yielded %d rows, want %d", streamed, n)
	}
	if got := tracer.count.Load() - mark; got != 1 {
		t.Errorf("Stream outside a transaction issued %d statements, want exactly 1: %v", got, tracer.since(mark))
	}
}

// TestStream_RefusedInsideTransactionLeavesTheTxUsable checks the refusal is
// inert rather than merely early: because it returns before the hook chain is
// built, nothing has touched the connection, so the very next statement on the
// same txCtx runs normally. A guard that acquired the reservation and forgot to
// give it back would hang here instead.
func TestStream_RefusedInsideTransactionLeavesTheTxUsable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), streamTxDeadline)
	defer cancel()
	client := newClient()

	author := t.Name()
	seedArticles(t, ctx, client, author, 2)

	authorEq := author
	if err := client.WithTx(ctx, "stream-refusal-then-read", func(txCtx context.Context) error {
		for _, err := range client.Articles().Stream(txCtx, &models.StreamArticlesInput{}) {
			if err == nil {
				t.Error("Stream inside a transaction was not refused")
			}
			break
		}
		got, err := client.Articles().GetMany(txCtx, &models.GetArticlesInput{
			Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		})
		if err != nil {
			return err
		}
		if len(got) != 2 {
			t.Errorf("GetMany after a refused Stream returned %d rows, want 2", len(got))
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}
