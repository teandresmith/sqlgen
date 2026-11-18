package pgx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/teandresmith/sqlgen/database"
)

// MapError translates pgx driver errors into sqlgen error types.
// It maps *pgconn.PgError to *database.ConstraintError for constraint
// violations, or to database.ErrDeadlock / database.ErrConnectionFailed
// for the corresponding PostgreSQL error classes, and a transport failure (a
// failed connect or a broken socket) to database.ErrConnectionFailed. Without
// the call's context it cannot tell a connect timeout from the caller's
// deadline, so it maps neither; the adapter's own calls can.
// Returns (true, mapped error) when the error was recognized, or
// (false, original error) when it was not.
func MapError(err error) (bool, error) {
	return mapError(err, callerUnknown)
}

// mapErrorCtx maps err from a call made on ctx. While ctx is live, a
// transport failure maps to database.ErrConnectionFailed even when it looks
// like a deadline, because the deadline was the driver's (connect_timeout);
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

// mapError implements MapError and mapErrorCtx. A server error is mapped by its code even when it
// arrived while connecting, so a rejected password is not a connection
// failure but too_many_connections is.
func mapError(err error, caller callerState) (bool, error) {
	if err == nil {
		return false, nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		if isTransportError(err, caller) {
			return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
		}
		return false, err
	}

	// Connection exception class (08xxx), and too_many_connections at connect
	if strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "53300" {
		return true, fmt.Errorf("%w: %w", database.ErrConnectionFailed, err)
	}

	switch pgErr.Code {
	case "23505": // unique_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintUnique,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23503": // foreign_key_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintForeignKey,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23502": // not_null_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintNotNull,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "23514": // check_violation
		return true, &database.ConstraintError{
			Type:       database.ConstraintCheck,
			Constraint: pgErr.ConstraintName,
			Column:     pgErr.ColumnName,
			Detail:     pgErr.Detail,
			Err:        err,
		}
	case "40P01": // deadlock_detected
		return true, fmt.Errorf("%w: %w", database.ErrDeadlock, err)
	default:
		return false, err
	}
}

// isTransportError reports whether err is a failure to reach the database or
// to keep the connection, rather than an error the server sent: a failed
// connect (dial or TLS), or a read or write on a broken socket. The caller's
// own deadline or cancellation is not a connection failure: once the call's
// context has ended nothing is, and when it is unknown an error that looks
// like a deadline or cancellation is taken to be the caller's.
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
	var connectErr *pgconn.ConnectError
	var opErr *net.OpError
	return errors.As(err, &connectErr) || errors.As(err, &opErr)
}
