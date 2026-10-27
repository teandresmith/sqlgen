package tests

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"

	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// The bulk `updateProducts(filter, input)` read-back when `_inc` / `_dec`
// operators are present: the matched rows come back from one batched read,
// and a set field that soft-deletes a row drops it from the result exactly as
// the operator-free mutation does.

// upfTracedServer boots a GraphQL handler over a client whose every statement,
// transactional ones included, is counted (see statement_counter_test.go).
func upfTracedServer(t *testing.T) (string, *statementCounter) {
	t.Helper()
	client, counter := tracedClient(t)
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL, counter
}

// upfSeedProducts inserts n products named name under one fresh category.
func upfSeedProducts(t *testing.T, name string, n int) {
	t.Helper()
	ctx := context.Background()
	var categoryID int64
	if err := testPool.QueryRow(ctx, `INSERT INTO categories (name) VALUES ($1) RETURNING id`, name+"-category").Scan(&categoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	for i := range n {
		if _, err := testPool.Exec(ctx, `INSERT INTO products (name, price, stock, category_id) VALUES ($1, 1.00, 10, $2)`, name, categoryID); err != nil {
			t.Fatalf("seed product %d: %v", i, err)
		}
	}
}

// upfCountUpdateProducts runs updateProducts over the products named name,
// selecting one O2M relationship, and returns the statements it issued.
func upfCountUpdateProducts(t *testing.T, url string, counter *statementCounter, name, input string) int {
	t.Helper()
	counter.reset()
	resp := postGQL(t, url, fmt.Sprintf(`
		mutation {
			updateProducts(filter: { name: { eq: %q } }, input: %s) { id stock orderItems { id } }
		}
	`, name, input), nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("updateProducts(%s): graphql errors: %+v", input, resp.Errors)
	}
	return counter.count()
}

// TestBulkUpdateReadBack_StatementCountIsOneReadPlusTheOperators pins the
// query-count contract of the operator path. Against the operator-free call
// over the same rows and selection it may add exactly: the transaction's
// BEGIN and COMMIT, one key read (UpdateWhere's own read of the matched rows,
// which is what reports their keys), and one statement per operator per row.
// The final read-back and its relationship batch are shared with the
// operator-free path, so nothing else may grow with the row count.
func TestBulkUpdateReadBack_StatementCountIsOneReadPlusTheOperators(t *testing.T) {
	const fixedOverhead = 3 // BEGIN, COMMIT, the key read
	url, counter := upfTracedServer(t)

	for _, rows := range []int{2, 5} {
		t.Run(fmt.Sprintf("%d rows", rows), func(t *testing.T) {
			truncateAll(t)
			name := fmt.Sprintf("upf-count-%d", rows)
			upfSeedProducts(t, name, rows)

			operatorFree := upfCountUpdateProducts(t, url, counter, name, `{ description: "plain" }`)
			withOperator := upfCountUpdateProducts(t, url, counter, name, `{ description: "with-op", stock_inc: 1 }`)

			if want := operatorFree + fixedOverhead + rows; withOperator != want {
				t.Errorf("updateProducts with stock_inc over %d rows issued %d statements, want %d "+
					"(operator-free %d + BEGIN/COMMIT + key read + %d increments)",
					rows, withOperator, want, operatorFree, rows)
			}
		})
	}
}

// TestBulkUpdateReadBack_SoftDeletingSetFieldDropsTheRow pins that a set field
// soft-deleting the matched row, sent with an operator, answers like the
// operator-free mutation: no error, and the row absent from the result.
func TestBulkUpdateReadBack_SoftDeletingSetFieldDropsTheRow(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "operator-free", input: `{ deletedAt: "2026-01-01T00:00:00Z" }`},
		{name: "with an operator", input: `{ deletedAt: "2026-01-01T00:00:00Z", stock_inc: 1 }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			upfSeedProducts(t, "upf-soft-delete", 1)

			resp := gqlExec(t, `
				mutation {
					updateProducts(filter: { name: { eq: "upf-soft-delete" } }, input: `+tt.input+`) { id }
				}
			`, nil, nil)
			if len(resp.Errors) > 0 {
				t.Fatalf("updateProducts(%s): graphql errors: %+v", tt.input, resp.Errors)
			}
			if string(resp.Data) != `{"updateProducts":[]}` {
				t.Errorf("updateProducts(%s) data = %s, want the soft-deleted row absent", tt.input, resp.Data)
			}
			var deleted bool
			if err := testPool.QueryRow(context.Background(),
				`SELECT deleted_at IS NOT NULL FROM products WHERE name = 'upf-soft-delete'`).Scan(&deleted); err != nil {
				t.Fatalf("read back products row: %v", err)
			}
			if !deleted {
				t.Errorf("updateProducts(%s): deleted_at is NULL, want the set field written", tt.input)
			}
		})
	}
}
