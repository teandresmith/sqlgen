package models

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/mock"
	"github.com/teandresmith/sqlgen/sql"
)

// fakeRow scans a configured string into the first scan target, mimicking
// the QueryRow result for "SELECT VERSION()".
type fakeRow struct {
	value string
	err   error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 0 {
		return errors.New("fakeRow.Scan: no destination")
	}
	target, ok := dest[0].(*string)
	if !ok {
		return errors.New("fakeRow.Scan: destination is not *string")
	}
	*target = r.value
	return nil
}

// versionQuerier returns a mock.Querier whose QueryRow handler responds to
// "SELECT VERSION()" with the configured version string. Other queries return
// an error so a stray SQL call surfaces immediately. The queryRowCount lets
// tests assert sync.Once is honoured (the probe runs once across calls).
func versionQuerier(version string, queryRowCount *int32) *mock.Querier {
	q := mock.New()
	q.QueryRowFn = func(ctx context.Context, sql string, args ...any) database.Row {
		if queryRowCount != nil {
			atomic.AddInt32(queryRowCount, 1)
		}
		if strings.Contains(sql, "VERSION()") {
			return &fakeRow{value: version}
		}
		return &fakeRow{err: errors.New("unexpected QueryRow: " + sql)}
	}
	return q
}

// TestMySQLVersionCache_parsesMajorVersion exercises the version probe across
// the canonical MySQL version-string shapes — the bare 8.x release, the 5.7
// pre-8.0 path that triggers the NoWait/SkipLocked rejection at the runtime
// guard, and the suffixed `-log` variant emitted by some Linux distributions.
func TestMySQLVersionCache_parsesMajorVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantMajor   int
		wantRaw     string
		wantMariaDB bool
	}{
		{name: "mysql_8.0.34", version: "8.0.34", wantMajor: 8, wantRaw: "8.0.34"},
		{name: "mysql_5.7.40", version: "5.7.40", wantMajor: 5, wantRaw: "5.7.40"},
		{name: "mysql_8.0.34-log", version: "8.0.34-log", wantMajor: 8, wantRaw: "8.0.34-log"},
		{name: "mariadb_10.11.6", version: "10.11.6-MariaDB", wantMajor: 10, wantRaw: "10.11.6-MariaDB", wantMariaDB: true},
		{name: "mariadb_with_distro_suffix", version: "10.6.12-MariaDB-0ubuntu0.22.04.1", wantMajor: 10, wantRaw: "10.6.12-MariaDB-0ubuntu0.22.04.1", wantMariaDB: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := newMySQLVersionCache(versionQuerier(tc.version, nil))
			got, err := cache.get(context.Background())
			if err != nil {
				t.Fatalf("get(): unexpected error: %v", err)
			}
			if got.Major != tc.wantMajor {
				t.Errorf("Major = %d, want %d", got.Major, tc.wantMajor)
			}
			if got.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q", got.Raw, tc.wantRaw)
			}
			if got.MariaDB != tc.wantMariaDB {
				t.Errorf("MariaDB = %v, want %v", got.MariaDB, tc.wantMariaDB)
			}
		})
	}
}

// TestMySQLVersionCache_malformedVersion verifies the cache surfaces a parse
// error so the runtime guard can wrap it into the per-method error context
// instead of silently treating an unparseable version as Major=0.
func TestMySQLVersionCache_malformedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "no_separator", version: "garbage"},
		{name: "empty", version: ""},
		{name: "leading_dot", version: ".5.0"},
		{name: "non_numeric_major", version: "abc.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := newMySQLVersionCache(versionQuerier(tc.version, nil))
			_, err := cache.get(context.Background())
			if err == nil {
				t.Fatalf("get(): want error for malformed version %q, got nil", tc.version)
			}
			if !strings.Contains(err.Error(), "mysql version") {
				t.Errorf("error %q must mention 'mysql version' for diagnosability", err.Error())
			}
		})
	}
}

// TestMySQLVersionCache_scanErrorPropagated checks that a QueryRow.Scan
// failure (e.g. driver disconnect) surfaces from get() rather than being
// swallowed and replaced with a fabricated zero value.
func TestMySQLVersionCache_scanErrorPropagated(t *testing.T) {
	q := mock.New()
	scanErr := errors.New("connection lost")
	q.QueryRowFn = func(ctx context.Context, sql string, args ...any) database.Row {
		return &fakeRow{err: scanErr}
	}
	cache := newMySQLVersionCache(q)
	_, err := cache.get(context.Background())
	if err == nil {
		t.Fatal("get(): want error from underlying Scan, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("get() error = %v, want wrapped %v", err, scanErr)
	}
}

// TestMySQLVersionCache_singleProbe pins the sync.Once contract: the probe
// runs at most once across the lifetime of the cache, even under repeated
// calls and even when the underlying Querier is reused. The runtime guard
// fires the cache for every Get/GetMany/Connection that requests a version-
// gated mode, so a regression that rebuilt the cache per call would silently
// quintuple the round-trip cost on hot paths.
func TestMySQLVersionCache_singleProbe(t *testing.T) {
	var calls int32
	cache := newMySQLVersionCache(versionQuerier("8.0.34", &calls))
	for range 5 {
		if _, err := cache.get(context.Background()); err != nil {
			t.Fatalf("get(): unexpected error: %v", err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("QueryRow invoked %d times, want 1 (sync.Once must amortize the probe)", got)
	}
}

// TestMySQLVersionCache_errorCachedAcrossCalls verifies a probe failure is
// remembered for subsequent get() calls. Re-running SELECT VERSION() after a
// transient failure could mask the original symptom — the runtime guard
// expects a stable verdict for the lifetime of the client.
func TestMySQLVersionCache_errorCachedAcrossCalls(t *testing.T) {
	cache := newMySQLVersionCache(versionQuerier("garbage", nil))
	_, err1 := cache.get(context.Background())
	if err1 == nil {
		t.Fatal("first get(): want parse error, got nil")
	}
	_, err2 := cache.get(context.Background())
	if err2 == nil {
		t.Fatal("second get(): want cached error, got nil")
	}
	if err1.Error() != err2.Error() {
		t.Errorf("error not stable across calls; first=%q second=%q", err1.Error(), err2.Error())
	}
}

// recordingTxConn is the connection behind a mock transaction. It records every
// statement the transaction is asked to run, so a test can tell a guard that
// rejected before SQL from one that let the read through.
type recordingTxConn struct {
	*mock.Querier
	statements []string
}

func newRecordingTxConn() *recordingTxConn {
	c := &recordingTxConn{Querier: mock.New()}
	c.QueryFn = func(ctx context.Context, sql string, args ...any) (database.Rows, error) {
		c.statements = append(c.statements, sql)
		return &mock.Rows{}, nil
	}
	c.QueryRowFn = func(ctx context.Context, sql string, args ...any) database.Row {
		c.statements = append(c.statements, sql)
		return &mock.Row{}
	}
	return c
}

func (c *recordingTxConn) Commit(context.Context) error   { return nil }
func (c *recordingTxConn) Rollback(context.Context) error { return nil }

// TestLockModeGuard_oneToOneJoinVersion pins the version guard on a locked read
// that selects a one-to-one relationship. That read is a LEFT JOIN whose lock
// names only the parent alias (FOR UPDATE OF u), so the joined profile row is
// not locked; MySQL accepts the OF form from 8.0 and MariaDB not at all, so
// every mode is rejected there before any SQL. A read that selects no
// one-to-one relationship keeps the unqualified clause: it needs no probe for
// FOR UPDATE / LOCK IN SHARE MODE, and MariaDB still clears the NOWAIT /
// SKIP LOCKED check.
func TestLockModeGuard_oneToOneJoinVersion(t *testing.T) {
	withProfile := &UserFieldOptions{ID: true, Profile: &ProfileFieldOptions{ID: true}}
	plain := &UserFieldOptions{ID: true}
	getMany := func(ctx context.Context, c *Client, opts func(*CallOptions[UserFieldOptions])) error {
		_, err := c.Users().GetMany(ctx, &GetUsersInput{}, opts)
		return err
	}
	get := func(ctx context.Context, c *Client, opts func(*CallOptions[UserFieldOptions])) error {
		_, err := c.Users().Get(ctx, 1, opts)
		return err
	}
	paginate := func(ctx context.Context, c *Client, opts func(*CallOptions[UserFieldOptions])) error {
		_, err := c.Users().Paginate(ctx, PaginateInput[UserFilter]{Limit: 10}, opts)
		return err
	}
	const rejected = "with a one-to-one relationship selected requires MySQL 8.0+, not MariaDB"
	tests := []struct {
		name      string
		version   string
		mode      sql.LockMode
		fields    *UserFieldOptions
		call      func(context.Context, *Client, func(*CallOptions[UserFieldOptions])) error
		wantErr   string // substring; empty means the read reaches SQL
		wantProbe int32
		wantLock  string // suffix of the one statement the read issues
	}{
		{name: "mysql 5.7 rejects for_update through the join", version: "5.7.40", mode: sql.LockForUpdate, fields: withProfile, call: getMany, wantErr: "get many users: LockMode for_update " + rejected + " (server reports 5.7.40)", wantProbe: 1},
		{name: "mysql 5.7 rejects for_share through the join", version: "5.7.40", mode: sql.LockForShare, fields: withProfile, call: getMany, wantErr: "get many users: LockMode for_share " + rejected, wantProbe: 1},
		{name: "mariadb rejects for_update through the join", version: "10.11.6-MariaDB", mode: sql.LockForUpdate, fields: withProfile, call: getMany, wantErr: "get many users: LockMode for_update " + rejected + " (server reports 10.11.6-MariaDB)", wantProbe: 1},
		{name: "mariadb rejects for_share through the join", version: "10.11.6-MariaDB", mode: sql.LockForShare, fields: withProfile, call: getMany, wantErr: rejected, wantProbe: 1},
		{name: "mariadb rejects nowait through the join", version: "10.11.6-MariaDB", mode: sql.LockForUpdateNoWait, fields: withProfile, call: getMany, wantErr: rejected, wantProbe: 1},
		{name: "mariadb rejects skip_locked through the join", version: "10.11.6-MariaDB", mode: sql.LockForUpdateSkipLocked, fields: withProfile, call: getMany, wantErr: rejected, wantProbe: 1},
		{name: "get rejects through the join before its hooks", version: "10.11.6-MariaDB", mode: sql.LockForUpdate, fields: withProfile, call: get, wantErr: "get user: LockMode for_update " + rejected, wantProbe: 1},
		{name: "paginate rejects through the join before its count", version: "5.7.40", mode: sql.LockForUpdate, fields: withProfile, call: paginate, wantErr: "paginate users: LockMode for_update " + rejected, wantProbe: 1},
		{name: "mysql 8.0 locks only the parent alias", version: "8.0.34", mode: sql.LockForUpdate, fields: withProfile, call: getMany, wantProbe: 1, wantLock: " FOR UPDATE OF u"},
		{name: "mysql 8.0 share-locks only the parent alias", version: "8.0.34", mode: sql.LockForShare, fields: withProfile, call: getMany, wantProbe: 1, wantLock: " FOR SHARE OF u"},
		{name: "mysql 8.0 skip_locked names the parent alias", version: "8.0.34", mode: sql.LockForUpdateSkipLocked, fields: withProfile, call: getMany, wantProbe: 1, wantLock: " FOR UPDATE OF u SKIP LOCKED"},
		{name: "mysql 5.7 for_update without the join needs no probe", version: "5.7.40", mode: sql.LockForUpdate, fields: plain, call: getMany, wantLock: " FOR UPDATE"},
		{name: "mysql 5.7 for_share without the join needs no probe", version: "5.7.40", mode: sql.LockForShare, fields: nil, call: getMany, wantLock: " LOCK IN SHARE MODE"},
		{name: "mariadb nowait without the join keeps the unqualified clause", version: "10.11.6-MariaDB", mode: sql.LockForUpdateNoWait, fields: plain, call: getMany, wantProbe: 1, wantLock: " FOR UPDATE NOWAIT"},
		{name: "mariadb for_update without the join needs no probe", version: "10.11.6-MariaDB", mode: sql.LockForUpdate, fields: plain, call: getMany, wantLock: " FOR UPDATE"},
		{name: "mariadb unlocked read through the join needs no probe", version: "10.11.6-MariaDB", mode: sql.LockNone, fields: withProfile, call: getMany},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var probes int32
			pool := versionQuerier(tc.version, &probes)
			conn := newRecordingTxConn()
			pool.BeginFn = func(ctx context.Context, name string, _ ...database.TxOptions) (*database.Tx, error) {
				return database.NewTx(conn, name, ""), nil
			}
			client := New(pool)
			txCtx, err := database.NewTransaction(context.Background(), pool, "lock")
			if err != nil {
				t.Fatalf("NewTransaction: %v", err)
			}

			err = tc.call(txCtx, client, func(o *CallOptions[UserFieldOptions]) {
				o.LockMode = tc.mode
				o.FieldOptions = tc.fields
			})

			if got := atomic.LoadInt32(&probes); got != tc.wantProbe {
				t.Errorf("SELECT VERSION() probes = %d, want %d", got, tc.wantProbe)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				if len(conn.statements) != 0 {
					t.Errorf("the transaction ran %q, want no SQL before the rejection", conn.statements)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v, want the read to reach SQL", err)
			}
			if len(conn.statements) != 1 {
				t.Fatalf("the transaction ran %q, want exactly one SELECT", conn.statements)
			}
			stmt := conn.statements[0]
			if tc.wantLock == "" {
				for _, clause := range []string{"FOR UPDATE", "FOR SHARE", "LOCK IN SHARE MODE"} {
					if strings.Contains(stmt, clause) {
						t.Errorf("statement %q carries %s, want no lock clause", stmt, clause)
					}
				}
				return
			}
			if !strings.HasSuffix(stmt, tc.wantLock) {
				t.Errorf("statement %q does not end with %q", stmt, tc.wantLock)
			}
			if strings.Contains(stmt, "LEFT JOIN") != (tc.fields != nil && tc.fields.Profile != nil) {
				t.Errorf("statement %q: LEFT JOIN presence does not match the selected relationships", stmt)
			}
		})
	}
}
