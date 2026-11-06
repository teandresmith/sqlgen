package postgres

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
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	var version string
	if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	adapter := dbpgx.New(pool)
	q := &suite.CountingQuerier{Inner: adapter}
	base := batchingQuerier{CountingQuerier: q, batcher: adapter.(batcher)}
	suite.Run(t, &suite.Harness{
		Dialect:  "postgres",
		Driver:   "pgx",
		Database: "PostgreSQL " + version,
		Client:   models.New(base),
		Querier:  q,
		Base:     base,
		Caps:     suite.Capabilities{RowLocks: true, CompositeLoadChunk: 4000},
		Reset: func(t *testing.T) {
			if _, err := pool.Exec(ctx, "TRUNCATE crate_items, crates, item_stations, stations, parcels, shelves, item_receipts, item_notes, order_items, products, documents, revisions, labels, counters, author_tags, posts, author_profiles, tags, authors RESTART IDENTITY"); err != nil {
				t.Fatal(err)
			}
		},
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
	*suite.CountingQuerier
	batcher batcher
}

func (b batchingQuerier) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	for _, qq := range batch.QueuedQueries {
		b.Record(qq.SQL)
	}
	return b.batcher.SendBatch(ctx, batch)
}
