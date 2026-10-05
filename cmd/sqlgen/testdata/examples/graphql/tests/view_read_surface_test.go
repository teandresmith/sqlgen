package tests

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/99designs/gqlgen/graphql/handler"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// The generated view read surface — PRD §26.4 "Views on the GraphQL
// surface", §16.5 (materialized views), §25.1 (query count).
//
// The example carries one of each kind: `workspace_note_summary` is a regular,
// tenanted view (its isolation contract lives in view_tenancy_test.go) and
// `category_price_totals` is a materialized view over the shared `products`
// table. §26.4 says a matview emits the *identical* read surface and that
// Refresh / RefreshConcurrently stay Go-client-only, so the two claims are
// asserted against the running schema rather than against the golden text.

// bindingProof is a compile-time assertion that the view's object type is bound
// to the consumer's models package, not to a gqlgen-generated `graph/model`
// struct. That binding is the point of the view projection: it is what retires
// the hand-written `models:` entry a consumer once needed, and it is what
// makes the resolver hand back the same row type the Go client returns. If
// buildMergeInput stopped emitting the four per-view bindings, gqlgen would
// generate its own `model.CategoryPriceTotal` and this would not compile.
var bindingProof = func(q *sqlgenresolver.Q) {
	var (
		_ func(context.Context, int) (*models.CategoryPriceTotal, error)                                                                               = q.CategoryPriceTotal
		_ func(context.Context, *models.CategoryPriceTotalFilter, *int, *string, *int, *string) (*models.Connection[models.CategoryPriceTotal], error) = q.CategoryPriceTotals
		_ func(context.Context, uuid.UUID) (*models.WorkspaceNoteSummary, error)                                                                       = q.WorkspaceNoteSummary
	)
}

// newCountingTenantedHandler is newCountingHandler plus the header-driven
// tenant bridge, so the §25.1 count can be taken over a *tenanted* view. The
// tenant predicate is appended to the WHERE clause of a statement that would
// have run anyway, so it must cost zero extra round-trips — a shape that
// resolved the tenant with its own SELECT would pass every isolation assertion
// in view_tenancy_test.go and fail here.
func newCountingTenantedHandler(t *testing.T) (string, *countingQuerier) {
	t.Helper()

	counter := &countingQuerier{inner: dbpgx.New(testPool)}
	client := models.New(counter, models.WithTenantResolver(ctxTenantResolver()))
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	srv := handler.NewDefaultServer(es)
	httpsrv := httptest.NewServer(tenantHeaderMiddleware(sqlgenresolver.WithCallOptionsMiddleware(srv)))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL, counter
}

// --- §25.1 query count ---------------------------------------------------

// TestViewQueryCount pins PRD §26.4's closing claim: a view has no
// relationships, so its walker is columns-only and a view read is exactly one
// query under §25.1 — regardless of how many rows come back. The envelope
// queries cost the same two statements a table's do (count + page), and
// neither number moves with the result-set size, which is what the
// three-versus-nine-row subtest establishes.
func TestViewQueryCount(t *testing.T) {
	truncateAll(t)
	noteID := seedWorkspaceNoteRow(t, fixedTenantA, "counted", "draft", `{"team": "alpha"}`, 1)
	for i := range 8 {
		seedWorkspaceNoteRow(t, fixedTenantA, "filler", "published", `{"team": "alpha"}`, i+2)
	}
	seedNoteDocument(t, noteID, "counted-doc")

	url, counter := newCountingTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": fixedTenantA.String()}

	tests := []struct {
		name  string
		query string
		vars  map[string]any
		want  int
		why   string
	}{
		{
			name:  "by-pk read is one query",
			query: `query ($id: UUID!) { workspaceNoteSummary(id: $id) { id body kind labels documentCount } }`,
			vars:  map[string]any{"id": noteID.String()},
			want:  1,
			why:   "a view has no relationships, so nothing fans out (§26.4)",
		},
		{
			name:  "list with totalCount is count + page",
			query: `query { workspaceNoteSummaryList(limit: 5) { totalCount hasMore items { id body kind documentCount } } }`,
			want:  2,
			why:   "Paginate issues no statement of its own — Count and GetMany each issue one",
		},
		{
			name:  "connection with totalCount is count + page",
			query: `query { workspaceNoteSummaries(first: 5) { totalCount edges { node { id body } } } }`,
			want:  2,
			why:   "Connection has the same two-statement shape as Paginate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter.reset()
			resp := postGQLAt(t, url, tt.query, tt.vars, headers)
			if len(resp.Errors) != 0 {
				t.Fatalf("unexpected errors: %+v", resp.Errors)
			}
			if got := counter.count(); got != tt.want {
				t.Errorf("DB queries = %d, want %d — %s", got, tt.want, tt.why)
			}
		})
	}
}

// --- comparator families on a view ---------------------------------------

// TestViewFilter_EnumNarrows pins that the monomorphized enum comparator
// reaches a view. PRD §26.4 promises a view's <V>Filter is the table's read
// half; before this fixture nothing checked that promise for the enum family
// on a view, and the failure mode design §1.3 indicts is silent — a filter
// field the schema accepts and the translator drops narrows nothing and the
// caller sees every row.
func TestViewFilter_EnumNarrows(t *testing.T) {
	truncateAll(t)
	seedWorkspaceNoteRow(t, fixedTenantA, "draft-one", "draft", `{"team": "alpha"}`, 1)
	seedWorkspaceNoteRow(t, fixedTenantA, "draft-two", "draft", `{"team": "alpha"}`, 2)
	seedWorkspaceNoteRow(t, fixedTenantA, "published-one", "published", `{"team": "alpha"}`, 3)
	seedWorkspaceNoteRow(t, fixedTenantA, "archived-one", "archived", `{"team": "alpha"}`, 4)

	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": fixedTenantA.String()}

	tests := []struct {
		name    string
		filter  string
		want    int
		wantAll string
	}{
		{"eq", `{ kind: { eq: DRAFT } }`, 2, "DRAFT"},
		{"neq", `{ kind: { neq: DRAFT } }`, 2, ""},
		{"in", `{ kind: { in: [PUBLISHED, ARCHIVED] } }`, 2, ""},
		{"nin", `{ kind: { nin: [PUBLISHED, ARCHIVED] } }`, 2, "DRAFT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := viewListBodies(t, url, headers, tt.filter)
			if len(items) != tt.want {
				t.Fatalf("filter %s matched %d rows, want %d (bodies: %v)", tt.filter, len(items), tt.want, bodiesOf(items))
			}
			if tt.wantAll == "" {
				return
			}
			for _, it := range items {
				if it.Kind != tt.wantAll {
					t.Errorf("row %q has kind %q, want %q — the filter was accepted and ignored", it.Body, it.Kind, tt.wantAll)
				}
			}
		})
	}
}

// TestViewFilter_JSONBNarrows pins the JSONB family on a view. `labels` is a
// jsonb column projected straight through the view, so every operator here
// compiles to the PostgreSQL-only document operators §11.2 gates on the
// dialect.
func TestViewFilter_JSONBNarrows(t *testing.T) {
	truncateAll(t)
	seedWorkspaceNoteRow(t, fixedTenantA, "alpha-note", "draft", `{"team": "alpha", "priority": 1}`, 1)
	seedWorkspaceNoteRow(t, fixedTenantA, "bravo-note", "draft", `{"team": "bravo"}`, 2)
	seedWorkspaceNoteRow(t, fixedTenantA, "untagged-note", "draft", `{"other": true}`, 3)

	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": fixedTenantA.String()}

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{"hasKey", `{ labels: { hasKey: "team" } }`, []string{"alpha-note", "bravo-note"}},
		{"hasAllKeys", `{ labels: { hasAllKeys: ["team", "priority"] } }`, []string{"alpha-note"}},
		{"hasAnyKey", `{ labels: { hasAnyKey: ["priority", "other"] } }`, []string{"alpha-note", "untagged-note"}},
		{"contains", `{ labels: { contains: { team: "bravo" } } }`, []string{"bravo-note"}},
		{"pathExists", `{ labels: { pathExists: "$.priority" } }`, []string{"alpha-note"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bodiesOf(viewListBodies(t, url, headers, tt.filter))
			if !equalStringSets(got, tt.want) {
				t.Errorf("filter %s matched %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

// viewListBodies runs `workspaceNoteSummaryList` with the supplied inline
// filter literal and returns the matched rows. The filter is inlined rather
// than passed as a variable so each case reads as the GraphQL a caller would
// actually send.
func viewListBodies(t *testing.T, url string, headers map[string]string, filter string) []viewSummaryNode {
	t.Helper()
	query := `
		query {
			workspaceNoteSummaryList(filter: ` + filter + `, sort: [{ field: PINNED_ORDER, direction: ASC }]) {
				totalCount
				items { id workspaceID body kind documentCount }
			}
		}
	`
	resp := postGQLAt(t, url, query, nil, headers)
	if len(resp.Errors) != 0 {
		t.Fatalf("filter %s: unexpected errors: %+v", filter, resp.Errors)
	}
	var out struct {
		WorkspaceNoteSummaryList struct {
			TotalCount int               `json:"totalCount"`
			Items      []viewSummaryNode `json:"items"`
		} `json:"workspaceNoteSummaryList"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		t.Fatalf("filter %s decode (raw=%s): %v", filter, resp.Data, err)
	}
	// totalCount is computed by a separate statement from the page, so a filter
	// that reached only one of the two would show up as a mismatch here.
	if got, want := out.WorkspaceNoteSummaryList.TotalCount, len(out.WorkspaceNoteSummaryList.Items); got != want {
		t.Errorf("filter %s: totalCount = %d but items = %d — the filter did not reach both statements", filter, got, want)
	}
	return out.WorkspaceNoteSummaryList.Items
}

func bodiesOf(items []viewSummaryNode) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Body)
	}
	return out
}

func equalStringSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(want))
	for _, w := range want {
		seen[w]++
	}
	for _, g := range got {
		seen[g]--
		if seen[g] < 0 {
			return false
		}
	}
	return true
}

// --- materialized view ---------------------------------------------------

// seedCategoryWithProducts creates one category and `n` products priced at
// `unit` each, then refreshes the matview so it observes them.
func seedCategoryWithProducts(t *testing.T, name string, n int, unit string) int {
	t.Helper()
	cat := seedCategory(t, name)
	for i := range n {
		gqlExecData(t, `
			mutation ($categoryID: Int!, $name: String!, $price: Decimal!, $createdAt: Time!) {
				createProduct(input: {
					name: $name, price: $price, stock: 1,
					categoryID: $categoryID, createdAt: $createdAt
				}) { id }
			}
		`, map[string]any{
			"categoryID": cat.ID,
			"name":       name + "-product-" + string(rune('a'+i)),
			"price":      unit,
			"createdAt":  fixedTimestamp,
		}, &struct {
			CreateProduct struct {
				ID string `json:"id"`
			} `json:"createProduct"`
		}{})
	}
	refreshCategoryPriceTotals(t)
	return cat.ID
}

// TestMaterializedView_ReadSurface pins §26.4's "materialized views emit this
// identical surface": all three read queries answer, over the same object type
// and envelopes a regular view gets, with no per-matview arm anywhere in the
// templates.
func TestMaterializedView_ReadSurface(t *testing.T) {
	truncateAll(t)
	catID := seedCategoryWithProducts(t, "MatviewCat", 3, "2.50")

	type totalNode struct {
		CategoryID   int    `json:"categoryID"`
		ProductCount int    `json:"productCount"`
		TotalPrice   string `json:"totalPrice"`
	}

	t.Run("by pk", func(t *testing.T) {
		var out struct {
			CategoryPriceTotal *totalNode `json:"categoryPriceTotal"`
		}
		gqlExecData(t, `
			query ($id: Int!) {
				categoryPriceTotal(categoryID: $id) { categoryID productCount totalPrice }
			}
		`, map[string]any{"id": catID}, &out)
		if out.CategoryPriceTotal == nil {
			t.Fatal("categoryPriceTotal returned null for a category the matview covers")
		}
		if got, want := out.CategoryPriceTotal.ProductCount, 3; got != want {
			t.Errorf("productCount = %d, want %d", got, want)
		}
		if got, want := out.CategoryPriceTotal.TotalPrice, "7.5"; got != want {
			t.Errorf("totalPrice = %q, want %q (the aggregate column's decimal binding)", got, want)
		}
	})

	t.Run("list", func(t *testing.T) {
		var out struct {
			CategoryPriceTotalList struct {
				TotalCount int         `json:"totalCount"`
				Items      []totalNode `json:"items"`
			} `json:"categoryPriceTotalList"`
		}
		gqlExecData(t, `
			query {
				categoryPriceTotalList(filter: { productCount: { gte: 2 } }) {
					totalCount
					items { categoryID productCount }
				}
			}
		`, nil, &out)
		if got, want := out.CategoryPriceTotalList.TotalCount, 1; got != want {
			t.Fatalf("totalCount = %d, want %d (items: %+v)", got, want, out.CategoryPriceTotalList.Items)
		}
		if got, want := out.CategoryPriceTotalList.Items[0].CategoryID, catID; got != want {
			t.Errorf("items[0].categoryID = %d, want %d", got, want)
		}
	})

	t.Run("connection", func(t *testing.T) {
		var out struct {
			CategoryPriceTotals struct {
				TotalCount int `json:"totalCount"`
				Edges      []struct {
					Node totalNode `json:"node"`
				} `json:"edges"`
			} `json:"categoryPriceTotals"`
		}
		// The connection query exists only because sqlgen.yml names
		// cursor_keys for this view — views have no primary-key fallback
		// (§4.13), and the inherited default `id` is not one of its columns.
		gqlExecData(t, `
			query {
				categoryPriceTotals(first: 10) {
					totalCount
					edges { node { categoryID productCount } }
				}
			}
		`, nil, &out)
		if got, want := out.CategoryPriceTotals.TotalCount, 1; got != want {
			t.Fatalf("totalCount = %d, want %d", got, want)
		}
		if got, want := out.CategoryPriceTotals.Edges[0].Node.ProductCount, 3; got != want {
			t.Errorf("edges[0].node.productCount = %d, want %d", got, want)
		}
	})

	t.Run("stays stale until refreshed", func(t *testing.T) {
		// §16.5: a matview does not observe writes to its base tables. The
		// GraphQL surface exposes no way to refresh it — that is the point of
		// the next test — so the staleness is observable from here and the
		// consumer's only remedy is the Go client.
		gqlExecData(t, `
			mutation ($categoryID: Int!, $createdAt: Time!) {
				createProduct(input: {
					name: "MatviewCat-late", price: "1.00", stock: 1,
					categoryID: $categoryID, createdAt: $createdAt
				}) { id }
			}
		`, map[string]any{"categoryID": catID, "createdAt": fixedTimestamp}, &struct {
			CreateProduct struct {
				ID string `json:"id"`
			} `json:"createProduct"`
		}{})

		var stale struct {
			CategoryPriceTotal *totalNode `json:"categoryPriceTotal"`
		}
		gqlExecData(t, `
			query ($id: Int!) { categoryPriceTotal(categoryID: $id) { productCount } }
		`, map[string]any{"id": catID}, &stale)
		if got, want := stale.CategoryPriceTotal.ProductCount, 3; got != want {
			t.Errorf("productCount = %d, want %d — the matview answered from base-table state", got, want)
		}

		refreshCategoryPriceTotals(t)

		var fresh struct {
			CategoryPriceTotal *totalNode `json:"categoryPriceTotal"`
		}
		gqlExecData(t, `
			query ($id: Int!) { categoryPriceTotal(categoryID: $id) { productCount } }
		`, map[string]any{"id": catID}, &fresh)
		if got, want := fresh.CategoryPriceTotal.ProductCount, 4; got != want {
			t.Errorf("productCount after refresh = %d, want %d", got, want)
		}
	})

	t.Run("refresh is reachable on the Go client", func(t *testing.T) {
		// The other half of "Refresh is Go-client-only": the schema tests pin
		// that it is absent from the API, and this pins that it is present and
		// works underneath — otherwise "Go-client-only" would be indistinguishable
		// from "not generated at all".
		//
		// RefreshConcurrently is the variant worth calling: per §16.5.2 the
		// `@pk: category_id` annotation is what makes it generated, and the
		// statement itself requires a real UNIQUE index, which TestMain creates.
		// A fixture that asserted the PK but never established the index would
		// ship a method that fails on first use.
		gqlExecData(t, `
			mutation ($categoryID: Int!, $createdAt: Time!) {
				createProduct(input: {
					name: "MatviewCat-concurrent", price: "1.00", stock: 1,
					categoryID: $categoryID, createdAt: $createdAt
				}) { id }
			}
		`, map[string]any{"categoryID": catID, "createdAt": fixedTimestamp}, &struct {
			CreateProduct struct {
				ID string `json:"id"`
			} `json:"createProduct"`
		}{})

		if err := testClient.CategoryPriceTotal().RefreshConcurrently(context.Background()); err != nil {
			t.Fatalf("RefreshConcurrently: %v", err)
		}

		var out struct {
			CategoryPriceTotal *totalNode `json:"categoryPriceTotal"`
		}
		gqlExecData(t, `
			query ($id: Int!) { categoryPriceTotal(categoryID: $id) { productCount } }
		`, map[string]any{"id": catID}, &out)
		if got, want := out.CategoryPriceTotal.ProductCount, 5; got != want {
			t.Errorf("productCount after RefreshConcurrently = %d, want %d", got, want)
		}
	})
}

// --- the published schema ------------------------------------------------

// introspectedSchema is the slice of the introspection response the two
// schema-shape tests below read.
type introspectedSchema struct {
	Schema struct {
		QueryType struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"queryType"`
		MutationType struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"mutationType"`
		Types []struct {
			Name   string `json:"name"`
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
			InputFields []struct {
				Name string `json:"name"`
			} `json:"inputFields"`
		} `json:"types"`
	} `json:"__schema"`
}

func introspect(t *testing.T) introspectedSchema {
	t.Helper()
	var out introspectedSchema
	gqlExecData(t, `
		query {
			__schema {
				queryType { fields { name } }
				mutationType { fields { name } }
				types { name fields(includeDeprecated: true) { name } inputFields { name } }
			}
		}
	`, nil, &out)
	if len(out.Schema.Types) == 0 {
		t.Fatal("introspection returned no types")
	}
	return out
}

// TestViewSchema_ServesTheReadSet walks the *served* schema — not the golden
// text — and pins PRD §26.4's emitted-set table for both views: the object
// type, three envelopes, filter and sort inputs, and the three Query fields,
// plus the absence of the two mutation inputs. Doing it through introspection
// is what makes it a statement about what a consumer can call.
//
// This is presence-and-absence over a named list, not closure: the
// "and nothing else" half is `TestAPIViewSchema_declaresExactlySevenNames` in
// cmd/sqlgen/gen, which can compare against the whole declaration set of one
// view's file. Introspection sees every type in the project at once and cannot.
func TestViewSchema_ServesTheReadSet(t *testing.T) {
	schema := introspect(t)

	types := make(map[string]bool, len(schema.Schema.Types))
	for _, ty := range schema.Schema.Types {
		types[ty.Name] = true
	}
	queryFields := make(map[string]bool, len(schema.Schema.QueryType.Fields))
	for _, f := range schema.Schema.QueryType.Fields {
		queryFields[f.Name] = true
	}

	for _, v := range []struct {
		typeName  string
		queryBase string
		queryList string
		queryConn string
	}{
		{"WorkspaceNoteSummary", "workspaceNoteSummary", "workspaceNoteSummaryList", "workspaceNoteSummaries"},
		{"CategoryPriceTotal", "categoryPriceTotal", "categoryPriceTotalList", "categoryPriceTotals"},
	} {
		t.Run(v.typeName, func(t *testing.T) {
			for _, suffix := range []string{"", "Connection", "Edge", "ListResult", "Filter", "Sort", "SortField"} {
				if !types[v.typeName+suffix] {
					t.Errorf("type %q is absent from the served schema", v.typeName+suffix)
				}
			}
			for _, f := range []string{v.queryBase, v.queryList, v.queryConn} {
				if !queryFields[f] {
					t.Errorf("Query.%s is absent from the served schema", f)
				}
			}
			// The never half of §26.4's table.
			for _, absent := range []string{"Create" + v.typeName + "Input", "Update" + v.typeName + "Input"} {
				if types[absent] {
					t.Errorf("type %q exists — a view emits no mutation input", absent)
				}
			}
		})
	}
}

// TestViewSchema_NoMutationsAndNoRefresh pins the two "never" clauses that make
// a view read-only on the API: no Mutation field addresses a view, and
// Refresh / RefreshConcurrently — which the matview's Go client does expose —
// appear nowhere in the schema (§26.4, §29.2.5).
func TestViewSchema_NoMutationsAndNoRefresh(t *testing.T) {
	schema := introspect(t)

	viewTypes := []string{"WorkspaceNoteSummary", "CategoryPriceTotal"}

	// Without this the loop below would pass by vacuity if the whole mutation
	// surface ever went missing — the tables in this example do have one, so
	// "no mutation addresses a view" has to be checked against a non-empty set.
	if len(schema.Schema.MutationType.Fields) == 0 {
		t.Fatal("the served schema exposes no Mutation fields at all — the view assertion below would prove nothing")
	}

	for _, f := range schema.Schema.MutationType.Fields {
		for _, v := range viewTypes {
			// createCategoryPriceTotal / updateWorkspaceNoteSummary / … — the
			// mutation names are the view's Go struct name with a verb in
			// front, so a case-insensitive containment check catches every
			// operation without enumerating them.
			if strings.Contains(strings.ToLower(f.Name), strings.ToLower(v)) {
				t.Errorf("Mutation.%s addresses view type %s — a view emits no mutation field", f.Name, v)
			}
		}
	}

	// Refresh is a Go-client-only affordance. Nothing named after it may reach
	// the schema, on the root types or on the matview's own object type.
	for _, ty := range schema.Schema.Types {
		for _, f := range ty.Fields {
			if strings.Contains(strings.ToLower(f.Name), "refresh") {
				t.Errorf("%s.%s reaches the schema — Refresh / RefreshConcurrently are Go-client-only (§26.4)", ty.Name, f.Name)
			}
		}
		for _, f := range ty.InputFields {
			if strings.Contains(strings.ToLower(f.Name), "refresh") {
				t.Errorf("input %s.%s reaches the schema — Refresh / RefreshConcurrently are Go-client-only (§26.4)", ty.Name, f.Name)
			}
		}
	}
}
