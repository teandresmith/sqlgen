// Package uuidstd provides a built-in type mapping for the standard library
// uuid package.
package uuidstd

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// ImportPath is the Go import path for the standard library uuid package.
const ImportPath = "uuid"

// SQLTypes returns the SQL types this integration handles.
func SQLTypes() []string {
	return []string{"uuid"}
}

// Override returns the TypeOverride for UUID columns using the standard
// library uuid package. uuid.UUID implements neither sql.Scanner nor
// driver.Valuer and needs neither: database/sql special-cases the type in
// both directions, and pgx unwraps it to [16]byte and routes it through
// UUIDCodec — the driver-special-cased alternative to Scanner/Valuer that
// PRD §4.7 admits (PRD §7.4).
//
// Nullable is deliberately left unset. The package ships no NullUUID, so a
// nullable column resolves to *uuid.UUID through FromOverride's
// value-typed branch. ZeroValue must be the composite literal: the package's
// Nil is a function, not a variable.
func Override() config.TypeOverride {
	return config.TypeOverride{
		Type:      "uuid.UUID",
		Import:    ImportPath,
		ZeroValue: "uuid.UUID{}",
	}
}

// The Go expressions that generate a new UUID with this integration (PRD §7.4
// "Generating UUID values"). The string forms are separate constants rather
// than V4Expr + ".String()" because the three integrations disagree: google's
// v4 string form is uuid.NewString(), which is not its value form with
// .String() appended.
//
// The v4 spelling is NewV4 rather than New on purpose. The package's New is
// documented as "an algorithm suitable for most purposes" and is not
// contractually v4, so an explicit uuid_version: v4 is honored with the call
// that names the version.
//
// Neither constructor returns an error and the package exports no Must, so
// neither form wraps.
const (
	// ParseFunc is the string constructor the GraphQL scalar unmarshalers
	// call (PRD §7.4 "Parsing a UUID from a string"). It is a bare function
	// name, not a call, because the caller supplies the argument.
	ParseFunc = "uuid.Parse"

	V4Expr       = "uuid.NewV4()"
	V7Expr       = "uuid.NewV7()"
	V4StringExpr = "uuid.NewV4().String()"
	V7StringExpr = "uuid.NewV7().String()"
)
