package main_test

import "testing"

// TestJSONTypedAccessOverride_MySQLScannerWrapper retypes a MySQL JSON column
// through column_map to a slice type that implements sql.Scanner and
// driver.Valuer by decoding and encoding JSON. database/sql hands such a type
// the column's bytes and binds what its Value returns, so Create writes the
// typed values and Get reads them back.
func TestJSONTypedAccessOverride_MySQLScannerWrapper(t *testing.T) {
	gtwRunProbes(t, []gtwProbe{
		{
			pkg:     "mysql_json_wrapper",
			dialect: "mysql",
			config:  gtwOutput("stdlib") + gtwYearValuesRetype("mysql_json_wrapper"),
			schema:  "CREATE TABLE assets (id BIGINT PRIMARY KEY, property_tax_rate JSON NOT NULL);\n",
			files:   map[string]string{"yearvalues/year_values.go": gtwYearValues},
			test: `package probe

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	_ "github.com/go-sql-driver/mysql"
	dbstdlib "github.com/teandresmith/sqlgen/database/stdlib"

	"` + gtwProbeModule + `/jsontypes"
	"` + gtwProbeModule + `/mysql_json_wrapper/models"
	"` + gtwProbeModule + `/mysql_json_wrapper/yearvalues"
)

const driverName = "mysql"
` + gtwStdlibOpen + `
func TestJSONColumnRetypedToScannerWrapper(t *testing.T) {
	ctx := context.Background()
	client, _ := open(t)

	written := yearvalues.YearValues{{Year: 2024, Value: 1.25}, {Year: 2025, Value: 1.5}}
	if _, err := client.Assets().Create(ctx, &models.CreateAssetInput{ID: 1, PropertyTaxRate: written}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := client.Assets().Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get of the row Create wrote: %v", err)
	}
	want := []jsontypes.YearValue{{Year: 2024, Value: 1.25}, {Year: 2025, Value: 1.5}}
	if diff := cmp.Diff(want, []jsontypes.YearValue(got.PropertyTaxRate)); diff != "" {
		t.Errorf("Get of the row Create wrote: PropertyTaxRate mismatch (-want +got):\n%s", diff)
	}
}
`,
		},
	})
}

// gtwYearValues is a wrapper over []jsontypes.YearValue that crosses the
// database/sql boundary on its own: Scan decodes the column's JSON and Value
// encodes it.
const gtwYearValues = `package yearvalues

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"` + gtwProbeModule + `/jsontypes"
)

// YearValues is a JSON array of yearly values.
type YearValues []jsontypes.YearValue

// Scan decodes a JSON column value.
func (v *YearValues) Scan(src any) error {
	switch s := src.(type) {
	case nil:
		*v = nil
		return nil
	case []byte:
		return json.Unmarshal(s, v)
	case string:
		return json.Unmarshal([]byte(s), v)
	default:
		return fmt.Errorf("yearvalues: cannot scan %T", src)
	}
}

// Value encodes v as JSON text.
func (v YearValues) Value() (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
`

// gtwYearValuesRetype retypes assets.property_tax_rate to the YearValues
// wrapper in the probe package pkg.
func gtwYearValuesRetype(pkg string) string {
	return "tables:\n  assets:\n    column_map:\n      property_tax_rate:\n        type: yearvalues.YearValues\n        import: " + gtwProbeModule + "/" + pkg + "/yearvalues\n"
}
