package database

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by generated client methods. Check with errors.Is().
var (
	// ErrNotFound is returned when Get/Exists finds no matching row.
	ErrNotFound = errors.New("sqlgen: resource not found")

	// ErrEmptyFilter is returned when any *Where operation (UpdateWhere,
	// SoftDeleteWhere, RestoreWhere, HardDeleteWhere) is called with a nil
	// or empty filter. Prevents accidental bulk operations.
	ErrEmptyFilter = errors.New("sqlgen: empty filter on bulk operation")

	// ErrNilInput is returned when a write operation is called with a nil
	// input pointer — Create, Update, Upsert, UpdateWhere, or a nil element
	// inside a CreateMany / UpdateMany batch. Reads normalize a nil input to the
	// zero value instead — see PRD §9.4c — but a write cannot: a zero
	// Create<T>Input is a legal argument that writes a row. It sends each
	// required field's zero value, and with no required field the row takes
	// every column's default, so treating nil the same way would turn a
	// caller's mistake into a silently written row.
	ErrNilInput = errors.New("sqlgen: nil input")

	// ErrAmbiguousFilter is returned when a filter contains both And and Or
	// conditions at the same level, making the intent unclear.
	ErrAmbiguousFilter = errors.New("sqlgen: filter contains both And and Or at the same level")

	// ErrInvalidCursor is returned when a pagination cursor cannot be decoded,
	// parsed, or does not contain the expected keys. Also returned when both
	// first and last are provided simultaneously.
	ErrInvalidCursor = errors.New("sqlgen: invalid cursor")

	// ErrDeadlock is returned when the database detects a deadlock between
	// concurrent transactions and aborts this one. Consumers should typically
	// retry the transaction.
	ErrDeadlock = errors.New("sqlgen: deadlock detected")

	// ErrConnectionFailed is returned when a database operation fails due to
	// a connection-level error (connection refused, reset, timeout, pool
	// exhaustion). Wraps the underlying driver error for inspection.
	ErrConnectionFailed = errors.New("sqlgen: connection failed")

	// ErrConstraintViolation is the base error for all constraint failures.
	// Use errors.As() to extract the specific ConstraintError.
	ErrConstraintViolation = errors.New("sqlgen: constraint violation")

	// ErrRefreshConcurrentlyInTx is returned when RefreshConcurrently on a
	// materialized-view client is called while an ambient transaction is
	// present. PostgreSQL forbids REFRESH MATERIALIZED VIEW CONCURRENTLY
	// inside a transaction block; the generated method fails fast before
	// issuing any SQL instead of surfacing an opaque server error.
	ErrRefreshConcurrentlyInTx = errors.New("sqlgen: REFRESH MATERIALIZED VIEW CONCURRENTLY cannot run inside a transaction")

	// ErrAlreadyRelated is returned when a nested write would relate a row
	// that is already related elsewhere: a `connect` names a target already
	// parented to a different row and the edge does not set allow_reparent,
	// or a has-one `create` or `connect` finds the parent already holding a
	// different child. Use errors.As to extract the *NestedMutationError for
	// the edge, verb and id. See PRD §9.9.
	ErrAlreadyRelated = errors.New("sqlgen: already related to another row")

	// ErrNestedVerbConflict is returned before any statement on the edge runs when one
	// nested block names the same target under two verbs, or when a
	// self-referential edge names the parent's own primary key. See PRD §9.9.
	ErrNestedVerbConflict = errors.New("sqlgen: conflicting verbs on one nested relationship")
)

// ConstraintType identifies the kind of database constraint that was violated.
type ConstraintType string

const (
	// ConstraintUnique indicates a UNIQUE or PRIMARY KEY violation.
	ConstraintUnique ConstraintType = "unique"

	// ConstraintForeignKey indicates a FOREIGN KEY violation.
	ConstraintForeignKey ConstraintType = "foreign_key"

	// ConstraintCheck indicates a CHECK constraint violation.
	ConstraintCheck ConstraintType = "check"

	// ConstraintNotNull indicates a NOT NULL violation.
	ConstraintNotNull ConstraintType = "not_null"
)

// ConstraintError represents a database constraint violation. It wraps
// ErrConstraintViolation so that errors.Is(err, ErrConstraintViolation)
// returns true. Use errors.As to extract the structured fields.
type ConstraintError struct {
	Type       ConstraintType // Unique, ForeignKey, Check, NotNull
	Constraint string         // constraint name from the database (e.g., "products_name_key")
	Column     string         // affected column, if detectable
	Detail     string         // database-provided detail message
	Err        error          // original driver error (wrapped)
}

// Error returns a human-readable error message including the constraint type,
// constraint name, and detail when available.
func (e *ConstraintError) Error() string {
	msg := fmt.Sprintf("sqlgen: %s constraint violation", e.Type)
	if e.Constraint != "" {
		msg += fmt.Sprintf(" on constraint %s", e.Constraint)
	}
	if e.Column != "" {
		msg += fmt.Sprintf(" (column %s)", e.Column)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap returns ErrConstraintViolation so that errors.Is(err,
// ErrConstraintViolation) works through the error chain.
func (e *ConstraintError) Unwrap() error {
	return ErrConstraintViolation
}

// NestedMutationError reports which edge and which verb of a nested mutation
// failed, and on which target (PRD §22.2). It wraps one of the two nested
// sentinels, so errors.Is(err, ErrAlreadyRelated) keeps working through it
// exactly as it does through ConstraintError for ErrConstraintViolation.
//
// Edge and Verb carry the same attribution the wrapped message states (PRD
// §9.9.8), so a consumer — and the generated GraphQL error mapper — reads the
// failing relationship off the struct instead of parsing it back out of a
// string.
type NestedMutationError struct {
	Edge string // relationship field name, e.g. "Categories"
	// Verb is the nested verb that failed: "create", "connect", "disconnect",
	// "clear", or "link" — the M2M step that writes the junction rows for the
	// targets a create and a connect on one edge produced (PRD §9.9.6).
	Verb string
	ID   any // the offending target id (for an occupied has-one, the child the edge already holds); nil when the failure is not id-scoped
	// Err is the sentinel this failure carries — ErrAlreadyRelated,
	// ErrNestedVerbConflict, or ErrNotFound for a connect whose target is not
	// visible (PRD §9.9.8). Unwrap returns it, so which one it is decides what
	// errors.Is matches; there is no fixed sentinel the way ConstraintError
	// always unwraps to ErrConstraintViolation.
	Err error
}

// Error renders the edge, the verb, the offending id when there is one, and
// the wrapped cause. The caller's own wrapping supplies the operation and the
// table in front of it (PRD §22.3).
func (e *NestedMutationError) Error() string {
	if e.ID == nil {
		return fmt.Sprintf("%s: %s: %v", e.Edge, e.Verb, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v: %v", e.Edge, e.Verb, e.ID, e.Err)
}

// Unwrap returns the wrapped sentinel so that errors.Is(err,
// ErrAlreadyRelated) and errors.Is(err, ErrNestedVerbConflict) work through
// the error chain.
func (e *NestedMutationError) Unwrap() error {
	return e.Err
}
