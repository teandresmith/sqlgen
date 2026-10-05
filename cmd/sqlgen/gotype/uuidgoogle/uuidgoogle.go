// Package uuidgoogle provides a built-in type mapping for github.com/google/uuid.
package uuidgoogle

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// ImportPath is the Go import path for the google/uuid library.
const ImportPath = "github.com/google/uuid"

// SQLTypes returns the SQL types this integration handles.
func SQLTypes() []string {
	return []string{"uuid"}
}

// Override returns the TypeOverride for UUID columns using google/uuid.
// Both uuid.UUID and uuid.NullUUID implement sql.Scanner/driver.Valuer, which
// is what PRD §4.7 requires of any overridden type.
func Override() config.TypeOverride {
	return config.TypeOverride{
		Type:      "uuid.UUID",
		Import:    ImportPath,
		ZeroValue: "uuid.UUID{}",
		Nullable: config.NullableVariant{
			Type:            "uuid.NullUUID",
			UnderlyingField: "UUID",
		},
	}
}

// The Go expressions that generate a new UUID with this integration (PRD §7.4
// "Generating UUID values").
//
// google is the only one of the three that exports New (a v4 generator
// returning a bare UUID) and the only one that exports NewString, so its v4
// forms are the only unwrapped ones in the table. NewV7 returns (UUID, error)
// and is wrapped in the package's own Must.
const (
	// ParseFunc is the string constructor the GraphQL scalar unmarshalers
	// call (PRD §7.4 "Parsing a UUID from a string"). google spells it Parse,
	// as the standard library does; gofrs is the odd one out.
	ParseFunc = "uuid.Parse"

	V4Expr       = "uuid.New()"
	V7Expr       = "uuid.Must(uuid.NewV7())"
	V4StringExpr = "uuid.NewString()"
	V7StringExpr = "uuid.Must(uuid.NewV7()).String()"
)
