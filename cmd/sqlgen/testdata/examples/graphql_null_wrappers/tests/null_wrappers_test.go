package tests

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// accountFields is the projection every wrapper assertion reads back.
const accountFields = `id name externalRef nickname flag smallCount midCount bigCount ratio observedAt balance tenantRef parentID deletedAt`

// account mirrors accountFields. Every wrapper-typed field is a pointer so the
// tests can tell JSON null apart from a zero value — the whole point of the
// Null-wrapper scalars is that Valid=false marshals as null rather than as 0,
// "" or a zero timestamp.
type account struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	ExternalRef int64    `json:"externalRef"`
	Nickname    *string  `json:"nickname"`
	Flag        *bool    `json:"flag"`
	SmallCount  *int16   `json:"smallCount"`
	MidCount    *int32   `json:"midCount"`
	BigCount    *int64   `json:"bigCount"`
	Ratio       *float64 `json:"ratio"`
	ObservedAt  *string  `json:"observedAt"`
	Balance     *string  `json:"balance"`
	TenantRef   *string  `json:"tenantRef"`
	ParentID    *int64   `json:"parentID"`
	DeletedAt   *string  `json:"deletedAt"`
}

// TestNullWrappersRoundTripValues is the module-wide `use_pointers: false`
// proof: every nullable column here resolves to a database/sql wrapper (or, for
// balance / tenantRef, to an integration-owned Null variant), and each one is
// written and read back through a live gqlgen handler against a real
// PostgreSQL.
//
// The `graphql` example's `scalar_probes` table already pins the seven Go
// types' SCHEMA and input translator via a table-scoped override. What it
// cannot pin is that a whole module generated under the flag compiles and
// runs — the shape a non-compiling models package once went unnoticed in.
func TestNullWrappersRoundTripValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	var created struct {
		CreateAccount account `json:"createAccount"`
	}
	gqlExecData(t, `mutation {
		createAccount(input: {
			name: "valued"
			externalRef: 7
			createdAt: "2026-01-02T03:04:05Z"
			nickname: "nick"
			flag: true
			smallCount: 12
			midCount: 3400
			bigCount: 5600000
			ratio: 1.25
			observedAt: "2026-02-03T04:05:06Z"
			balance: "99.25"
			tenantRef: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		}) { `+accountFields+` }
	}`, nil, &created)

	got := created.CreateAccount
	assertEqPtr(t, "nickname", got.Nickname, "nick")
	assertEqPtr(t, "flag", got.Flag, true)
	assertEqPtr(t, "smallCount", got.SmallCount, int16(12))
	assertEqPtr(t, "midCount", got.MidCount, int32(3400))
	assertEqPtr(t, "bigCount", got.BigCount, int64(5600000))
	assertEqPtr(t, "ratio", got.Ratio, 1.25)
	assertEqTimePtr(t, "observedAt", got.ObservedAt, "2026-02-03T04:05:06Z")
	assertEqPtr(t, "balance", got.Balance, "99.25")
	assertEqPtr(t, "tenantRef", got.TenantRef, "6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	// Read back through a separate query so the assertions cover what the
	// database stored, not just what the mutation echoed. A wrapper whose
	// Valuer wrote the zero value while its Marshaler echoed the input would
	// pass the block above and fail here.
	var fetched struct {
		Account account `json:"account"`
	}
	gqlExecData(t, `query { account(id: 1) { `+accountFields+` } }`, nil, &fetched)

	// cmp.Diff compares through the pointers, so a null/non-null mismatch or a
	// differing value both show up; == would only compare addresses.
	if diff := cmp.Diff(got, fetched.Account); diff != "" {
		t.Errorf("read-back differs from mutation response (-mutation +query):\n%s", diff)
	}
}

// TestNullWrappersMarshalNull is the Valid=false half: an omitted nullable
// column must come back as JSON null, not as the wrapper's zero value. It also
// guards against a nullable `integer` being advertised as String in front of a
// non-string Go field.
func TestNullWrappersMarshalNull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	var created struct {
		CreateAccount account `json:"createAccount"`
	}
	gqlExecData(t, `mutation {
		createAccount(input: {
			name: "empty"
			externalRef: 7
			createdAt: "2026-01-02T03:04:05Z"
		}) { `+accountFields+` }
	}`, nil, &created)

	got := created.CreateAccount
	nulls := []struct {
		name string
		null bool
	}{
		{"nickname", got.Nickname == nil},
		{"flag", got.Flag == nil},
		{"smallCount", got.SmallCount == nil},
		{"midCount", got.MidCount == nil},
		{"bigCount", got.BigCount == nil},
		{"ratio", got.Ratio == nil},
		{"observedAt", got.ObservedAt == nil},
		{"balance", got.Balance == nil},
		{"tenantRef", got.TenantRef == nil},
		{"parentID", got.ParentID == nil},
		{"deletedAt", got.DeletedAt == nil},
	}
	for _, n := range nulls {
		if !n.null {
			t.Errorf("%s: omitted nullable column did not marshal as JSON null (got %+v)", n.name, got)
		}
	}
}

// TestNullWrappersFilter runs each wrapper column through its generated
// comparator translator. Under `use_pointers: false` a wrapper is classified by
// the type it WRAPS: a nullable `double precision`
// takes comparator.NullableNumber[float64], not [sql.NullFloat64], and
// sql.NullBool / sql.NullTime take NullableBool / NullableTime rather than
// falling through to a string comparator.
func TestNullWrappersFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	gqlExecData(t, `mutation {
		createAccount(input: {
			name: "filterable"
			externalRef: 7
			createdAt: "2026-01-02T03:04:05Z"
			nickname: "nick"
			flag: true
			smallCount: 12
			midCount: 3400
			bigCount: 5600000
			ratio: 1.25
			observedAt: "2026-02-03T04:05:06Z"
			balance: "99.25"
		}) { id }
	}`, nil, nil)

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"NullableString eq", `nickname: {eq: "nick"}`, 1},
		{"NullableString isNull", `nickname: {isNull: true}`, 0},
		{"NullableBoolean eq", `flag: {eq: true}`, 1},
		{"NullableNumeric int16 gt", `smallCount: {gt: 5}`, 1},
		{"NullableNumeric int32 lt", `midCount: {lt: 9999}`, 1},
		{"NullableNumeric int64 eq", `bigCount: {eq: 5600000}`, 1},
		{"NullableNumeric float64 gte", `ratio: {gte: 1.25}`, 1},
		{"NullableTime gt", `observedAt: {gt: "2026-01-01T00:00:00Z"}`, 1},
		{"NullableTime isNull", `observedAt: {isNull: true}`, 0},
		{"NullableDecimal gt", `balance: {gt: "50.00"}`, 1},
		{"NullableNumeric FK isNull", `parentID: {isNull: true}`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out struct {
				AccountList struct {
					Items []account `json:"items"`
				} `json:"accountList"`
			}
			gqlExecData(t, `query { accountList(filter: {`+tt.filter+`}) { items { id } } }`, nil, &out)
			if len(out.AccountList.Items) != tt.want {
				t.Errorf("filter %s: got %d rows, want %d", tt.filter, len(out.AccountList.Items), tt.want)
			}
		})
	}
}

// assertEqTimePtr fails when p is nil or does not point at the same INSTANT as
// want. PostgreSQL renders timestamptz in the session timezone, so the wire
// string is "2026-02-02T21:05:06-07:00" for the same moment as
// "2026-02-03T04:05:06Z" — comparing the strings would assert the container's
// timezone rather than the value.
func assertEqTimePtr(t *testing.T, field string, p *string, want string) {
	t.Helper()

	if p == nil {
		t.Errorf("%s: got null, want %s", field, want)
		return
	}
	gotTime, err := time.Parse(time.RFC3339, *p)
	if err != nil {
		t.Errorf("%s: got %q, which is not RFC3339: %v", field, *p, err)
		return
	}
	wantTime, err := time.Parse(time.RFC3339, want)
	if err != nil {
		t.Fatalf("%s: want %q is not RFC3339: %v", field, want, err)
	}
	if !gotTime.Equal(wantTime) {
		t.Errorf("%s: got %s, want the same instant as %s", field, *p, want)
	}
}

// assertEqPtr fails when p is nil or does not point at want.
func assertEqPtr[T comparable](t *testing.T, field string, p *T, want T) {
	t.Helper()

	if p == nil {
		t.Errorf("%s: got null, want %v", field, want)
		return
	}
	if *p != want {
		t.Errorf("%s: got %v, want %v", field, *p, want)
	}
}
