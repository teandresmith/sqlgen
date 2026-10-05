package tests

// Raw takes a scan callback (PostgreSQL).
//
// PRD §20.2: Client.Raw passes the result set to a callback instead of
// returning it, so scanning happens inside the hook chain and the rows —
// together with, inside a transaction, the connection reservation they carry
// (§18.5) — cannot outlive the call. These tests pin the four things that
// follow from that shape: the scan still works and still reports errors, two
// Raw reads on one txCtx serialize, a global query hook sees each call once and
// spans the scan, and panic recovery now covers fn without stranding the
// connection.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/hook"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

// rawDeadline bounds every test in this file. Several of them hold or queue on
// a transaction's reservation, and the failure mode a reservation bug produces
// is a hang — an unbounded context would spend the module's whole go test
// -timeout and report nothing useful.
const rawDeadline = 30 * time.Second

const rawTitlesSQL = `SELECT title FROM articles WHERE author = $1 ORDER BY title`

// scanTitles is the fn every read in this file uses: it drains the result set
// and deliberately does not check rows.Err(), so the tests also show that Raw
// surfaces an iteration error the callback forgot to read.
func scanTitles(dst *[]string) func(database.Rows) error {
	return func(rows database.Rows) error {
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				return err
			}
			*dst = append(*dst, title)
		}
		return nil
	}
}

// seededTitles seeds n articles for author and returns their titles in the
// order rawTitlesSQL reads them back.
func seededTitles(t *testing.T, ctx context.Context, client *models.Client, author string, n int) []string {
	t.Helper()
	ids := seedArticles(t, ctx, client, author, n)
	authorEq := author
	articles, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	})
	if err != nil {
		t.Fatalf("GetMany seeded articles: %v", err)
	}
	if len(articles) != len(ids) {
		t.Fatalf("GetMany seeded articles returned %d rows, want %d", len(articles), len(ids))
	}
	titles := make([]string, 0, len(articles))
	for _, a := range articles {
		titles = append(titles, a.Title)
	}
	// rawTitlesSQL orders by title; the seed titles share a prefix and differ
	// only in zero-padded digits, so a byte sort matches any collation.
	slices.Sort(titles)
	return titles
}

// TestRaw_ScansOutsideTransaction is the baseline: args bind positionally from
// the []any, fn sees every row, and nothing is returned but the error.
func TestRaw_ScansOutsideTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()
	client := newClient()

	author := t.Name()
	want := seededTitles(t, ctx, client, author, 5)

	var got []string
	if err := client.Raw(ctx, rawTitlesSQL, []any{author}, scanTitles(&got)); err != nil {
		t.Fatalf("Raw(%q, %q) unexpected error: %v", rawTitlesSQL, author, err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Raw(%q, %q) titles mismatch (-want +got):\n%s", rawTitlesSQL, author, diff)
	}
}

// TestRaw_CallbackErrorIsReturnedAsIs pins the doc comment's promise that fn's
// error comes back unwrapped — so errors.Is works and the message is the
// caller's own — and that returning early, with rows left unread, still gives
// the connection back: the next statement on the same txCtx runs.
func TestRaw_CallbackErrorIsReturnedAsIs(t *testing.T) {
	errStop := errors.New("stop after first row")

	tests := []struct {
		name string
		inTx bool
	}{
		{name: "outside a transaction"},
		{name: "inside a transaction", inTx: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
			defer cancel()
			client := newClient()

			author := t.Name()
			seedArticles(t, ctx, client, author, 3)

			run := func(ctx context.Context) error {
				err := client.Raw(ctx, rawTitlesSQL, []any{author}, func(rows database.Rows) error {
					rows.Next() // leave the rest of the set unread
					return errStop
				})
				if !errors.Is(err, errStop) {
					t.Errorf("Raw() error = %v, want %v", err, errStop)
				}
				if err != nil && err.Error() != errStop.Error() {
					t.Errorf("Raw() error = %q, want fn's error unwrapped: %q", err.Error(), errStop.Error())
				}

				// An abandoned result set must not keep the connection: on a
				// txCtx a leaked reservation would park this read until ctx
				// expires.
				authorEq := author
				got, err := client.Articles().GetMany(ctx, &models.GetArticlesInput{
					Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
				})
				if err != nil {
					return err
				}
				if len(got) != 3 {
					t.Errorf("GetMany after an abandoned Raw returned %d rows, want 3", len(got))
				}
				return nil
			}

			var err error
			if tt.inTx {
				err = client.WithTx(ctx, "raw-fn-error", run)
			} else {
				err = run(ctx)
			}
			if err != nil {
				t.Fatalf("read after Raw: %v", err)
			}
		})
	}
}

// TestRaw_SurfacesRowsErr checks the iteration error reaches the caller even
// though fn never calls rows.Err(): Postgres sends two rows and then a
// division-by-zero, which surfaces only after Next reports false.
func TestRaw_SurfacesRowsErr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()
	client := newClient()

	const query = `SELECT (1 / (3 - g))::text FROM generate_series(1, 5) AS g`
	var got []string
	err := client.Raw(ctx, query, nil, scanTitles(&got))

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("Raw(%q) error = %v, want a *pgconn.PgError", query, err)
	}
	if pgErr.Code != "22012" {
		t.Errorf("Raw(%q) SQLSTATE = %s, want 22012 (division_by_zero)", query, pgErr.Code)
	}
	if diff := cmp.Diff([]string{"0", "1"}, got); diff != "" {
		t.Errorf("Raw(%q) rows before the error mismatch (-want +got):\n%s", query, diff)
	}
}

// TestRaw_ConcurrentReadsOnOneTxCtxSerialize is the case §3.7 exists to make
// safe: two Raw reads fanned out over one txCtx. The first holds its result set
// open until the second has been issued, so the second must queue on the
// reservation rather than reach the connection — on pgx the alternative is
// "conn busy" and a poisoned transaction. Run under -race.
func TestRaw_ConcurrentReadsOnOneTxCtxSerialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()
	client := newClient()

	authorA, authorB := t.Name()+"/a", t.Name()+"/b"
	wantA := seededTitles(t, ctx, client, authorA, 20)
	wantB := seededTitles(t, ctx, client, authorB, 7)

	var gotA, gotB []string
	var errA, errB error
	// secondScanning flips when the second read's fn starts. The first read
	// checks it on every row it scans: its rows are open for that whole loop,
	// so seeing the flag set there means both result sets were open at once.
	var secondScanning atomic.Bool

	if err := client.WithTx(ctx, "raw-fanout", func(txCtx context.Context) error {
		holding := make(chan struct{})
		signalHolding := sync.OnceFunc(func() { close(holding) })
		issued := make(chan struct{})

		var wg sync.WaitGroup
		wg.Go(func() {
			// Unblocks the second read even if this one fails before its
			// first row, so a regression reports instead of hanging.
			defer signalHolding()
			errA = client.Raw(txCtx, rawTitlesSQL, []any{authorA}, func(rows database.Rows) error {
				first := true
				for rows.Next() {
					var title string
					if err := rows.Scan(&title); err != nil {
						return err
					}
					gotA = append(gotA, title)
					if first {
						first = false
						signalHolding()
						// The sleep only widens the window for the second read
						// to reach the reservation while this result set is
						// open; no assertion depends on it winning that race.
						<-issued
						time.Sleep(50 * time.Millisecond)
					}
					if secondScanning.Load() {
						return errors.New("second Raw scanned while the first result set was open")
					}
				}
				return nil
			})
		})
		wg.Go(func() {
			<-holding
			close(issued)
			errB = client.Raw(txCtx, rawTitlesSQL, []any{authorB}, func(rows database.Rows) error {
				secondScanning.Store(true)
				return scanTitles(&gotB)(rows)
			})
		})
		wg.Wait()
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if errA != nil {
		t.Errorf("first Raw on the shared txCtx: %v", errA)
	}
	if errB != nil {
		t.Errorf("second Raw on the shared txCtx: %v", errB)
	}
	if diff := cmp.Diff(wantA, gotA); diff != "" {
		t.Errorf("first Raw titles mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantB, gotB); diff != "" {
		t.Errorf("second Raw titles mismatch (-want +got):\n%s", diff)
	}
}

// TestRaw_StatementInsideFnWaitsOnItsOwnRows pins the one misuse the callback
// cannot prevent, which the doc comment warns about: fn is consumer code
// running inside an open result set, so a statement it issues on the same
// txCtx before draining waits on the reservation its own rows hold. The wait
// ends only when ctx does, with a named error; afterwards the transaction is
// intact.
func TestRaw_StatementInsideFnWaitsOnItsOwnRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()
	client := newClient()

	author := t.Name()
	seedArticles(t, ctx, client, author, 3)
	authorEq := author
	filter := &models.GetArticlesInput{
		Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
	}

	var innerErr error
	if err := client.WithTx(ctx, "raw-self-wait", func(txCtx context.Context) error {
		if err := client.Raw(txCtx, rawTitlesSQL, []any{author}, func(rows database.Rows) error {
			rows.Next() // hold the set open
			innerCtx, innerCancel := context.WithTimeout(txCtx, 250*time.Millisecond)
			defer innerCancel()
			_, innerErr = client.Articles().GetMany(innerCtx, filter)
			return nil
		}); err != nil {
			return err
		}

		// Raw has returned, so its rows are closed and the reservation is back.
		got, err := client.Articles().GetMany(txCtx, filter)
		if err != nil {
			return err
		}
		if len(got) != 3 {
			t.Errorf("GetMany after Raw returned %d rows, want 3", len(got))
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if !errors.Is(innerErr, context.DeadlineExceeded) {
		t.Errorf("statement inside fn error = %v, want context.DeadlineExceeded", innerErr)
	}
	if innerErr != nil && !strings.Contains(innerErr.Error(), "waiting for the connection held by an open result set") {
		t.Errorf("statement inside fn error = %q, want it to name the open result set", innerErr.Error())
	}
}

// TestRaw_PanicInFnIsRecoveredAndReleasesTheConnection covers the acceptance
// criterion that panic recovery still wraps the call — and, since fn now runs
// inside it, that a panic while scanning comes back as an error instead of
// unwinding into the caller. The rows are closed on the way out, so the
// transaction's next statement does not wait.
func TestRaw_PanicInFnIsRecoveredAndReleasesTheConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()
	client := newClient()

	author := t.Name()
	seedArticles(t, ctx, client, author, 3)
	authorEq := author

	var rawErr error
	if err := client.WithTx(ctx, "raw-panic", func(txCtx context.Context) error {
		rawErr = client.Raw(txCtx, rawTitlesSQL, []any{author}, func(rows database.Rows) error {
			rows.Next()
			panic("scan exploded")
		})

		// Bounded well inside rawDeadline so a stranded reservation fails
		// here, with this test's message.
		readCtx, readCancel := context.WithTimeout(txCtx, 5*time.Second)
		defer readCancel()
		got, err := client.Articles().GetMany(readCtx, &models.GetArticlesInput{
			Filter: &models.ArticleFilter{Author: &comparator.String{Eq: &authorEq}},
		})
		if err != nil {
			return err
		}
		if len(got) != 3 {
			t.Errorf("GetMany after a panicking Raw returned %d rows, want 3", len(got))
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if rawErr == nil {
		t.Fatal("Raw with a panicking fn returned nil error")
	}
	for _, want := range []string{"panic in raw_query", "scan exploded"} {
		if !strings.Contains(rawErr.Error(), want) {
			t.Errorf("Raw with a panicking fn error = %q, want it to mention %q", rawErr.Error(), want)
		}
	}
}

// TestRaw_GlobalQueryHookSeesEachCallOnce checks Raw still runs through the
// global query-hook chain — once per call, as Op "raw_query", with the args as
// Input — and pins the two consequences of scanning inside the terminal that
// PRD §20.2 states: the hook's span now covers the scan, and the hook receives
// no result set to intercept.
func TestRaw_GlobalQueryHookSeesEachCallOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rawDeadline)
	defer cancel()

	var (
		mu       sync.Mutex
		events   []string
		rawCalls int
		inputs   []any
		results  []any
	)
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	client := models.New(dbpgx.New(testPool), models.WithQueryHook(func(next hook.QueryHandler) hook.QueryHandler {
		return func(ctx context.Context, q *hook.QueryContext) (any, error) {
			if q.Op != hook.QueryOp("raw_query") {
				return next(ctx, q)
			}
			mu.Lock()
			rawCalls++
			inputs = append(inputs, q.Input)
			mu.Unlock()
			record("hook before")
			result, err := next(ctx, q)
			record("hook after")
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
			return result, err
		}
	}))

	author := t.Name()
	want := seededTitles(t, ctx, client, author, 2)

	var outside, inside []string
	if err := client.Raw(ctx, rawTitlesSQL, []any{author}, func(rows database.Rows) error {
		record("scan")
		return scanTitles(&outside)(rows)
	}); err != nil {
		t.Fatalf("Raw outside a transaction: %v", err)
	}
	if err := client.WithTx(ctx, "raw-hook", func(txCtx context.Context) error {
		return client.Raw(txCtx, rawTitlesSQL, []any{author}, func(rows database.Rows) error {
			record("scan")
			return scanTitles(&inside)(rows)
		})
	}); err != nil {
		t.Fatalf("Raw inside a transaction: %v", err)
	}

	if rawCalls != 2 {
		t.Errorf("query hook saw %d raw_query calls, want 2 (one per Raw)", rawCalls)
	}
	wantEvents := []string{"hook before", "scan", "hook after", "hook before", "scan", "hook after"}
	if diff := cmp.Diff(wantEvents, events); diff != "" {
		t.Errorf("query hook span does not cover the scan (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{[]any{author}, []any{author}}, inputs); diff != "" {
		t.Errorf("raw_query Input mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{nil, nil}, results); diff != "" {
		t.Errorf("raw_query terminal result mismatch — the hook must see no rows (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, outside); diff != "" {
		t.Errorf("Raw outside a transaction titles mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, inside); diff != "" {
		t.Errorf("Raw inside a transaction titles mismatch (-want +got):\n%s", diff)
	}
}
