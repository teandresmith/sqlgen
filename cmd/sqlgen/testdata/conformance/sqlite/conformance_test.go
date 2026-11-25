package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/suite"
)

func TestConformance(t *testing.T) {
	// A pragma in the DSN applies to every pooled connection, not only the first.
	const dsn = "file:conformance?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, file := range []string{"schema.sql", "views/premium_products.sql"} {
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
		if _, err := db.Exec("DELETE FROM crate_seals; DELETE FROM seals; DELETE FROM crate_items; DELETE FROM crates; DELETE FROM item_stations; DELETE FROM stations; DELETE FROM parcels; DELETE FROM shelves; DELETE FROM item_receipts; DELETE FROM item_notes; DELETE FROM order_items; DELETE FROM products; DELETE FROM documents; DELETE FROM revisions; DELETE FROM labels; DELETE FROM counters; DELETE FROM author_tags; DELETE FROM posts; DELETE FROM author_profiles; DELETE FROM tags; DELETE FROM authors"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("DELETE FROM van_permits; DELETE FROM van_tools; DELETE FROM permits; DELETE FROM tools; DELETE FROM slots; DELETE FROM lockers; DELETE FROM bays; DELETE FROM vans; DELETE FROM routes; DELETE FROM depots"); err != nil {
			t.Fatal(err)
		}
	}
	q := &suite.CountingQuerier{Inner: dbstdlib.New(db)}
	suite.Run(t, &suite.Harness{
		Dialect:  "sqlite",
		Driver:   "stdlib",
		Database: "SQLite " + version,
		Client:   models.New(q),
		Querier:  q,
		Base:     q,
		Caps:     suite.Capabilities{RowLocks: false, CompositeLoadChunk: 8191},
		Reset:    reset,
	})

	t.Run("RoundTrips", func(t *testing.T) {
		rec, err := roundtrip.NewRecorder()
		if err != nil {
			t.Fatal(err)
		}
		// The same in-memory database, through a pool whose driver reports
		// every statement to rec.
		rtdb, err := roundtrip.OpenDB(rec, "sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rtdb.Close() })
		if err := roundtrip.Warm(context.Background(), rtdb, 8); err != nil {
			t.Fatal(err)
		}
		rq := &suite.CountingQuerier{Inner: dbstdlib.New(rtdb)}
		suite.RunRoundTrips(t, &suite.Harness{
			Dialect: "sqlite",
			Driver:  "stdlib",
			Client:  models.New(rq.Inner),
			Querier: rq,
			Base:    rq.Inner,
			Caps:    suite.Capabilities{RowLocks: false, CompositeLoadChunk: 8191},
			Reset:   reset,
		}, rec)
	})
}
