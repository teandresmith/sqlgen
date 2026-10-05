package tests

import (
	"context"
	"testing"
)

// FK-primary-key create surface (PRD §8.6 / §26.4).
//
// `user_credentials.user_id` is both the primary key and a foreign key to
// `users`. Auto-detection classified such a key on SQL type alone, so a bare
// `uuid` PK with no DEFAULT resolved to `app` — the generated client minted a
// fresh UUID for it and dropped the column from the GraphQL create input
// entirely. A client had no way to name the parent row it was extending, and
// the minted key could never satisfy the foreign key.
//
// The strategy is now `caller`, so the column is a required `UUID!` on
// CreateUserCredentialInput. These tests drive that through the real GraphQL
// surface against a live Postgres container.

// TestFKPrimaryKeyCreate_NamesParentRow creates a 1:1 extension row against an
// existing user and reads it back by that key.
func TestFKPrimaryKeyCreate_NamesParentRow(t *testing.T) {
	truncateAll(t)

	user := seedUser(t, "fkpk@example.com", "FK PK User")

	type credentialPayload struct {
		UserID     string `json:"userID"`
		Provider   string `json:"provider"`
		ExternalID string `json:"externalID"`
	}

	var created struct {
		CreateUserCredential credentialPayload `json:"createUserCredential"`
	}
	gqlExecData(t, `
		mutation Extend($userID: UUID!, $createdAt: Time!) {
			createUserCredential(input: {
				userID: $userID
				provider: "oauth-github"
				externalID: "gh-42"
				createdAt: $createdAt
			}) {
				userID
				provider
				externalID
			}
		}
	`, map[string]any{
		"userID":    user.ID,
		"createdAt": fixedTimestamp,
	}, &created)

	// The PK must be the value supplied, not a server-minted UUID. Under the
	// pre-fix `app` strategy the field did not exist on the input at all.
	if created.CreateUserCredential.UserID != user.ID {
		t.Errorf("createUserCredential userID = %q, want %q (the parent row's key)",
			created.CreateUserCredential.UserID, user.ID)
	}
	if created.CreateUserCredential.Provider != "oauth-github" {
		t.Errorf("createUserCredential provider = %q, want %q",
			created.CreateUserCredential.Provider, "oauth-github")
	}

	var fetched struct {
		UserCredential *credentialPayload `json:"userCredential"`
	}
	gqlExecData(t, `
		query ($userID: UUID!) {
			userCredential(userID: $userID) {
				userID
				provider
				externalID
			}
		}
	`, map[string]any{"userID": user.ID}, &fetched)

	if fetched.UserCredential == nil {
		t.Fatal("userCredential: row not found after create")
	}
	if fetched.UserCredential.UserID != user.ID {
		t.Errorf("userCredential userID = %q, want %q", fetched.UserCredential.UserID, user.ID)
	}
	if fetched.UserCredential.ExternalID != "gh-42" {
		t.Errorf("userCredential externalID = %q, want %q", fetched.UserCredential.ExternalID, "gh-42")
	}

	// The row must satisfy the foreign key — that it exists at all proves the
	// insert did not write a minted key, but pin the join so a future
	// regression cannot pass by writing an orphan.
	var joined int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM user_credentials c JOIN users u ON u.id = c.user_id
	`).Scan(&joined); err != nil {
		t.Fatalf("count joined user_credentials: %v", err)
	}
	if joined != 1 {
		t.Errorf("user_credentials joined to users = %d rows, want 1", joined)
	}
}

// TestFKPrimaryKeyCreate_RejectsUnknownParent pins the constraint the caller
// strategy exists to respect: the supplied key must name a real parent row.
// A minted key would land here on every call.
func TestFKPrimaryKeyCreate_RejectsUnknownParent(t *testing.T) {
	truncateAll(t)

	resp := gqlExec(t, `
		mutation Orphan($userID: UUID!, $createdAt: Time!) {
			createUserCredential(input: {
				userID: $userID
				provider: "oauth-github"
				externalID: "gh-orphan"
				createdAt: $createdAt
			}) {
				userID
			}
		}
	`, map[string]any{
		"userID":    "00000000-0000-0000-0000-000000000001",
		"createdAt": fixedTimestamp,
	}, nil)

	if len(resp.Errors) == 0 {
		t.Fatal("createUserCredential with an unknown parent: got no error, want a foreign-key violation")
	}
}

// TestFKPrimaryKeyInputShape asserts the served schema itself, through
// introspection: the FK primary key is a required field on the create input
// and absent from the update input (§26.4 — a PK is the
// row's identity, addressed through the mutation's PK argument).
//
// Introspection rather than a malformed mutation: naming an unknown input
// field is a *validation* failure, which gqlgen answers with HTTP 422, and
// gqlExec fatals on any non-200. Asking the schema what it declares tests the
// same contract and is a well-formed 200 request.
func TestFKPrimaryKeyInputShape(t *testing.T) {
	type inputField struct {
		Name string `json:"name"`
		Type struct {
			Kind   string `json:"kind"`
			OfType struct {
				Name string `json:"name"`
			} `json:"ofType"`
		} `json:"type"`
	}
	var out struct {
		Create struct {
			InputFields []inputField `json:"inputFields"`
		} `json:"create"`
		Update struct {
			InputFields []inputField `json:"inputFields"`
		} `json:"update"`
	}
	gqlExecData(t, `
		query {
			create: __type(name: "CreateUserCredentialInput") {
				inputFields { name type { kind ofType { name } } }
			}
			update: __type(name: "UpdateUserCredentialInput") {
				inputFields { name type { kind ofType { name } } }
			}
		}
	`, nil, &out)

	find := func(fields []inputField, name string) (inputField, bool) {
		for _, f := range fields {
			if f.Name == name {
				return f, true
			}
		}
		return inputField{}, false
	}

	// Guard against a typo in the type name silently emptying both lists and
	// making every absence assertion below vacuously true.
	if len(out.Create.InputFields) == 0 {
		t.Fatal("CreateUserCredentialInput has no input fields — wrong type name?")
	}
	if len(out.Update.InputFields) == 0 {
		t.Fatal("UpdateUserCredentialInput has no input fields — wrong type name?")
	}

	userID, ok := find(out.Create.InputFields, "userID")
	if !ok {
		t.Fatalf("CreateUserCredentialInput is missing userID; has %v", out.Create.InputFields)
	}
	// Required, not optional: the FK PK is caller-supplied, so `UUID!`.
	if userID.Type.Kind != "NON_NULL" || userID.Type.OfType.Name != "UUID" {
		t.Errorf("CreateUserCredentialInput.userID type = %s of %q, want NON_NULL of \"UUID\"",
			userID.Type.Kind, userID.Type.OfType.Name)
	}

	if _, ok := find(out.Update.InputFields, "userID"); ok {
		t.Error("UpdateUserCredentialInput declares userID; a PK is never a settable attribute (§26.4)")
	}
	// The update input is otherwise populated, so the absence above is a rule
	// about PK columns rather than an empty type.
	if _, ok := find(out.Update.InputFields, "provider"); !ok {
		t.Errorf("UpdateUserCredentialInput is missing provider; has %v", out.Update.InputFields)
	}
}
