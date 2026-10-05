package tests

import (
	"context"
	"strings"
	"testing"
	"uuid"
)

// Error-code coverage for PRD §26.5.5. Each subtest provokes one runtime
// error category through the live gqlgen handler and asserts the mapped
// `extensions.code` value (plus a sensible message substring) per the
// sentinel table in §26.5.5. UNAUTHENTICATED / FORBIDDEN are pinned in
// `tests/tenancy_test.go` rather than duplicated here.
//
// INTERNAL (the unmapped-error catchall) is the structural fallback in the
// mapper and is exercised whenever an upstream returns a non-sentinel
// error; we don't pin it from the live handler because every reachable
// failure path with this schema lands on one of the sentinel branches —
// pinning the catchall would require deliberately corrupting runtime state
// in a way that doesn't model real consumer behavior.

func TestErrorCodes_NotFound(t *testing.T) {
	truncateAll(t)

	// updateUser(id: <random>, input: { name: "x" }) — the underlying
	// Update returns database.ErrNotFound when the PK isn't present, and
	// the resolver pipes that through mapErrorToGQL (no soft-delete /
	// restore-style nil-on-miss branch — those exist only for Get and
	// HardDelete per §26.5.1).
	missingID := uuid.New().String()
	resp := gqlExec(t, `
		mutation ($id: UUID!) {
			updateUser(id: $id, input: { name: "missing" }) { id }
		}
	`, map[string]any{"id": missingID}, nil)

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "NOT_FOUND"; got != want {
		t.Errorf("extensions.code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "not found") {
		t.Errorf("message = %q, want substring %q", resp.Errors[0].Message, "not found")
	}
}

func TestErrorCodes_Conflict(t *testing.T) {
	truncateAll(t)

	// Seed a user; re-create with the same email to violate the
	// users.email UNIQUE constraint → pgx adapter wraps as
	// *database.ConstraintError{Type: ConstraintUnique} → CONFLICT.
	create := `
		mutation ($email: String!, $createdAt: Time!) {
			createUser(input: {
				email: $email, name: "Dup", isActive: true, createdAt: $createdAt
			}) { id }
		}`
	vars := map[string]any{
		"email":     "conflict@example.com",
		"createdAt": "2026-01-01T00:00:00Z",
	}

	if r := gqlExec(t, create, vars, nil); len(r.Errors) != 0 {
		t.Fatalf("seed user: unexpected errors: %+v", r.Errors)
	}
	resp := gqlExec(t, create, vars, nil)
	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "CONFLICT"; got != want {
		t.Errorf("extensions.code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "duplicate") {
		t.Errorf("message = %q, want substring %q", resp.Errors[0].Message, "duplicate")
	}
}

func TestErrorCodes_BadReference(t *testing.T) {
	truncateAll(t)

	// createOrder against a non-existent user_id — the orders.user_id FK
	// fires ConstraintForeignKey → BAD_REFERENCE. Note we pick an FK that
	// receives a syntactically valid UUID (so we get past gqlgen's input
	// validation) but no matching parent row.
	resp := gqlExec(t, `
		mutation ($userID: UUID!, $createdAt: Time!) {
			createOrder(input: {
				userID: $userID, total: "0.00", createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"userID":    uuid.New().String(),
		"createdAt": "2026-01-01T00:00:00Z",
	}, nil)

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "BAD_REFERENCE"; got != want {
		t.Errorf("extensions.code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "fk constraint") {
		t.Errorf("message = %q, want substring %q", resp.Errors[0].Message, "fk constraint")
	}
}

func TestErrorCodes_InvalidInput_Check(t *testing.T) {
	truncateAll(t)

	// Postgres CHECK constraints aren't part of the example schema (the
	// PRD's §26.4 mapping table doesn't surface them at the GraphQL
	// layer), so we add one in-flight against an existing managed table.
	// The constraint is dropped on cleanup regardless of test outcome so
	// later tests see the original schema. This is the cleanest way to
	// reach the ConstraintCheck → INVALID_INPUT branch through the live
	// handler — see §26.5.5 for the mapping.
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `ALTER TABLE products ADD CONSTRAINT chk_test_stock_below_threshold CHECK (stock < 1000000)`); err != nil {
		t.Fatalf("add check constraint: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `ALTER TABLE products DROP CONSTRAINT chk_test_stock_below_threshold`); err != nil {
			t.Errorf("drop check constraint (subsequent tests may see leaked schema): %v", err)
		}
	})

	cat := seedCategory(t, "InvalidInputCheck")
	resp := gqlExec(t, `
		mutation ($categoryID: Int!, $createdAt: Time!) {
			createProduct(input: {
				name: "boom", price: "1.00", stock: 9999999,
				categoryID: $categoryID, createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{
		"categoryID": cat.ID,
		"createdAt":  "2026-01-01T00:00:00Z",
	}, nil)

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "INVALID_INPUT"; got != want {
		t.Errorf("extensions.code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "check failed") {
		t.Errorf("message = %q, want substring %q", resp.Errors[0].Message, "check failed")
	}
}

func TestErrorCodes_InvalidInput_NotNull(t *testing.T) {
	truncateAll(t)

	// orders.notes is nullable in the live schema; tighten it temporarily
	// to NOT NULL with no default. createOrder without `notes` then emits
	// an INSERT that omits the column → postgres defaults to NULL → NOT
	// NULL violation → ConstraintNotNull → INVALID_INPUT.
	//
	// The input layer cannot send NULL through omittable for a column that
	// was originally NOT NULL (passing `null` collapses to Unset, so the
	// column is omitted from the INSERT entirely); inverting the column's
	// nullability under the test is the surgical way to reach the mapper's
	// ConstraintNotNull branch.
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `ALTER TABLE orders ALTER COLUMN notes SET NOT NULL`); err != nil {
		t.Fatalf("tighten orders.notes: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `ALTER TABLE orders ALTER COLUMN notes DROP NOT NULL`); err != nil {
			t.Errorf("restore orders.notes (subsequent tests may see leaked NOT NULL): %v", err)
		}
	})

	u := seedUser(t, "notnull@example.com", "NotNull")
	resp := gqlExec(t, `
		mutation ($userID: UUID!, $createdAt: Time!) {
			createOrder(input: {
				userID: $userID, total: "0.00", createdAt: $createdAt
			}) { id }
		}
	`, map[string]any{"userID": u.ID, "createdAt": "2026-01-01T00:00:00Z"}, nil)

	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %+v", len(resp.Errors), resp.Errors)
	}
	if got, want := extensionsCode(resp.Errors[0]), "INVALID_INPUT"; got != want {
		t.Errorf("extensions.code = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Errors[0].Message, "missing required") {
		t.Errorf("message = %q, want substring %q", resp.Errors[0].Message, "missing required")
	}
}
