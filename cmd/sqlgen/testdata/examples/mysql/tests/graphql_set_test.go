package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file is the SET-column integration proof, and the reason this example
// has a GraphQL surface at all.
//
// A MySQL `SET` column resolves to a named slice — `UsersPermissionsSet` =
// `[]UsersPermissionsSetValue` — which had no GraphQL binding of any kind.
// Originally the column fell through to `String`, so gqlgen answered the
// read side with a `panic("not implemented")` field resolver and typed the
// input field `string` against a named-slice model field, which did not
// compile. A later change turned that into a named codegen error, so a SET
// column plus `api.graphql.enabled` simply failed generation.
//
// None of that is provable by a unit test. The defect was "the generated
// module does not compile / panics at request time", and only a module that
// actually runs gqlgen over a SET column can pin it. That is what these tests
// are: writes and reads through the live handler, against a real MySQL.
//
// The projection under test (PRD §26.4) is a GraphQL enum list. The GraphQL
// enum is named after the VALUE type and each element binds to it, while the
// named slice is the column's own Go type — the same two-entry `models:`
// shape a PostgreSQL enum array uses.

// Every fixture below carries a `gql-` email prefix and cleans up after itself
// via t.Cleanup + HardDelete, matching `set_test.go`'s convention. The module's
// tests share one MySQL container and each owns its own rows, so a blanket
// truncate here would clobber fixtures the non-API tests rely on; scoped
// cleanup also keeps `-count=2` from tripping the unique email constraint.

// createUserWithPermissions runs the createUser mutation with the given SET
// members and returns the id plus the permissions the server echoed back. The
// created row is removed when the test ends.
func createUserWithPermissions(t *testing.T, email string, perms []string) (int64, []string) {
	t.Helper()
	var out struct {
		CreateUser struct {
			ID          int64    `json:"id"`
			Permissions []string `json:"permissions"`
		} `json:"createUser"`
	}
	gqlExecData(t, `
		mutation ($email: String!, $permissions: [UsersPermissionsSetValue!]!) {
			createUser(input: {
				name: "SET probe", email: $email, role: VIEWER,
				permissions: $permissions, isActive: true, balance: 0,
				loginCount: 0, createdAt: "2026-01-01T00:00:00Z",
				updatedAt: "2026-01-01T00:00:00Z"
			}) { id permissions }
		}
	`, map[string]any{"email": email, "permissions": perms}, &out)
	id := out.CreateUser.ID
	t.Cleanup(func() { _ = newClient().Users().HardDelete(context.Background(), id) })
	return id, out.CreateUser.Permissions
}

// TestGraphQLSetColumnRoundTrip is the core pin: a SET column written and read
// back through the live handler as a list of GraphQL enum identifiers.
//
// The wire form is the SCREAMING_SNAKE_CASE identifier (`READ`), while the SQL
// literal stored in the column stays lowercase (`read`) — the same split
// schema enums have. Both halves are asserted, because a marshaler that
// round-trips through GraphQL while writing the wrong literal into MySQL would
// pass a wire-only check and silently corrupt the column.
func TestGraphQLSetColumnRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		perms    []string
		wantSQL  string
		wantBack []string
	}{
		{
			name:     "single member",
			email:    "gql-set-single@example.com",
			perms:    []string{"READ"},
			wantSQL:  "read",
			wantBack: []string{"READ"},
		},
		{
			name:     "several members",
			email:    "gql-set-several@example.com",
			perms:    []string{"READ", "WRITE", "ADMIN"},
			wantSQL:  "read,write,admin",
			wantBack: []string{"READ", "WRITE", "ADMIN"},
		},
		{
			// A SET column with no members is stored as the empty string, not
			// NULL. The GraphQL type is `[…!]!`, so the empty case must come
			// back as an empty list rather than null.
			name:     "empty set",
			email:    "gql-set-empty@example.com",
			perms:    []string{},
			wantSQL:  "",
			wantBack: []string{},
		},
		{
			name:     "every member",
			email:    "gql-set-all@example.com",
			perms:    []string{"READ", "WRITE", "DELETE", "ADMIN"},
			wantSQL:  "read,write,delete,admin",
			wantBack: []string{"READ", "WRITE", "DELETE", "ADMIN"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, echoed := createUserWithPermissions(t, tt.email, tt.perms)

			// The mutation's own return value already exercises the read path
			// through the row struct's named-slice binding.
			if diff := cmp.Diff(tt.wantBack, echoed); diff != "" {
				t.Errorf("createUser echo mismatch (-want +got):\n%s", diff)
			}

			// Re-read through a separate query so the value comes back off a
			// database round-trip (Scan) rather than out of the input struct.
			var got struct {
				User struct {
					Permissions []string `json:"permissions"`
				} `json:"user"`
			}
			gqlExecData(t, `
				query ($id: Int!) { user(id: $id) { permissions } }
			`, map[string]any{"id": id}, &got)
			if diff := cmp.Diff(tt.wantBack, got.User.Permissions); diff != "" {
				t.Errorf("re-read mismatch (-want +got):\n%s", diff)
			}

			// MySQL normalizes SET storage to the column's declared member
			// order, which for these fixtures is also the order written.
			var stored string
			row := testDB.QueryRowContext(context.Background(),
				"SELECT permissions FROM users WHERE id = ?", id)
			if err := row.Scan(&stored); err != nil {
				t.Fatalf("reading stored SET literal: %v", err)
			}
			if stored != tt.wantSQL {
				t.Errorf("stored SET literal = %q, want %q — the GraphQL identifier must not reach the column", stored, tt.wantSQL)
			}
		})
	}
}

// TestGraphQLSetColumnRejectsUnknownMember pins the inbound validation half.
// gqlgen validates against the generated `enum UsersPermissionsSetValue`
// before any resolver runs, so an unknown member is a typed request error
// rather than an invalid literal handed to MySQL.
func TestGraphQLSetColumnRejectsUnknownMember(t *testing.T) {
	resp := gqlExec(t, `
		mutation ($permissions: [UsersPermissionsSetValue!]!) {
			createUser(input: {
				name: "SET probe", email: "gql-set-invalid@example.com", role: VIEWER,
				permissions: $permissions, isActive: true, balance: 0,
				loginCount: 0, createdAt: "2026-01-01T00:00:00Z",
				updatedAt: "2026-01-01T00:00:00Z"
			}) { id }
		}
	`, map[string]any{"permissions": []string{"SUPERUSER"}})

	if len(resp.Errors) == 0 {
		t.Fatal("createUser accepted an unknown SET member, want a validation error")
	}
	if !strings.Contains(resp.Errors[0].Message, "SUPERUSER") {
		t.Errorf("error = %q, want it to name the rejected member", resp.Errors[0].Message)
	}
}

// TestGraphQLSetColumnUpdate pins the update input, which is the shape whose
// nullability differs from create: every update field is optional, so gqlgen
// emits the list as `[UsersPermissionsSetValue!]` there. It still arrives as a
// bare `[]T` (not `*[]T`) because a list is already nil-able, which is exactly
// what lets the generated translator cast straight to the named slice with no
// deref.
func TestGraphQLSetColumnUpdate(t *testing.T) {
	id, _ := createUserWithPermissions(t, "gql-set-update@example.com", []string{"READ"})

	var out struct {
		UpdateUser struct {
			Permissions []string `json:"permissions"`
		} `json:"updateUser"`
	}
	gqlExecData(t, `
		mutation ($id: Int!, $permissions: [UsersPermissionsSetValue!]) {
			updateUser(id: $id, input: { permissions: $permissions }) { permissions }
		}
	`, map[string]any{"id": id, "permissions": []string{"WRITE", "DELETE"}}, &out)

	if diff := cmp.Diff([]string{"WRITE", "DELETE"}, out.UpdateUser.Permissions); diff != "" {
		t.Errorf("updateUser mismatch (-want +got):\n%s", diff)
	}

	// Omitting the field entirely must leave the column untouched — the
	// omittable wrap is what carries "not provided" through the translator.
	var untouched struct {
		UpdateUser struct {
			Permissions []string `json:"permissions"`
		} `json:"updateUser"`
	}
	gqlExecData(t, `
		mutation ($id: Int!) {
			updateUser(id: $id, input: { bio: "unrelated edit" }) { permissions }
		}
	`, map[string]any{"id": id}, &untouched)

	if diff := cmp.Diff([]string{"WRITE", "DELETE"}, untouched.UpdateUser.Permissions); diff != "" {
		t.Errorf("omitted permissions changed the column (-want +got):\n%s", diff)
	}
}

// TestGraphQLKSUIDScalar covers the second column that had to be bound before
// this example could enable its API: `warehouses.external_id` is retyped to
// ksuid.KSUID by a table-level override, a Go type the built-in registry does
// not own, so it needs a consumer `api.graphql.scalars` declaration (§26.4.1).
// It rides here because it shares this example's reason for existing — a Go
// type that reaches no other module's GraphQL surface.
func TestGraphQLKSUIDScalar(t *testing.T) {
	const externalID = "2SxTgVeVvGDJfBrmYnHDCBaKCQm"
	var out struct {
		CreateWarehouse struct {
			ID         int64  `json:"id"`
			ExternalID string `json:"externalID"`
		} `json:"createWarehouse"`
	}
	gqlExecData(t, `
		mutation ($externalID: KSUID!) {
			createWarehouse(input: {
				externalID: $externalID, name: "Depot", price: "10.00",
				createdAt: "2026-01-01T00:00:00Z"
			}) { id externalID }
		}
	`, map[string]any{"externalID": externalID}, &out)
	t.Cleanup(func() { _ = newClient().Warehouses().HardDelete(context.Background(), out.CreateWarehouse.ID) })

	if out.CreateWarehouse.ExternalID != externalID {
		t.Errorf("KSUID create echo = %q, want %q", out.CreateWarehouse.ExternalID, externalID)
	}

	var got struct {
		Warehouse struct {
			ExternalID string `json:"externalID"`
		} `json:"warehouse"`
	}
	gqlExecData(t, `
		query ($id: Int!) { warehouse(id: $id) { externalID } }
	`, map[string]any{"id": out.CreateWarehouse.ID}, &got)

	if got.Warehouse.ExternalID != externalID {
		t.Errorf("KSUID re-read = %q, want %q", got.Warehouse.ExternalID, externalID)
	}
}
