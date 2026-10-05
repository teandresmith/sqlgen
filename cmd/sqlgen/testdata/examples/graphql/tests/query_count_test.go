package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"

	"github.com/teandresmith/sqlgen/database"
	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// countingQuerier instruments the database.Querier interface so tests can
// pin the §25.1 query-count guarantee end-to-end. Every Exec / Query /
// QueryRow / Begin call increments a shared counter, then delegates to
// the wrapped querier. Begin counts as a single "query" too — the §25.1
// formula speaks in terms of round-trips to the DB, and BEGIN is one.
//
// Counter access is mutex-guarded so the relationship loaders (which fan
// out O2M and M2M batches across an errgroup — §13.2) can't race the
// counter against the test goroutine reading it.
type countingQuerier struct {
	inner database.Querier
	mu    sync.Mutex
	n     int
}

func (c *countingQuerier) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Exec(ctx, sql, args...)
}

func (c *countingQuerier) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Query(ctx, sql, args...)
}

func (c *countingQuerier) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.QueryRow(ctx, sql, args...)
}

func (c *countingQuerier) Begin(ctx context.Context, name string, opts ...database.TxOptions) (*database.Tx, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Begin(ctx, name, opts...)
}

func (c *countingQuerier) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func (c *countingQuerier) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n = 0
}

// newCountingHandler boots a parallel gqlgen handler stack against the
// shared postgres testcontainer but wraps the pgx Querier in a counting
// shim, returning the live URL plus the counter handle. The standard
// `testServer` is reused for seeding so its queries aren't included in
// the count — every GraphQL request the test issues against this URL is.
func newCountingHandler(t *testing.T) (string, *countingQuerier) {
	t.Helper()

	counter := &countingQuerier{inner: dbpgx.New(testPool)}
	client := models.New(counter)
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	srv := handler.NewDefaultServer(es)
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL, counter
}

// postGQL issues a single GraphQL request against the supplied URL. It
// mirrors gqlExec but accepts an arbitrary endpoint so the count test
// can target its own parallel handler.
func postGQL(t *testing.T, url, query string, vars map[string]any) gqlResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
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

// TestQueryCount_SingleTable pins the §25.1 baseline: one root query with
// no relationship selections issues exactly one DB query.
func TestQueryCount_SingleTable(t *testing.T) {
	truncateAll(t)
	u := seedUser(t, "qcount-single@example.com", "Single")

	url, counter := newCountingHandler(t)
	counter.reset()

	resp := postGQL(t, url, `
		query ($id: UUID!) { user(id: $id) { id email name } }
	`, map[string]any{"id": u.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}

	if got, want := counter.count(), 1; got != want {
		t.Errorf("expected 1 DB query for single-table get, got %d", got)
	}
}

// TestQueryCount_NestedRelationships pins the §25.1 formula:
//
//	total = 1 + count(O2M relationships loaded) + 2 * count(M2M relationships loaded)
//
// A `user(id) { orders { orderItems { … } } categories { … } }` query
// selects 2 O2M relationships (orders, orderItems) plus 1 M2M relationship
// (categories) on top of the root user fetch — so the expected DB query
// count is 1 + 2 + 2*1 = 5. The PRD calls out exactly this stair-step:
// each additional O2M relationship costs one batched IN(…) query, each
// M2M costs two (junction + entities), regardless of result-set size
// and regardless of nesting depth.
//
// We seed exactly one row per layer so the loaders all have data to
// batch; otherwise an empty parent set short-circuits the child loader
// and the count would be artificially lower (an "empty" relationship
// still costs zero queries because there's nothing to fan out over).
func TestQueryCount_NestedRelationships(t *testing.T) {
	truncateAll(t)

	u := seedUser(t, "qcount-nested@example.com", "Nested")
	cat := seedCategory(t, "QCountCat")

	// One order, one orderItem, one user_categories junction row — the
	// minimum data each loader needs to fire its batched IN(…). We seed
	// via the standard `testServer` so these setup queries are NOT
	// counted by the parallel counting handler.
	var orderOut struct {
		CreateOrder struct {
			ID string `json:"id"`
		} `json:"createOrder"`
	}
	gqlExecData(t, `
		mutation ($userID: UUID!, $createdAt: Time!) {
			createOrder(input: {
				userID: $userID, total: "1.00", createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{"userID": u.ID, "createdAt": fixedTimestamp}, &orderOut)

	// Need a product for the order_items.product_id FK.
	var productOut struct {
		CreateProduct struct {
			ID string `json:"id"`
		} `json:"createProduct"`
	}
	gqlExecData(t, `
		mutation ($categoryID: Int!, $createdAt: Time!) {
			createProduct(input: {
				name: "QCount Product", price: "1.00", stock: 1,
				categoryID: $categoryID, createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{"categoryID": cat.ID, "createdAt": fixedTimestamp}, &productOut)

	gqlExecData(t, `
		mutation ($orderID: UUID!, $productID: UUID!, $createdAt: Time!) {
			createOrderItem(input: {
				orderID: $orderID, productID: $productID,
				quantity: 1, unitPrice: "1.00", createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"orderID":   orderOut.CreateOrder.ID,
		"productID": productOut.CreateProduct.ID,
		"createdAt": fixedTimestamp,
	}, &struct {
		CreateOrderItem struct {
			ID string `json:"id"`
		} `json:"createOrderItem"`
	}{})

	// user_categories junction row — wire the M2M relationship between
	// the seeded user and category. The runtime resolver doesn't expose
	// a user_categories curated surface (it's an M2M junction, not a
	// data-bearing table), so we seed it via raw SQL.
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO user_categories (user_id, category_id) VALUES ($1, $2)`,
		u.ID, cat.ID,
	); err != nil {
		t.Fatalf("seed user_categories: %v", err)
	}

	url, counter := newCountingHandler(t)
	counter.reset()

	resp := postGQL(t, url, `
		query ($id: UUID!) {
			user(id: $id) {
				id
				orders {
					id
					orderItems { id }
				}
				categories { id }
			}
		}
	`, map[string]any{"id": u.ID})
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}

	// 1 (user Get) + 2 (orders, orderItems O2M) + 2 (categories M2M:
	// junction + entities) = 5.
	const want = 5
	if got := counter.count(); got != want {
		t.Errorf("expected %d DB queries (1 base + 2 O2M + 2*1 M2M), got %d", want, got)
	}
}
