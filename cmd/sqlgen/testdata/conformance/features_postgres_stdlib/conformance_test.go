package featurespostgresstdlib

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

	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
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
	for _, file := range []string{"../features_postgres/schema.sql", "../features_postgres/views/active_tasks.sql"} {
		ddl, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(ddl)); err != nil {
			t.Fatal(err)
		}
	}
	var version string
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	reset := func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "TRUNCATE template_tags, template_notes, templates, board_tags, cards, tags, boards, tasks, memberships, notes, bulletins, regions RESTART IDENTITY"); err != nil {
			t.Fatal(err)
		}
	}
	q := &featuressuite.CountingQuerier{Inner: dbstdlib.New(db)}
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "postgres",
		Driver:   "stdlib",
		Database: "PostgreSQL " + version,
		Querier:  q,
		Base:     q,
		Caps:     featuressuite.Capabilities{RowLocks: true, KeySchema: "public"},
		Reset:    reset,
	})

	t.Run("RoundTrips", func(t *testing.T) {
		rec, err := roundtrip.NewRecorder()
		if err != nil {
			t.Fatal(err)
		}
		// The same database, through a pool whose driver reports every
		// statement to rec.
		rtdb, err := roundtrip.OpenDB(rec, "postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rtdb.Close() })
		if err := roundtrip.Warm(ctx, rtdb, 8); err != nil {
			t.Fatal(err)
		}
		rq := &featuressuite.CountingQuerier{Inner: dbstdlib.New(rtdb)}
		featuressuite.RunRoundTrips(t, &featuressuite.Harness{
			Dialect: "postgres",
			Driver:  "stdlib",
			Querier: rq,
			Base:    rq.Inner,
			Caps:    featuressuite.Capabilities{RowLocks: true, KeySchema: "public"},
			Reset:   reset,
		}, rec)
	})
}
