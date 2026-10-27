package tests

import (
	"testing"
)

// A NOT NULL column with a DB default is omittable on the Go create input, so
// the GraphQL create input leaves it optional: a client that omits it gets the
// default, and one that supplies it gets its own value.

// TestCreateInputDefaults_OmittedDefaultedColumnTakesTheDefault creates a user
// naming only the columns without a default.
func TestCreateInputDefaults_OmittedDefaultedColumnTakesTheDefault(t *testing.T) {
	truncateAll(t)

	var out struct {
		CreateUser struct {
			IsActive  bool    `json:"isActive"`
			CreatedAt *string `json:"createdAt"`
		} `json:"createUser"`
	}
	gqlExecData(t, `
		mutation {
			createUser(input: { email: "upf-defaults@example.com", name: "Defaults" }) { isActive createdAt }
		}
	`, nil, &out)

	if !out.CreateUser.IsActive {
		t.Errorf("createUser without isActive: isActive = false, want the column default true")
	}
	if out.CreateUser.CreatedAt == nil || *out.CreateUser.CreatedAt == "" {
		t.Errorf("createUser without createdAt: createdAt = %v, want the column default", out.CreateUser.CreatedAt)
	}
}

// TestCreateInputDefaults_SuppliedDefaultedColumnKeepsItsValue pins the other
// half: an optional field the client does send is written as sent.
func TestCreateInputDefaults_SuppliedDefaultedColumnKeepsItsValue(t *testing.T) {
	truncateAll(t)

	var out struct {
		CreateUser struct {
			IsActive bool `json:"isActive"`
		} `json:"createUser"`
	}
	gqlExecData(t, `
		mutation {
			createUser(input: { email: "upf-supplied@example.com", name: "Supplied", isActive: false }) { isActive }
		}
	`, nil, &out)

	if out.CreateUser.IsActive {
		t.Errorf("createUser(isActive: false): isActive = true, want false as supplied")
	}
}
