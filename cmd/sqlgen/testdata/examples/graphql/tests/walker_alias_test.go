package tests

import (
	"testing"
)

// Alias-coalescing end-to-end coverage. gqlgen's CollectFields resolves an
// alias to its underlying field name but keeps one CollectedField per alias, so
// a selection set naming the same field twice reaches the walker twice carrying
// the same Name. The walker's column arms are idempotent (`fo.<F> = true`), but
// every other arm assigned — so the last alias won and the rest rendered off
// columns the SELECT never fetched. No error was raised: `email` is `String!`
// and `total` is `Decimal!`, so the response carried a well-formed Go zero
// value.
//
// Every test below covers one walker arm and was verified failing-first
// against the pre-fix generated walker.

// seedAliasedUser gives the alias tests one user with one order, so both the
// envelope selections and the `orders` relationship have exactly one row to
// render every alias from.
func seedAliasedUser(t *testing.T) (userID string) {
	t.Helper()
	truncateAll(t)
	u := seedUser(t, "alias-a@example.com", "Alias-A")
	var out struct {
		CreateOrder struct {
			ID string `json:"id"`
		} `json:"createOrder"`
	}
	gqlExecData(t, `
		mutation Seed($uid: UUID!, $createdAt: Time!) {
			createOrder(input: { userID: $uid, total: "42.50", notes: "n", createdAt: $createdAt }) { id }
		}
	`, map[string]any{"uid": u.ID, "createdAt": fixedTimestamp}, &out)
	return u.ID
}

// TestAliases_ConnectionEdges: `a: edges { node { email } } b: edges { node
// { name } }`. Pre-fix the second `edges` entry overwrote entityFields, so
// `a.node.email` came back "".
func TestAliases_ConnectionEdges(t *testing.T) {
	seedAliasedUser(t)

	var out struct {
		Users struct {
			A []struct {
				Node struct {
					Email string `json:"email"`
				} `json:"node"`
			} `json:"a"`
			B []struct {
				Node struct {
					Name string `json:"name"`
				} `json:"node"`
			} `json:"b"`
		} `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { a: edges { node { email } } b: edges { node { name } } } }`, nil, &out)

	if got, want := out.Users.A[0].Node.Email, "alias-a@example.com"; got != want {
		t.Errorf("alias a: email = %q, want %q", got, want)
	}
	if got, want := out.Users.B[0].Node.Name, "Alias-A"; got != want {
		t.Errorf("alias b: name = %q, want %q", got, want)
	}
}

// TestAliases_ConnectionEdgesThreeWay: with three aliases only the last one
// survived pre-fix, so the union has to accumulate across every entry rather
// than merging pairwise into whichever two the loop saw last.
func TestAliases_ConnectionEdgesThreeWay(t *testing.T) {
	seedAliasedUser(t)

	var out struct {
		Users struct {
			A []struct {
				Node struct {
					Email string `json:"email"`
				} `json:"node"`
			} `json:"a"`
			B []struct {
				Node struct {
					Name string `json:"name"`
				} `json:"node"`
			} `json:"b"`
			C []struct {
				Node struct {
					IsActive bool `json:"isActive"`
				} `json:"node"`
			} `json:"c"`
		} `json:"users"`
	}
	gqlExecData(t, `{
		users(first: 1) {
			a: edges { node { email } }
			b: edges { node { name } }
			c: edges { node { isActive } }
		}
	}`, nil, &out)

	if got, want := out.Users.A[0].Node.Email, "alias-a@example.com"; got != want {
		t.Errorf("alias a: email = %q, want %q", got, want)
	}
	if got, want := out.Users.B[0].Node.Name, "Alias-A"; got != want {
		t.Errorf("alias b: name = %q, want %q", got, want)
	}
	if !out.Users.C[0].Node.IsActive {
		t.Error("alias c: isActive = false, want true")
	}
}

// TestAliases_ConnectionNode aliases one level deeper. `a: edges { x: node … }
// b: edges { y: node … }` survives the outer coalesce as two `node` entries
// under different aliases, so the inner descent has to union too.
func TestAliases_ConnectionNode(t *testing.T) {
	seedAliasedUser(t)

	var out struct {
		Users struct {
			A []struct {
				X struct {
					Email string `json:"email"`
				} `json:"x"`
			} `json:"a"`
			B []struct {
				Y struct {
					Name string `json:"name"`
				} `json:"y"`
			} `json:"b"`
		} `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { a: edges { x: node { email } } b: edges { y: node { name } } } }`, nil, &out)

	if got, want := out.Users.A[0].X.Email, "alias-a@example.com"; got != want {
		t.Errorf("alias x: email = %q, want %q", got, want)
	}
	if got, want := out.Users.B[0].Y.Name, "Alias-A"; got != want {
		t.Errorf("alias y: name = %q, want %q", got, want)
	}
}

// TestAliases_ListResultItems is the offset-envelope half of the same defect —
// the `items` arm assigned entityFields exactly as the `edges` arm did.
func TestAliases_ListResultItems(t *testing.T) {
	seedAliasedUser(t)

	var out struct {
		UserList struct {
			A []struct {
				Email string `json:"email"`
			} `json:"a"`
			B []struct {
				Name string `json:"name"`
			} `json:"b"`
		} `json:"userList"`
	}
	gqlExecData(t, `{ userList(limit: 1) { a: items { email } b: items { name } } }`, nil, &out)

	if got, want := out.UserList.A[0].Email, "alias-a@example.com"; got != want {
		t.Errorf("alias a: email = %q, want %q", got, want)
	}
	if got, want := out.UserList.B[0].Name, "Alias-A"; got != want {
		t.Errorf("alias b: name = %q, want %q", got, want)
	}
}

// TestAliases_Relationship covers the relationship arm, which used to be
// last-wins: `x: orders { total } y: orders { createdAt }` returned
// `x[0].total: "0"` — a Decimal! rendered from a column the SELECT skipped.
//
// Relationship fields take no GraphQL arguments (PRD §26.4, and §26.7 defers
// per-relationship filter / sort), so two aliases of one relationship can
// differ only in what they select. The union is therefore the whole of what
// both asked for, and gqlgen renders both aliases from the one load it drives.
func TestAliases_Relationship(t *testing.T) {
	uid := seedAliasedUser(t)

	var out struct {
		User struct {
			X []struct {
				Total string `json:"total"`
			} `json:"x"`
			Y []struct {
				CreatedAt string `json:"createdAt"`
			} `json:"y"`
		} `json:"user"`
	}
	gqlExecData(t, `query Q($id: UUID!) { user(id: $id) { x: orders { total } y: orders { createdAt } } }`,
		map[string]any{"id": uid}, &out)

	if got, want := out.User.X[0].Total, "42.5"; got != want {
		t.Errorf("alias x: total = %q, want %q", got, want)
	}
	if out.User.Y[0].CreatedAt == "" {
		t.Error("alias y: createdAt is empty")
	}
}

// TestAliases_PlainColumns is the immune row of the measurement table, pinned
// so the coalesce cannot break what already worked: the column arms are
// idempotent, so aliased plain columns were correct before the fix and must
// stay correct after it.
func TestAliases_PlainColumns(t *testing.T) {
	uid := seedAliasedUser(t)

	var out struct {
		User struct {
			P string `json:"p"`
			Q string `json:"q"`
		} `json:"user"`
	}
	gqlExecData(t, `query Q($id: UUID!) { user(id: $id) { p: name q: email } }`,
		map[string]any{"id": uid}, &out)

	if got, want := out.User.P, "Alias-A"; got != want {
		t.Errorf("alias p: name = %q, want %q", got, want)
	}
	if got, want := out.User.Q, "alias-a@example.com"; got != want {
		t.Errorf("alias q: email = %q, want %q", got, want)
	}
}

// TestAliases_RelationshipCostsOneQuery pins that the union feeds a single
// load. Two aliases of one O2M relationship must still cost `1 + 1` queries —
// answering them from two independent loads would break §25.1, and the flat
// [Type!]! relationship shape (PRD §26.4) could not express two loads anyway.
func TestAliases_RelationshipCostsOneQuery(t *testing.T) {
	uid := seedAliasedUser(t)

	url, counter := newCountingHandler(t)
	counter.reset()

	resp := postGQL(t, url, `query Q($id: UUID!) { user(id: $id) { x: orders { total } y: orders { createdAt } } }`,
		map[string]any{"id": uid})
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	if got, want := counter.count(), 2; got != want {
		t.Errorf("two aliases of one O2M relationship cost %d queries, want %d", got, want)
	}
}

// TestAliases_CountOnlyStillShortCircuits pins that coalescing did not disturb
// the count-only envelope path: an aliased `totalCount` still reports
// wantsRows=false and costs exactly one COUNT (PRD §9.6, §25.1).
func TestAliases_CountOnlyStillShortCircuits(t *testing.T) {
	seedAliasedUser(t)

	url, counter := newCountingHandler(t)
	counter.reset()

	resp := postGQL(t, url, `{ users { a: totalCount b: totalCount } }`, nil)
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", resp.Errors)
	}
	if got, want := counter.count(), 1; got != want {
		t.Errorf("aliased count-only envelope cost %d queries, want %d", got, want)
	}
}

// TestAliases_OneToOneRelationship covers the third assigning arm shape. An
// O2O relationship assigns a bare `*<Target>FieldOptions` rather than wrapping
// it in a `<Target>RelationshipOptions`, so it was last-wins for the same
// reason the list arm was — and its target rides the parent's JOIN rather than
// a separate fetch, so an under-projected alias renders zero values off a row
// that was joined correctly.
func TestAliases_OneToOneRelationship(t *testing.T) {
	assetID := seedSharedTargetAsset(t)

	var out struct {
		Asset struct {
			M struct {
				Name string `json:"name"`
			} `json:"m"`
			N struct {
				EntityType string `json:"entityType"`
			} `json:"n"`
		} `json:"asset"`
	}
	gqlExecData(t, `query Q($id: UUID!) {
		asset(id: $id) {
			m: primaryDocument { name }
			n: primaryDocument { entityType }
		}
	}`, map[string]any{"id": assetID.String()}, &out)

	if got, want := out.Asset.M.Name, "site_a"; got != want {
		t.Errorf("alias m: name = %q, want %q", got, want)
	}
	if got, want := out.Asset.N.EntityType, "ASSETPRIMARY"; got != want {
		t.Errorf("alias n: entityType = %q, want %q", got, want)
	}
}
