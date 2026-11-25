package mysql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/suite"
)

func TestConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker")
	}
	ctx := context.Background()
	ctr, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("conformance"),
		tcmysql.WithUsername("test"),
		tcmysql.WithPassword("test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// The driver runs one statement per call unless the DSN allows several.
	exec := func(t *testing.T, stmts ...string) {
		t.Helper()
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, file := range []string{"schema.sql", "views/premium_products.sql"} {
		ddl, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for stmt := range strings.SplitSeq(string(ddl), ";") {
			if body := stripComments(stmt); body != "" {
				exec(t, body)
			}
		}
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	reset := func(t *testing.T) {
		// TRUNCATE refuses a table another table's foreign key references.
		exec(t, "DELETE FROM crate_seals", "DELETE FROM seals", "DELETE FROM crate_items", "DELETE FROM crates", "DELETE FROM item_stations", "DELETE FROM stations", "DELETE FROM parcels",
			"DELETE FROM shelves", "DELETE FROM item_receipts", "DELETE FROM item_notes",
			"DELETE FROM order_items", "DELETE FROM products", "DELETE FROM documents",
			"DELETE FROM revisions", "DELETE FROM labels", "DELETE FROM counters",
			"DELETE FROM author_tags", "DELETE FROM posts", "DELETE FROM author_profiles",
			"DELETE FROM tags", "DELETE FROM authors")
		exec(t, "DELETE FROM van_permits", "DELETE FROM van_tools", "DELETE FROM permits", "DELETE FROM tools",
			"DELETE FROM slots", "DELETE FROM lockers", "DELETE FROM bays", "DELETE FROM vans", "DELETE FROM routes", "DELETE FROM depots")
	}
	q := &suite.CountingQuerier{Inner: dbstdlib.New(db)}
	suite.Run(t, &suite.Harness{
		Dialect:  "mysql",
		Driver:   "stdlib",
		Database: "MySQL " + version,
		Client:   models.New(q),
		Querier:  q,
		Base:     q,
		Caps:     suite.Capabilities{RowLocks: true, CompositeLoadChunk: 16383, CaseInsensitiveText: true},
		Reset:    reset,
	})

	t.Run("RoundTrips", func(t *testing.T) {
		rec, err := roundtrip.NewRecorder()
		if err != nil {
			t.Fatal(err)
		}
		// The same database, through a pool whose driver reports every
		// statement to rec.
		rtdb, err := roundtrip.OpenDB(rec, "mysql", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rtdb.Close() })
		if err := roundtrip.Warm(ctx, rtdb, 8); err != nil {
			t.Fatal(err)
		}
		rq := &suite.CountingQuerier{Inner: dbstdlib.New(rtdb)}
		suite.RunRoundTrips(t, &suite.Harness{
			Dialect: "mysql",
			Driver:  "stdlib",
			Client:  models.New(rq.Inner),
			Querier: rq,
			Base:    rq.Inner,
			Caps:    suite.Capabilities{RowLocks: true, CompositeLoadChunk: 16383, CaseInsensitiveText: true},
			Reset:   reset,
		}, rec)
	})
}

// stripComments drops the -- comment lines of a schema statement.
func stripComments(stmt string) string {
	var lines []string
	for line := range strings.Lines(stmt) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, ""))
}
