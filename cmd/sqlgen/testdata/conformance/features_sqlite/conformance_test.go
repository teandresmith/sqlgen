package featuressqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
)

func TestConformance(t *testing.T) {
	// A pragma in the DSN applies to every pooled connection, not only the first.
	const dsn = "file:features?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, file := range []string{"schema.sql", "views/active_tasks.sql"} {
		ddl, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(ddl)); err != nil {
			t.Fatal(err)
		}
	}
	var version string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	reset := func(t *testing.T) {
		if _, err := db.Exec("DELETE FROM template_tags; DELETE FROM template_notes; DELETE FROM templates; DELETE FROM board_tags; DELETE FROM cards; DELETE FROM tags; DELETE FROM boards; DELETE FROM tasks; DELETE FROM memberships; DELETE FROM notes; DELETE FROM bulletins; DELETE FROM regions"); err != nil {
			t.Fatal(err)
		}
	}
	q := &featuressuite.CountingQuerier{Inner: dbstdlib.New(db)}
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "sqlite",
		Driver:   "stdlib",
		Database: "SQLite " + version,
		Querier:  q,
		Base:     q,
		Caps:     featuressuite.Capabilities{RowLocks: false},
		Reset:    reset,
	})

	t.Run("RoundTrips", func(t *testing.T) {
		rec, err := roundtrip.NewRecorder()
		if err != nil {
			t.Fatal(err)
		}
		// The same database, through a pool whose driver reports every
		// statement to rec.
		rtdb, err := roundtrip.OpenDB(rec, "sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rtdb.Close() })
		if err := roundtrip.Warm(context.Background(), rtdb, 8); err != nil {
			t.Fatal(err)
		}
		rq := &featuressuite.CountingQuerier{Inner: dbstdlib.New(rtdb)}
		featuressuite.RunRoundTrips(t, &featuressuite.Harness{
			Dialect: "sqlite",
			Driver:  "stdlib",
			Querier: rq,
			Base:    rq.Inner,
			Caps:    featuressuite.Capabilities{RowLocks: false},
			Reset:   reset,
		}, rec)
	})
}
