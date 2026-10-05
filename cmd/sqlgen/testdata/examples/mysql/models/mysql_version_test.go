package models

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teandresmith/sqlgen/database"
	"github.com/teandresmith/sqlgen/database/mock"
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
		name      string
		version   string
		wantMajor int
		wantRaw   string
	}{
		{name: "mysql_8.0.34", version: "8.0.34", wantMajor: 8, wantRaw: "8.0.34"},
		{name: "mysql_5.7.40", version: "5.7.40", wantMajor: 5, wantRaw: "5.7.40"},
		{name: "mysql_8.0.34-log", version: "8.0.34-log", wantMajor: 8, wantRaw: "8.0.34-log"},
		{name: "mariadb_10.11.6", version: "10.11.6-MariaDB", wantMajor: 10, wantRaw: "10.11.6-MariaDB"},
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
