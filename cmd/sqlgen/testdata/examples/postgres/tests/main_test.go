package tests

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/postgres/models"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("sqlgen_e2e"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		panic(fmt.Sprintf("starting postgres container: %v", err))
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		panic(fmt.Sprintf("creating pool: %v", err))
	}

	// Apply schema
	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testPool.Exec(ctx, string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	// Apply view definitions from annotation files (order_totals is a
	// materialized view — the annotation file is valid SQL either way).
	for _, viewFile := range []string{"product_summary", "category_stats", "order_totals"} {
		viewSQL, err := os.ReadFile("../views/" + viewFile + ".sql")
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		if _, err := testPool.Exec(ctx, string(viewSQL)); err != nil {
			panic(fmt.Sprintf("applying view %s: %v", viewFile, err))
		}
	}

	// The unique index backing the @pk assertion — the precondition for
	// REFRESH MATERIALIZED VIEW CONCURRENTLY.
	if _, err := testPool.Exec(ctx, "CREATE UNIQUE INDEX order_totals_product_id_idx ON order_totals (product_id)"); err != nil {
		panic(fmt.Sprintf("creating order_totals unique index: %v", err))
	}

	code := m.Run()

	testPool.Close()
	_ = pgContainer.Terminate(ctx)
	os.Exit(code)
}

func newClient() *models.Client {
	return models.New(dbpgx.New(testPool))
}

// mustJSON marshals v to JSON and returns a types.JSON wrapping the bytes.
// Used by tests to author types.JSON fixtures from Go literals without the
// verbosity of inline byte slices.
func mustJSON(v any) types.JSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return types.JSON(b)
}

// assertJSONStructEqual asserts two types.JSON values decode to equal Go
// any-trees. Avoids byte-equality which fails when the storage engine
// canonicalizes (Postgres jsonb normalizes whitespace, may reorder keys).
func assertJSONStructEqual(t *testing.T, label string, want, got types.JSON) {
	t.Helper()
	var w, g any
	if err := want.Decode(&w); err != nil {
		t.Fatalf("%s: decode want: %v", label, err)
	}
	if err := got.Decode(&g); err != nil {
		t.Fatalf("%s: decode got: %v", label, err)
	}
	if !jsonAnyEqual(w, g) {
		t.Errorf("%s round-trip:\ngot  %s\nwant %s", label, got, want)
	}
}

// jsonAnyEqual structurally compares two any values decoded from JSON.
func jsonAnyEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !jsonAnyEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonAnyEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// idStrings renders UUID primary keys for comparator.ID, whose operands are
// strings whatever the column's Go type — ID and FK values are converted to
// strings for comparison (PRD §7.4). Only the slice operands need it; a single
// operand takes .String() inline.
func idStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
