package tests

import (
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
)

// fixedTimestamp is the constant Time string fed into createdAt on every seed.
// Holding it constant keeps `TimeComparator` assertions deterministic and
// avoids any time-zone parsing weirdness between test boots.
const fixedTimestamp = "2026-01-01T00:00:00Z"

// --- helpers -------------------------------------------------------------

type userPayload struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	IsActive  bool   `json:"isActive"`
	DeletedAt any    `json:"deletedAt"`
}

type categoryPayload struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description any    `json:"description"`
}

func seedUser(t *testing.T, email, name string) userPayload {
	t.Helper()
	var out struct {
		CreateUser userPayload `json:"createUser"`
	}
	gqlExecData(t, `
		mutation Seed($email: String!, $name: String!, $createdAt: Time!) {
			createUser(input: { email: $email, name: $name, isActive: true, createdAt: $createdAt }) {
				id email name isActive
			}
		}
	`, map[string]any{
		"email":     email,
		"name":      name,
		"createdAt": fixedTimestamp,
	}, &out)
	if out.CreateUser.ID == "" {
		t.Fatalf("seedUser: empty ID")
	}
	return out.CreateUser
}

func seedCategory(t *testing.T, name string) categoryPayload {
	t.Helper()
	var out struct {
		CreateCategory categoryPayload `json:"createCategory"`
	}
	gqlExecData(t, `
		mutation Seed($name: String!, $createdAt: Time!) {
			createCategory(input: { name: $name, createdAt: $createdAt }) {
				id name description
			}
		}
	`, map[string]any{
		"name":      name,
		"createdAt": fixedTimestamp,
	}, &out)
	if out.CreateCategory.ID == 0 {
		t.Fatalf("seedCategory: zero ID")
	}
	return out.CreateCategory
}

// --- curated surface: User (uuid PK + soft delete + restore) -------------

func TestCuratedSurface_User(t *testing.T) {
	truncateAll(t)

	var seeded userPayload
	t.Run("Create", func(t *testing.T) {
		seeded = seedUser(t, "alice@example.com", "Alice")
		if _, err := uuid.Parse(seeded.ID); err != nil {
			t.Fatalf("expected UUID id, got %q: %v", seeded.ID, err)
		}
		if seeded.Email != "alice@example.com" || seeded.Name != "Alice" || !seeded.IsActive {
			t.Fatalf("create returned unexpected payload: %+v", seeded)
		}
	})

	t.Run("GetByPK", func(t *testing.T) {
		var out struct {
			User *userPayload `json:"user"`
		}
		gqlExecData(t, `
			query ($id: UUID!) { user(id: $id) { id email name } }
		`, map[string]any{"id": seeded.ID}, &out)
		if out.User == nil || out.User.ID != seeded.ID {
			t.Fatalf("expected user with id %s, got %+v", seeded.ID, out.User)
		}

		// Missing PK returns null, per §26.5.1 ("returns null when missing").
		gqlExecData(t, `
			query ($id: UUID!) { user(id: $id) { id } }
		`, map[string]any{"id": uuid.New().String()}, &out)
		if out.User != nil {
			t.Fatalf("expected null for missing PK, got %+v", out.User)
		}
	})

	t.Run("Connection", func(t *testing.T) {
		seedUser(t, "bob@example.com", "Bob")
		var out struct {
			Users struct {
				TotalCount int `json:"totalCount"`
				Edges      []struct {
					Node   userPayload `json:"node"`
					Cursor string      `json:"cursor"`
				} `json:"edges"`
				PageInfo struct {
					HasNextPage     bool    `json:"hasNextPage"`
					HasPreviousPage bool    `json:"hasPreviousPage"`
					StartCursor     *string `json:"startCursor"`
					EndCursor       *string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"users"`
		}
		gqlExecData(t, `
			query { users(first: 10) {
				totalCount
				edges { node { id email } cursor }
				pageInfo { hasNextPage hasPreviousPage startCursor endCursor }
			} }
		`, nil, &out)
		if out.Users.TotalCount < 2 {
			t.Fatalf("expected totalCount >= 2, got %d", out.Users.TotalCount)
		}
		if len(out.Users.Edges) < 2 {
			t.Fatalf("expected at least 2 edges, got %d", len(out.Users.Edges))
		}
		for i, e := range out.Users.Edges {
			if e.Node.ID == "" {
				t.Errorf("edge %d: empty node id", i)
			}
			if e.Cursor == "" {
				t.Errorf("edge %d: empty cursor", i)
			}
		}
	})

	t.Run("List", func(t *testing.T) {
		var out struct {
			UserList struct {
				Items      []userPayload `json:"items"`
				TotalCount int           `json:"totalCount"`
				Offset     int           `json:"offset"`
				Limit      int           `json:"limit"`
				HasMore    bool          `json:"hasMore"`
			} `json:"userList"`
		}
		gqlExecData(t, `
			query { userList(sort: [{ field: EMAIL, direction: ASC }], limit: 1, offset: 0) {
				items { id email }
				totalCount offset limit hasMore
			} }
		`, nil, &out)
		if out.UserList.Limit != 1 || out.UserList.Offset != 0 {
			t.Fatalf("expected offset=0 limit=1, got %+v", out.UserList)
		}
		if len(out.UserList.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(out.UserList.Items))
		}
		if !out.UserList.HasMore {
			t.Fatalf("expected hasMore=true (totalCount=%d limit=1)", out.UserList.TotalCount)
		}
		if out.UserList.Items[0].Email != "alice@example.com" {
			t.Errorf("expected first user alice (ASC by email), got %q", out.UserList.Items[0].Email)
		}
	})

	t.Run("CreateMany", func(t *testing.T) {
		var out struct {
			CreateUsers []userPayload `json:"createUsers"`
		}
		gqlExecData(t, `
			mutation ($inputs: [CreateUserInput!]!) {
				createUsers(inputs: $inputs) { id email name }
			}
		`, map[string]any{
			"inputs": []map[string]any{
				{"email": "carol@example.com", "name": "Carol", "isActive": true, "createdAt": fixedTimestamp},
				{"email": "dave@example.com", "name": "Dave", "isActive": true, "createdAt": fixedTimestamp},
			},
		}, &out)
		if len(out.CreateUsers) != 2 {
			t.Fatalf("expected 2 created users, got %d", len(out.CreateUsers))
		}
	})

	t.Run("Update", func(t *testing.T) {
		var out struct {
			UpdateUser userPayload `json:"updateUser"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!, $name: String!) {
				updateUser(id: $id, input: { name: $name }) { id name }
			}
		`, map[string]any{"id": seeded.ID, "name": "Alice Updated"}, &out)
		if out.UpdateUser.Name != "Alice Updated" {
			t.Fatalf("expected name to be Alice Updated, got %q", out.UpdateUser.Name)
		}
	})

	t.Run("UpdateMany", func(t *testing.T) {
		var out struct {
			UpdateUsers []userPayload `json:"updateUsers"`
		}
		gqlExecData(t, `
			mutation ($filter: UserFilter!, $name: String!) {
				updateUsers(filter: $filter, input: { name: $name }) { id name email }
			}
		`, map[string]any{
			"filter": map[string]any{"email": map[string]any{"eq": "carol@example.com"}},
			"name":   "Carol Updated",
		}, &out)
		if len(out.UpdateUsers) != 1 || out.UpdateUsers[0].Name != "Carol Updated" {
			t.Fatalf("expected one updated user named 'Carol Updated', got %+v", out.UpdateUsers)
		}
	})

	t.Run("Upsert", func(t *testing.T) {
		// Upsert with a fresh email: inserts a new row (PK is server-assigned UUID,
		// so this exercises the insert branch — upsert-by-unique on email is the
		// runtime's conflict target).
		var out struct {
			UpsertUser userPayload `json:"upsertUser"`
		}
		gqlExecData(t, `
			mutation ($input: CreateUserInput!) {
				upsertUser(input: $input) { id email name }
			}
		`, map[string]any{
			"input": map[string]any{
				"email": "eve@example.com", "name": "Eve", "isActive": true, "createdAt": fixedTimestamp,
			},
		}, &out)
		if out.UpsertUser.Email != "eve@example.com" {
			t.Fatalf("upsert returned unexpected email: %q", out.UpsertUser.Email)
		}
	})

	t.Run("SoftDelete", func(t *testing.T) {
		var out struct {
			SoftDeleteUser *userPayload `json:"softDeleteUser"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!) {
				softDeleteUser(id: $id) { id email deletedAt }
			}
		`, map[string]any{"id": seeded.ID}, &out)
		if out.SoftDeleteUser == nil {
			t.Fatalf("softDeleteUser returned nil")
		}
		if out.SoftDeleteUser.DeletedAt == nil {
			t.Fatalf("expected deletedAt to be set, got nil")
		}
	})

	t.Run("Restore", func(t *testing.T) {
		var out struct {
			RestoreUser *userPayload `json:"restoreUser"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!) {
				restoreUser(id: $id) { id deletedAt }
			}
		`, map[string]any{"id": seeded.ID}, &out)
		if out.RestoreUser == nil {
			t.Fatalf("restoreUser returned nil")
		}
		if out.RestoreUser.DeletedAt != nil {
			t.Fatalf("expected deletedAt to be null after restore, got %v", out.RestoreUser.DeletedAt)
		}
	})

	t.Run("HardDelete", func(t *testing.T) {
		var out struct {
			HardDeleteUser bool `json:"hardDeleteUser"`
		}
		gqlExecData(t, `
			mutation ($id: UUID!) { hardDeleteUser(id: $id) }
		`, map[string]any{"id": seeded.ID}, &out)
		if !out.HardDeleteUser {
			t.Fatalf("hardDeleteUser returned false")
		}

		// Re-fetch should return null now that the row is gone.
		var got struct {
			User *userPayload `json:"user"`
		}
		gqlExecData(t, `query ($id: UUID!) { user(id: $id) { id } }`,
			map[string]any{"id": seeded.ID}, &got)
		if got.User != nil {
			t.Fatalf("expected null after hard delete, got %+v", got.User)
		}
	})
}

// --- curated surface: Category (int PK, no soft-delete) -------------------

func TestCuratedSurface_Category(t *testing.T) {
	truncateAll(t)

	var seeded categoryPayload
	t.Run("Create", func(t *testing.T) {
		seeded = seedCategory(t, "Electronics")
		if seeded.ID == 0 {
			t.Fatalf("expected non-zero int PK")
		}
	})

	t.Run("GetByPK", func(t *testing.T) {
		var out struct {
			Category *categoryPayload `json:"category"`
		}
		gqlExecData(t, `query ($id: Int!) { category(id: $id) { id name } }`,
			map[string]any{"id": seeded.ID}, &out)
		if out.Category == nil || out.Category.Name != "Electronics" {
			t.Fatalf("expected Electronics, got %+v", out.Category)
		}
	})

	t.Run("Connection", func(t *testing.T) {
		seedCategory(t, "Books")
		var out struct {
			Categories struct {
				TotalCount int `json:"totalCount"`
				Edges      []struct {
					Node   categoryPayload `json:"node"`
					Cursor string          `json:"cursor"`
				} `json:"edges"`
				PageInfo struct {
					HasNextPage     bool    `json:"hasNextPage"`
					HasPreviousPage bool    `json:"hasPreviousPage"`
					StartCursor     *string `json:"startCursor"`
					EndCursor       *string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"categories"`
		}
		gqlExecData(t, `
			query { categories(first: 10) {
				totalCount edges { node { id name } cursor }
				pageInfo { hasNextPage hasPreviousPage startCursor endCursor }
			} }
		`, nil, &out)
		if out.Categories.TotalCount < 2 {
			t.Fatalf("expected totalCount >= 2, got %d", out.Categories.TotalCount)
		}
		if len(out.Categories.Edges) < 2 {
			t.Fatalf("expected at least 2 edges, got %d", len(out.Categories.Edges))
		}
	})

	t.Run("List", func(t *testing.T) {
		var out struct {
			CategoryList struct {
				Items      []categoryPayload `json:"items"`
				TotalCount int               `json:"totalCount"`
				Offset     int               `json:"offset"`
				Limit      int               `json:"limit"`
				HasMore    bool              `json:"hasMore"`
			} `json:"categoryList"`
		}
		gqlExecData(t, `
			query { categoryList(sort: [{ field: NAME, direction: ASC }], limit: 1, offset: 0) {
				items { id name } totalCount offset limit hasMore
			} }
		`, nil, &out)
		if out.CategoryList.Items[0].Name != "Books" {
			t.Errorf("expected first Books (ASC by name), got %q", out.CategoryList.Items[0].Name)
		}
		if !out.CategoryList.HasMore {
			t.Errorf("expected hasMore=true (totalCount=%d limit=1)", out.CategoryList.TotalCount)
		}
	})

	t.Run("CreateMany", func(t *testing.T) {
		var out struct {
			CreateCategories []categoryPayload `json:"createCategories"`
		}
		gqlExecData(t, `
			mutation ($inputs: [CreateCategoryInput!]!) {
				createCategories(inputs: $inputs) { id name }
			}
		`, map[string]any{
			"inputs": []map[string]any{
				{"name": "Toys", "createdAt": fixedTimestamp},
				{"name": "Garden", "createdAt": fixedTimestamp},
			},
		}, &out)
		if len(out.CreateCategories) != 2 {
			t.Fatalf("expected 2 created, got %d", len(out.CreateCategories))
		}
	})

	t.Run("Update", func(t *testing.T) {
		var out struct {
			UpdateCategory categoryPayload `json:"updateCategory"`
		}
		gqlExecData(t, `
			mutation ($id: Int!, $name: String!) {
				updateCategory(id: $id, input: { name: $name }) { id name }
			}
		`, map[string]any{"id": seeded.ID, "name": "Electronics Updated"}, &out)
		if out.UpdateCategory.Name != "Electronics Updated" {
			t.Fatalf("expected name updated, got %+v", out.UpdateCategory)
		}
	})

	t.Run("UpdateMany", func(t *testing.T) {
		var out struct {
			UpdateCategories []categoryPayload `json:"updateCategories"`
		}
		gqlExecData(t, `
			mutation ($filter: CategoryFilter!) {
				updateCategories(filter: $filter, input: { description: "via-bulk-update" }) { id name description }
			}
		`, map[string]any{
			"filter": map[string]any{"name": map[string]any{"eq": "Books"}},
		}, &out)
		if len(out.UpdateCategories) != 1 {
			t.Fatalf("expected one row, got %d", len(out.UpdateCategories))
		}
		if got, _ := out.UpdateCategories[0].Description.(string); got != "via-bulk-update" {
			t.Errorf("expected description set, got %v", out.UpdateCategories[0].Description)
		}
	})

	t.Run("Upsert", func(t *testing.T) {
		var out struct {
			UpsertCategory categoryPayload `json:"upsertCategory"`
		}
		gqlExecData(t, `
			mutation ($input: CreateCategoryInput!) {
				upsertCategory(input: $input) { id name }
			}
		`, map[string]any{
			"input": map[string]any{"name": "Furniture", "createdAt": fixedTimestamp},
		}, &out)
		if out.UpsertCategory.Name != "Furniture" {
			t.Fatalf("upsert returned unexpected name: %q", out.UpsertCategory.Name)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		// Category has no soft-delete column, so the schema exposes
		// `deleteCategory(id: Int!): Boolean!` per §26.5.1.
		var out struct {
			DeleteCategory bool `json:"deleteCategory"`
		}
		gqlExecData(t, `
			mutation ($id: Int!) { deleteCategory(id: $id) }
		`, map[string]any{"id": seeded.ID}, &out)
		if !out.DeleteCategory {
			t.Fatalf("deleteCategory returned false")
		}
	})
}

// --- response-shape pins -------------------------------------------------

// TestResponseShape_PageInfo pins the PageInfo envelope to the four fields
// PRD §26.4 mandates: hasNextPage, hasPreviousPage, startCursor, endCursor.
// The diff is byte-equal — any added/removed/renamed field flips this test
// red so the runtime PageInfo struct (database.PageInfo) and the GraphQL
// PageInfo type stay in lock-step (§26.5.6 "envelope binding").
func TestResponseShape_PageInfo(t *testing.T) {
	truncateAll(t)
	seedCategory(t, "PageInfoShape-A")
	seedCategory(t, "PageInfoShape-B")

	var resp struct {
		Categories struct {
			PageInfo map[string]any `json:"pageInfo"`
		} `json:"categories"`
	}
	gqlExecData(t, `
		query { categories(first: 1) {
			pageInfo { hasNextPage hasPreviousPage startCursor endCursor }
		} }
	`, nil, &resp)

	pi := resp.Categories.PageInfo
	gotKeys := keysOf(pi)
	wantKeys := []string{"endCursor", "hasNextPage", "hasPreviousPage", "startCursor"}
	if diff := cmp.Diff(wantKeys, gotKeys); diff != "" {
		t.Errorf("PageInfo shape mismatch (-want +got):\n%s", diff)
	}
	if _, ok := pi["hasNextPage"].(bool); !ok {
		t.Errorf("hasNextPage: want bool, got %T", pi["hasNextPage"])
	}
	if _, ok := pi["hasPreviousPage"].(bool); !ok {
		t.Errorf("hasPreviousPage: want bool, got %T", pi["hasPreviousPage"])
	}
	// startCursor / endCursor are nullable strings: either string or nil JSON null.
	if v := pi["startCursor"]; v != nil {
		if _, ok := v.(string); !ok {
			t.Errorf("startCursor: want string or null, got %T", v)
		}
	}
	if v := pi["endCursor"]; v != nil {
		if _, ok := v.(string); !ok {
			t.Errorf("endCursor: want string or null, got %T", v)
		}
	}
}

// TestResponseShape_Edges pins the Edge envelope to { node, cursor }
// per PRD §26.4.
func TestResponseShape_Edges(t *testing.T) {
	truncateAll(t)
	seedCategory(t, "EdgeShape-A")

	var resp struct {
		Categories struct {
			Edges []map[string]any `json:"edges"`
		} `json:"categories"`
	}
	gqlExecData(t, `
		query { categories(first: 1) { edges { node { id } cursor } } }
	`, nil, &resp)

	if len(resp.Categories.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(resp.Categories.Edges))
	}
	wantKeys := []string{"cursor", "node"}
	if diff := cmp.Diff(wantKeys, keysOf(resp.Categories.Edges[0])); diff != "" {
		t.Errorf("Edge shape mismatch (-want +got):\n%s", diff)
	}
	if _, ok := resp.Categories.Edges[0]["cursor"].(string); !ok {
		t.Errorf("cursor: want string, got %T", resp.Categories.Edges[0]["cursor"])
	}
	if _, ok := resp.Categories.Edges[0]["node"].(map[string]any); !ok {
		t.Errorf("node: want object, got %T", resp.Categories.Edges[0]["node"])
	}
}

// TestResponseShape_ListResult pins the <Type>ListResult envelope to
// { items, totalCount, offset, limit, hasMore } per PRD §26.4.
func TestResponseShape_ListResult(t *testing.T) {
	truncateAll(t)
	seedCategory(t, "ListResultShape-A")
	seedCategory(t, "ListResultShape-B")

	var resp struct {
		CategoryList map[string]any `json:"categoryList"`
	}
	gqlExecData(t, `
		query { categoryList(limit: 1, offset: 0) {
			items { id } totalCount offset limit hasMore
		} }
	`, nil, &resp)

	wantKeys := []string{"hasMore", "items", "limit", "offset", "totalCount"}
	if diff := cmp.Diff(wantKeys, keysOf(resp.CategoryList)); diff != "" {
		t.Errorf("ListResult shape mismatch (-want +got):\n%s", diff)
	}
	if _, ok := resp.CategoryList["items"].([]any); !ok {
		t.Errorf("items: want list, got %T", resp.CategoryList["items"])
	}
	if _, ok := resp.CategoryList["hasMore"].(bool); !ok {
		t.Errorf("hasMore: want bool, got %T", resp.CategoryList["hasMore"])
	}
	for _, k := range []string{"totalCount", "offset", "limit"} {
		// JSON unmarshals all numeric scalars into float64; the schema declares
		// Int! so any present value must be a finite number.
		if _, ok := resp.CategoryList[k].(float64); !ok {
			t.Errorf("%s: want number, got %T", k, resp.CategoryList[k])
		}
	}
}

// keysOf returns the sorted keys of a JSON object payload. Sorting is what
// lets cmp.Diff produce a stable diff regardless of Go map iteration order.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Hand-rolled lexicographic sort to avoid an extra import on a tiny slice.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
