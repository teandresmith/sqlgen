package roundtrip

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
)

// OpenDB opens a database/sql pool on the registered driver driverName whose
// connections report every statement to r: statements sent inside a
// transaction and on a pinned session included, BEGIN, COMMIT and ROLLBACK
// excluded because the driver sends them itself. Each statement is counted
// once however the driver executes it; a driver that prepares a parameterized
// statement before executing it (go-sql-driver/mysql without
// interpolateParams) is recorded at the execution.
func OpenDB(r *Recorder, driverName, dsn string) (*sql.DB, error) {
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driverName, err)
	}
	drv := probe.Driver()
	if err := probe.Close(); err != nil {
		return nil, fmt.Errorf("close the %s probe: %w", driverName, err)
	}
	var inner driver.Connector = dsnConnector{drv: drv, dsn: dsn}
	if dc, ok := drv.(driver.DriverContext); ok {
		if inner, err = dc.OpenConnector(dsn); err != nil {
			return nil, fmt.Errorf("open a %s connector: %w", driverName, err)
		}
	}
	return sql.OpenDB(&connector{inner: inner, r: r}), nil
}

// Warm opens n connections at once and returns them to db's idle pool, so a
// measured operation that fans out does not wait on a connection handshake.
// It raises db's idle limit to n.
func Warm(ctx context.Context, db *sql.DB, n int) error {
	db.SetMaxIdleConns(n)
	conns := make([]*sql.Conn, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			c, err := db.Conn(ctx)
			if err == nil {
				err = c.PingContext(ctx)
			}
			conns[i], errs[i] = c, err
		})
	}
	wg.Wait()
	for _, c := range conns {
		if c != nil {
			errs = append(errs, c.Close())
		}
	}
	return errors.Join(errs...)
}

type dsnConnector struct {
	drv driver.Driver
	dsn string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.drv }

type connector struct {
	inner driver.Connector
	r     *Recorder
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	ic, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &conn{inner: ic, r: c.r}, nil
}

func (c *connector) Driver() driver.Driver { return c.inner.Driver() }

// conn reports each statement sent on inner. It implements every optional
// connection interface database/sql looks for, answering as database/sql
// would without it when inner lacks one.
type conn struct {
	inner driver.Conn
	r     *Recorder
}

var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
)

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var (
		s   driver.Stmt
		err error
	)
	if pc, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err = pc.PrepareContext(ctx, query)
	} else {
		s, err = c.inner.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &stmt{inner: s, conn: c, query: query}, nil
}

func (c *conn) Close() error { return c.inner.Close() }

func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.r.BeganTx()
	if bt, ok := c.inner.(driver.ConnBeginTx); ok {
		return bt.BeginTx(ctx, opts)
	}
	if opts != (driver.TxOptions{}) {
		return nil, errors.New("roundtrip: the driver does not support transaction options")
	}
	return c.inner.Begin() //nolint:staticcheck // the fallback database/sql itself takes for a driver without ConnBeginTx
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return record(c.r, query, func() (driver.Result, error) { return ec.ExecContext(ctx, query, args) })
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return record(c.r, query, func() (driver.Rows, error) { return qc.QueryContext(ctx, query, args) })
}

func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := c.inner.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (c *conn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *conn) ResetSession(ctx context.Context) error {
	if sr, ok := c.inner.(driver.SessionResetter); ok {
		return sr.ResetSession(ctx)
	}
	return nil
}

func (c *conn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// record runs send and records query once it has run. A driver.ErrSkip
// means the driver did not send it: database/sql falls back to preparing the
// statement, and the execution is recorded there.
func record[T any](r *Recorder, query string, send func() (T, error)) (T, error) {
	done := r.Statement(query)
	res, err := send()
	if errors.Is(err, driver.ErrSkip) {
		return res, err
	}
	done()
	return res, err
}

// stmt reports each execution of a prepared statement.
type stmt struct {
	inner driver.Stmt
	conn  *conn
	query string
}

var (
	_ driver.StmtExecContext   = (*stmt)(nil)
	_ driver.StmtQueryContext  = (*stmt)(nil)
	_ driver.NamedValueChecker = (*stmt)(nil)
)

func (s *stmt) Close() error  { return s.inner.Close() }
func (s *stmt) NumInput() int { return s.inner.NumInput() }

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return record(s.conn.r, s.query, func() (driver.Result, error) { return s.inner.Exec(args) }) //nolint:staticcheck // the fallback for a driver without StmtExecContext
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return record(s.conn.r, s.query, func() (driver.Rows, error) { return s.inner.Query(args) }) //nolint:staticcheck // the fallback for a driver without StmtQueryContext
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := s.inner.(driver.StmtExecContext); ok {
		return record(s.conn.r, s.query, func() (driver.Result, error) { return ec.ExecContext(ctx, args) })
	}
	values, err := plainValues(args)
	if err != nil {
		return nil, err
	}
	return s.Exec(values)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := s.inner.(driver.StmtQueryContext); ok {
		return record(s.conn.r, s.query, func() (driver.Rows, error) { return qc.QueryContext(ctx, args) })
	}
	values, err := plainValues(args)
	if err != nil {
		return nil, err
	}
	return s.Query(values)
}

// CheckNamedValue asks the statement, then the connection, as database/sql
// does when the statement does not implement it.
func (s *stmt) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := s.inner.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return s.conn.CheckNamedValue(nv)
}

func plainValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, a := range args {
		if a.Name != "" {
			return nil, errors.New("roundtrip: the driver does not support named parameters")
		}
		values[i] = a.Value
	}
	return values, nil
}
