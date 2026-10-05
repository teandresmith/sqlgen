// Package decimal provides a built-in type mapping for github.com/shopspring/decimal.
package decimal

import "github.com/teandresmith/sqlgen/cmd/sqlgen/config"

// ImportPath is the Go import path for the shopspring/decimal library.
const ImportPath = "github.com/shopspring/decimal"

// SQLTypes returns the SQL types this integration handles.
func SQLTypes() []string {
	return []string{"numeric", "decimal"}
}

// Override returns the TypeOverride for numeric/decimal columns using shopspring/decimal.
// Both decimal.Decimal and decimal.NullDecimal implement sql.Scanner/driver.Valuer,
// which is what PRD §4.7 requires of any overridden type.
func Override() config.TypeOverride {
	return config.TypeOverride{
		Type:      "decimal.Decimal",
		Import:    ImportPath,
		ZeroValue: "decimal.Decimal{}",
		Nullable: config.NullableVariant{
			Type:            "decimal.NullDecimal",
			UnderlyingField: "Decimal",
		},
	}
}
