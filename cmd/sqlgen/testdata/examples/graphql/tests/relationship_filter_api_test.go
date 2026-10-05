package tests

import (
	"context"
	"slices"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
)

// Relationship filters over the real gqlgen server.
//
// The model layer puts a `*<Target>Filter` member on every model-side
// `<T>Filter` and compiles it to a correlated EXISTS. Without the API
// projection, the consumer's actual story — "give me the tasks that are DONE
// and assigned to this person, in one query, without a reverse-navigation
// workaround" — has no GraphQL spelling. These tests drive that story end to
// end against a real PostgreSQL through the same handler a consumer runs.
//
// Every assertion is a STRICT SUBSET of the seeded rows, so the old behaviour
// (schema rejects the field ⇒ the query never parses; a schema-only
// implementation ⇒ the model filter receives nil and the server answers with
// the UNFILTERED set) fails each one.

// relFilterSeed is the fixture the O2M and M2M cases share. Users are seeded
// through the pool rather than through createUser so the shape of the fixture
// is visible in one place and the seeding path is not the surface under test.
type relFilterSeed struct {
	alice uuid.UUID // active, one order noted "priority", member of "books"
	bob   uuid.UUID // active, one order noted "standard", member of "music"
	carol uuid.UUID // INACTIVE, one order noted "priority"
	dave  uuid.UUID // active, no orders at all
}

func seedRelFilterUsers(t *testing.T) relFilterSeed {
	t.Helper()
	truncateAll(t)

	mkUser := func(email, name string, active bool) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := testPool.QueryRow(
			context.Background(),
			`INSERT INTO users (email, name, is_active) VALUES ($1, $2, $3) RETURNING id`,
			email, name, active,
		).Scan(&id); err != nil {
			t.Fatalf("seed user %q: %v", email, err)
		}
		return id
	}
	mkOrder := func(user uuid.UUID, notes string) {
		t.Helper()
		if _, err := testPool.Exec(
			context.Background(),
			`INSERT INTO orders (user_id, total, notes) VALUES ($1, 10, $2)`,
			user, notes,
		); err != nil {
			t.Fatalf("seed order for %s: %v", user, err)
		}
	}

	seed := relFilterSeed{
		alice: mkUser("alice@example.com", "Alice", true),
		bob:   mkUser("bob@example.com", "Bob", true),
		carol: mkUser("carol@example.com", "Carol", false),
		dave:  mkUser("dave@example.com", "Dave", true),
	}
	mkOrder(seed.alice, "priority")
	mkOrder(seed.bob, "standard")
	// Carol also has a priority order — so a filter that only looked at the
	// relationship, ignoring the sibling column predicate, would return her too.
	mkOrder(seed.carol, "priority")
	return seed
}

// userListIDs runs `userList` with the given filter and returns the matched
// ids, sorted so assertions are order-independent.
//
// `varDefs` is spelled per case: GraphQL rejects a document declaring a
// variable it never uses, so one shared union of every case's variables would
// fail validation on all of them.
func userListIDs(t *testing.T, varDefs, filter string, vars map[string]any) []string {
	t.Helper()
	query := `query ` + varDefs + ` {
		userList(filter: ` + filter + `) {
			items { id }
			totalCount
		}
	}`
	var out struct {
		UserList struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			TotalCount int `json:"totalCount"`
		} `json:"userList"`
	}
	gqlExecData(t, query, vars, &out)
	ids := make([]string, 0, len(out.UserList.Items))
	for _, item := range out.UserList.Items {
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	if got, want := out.UserList.TotalCount, len(ids); got != want {
		t.Errorf("totalCount = %d but %d items returned — the count and the page took different WHERE clauses", got, want)
	}
	return ids
}

func sortedIDs(ids ...uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}

// TestRelationshipFilterAPI_ColumnAndRelationshipInOneQuery is the core pin,
// in the shape the consumer reported it: one filter carrying a column predicate
// AND a relationship predicate, resolved in a single query with no reverse
// navigation. `tasks(filter: {status: DONE, assignees: {id: {eq: X}}})` reads
// here as `userList(filter: {isActive: {eq: true}, orders: {notes: …}})`.
//
// Carol exists precisely so this cannot pass by accident: she satisfies the
// relationship half and fails the column half, so a server that honoured only
// one of the two returns a different set.
func TestRelationshipFilterAPI_ColumnAndRelationshipInOneQuery(t *testing.T) {
	seed := seedRelFilterUsers(t)

	got := userListIDs(
		t,
		`($notes: String!)`,
		`{isActive: {eq: true}, orders: {notes: {eq: $notes}}}`,
		map[string]any{"notes": "priority"},
	)
	if diff := cmp.Diff(sortedIDs(seed.alice), got); diff != "" {
		t.Errorf("userList(isActive + orders.notes) (-want +got):\n%s", diff)
	}
}

// TestRelationshipFilterAPI_RelationshipUnderOr pins the composition half: a
// relationship member is an ordinary condition inside `or`, subject to exactly
// the §11.1 composition rule every column comparator follows.
//
// That rule has two halves and both are asserted here, because a relationship
// member that silently dropped out of one would still pass the other. Members
// of `or` are OR'd together; a single member's OWN fields are AND'd, since a
// filter object is always a conjunction of its own fields.
func TestRelationshipFilterAPI_RelationshipUnderOr(t *testing.T) {
	seed := seedRelFilterUsers(t)

	// Two entries ⇒ union, which is the reading the syntax suggests. Alice and
	// Carol have a priority order; Bob is named Bob. Dave matches neither,
	// which is what makes this a union rather than "everything".
	union := userListIDs(
		t,
		`($notes: String!, $name: String!)`,
		`{or: [{orders: {notes: {eq: $notes}}}, {name: {eq: $name}}]}`,
		map[string]any{"notes": "priority", "name": "Bob"},
	)
	if diff := cmp.Diff(sortedIDs(seed.alice, seed.bob, seed.carol), union); diff != "" {
		t.Errorf("userList(or[{orders}, {name}]) (-want +got):\n%s", diff)
	}

	// One entry carrying both predicates ⇒ intersection, because a member's own
	// fields are conjunctive. Alice satisfies both; Carol has the priority
	// order but is inactive, Bob is active without one.
	intersection := userListIDs(
		t,
		`($notes: String!)`,
		`{or: [{orders: {notes: {eq: $notes}}, isActive: {eq: true}}]}`,
		map[string]any{"notes": "priority"},
	)
	if diff := cmp.Diff(sortedIDs(seed.alice), intersection); diff != "" {
		t.Errorf("userList(or[{orders, isActive}]) (-want +got):\n%s", diff)
	}
}

// TestRelationshipFilterAPI_ManyToMany drives the junction shape through the
// API. The model-side tests prove the single-EXISTS-over-the-junction SQL;
// what is new here is that `users: UserFilter` on `CategoryFilter` reaches it.
func TestRelationshipFilterAPI_ManyToMany(t *testing.T) {
	seed := seedRelFilterUsers(t)

	mkCategory := func(name string) int64 {
		t.Helper()
		var id int64
		if err := testPool.QueryRow(
			context.Background(),
			`INSERT INTO categories (name) VALUES ($1) RETURNING id`, name,
		).Scan(&id); err != nil {
			t.Fatalf("seed category %q: %v", name, err)
		}
		return id
	}
	link := func(user uuid.UUID, category int64) {
		t.Helper()
		if _, err := testPool.Exec(
			context.Background(),
			`INSERT INTO user_categories (user_id, category_id) VALUES ($1, $2)`, user, category,
		); err != nil {
			t.Fatalf("link user %s to category %d: %v", user, category, err)
		}
	}

	books := mkCategory("books")
	music := mkCategory("music")
	mkCategory("orphan") // no members — must not match
	link(seed.alice, books)
	link(seed.bob, music)

	var out struct {
		CategoryList struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		} `json:"categoryList"`
	}
	gqlExecData(t, `
		query ($email: String!) {
			categoryList(filter: {users: {email: {eq: $email}}}) {
				items { name }
			}
		}
	`, map[string]any{"email": "alice@example.com"}, &out)

	names := make([]string, 0, len(out.CategoryList.Items))
	for _, item := range out.CategoryList.Items {
		names = append(names, item.Name)
	}
	slices.Sort(names)
	if diff := cmp.Diff([]string{"books"}, names); diff != "" {
		t.Errorf("categoryList(users.email) (-want +got):\n%s", diff)
	}
}

// TestRelationshipFilterAPI_QueryCountUnchanged pins the §25.1 guarantee for
// the new surface: the EXISTS is a predicate inside the list's own statement,
// not an extra round-trip. A relationship-filtered list must cost exactly what
// an unfiltered one costs.
func TestRelationshipFilterAPI_QueryCountUnchanged(t *testing.T) {
	seedRelFilterUsers(t)
	url, counter := newCountingHandler(t)

	run := func(filterArg string) int {
		t.Helper()
		counter.reset()
		resp := postGQL(t, url, `query { userList`+filterArg+` { items { id } totalCount } }`, nil)
		if len(resp.Errors) > 0 {
			t.Fatalf("graphql errors for %s: %+v", filterArg, resp.Errors)
		}
		return counter.count()
	}

	plain := run("")
	filtered := run(`(filter: {orders: {notes: {eq: "priority"}}})`)
	if plain != filtered {
		t.Errorf("relationship-filtered list issued %d queries, unfiltered issued %d — the EXISTS must be a predicate, not a round-trip", filtered, plain)
	}
	if plain == 0 {
		t.Fatal("counting handler observed no queries; the comparison above is vacuous")
	}
}

// --- tenanted target + self-reference -------------------------------------
//
// `workspace_notes.parent_id` is a self-referential FK onto a TENANTED table,
// which is what makes the two cases below expressible in this example at all
// (see the fixture note in schema.sql).

// seedNote inserts one workspace note, optionally as the child of another.
func seedNote(t *testing.T, tenant uuid.UUID, body string, parent *uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testPool.QueryRow(
		context.Background(),
		`INSERT INTO workspace_notes (workspace_id, parent_id, body, pinned_order, kind, labels)
		 VALUES ($1, $2, $3, 0, 'draft', '{}'::jsonb) RETURNING id`,
		tenant, parent, body,
	).Scan(&id); err != nil {
		t.Fatalf("seed workspace_note(%s, %q): %v", tenant, body, err)
	}
	return id
}

// noteListBodies runs `workspaceNoteList` as one tenant and returns the bodies
// it matched, sorted.
func noteListBodies(t *testing.T, url string, tenant uuid.UUID, filter string, vars map[string]any) []string {
	t.Helper()
	query := `query ($body: String!) {
		workspaceNoteList(filter: ` + filter + `) {
			items { body }
		}
	}`
	resp := postGQLAt(t, url, query, vars, map[string]string{"X-Workspace-ID": tenant.String()})
	if len(resp.Errors) > 0 {
		t.Fatalf("tenant %s: graphql errors: %+v", tenant, resp.Errors)
	}
	var out struct {
		WorkspaceNoteList struct {
			Items []struct {
				Body string `json:"body"`
			} `json:"items"`
		} `json:"workspaceNoteList"`
	}
	decodeData(t, resp, &out)
	bodies := make([]string, 0, len(out.WorkspaceNoteList.Items))
	for _, item := range out.WorkspaceNoteList.Items {
		bodies = append(bodies, item.Body)
	}
	slices.Sort(bodies)
	return bodies
}

// TestRelationshipFilterAPI_TenantedTargetIsScoped is the subquery tenant
// injection reaching the API. Without the tenant predicate INSIDE the subquery, a
// relationship filter is a cross-tenant existence probe: tenant A asks "does
// any of my notes have a child whose body is X" and learns the answer for a
// row owned by tenant B.
//
// The fixture plants exactly that row — a child carrying tenant B's
// workspace_id whose parent_id points at tenant A's note — and asserts three
// things in order, so a pass cannot come from an empty table: the drifted row
// IS visible to its own tenant; A's filter on its OWN child matches; A's
// filter on B's child matches nothing.
func TestRelationshipFilterAPI_TenantedTargetIsScoped(t *testing.T) {
	truncateAll(t)
	url := newHeaderTenantedHandler(t)

	parent := seedNote(t, fixedTenantA, "a-parent", nil)
	seedNote(t, fixedTenantA, "a-child", &parent)
	// The drifted row: tenant B's, but hanging off tenant A's note.
	seedNote(t, fixedTenantB, "b-child", &parent)

	// 1. Attributability — the drifted row exists and tenant B can read it, so
	//    the exclusion below is the tenant predicate and not a missing row.
	if got := noteListBodies(t, url, fixedTenantB, `{body: {eq: $body}}`, map[string]any{"body": "b-child"}); len(got) != 1 {
		t.Fatalf("tenant B cannot see its own drifted row (%v); the assertions below would be vacuous", got)
	}

	// 2. Positive control — the relationship filter works for A's own child.
	if diff := cmp.Diff([]string{"a-parent"}, noteListBodies(t, url, fixedTenantA,
		`{children: {body: {eq: $body}}}`, map[string]any{"body": "a-child"})); diff != "" {
		t.Fatalf("tenant A filtering on its own child (-want +got):\n%s", diff)
	}

	// 3. The security assertion — B's child must not satisfy A's filter.
	if got := noteListBodies(t, url, fixedTenantA,
		`{children: {body: {eq: $body}}}`, map[string]any{"body": "b-child"}); len(got) != 0 {
		t.Errorf("tenant A matched %v through a child row owned by tenant B — the subquery's tenant predicate is missing", got)
	}
}

// TestRelationshipFilterAPI_SelfReferentialTwoLevels drives a two-level nested
// value through the recursive input (§13.5). Each level compiles to its own
// EXISTS with its own depth-keyed alias; a shared alias would let the inner
// subquery shadow the reference its own correlation resolves against, which
// returns wrong rows with no error anywhere.
func TestRelationshipFilterAPI_SelfReferentialTwoLevels(t *testing.T) {
	truncateAll(t)
	url := newHeaderTenantedHandler(t)

	grandparent := seedNote(t, fixedTenantA, "gp", nil)
	parent := seedNote(t, fixedTenantA, "p", &grandparent)
	seedNote(t, fixedTenantA, "c", &parent)
	// A sibling branch with no grandchild — so "has a child that has a child"
	// is a narrower answer than "has a child".
	lonely := seedNote(t, fixedTenantA, "lonely", nil)
	seedNote(t, fixedTenantA, "lonely-child", &lonely)

	oneLevel := noteListBodies(t, url, fixedTenantA,
		`{children: {body: {eq: $body}}}`, map[string]any{"body": "c"})
	if diff := cmp.Diff([]string{"p"}, oneLevel); diff != "" {
		t.Errorf("one level of nesting (-want +got):\n%s", diff)
	}

	twoLevels := noteListBodies(t, url, fixedTenantA,
		`{children: {children: {body: {eq: $body}}}}`, map[string]any{"body": "c"})
	if diff := cmp.Diff([]string{"gp"}, twoLevels); diff != "" {
		t.Errorf("two levels of nesting (-want +got):\n%s", diff)
	}
}
