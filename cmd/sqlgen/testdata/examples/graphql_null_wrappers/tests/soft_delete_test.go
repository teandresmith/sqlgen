package tests

import (
	"testing"
)

// TestSoftDeleteOnNullTimeWrapper covers the soft-delete path under
// `use_pointers: false`.
//
// PostgreSQL is the only dialect where the soft-delete column lands on a
// database/sql wrapper: `deleted_at TIMESTAMPTZ` resolves to sql.NullTime under
// the flag, where SQLite claims datetime for types.NullDateTime and never
// reaches the stdlib family. So the generated soft-delete predicate has to
// write and test a wrapper — `deleted_at IS NULL` on read, a valid wrapper on
// delete, an invalid one on restore — rather than a plain *time.Time.
//
// The comparator classification is what makes this more than a smoke test:
// sql.NullTime was falling through resolveSimpleComparator's default arm to a
// STRING comparator before it was fixed, which is exactly the classification
// the soft-delete filter depends on.
func TestSoftDeleteOnNullTimeWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	gqlExecData(t, `mutation {
		createAccount(input: {name: "doomed", externalRef: 1, createdAt: "2026-01-01T00:00:00Z"}) { id deletedAt }
	}`, nil, nil)

	assertListCount(t, "before delete", 1)

	// Soft delete stamps the wrapper.
	var deleted struct {
		SoftDeleteAccount struct {
			ID        int64   `json:"id"`
			DeletedAt *string `json:"deletedAt"`
		} `json:"softDeleteAccount"`
	}
	gqlExecData(t, `mutation { softDeleteAccount(id: 1) { id deletedAt } }`, nil, &deleted)
	if deleted.SoftDeleteAccount.DeletedAt == nil {
		t.Error("softDeleteAccount left deletedAt null; the sql.NullTime wrapper was not stamped")
	}

	// The default read filter excludes it.
	assertListCount(t, "after delete", 0)

	// Restore clears the wrapper back to Valid=false.
	var restored struct {
		RestoreAccount struct {
			DeletedAt *string `json:"deletedAt"`
		} `json:"restoreAccount"`
	}
	gqlExecData(t, `mutation { restoreAccount(id: 1) { id deletedAt } }`, nil, &restored)
	if restored.RestoreAccount.DeletedAt != nil {
		t.Errorf("restoreAccount left deletedAt set to %q; the wrapper was not cleared", *restored.RestoreAccount.DeletedAt)
	}

	assertListCount(t, "after restore", 1)
}

// assertListCount asserts how many accounts the default (soft-delete-filtered)
// list returns.
func assertListCount(t *testing.T, stage string, want int) {
	t.Helper()

	var out struct {
		AccountList struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		} `json:"accountList"`
	}
	gqlExecData(t, `query { accountList { items { id } } }`, nil, &out)
	if len(out.AccountList.Items) != want {
		t.Errorf("%s: got %d accounts, want %d", stage, len(out.AccountList.Items), want)
	}
}
