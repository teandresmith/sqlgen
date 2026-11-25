package featurespostgres

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	featuressuite "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/features_suite"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/conformance/roundtrip/pgxtrace"
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
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, file := range []string{"schema.sql", "views/active_tasks.sql"} {
		ddl, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(ddl)); err != nil {
			t.Fatal(err)
		}
	}
	var version string
	if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	reset := func(t *testing.T) {
		if _, err := pool.Exec(ctx, "TRUNCATE template_tags, template_notes, templates, board_tags, cards, tags, boards, tasks, memberships, notes, bulletins, regions RESTART IDENTITY"); err != nil {
			t.Fatal(err)
		}
	}
	adapter := dbpgx.New(pool)
	q := &featuressuite.CountingQuerier{Inner: adapter}
	featuressuite.Run(t, &featuressuite.Harness{
		Dialect:  "postgres",
		Driver:   "pgx",
		Database: "PostgreSQL " + version,
		Querier:  q,
		Base:     batchingQuerier{CountingQuerier: q, batcher: adapter.(batcher)},
		Caps:     featuressuite.Capabilities{RowLocks: true, KeySchema: "public"},
		Reset:    reset,
	})

	t.Run("RoundTrips", func(t *testing.T) {
		rec, err := roundtrip.NewRecorder()
		if err != nil {
			t.Fatal(err)
		}
		// The same database, through a pool whose tracer reports every
		// statement and batch to rec.
		rtpool, err := pgxtrace.NewPool(ctx, rec, dsn, 8)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(rtpool.Close)
		rtadapter := dbpgx.New(rtpool)
		featuressuite.RunRoundTrips(t, &featuressuite.Harness{
			Dialect: "postgres",
			Driver:  "pgx",
			Querier: &featuressuite.CountingQuerier{Inner: rtadapter},
			Base:    rtadapter,
			Caps:    featuressuite.Capabilities{RowLocks: true, KeySchema: "public"},
			Reset:   reset,
		}, rec)
	})
}

// batcher is the pgx adapter's SendBatch.
type batcher interface {
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

// batchingQuerier is the counting querier with the adapter's SendBatch, so
// the generated client takes its pgx batch path. It records each queued
// statement.
type batchingQuerier struct {
	*featuressuite.CountingQuerier
	batcher batcher
}

func (b batchingQuerier) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	for _, qq := range batch.QueuedQueries {
		b.Record(qq.SQL)
	}
	return b.batcher.SendBatch(ctx, batch)
}
