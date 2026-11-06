package sqlite

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/suite"
)

func TestConformance(t *testing.T) {
	// A pragma in the DSN applies to every pooled connection, not only the first.
	db, err := sql.Open("sqlite", "file:conformance?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	var version string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
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
		Reset: func(t *testing.T) {
			if _, err := db.Exec("DELETE FROM crate_items; DELETE FROM crates; DELETE FROM item_stations; DELETE FROM stations; DELETE FROM parcels; DELETE FROM shelves; DELETE FROM item_receipts; DELETE FROM item_notes; DELETE FROM order_items; DELETE FROM products; DELETE FROM documents; DELETE FROM revisions; DELETE FROM labels; DELETE FROM counters; DELETE FROM author_tags; DELETE FROM posts; DELETE FROM author_profiles; DELETE FROM tags; DELETE FROM authors"); err != nil {
				t.Fatal(err)
			}
		},
	})
}
