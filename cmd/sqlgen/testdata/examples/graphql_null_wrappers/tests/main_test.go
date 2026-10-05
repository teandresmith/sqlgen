package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_null_wrappers/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_null_wrappers/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql_null_wrappers/models/graph/sqlgenresolver"
)

var (
	testPool   *pgxpool.Pool
	testServer *httptest.Server
	testClient *models.Client
)

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

	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}
	if _, err := testPool.Exec(ctx, string(schema)); err != nil {
		panic(fmt.Sprintf("applying schema: %v", err))
	}

	testClient = models.New(dbpgx.New(testPool))

	resolver := &graph.Resolver{
		Client: testClient,
		Q:      &sqlgenresolver.Q{Client: testClient},
		M:      &sqlgenresolver.M{Client: testClient},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	testServer = httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(handler.NewDefaultServer(es)))

	code := m.Run()

	testServer.Close()
	testPool.Close()
	_ = pgContainer.Terminate(ctx)
	os.Exit(code)
}

// gqlResponse is the standard GraphQL response envelope. `data` stays a
// json.RawMessage so each test unmarshals into its own typed struct rather
// than a generic map[string]any tree.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

type gqlError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

// gqlExec sends a GraphQL request against the test server and returns the
// parsed response envelope. It does NOT fail the test on GraphQL errors —
// callers inspect resp.Errors.
func gqlExec(t *testing.T, query string, variables map[string]any) gqlResponse {
	t.Helper()

	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, testServer.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", resp.StatusCode, raw)
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response (raw=%s): %v", raw, err)
	}
	return out
}

// gqlExecData runs gqlExec, asserts no errors, and unmarshals data into out.
func gqlExecData(t *testing.T, query string, variables map[string]any, out any) {
	t.Helper()

	resp := gqlExec(t, query, variables)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
	if out == nil {
		return
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}
}

// truncateAll resets both tables. RESTART IDENTITY rolls the BIGSERIAL PKs
// back to 1 so each test's id assertions are stable.
func truncateAll(t *testing.T) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), `TRUNCATE TABLE entries, accounts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
