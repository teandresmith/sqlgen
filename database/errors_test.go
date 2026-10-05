package database_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/teandresmith/sqlgen/database"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrNotFound",
			err:     database.ErrNotFound,
			wantMsg: "sqlgen: resource not found",
		},
		{
			name:    "ErrEmptyFilter",
			err:     database.ErrEmptyFilter,
			wantMsg: "sqlgen: empty filter on bulk operation",
		},
		{
			name:    "ErrNilInput",
			err:     database.ErrNilInput,
			wantMsg: "sqlgen: nil input",
		},
		{
			name:    "ErrAmbiguousFilter",
			err:     database.ErrAmbiguousFilter,
			wantMsg: "sqlgen: filter contains both And and Or at the same level",
		},
		{
			name:    "ErrInvalidCursor",
			err:     database.ErrInvalidCursor,
			wantMsg: "sqlgen: invalid cursor",
		},
		{
			name:    "ErrDeadlock",
			err:     database.ErrDeadlock,
			wantMsg: "sqlgen: deadlock detected",
		},
		{
			name:    "ErrConnectionFailed",
			err:     database.ErrConnectionFailed,
			wantMsg: "sqlgen: connection failed",
		},
		{
			name:    "ErrConstraintViolation",
			err:     database.ErrConstraintViolation,
			wantMsg: "sqlgen: constraint violation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("%s.Error() = %q, want %q", tt.name, got, tt.wantMsg)
			}
		})
	}
}

func TestConstraintTypeValues(t *testing.T) {
	tests := []struct {
		name string
		ct   database.ConstraintType
		want string
	}{
		{name: "ConstraintUnique", ct: database.ConstraintUnique, want: "unique"},
		{name: "ConstraintForeignKey", ct: database.ConstraintForeignKey, want: "foreign_key"},
		{name: "ConstraintCheck", ct: database.ConstraintCheck, want: "check"},
		{name: "ConstraintNotNull", ct: database.ConstraintNotNull, want: "not_null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.ct); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestConstraintErrorIs(t *testing.T) {
	ce := &database.ConstraintError{
		Type:       database.ConstraintUnique,
		Constraint: "users_email_key",
		Column:     "email",
		Detail:     "Key (email)=(test@example.com) already exists.",
		Err:        errors.New("driver error"),
	}

	if !errors.Is(ce, database.ErrConstraintViolation) {
		t.Error("errors.Is(ConstraintError, ErrConstraintViolation) = false, want true")
	}
}

func TestConstraintErrorIsWrapped(t *testing.T) {
	ce := &database.ConstraintError{
		Type:       database.ConstraintForeignKey,
		Constraint: "orders_user_id_fkey",
		Column:     "user_id",
		Detail:     "Key (user_id)=(999) is not present.",
		Err:        errors.New("driver error"),
	}

	wrapped := fmt.Errorf("create order: %w", ce)

	if !errors.Is(wrapped, database.ErrConstraintViolation) {
		t.Error("errors.Is(wrapped ConstraintError, ErrConstraintViolation) = false, want true")
	}
}

func TestConstraintErrorAs(t *testing.T) {
	ce := &database.ConstraintError{
		Type:       database.ConstraintUnique,
		Constraint: "products_name_key",
		Column:     "name",
		Detail:     "Key (name)=(Widget) already exists.",
		Err:        errors.New("driver error"),
	}

	wrapped := fmt.Errorf("create product: %w", ce)

	var got *database.ConstraintError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As(wrapped, &ConstraintError) = false, want true")
	}
	if got.Type != database.ConstraintUnique {
		t.Errorf("ConstraintError.Type = %q, want %q", got.Type, database.ConstraintUnique)
	}
	if got.Constraint != "products_name_key" {
		t.Errorf("ConstraintError.Constraint = %q, want %q", got.Constraint, "products_name_key")
	}
	if got.Column != "name" {
		t.Errorf("ConstraintError.Column = %q, want %q", got.Column, "name")
	}
	if got.Detail != "Key (name)=(Widget) already exists." {
		t.Errorf("ConstraintError.Detail = %q, want %q", got.Detail, "Key (name)=(Widget) already exists.")
	}
	if got.Err == nil {
		t.Error("ConstraintError.Err = nil, want non-nil")
	}
}

func TestConstraintErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *database.ConstraintError
		want string
	}{
		{
			name: "all fields populated",
			err: &database.ConstraintError{
				Type:       database.ConstraintUnique,
				Constraint: "users_email_key",
				Column:     "email",
				Detail:     "Key (email)=(test@example.com) already exists.",
				Err:        errors.New("driver error"),
			},
			want: `sqlgen: unique constraint violation on constraint users_email_key (column email): Key (email)=(test@example.com) already exists.`,
		},
		{
			name: "no constraint name",
			err: &database.ConstraintError{
				Type:   database.ConstraintNotNull,
				Column: "name",
				Detail: "Column 'name' cannot be null",
				Err:    errors.New("driver error"),
			},
			want: `sqlgen: not_null constraint violation (column name): Column 'name' cannot be null`,
		},
		{
			name: "no column",
			err: &database.ConstraintError{
				Type:       database.ConstraintForeignKey,
				Constraint: "orders_user_id_fkey",
				Detail:     "Key (user_id)=(999) is not present.",
				Err:        errors.New("driver error"),
			},
			want: `sqlgen: foreign_key constraint violation on constraint orders_user_id_fkey: Key (user_id)=(999) is not present.`,
		},
		{
			name: "type and detail only",
			err: &database.ConstraintError{
				Type:   database.ConstraintCheck,
				Detail: "new row violates check constraint",
				Err:    errors.New("driver error"),
			},
			want: `sqlgen: check constraint violation: new row violates check constraint`,
		},
		{
			name: "type only",
			err: &database.ConstraintError{
				Type: database.ConstraintUnique,
				Err:  errors.New("driver error"),
			},
			want: `sqlgen: unique constraint violation`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("ConstraintError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConstraintErrorUnwrapChain(t *testing.T) {
	driverErr := errors.New("pq: duplicate key value violates unique constraint")
	ce := &database.ConstraintError{
		Type:       database.ConstraintUnique,
		Constraint: "users_email_key",
		Column:     "email",
		Detail:     "Key (email)=(test@example.com) already exists.",
		Err:        driverErr,
	}

	// Wrap twice to test the full chain
	wrapped := fmt.Errorf("create user: %w", ce)

	// errors.Is should find ErrConstraintViolation through the chain
	if !errors.Is(wrapped, database.ErrConstraintViolation) {
		t.Error("errors.Is through wrapped chain = false, want true")
	}

	// errors.As should extract the ConstraintError
	var extracted *database.ConstraintError
	if !errors.As(wrapped, &extracted) {
		t.Fatal("errors.As through wrapped chain = false, want true")
	}
	if !errors.Is(extracted.Err, driverErr) {
		t.Errorf("ConstraintError.Err = %v, want %v", extracted.Err, driverErr)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []error{
		database.ErrNotFound,
		database.ErrEmptyFilter,
		database.ErrNilInput,
		database.ErrAmbiguousFilter,
		database.ErrInvalidCursor,
		database.ErrDeadlock,
		database.ErrConnectionFailed,
		database.ErrConstraintViolation,
		database.ErrRefreshConcurrentlyInTx,
		database.ErrAlreadyRelated,
		database.ErrNestedVerbConflict,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = true, want false", a, b)
			}
		}
	}
}

// TestNestedMutationErrorIs is the contract PRD §22.2 states for the nested
// wrapper: it carries the edge, verb and target id structurally, and still
// answers errors.Is for the sentinel it wraps — exactly as ConstraintError
// does for ErrConstraintViolation. Both directions matter: a consumer that
// only checks the sentinel keeps working, and one that wants the edge reads
// it off the struct instead of parsing it back out of the message.
func TestNestedMutationErrorIs(t *testing.T) {
	tests := []struct {
		name     string
		err      *database.NestedMutationError
		sentinel error
		other    error
		wantMsg  string
	}{
		{
			name:     "already related carries the offending id",
			err:      &database.NestedMutationError{Edge: "Categories", Verb: "connect", ID: 7, Err: database.ErrAlreadyRelated},
			sentinel: database.ErrAlreadyRelated,
			other:    database.ErrNestedVerbConflict,
			wantMsg:  "Categories: connect: 7: sqlgen: already related to another row",
		},
		{
			// The has-one occupancy refusal (PRD §9.9.6) names the child the
			// edge already holds, and the one message has to read right for it
			// as well as for a connect target parented elsewhere.
			name:     "occupied has-one names the child it already holds",
			err:      &database.NestedMutationError{Edge: "Badge", Verb: "create", ID: 42, Err: database.ErrAlreadyRelated},
			sentinel: database.ErrAlreadyRelated,
			other:    database.ErrNestedVerbConflict,
			wantMsg:  "Badge: create: 42: sqlgen: already related to another row",
		},
		{
			name:     "verb conflict without an id-scoped target",
			err:      &database.NestedMutationError{Edge: "Events", Verb: "disconnect", Err: database.ErrNestedVerbConflict},
			sentinel: database.ErrNestedVerbConflict,
			other:    database.ErrAlreadyRelated,
			wantMsg:  "Events: disconnect: sqlgen: conflicting verbs on one nested relationship",
		},
		{
			// A connect visibility miss is ErrNotFound (PRD §9.9.8) — the
			// wrapper is not restricted to the two new sentinels, and the
			// GraphQL mapper's NOT_FOUND arm depends on this still matching.
			name:     "visibility miss wraps ErrNotFound",
			err:      &database.NestedMutationError{Edge: "Categories", Verb: "connect", ID: "abc", Err: database.ErrNotFound},
			sentinel: database.ErrNotFound,
			other:    database.ErrAlreadyRelated,
			wantMsg:  "Categories: connect: abc: sqlgen: resource not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}

			// Wrapped the way a generated client method wraps it (PRD §22.3),
			// so the chain the consumer actually sees is what is checked.
			wrapped := fmt.Errorf("update user with related: %w", tt.err)

			if !errors.Is(wrapped, tt.sentinel) {
				t.Errorf("errors.Is(wrapped, %v) = false, want true", tt.sentinel)
			}
			if errors.Is(wrapped, tt.other) {
				t.Errorf("errors.Is(wrapped, %v) = true, want false — the wrapper must not match a sentinel it does not carry", tt.other)
			}

			extracted, ok := errors.AsType[*database.NestedMutationError](wrapped)
			if !ok {
				t.Fatal("errors.AsType[*NestedMutationError] through the wrapped chain = false, want true")
			}
			if extracted.Edge != tt.err.Edge || extracted.Verb != tt.err.Verb || extracted.ID != tt.err.ID {
				t.Errorf("extracted = {%q %q %v}, want {%q %q %v}",
					extracted.Edge, extracted.Verb, extracted.ID, tt.err.Edge, tt.err.Verb, tt.err.ID)
			}
		})
	}
}
