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
	"path/filepath"
	"sort"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

var (
	testPool   *pgxpool.Pool
	testServer *httptest.Server
	testClient *models.Client
	// testConnStr is the container's DSN, kept so a test that needs its own
	// pool — one carrying a pgx tracer, say — can build one against the same
	// database rather than wrapping testPool. A database.Querier shim cannot
	// see inside a transaction: database.Conn hands the body the *database.Tx,
	// which holds the driver connection the shim delegated to, so every
	// statement between BEGIN and COMMIT bypasses it.
	testConnStr string
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
	testConnStr = connStr

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

	// The annotation files under ../views/ are valid DDL, so the
	// same files sqlgen parses create the real relations the GraphQL read
	// surface queries. `category_price_totals` is a MATERIALIZED view: it is
	// populated at CREATE time and thereafter only by refreshCategoryPriceTotals,
	// which is exactly the staleness contract PRD §16.5 gives it.
	viewFiles, err := filepath.Glob("../views/*.sql")
	if err != nil {
		panic(fmt.Sprintf("globbing views: %v", err))
	}
	if len(viewFiles) == 0 {
		panic("no view files found under ../views — sqlgen parses that directory, so an empty glob means the read surface is generated against relations this database does not have")
	}
	sort.Strings(viewFiles)
	for _, viewFile := range viewFiles {
		viewSQL, err := os.ReadFile(viewFile) //nolint:gosec // test fixture, path comes from a glob of the example's own views dir
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		if _, err := testPool.Exec(ctx, string(viewSQL)); err != nil {
			panic(fmt.Sprintf("applying view %s: %v", viewFile, err))
		}
	}

	// category_price_totals asserts `@pk: category_id`, which per PRD §16.5.2
	// is what makes the matview concurrently refreshable — so the generated
	// client emits RefreshConcurrently, and that statement requires a real
	// UNIQUE index to exist. The annotation alone does not create one. Without
	// this the fixture would advertise a precondition it never established.
	if _, err := testPool.Exec(ctx, `CREATE UNIQUE INDEX category_price_totals_category_id_idx ON category_price_totals (category_id)`); err != nil {
		panic(fmt.Sprintf("creating category_price_totals unique index: %v", err))
	}

	testClient = models.New(dbpgx.New(testPool))

	resolver := &graph.Resolver{
		Client: testClient,
		Q:      &sqlgenresolver.Q{Client: testClient},
		M:      &sqlgenresolver.M{Client: testClient},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	srv := handler.NewDefaultServer(es)
	testServer = httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))

	code := m.Run()

	testServer.Close()
	testPool.Close()
	_ = pgContainer.Terminate(ctx)
	cleanupSqlgenBuild()
	os.Exit(code)
}

// gqlResponse is the standard GraphQL response envelope. We keep `data` as a
// raw json.RawMessage so each test can unmarshal into its own typed struct
// rather than a generic map[string]any tree.
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
// callers inspect resp.Errors to assert either success or expected codes.
func gqlExec(t *testing.T, query string, variables map[string]any, headers map[string]string) gqlResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, testServer.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
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
	resp := gqlExec(t, query, variables, nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}
}

// truncateAll resets every managed table to its empty state. Used by tests
// that need a clean slate (PageInfo cursor pins, list-totalCount checks).
// Uses TRUNCATE … CASCADE so we don't have to spell out the FK dependency
// order; RESTART IDENTITY rolls the categories BIGSERIAL back to 1 so each
// test's int-PK assertions are stable.
func truncateAll(t *testing.T) {
	t.Helper()
	const stmt = `TRUNCATE TABLE order_items, orders, profiles, user_credentials, user_sessions, user_badges, products, user_categories, workspace_settings, workspace_notes, events, asset_document_links, documents, assets, users, categories, scalar_probes, numeric_widths, filter_probes RESTART IDENTITY CASCADE`
	if _, err := testPool.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// refreshCategoryPriceTotals repopulates the materialized view from the
// current `products` rows. A matview does not observe writes to its base
// tables (PRD §16.5), so every test that seeds products and then reads
// `categoryPriceTotal*` over GraphQL has to call this in between — the same
// step a consumer performs through the Go-client-only Refresh method the
// GraphQL surface deliberately does not expose.
func refreshCategoryPriceTotals(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `REFRESH MATERIALIZED VIEW category_price_totals`); err != nil {
		t.Fatalf("refresh category_price_totals: %v", err)
	}
}

// extensionsCode reads `extensions.code` off a gqlError as a string. The
// `code` extension is the PRD §26.5.5 contract for every sentinel-mapped
// error; tests assert on this value (not on `message`, which is only a
// human-readable hint). Returns "" when the field is absent or non-string,
// so callers can distinguish missing-code from typed mismatches.
func extensionsCode(e gqlError) string {
	if e.Extensions == nil {
		return ""
	}
	v, ok := e.Extensions["code"].(string)
	if !ok {
		return ""
	}
	return v
}
