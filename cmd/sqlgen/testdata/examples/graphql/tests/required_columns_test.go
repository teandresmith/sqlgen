package tests

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"uuid"
)

// Required-columns end-to-end coverage. A GraphQL selection set narrows the
// SELECT to the fields the caller asked for, but four things are read off the
// row that nobody asks for by name: the cursor keys each edge cursor is encoded
// from, the parent key the O2M / M2M loaders map children back through, the O2O
// target PK the scan tests to tell a real row from a LEFT JOIN miss, and the
// M2M target PK the target-to-parent map is keyed on. Every test below omits
// exactly one of those columns from the selection set — the shape every
// pre-existing connection and relationship E2E happens to include, which is
// why the bug stayed invisible.

// decodedCursor base64-decodes an edge cursor into its JSON payload.
func decodedCursor(t *testing.T, cursor string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("decoding cursor %q: %v", cursor, err)
	}
	return string(raw)
}

// connectionPage is the shape every cursor assertion below reads back. Node is
// deliberately untyped: each test selects a different non-key field, and what
// matters is the cursor and whether page 2 moved.
type connectionPage struct {
	Edges []struct {
		Cursor string          `json:"cursor"`
		Node   json.RawMessage `json:"node"`
	} `json:"edges"`
	PageInfo struct {
		EndCursor *string `json:"endCursor"`
	} `json:"pageInfo"`
}

const zeroUUIDCursor = `{"id":"00000000-0000-0000-0000-000000000000"}`

// TestRequiredColumns_TableCursorWithoutSelectingKey pins the original symptom:
// a connection query that does not select the cursor key used to encode a
// zero-valued cursor, and paging on `endCursor` returned page 1 forever because
// `id > '00000000-…'` matches every row.
func TestRequiredColumns_TableCursorWithoutSelectingKey(t *testing.T) {
	truncateAll(t)
	seedUser(t, "rc-a@example.com", "RC-A")
	seedUser(t, "rc-b@example.com", "RC-B")
	seedUser(t, "rc-c@example.com", "RC-C")

	var page1 struct {
		Users connectionPage `json:"users"`
	}
	gqlExecData(t, `{ users(first: 1) { edges { cursor node { name } } pageInfo { endCursor } } }`, nil, &page1)

	if len(page1.Users.Edges) != 1 {
		t.Fatalf("page 1 returned %d edges, want 1", len(page1.Users.Edges))
	}
	if got := decodedCursor(t, page1.Users.Edges[0].Cursor); got == zeroUUIDCursor {
		t.Fatalf("cursor encodes the zero UUID (%s) — the cursor key was not read back", got)
	}
	if page1.Users.PageInfo.EndCursor == nil {
		t.Fatal("page 1 has no endCursor")
	}

	var page2 struct {
		Users connectionPage `json:"users"`
	}
	gqlExecData(t, `
		query ($after: String) {
			users(first: 1, after: $after) { edges { cursor node { name } } }
		}
	`, map[string]any{"after": *page1.Users.PageInfo.EndCursor}, &page2)

	if len(page2.Users.Edges) != 1 {
		t.Fatalf("page 2 returned %d edges, want 1", len(page2.Users.Edges))
	}
	if page2.Users.Edges[0].Cursor == page1.Users.Edges[0].Cursor {
		t.Errorf("page 2 repeated page 1 (cursor %s) — pagination cannot terminate",
			decodedCursor(t, page2.Users.Edges[0].Cursor))
	}
}

// TestRequiredColumns_ViewCursorWithoutSelectingKey is the view-side half, and
// the one case where the cursor key is not a primary key: category_price_totals
// is a materialized view whose cursor_keys is `category_id` (PRD §4.13 — a view
// has no PK to fall back on).
func TestRequiredColumns_ViewCursorWithoutSelectingKey(t *testing.T) {
	truncateAll(t)
	seedCategoryWithProducts(t, "rc-view-a", 2, "10.00")
	seedCategoryWithProducts(t, "rc-view-b", 1, "5.00")

	var page1 struct {
		CategoryPriceTotals connectionPage `json:"categoryPriceTotals"`
	}
	gqlExecData(t, `
		{ categoryPriceTotals(first: 1) { edges { cursor node { totalPrice } } pageInfo { endCursor } } }
	`, nil, &page1)

	if len(page1.CategoryPriceTotals.Edges) != 1 {
		t.Fatalf("page 1 returned %d edges, want 1", len(page1.CategoryPriceTotals.Edges))
	}
	if got := decodedCursor(t, page1.CategoryPriceTotals.Edges[0].Cursor); got == `{"category_id":0}` {
		t.Fatalf("cursor encodes a zero category_id (%s) — the cursor key was not read back", got)
	}

	if page1.CategoryPriceTotals.PageInfo.EndCursor == nil {
		t.Fatal("page 1 has no endCursor")
	}

	var page2 struct {
		CategoryPriceTotals connectionPage `json:"categoryPriceTotals"`
	}
	gqlExecData(t, `
		query ($after: String) {
			categoryPriceTotals(first: 1, after: $after) { edges { cursor node { totalPrice } } }
		}
	`, map[string]any{"after": *page1.CategoryPriceTotals.PageInfo.EndCursor}, &page2)

	if len(page2.CategoryPriceTotals.Edges) != 1 {
		t.Fatalf("page 2 returned %d edges, want 1", len(page2.CategoryPriceTotals.Edges))
	}
	if page2.CategoryPriceTotals.Edges[0].Cursor == page1.CategoryPriceTotals.Edges[0].Cursor {
		t.Error("page 2 repeated page 1 — the view's cursor key was not read back")
	}
}

// TestRequiredColumns_O2MWithoutParentPK: the O2M loader batches children with
// `WHERE user_id IN (parent keys)` collected off the scanned parents, so an
// unselected parent `id` used to send a list of zero UUIDs and return nothing.
func TestRequiredColumns_O2MWithoutParentPK(t *testing.T) {
	truncateAll(t)
	user := seedUser(t, "rc-o2m@example.com", "RC-O2M")
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
				ID string `json:"id"`
			} `json:"orders"`
		} `json:"user"`
	}
	gqlExecData(t, `query ($id: UUID!) { user(id: $id) { name orders { id } } }`,
		map[string]any{"id": user.ID}, &out)

	if len(out.User.Orders) != 1 {
		t.Errorf("got %d orders without selecting the parent id, want 1", len(out.User.Orders))
	}
}

// TestRequiredColumns_O2OWithoutChildPK: the O2O join is scanned in one query
// and a LEFT JOIN miss is detected by testing the target's PK against its zero
// value, so an unselected child `id` used to read as a miss and null out a
// child that is really there.
func TestRequiredColumns_O2OWithoutChildPK(t *testing.T) {
	truncateAll(t)

	var asset struct {
		CreateAsset struct {
			ID string `json:"id"`
		} `json:"createAsset"`
	}
	gqlExecData(t, `
		mutation ($name: String!, $categories: [DocumentEntityTypeEnum!]!, $createdAt: Time!) {
			createAsset(input: { name: $name, declaredCategories: $categories, createdAt: $createdAt }) { id }
		}
	`, map[string]any{
		"name":       "RC Asset",
		"categories": []string{"ASSETPRIMARY"},
		"createdAt":  fixedTimestamp,
	}, &asset)

	gqlExecData(t, `
		mutation ($entityID: UUID!, $createdAt: Time!) {
			createDocument(input: {
				entityID: $entityID, entityType: ASSETPRIMARY, name: "site_main", createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{"entityID": asset.CreateAsset.ID, "createdAt": fixedTimestamp}, &struct {
		CreateDocument struct {
			ID string `json:"id"`
		} `json:"createDocument"`
	}{})

	var out struct {
		Asset struct {
			PrimaryDocument *struct {
				Name string `json:"name"`
			} `json:"primaryDocument"`
		} `json:"asset"`
	}
	gqlExecData(t, `query ($id: UUID!) { asset(id: $id) { name primaryDocument { name } } }`,
		map[string]any{"id": asset.CreateAsset.ID}, &out)

	if out.Asset.PrimaryDocument == nil {
		t.Fatal("primaryDocument is null without selecting the child id, want the document")
	}
	if got, want := out.Asset.PrimaryDocument.Name, "site_main"; got != want {
		t.Errorf("primaryDocument.name = %q, want %q", got, want)
	}
}

// TestRequiredColumns_M2MWithoutChildPK: the M2M loader resolves target IDs
// through the junction and then maps loaded targets back by their own PK, so
// an unselected child `id` used to key every target on the zero value and
// strand all of them.
func TestRequiredColumns_M2MWithoutChildPK(t *testing.T) {
	truncateAll(t)
	user := seedUser(t, "rc-m2m@example.com", "RC-M2M")
	first := seedCategory(t, "RC-Cat-A")
	second := seedCategory(t, "RC-Cat-B")

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
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"user"`
	}
	gqlExecData(t, `query ($id: UUID!) { user(id: $id) { id categories { name } } }`,
		map[string]any{"id": user.ID}, &out)

	if len(out.User.Categories) != 2 {
		t.Fatalf("got %d categories without selecting the child id, want 2", len(out.User.Categories))
	}
	for i, want := range []string{"RC-Cat-A", "RC-Cat-B"} {
		if got := out.User.Categories[i].Name; got != want {
			t.Errorf("categories[%d].name = %q, want %q", i, got, want)
		}
	}
}

// TestRequiredColumns_TotalCountOnlyStaysOneQuery guards the other side of the
// fix. PRD §9.6 defines an all-false FieldOptions as "skip the entity fetch
// entirely", which is what a `{ totalCount }`-only connection query produces —
// so the union must sit inside the narrowed-but-non-empty arm. Forcing the
// cursor keys unconditionally would resurrect the projection and cost a second
// query on every count-only request.
func TestRequiredColumns_TotalCountOnlyStaysOneQuery(t *testing.T) {
	truncateAll(t)
	seedUser(t, "rc-count@example.com", "RC-Count")

	url, counter := newCountingHandler(t)
	counter.reset()

	resp := postGQL(t, url, `{ users(first: 1) { totalCount } }`, nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
	if got, want := counter.count(), 1; got != want {
		t.Errorf("totalCount-only connection query issued %d DB queries, want %d — the §9.6 short-circuit is gone", got, want)
	}
}

// TestRequiredColumns_CompositeCursorKeysWithoutSelectingThem covers the
// multi-key union. workspace_settings carries a two-column cursor
// (`workspace_id`, `key` — sqlgen.yml) over a composite PK, so the union has to
// add both, and both have to reach the encoder. The entity is tenanted, so this
// also proves the union composes with the tenant predicate rather than
// displacing it.
func TestRequiredColumns_CompositeCursorKeysWithoutSelectingThem(t *testing.T) {
	truncateAll(t)

	tenant := uuid.New()
	seedWorkspaceSetting(t, tenant, "alpha", "a-value")
	seedWorkspaceSetting(t, tenant, "beta", "b-value")

	url := newHeaderTenantedHandler(t)
	headers := map[string]string{"X-Workspace-ID": tenant.String()}

	var page1 struct {
		WorkspaceSettings connectionPage `json:"workspaceSettings"`
	}
	resp := postGQLAt(t, url, `
		{ workspaceSettings(first: 1) { edges { cursor node { value } } pageInfo { endCursor } } }
	`, nil, headers)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp.Errors)
	}
	if err := json.Unmarshal(resp.Data, &page1); err != nil {
		t.Fatalf("decode page 1 (raw=%s): %v", resp.Data, err)
	}
	if len(page1.WorkspaceSettings.Edges) != 1 {
		t.Fatalf("page 1 returned %d edges, want 1", len(page1.WorkspaceSettings.Edges))
	}

	// Both keys, both real: neither the tenant UUID nor the key string may come
	// back at its zero value.
	cursor := decodedCursor(t, page1.WorkspaceSettings.Edges[0].Cursor)
	for _, want := range []string{`"key":"alpha"`, `"workspace_id":"` + tenant.String() + `"`} {
		if !strings.Contains(cursor, want) {
			t.Errorf("cursor %s is missing %s — a cursor key was not read back", cursor, want)
		}
	}
	if page1.WorkspaceSettings.PageInfo.EndCursor == nil {
		t.Fatal("page 1 has no endCursor")
	}

	var page2 struct {
		WorkspaceSettings connectionPage `json:"workspaceSettings"`
	}
	resp2 := postGQLAt(t, url, `
		query ($after: String) {
			workspaceSettings(first: 1, after: $after) { edges { cursor node { value } } }
		}
	`, map[string]any{"after": *page1.WorkspaceSettings.PageInfo.EndCursor}, headers)
	if len(resp2.Errors) > 0 {
		t.Fatalf("graphql errors: %+v", resp2.Errors)
	}
	if err := json.Unmarshal(resp2.Data, &page2); err != nil {
		t.Fatalf("decode page 2 (raw=%s): %v", resp2.Data, err)
	}
	if len(page2.WorkspaceSettings.Edges) != 1 {
		t.Fatalf("page 2 returned %d edges, want 1", len(page2.WorkspaceSettings.Edges))
	}
	if !strings.Contains(decodedCursor(t, page2.WorkspaceSettings.Edges[0].Cursor), `"key":"beta"`) {
		t.Errorf("page 2 did not advance past the first key: %s",
			decodedCursor(t, page2.WorkspaceSettings.Edges[0].Cursor))
	}
}
