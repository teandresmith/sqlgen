package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"
	"github.com/teandresmith/sqlgen/types"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/mysql/models/graph/sqlgenresolver"
)

var (
	testDB     *sql.DB
	testServer *httptest.Server
	// testConnStr is the container DSN, kept so a test can open its own pool
	// against the same database — one with a single connection, whose
	// server-side session counters then see every statement a call issues.
	testConnStr string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	ctx := context.Background()

	mysqlContainer, err := mysql.Run(
		ctx,
		"mysql:8.0",
		mysql.WithDatabase("sqlgen_e2e"),
		mysql.WithUsername("test"),
		mysql.WithPassword("test"),
	)
	if err != nil {
		panic(fmt.Sprintf("starting mysql container: %v", err))
	}

	connStr, err := mysqlContainer.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		panic(fmt.Sprintf("getting connection string: %v", err))
	}
	testConnStr = connStr

	testDB, err = sql.Open("mysql", connStr)
	if err != nil {
		panic(fmt.Sprintf("opening database: %v", err))
	}

	// Apply schema
	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		panic(fmt.Sprintf("reading schema: %v", err))
	}

	// MySQL requires executing statements one at a time
	stmts := splitStatements(string(schema))
	for _, stmt := range stmts {
		if stmt == "" {
			continue
		}
		if _, err := testDB.ExecContext(ctx, stmt); err != nil {
			panic(fmt.Sprintf("applying schema statement: %v\nSQL: %s", err, stmt))
		}
	}

	// Apply view definitions from annotation files
	for _, viewFile := range []string{"product_summary", "category_stats"} {
		viewSQL, err := os.ReadFile("../views/" + viewFile + ".sql")
		if err != nil {
			panic(fmt.Sprintf("reading view %s: %v", viewFile, err))
		}
		for _, stmt := range splitStatements(string(viewSQL)) {
			if stmt == "" {
				continue
			}
			if _, err := testDB.ExecContext(ctx, stmt); err != nil {
				panic(fmt.Sprintf("applying view %s: %v\nSQL: %s", viewFile, err, stmt))
			}
		}
	}

	// Live gqlgen handler over the same database. This example is the only
	// module that compiles a MySQL-specific Go type into a GraphQL schema —
	// the SET column above all — so the surface is
	// exercised through a real server rather than asserted on generated text.
	client := newClient()
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	testServer = httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(handler.NewDefaultServer(es)))

	code := m.Run()

	testServer.Close()
	testDB.Close()
	_ = mysqlContainer.Terminate(ctx)
	os.Exit(code)
}

// gqlResponse is the standard GraphQL response envelope. `data` stays a raw
// json.RawMessage so each test unmarshals into its own typed struct rather
// than a generic map tree.
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
// parsed envelope. It does NOT fail on GraphQL errors — callers inspect
// resp.Errors to assert either success or an expected rejection.
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
	// 200 carries a resolved response (with or without field errors); 422 is
	// what gqlgen returns when the document or its variables fail validation
	// before any resolver runs — an enum member outside the declared set, for
	// one. Both are well-formed GraphQL envelopes and both are outcomes tests
	// here assert on, so only a third status is a harness failure.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnprocessableEntity {
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
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}
}

// splitStatements splits a SQL script into individual statements on semicolons.
func splitStatements(script string) []string {
	var stmts []string
	var current []byte
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(script); i++ {
		c := script[i]
		if inQuote {
			current = append(current, c)
			if c == quoteChar {
				inQuote = false
			}
			continue
		}
		if c == '\'' || c == '"' {
			inQuote = true
			quoteChar = c
			current = append(current, c)
			continue
		}
		if c == '-' && i+1 < len(script) && script[i+1] == '-' {
			// Skip line comment
			for i < len(script) && script[i] != '\n' {
				i++
			}
			continue
		}
		if c == ';' {
			s := trimSpace(current)
			if len(s) > 0 {
				stmts = append(stmts, string(s))
			}
			current = current[:0]
			continue
		}
		current = append(current, c)
	}
	s := trimSpace(current)
	if len(s) > 0 {
		stmts = append(stmts, string(s))
	}
	return stmts
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}

func newClient() *models.Client {
	return models.New(dbstdlib.New(testDB))
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
