package featuresmysql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
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
	for _, file := range []string{"schema.sql", "views/active_tasks.sql"} {
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
		exec(t, "DELETE FROM template_tags", "DELETE FROM template_notes", "DELETE FROM templates",
			"DELETE FROM board_tags", "DELETE FROM cards", "DELETE FROM tags", "DELETE FROM boards",
			"DELETE FROM tasks", "DELETE FROM memberships", "DELETE FROM notes", "DELETE FROM bulletins",
			"DELETE FROM regions")
	}
	q := &featuressuite.CountingQuerier{Inner: dbstdlib.New(db)}
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "mysql",
		Driver:   "stdlib",
		Database: "MySQL " + version,
		Querier:  q,
		Base:     q,
		Caps:     featuressuite.Capabilities{RowLocks: true, CaseInsensitiveText: true},
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
		rq := &featuressuite.CountingQuerier{Inner: dbstdlib.New(rtdb)}
		featuressuite.RunRoundTrips(t, &featuressuite.Harness{
			Dialect: "mysql",
			Driver:  "stdlib",
			Querier: rq,
			Base:    rq.Inner,
			Caps:    featuressuite.Capabilities{RowLocks: true, CaseInsensitiveText: true},
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
