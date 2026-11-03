package stdlib

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"

	"github.com/go-sql-driver/mysql"
	"github.com/teandresmith/sqlgen/database"
)

// MapError translates driver-specific errors from database/sql drivers into
// sqlgen error types. It checks for lib/pq, go-sql-driver/mysql, and
// modernc/sqlite error types in order, then for a transport failure (a failed
// connect or a broken connection), which maps to database.ErrConnectionFailed.
// Without the call's context it cannot tell a driver's connect timeout from
// the caller's deadline, so it maps neither; the adapter's own calls can.
// Returns (true, mapped error) when the error was recognized, or
// (false, original error) when it was not.
func MapError(err error) (bool, error) {
	return mapError(err, callerUnknown)
}

// mapErrorCtx maps err from a call made on ctx. While ctx is live, a
// transport failure maps to database.ErrConnectionFailed even when it looks
// like a deadline, because the deadline was the driver's (a connect timeout);
// once ctx has ended, the caller's deadline or cancellation is the cause and
// no transport failure is mapped.
func mapErrorCtx(ctx context.Context, err error) (bool, error) {
	if ctx.Err() != nil {
		return mapError(err, callerEnded)
	}
	return mapError(err, callerLive)
}

// callerState is what is known about the context of the call an error came
// from.
type callerState int

const (
	callerUnknown callerState = iota // MapError: no context to consult
	callerLive                       // the call's context had not ended
	callerEnded                      // the call's context had ended
)

// mapError implements MapError and mapErrorCtx.
func mapError(err error, caller callerState) (bool, error) {
	if err == nil {
		return false, nil
	}
	if ok, mapped := mapPqError(err); ok {
		return true, mapped
	}
	if ok, mapped := mapMySQLError(err); ok {
		return true, mapped
	}
	if ok, mapped := mapModerncSQLiteError(err); ok {
		return true, mapped
	}
	if isTransportError(err, caller) {
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
	}
	return false, err
}

// isTransportError reports whether err is a failure to reach the database or
// to keep the connection, rather than an error the server sent: a refused or
// failed dial, a broken socket, or a connection the driver has given up on.
// The caller's own deadline or cancellation is not a connection failure: once
// the call's context has ended nothing is, and when it is unknown an error that
// looks like a deadline or cancellation is taken to be the caller's.
func isTransportError(err error, caller callerState) bool {
	switch caller {
	case callerEnded:
		return false
	case callerUnknown:
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
	case callerLive:
		// Any transport failure counts, a driver's connect timeout included.
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn)
}
