package postgresstdlib

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/models"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/suite"
)

func TestConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker")
	}
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("conformance"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// The pgx module's DDL: this module differs from it only in the driver.
	schema, err := os.ReadFile("../postgres/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	var version string
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	q := &suite.CountingQuerier{Inner: dbstdlib.New(db)}
	suite.Run(t, &suite.Harness{
		Dialect:  "postgres",
		Driver:   "stdlib",
		Database: "PostgreSQL " + version,
		Client:   models.New(q),
		Querier:  q,
		Base:     q,
		Caps:     suite.Capabilities{RowLocks: true},
		Reset: func(t *testing.T) {
			if _, err := db.ExecContext(ctx, "TRUNCATE order_items, products, documents, revisions, labels, counters, author_tags, posts, author_profiles, tags, authors RESTART IDENTITY"); err != nil {
				t.Fatal(err)
			}
		},
	})
}
