package tests

import (
	"context"
	"testing"
)

// Composite-PK create surface (PRD §26.4).
//
// A composite primary key is always caller-strategy — it is a tuple of
// foreign keys with nothing to generate it — so its columns belong in
// `Create<T>Input`. Before that rule existed the API layer dropped every PK
// column by membership alone, which left `user_categories` (an all-PK M2M
// junction) with no create input and no create/upsert mutation at all, and
// left `workspace_settings` with an input that could only ever write
// zero-valued keys.
//
// These tests drive the junction's own create through the real GraphQL
// surface, which is the only link-management path the generated API offers
// (there is no connect/disconnect or nested-write surface).

// TestCompositePKCreate_AllPKJunction creates an M2M link whose columns are
// exactly its composite key, then reads it back by that key.
func TestCompositePKCreate_AllPKJunction(t *testing.T) {
	truncateAll(t)

	user := seedUser(t, "junction@example.com", "Junction User")
	category := seedCategory(t, "Junction Category")

	type userCategoryPayload struct {
		UserID     string `json:"userID"`
		CategoryID int    `json:"categoryID"`
	}

	var created struct {
		CreateUserCategory userCategoryPayload `json:"createUserCategory"`
	}
	gqlExecData(t, `
		mutation Link($userID: UUID!, $categoryID: Int!) {
			createUserCategory(input: { userID: $userID, categoryID: $categoryID }) {
				userID
				categoryID
			}
		}
	`, map[string]any{
		"userID":     user.ID,
		"categoryID": category.ID,
	}, &created)

	// The keys must round-trip as supplied. The defect this covers wrote a
	// zero UUID and a zero int instead.
	if created.CreateUserCategory.UserID != user.ID {
		t.Errorf("createUserCategory userID = %q, want %q", created.CreateUserCategory.UserID, user.ID)
	}
	if created.CreateUserCategory.CategoryID != category.ID {
		t.Errorf("createUserCategory categoryID = %d, want %d", created.CreateUserCategory.CategoryID, category.ID)
	}

	var fetched struct {
		UserCategory *userCategoryPayload `json:"userCategory"`
	}
	gqlExecData(t, `
		query ($userID: UUID!, $categoryID: Int!) {
			userCategory(userID: $userID, categoryID: $categoryID) {
				userID
				categoryID
			}
		}
	`, map[string]any{
		"userID":     user.ID,
		"categoryID": category.ID,
	}, &fetched)

	if fetched.UserCategory == nil {
		t.Fatal("userCategory: link not found after create")
	}
	if fetched.UserCategory.UserID != user.ID || fetched.UserCategory.CategoryID != category.ID {
		t.Errorf("userCategory = %+v, want (%s, %d)", *fetched.UserCategory, user.ID, category.ID)
	}

	// Exactly one row — the create wrote the supplied keys, not a second
	// zero-keyed row.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_categories`).Scan(&count); err != nil {
		t.Fatalf("count user_categories: %v", err)
	}
	if count != 1 {
		t.Errorf("user_categories row count = %d, want 1", count)
	}
}

// TestCompositePKCreate_AllPKJunctionUpsertIsIdempotent pins the upsert half.
// A junction whose columns are exactly its conflict target has nothing to
// SET, so the generated statement degrades to ON CONFLICT … DO NOTHING —
// "ensure this link exists". Re-running it must succeed, not raise a SQL
// syntax error or a duplicate-key violation.
func TestCompositePKCreate_AllPKJunctionUpsertIsIdempotent(t *testing.T) {
	truncateAll(t)

	user := seedUser(t, "upsert@example.com", "Upsert User")
	category := seedCategory(t, "Upsert Category")

	const mutation = `
		mutation Link($userID: UUID!, $categoryID: Int!) {
			upsertUserCategory(input: { userID: $userID, categoryID: $categoryID }) {
				userID
				categoryID
			}
		}
	`
	vars := map[string]any{
		"userID":     user.ID,
		"categoryID": category.ID,
	}

	for i := range 2 {
		resp := gqlExec(t, mutation, vars, nil)
		if len(resp.Errors) > 0 {
			t.Fatalf("upsertUserCategory call %d: graphql errors: %+v", i+1, resp.Errors)
		}
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_categories`).Scan(&count); err != nil {
		t.Fatalf("count user_categories: %v", err)
	}
	if count != 1 {
		t.Errorf("user_categories row count after two upserts = %d, want 1", count)
	}
}

// TestCompositePKCreate_TenantInPKVerifyMatch drives create on a composite-PK
// table whose tenant column is part of the key (PRD §29.7). The caller now
// supplies workspaceID in the input, so the verify-match branch is reachable
// from the create path: a matching value writes the row, a mismatched one
// short-circuits with tenancy.ErrMismatch → FORBIDDEN.
//
// Before the create input carried the key columns this path was unreachable
// — the translator left workspaceID at the zero UUID, so every
// createWorkspaceSetting failed the verify-match regardless of the header.
func TestCompositePKCreate_TenantInPKVerifyMatch(t *testing.T) {
	truncateAll(t)

	url := newHeaderTenantedHandler(t)

	const mutation = `
		mutation ($workspaceID: UUID!, $key: ID!, $value: String!, $createdAt: Time!) {
			createWorkspaceSetting(input: {
				workspaceID: $workspaceID
				key:         $key
				value:       $value
				createdAt:   $createdAt
			}) {
				workspaceID
				key
				value
			}
		}
	`

	t.Run("matching tenant writes the row", func(t *testing.T) {
		resp := postGQLAt(t, url, mutation, map[string]any{
			"workspaceID": fixedTenantA.String(),
			"key":         "theme",
			"value":       "dark",
			"createdAt":   fixedTimestamp,
		}, map[string]string{
			"X-Workspace-ID": fixedTenantA.String(),
		})
		if len(resp.Errors) > 0 {
			t.Fatalf("createWorkspaceSetting: graphql errors: %+v", resp.Errors)
		}

		var got string
		if err := testPool.QueryRow(context.Background(),
			`SELECT value FROM workspace_settings WHERE workspace_id = $1 AND key = $2`,
			fixedTenantA, "theme").Scan(&got); err != nil {
			t.Fatalf("read back workspace_setting: %v", err)
		}
		if got != "dark" {
			t.Errorf("value = %q, want %q", got, "dark")
		}
	})

	t.Run("mismatched tenant is forbidden", func(t *testing.T) {
		resp := postGQLAt(t, url, mutation, map[string]any{
			"workspaceID": fixedTenantB.String(), // mismatched against header(A)
			"key":         "locale",
			"value":       "en",
			"createdAt":   fixedTimestamp,
		}, map[string]string{
			"X-Workspace-ID": fixedTenantA.String(),
		})
		if len(resp.Errors) == 0 {
			t.Fatal("createWorkspaceSetting with a mismatched tenant: want an error, got none")
		}
		if code := extensionsCode(resp.Errors[0]); code != "FORBIDDEN" {
			t.Errorf("extensions.code = %q, want %q (errors: %+v)", code, "FORBIDDEN", resp.Errors)
		}

		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM workspace_settings WHERE key = $1`, "locale").Scan(&count); err != nil {
			t.Fatalf("count workspace_settings: %v", err)
		}
		if count != 0 {
			t.Errorf("mismatched create wrote %d rows, want 0", count)
		}
	})
}
