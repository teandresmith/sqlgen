package tests

import (
	"strings"
	"testing"
)

// maxSafeJSONInt is 2^53, the largest integer a IEEE-754 double represents
// exactly. A value above it survives a round trip only if every hop — the
// gqlgen input binding, the model field and the database column — is a true
// 64-bit integer. Under gqlgen's unpinned `Int64` extraBuiltin the GENERATED
// input position binds Model[0] (`graphql.Int` → Go `int`), which sqlgen
// avoids by pinning the `models:` entry to graphql.Int64.
const maxSafeJSONInt = int64(1) << 53

// TestBundledScalarInt64RoundTrip is the bundled-scalar proof this module
// exists for.
//
// `Int64: {go_type: int64, marshaling: builtin}` routes every int64 column onto
// gqlgen's own bundled Int64 marshaler. The defect it pins was "the generated
// module does not compile" — so the load-bearing evidence is that this package
// builds at all. The value assertions below add the runtime half: that the
// pinned marshaler is lossless rather than silently narrowing.
func TestBundledScalarInt64RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	// 2^53+1 is not representable as a float64, so a wire type of `Int`
	// (or any JSON-number path that goes through a double) corrupts it.
	const beyondDouble = maxSafeJSONInt + 1

	var created struct {
		CreateAccount struct {
			ID          int64 `json:"id"`
			ExternalRef int64 `json:"externalRef"`
		} `json:"createAccount"`
	}
	gqlExecData(t, `mutation {
		createAccount(input: {
			name: "int64 probe"
			externalRef: 9007199254740993
			createdAt: "2026-01-02T03:04:05Z"
			bigCount: 9007199254740993
		}) { id externalRef }
	}`, nil, &created)

	if created.CreateAccount.ExternalRef != beyondDouble {
		t.Errorf("externalRef round-trip: got %d, want %d", created.CreateAccount.ExternalRef, beyondDouble)
	}

	// The PK resolver argument is `Int64!` too (`account(id: Int64!)`), so the
	// read path exercises the same binding on an argument position rather than
	// an input-object field.
	var fetched struct {
		Account struct {
			ID          int64  `json:"id"`
			ExternalRef int64  `json:"externalRef"`
			BigCount    *int64 `json:"bigCount"`
		} `json:"account"`
	}
	gqlExecData(t, `query { account(id: 1) { id externalRef bigCount } }`, nil, &fetched)

	if fetched.Account.ExternalRef != beyondDouble {
		t.Errorf("externalRef read-back: got %d, want %d", fetched.Account.ExternalRef, beyondDouble)
	}
	if fetched.Account.BigCount == nil || *fetched.Account.BigCount != beyondDouble {
		t.Errorf("bigCount read-back: got %v, want %d", fetched.Account.BigCount, beyondDouble)
	}
}

// TestBundledScalarInt64IsDeclaredInSchema pins the wire type itself. If the
// `Int64` declaration were dropped from sqlgen.yml, every assertion in
// TestBundledScalarInt64RoundTrip would still pass on a 64-bit host —
// `graphql.UnmarshalInt` is `interfaceToSignedNumber[int]` and only
// range-checks when strconv.IntSize == 32 — so the round-trip test alone does
// not prove the route is taken. The schema does.
func TestBundledScalarInt64IsDeclaredInSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	var out struct {
		Type struct {
			Fields []struct {
				Name string `json:"name"`
				Type struct {
					OfType struct {
						Name string `json:"name"`
					} `json:"ofType"`
				} `json:"type"`
			} `json:"fields"`
		} `json:"__type"`
	}
	gqlExecData(t, `query { __type(name: "Account") { fields { name type { ofType { name } } } } }`, nil, &out)

	want := map[string]string{"id": "Int64", "externalRef": "Int64"}
	seen := 0
	for _, f := range out.Type.Fields {
		if w, ok := want[f.Name]; ok {
			seen++
			if f.Type.OfType.Name != w {
				t.Errorf("Account.%s: bound to %q, want %q", f.Name, f.Type.OfType.Name, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("introspection returned %d of %d expected fields", seen, len(want))
	}
}

// TestBundledScalarInt64RejectsOverflow pins the guard the pinned marshaler
// brings with it: gqlgen's UnmarshalInt64 refuses a value that is not an
// integer, so the scalar fails loudly rather than truncating.
func TestBundledScalarInt64RejectsOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	resp := gqlExec(t, `mutation {
		createAccount(input: {
			name: "bad int64"
			externalRef: 1.5
			createdAt: "2026-01-02T03:04:05Z"
		}) { id }
	}`, nil)
	if len(resp.Errors) == 0 {
		t.Fatalf("expected a validation error for a non-integer Int64, got data: %s", resp.Data)
	}
	if !strings.Contains(strings.ToLower(resp.Errors[0].Message), "int64") {
		t.Errorf("error should name the Int64 scalar, got %q", resp.Errors[0].Message)
	}
}
