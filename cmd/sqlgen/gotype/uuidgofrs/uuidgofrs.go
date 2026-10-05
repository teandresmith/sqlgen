// Package uuidgofrs provides a built-in type mapping for github.com/gofrs/uuid/v5.
package uuidgofrs

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// ImportPath is the Go import path for the gofrs/uuid library.
const ImportPath = "github.com/gofrs/uuid/v5"

// SQLTypes returns the SQL types this integration handles.
func SQLTypes() []string {
	return []string{"uuid"}
}

// Override returns the TypeOverride for UUID columns using gofrs/uuid.
// Both uuid.UUID and uuid.NullUUID implement sql.Scanner/driver.Valuer, which
// is what PRD §4.7 requires of any overridden type.
func Override() config.TypeOverride {
	return config.TypeOverride{
		Type:      "uuid.UUID",
		Import:    ImportPath,
		ZeroValue: "uuid.Nil",
		Nullable: config.NullableVariant{
			Type:            "uuid.NullUUID",
			UnderlyingField: "UUID",
		},
	}
}

// The Go expressions that generate a new UUID with this integration (PRD §7.4
// "Generating UUID values").
//
// Both constructors return (UUID, error) and the package exports no NewString,
// so every form goes through the package's own Must.
const (
	// ParseFunc is the string constructor the GraphQL scalar unmarshalers
	// call (PRD §7.4 "Parsing a UUID from a string"). gofrs exports no Parse
	// at all — FromString is its string constructor, and it returns the same
	// (UUID, error) pair the other two do, so only the name differs. The
	// FromStringOrNil variant is deliberately not used: an unparseable scalar
	// input is a client error the unmarshaler reports, not one it collapses
	// to the zero UUID.
	ParseFunc = "uuid.FromString"

	V4Expr       = "uuid.Must(uuid.NewV4())"
	V7Expr       = "uuid.Must(uuid.NewV7())"
	V4StringExpr = "uuid.Must(uuid.NewV4()).String()"
	V7StringExpr = "uuid.Must(uuid.NewV7()).String()"
)
