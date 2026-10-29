package suite

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/hook"
	"github.com/teandresmith/sqlgen/sql"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
)

// stream: Stream yields the matching rows one at a time through an iterator.
func stream(t *testing.T, h *Harness) {
	ctx := context.Background()

	t.Run("yields only the matching rows, each once, in the requested sort order", func(t *testing.T) {
		h.Reset(t)
		// Inserted out of price order, so a stream that ignored Sorts would
		// come back in a different order.
		p300 := createPricedProduct(t, h, "p300", 300)
		p100 := createPricedProduct(t, h, "p100", 100)
		p200 := createPricedProduct(t, h, "p200", 200)
		createPricedProduct(t, h, "cheap", 50)
		// A child row: the yielded entity's relationship field stays at its zero value.
		createOrderItem(t, h, models.OrderItemPK{OrderID: 1, ProductID: p300.ID})
		got, errs := strCollect(h.Client.Products().Stream(ctx, &models.StreamProductsInput{
			Filter: &models.ProductFilter{Price: &comparator.Number[int64]{Gte: new(int64(100))}},
			Sorts:  []sql.Sort{{Column: "price", Direction: sql.Asc}},
		}))
		if len(errs) != 0 {
			t.Fatalf("Stream yielded errors %v, want none", errs)
		}
		if diff := cmp.Diff([]*models.Product{p100, p200, p300}, got); diff != "" {
			t.Errorf("Stream(price >= 100, by price) yielded (-want +got):\n%s", diff)
		}
	})

	t.Run("nil input yields the same rows as the zero-value input", func(t *testing.T) {
		h.Reset(t)
		want := []int64{
			createPricedProduct(t, h, "a", 300).ID,
			createPricedProduct(t, h, "b", 100).ID,
			createPricedProduct(t, h, "c", 200).ID,
		}
		nilRows, nilErrs := strCollect(h.Client.Products().Stream(ctx, nil))
		if len(nilErrs) != 0 {
			t.Fatalf("Stream(nil) yielded errors %v, want none", nilErrs)
		}
		zeroRows, zeroErrs := strCollect(h.Client.Products().Stream(ctx, &models.StreamProductsInput{}))
		if len(zeroErrs) != 0 {
			t.Fatalf("Stream(&StreamProductsInput{}) yielded errors %v, want none", zeroErrs)
		}
		// No sorts, so the order is the database's: compare as sets.
		if diff := cmp.Diff(want, strSortedIDs(nilRows)); diff != "" {
			t.Errorf("Stream(nil) yielded keys (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(want, strSortedIDs(zeroRows)); diff != "" {
			t.Errorf("Stream(&StreamProductsInput{}) yielded keys (-want +got):\n%s", diff)
		}
	})

	t.Run("a hook sees a non-nil input when the caller passes nil", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		if _, errs := strCollect(client.Products().Stream(ctx, nil)); len(errs) != 0 {
			t.Fatalf("Stream(nil) yielded errors %v, want none", errs)
		}
		if len(seen()) != 1 {
			t.Fatalf("query hook fired %d times, want 1", len(seen()))
		}
		in, ok := seen()[0].Input.(*models.StreamProductsInput)
		if !ok || in == nil {
			t.Fatalf("QueryContext.Input = %#v, want a non-nil *StreamProductsInput", seen()[0].Input)
		}
		if in.Filter != nil || in.Sorts != nil {
			t.Errorf("QueryContext.Input = %+v, want the zero-value input", *in)
		}
	})

	t.Run("a filter matching no row yields nothing and no error", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		got, errs := strCollect(h.Client.Products().Stream(ctx, &models.StreamProductsInput{
			Filter: &models.ProductFilter{Name: &comparator.String{Eq: new("missing")}},
		}))
		if len(errs) != 0 {
			t.Errorf("Stream(name = missing) yielded errors %v, want none", errs)
		}
		if len(got) != 0 {
			t.Errorf("Stream(name = missing) yielded %d rows, want none", len(got))
		}
	})

	t.Run("soft-deleted rows are not yielded by default", func(t *testing.T) {
		h.Reset(t)
		a := createDocument(t, h, "a")
		createDeletedDocument(t, h, "gone")
		createDocument(t, h, "other")
		// The filter matches both a and gone, so only the default exclusion drops gone.
		got, errs := strCollect(h.Client.Documents().Stream(ctx, &models.StreamDocumentsInput{
			Filter: &models.DocumentFilter{Title: &comparator.String{In: []string{"a", "gone"}}},
		}))
		if len(errs) != 0 {
			t.Fatalf("Stream(title in [a, gone]) yielded errors %v, want none", errs)
		}
		if diff := cmp.Diff([]*models.Document{a}, got); diff != "" {
			t.Errorf("Stream(title in [a, gone]) yielded (-want +got):\n%s", diff)
		}
	})

	t.Run("a deleted-at comparator yields soft-deleted rows", func(t *testing.T) {
		h.Reset(t)
		createDocument(t, h, "a")
		gone := createDeletedDocument(t, h, "gone")
		got, errs := strCollect(h.Client.Documents().Stream(ctx, &models.StreamDocumentsInput{
			Filter: &models.DocumentFilter{
				Title:     &comparator.String{In: []string{"a", "gone"}},
				DeletedAt: &comparator.NullableTime{Null: new(false)},
			},
		}))
		if len(errs) != 0 {
			t.Fatalf("Stream(title in [a, gone], deleted) yielded errors %v, want none", errs)
		}
		if diff := cmp.Diff([]*models.Document{gone}, got); diff != "" {
			t.Errorf("Stream(title in [a, gone], deleted) yielded (-want +got):\n%s", diff)
		}
	})

	t.Run("inside a transaction without the opt-in yields one error, fires no hook and leaves the transaction usable", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "before")
		client, seen := queryRecorder(h)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			if _, err := client.Products().Create(txCtx, &models.CreateProductInput{Name: "inside", Price: 100}); err != nil {
				return err
			}
			var (
				yields  int
				lastErr error
			)
			for p, err := range client.Products().Stream(txCtx, nil) {
				yields++
				lastErr = err
				if p != nil {
					t.Errorf("refused Stream yielded entity %+v, want nil", p)
				}
			}
			if yields != 1 {
				t.Errorf("refused Stream yielded %d times, want 1", yields)
			}
			// No t.Fatal inside the transaction: it would skip the rollback and
			// leave the transaction holding its locks for the subtests after it.
			if lastErr == nil {
				t.Error("Stream inside a transaction yielded no error, want the refusal")
			}
			for _, want := range []string{"Connection", "AllowInTransaction"} {
				if lastErr != nil && !strings.Contains(lastErr.Error(), want) {
					t.Errorf("refusal %q does not name %q", lastErr, want)
				}
			}
			if n := len(seen()); n != 0 {
				t.Errorf("refused Stream fired %d query hooks, want none: %+v", n, seen())
			}
			// The next statement on the transaction runs and sees the transaction's
			// write. Bounded: a refusal that kept the connection would make it wait.
			nextCtx, cancel := context.WithTimeout(txCtx, 5*time.Second)
			defer cancel()
			got, err := client.Products().GetMany(nextCtx, nil)
			if err != nil {
				return fmt.Errorf("GetMany after the refusal: %w", err)
			}
			if diff := cmp.Diff([]string{"before", "inside"}, strSortedNames(got)); diff != "" {
				t.Errorf("GetMany after the refusal (-want +got):\n%s", diff)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
	})

	t.Run("with the opt-in streams inside the transaction and sees its writes", func(t *testing.T) {
		h.Reset(t)
		before := createPricedProduct(t, h, "before", 100)
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			inside, err := h.Client.Products().Create(txCtx, &models.CreateProductInput{Name: "inside", Price: 200})
			if err != nil {
				return err
			}
			h.Querier.ResetCount()
			got, errs := strCollect(h.Client.Products().Stream(txCtx, &models.StreamProductsInput{
				Sorts: []sql.Sort{{Column: "price", Direction: sql.Desc}},
			}, func(o *models.CallOptions[models.StreamProductFieldOptions]) {
				o.AllowInTransaction = true
			}))
			// The counting querier sees only statements that bypass the transaction.
			if stmts := h.Querier.Statements(); len(stmts) != 0 {
				t.Errorf("opted-in Stream sent %q outside the transaction, want nothing", stmts)
			}
			if len(errs) != 0 {
				return fmt.Errorf("opted-in Stream yielded errors %v, want none", errs)
			}
			if diff := cmp.Diff([]*models.Product{inside, before}, got); diff != "" {
				t.Errorf("opted-in Stream yielded (-want +got):\n%s", diff)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
	})

	t.Run("fires one stream query hook with the caller's input", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		createProduct(t, h, "b")
		client, seen := queryRecorder(h)
		input := &models.StreamProductsInput{Filter: &models.ProductFilter{Name: &comparator.String{In: []string{"a", "b"}}}}
		got, errs := strCollect(client.Products().Stream(ctx, input))
		if len(errs) != 0 || len(got) != 2 {
			t.Fatalf("Stream(name in [a, b]) yielded %d rows and errors %v, want 2 rows and none", len(got), errs)
		}
		if len(seen()) != 1 {
			t.Fatalf("query hook fired %d times, want 1", len(seen()))
		}
		q := seen()[0]
		if q.Op != hook.OpStream || q.Table != models.TableProducts || q.PK != nil {
			t.Errorf("QueryContext = {Op: %q, Table: %q, PK: %v}, want {Op: %q, Table: %q, PK: nil}",
				q.Op, q.Table, q.PK, hook.OpStream, models.TableProducts)
		}
		if q.Input != any(input) {
			t.Errorf("QueryContext.Input = %v, want the caller's input %p", q.Input, input)
		}
	})

	t.Run("a hook sees the cache skipped whatever the caller set", func(t *testing.T) {
		h.Reset(t)
		createProduct(t, h, "a")
		client, seen := queryRecorder(h)
		if _, errs := strCollect(client.Products().Stream(ctx, nil)); len(errs) != 0 {
			t.Fatalf("Stream(nil) yielded errors %v, want none", errs)
		}
		if len(seen()) != 1 {
			t.Fatalf("query hook fired %d times, want 1", len(seen()))
		}
		opts, ok := seen()[0].CallOptions.(models.CallOptions[models.StreamProductFieldOptions])
		if !ok {
			t.Fatalf("QueryContext.CallOptions = %T, want CallOptions[StreamProductFieldOptions]", seen()[0].CallOptions)
		}
		if !opts.SkipCache {
			t.Error("QueryContext.CallOptions.SkipCache = false, want true")
		}
	})

	t.Run("a narrowed field selection fills only the selected fields", func(t *testing.T) {
		h.Reset(t)
		a := createPricedProduct(t, h, "a", 200)
		b := createPricedProduct(t, h, "b", 100)
		got, errs := strCollect(h.Client.Products().Stream(ctx, &models.StreamProductsInput{
			Sorts: []sql.Sort{{Column: "price", Direction: sql.Asc}},
		}, func(o *models.CallOptions[models.StreamProductFieldOptions]) {
			o.FieldOptions = &models.StreamProductFieldOptions{ID: true, Price: true}
		}))
		if len(errs) != 0 {
			t.Fatalf("Stream(selecting id, price) yielded errors %v, want none", errs)
		}
		// Name and CreatedAt are set on every stored row, so a full read would fill them.
		want := []*models.Product{{ID: b.ID, Price: 100}, {ID: a.ID, Price: 200}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Stream(selecting id, price) yielded (-want +got):\n%s", diff)
		}
	})

	// The subtests after this one break out of a stream or end one with an
	// error. If breaking out left the result set open, each would leak a
	// cursor, and on SQLite the next h.Reset would block until the test
	// timeout, far from the cause. So this subtest runs before them and, when it
	// fails, the stream contract stops here with one named failure.
	brokeCleanly := t.Run("breaking out of the loop stops iteration and frees the connection for the next statement", func(t *testing.T) {
		h.Reset(t)
		for _, name := range []string{"a", "b", "c"} {
			createProduct(t, h, name)
		}
		// Inside a transaction the result set holds the transaction's only
		// connection, so a set left open after the break would make the next
		// statement on txCtx wait until its deadline.
		err := database.WithTransaction(ctx, h.Querier, "conformance", func(txCtx context.Context) error {
			// The proof is the GetMany on txCtx below: it runs only once the
			// result set is closed. strRangeGuarded keeps a stray range panic
			// from skipping the rollback.
			rows, errs, panicked := strRangeGuarded(h.Client.Products().Stream(txCtx, nil, func(o *models.CallOptions[models.StreamProductFieldOptions]) {
				o.AllowInTransaction = true
			}), true)
			if panicked != nil {
				return fmt.Errorf("ranging over the opted-in Stream panicked: %v", panicked)
			}
			if rows != 1 || len(errs) != 0 {
				return fmt.Errorf("opted-in Stream yielded %d rows and errors %v before the break, want 1 row and none", rows, errs)
			}
			nextCtx, cancel := context.WithTimeout(txCtx, 5*time.Second)
			defer cancel()
			got, err := h.Client.Products().GetMany(nextCtx, nil)
			if err != nil {
				return fmt.Errorf("GetMany after the break: %w", err)
			}
			if len(got) != 3 {
				t.Errorf("GetMany after the break returned %d rows, want 3", len(got))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction error = %v, want nil", err)
		}
	})
	if !brokeCleanly {
		t.Error("not running the remaining Stream subtests: a result set left open after a break would hang them")
		return
	}

	t.Run("a row error from the driver is yielded after the rows before it", func(t *testing.T) {
		h.Reset(t)
		for _, name := range []string{"a", "b", "c"} {
			createProduct(t, h, name)
		}
		errDriver := errors.New("driver failed mid-stream")
		client := models.New(&strFailingQuerier{Querier: h.Querier, after: 2, err: errDriver})
		var (
			rows int
			errs []error
		)
		for p, err := range client.Products().Stream(ctx, nil) {
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if p == nil {
				t.Error("Stream yielded a nil entity with no error")
			}
			if len(errs) != 0 {
				t.Error("Stream yielded a row after the error")
			}
			rows++
		}
		if rows != 2 {
			t.Errorf("Stream yielded %d rows before the error, want 2", rows)
		}
		if len(errs) != 1 || !errors.Is(errs[0], errDriver) {
			t.Errorf("Stream yielded errors %v, want exactly one wrapping %v", errs, errDriver)
		}
	})

	errAbort := errors.New("aborted by hook")
	hookErrors := []struct {
		name       string
		hook       hook.QueryHook
		breakEarly bool
		wantRows   int
		check      func(error) bool
		want       string
	}{
		{
			name: "a panicking hook is yielded as the recovered error after no rows",
			hook: func(hook.QueryHandler) hook.QueryHandler {
				return func(context.Context, *hook.QueryContext) (any, error) { panic("boom from hook") }
			},
			check: func(err error) bool {
				return err.Error() == fmt.Sprintf("sqlgen: panic in stream %s: boom from hook", models.TableProducts)
			},
			want: fmt.Sprintf("sqlgen: panic in stream %s: boom from hook", models.TableProducts),
		},
		{
			name: "a hook's abort error is yielded once after no rows",
			hook: func(hook.QueryHandler) hook.QueryHandler {
				return func(context.Context, *hook.QueryContext) (any, error) { return nil, errAbort }
			},
			check: func(err error) bool { return errors.Is(err, errAbort) },
			want:  errAbort.Error(),
		},
		{
			name: "a hook's error after the rows is yielded last",
			hook: func(next hook.QueryHandler) hook.QueryHandler {
				return func(ctx context.Context, q *hook.QueryContext) (any, error) {
					if _, err := next(ctx, q); err != nil {
						return nil, err
					}
					return nil, errAbort
				}
			},
			wantRows: 2,
			check:    func(err error) bool { return errors.Is(err, errAbort) },
			want:     errAbort.Error(),
		},
		{
			name: "a hook's error after the consumer broke out is not yielded",
			hook: func(next hook.QueryHandler) hook.QueryHandler {
				return func(ctx context.Context, q *hook.QueryContext) (any, error) {
					if _, err := next(ctx, q); err != nil {
						return nil, err
					}
					return nil, errAbort
				}
			},
			breakEarly: true,
			wantRows:   1,
		},
	}
	for _, tc := range hookErrors {
		t.Run(tc.name, func(t *testing.T) {
			h.Reset(t)
			createProduct(t, h, "a")
			createProduct(t, h, "b")
			client := models.New(h.Querier, models.WithQueryHook(tc.hook))
			rows, errs, panicked := strRangeGuarded(client.Products().Stream(ctx, nil), tc.breakEarly)
			// When the consumer breaks out, strRangeGuarded stops ranging after the
			// first yield, so it cannot see a later yield as an error. The hook's
			// error is yielded after the chain returns, outside its panic recovery,
			// so yielding it after the break makes the Go runtime panic: panicked is
			// the oracle for the break case.
			if panicked != nil {
				t.Fatalf("ranging over Stream panicked: %v", panicked)
			}
			if rows != tc.wantRows {
				t.Errorf("Stream yielded %d rows, want %d", rows, tc.wantRows)
			}
			if tc.breakEarly {
				return
			}
			if len(errs) != 1 || !tc.check(errs[0]) {
				t.Errorf("Stream yielded errors %v, want exactly one: %s", errs, tc.want)
			}
		})
	}
}

// strCollect drains a stream, keeping every yielded entity and every error.
func strCollect[E any](seq func(func(E, error) bool)) ([]E, []error) {
	var (
		rows []E
		errs []error
	)
	for e, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rows = append(rows, e)
	}
	return rows, errs
}

// strRangeGuarded ranges over a product stream, breaking after the first row
// when breakEarly is set, and reports what it saw and any panic the range
// raised (a stream that yields after the loop body returned false panics).
// After a yielded error it stops counting rows as before the error.
func strRangeGuarded(seq func(func(*models.Product, error) bool), breakEarly bool) (rows int, errs []error, panicked any) {
	defer func() { panicked = recover() }()
	for p, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if p != nil && len(errs) == 0 {
			rows++
		}
		if breakEarly {
			break
		}
	}
	return rows, errs, nil
}

func strSortedIDs(ps []*models.Product) []int64 {
	ids := make([]int64, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	slices.Sort(ids)
	return ids
}

func strSortedNames(ps []*models.Product) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	slices.Sort(names)
	return names
}

// strFailingQuerier hands out result sets that fail after `after` rows, the
// way a driver reports a connection lost mid-stream: Next returns false and
// Err returns the failure.
type strFailingQuerier struct {
	database.Querier
	after int
	err   error
}

func (q *strFailingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	rows, err := q.Querier.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &strFailingRows{Rows: rows, left: q.after, err: q.err}, nil
}

type strFailingRows struct {
	database.Rows
	left   int
	err    error
	failed bool
}

func (r *strFailingRows) Next() bool {
	if r.left == 0 {
		r.failed = true
		return false
	}
	r.left--
	return r.Rows.Next()
}

func (r *strFailingRows) Err() error {
	if r.failed {
		return r.err
	}
	return r.Rows.Err()
}
