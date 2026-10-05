package tests

import (
	"testing"

	uuid "github.com/gofrs/uuid/v5"
)

// TestUUIDViaColumnMapOverride is the column_map retype fixture, moved here
// from the `graphql` example when that one adopted the standard library as its
// UUID binding.
//
// `entries.upstream_ref` is a TEXT column. Nothing in the resolution chain maps
// TEXT to uuid.UUID, so the Go type — and therefore the `UUID` scalar this
// field carries — exists only because `column_map.upstream_ref.type` + `.import`
// put it there. Round-tripping a value proves the override survives the whole
// path: row struct, gqlgen model binding, generated marshaler, and the driver.
//
// The driver hop is why this test belongs on a wrapper-backed integration.
// gofrs's uuid.UUID ships sql.Scanner / driver.Valuer, so pgx can scan a TEXT
// column into it. The standard library's ships neither, and pgx registers its
// UUIDCodec against the `uuid` OID, so the identical config fails there with
// `cannot scan text (OID 25) in text format into *uuid.UUID` (PRD §7.4).
//
// The column is nullable and `column_map` has no `nullable:` sub-field, so the
// field is `*uuid.UUID` rather than the `uuid.NullUUID` this module's
// `overrides.types` entries produce — the one column here that does not follow
// `use_pointers: false`.
func TestUUIDViaColumnMapOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	truncateAll(t)

	gqlExecData(t, `mutation {
		createAccount(input: {name: "column-map acct", externalRef: 1, createdAt: "2026-01-01T00:00:00Z"}) { id }
	}`, nil, nil)

	upstreamRef := uuid.Must(uuid.NewV4()).String()

	var created struct {
		CreateEntry struct {
			ID          int64   `json:"id"`
			UpstreamRef *string `json:"upstreamRef"`
		} `json:"createEntry"`
	}
	gqlExecData(t, `
		mutation ($upstreamRef: UUID) {
			createEntry(input: {
				accountID: 1, memo: "column-map", amount: "10.00",
				upstreamRef: $upstreamRef, createdAt: "2026-01-01T00:00:00Z"
			}) { id upstreamRef }
		}
	`, map[string]any{"upstreamRef": upstreamRef}, &created)

	if created.CreateEntry.UpstreamRef == nil {
		t.Fatal("create: upstreamRef came back null, want the value written")
	}
	if *created.CreateEntry.UpstreamRef != upstreamRef {
		t.Errorf("create: upstreamRef = %q, want %q", *created.CreateEntry.UpstreamRef, upstreamRef)
	}

	// Read it back through the query path, which scans the column rather than
	// projecting it from RETURNING.
	var fetched struct {
		Entry struct {
			UpstreamRef *string `json:"upstreamRef"`
		} `json:"entry"`
	}
	gqlExecData(t, `
		query ($id: Int64!) {
			entry(id: $id) { upstreamRef }
		}
	`, map[string]any{"id": created.CreateEntry.ID}, &fetched)

	if fetched.Entry.UpstreamRef == nil {
		t.Fatal("read: upstreamRef came back null, want the value written")
	}
	if *fetched.Entry.UpstreamRef != upstreamRef {
		t.Errorf("read: upstreamRef = %q, want %q", *fetched.Entry.UpstreamRef, upstreamRef)
	}

	// Unset stays null — the pointer shape carries the null state directly,
	// with no wrapper Valid flag in the path.
	var unset struct {
		CreateEntry struct {
			UpstreamRef *string `json:"upstreamRef"`
		} `json:"createEntry"`
	}
	gqlExecData(t, `mutation {
		createEntry(input: {accountID: 1, memo: "no ref", amount: "1.00", createdAt: "2026-01-01T00:00:00Z"}) { upstreamRef }
	}`, nil, &unset)

	if unset.CreateEntry.UpstreamRef != nil {
		t.Errorf("unset: upstreamRef = %q, want null", *unset.CreateEntry.UpstreamRef)
	}
}
