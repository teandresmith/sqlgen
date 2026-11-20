package featuressuite

import (
	"context"
	"errors"
	"sync"

	"github.com/teandresmith/sqlgen/database"
)

// CountingQuerier records every statement sent through it.
type CountingQuerier struct {
	Inner database.Querier

	mu    sync.Mutex
	stmts []string
}

// Record records sql as sent. A driver-specific wrapper calls it for
// statements it sends without going through q, such as a pgx batch.
func (q *CountingQuerier) Record(sql string) {
	q.mu.Lock()
	q.stmts = append(q.stmts, sql)
	q.mu.Unlock()
}

// Statements returns the statements recorded since the last ResetCount.
func (q *CountingQuerier) Statements() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.stmts...)
}

// ResetCount clears the recorded statements.
func (q *CountingQuerier) ResetCount() {
	q.mu.Lock()
	q.stmts = nil
	q.mu.Unlock()
}

func (q *CountingQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	q.Record(sql)
	return q.Inner.Exec(ctx, sql, args...)
}

func (q *CountingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	q.Record(sql)
	return q.Inner.Query(ctx, sql, args...)
}

func (q *CountingQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	q.Record(sql)
	return q.Inner.QueryRow(ctx, sql, args...)
}

func (q *CountingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	return q.Inner.Begin(ctx, name, opts...)
}

// WithSession forwards to Inner when Inner is a database.SessionPinner,
// recording the statements fn issues on the session.
func (q *CountingQuerier) WithSession(ctx context.Context, fn func(session database.Querier) error) error {
	pinner, ok := q.Inner.(database.SessionPinner)
	if !ok {
		return errors.New("conformance: the inner querier cannot pin a session")
	}
	return pinner.WithSession(ctx, func(session database.Querier) error {
		return fn(&sessionQuerier{Querier: session, q: q})
	})
}

// sessionQuerier records on q the statements sent on one pinned session.
type sessionQuerier struct {
	database.Querier
	q *CountingQuerier
}

func (s *sessionQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	s.q.Record(sql)
	return s.Querier.Exec(ctx, sql, args...)
}

func (s *sessionQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	s.q.Record(sql)
	return s.Querier.Query(ctx, sql, args...)
}

func (s *sessionQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	s.q.Record(sql)
	return s.Querier.QueryRow(ctx, sql, args...)
}
