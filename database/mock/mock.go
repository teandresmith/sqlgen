// Package mock provides a configurable mock Querier for unit testing.
// It implements the database.Querier interface with function fields that
// can be set to control behavior in tests. All methods return reasonable
// zero values when their corresponding function fields are nil.
package mock

import (
	"context"
	"reflect"

	"github.com/teandresmith/sqlgen/database"
)

// Compile-time interface checks.
var (
	_ database.Querier       = (*Querier)(nil)
	_ database.SessionPinner = (*Querier)(nil)
	_ database.Result        = (*Result)(nil)
	_ database.Row           = (*Row)(nil)
	_ database.Rows          = (*Rows)(nil)
)

// Querier is a configurable mock implementation of database.Querier.
// Set the function fields to control the return values of each method.
// Unset fields return zero values without error.
type Querier struct {
	ExecFn     func(ctx context.Context, sql string, arguments ...any) (database.Result, error)
	QueryFn    func(ctx context.Context, sql string, args ...any) (database.Rows, error)
	QueryRowFn func(ctx context.Context, sql string, args ...any) database.Row
	BeginFn    func(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error)
}

// New returns a new Querier with all function fields unset.
func New() *Querier {
	return &Querier{}
}

// Exec calls ExecFn if set, otherwise returns an empty Result and nil error.
func (q *Querier) Exec(ctx context.Context, sql string, arguments ...any) (database.Result, error) {
	if q.ExecFn != nil {
		return q.ExecFn(ctx, sql, arguments...)
	}
	return &Result{}, nil
}

// Query calls QueryFn if set, otherwise returns an empty Rows and nil error.
func (q *Querier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	if q.QueryFn != nil {
		return q.QueryFn(ctx, sql, args...)
	}
	return &Rows{}, nil
}

// QueryRow calls QueryRowFn if set, otherwise returns an empty Row.
func (q *Querier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	if q.QueryRowFn != nil {
		return q.QueryRowFn(ctx, sql, args...)
	}
	return &Row{}
}

// Begin calls BeginFn if set, otherwise returns nil Tx and nil error.
func (q *Querier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	if q.BeginFn != nil {
		return q.BeginFn(ctx, name, opts...)
	}
	return nil, nil
}

// WithSession calls fn with q itself (database.SessionPinner): a mock has no
// pool, so every statement already runs on the one session there is.
func (q *Querier) WithSession(_ context.Context, fn func(session database.Querier) error) error {
	return fn(q)
}

// Result is a configurable mock implementation of database.Result.
type Result struct {
	RowsAffectedFn func() (int64, error)
	LastInsertIdFn func() (int64, error)
}

// RowsAffected calls RowsAffectedFn if set, otherwise returns 0 and nil error.
func (r *Result) RowsAffected() (int64, error) {
	if r.RowsAffectedFn != nil {
		return r.RowsAffectedFn()
	}
	return 0, nil
}

// LastInsertId calls LastInsertIdFn if set, otherwise returns 0 and nil error.
func (r *Result) LastInsertId() (int64, error) {
	if r.LastInsertIdFn != nil {
		return r.LastInsertIdFn()
	}
	return 0, nil
}

// Row is a configurable mock implementation of database.Row.
// Set Values for simple scan-by-position, or ScanFn for custom behavior.
type Row struct {
	// Values holds the values to copy into scan destinations.
	// When Scan is called, each value is copied to the corresponding
	// destination pointer using reflection.
	Values []any

	// ScanFn overrides the default Values-based scanning.
	ScanFn func(dest ...any) error
}

// NewRow returns a Row with the given values pre-loaded for scanning.
func NewRow(values ...any) *Row {
	return &Row{Values: values}
}

// Scan calls ScanFn if set. Otherwise, it copies Values into the destination
// pointers using reflection.
func (r *Row) Scan(dest ...any) error {
	if r.ScanFn != nil {
		return r.ScanFn(dest...)
	}
	for i, v := range r.Values {
		if i >= len(dest) {
			break
		}
		dp := reflect.ValueOf(dest[i])
		if dp.Kind() != reflect.Pointer || dp.IsNil() {
			continue
		}
		dp.Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// Rows is a configurable mock implementation of database.Rows.
type Rows struct {
	NextFn    func() bool
	ScanFn    func(dest ...any) error
	ColumnsFn func() ([]string, error)
	CloseFn   func() error
	ErrFn     func() error
}

// Next calls NextFn if set, otherwise returns false.
func (r *Rows) Next() bool {
	if r.NextFn != nil {
		return r.NextFn()
	}
	return false
}

// Scan calls ScanFn if set, otherwise returns nil.
func (r *Rows) Scan(dest ...any) error {
	if r.ScanFn != nil {
		return r.ScanFn(dest...)
	}
	return nil
}

// Columns calls ColumnsFn if set, otherwise returns nil and nil error.
func (r *Rows) Columns() ([]string, error) {
	if r.ColumnsFn != nil {
		return r.ColumnsFn()
	}
	return nil, nil
}

// Close calls CloseFn if set, otherwise returns nil.
func (r *Rows) Close() error {
	if r.CloseFn != nil {
		return r.CloseFn()
	}
	return nil
}

// Err calls ErrFn if set, otherwise returns nil.
func (r *Rows) Err() error {
	if r.ErrFn != nil {
		return r.ErrFn()
	}
	return nil
}
