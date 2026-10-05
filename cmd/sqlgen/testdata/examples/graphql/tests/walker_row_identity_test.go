package tests

import (
	"encoding/json"
	"strings"
	"testing"
)

// Row-identity end-to-end coverage. GraphQL has selections that ask for a row
// without naming a single column of it: `__typename`, an edge's `cursor`, every
// `pageInfo` field. The walker translated all of them to an all-false
// FieldOptions, which PRD §9.6 defines as "the caller wants no rows" — so the
// read short-circuited and the request came back empty, or (for
// `pageInfo { hasNextPage }`) came back wrong.
//
// Every test below was verified failing-first against the pre-fix generated
// walker. The two `totalCount`-only pins are the other side of the fix: that
// selection genuinely wants no rows and must still cost exactly one query
// (PRD §25.1).

// rowIdentityConnection is the read-back shape for the connection assertions.
// Node is untyped because these queries deliberately select no node field.
type rowIdentityConnection struct {
	Edges []struct {
		Cursor   string          `json:"cursor"`
		Node     json.RawMessage `json:"node"`
		Typename string          `json:"__typename"`
	} `json:"edges"`
	PageInfo struct {
		StartCursor *string `json:"startCursor"`
		EndCursor   *string `json:"endCursor"`
		HasNextPage bool    `json:"hasNextPage"`
	} `json:"pageInfo"`
	TotalCount int `json:"totalCount"`
}

// seedTwoUsers gives every connection test a page with a successor, so
// `hasNextPage` under `first: 1` has a true value to report. It returns the
// first user so a by-PK query can address a row that exists.
func seedTwoUsers(t *testing.T) userPayload {
	t.Helper()
	truncateAll(t)
	first := seedUser(t, "ri-a@example.com", "RI-A")
	seedUser(t, "ri-b@example.com", "RI-B")
	return first
}

// TestRowIdentity_CursorOnlyEdgeReturnsTheEdge pins the original symptom: a
// connection query selecting only the cursor returned `edges: []` while
// `totalCount` reported the rows were there.
func TestRowIdentity_CursorOnlyEdgeReturnsTheEdge(t *testing.T) {
	seedTwoUsers(t)

	var out struct {
		Users rowIdentityConnection `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { edges { cursor } } }`, nil, &out)

	if len(out.Users.Edges) != 1 {
		t.Fatalf("got %d edges for a cursor-only selection, want 1", len(out.Users.Edges))
	}
	cursor := out.Users.Edges[0].Cursor
	if cursor == "" {
		t.Fatal("edge cursor is empty")
	}
	// The cursor must carry the row's real key, not the zero value a
	// never-fetched row would encode (the required-columns failure mode one
	// layer down).
	if decoded := decodedCursor(t, cursor); strings.Contains(decoded, "00000000-0000-0000-0000-000000000000") {
		t.Errorf("cursor decodes to a zero-valued key: %s", decoded)
	}
}

// TestRowIdentity_EdgeTypenameOnlyReturnsTheEdge: `__typename` names no column
// either, and an edge exists per row regardless of what is selected on it.
func TestRowIdentity_EdgeTypenameOnlyReturnsTheEdge(t *testing.T) {
	seedTwoUsers(t)

	var out struct {
		Users rowIdentityConnection `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { edges { __typename } } }`, nil, &out)

	if len(out.Users.Edges) != 1 {
		t.Fatalf("got %d edges for `edges { __typename }`, want 1", len(out.Users.Edges))
	}
	if got, want := out.Users.Edges[0].Typename, "UserEdge"; got != want {
		t.Errorf("edge __typename = %q, want %q", got, want)
	}
}

// TestRowIdentity_NodeTypenameOnlyReturnsTheNode descends one level further:
// the walker reaches the node's selection set and finds only `__typename`.
func TestRowIdentity_NodeTypenameOnlyReturnsTheNode(t *testing.T) {
	seedTwoUsers(t)

	var out struct {
		Users struct {
			Edges []struct {
				Node struct {
					Typename string `json:"__typename"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { edges { node { __typename } } } }`, nil, &out)

	if len(out.Users.Edges) != 1 {
		t.Fatalf("got %d edges for `edges { node { __typename } }`, want 1", len(out.Users.Edges))
	}
	if got, want := out.Users.Edges[0].Node.Typename, "User"; got != want {
		t.Errorf("node __typename = %q, want %q", got, want)
	}
}

// TestRowIdentity_PageInfoCursorsAreNotNull: startCursor / endCursor are
// encoded from the first and last row of the fetched page, so a skipped fetch
// left both null on a page that has rows.
func TestRowIdentity_PageInfoCursorsAreNotNull(t *testing.T) {
	seedTwoUsers(t)

	var out struct {
		Users rowIdentityConnection `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { pageInfo { startCursor endCursor } } }`, nil, &out)

	if out.Users.PageInfo.StartCursor == nil || out.Users.PageInfo.EndCursor == nil {
		t.Fatalf("pageInfo cursors are null on a non-empty page: start=%v end=%v",
			out.Users.PageInfo.StartCursor, out.Users.PageInfo.EndCursor)
	}
}

// TestRowIdentity_PageInfoHasNextPageIsTrue is the sharpest row of the family:
// a *wrong boolean* rather than an empty list. hasNextPage is derived from the
// limit+1 probe row, so skipping the fetch reported `false` on a page that has
// a successor — and the same query selecting a node field reports `true`.
func TestRowIdentity_PageInfoHasNextPageIsTrue(t *testing.T) {
	seedTwoUsers(t)

	var bare struct {
		Users rowIdentityConnection `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { pageInfo { hasNextPage } } }`, nil, &bare)

	var withNode struct {
		Users rowIdentityConnection `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { pageInfo { hasNextPage } edges { node { name } } } }`, nil, &withNode)

	if !withNode.Users.PageInfo.HasNextPage {
		t.Fatal("fixture is wrong: hasNextPage is false even with a node selection")
	}
	if !bare.Users.PageInfo.HasNextPage {
		t.Error("hasNextPage = false without a node selection, want true — the same page reports true when a column is named")
	}
}

// TestRowIdentity_ListItemsTypenameOnlyReturnsTheItem covers the offset
// envelope: `items` is its row-bearing key, and it has the same defect.
func TestRowIdentity_ListItemsTypenameOnlyReturnsTheItem(t *testing.T) {
	seedTwoUsers(t)

	var out struct {
		UserList struct {
			Items []struct {
				Typename string `json:"__typename"`
			} `json:"items"`
		} `json:"userList"`
	}
	gqlExecData(t, `{ userList(limit: 1) { items { __typename } } }`, nil, &out)

	if len(out.UserList.Items) != 1 {
		t.Fatalf("got %d items for `items { __typename }`, want 1", len(out.UserList.Items))
	}
	if got, want := out.UserList.Items[0].Typename, "User"; got != want {
		t.Errorf("item __typename = %q, want %q", got, want)
	}
}

// TestRowIdentity_GetByPKTypenameOnlyReturnsTheObject: no envelope at all. The
// by-PK query returned `null` for a row that exists, and cost zero DB queries
// doing it.
func TestRowIdentity_GetByPKTypenameOnlyReturnsTheObject(t *testing.T) {
	truncateAll(t)
	user := seedUser(t, "ri-get@example.com", "RI-Get")

	var out struct {
		User *struct {
			Typename string `json:"__typename"`
		} `json:"user"`
	}
	gqlExecData(t, `query ($id: UUID!) { user(id: $id) { __typename } }`,
		map[string]any{"id": user.ID}, &out)

	if out.User == nil {
		t.Fatal("user is null for a `__typename`-only selection on a row that exists")
	}
	if got, want := out.User.Typename, "User"; got != want {
		t.Errorf("__typename = %q, want %q", got, want)
	}
}

// TestRowIdentity_NestedM2MTypenameOnlyReturnsTheChildren: the recursive half.
// An all-false child FieldOptions short-circuited the M2M step-2 fetch after
// the junction query had already run — a wasted round-trip and an empty answer.
func TestRowIdentity_NestedM2MTypenameOnlyReturnsTheChildren(t *testing.T) {
	truncateAll(t)
	user := seedUser(t, "ri-m2m@example.com", "RI-M2M")
	first := seedCategory(t, "RI-Cat-A")
	second := seedCategory(t, "RI-Cat-B")

	for _, categoryID := range []int{first.ID, second.ID} {
		gqlExecData(t, `
			mutation ($userID: UUID!, $categoryID: Int!) {
				createUserCategory(input: { userID: $userID, categoryID: $categoryID }) { userID categoryID }
			}
		`, map[string]any{"userID": user.ID, "categoryID": categoryID}, &struct {
			CreateUserCategory struct {
				UserID string `json:"userID"`
			} `json:"createUserCategory"`
		}{})
	}

	var out struct {
		User struct {
			Categories []struct {
				Typename string `json:"__typename"`
			} `json:"categories"`
		} `json:"user"`
	}
	gqlExecData(t, `query ($id: UUID!) { user(id: $id) { id categories { __typename } } }`,
		map[string]any{"id": user.ID}, &out)

	if len(out.User.Categories) != 2 {
		t.Fatalf("got %d categories for `categories { __typename }`, want 2", len(out.User.Categories))
	}
}

// TestRowIdentity_NestedO2MTypenameOnlyStillReturnsTheChildren pins the one row
// of the family that already worked before this fix. It worked by accident —
// the O2M loader force-selected the child FK, which made an all-false child
// FieldOptions non-empty. That force-select has since become part of the child
// fetch's requiredColumns, so the walker is now the only thing keeping
// this answer correct and this test is its guard.
func TestRowIdentity_NestedO2MTypenameOnlyStillReturnsTheChildren(t *testing.T) {
	truncateAll(t)
	user := seedUser(t, "ri-o2m@example.com", "RI-O2M")
	gqlExecData(t, `
		mutation ($userID: UUID!, $createdAt: Time!) {
			createOrder(input: { userID: $userID, total: "10.00", createdAt: $createdAt }) { id }
		}
	`, map[string]any{"userID": user.ID, "createdAt": fixedTimestamp}, &struct {
		CreateOrder struct {
			ID string `json:"id"`
		} `json:"createOrder"`
	}{})

	var out struct {
		User struct {
			Orders []struct {
				Typename string `json:"__typename"`
			} `json:"orders"`
		} `json:"user"`
	}
	gqlExecData(t, `query ($id: UUID!) { user(id: $id) { id orders { __typename } } }`,
		map[string]any{"id": user.ID}, &out)

	if len(out.User.Orders) != 1 {
		t.Fatalf("got %d orders for `orders { __typename }`, want 1", len(out.User.Orders))
	}
}

// TestRowIdentity_ViewCursorOnlyEdgeReturnsTheEdge: the fix is walker-wide, so
// a view's connection gets it too. `category_price_totals` is the matview whose
// cursor key is its `@pk` — the entity kind where the row identity is not the
// inherited default.
func TestRowIdentity_ViewCursorOnlyEdgeReturnsTheEdge(t *testing.T) {
	truncateAll(t)
	seedCategoryWithProducts(t, "RIMatviewCat", 2, "3.00")

	var out struct {
		CategoryPriceTotals rowIdentityConnection `json:"categoryPriceTotals"`
	}
	gqlExecData(t, `{ categoryPriceTotals(first: 1) { edges { cursor } } }`, nil, &out)

	if len(out.CategoryPriceTotals.Edges) != 1 {
		t.Fatalf("got %d view edges for a cursor-only selection, want 1", len(out.CategoryPriceTotals.Edges))
	}
	if out.CategoryPriceTotals.Edges[0].Cursor == "" {
		t.Error("view edge cursor is empty")
	}
}

// TestRowIdentity_QueryCounts pins both sides of the fix in the §25.1 currency.
// A count-only envelope selection never reaches a row and must still cost one
// query; every row-bearing selection costs the COUNT plus the page, which is
// the correct count for a query whose answer is read off rows.
func TestRowIdentity_QueryCounts(t *testing.T) {
	first := seedTwoUsers(t)
	url, counter := newCountingHandler(t)

	tests := []struct {
		name  string
		query string
		vars  map[string]any
		want  int
		why   string
	}{
		{
			name:  "connection totalCount only",
			query: `{ users(first: 1) { totalCount } }`,
			want:  1,
			why:   "no row-bearing field is selected, so the §9.6 all-false short-circuit stands",
		},
		{
			name:  "get by PK typename only",
			query: `query ($id: UUID!) { user(id: $id) { __typename } }`,
			vars:  map[string]any{"id": first.ID},
			want:  1,
			why:   "no envelope to consult — a by-PK selection always wants its row",
		},
		{
			name:  "list totalCount only",
			query: `{ userList(limit: 1) { totalCount hasMore } }`,
			want:  1,
			why:   "hasMore is derived from totalCount / offset / limit, not from rows",
		},
		{
			name:  "connection cursor only",
			query: `{ users(first: 1) { edges { cursor } } }`,
			want:  2,
			why:   "an edge exists per row, so the page has to be fetched",
		},
		{
			name:  "connection pageInfo only",
			query: `{ users(first: 1) { pageInfo { hasNextPage } } }`,
			want:  2,
			why:   "hasNextPage is read off the limit+1 probe row",
		},
		{
			name:  "list items typename only",
			query: `{ userList(limit: 1) { items { __typename } } }`,
			want:  2,
			why:   "an item exists per row, so the page has to be fetched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter.reset()
			resp := postGQL(t, url, tt.query, tt.vars)
			if len(resp.Errors) != 0 {
				t.Fatalf("unexpected errors: %+v", resp.Errors)
			}
			if got := counter.count(); got != tt.want {
				t.Errorf("DB queries = %d, want %d — %s", got, tt.want, tt.why)
			}
		})
	}
}
