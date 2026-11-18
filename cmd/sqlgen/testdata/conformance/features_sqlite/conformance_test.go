package featuressqlite

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features/models"
	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
)

func TestConformance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:features?mode=memory&cache=shared&_pragma=foreign_keys(1)")
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
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "sqlite",
		Driver:   "stdlib",
		Database: "SQLite " + version,
		Client:   models.New(dbstdlib.New(db)),
	})
}
