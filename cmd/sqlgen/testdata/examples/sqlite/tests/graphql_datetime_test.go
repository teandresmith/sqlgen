package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"

	graph "github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models/graph"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/testdata/examples/sqlite/models/graph/sqlgenresolver"
)

// newGraphQLServer wires the generated graph package against the shared test
// database and returns a live gqlgen handler. The middleware wrap is the same
// one the graphql example uses — sqlgenresolver's call options ride on the
// request context.
func newGraphQLServer(t *testing.T) *httptest.Server {
	t.Helper()

	client := newClient()
	resolver := &graph.Resolver{
		Client: client,
		Q:      &sqlgenresolver.Q{Client: client},
		M:      &sqlgenresolver.M{Client: client},
	}
	es := graph.NewExecutableSchema(graph.Config{Resolvers: resolver})
	srv := httptest.NewServer(sqlgenresolver.WithCallOptionsMiddleware(handler.NewDefaultServer(es)))
	t.Cleanup(srv.Close)
	return srv
}

// cleanupUsers removes the rows this file seeds. Every other test in this
// package scopes its `users` assertions by id or name, so leftovers are
// invisible today — but `email` is UNIQUE, so without this a second run in the
// same database (`go test -count=2`) fails on a duplicate email, and any future
// unscoped `users` assertion would inherit the rows.
func cleanupUsers(t *testing.T, emails ...string) {
	t.Helper()

	t.Cleanup(func() {
		for _, email := range emails {
			if _, err := testDB.Exec(`DELETE FROM users WHERE email = ?`, email); err != nil {
				t.Errorf("cleaning up user %s: %v", email, err)
			}
		}
	})
}

// gqlPost posts a GraphQL query to srv and returns the raw JSON response body.
func gqlPost(t *testing.T, srv *httptest.Server, query string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatalf("marshaling query: %v", err)
	}
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body)) //nolint:noctx // test helper against a local httptest server
	if err != nil {
		t.Fatalf("posting query: %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	return string(out)
}

// createdUserID decodes the id from a createUser mutation response.
func createdUserID(t *testing.T, resp string) int64 {
	t.Helper()

	var decoded struct {
		Data struct {
			CreateUser struct {
				ID int64 `json:"id"`
			} `json:"createUser"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &decoded); err != nil {
		t.Fatalf("decoding createUser response: %v\ngot: %s", err, resp)
	}
	if decoded.Data.CreateUser.ID == 0 {
		t.Fatalf("createUser returned no id: %s", resp)
	}
	return decoded.Data.CreateUser.ID
}

// TestGraphQLDateTimeRoundTrip is the reason this example carries a GraphQL
// surface at all.
//
// SQLite has no date/time storage class, so `registerDialectDefaults`
// (cmd/sqlgen/gotype/gotype.go) registers types.DateTime / types.NullDateTime
// as the DIALECT DEFAULT for datetime / timestamp / date columns — no
// `overrides.types` block declares them anywhere in this module. Every other
// module that compiles those two Go types into a gqlgen schema reaches them
// through an explicit override (the `graphql` example's `events` table), so
// the defaulting path itself had no GraphQL proof until this test.
//
// `users.created_at` is NOT NULL (types.DateTime → DateTime!) and
// `users.updated_at` is nullable (types.NullDateTime → NullDateTime); neither
// is a soft-delete column, so a row written here stays visible to the
// read-back query.
//
// The assertions run through a live handler against a real SQLite database, so
// a marshaler that round-trips on the wire while storing something else in the
// TEXT column fails here: the stored value is read back through a second query
// rather than trusted from the mutation's own response.
func TestGraphQLDateTimeRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	srv := newGraphQLServer(t)
	cleanupUsers(t, "gqldatetime-datetime@example.com", "gqldatetime-null-datetime@example.com")

	// Both DateTime shapes supplied, so the input translator's non-null arm and
	// its Null-wrapper arm are exercised on write.
	got := gqlPost(t, srv, `mutation {
		createUser(input: {
			name: "datetime probe"
			email: "gqldatetime-datetime@example.com"
			role: "viewer"
			isActive: true
			balance: 0.0
			loginCount: 0
			createdAt: "2026-01-02T03:04:05Z"
			updatedAt: "2026-03-04T05:06:07Z"
		}) { id createdAt updatedAt }
	}`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("createUser returned errors: %s", got)
	}
	for _, want := range []string{`"createdAt":"2026-01-02T03:04:05Z"`, `"updatedAt":"2026-03-04T05:06:07Z"`} {
		if !strings.Contains(got, want) {
			t.Errorf("createUser response missing %s\ngot: %s", want, got)
		}
	}
	id := createdUserID(t, got)

	// Read back through a separate query so the assertion covers what SQLite
	// actually stored, not just what the mutation echoed.
	got = gqlPost(t, srv, `query { user(id: `+strconv.FormatInt(id, 10)+`) { createdAt updatedAt } }`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("user query returned errors: %s", got)
	}
	for _, want := range []string{`"createdAt":"2026-01-02T03:04:05Z"`, `"updatedAt":"2026-03-04T05:06:07Z"`} {
		if !strings.Contains(got, want) {
			t.Errorf("user read-back missing %s\ngot: %s", want, got)
		}
	}

	// A stored NULL must marshal back as JSON null rather than a zero timestamp
	// — the NullDateTime scalar's Valid=false arm.
	//
	// The NULL is seeded through SQL rather than the mutation on purpose:
	// `users.updated_at` carries a SQLite-side DEFAULT (datetime('now')), and a
	// gqlgen input cannot ask for NULL anyway — a nullable input field arrives
	// as a nil pointer whether it was omitted or written as an explicit `null`,
	// so both are treated as unset and the database default fills in. What is
	// testable here, and what the scalar owns, is the read direction.
	if _, err := testDB.Exec(
		`INSERT INTO users (name, email, role, is_active, balance, login_count, created_at, updated_at)
		 VALUES (?, ?, 'viewer', 1, 0.0, 0, ?, NULL)`,
		"null datetime probe", "gqldatetime-null-datetime@example.com", "2026-05-06T07:08:09Z",
	); err != nil {
		t.Fatalf("seeding NULL updated_at: %v", err)
	}

	got = gqlPost(t, srv, `query {
		userList(filter: {email: {eq: "gqldatetime-null-datetime@example.com"}}) {
			items { createdAt updatedAt }
		}
	}`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("userList (null updatedAt) returned errors: %s", got)
	}
	if !strings.Contains(got, `"updatedAt":null`) {
		t.Errorf("stored NULL did not marshal as JSON null\ngot: %s", got)
	}
	if !strings.Contains(got, `"createdAt":"2026-05-06T07:08:09Z"`) {
		t.Errorf("DateTime did not parse back from SQLite TEXT storage\ngot: %s", got)
	}
}

// TestGraphQLDateTimeFilter exercises the comparator translators for both
// DateTime shapes — TimeComparator over the NOT NULL column and
// NullableTimeComparator over the wrapper — so the filter half of the
// dialect-default binding is covered alongside the field half.
func TestGraphQLDateTimeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	srv := newGraphQLServer(t)
	cleanupUsers(t, "gqldatetime-filter@example.com")

	got := gqlPost(t, srv, `mutation {
		createUser(input: {
			name: "filter probe"
			email: "gqldatetime-filter@example.com"
			role: "viewer"
			isActive: true
			balance: 0.0
			loginCount: 0
			createdAt: "2030-01-01T00:00:00Z"
		}) { id }
	}`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("createUser returned errors: %s", got)
	}

	got = gqlPost(t, srv, `query {
		userList(filter: {
			email: {eq: "gqldatetime-filter@example.com"}
			createdAt: {gt: "2029-01-01T00:00:00Z"}
			updatedAt: {isNull: false}
		}) { items { name createdAt } }
	}`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("userList returned errors: %s", got)
	}
	if !strings.Contains(got, `"name":"filter probe"`) {
		t.Errorf("DateTime filter did not match the seeded row\ngot: %s", got)
	}

	// Negative case. Without it the positive query above proves nothing about
	// the comparator: it is ANDed with an `email` equality that already narrows
	// to one row, so a TimeComparator that translated to an always-true
	// predicate would still pass. This bound excludes the seeded row on the
	// DateTime column alone.
	got = gqlPost(t, srv, `query {
		userList(filter: {
			email: {eq: "gqldatetime-filter@example.com"}
			createdAt: {lt: "2000-01-01T00:00:00Z"}
		}) { items { name } }
	}`)
	if strings.Contains(got, `"errors"`) {
		t.Fatalf("userList (negative) returned errors: %s", got)
	}
	if strings.Contains(got, `"name":"filter probe"`) {
		t.Errorf("createdAt lt 2000 matched a row stamped 2030; the comparator is not discriminating\ngot: %s", got)
	}
}
