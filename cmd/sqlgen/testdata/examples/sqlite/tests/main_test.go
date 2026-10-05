package tests

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	// `file::memory:?cache=shared` makes the in-memory database visible
	// across every connection sql.DB pulls from the pool. Without it, the
	// modernc.org/sqlite driver gives each pool connection its own private
	// in-memory database, so a relationship loader that fans out to multiple
	// goroutines (e.g. sub-categorized polymorphism's three nested loads)
	// sees "no such table" on every connection except the one that ran the
	// schema. Single-relationship tests pass by accident because sql.DB
	// reuses the warm connection.
	var err error
	testDB, err = sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}

	// Enable WAL mode and foreign keys
	if _, err := testDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		panic(fmt.Sprintf("setting WAL mode: %v", err))
	}
	if _, err := testDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		panic(fmt.Sprintf("enabling foreign keys: %v", err))
	}

	// Apply schema
	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testDB.Exec(string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	// Apply view definitions from annotation files
	for _, viewFile := range []string{"product_summary", "category_stats"} {
		viewSQL, err := os.ReadFile("../views/" + viewFile + ".sql")
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		if _, err := testDB.Exec(string(viewSQL)); err != nil {
			panic(fmt.Sprintf("applying view %s: %v", viewFile, err))
		}
	}

	code := m.Run()

	testDB.Close()
	os.Exit(code)
}

func newClient() *models.Client {
	return models.New(dbstdlib.New(testDB))
}
