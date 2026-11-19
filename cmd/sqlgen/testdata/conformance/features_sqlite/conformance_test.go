package featuressqlite

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
)

func TestConformance(t *testing.T) {
	// A pragma in the DSN applies to every pooled connection, not only the first.
	db, err := sql.Open("sqlite", "file:features?mode=memory&cache=shared&_pragma=foreign_keys(1)")
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
	q := &featuressuite.CountingQuerier{Inner: dbstdlib.New(db)}
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "sqlite",
		Driver:   "stdlib",
		Database: "SQLite " + version,
		Querier:  q,
		Base:     q,
		Caps:     featuressuite.Capabilities{RowLocks: false},
		Reset: func(t *testing.T) {
			if _, err := db.Exec("DELETE FROM template_tags; DELETE FROM template_notes; DELETE FROM templates; DELETE FROM board_tags; DELETE FROM cards; DELETE FROM tags; DELETE FROM boards; DELETE FROM tasks; DELETE FROM memberships; DELETE FROM notes; DELETE FROM bulletins; DELETE FROM regions"); err != nil {
				t.Fatal(err)
			}
		},
	})
}
