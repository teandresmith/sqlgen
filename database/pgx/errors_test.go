package pgx_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teandresmith/sqlgen/database"
	pgxadapter "github.com/teandresmith/sqlgen/database/pgx"
)

func TestMapErrorConstraintCodes(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		constraintName string
		columnName     string
		detail         string
		wantType       database.ConstraintType
	}{
		{
			name:           "23505 unique violation",
			code:           "23505",
			constraintName: "users_email_key",
			columnName:     "email",
			detail:         "Key (email)=(test@test.com) already exists.",
			wantType:       database.ConstraintUnique,
		},
		{
			name:           "23503 foreign key violation",
			code:           "23503",
			constraintName: "orders_user_id_fkey",
			columnName:     "user_id",
			detail:         "Key (user_id)=(999) is not present in table \"users\".",
			wantType:       database.ConstraintForeignKey,
		},
		{
			name:           "23502 not null violation",
			code:           "23502",
			constraintName: "users_name_not_null",
			columnName:     "name",
			detail:         "Failing row contains (1, null, test@test.com).",
			wantType:       database.ConstraintNotNull,
		},
		{
			name:           "23514 check violation",
			code:           "23514",
			constraintName: "users_age_check",
			columnName:     "age",
			detail:         "Failing row contains (1, alice, -1).",
			wantType:       database.ConstraintCheck,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{
				Code:           tt.code,
				ConstraintName: tt.constraintName,
				ColumnName:     tt.columnName,
				Detail:         tt.detail,
			}

			ok, got := pgxadapter.MapError(pgErr)
			if !ok {
				t.Fatalf("MapError(%s) returned ok=false, want true", tt.code)
			}

			var ce *database.ConstraintError
			if !errors.As(got, &ce) {
				t.Fatalf("MapError(%s) did not return ConstraintError, got %T: %v", tt.code, got, got)
			}
			if ce.Type != tt.wantType {
				t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, tt.wantType)
			}
			if ce.Constraint != tt.constraintName {
				t.Errorf("ConstraintError.Constraint = %q, want %q", ce.Constraint, tt.constraintName)
			}
			if ce.Column != tt.columnName {
				t.Errorf("ConstraintError.Column = %q, want %q", ce.Column, tt.columnName)
			}
			if ce.Detail != tt.detail {
				t.Errorf("ConstraintError.Detail = %q, want %q", ce.Detail, tt.detail)
			}
			if !errors.Is(got, database.ErrConstraintViolation) {
				t.Error("errors.Is(mapped, ErrConstraintViolation) = false, want true")
			}
		})
	}
}

func TestMapErrorDeadlock(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "40P01"}

	ok, got := pgxadapter.MapError(pgErr)

	if !ok {
		t.Fatal("MapError(40P01) returned ok=false, want true")
	}
	if !errors.Is(got, database.ErrDeadlock) {
		t.Errorf("MapError(40P01) is not ErrDeadlock: %v", got)
	}
	if !errors.Is(got, pgErr) {
		t.Errorf("MapError(40P01) = %v, does not wrap the driver error %v", got, pgErr)
	}
}

func TestMapErrorConnectionFailed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "08000 connection exception", err: &pgconn.PgError{Code: "08000"}, want: true},
		{name: "08003 connection does not exist", err: &pgconn.PgError{Code: "08003"}, want: true},
		{name: "08006 connection failure", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "08001 unable to connect", err: &pgconn.PgError{Code: "08001"}, want: true},
		{name: "08P01 protocol violation", err: &pgconn.PgError{Code: "08P01"}, want: true},
		{name: "53300 too many connections at connect", err: fmt.Errorf("connect: %w", &pgconn.PgError{Code: "53300"}), want: true},
		{name: "28P01 a rejected password at connect is not a connection failure", err: fmt.Errorf("connect: %w", &pgconn.PgError{Code: "28P01"}), want: false},
		{name: "3D000 an unknown database at connect is not a connection failure", err: fmt.Errorf("connect: %w", &pgconn.PgError{Code: "3D000"}), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, got := pgxadapter.MapError(tt.err)

			if ok != tt.want || errors.Is(got, database.ErrConnectionFailed) != tt.want {
				t.Fatalf("MapError(%v) = (%v, %v), want recognized and ErrConnectionFailed = %v", tt.err, ok, got, tt.want)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("MapError(%v) = %v, want it to wrap the driver error", tt.err, got)
			}
		})
	}
}

func TestMapErrorUnrecognized(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42P01"} // undefined_table

	ok, got := pgxadapter.MapError(pgErr)

	if ok {
		t.Error("MapError(42P01) returned ok=true, want false for unrecognized code")
	}
	if got != error(pgErr) { //nolint:errorlint // identity: an unrecognized error is returned as-is
		t.Errorf("MapError(42P01) returned %v (%T), want the input error itself", got, got)
	}
}

func TestMapErrorNil(t *testing.T) {
	ok, got := pgxadapter.MapError(nil)
	if ok || got != nil {
		t.Errorf("MapError(nil) = (%v, %v), want (false, nil)", ok, got)
	}
}

func TestMapErrorNonPgError(t *testing.T) {
	in := errors.New("some other error")
	ok, got := pgxadapter.MapError(in)

	if ok {
		t.Error("MapError(non-pgx error) returned ok=true, want false")
	}
	if got != in { //nolint:errorlint // identity: an unrecognized error is returned as-is
		t.Errorf("MapError(non-pgx error) returned %v (%T), want the input error itself", got, got)
	}
}

func TestMapErrorWrappedPgError(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "users_email_key",
		ColumnName:     "email",
		Detail:         "Key (email)=(dup@test.com) already exists.",
	}
	wrapped := fmt.Errorf("some context: %w", pgErr)

	ok, got := pgxadapter.MapError(wrapped)
	if !ok {
		t.Fatal("MapError(wrapped PgError) returned ok=false, want true")
	}

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError(wrapped PgError) did not return ConstraintError, got %T: %v", got, got)
	}
	if ce.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", ce.Type, database.ConstraintUnique)
	}
	if ce.Err == nil {
		t.Error("ConstraintError.Err = nil, want non-nil")
	}
}

func TestMapErrorPreservesOriginalInErr(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "pk_test",
	}

	_, got := pgxadapter.MapError(pgErr)

	var ce *database.ConstraintError
	if !errors.As(got, &ce) {
		t.Fatalf("MapError did not return ConstraintError")
	}
	if !errors.Is(ce.Err, pgErr) {
		t.Errorf("ConstraintError.Err does not wrap original PgError")
	}
}

func TestMapErrorTransportFailures(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	// MapError has no context, so an error matching a context error is taken
	// to be the caller's.
	timeout := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "a broken socket read", err: fmt.Errorf("query: %w", reset), want: true},
		{name: "a context deadline is not a connection failure", err: fmt.Errorf("read: %w: %w", timeout, context.DeadlineExceeded), want: false},
		{name: "a context cancellation is not a connection failure", err: fmt.Errorf("read: %w: %w", timeout, context.Canceled), want: false},
		{name: "an unrecognized error is not a connection failure", err: errors.New("syntax error"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, mapped := pgxadapter.MapError(tt.err)
			if ok != tt.want || errors.Is(mapped, database.ErrConnectionFailed) != tt.want {
				t.Fatalf("MapError(%v) = (%v, %v), want recognized and ErrConnectionFailed = %v", tt.err, ok, mapped, tt.want)
			}
			if !errors.Is(mapped, tt.err) {
				t.Errorf("MapError(%v) = %v, want it to wrap the original error", tt.err, mapped)
			}
		})
	}
}

func TestMapErrorCtxTransportFailures(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	dialTimeout := realDialTimeout(t)
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "a driver's dial timeout while the caller waits is a connection failure", ctx: context.Background(), err: dialTimeout, want: true},
		{name: "a broken socket while the caller waits is a connection failure", ctx: context.Background(), err: reset, want: true},
		{name: "a broken socket after the caller's context ended is not a connection failure", ctx: ended, err: reset, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, mapped := pgxadapter.MapErrorCtx(tt.ctx, tt.err)
			if ok != tt.want || errors.Is(mapped, database.ErrConnectionFailed) != tt.want {
				t.Fatalf("mapErrorCtx(%v) = (%v, %v), want recognized and ErrConnectionFailed = %v", tt.err, ok, mapped, tt.want)
			}
			if !errors.Is(mapped, tt.err) {
				t.Errorf("mapErrorCtx(%v) = %v, want it to wrap the original error", tt.err, mapped)
			}
		})
	}
}

func TestRefusedConnectionIsConnectionFailed(t *testing.T) {
	ctx := context.Background()
	// Nothing listens on port 1, so the dial is refused.
	pool, err := pgxpool.New(ctx, "postgres://u:p@127.0.0.1:1/x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := pgxadapter.New(pool)

	t.Run("exec", func(t *testing.T) {
		_, err := q.Exec(ctx, "SELECT 1")
		assertRefusedIsConnectionFailed(t, err)
	})
	t.Run("begin", func(t *testing.T) {
		_, err := q.Begin(ctx, "refused")
		assertRefusedIsConnectionFailed(t, err)
	})
}

func assertRefusedIsConnectionFailed(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, database.ErrConnectionFailed) {
		t.Errorf("error = %v, want ErrConnectionFailed", err)
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) || !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("error = %v, want it to wrap the refused connect", err)
	}
}

func TestConnectTimeoutIsConnectionFailedOnlyWhileTheCallerWaits(t *testing.T) {
	addr := silentListener(t)

	t.Run("the driver's connect timeout is a connection failure", func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), "postgres://u:p@"+addr+"/x?connect_timeout=1")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)

		_, err = pgxadapter.New(pool).Exec(context.Background(), "SELECT 1")
		if !errors.Is(err, database.ErrConnectionFailed) {
			t.Errorf("Exec() error = %v, want ErrConnectionFailed", err)
		}
	})

	t.Run("the caller's deadline is not a connection failure", func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), "postgres://u:p@"+addr+"/x")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err = pgxadapter.New(pool).Exec(ctx, "SELECT 1")
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, database.ErrConnectionFailed) {
			t.Errorf("Exec() error = %v, want the caller's deadline and not ErrConnectionFailed", err)
		}
	})
}

// silentListener accepts TCP connections and never answers, so a client's
// connect handshake waits until a timeout ends it.
func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// realDialTimeout returns the error a dial that runs out of time reports. Like
// a driver's connect timeout, it matches context.DeadlineExceeded although no
// caller's context has ended.
func realDialTimeout(t *testing.T) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	d := net.Dialer{Timeout: time.Nanosecond}
	_, err = d.Dial("tcp", ln.Addr().String())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a 1ns dial returned %v, want a timeout matching context.DeadlineExceeded", err)
	}
	return err
}
