package tests

import (
	"testing"
)

// TestNullableFKRelationshipLoaders covers the path the `graphql` example's
// `scalar_probes` fixture structurally cannot reach.
//
// `scalar_probes` is a standalone table — no FK, no relationship — so it pins
// the seven wrapper types' schema and input translator but never runs a
// relationship loader whose join column is a wrapper. `accounts.parent_id` is a
// NULLABLE self-FK, so under `use_pointers: false` the M2O parent lookup and
// the O2M children lookup both key on a sql.NullInt64: the loader has to read
// through the wrapper to build its IN list, and has to skip rows whose FK is
// NULL rather than treating the zero value as a real parent id.
func TestNullableFKRelationshipLoaders(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	// Root has a NULL parent_id; two children point at it.
	gqlExecData(t, `mutation {
		createAccount(input: {name: "root", externalRef: 1, createdAt: "2026-01-01T00:00:00Z"}) { id }
	}`, nil, nil)
	for _, name := range []string{"child-a", "child-b"} {
		gqlExecData(t, `mutation {
			createAccount(input: {name: "`+name+`", externalRef: 2, createdAt: "2026-01-01T00:00:00Z", parentID: 1}) { id }
		}`, nil, nil)
	}

	var out struct {
		AccountList struct {
			Items []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				ParentID *int64 `json:"parentID"`
				Accounts []struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"accounts"`
			} `json:"items"`
		} `json:"accountList"`
	}
	gqlExecData(t, `query {
		accountList(sort: [{field: ID, direction: ASC}]) {
			items { id name parentID accounts { id name } }
		}
	}`, nil, &out)

	if len(out.AccountList.Items) != 3 {
		t.Fatalf("got %d accounts, want 3", len(out.AccountList.Items))
	}

	root := out.AccountList.Items[0]
	if root.ParentID != nil {
		t.Errorf("root parentID: got %v, want null", *root.ParentID)
	}
	// The O2M loader keys on the wrapper. A loader that read the wrapper's
	// zero value instead of skipping NULL would make root its own child.
	if len(root.Accounts) != 2 {
		t.Errorf("root children: got %d, want 2 (%+v)", len(root.Accounts), root.Accounts)
	}

	for _, child := range out.AccountList.Items[1:] {
		if child.ParentID == nil || *child.ParentID != root.ID {
			t.Errorf("%s parentID: got %v, want %d", child.Name, child.ParentID, root.ID)
		}
		if len(child.Accounts) != 0 {
			t.Errorf("%s should have no children, got %d", child.Name, len(child.Accounts))
		}
	}
}

// TestNotNullFKRelationshipLoader pins the contrast case: `entries.account_id`
// is NOT NULL and, because the `Int64` scalar is declared, is an `Int64` on the
// wire while staying a bare int64 in the model. The O2M loader therefore keys
// on an unwrapped int64 in the same module where the sibling loader keys on a
// wrapper.
func TestNotNullFKRelationshipLoader(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	gqlExecData(t, `mutation {
		createAccount(input: {name: "acct", externalRef: 1, createdAt: "2026-01-01T00:00:00Z"}) { id }
	}`, nil, nil)
	for _, memo := range []string{"first", "second"} {
		gqlExecData(t, `mutation {
			createEntry(input: {accountID: 1, memo: "`+memo+`", amount: "10.00", createdAt: "2026-01-01T00:00:00Z"}) { id }
		}`, nil, nil)
	}

	var out struct {
		Account struct {
			Entries []struct {
				Memo *string `json:"memo"`
			} `json:"entries"`
		} `json:"account"`
	}
	gqlExecData(t, `query { account(id: 1) { entries { memo } } }`, nil, &out)

	if len(out.Account.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(out.Account.Entries))
	}
	for _, e := range out.Account.Entries {
		if e.Memo == nil {
			t.Errorf("entry memo came back null, want a value")
		}
	}
}
