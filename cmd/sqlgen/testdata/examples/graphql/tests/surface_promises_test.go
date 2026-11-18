package tests

// PRD §26.5 – §26.5.7 promises about the generated GraphQL surface, measured
// over HTTP against the live database: what the excluded methods fold into,
// what a missing row returns, what a nested update without its flat half
// issues, how a soft-delete comparator includes deleted rows, and the
// read-side query count — O2O joins, mutation results, child counts, chunked
// parent sets, and the fail-fast nil for an un-prefetched relationship.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbpgx "github.com/teandresmith/sqlgen/database/pgx"

	models "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models"
	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/graphql/models/graph/sqlgenresolver"
)

// trsSQLLog is a pgx.QueryTracer that records the text of every statement
// issued on its pool's connections, transactional ones included (see
// statement_counter_test.go for why a Querier shim cannot see those).
type trsSQLLog struct {
	mu  sync.Mutex
	sql []string
}

func (l *trsSQLLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	l.sql = append(l.sql, d.SQL)
	l.mu.Unlock()
	return ctx
}

func (l *trsSQLLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (l *trsSQLLog) reset() {
	l.mu.Lock()
	l.sql = nil
	l.mu.Unlock()
}

func (l *trsSQLLog) statements() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.sql)
}

// trsLoggedServer boots a GraphQL handler over its own single-connection pool
// whose statements land in the returned log.
func trsLoggedServer(t *testing.T) (string, *trsSQLLog) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testConnStr)
	if err != nil {
		t.Fatalf("parsing the container DSN: %v", err)
	}
	log := &trsSQLLog{}
	cfg.ConnConfig.Tracer = log
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("creating the traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	client := models.New(dbpgx.New(pool))
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)
	return httpsrv.URL, log
}

// trsMustExec runs one setup statement on the shared pool.
func trsMustExec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("setup %q: %v", q, err)
	}
}

// trsSeedOrders inserts n orders owned by user.
func trsSeedOrders(t *testing.T, user string, n int) {
	t.Helper()
	trsMustExec(t, `INSERT INTO orders (user_id) SELECT $1::uuid FROM generate_series(1, $2)`, user, n)
}

// trsData fails the test on GraphQL errors and decodes data into out.
func trsData(t *testing.T, resp gqlResponse, out any) {
	t.Helper()
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data (raw=%s): %v", resp.Data, err)
	}
}

// TestSurfacePromises_TotalCountOnlyListIsOneCount pins PRD §26.5.1's
// `Count` row: `<table>List(filter) { totalCount }` is "count only, no rows" —
// the §26.5.2 count-only envelope, which never reaches the entity read and
// costs exactly one `COUNT`.
func TestSurfacePromises_TotalCountOnlyListIsOneCount(t *testing.T) {
	url, log := trsLoggedServer(t)
	truncateAll(t)
	seedUser(t, "trs-count-a@example.com", "trs-count-match")
	seedUser(t, "trs-count-b@example.com", "trs-count-match")
	seedUser(t, "trs-count-c@example.com", "trs-count-other")

	tests := []struct {
		name      string
		args      string
		wantCount int
	}{
		{name: "no filter counts every row", args: "", wantCount: 3},
		{name: "a filter narrows the count", args: `(filter: { name: { eq: "trs-count-match" } })`, wantCount: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log.reset()
			var out struct {
				UserList struct {
					TotalCount int `json:"totalCount"`
				} `json:"userList"`
			}
			trsData(t, postGQL(t, url, fmt.Sprintf(`{ userList%s { totalCount } }`, tt.args), nil), &out)
			if out.UserList.TotalCount != tt.wantCount {
				t.Errorf("totalCount = %d, want %d", out.UserList.TotalCount, tt.wantCount)
			}
			stmts := log.statements()
			if len(stmts) != 1 {
				t.Fatalf("a totalCount-only selection issued %d statements, want exactly one COUNT:\n%s", len(stmts), strings.Join(stmts, "\n"))
			}
			if !strings.Contains(strings.ToUpper(stmts[0]), "COUNT(") {
				t.Errorf("the one statement is not a COUNT:\n%s", stmts[0])
			}
		})
	}
}

// TestSurfacePromises_SoftDeleteAndRestoreOfMissingRowReturnNull pins PRD
// §26.5.1's mutation-result nullability: soft delete and restore return
// nullable `<T>` "so the client can distinguish 'not found' (null) from
// 'internal error' (GraphQL error)".
func TestSurfacePromises_SoftDeleteAndRestoreOfMissingRowReturnNull(t *testing.T) {
	truncateAll(t)
	missing := uuid.New().String()
	tests := []struct {
		name     string
		mutation string
		field    string
	}{
		{name: "soft delete", mutation: `mutation ($id: UUID!) { softDeleteUser(id: $id) { id } }`, field: "softDeleteUser"},
		{name: "restore", mutation: `mutation ($id: UUID!) { restoreUser(id: $id) { id } }`, field: "restoreUser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := gqlExec(t, tt.mutation, map[string]any{"id": missing}, nil)
			if len(resp.Errors) > 0 {
				t.Fatalf("%s of a missing row raised errors %+v, want a null result", tt.field, resp.Errors)
			}
			var out map[string]json.RawMessage
			trsData(t, resp, &out)
			got, ok := out[tt.field]
			if !ok || string(got) != "null" {
				t.Errorf("%s = %s (present=%v), want null", tt.field, got, ok)
			}
		})
	}
}

// TestSurfacePromises_HardDeleteMissingRowReturnsTrue pins PRD §26.5.1 "Hard
// delete returns `Boolean!`: `true` once the delete ran without error, whether
// or not the row existed", since the client's HardDelete is idempotent —
// through both spellings: `hardDelete<T>` beside a soft delete, and the bare
// `delete<T>`.
func TestSurfacePromises_HardDeleteMissingRowReturnsTrue(t *testing.T) {
	truncateAll(t)
	tests := []struct {
		name     string
		mutation string
		vars     map[string]any
		field    string
	}{
		{name: "hardDelete on a soft-delete table", mutation: `mutation ($id: UUID!) { hardDeleteUser(id: $id) }`, vars: map[string]any{"id": uuid.New().String()}, field: "hardDeleteUser"},
		{name: "bare delete on a hard-only table", mutation: `mutation ($id: Int!) { deleteCategory(id: $id) }`, vars: map[string]any{"id": 987654321}, field: "deleteCategory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out map[string]json.RawMessage
			trsData(t, gqlExec(t, tt.mutation, tt.vars, nil), &out)
			if got := string(out[tt.field]); got != "true" {
				t.Errorf("%s of a missing row = %s, want true", tt.field, got)
			}
		})
	}
}

// trsUpdateUsersRE matches an UPDATE whose target is the users table.
var trsUpdateUsersRE = regexp.MustCompile(`(?is)^\s*UPDATE\s+("public"\.)?"users"`)

// TestSurfacePromises_NestedUpdateWithoutFlatMemberIssuesNoParentUpdate pins
// PRD §26.5.1: an update that omits the flat member, "the parent write then
// issues no `UPDATE` at all". The control sets the flat member and does.
func TestSurfacePromises_NestedUpdateWithoutFlatMemberIssuesNoParentUpdate(t *testing.T) {
	truncateAll(t)
	user := seedUserRow(t, "trs-nested-noupdate@example.com")
	url, log := trsLoggedServer(t)

	tests := []struct {
		name       string
		input      string
		wantUpdate bool
	}{
		{name: "nested members only", input: `{ events: { create: [{ action: "trs-only-nested", occurredAt: $occ }] } }`, wantUpdate: false},
		{name: "flat member set", input: `{ user: { name: "trs-renamed" }, events: { create: [{ action: "trs-with-flat", occurredAt: $occ }] } }`, wantUpdate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log.reset()
			var out struct {
				UpdateUserWithRelated struct {
					ID     string `json:"id"`
					Events []struct {
						ID string `json:"id"`
					} `json:"events"`
				} `json:"updateUserWithRelated"`
			}
			trsData(t, postGQL(t, url, `mutation ($id: UUID!, $occ: DateTime!) {
				updateUserWithRelated(id: $id, input: `+tt.input+`) { id events { id } }
			}`, map[string]any{"id": user.String(), "occ": nestedGQLOccurredAt}), &out)
			if len(out.UpdateUserWithRelated.Events) == 0 {
				t.Fatalf("the nested create did not land: %+v", out.UpdateUserWithRelated)
			}
			var updates []string
			for _, q := range log.statements() {
				if trsUpdateUsersRE.MatchString(q) {
					updates = append(updates, q)
				}
			}
			if got := len(updates) > 0; got != tt.wantUpdate {
				t.Errorf("issued an UPDATE on users = %v, want %v; statements:\n%v", got, tt.wantUpdate, log.statements())
			}
		})
	}
}

// TestSurfacePromises_EmptyDeletedAtComparatorIncludesSoftDeleted pins PRD
// §26.5.3 "To include soft-deleted rows, the GraphQL caller passes `deletedAt:
// {}` (an empty comparator that adds no SQL but defeats the auto-inject)".
func TestSurfacePromises_EmptyDeletedAtComparatorIncludesSoftDeleted(t *testing.T) {
	truncateAll(t)
	live := seedUser(t, "trs-live@example.com", "trs-softdel")
	gone := seedUser(t, "trs-gone@example.com", "trs-softdel")
	var del struct {
		SoftDeleteUser struct {
			ID string `json:"id"`
		} `json:"softDeleteUser"`
	}
	gqlExecData(t, `mutation ($id: UUID!) { softDeleteUser(id: $id) { id } }`, map[string]any{"id": gone.ID}, &del)
	if del.SoftDeleteUser.ID != gone.ID {
		t.Fatalf("softDeleteUser = %+v, want %s", del.SoftDeleteUser, gone.ID)
	}

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "default excludes the soft-deleted row", filter: `{ name: { eq: "trs-softdel" } }`, want: []string{live.ID}},
		{name: "deletedAt: {} includes it", filter: `{ name: { eq: "trs-softdel" }, deletedAt: {} }`, want: []string{live.ID, gone.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out struct {
				UserList struct {
					Items []struct {
						ID string `json:"id"`
					} `json:"items"`
				} `json:"userList"`
			}
			gqlExecData(t, `{ userList(filter: `+tt.filter+`) { items { id } } }`, nil, &out)
			var got []string
			for _, it := range out.UserList.Items {
				got = append(got, it.ID)
			}
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tt.want))
			if !slices.Equal(got, want) {
				t.Errorf("userList ids = %v, want %v", got, want)
			}
		})
	}
}

// TestSurfacePromises_O2OSelectionIsOneQuery pins PRD §26.5.7's
// `product(id: 1) { company { name } }` row: a get selecting an O2O is one
// `Get` with the O2O JOIN'd — this example's O2O edges are `User.badge` and
// `Asset.primaryDocument`.
func TestSurfacePromises_O2OSelectionIsOneQuery(t *testing.T) {
	truncateAll(t)
	user := seedUserRow(t, "trs-o2o@example.com")
	trsMustExec(t, `INSERT INTO user_badges (user_id, label) VALUES ($1, 'trs-badge')`, user)
	assetIDs := seedO2OSortAssets(t, 1)

	url, counter := newCountingHandler(t)
	tests := []struct {
		name  string
		query string
		id    string
		want  string
	}{
		{name: "user badge", query: `query ($id: UUID!) { user(id: $id) { name badge { label } } }`, id: user.String(), want: `"label":"trs-badge"`},
		{name: "asset primary document", query: `query ($id: UUID!) { asset(id: $id) { name primaryDocument { name } } }`, id: assetIDs[0], want: `"name":"o2o-sort-0-doc"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter.reset()
			resp := postGQL(t, url, tt.query, map[string]any{"id": tt.id})
			if len(resp.Errors) > 0 {
				t.Fatalf("graphql errors: %+v", resp.Errors)
			}
			if !regexp.MustCompile(regexp.QuoteMeta(tt.want)).Match(resp.Data) {
				t.Errorf("data = %s, want the O2O target carrying %s", resp.Data, tt.want)
			}
			if got := counter.count(); got != 1 {
				t.Errorf("queries = %d, want 1 (the O2O is joined into the Get)", got)
			}
		})
	}
}

// TestSurfacePromises_MutationResultLoadsRelationshipsThroughFieldOptions pins
// PRD §26.5.7's `createProduct(input) { reviews { count } }` row: a mutation's
// result relationships come back populated, loaded by the read the mutation
// chains internally with the request's FieldOptions — so selecting them adds
// exactly the §25.1 loader terms (O2M one, M2M two) to the mutation's own
// statements, never a per-child or lazy load.
func TestSurfacePromises_MutationResultLoadsRelationshipsThroughFieldOptions(t *testing.T) {
	truncateAll(t)
	user := seedUserRow(t, "trs-mutres@example.com")
	trsSeedOrders(t, user.String(), 3)
	cat := seedCategoryRow(t, "trs-mutres-cat")
	trsMustExec(t, `INSERT INTO user_categories (user_id, category_id) VALUES ($1, $2)`, user, cat)

	url, counter := newCountingHandler(t)
	run := func(t *testing.T, selection string) (int, int, int) {
		t.Helper()
		counter.reset()
		var out struct {
			UpdateUser struct {
				Name   string            `json:"name"`
				Orders []json.RawMessage `json:"orders"`
				Cats   []json.RawMessage `json:"categories"`
			} `json:"updateUser"`
		}
		trsData(t, postGQL(t, url, `mutation ($id: UUID!) { updateUser(id: $id, input: { name: "trs-mutres-renamed" }) { `+selection+` } }`,
			map[string]any{"id": user.String()}), &out)
		if out.UpdateUser.Name != "trs-mutres-renamed" {
			t.Errorf("name = %q, want the update applied", out.UpdateUser.Name)
		}
		return counter.count(), len(out.UpdateUser.Orders), len(out.UpdateUser.Cats)
	}
	base, _, _ := run(t, `id name`)

	tests := []struct {
		name       string
		selection  string
		wantExtra  int
		wantOrders int
		wantCats   int
	}{
		{name: "one O2M adds one query", selection: `id name orders { id }`, wantExtra: 1, wantOrders: 3},
		{name: "one O2M and one M2M add three", selection: `id name orders { id } categories { id }`, wantExtra: 3, wantOrders: 3, wantCats: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, orders, cats := run(t, tt.selection)
			if orders != tt.wantOrders || cats != tt.wantCats {
				t.Errorf("result relationships = %d orders, %d categories, want %d and %d", orders, cats, tt.wantOrders, tt.wantCats)
			}
			if got-base != tt.wantExtra {
				t.Errorf("statements = %d, %d over the relationship-free mutation's %d, want %d over", got, got-base, base, tt.wantExtra)
			}
		})
	}
}

// TestSurfacePromises_QueryCountIndependentOfChildren pins PRD §26.5.7 "The
// count never depends on the number of children": one order or forty, the
// same selection costs the same queries.
func TestSurfacePromises_QueryCountIndependentOfChildren(t *testing.T) {
	url, counter := newCountingHandler(t)
	tests := []struct {
		name     string
		children int
	}{
		{name: "one child", children: 1},
		{name: "forty children", children: 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAll(t)
			user := seedUserRow(t, "trs-children@example.com")
			trsSeedOrders(t, user.String(), tt.children)
			counter.reset()
			var out struct {
				User struct {
					Orders []json.RawMessage `json:"orders"`
				} `json:"user"`
			}
			trsData(t, postGQL(t, url, `query ($id: UUID!) { user(id: $id) { id orders { id total } } }`, map[string]any{"id": user.String()}), &out)
			if len(out.User.Orders) != tt.children {
				t.Fatalf("orders = %d, want %d", len(out.User.Orders), tt.children)
			}
			if got := counter.count(); got != 2 {
				t.Errorf("queries = %d, want 2 (1 + one O2M) whatever the child count", got)
			}
		})
	}
}

// trsChunk is the per-statement key chunk on PostgreSQL for a one-column key
// (PRD §25.1: floor(65535 / 2 / 1)).
const trsChunk = 32767

// TestSurfacePromises_ChunkedParentSetAddsOneQueryPerChunk pins PRD §26.5 /
// §26.5.7: "a larger parent set adds one query per extra chunk to each chunked
// step". `category(id) { users { categories { id } } }` loads the users M2M
// over one parent (two steps), then the categories M2M over every user (two
// chunked steps, junction and target). One parent past the chunk adds one
// query to each of those two steps.
func TestSurfacePromises_ChunkedParentSetAddsOneQueryPerChunk(t *testing.T) {
	truncateAll(t)
	t.Cleanup(func() { truncateAll(t) })
	var cat int64
	if err := testPool.QueryRow(context.Background(), `INSERT INTO categories (name) VALUES ('trs-chunk') RETURNING id`).Scan(&cat); err != nil {
		t.Fatalf("seeding the category: %v", err)
	}
	addUsers := func(t *testing.T, from, to int) {
		t.Helper()
		trsMustExec(t, `INSERT INTO users (email, name) SELECT format('trs-chunk-%s@example.com', i), 'trs-chunk' FROM generate_series($1::int, $2::int) AS i`, from, to)
		trsMustExec(t, `INSERT INTO user_categories (user_id, category_id) SELECT id, $1 FROM users WHERE name = 'trs-chunk' ON CONFLICT DO NOTHING`, cat)
	}

	url, counter := newCountingHandler(t)
	tests := []struct {
		name      string
		upTo      int
		wantQuery int
	}{
		// 1 Get + 2 (users M2M, one parent) + 2 × 1 chunk (categories M2M).
		{name: "a parent set of exactly one chunk", upTo: trsChunk, wantQuery: 5},
		// One user more: the categories M2M runs two chunks of each step.
		{name: "one parent past the chunk", upTo: trsChunk + 1, wantQuery: 7},
	}
	seeded := 0
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addUsers(t, seeded+1, tt.upTo)
			seeded = tt.upTo
			counter.reset()
			var out struct {
				Category struct {
					Users []struct {
						Categories []struct {
							ID int64 `json:"id"`
						} `json:"categories"`
					} `json:"users"`
				} `json:"category"`
			}
			trsData(t, postGQL(t, url, `query ($id: Int!) { category(id: $id) { users { categories { id } } } }`, map[string]any{"id": cat}), &out)
			if len(out.Category.Users) != tt.upTo {
				t.Fatalf("users = %d, want %d", len(out.Category.Users), tt.upTo)
			}
			for i, u := range out.Category.Users {
				if len(u.Categories) != 1 || u.Categories[0].ID != cat {
					t.Fatalf("user %d categories = %+v, want exactly [%d]", i, u.Categories, cat)
				}
			}
			if got := counter.count(); got != tt.wantQuery {
				t.Errorf("queries = %d, want %d", got, tt.wantQuery)
			}
		})
	}
}

// trsUnprefetchedRoot serves the generated schema with one query swapped out:
// `user(id)` returns a row read with no FieldOptions, so none of its
// relationships is prefetched — the state a walker that missed a case would
// leave behind.
type trsUnprefetchedRoot struct {
	*graph.Resolver
	client *models.Client
}

func (r trsUnprefetchedRoot) Query() graph.QueryResolver {
	return trsUnprefetchedQuery{QueryResolver: r.Resolver.Query(), client: r.client}
}

type trsUnprefetchedQuery struct {
	graph.QueryResolver
	client *models.Client
}

func (q trsUnprefetchedQuery) User(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return q.client.Users().Get(ctx, id)
}

// TestSurfacePromises_UnprefetchedRelationshipResolvesNilWithoutLoading pins
// PRD §26.5.2 "Selected ⇒ available, unselected ⇒ nil … the field resolver
// returns nil — it does **not** lazy-load" and §26.5.7 "the child resolver
// returns nil". The user has a badge, orders and a category in the database;
// selected over an un-prefetched row they come back empty and cost no query
// past the row's own read.
func TestSurfacePromises_UnprefetchedRelationshipResolvesNilWithoutLoading(t *testing.T) {
	truncateAll(t)
	user := seedUserRow(t, "trs-failfast@example.com")
	trsSeedOrders(t, user.String(), 2)
	trsMustExec(t, `INSERT INTO user_badges (user_id, label) VALUES ($1, 'trs-failfast')`, user)
	cat := seedCategoryRow(t, "trs-failfast-cat")
	trsMustExec(t, `INSERT INTO user_categories (user_id, category_id) VALUES ($1, $2)`, user, cat)

	counter := &countingQuerier{inner: dbpgx.New(testPool)}
	client := models.New(counter)
	root := trsUnprefetchedRoot{
		Resolver: &graph.Resolver{Client: client, Q: &sqlgenresolver.Q{Client: client}, M: &sqlgenresolver.M{Client: client}},
		client:   client,
	}
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: root}))
	httpsrv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(srv))
	t.Cleanup(httpsrv.Close)

	counter.reset()
	var out struct {
		User struct {
			ID         string            `json:"id"`
			Badge      *json.RawMessage  `json:"badge"`
			Orders     []json.RawMessage `json:"orders"`
			Categories []json.RawMessage `json:"categories"`
		} `json:"user"`
	}
	trsData(t, postGQL(t, httpsrv.URL, `query ($id: UUID!) { user(id: $id) { id badge { label } orders { id } categories { id } } }`,
		map[string]any{"id": user.String()}), &out)
	if out.User.ID != user.String() {
		t.Fatalf("user = %+v, want the seeded row", out.User)
	}
	if out.User.Badge != nil {
		t.Errorf("badge = %s, want null for an un-prefetched O2O", *out.User.Badge)
	}
	if len(out.User.Orders) != 0 || len(out.User.Categories) != 0 {
		t.Errorf("orders = %d, categories = %d, want both empty for an un-prefetched relationship", len(out.User.Orders), len(out.User.Categories))
	}
	if got := counter.count(); got != 1 {
		t.Errorf("queries = %d, want 1 (the row's own read; no relationship is lazy-loaded)", got)
	}
}
