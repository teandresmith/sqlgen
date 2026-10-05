package gotype_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

func TestResolutionChainPrecedence(t *testing.T) {
	t.Run("type_map overrides all lower levels", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "global.UUID", Import: "example.com/global"},
		})
		r.RegisterIntegration("uuid", config.TypeOverride{
			Type: "integ.UUID", Import: "example.com/integ",
		})

		got := r.Resolve(
			"uuid", false, "id",
			map[string]string{"id": "MyID"},
			map[string]config.TypeOverride{
				"uuid": {Type: "table.UUID", Import: "example.com/table"},
			},
		)
		if got.Name != "MyID" {
			t.Errorf("Resolve() Name = %q, want %q", got.Name, "MyID")
		}
	})

	t.Run("table overrides override global and integrations", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "global.UUID", Import: "example.com/global"},
		})
		r.RegisterIntegration("uuid", config.TypeOverride{
			Type: "integ.UUID", Import: "example.com/integ",
		})

		got := r.Resolve(
			"uuid", false, "id", nil,
			map[string]config.TypeOverride{
				"uuid": {Type: "table.UUID", Import: "example.com/table"},
			},
		)
		if got.Name != "table.UUID" || got.Import != "example.com/table" {
			t.Errorf("Resolve() = {Name: %q, Import: %q}, want {Name: %q, Import: %q}",
				got.Name, got.Import, "table.UUID", "example.com/table")
		}
	})

	t.Run("global overrides override integrations and dialect", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "global.UUID", Import: "example.com/global"},
		})
		r.RegisterIntegration("uuid", config.TypeOverride{
			Type: "integ.UUID", Import: "example.com/integ",
		})

		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.Name != "global.UUID" || got.Import != "example.com/global" {
			t.Errorf("Resolve() = {Name: %q, Import: %q}, want {Name: %q, Import: %q}",
				got.Name, got.Import, "global.UUID", "example.com/global")
		}
	})

	t.Run("integrations override dialect defaults", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)
		r.RegisterIntegration("uuid", config.TypeOverride{
			Type: "integ.UUID", Import: "example.com/integ",
		})

		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.Name != "integ.UUID" || got.Import != "example.com/integ" {
			t.Errorf("Resolve() = {Name: %q, Import: %q}, want {Name: %q, Import: %q}",
				got.Name, got.Import, "integ.UUID", "example.com/integ")
		}
	})

	t.Run("dialect default when no overrides", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)

		got := r.Resolve("uuid", false, "id", nil, nil)
		// uuid with no configuration is the standard library binding (PRD §7.4).
		if got.Name != "uuid.UUID" || got.Import != uuidstd.ImportPath {
			t.Errorf("Resolve() = {Name: %q, Import: %q}, want {Name: %q, Import: %q}",
				got.Name, got.Import, "uuid.UUID", uuidstd.ImportPath)
		}
	})
}

func TestPostgresTypeMappings(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)

	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		// --- String types ---
		{"text", "text", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"text nullable", "text", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"varchar", "varchar", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"varchar nullable", "varchar", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"char", "char", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"char nullable", "char", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"name", "name", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"name nullable", "name", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},

		// --- Integer types ---
		{"int2", "int2", false, gotype.GoType{Name: "int16", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int2 nullable", "int2", true, gotype.GoType{Name: "*int16", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"smallint", "smallint", false, gotype.GoType{Name: "int16", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"smallint nullable", "smallint", true, gotype.GoType{Name: "*int16", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int4", "int4", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int4 nullable", "int4", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"integer", "integer", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"integer nullable", "integer", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"serial", "serial", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"serial nullable", "serial", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int8", "int8", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int8 nullable", "int8", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bigint", "bigint", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"bigint nullable", "bigint", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bigserial", "bigserial", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"bigserial nullable", "bigserial", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Float types ---
		{"float4", "float4", false, gotype.GoType{Name: "float32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"float4 nullable", "float4", true, gotype.GoType{Name: "*float32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"real", "real", false, gotype.GoType{Name: "float32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"real nullable", "real", true, gotype.GoType{Name: "*float32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"float8", "float8", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"float8 nullable", "float8", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"double precision", "double precision", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"double precision nullable", "double precision", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"numeric", "numeric", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"numeric nullable", "numeric", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"decimal", "decimal", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"decimal nullable", "decimal", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Boolean ---
		{"bool", "bool", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"bool nullable", "bool", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"boolean", "boolean", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"boolean nullable", "boolean", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Binary ---
		{"bytea", "bytea", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bytea nullable", "bytea", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Date/Time ---
		{"timestamp", "timestamp", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"timestamp nullable", "timestamp", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"timestamptz", "timestamptz", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"timestamptz nullable", "timestamptz", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"date", "date", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"date nullable", "date", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"interval", "interval", false, gotype.GoType{Name: "time.Duration", Import: "time", ZeroValue: "0", FKConvert: gotype.FKStringStringer}},
		{"interval nullable", "interval", true, gotype.GoType{Name: "*time.Duration", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"time", "time", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"time nullable", "time", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},

		// --- UUID (default, no integration) ---
		{"uuid", "uuid", false, gotype.GoType{Name: "uuid.UUID", Import: "uuid", ZeroValue: "uuid.UUID{}", FKConvert: gotype.FKStringStringer}},
		{"uuid nullable", "uuid", true, gotype.GoType{Name: "*uuid.UUID", Import: "uuid", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},

		// --- JSON ---
		{"json", "json", false, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"json nullable", "json", true, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"jsonb", "jsonb", false, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"jsonb nullable", "jsonb", true, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Network ---
		{"inet", "inet", false, gotype.GoType{Name: "net.IP", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"inet nullable", "inet", true, gotype.GoType{Name: "net.IP", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"cidr", "cidr", false, gotype.GoType{Name: "net.IPNet", Import: "net", ZeroValue: "net.IPNet{}", FKConvert: gotype.FKStringStringer}},
		{"cidr nullable", "cidr", true, gotype.GoType{Name: "*net.IPNet", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"macaddr", "macaddr", false, gotype.GoType{Name: "net.HardwareAddr", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"macaddr nullable", "macaddr", true, gotype.GoType{Name: "net.HardwareAddr", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, tt.nullable, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=%v) mismatch (-want +got):\n%s", tt.sqlType, tt.nullable, diff)
			}
		})
	}
}

func TestMySQLTypeMappings(t *testing.T) {
	r := gotype.NewResolver("mysql", true, nil)

	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		// --- Integer types ---
		{"tinyint", "tinyint", false, gotype.GoType{Name: "int8", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"tinyint nullable", "tinyint", true, gotype.GoType{Name: "*int8", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"smallint", "smallint", false, gotype.GoType{Name: "int16", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"smallint nullable", "smallint", true, gotype.GoType{Name: "*int16", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int", "int", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int nullable", "int", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"integer", "integer", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"integer nullable", "integer", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"mediumint", "mediumint", false, gotype.GoType{Name: "int32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"mediumint nullable", "mediumint", true, gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bigint", "bigint", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"bigint nullable", "bigint", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int unsigned", "int unsigned", false, gotype.GoType{Name: "uint32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int unsigned nullable", "int unsigned", true, gotype.GoType{Name: "*uint32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bigint unsigned", "bigint unsigned", false, gotype.GoType{Name: "uint64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"bigint unsigned nullable", "bigint unsigned", true, gotype.GoType{Name: "*uint64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Float types ---
		{"float", "float", false, gotype.GoType{Name: "float32", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"float nullable", "float", true, gotype.GoType{Name: "*float32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"double", "double", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"double nullable", "double", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"decimal", "decimal", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"decimal nullable", "decimal", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- String types ---
		{"varchar", "varchar", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"varchar nullable", "varchar", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"text", "text", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"text nullable", "text", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"char", "char", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"char nullable", "char", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"enum", "enum", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"enum nullable", "enum", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"tinytext", "tinytext", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"tinytext nullable", "tinytext", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"mediumtext", "mediumtext", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"mediumtext nullable", "mediumtext", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"longtext", "longtext", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"longtext nullable", "longtext", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"set", "set", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"set nullable", "set", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"time", "time", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"time nullable", "time", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},

		// --- Date/Time ---
		{"datetime", "datetime", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"datetime nullable", "datetime", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"timestamp", "timestamp", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"timestamp nullable", "timestamp", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"date", "date", false, gotype.GoType{Name: "time.Time", Import: "time", ZeroValue: "time.Time{}", FKConvert: gotype.FKStringStringer}},
		{"date nullable", "date", true, gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},

		// --- JSON ---
		{"json", "json", false, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"json nullable", "json", true, gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Binary types ---
		{"blob", "blob", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"blob nullable", "blob", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"binary", "binary", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"binary nullable", "binary", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"varbinary", "varbinary", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"varbinary nullable", "varbinary", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"tinyblob", "tinyblob", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"tinyblob nullable", "tinyblob", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"mediumblob", "mediumblob", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"mediumblob nullable", "mediumblob", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"longblob", "longblob", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"longblob nullable", "longblob", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Boolean ---
		{"bool", "bool", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"bool nullable", "bool", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"boolean", "boolean", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"boolean nullable", "boolean", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bit(1)", "bit(1)", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"bit(1) nullable", "bit(1)", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bit(8)", "bit(8)", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bit(8) nullable", "bit(8)", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Year ---
		{"year", "year", false, gotype.GoType{Name: "int16", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"year nullable", "year", true, gotype.GoType{Name: "*int16", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, tt.nullable, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=%v) mismatch (-want +got):\n%s", tt.sqlType, tt.nullable, diff)
			}
		})
	}
}

func TestSQLiteTypeMappings(t *testing.T) {
	r := gotype.NewResolver("sqlite", true, nil)

	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		// --- Integer types (all map to int64) ---
		{"integer", "integer", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"integer nullable", "integer", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int", "int", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"int nullable", "int", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"tinyint", "tinyint", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"tinyint nullable", "tinyint", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"smallint", "smallint", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"smallint nullable", "smallint", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bigint", "bigint", false, gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"bigint nullable", "bigint", true, gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- String types ---
		{"text", "text", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"text nullable", "text", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"varchar", "varchar", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"varchar nullable", "varchar", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"char", "char", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"char nullable", "char", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"clob", "clob", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"clob nullable", "clob", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},

		// --- Date/Time — types.DateTime integration for datetime/timestamp/date ---
		{"datetime", "datetime", false, gotype.GoType{Name: "types.DateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.DateTime{}", FKConvert: gotype.FKStringStringer}},
		{"datetime nullable", "datetime", true, gotype.GoType{Name: "types.NullDateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.NullDateTime{}", FKConvert: gotype.FKStringStringer}},
		{"timestamp", "timestamp", false, gotype.GoType{Name: "types.DateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.DateTime{}", FKConvert: gotype.FKStringStringer}},
		{"timestamp nullable", "timestamp", true, gotype.GoType{Name: "types.NullDateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.NullDateTime{}", FKConvert: gotype.FKStringStringer}},
		{"timestamptz", "timestamptz", false, gotype.GoType{Name: "types.DateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.DateTime{}", FKConvert: gotype.FKStringStringer}},
		{"timestamptz nullable", "timestamptz", true, gotype.GoType{Name: "types.NullDateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.NullDateTime{}", FKConvert: gotype.FKStringStringer}},
		{"date", "date", false, gotype.GoType{Name: "types.DateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.DateTime{}", FKConvert: gotype.FKStringStringer}},
		{"date nullable", "date", true, gotype.GoType{Name: "types.NullDateTime", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "types.NullDateTime{}", FKConvert: gotype.FKStringStringer}},
		{"time", "time", false, gotype.GoType{Name: "string", ZeroValue: `""`}},
		{"time nullable", "time", true, gotype.GoType{Name: "*string", ZeroValue: "nil"}},

		// --- Float types (all map to float64) ---
		{"real", "real", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"real nullable", "real", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"float", "float", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"float nullable", "float", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"double", "double", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"double nullable", "double", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"numeric", "numeric", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"numeric nullable", "numeric", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"decimal", "decimal", false, gotype.GoType{Name: "float64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}},
		{"decimal nullable", "decimal", true, gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Binary ---
		{"blob", "blob", false, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"blob nullable", "blob", true, gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},

		// --- Boolean ---
		{"boolean", "boolean", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"boolean nullable", "boolean", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bool", "bool", false, gotype.GoType{Name: "bool", ZeroValue: "false", FKConvert: gotype.FKStringSprint}},
		{"bool nullable", "bool", true, gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, tt.nullable, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=%v) mismatch (-want +got):\n%s", tt.sqlType, tt.nullable, diff)
			}
		})
	}
}

func TestNullablePointerTypes(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil) // usePointers=true

	tests := []struct {
		name    string
		sqlType string
		want    gotype.GoType
	}{
		{"string", "text", gotype.GoType{Name: "*string", ZeroValue: "nil"}},
		{"int16", "int2", gotype.GoType{Name: "*int16", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int32", "int4", gotype.GoType{Name: "*int32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"int64", "int8", gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"float32", "float4", gotype.GoType{Name: "*float32", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"float64", "float8", gotype.GoType{Name: "*float64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"bool", "bool", gotype.GoType{Name: "*bool", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"time", "timestamp", gotype.GoType{Name: "*time.Time", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		// Reference types stay the same
		{"bytes", "bytea", gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"json", "json", gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		// Ptr-only types always use pointer
		{"duration", "interval", gotype.GoType{Name: "*time.Duration", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"ipnet", "cidr", gotype.GoType{Name: "*net.IPNet", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, true, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=true, usePointers=true) mismatch (-want +got):\n%s", tt.sqlType, diff)
			}
		})
	}
}

func TestNullableSQLNullTypes(t *testing.T) {
	r := gotype.NewResolver("postgres", false, nil) // usePointers=false

	tests := []struct {
		name    string
		sqlType string
		want    gotype.GoType
	}{
		{"string", "text", gotype.GoType{Name: "sql.NullString", Import: "database/sql", ZeroValue: "sql.NullString{}"}},
		{"int16", "int2", gotype.GoType{Name: "sql.NullInt16", Import: "database/sql", ZeroValue: "sql.NullInt16{}", FKConvert: gotype.FKStringSprint}},
		{"int32", "int4", gotype.GoType{Name: "sql.NullInt32", Import: "database/sql", ZeroValue: "sql.NullInt32{}", FKConvert: gotype.FKStringSprint}},
		{"int64", "int8", gotype.GoType{Name: "sql.NullInt64", Import: "database/sql", ZeroValue: "sql.NullInt64{}", FKConvert: gotype.FKStringSprint}},
		{"float32 uses NullFloat64", "float4", gotype.GoType{Name: "sql.NullFloat64", Import: "database/sql", ZeroValue: "sql.NullFloat64{}", FKConvert: gotype.FKStringSprint}},
		{"float64", "float8", gotype.GoType{Name: "sql.NullFloat64", Import: "database/sql", ZeroValue: "sql.NullFloat64{}", FKConvert: gotype.FKStringSprint}},
		{"bool", "bool", gotype.GoType{Name: "sql.NullBool", Import: "database/sql", ZeroValue: "sql.NullBool{}", FKConvert: gotype.FKStringSprint}},
		{"time", "timestamp", gotype.GoType{Name: "sql.NullTime", Import: "database/sql", ZeroValue: "sql.NullTime{}", FKConvert: gotype.FKStringStringer}},
		// Reference types: same type regardless of usePointers
		{"bytes", "bytea", gotype.GoType{Name: "[]byte", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"json", "json", gotype.GoType{Name: "types.JSON", Import: "github.com/teandresmith/sqlgen/types", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}},
		{"inet", "inet", gotype.GoType{Name: "net.IP", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"macaddr", "macaddr", gotype.GoType{Name: "net.HardwareAddr", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		// Ptr-only types: still use *T even with usePointers=false
		{"duration ptr-only", "interval", gotype.GoType{Name: "*time.Duration", Import: "time", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
		{"ipnet ptr-only", "cidr", gotype.GoType{Name: "*net.IPNet", Import: "net", ZeroValue: "nil", FKConvert: gotype.FKStringStringer}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, true, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=true, usePointers=false) mismatch (-want +got):\n%s", tt.sqlType, diff)
			}
		})
	}
}

func TestPostgresArrayTypes(t *testing.T) {
	t.Run("built-in array types", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)

		tests := []struct {
			name     string
			sqlType  string
			nullable bool
			want     gotype.GoType
		}{
			{"text[]", "text[]", false, gotype.GoType{Name: "[]string", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"text[] nullable", "text[]", true, gotype.GoType{Name: "[]string", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"varchar[]", "varchar[]", false, gotype.GoType{Name: "[]string", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"int2[]", "int2[]", false, gotype.GoType{Name: "[]int16", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"smallint[]", "smallint[]", false, gotype.GoType{Name: "[]int16", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"int4[]", "int4[]", false, gotype.GoType{Name: "[]int32", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"integer[]", "integer[]", false, gotype.GoType{Name: "[]int32", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"int8[]", "int8[]", false, gotype.GoType{Name: "[]int64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"bigint[]", "bigint[]", false, gotype.GoType{Name: "[]int64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"float4[]", "float4[]", false, gotype.GoType{Name: "[]float32", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"real[]", "real[]", false, gotype.GoType{Name: "[]float32", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"float8[]", "float8[]", false, gotype.GoType{Name: "[]float64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"double precision[]", "double precision[]", false, gotype.GoType{Name: "[]float64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"bool[]", "bool[]", false, gotype.GoType{Name: "[]bool", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"boolean[]", "boolean[]", false, gotype.GoType{Name: "[]bool", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"uuid[]", "uuid[]", false, gotype.GoType{Name: "[]uuid.UUID", Import: "uuid", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"numeric[]", "numeric[]", false, gotype.GoType{Name: "[]float64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"decimal[]", "decimal[]", false, gotype.GoType{Name: "[]float64", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"timestamp[]", "timestamp[]", false, gotype.GoType{Name: "[]time.Time", Import: "time", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
			{"timestamptz[]", "timestamptz[]", false, gotype.GoType{Name: "[]time.Time", Import: "time", ZeroValue: "nil", IsSlice: true, FKConvert: gotype.FKStringSprint}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := r.Resolve(tt.sqlType, tt.nullable, "", nil, nil)
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("Resolve(%q, nullable=%v) mismatch (-want +got):\n%s", tt.sqlType, tt.nullable, diff)
				}
			})
		}
	})

	t.Run("array with uuid override", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {
				Type:   "uuid.UUID",
				Import: "github.com/google/uuid",
			},
		})

		got := r.Resolve("uuid[]", false, "", nil, nil)
		want := gotype.GoType{
			Name:      "[]uuid.UUID",
			Import:    "github.com/google/uuid",
			ZeroValue: "nil",
			IsSlice:   true,
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Resolve(uuid[] with uuid override) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("array with table-level override on base type", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)

		tableOverrides := map[string]config.TypeOverride{
			"uuid": {
				Type:   "customuuid.ID",
				Import: "example.com/customuuid",
			},
		}

		got := r.Resolve("uuid[]", false, "", nil, tableOverrides)
		want := gotype.GoType{
			Name:      "[]customuuid.ID",
			Import:    "example.com/customuuid",
			ZeroValue: "nil",
			IsSlice:   true,
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Resolve(uuid[] with table override) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("nullable array same as non-nullable", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)

		nonNull := r.Resolve("int4[]", false, "", nil, nil)
		null := r.Resolve("int4[]", true, "", nil, nil)

		if diff := cmp.Diff(nonNull, null); diff != "" {
			t.Errorf("nullable array should equal non-nullable array (-nonNull +null):\n%s", diff)
		}
	})
}

func TestFKStringConversion(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		sqlType string
		want    gotype.FKStringMethod
	}{
		// String types need no conversion
		{"string type", "postgres", "text", gotype.FKStringNone},
		{"varchar type", "mysql", "varchar", gotype.FKStringNone},

		// Types with .String() method
		{"uuid default", "postgres", "uuid", gotype.FKStringStringer},
		{"time.Time", "postgres", "timestamp", gotype.FKStringStringer},
		{"time.Duration", "postgres", "interval", gotype.FKStringStringer},
		{"net.IP", "postgres", "inet", gotype.FKStringStringer},
		{"net.IPNet", "postgres", "cidr", gotype.FKStringStringer},
		{"net.HardwareAddr", "postgres", "macaddr", gotype.FKStringStringer},

		// Types that need fmt.Sprint()
		{"int16", "postgres", "int2", gotype.FKStringSprint},
		{"int32", "postgres", "int4", gotype.FKStringSprint},
		{"int64", "postgres", "int8", gotype.FKStringSprint},
		{"float64", "postgres", "float8", gotype.FKStringSprint},
		{"bool", "postgres", "bool", gotype.FKStringSprint},
		{"types.JSON", "postgres", "json", gotype.FKStringSprint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver(tt.dialect, true, nil)
			got := r.Resolve(tt.sqlType, false, "", nil, nil)
			if got.FKConvert != tt.want {
				t.Errorf("Resolve(%q).FKConvert = %v, want %v", tt.sqlType, got.FKConvert, tt.want)
			}
		})
	}

	t.Run("override with .String() type", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
		})
		got := r.Resolve("uuid", false, "", nil, nil)
		if got.FKConvert != gotype.FKStringStringer {
			t.Errorf("uuid.UUID FKConvert = %v, want FKStringStringer", got.FKConvert)
		}
	})
}

// TestDeriveScalarExtraction covers the path that decides how to
// safely stringify a (possibly-null) FK value. The receiver is nil here so
// only the static known-wrappers + pointer/bare fallthroughs apply; the
// resolver-aware variant is exercised separately below.
func TestDeriveScalarExtraction(t *testing.T) {
	var r *gotype.Resolver // nil — covers the static-only path
	tests := []struct {
		name         string
		goType       string
		wantGuard    string
		wantUnwrap   string
		wantStringFK gotype.FKStringMethod
	}{
		// Bare types — no guard, pass-through unwrap.
		{name: "bare string", goType: "string", wantGuard: "", wantUnwrap: "$v", wantStringFK: gotype.FKStringNone},
		{name: "bare int64", goType: "int64", wantGuard: "", wantUnwrap: "$v", wantStringFK: gotype.FKStringSprint},
		{name: "bare uuid.UUID", goType: "uuid.UUID", wantGuard: "", wantUnwrap: "$v", wantStringFK: gotype.FKStringStringer},
		{name: "bare time.Time", goType: "time.Time", wantGuard: "", wantUnwrap: "$v", wantStringFK: gotype.FKStringStringer},

		// Pointer types — non-nil guard, paren-wrapped deref unwrap so that
		// `.String()` binds correctly: `(*$v).String()` not `*$v.String()`.
		{name: "*string", goType: "*string", wantGuard: "$v != nil", wantUnwrap: "(*$v)", wantStringFK: gotype.FKStringNone},
		{name: "*int64", goType: "*int64", wantGuard: "$v != nil", wantUnwrap: "(*$v)", wantStringFK: gotype.FKStringSprint},
		{name: "*uuid.UUID", goType: "*uuid.UUID", wantGuard: "$v != nil", wantUnwrap: "(*$v)", wantStringFK: gotype.FKStringStringer},

		// database/sql.NullX wrappers — Valid guard, named-field unwrap.
		{name: "sql.NullString", goType: "sql.NullString", wantGuard: "$v.Valid", wantUnwrap: "$v.String", wantStringFK: gotype.FKStringNone},
		{name: "sql.NullInt64", goType: "sql.NullInt64", wantGuard: "$v.Valid", wantUnwrap: "$v.Int64", wantStringFK: gotype.FKStringSprint},
		{name: "sql.NullTime", goType: "sql.NullTime", wantGuard: "$v.Valid", wantUnwrap: "$v.Time", wantStringFK: gotype.FKStringStringer},

		// Built-in integration wrappers — Valid guard, integration-specific underlying.
		{name: "uuid.NullUUID", goType: "uuid.NullUUID", wantGuard: "$v.Valid", wantUnwrap: "$v.UUID", wantStringFK: gotype.FKStringStringer},
		{name: "decimal.NullDecimal", goType: "decimal.NullDecimal", wantGuard: "$v.Valid", wantUnwrap: "$v.Decimal", wantStringFK: gotype.FKStringStringer},
		// types.NullDateTime carries the underlying time on a Time field
		// (matching sql.NullTime's shape), not DateTime — pre-fix the
		// extraction emitted a non-existent .DateTime field reference. The
		// runtime struct has Time + Valid only.
		{name: "types.NullDateTime", goType: "types.NullDateTime", wantGuard: "$v.Valid", wantUnwrap: "$v.Time", wantStringFK: gotype.FKStringStringer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.DeriveScalarExtraction(tt.goType)
			if got.GuardExpr != tt.wantGuard {
				t.Errorf("GuardExpr = %q, want %q", got.GuardExpr, tt.wantGuard)
			}
			if got.UnwrapExpr != tt.wantUnwrap {
				t.Errorf("UnwrapExpr = %q, want %q", got.UnwrapExpr, tt.wantUnwrap)
			}
			if got.StringMethod != tt.wantStringFK {
				t.Errorf("StringMethod = %v, want %v", got.StringMethod, tt.wantStringFK)
			}
		})
	}
}

// TestDeriveScalarExtraction_CustomValidField verifies that user-declared
// wrappers in sqlgen.yml with a non-default valid_field name produce the
// matching guard expression.
func TestDeriveScalarExtraction_CustomValidField(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"text": {
			Type:   "myapp.OptionalString",
			Import: "myapp/pkg/optional",
			Nullable: config.NullableVariant{
				Type:            "myapp.NullableString",
				Import:          "myapp/pkg/optional",
				UnderlyingField: "Value",
				ValidField:      "Set",
			},
		},
	})

	got := r.DeriveScalarExtraction("myapp.NullableString")
	if got.GuardExpr != "$v.Set" {
		t.Errorf("GuardExpr = %q, want %q", got.GuardExpr, "$v.Set")
	}
	if got.UnwrapExpr != "$v.Value" {
		t.Errorf("UnwrapExpr = %q, want %q", got.UnwrapExpr, "$v.Value")
	}
	// Underlying type "myapp.OptionalString" → fmt.Sprint (no .String()).
	if got.StringMethod != gotype.FKStringSprint {
		t.Errorf("StringMethod = %v, want FKStringSprint", got.StringMethod)
	}
}

// TestDeriveScalarExtraction_CustomValidMethod verifies user-declared
// wrappers with a method-based validity check.
func TestDeriveScalarExtraction_CustomValidMethod(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"numeric": {
			Type:   "ids.Money",
			Import: "myapp/pkg/ids",
			Nullable: config.NullableVariant{
				Type:            "ids.NullMoney",
				Import:          "myapp/pkg/ids",
				UnderlyingField: "Money",
				ValidMethod:     "IsPresent",
			},
		},
	})

	got := r.DeriveScalarExtraction("ids.NullMoney")
	if got.GuardExpr != "$v.IsPresent()" {
		t.Errorf("GuardExpr = %q, want %q", got.GuardExpr, "$v.IsPresent()")
	}
	if got.UnwrapExpr != "$v.Money" {
		t.Errorf("UnwrapExpr = %q, want %q", got.UnwrapExpr, "$v.Money")
	}
}

// TestDeriveScalarExtraction_InvertedValidMethod verifies inverted-method
// wrappers. time.Time-style: IsZero() returns
// true when invalid, so the guard negates the call.
func TestDeriveScalarExtraction_InvertedValidMethod(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"timestamptz": {
			Type:   "myapp.Stamp",
			Import: "myapp/pkg/stamp",
			Nullable: config.NullableVariant{
				Type:            "myapp.NullableStamp",
				Import:          "myapp/pkg/stamp",
				UnderlyingField: "Stamp",
				ValidMethod:     "IsZero",
				ValidInvert:     true,
			},
		},
	})

	got := r.DeriveScalarExtraction("myapp.NullableStamp")
	if got.GuardExpr != "!$v.IsZero()" {
		t.Errorf("GuardExpr = %q, want %q", got.GuardExpr, "!$v.IsZero()")
	}
	if got.UnwrapExpr != "$v.Stamp" {
		t.Errorf("UnwrapExpr = %q, want %q", got.UnwrapExpr, "$v.Stamp")
	}
}

// TestDeriveScalarExtraction_BuiltinWinsOverUser pins the precedence rule:
// even if a user declares a wrapper named identical to a built-in (e.g.
// uuid.NullUUID), the static known-wrapper extraction wins so the shape
// stays stable across configurations.
func TestDeriveScalarExtraction_BuiltinWinsOverUser(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"uuid": {
			Type:   "uuid.UUID",
			Import: "github.com/google/uuid",
			Nullable: config.NullableVariant{
				Type:            "uuid.NullUUID",
				UnderlyingField: "Bytes", // would-be-wrong override
				ValidField:      "OK",    // would-be-wrong override
			},
		},
	})

	got := r.DeriveScalarExtraction("uuid.NullUUID")
	if got.GuardExpr != "$v.Valid" {
		t.Errorf("GuardExpr = %q, want %q (built-in must win)", got.GuardExpr, "$v.Valid")
	}
	if got.UnwrapExpr != "$v.UUID" {
		t.Errorf("UnwrapExpr = %q, want %q (built-in must win)", got.UnwrapExpr, "$v.UUID")
	}
}

func TestNullableOverrideVariants(t *testing.T) {
	t.Run("explicit nullable type", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {
				Type:     "uuid.UUID",
				Import:   "github.com/google/uuid",
				Nullable: config.NullableVariant{Type: "uuid.NullUUID"},
			},
		})

		got := r.Resolve("uuid", true, "", nil, nil)
		want := gotype.GoType{
			Name:      "uuid.NullUUID",
			Import:    "github.com/google/uuid",
			ZeroValue: "uuid.NullUUID{}",
			FKConvert: gotype.FKStringStringer,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Resolve(uuid, nullable) with NullUUID (-want +got):\n%s", diff)
		}
	})

	t.Run("nullable with separate import", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"custom": {
				Type:   "Custom",
				Import: "example.com/custom",
				Nullable: config.NullableVariant{
					Type:   "nullcustom.NullCustom",
					Import: "example.com/nullcustom",
				},
			},
		})

		got := r.Resolve("custom", true, "", nil, nil)
		if got.Name != "nullcustom.NullCustom" {
			t.Errorf("Name = %q, want %q", got.Name, "nullcustom.NullCustom")
		}
		if got.Import != "example.com/nullcustom" {
			t.Errorf("Import = %q, want %q", got.Import, "example.com/nullcustom")
		}
	})

	t.Run("no nullable variant falls back to pointer", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"custom": {
				Type:   "Custom",
				Import: "example.com/custom",
			},
		})

		got := r.Resolve("custom", true, "", nil, nil)
		if got.Name != "*Custom" {
			t.Errorf("Name = %q, want %q", got.Name, "*Custom")
		}
		if got.ZeroValue != "nil" {
			t.Errorf("ZeroValue = %q, want %q", got.ZeroValue, "nil")
		}
	})

	t.Run("nullable pointer variant has nil zero value", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"custom": {
				Type:     "Custom",
				Import:   "example.com/custom",
				Nullable: config.NullableVariant{Type: "*Custom"},
			},
		})

		got := r.Resolve("custom", true, "", nil, nil)
		if got.ZeroValue != "nil" {
			t.Errorf("ZeroValue = %q, want %q", got.ZeroValue, "nil")
		}
	})
}

func TestCaseNormalization(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)

	// SQL types should be case-insensitive
	got := r.Resolve("TEXT", false, "", nil, nil)
	if got.Name != "string" {
		t.Errorf("Resolve(TEXT) Name = %q, want %q", got.Name, "string")
	}

	got = r.Resolve("Integer", false, "", nil, nil)
	if got.Name != "int32" {
		t.Errorf("Resolve(Integer) Name = %q, want %q", got.Name, "int32")
	}
}

func TestTypeMapLiteral(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)

	t.Run("known go type", func(t *testing.T) {
		got := r.Resolve("text", false, "name", map[string]string{"name": "int64"}, nil)
		want := gotype.GoType{Name: "int64", ZeroValue: "0", FKConvert: gotype.FKStringSprint}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("type_map int64 (-want +got):\n%s", diff)
		}
	})

	t.Run("known go type nullable", func(t *testing.T) {
		got := r.Resolve("text", true, "name", map[string]string{"name": "int64"}, nil)
		want := gotype.GoType{Name: "*int64", ZeroValue: "nil", FKConvert: gotype.FKStringSprint}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("type_map int64 nullable (-want +got):\n%s", diff)
		}
	})

	t.Run("custom type", func(t *testing.T) {
		got := r.Resolve("text", false, "col", map[string]string{"col": "MyType"}, nil)
		if got.Name != "MyType" {
			t.Errorf("Name = %q, want %q", got.Name, "MyType")
		}
		if got.ZeroValue != "MyType{}" {
			t.Errorf("ZeroValue = %q, want %q", got.ZeroValue, "MyType{}")
		}
	})

	t.Run("slice type not double-wrapped for nullable", func(t *testing.T) {
		got := r.Resolve("text", true, "col", map[string]string{"col": "[]string"}, nil)
		if got.Name != "[]string" {
			t.Errorf("Name = %q, want %q (should not be *[]string)", got.Name, "[]string")
		}
		if got.ZeroValue != "nil" {
			t.Errorf("ZeroValue = %q, want %q", got.ZeroValue, "nil")
		}
	})
}

// TestTypeMapLiteralImports pins the import a `type_map` literal resolves
// to. `type_map` has no `import` field, so a literal outside gotype's known
// registry resolves with an empty import and the generated file references a
// package nothing imports. That asymmetry is why §8.5 scopes `type_map` to
// builtins and same-package types and routes external types through
// `column_map.<col>.type` + `.import`. Asserted here rather than
// left incidental — the tree happens to contain no external `type_map` entry,
// so nothing else would catch a regression.
func TestTypeMapLiteralImports(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)

	tests := []struct {
		name       string
		literal    string
		wantImport string
		wantKnown  bool
	}{
		{"external decimal carries no import", "decimal.Decimal", "", false},
		{"external uuid carries no import", "uuid.UUID", "", false},
		{"external stdlib type outside the registry", "netip.Addr", "", false},
		{"registry member supplies its own import", "json.RawMessage", "encoding/json", true},
		{"registry member supplies its own import (time)", "time.Time", "time", true},
		{"same-package composite needs none", "Address", "", false},
		{"builtin needs none", "int32", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve("numeric", false, "col", map[string]string{"col": tt.literal}, nil)
			if got.Name != tt.literal {
				t.Errorf("Resolve(type_map col=%q) Name = %q, want %q", tt.literal, got.Name, tt.literal)
			}
			if got.Import != tt.wantImport {
				t.Errorf("Resolve(type_map col=%q) Import = %q, want %q", tt.literal, got.Import, tt.wantImport)
			}
			imp, known := gotype.KnownLiteral(tt.literal)
			if known != tt.wantKnown {
				t.Errorf("KnownLiteral(%q) known = %v, want %v", tt.literal, known, tt.wantKnown)
			}
			if imp != tt.wantImport {
				t.Errorf("KnownLiteral(%q) import = %q, want %q", tt.literal, imp, tt.wantImport)
			}
		})
	}
}

func TestMySQLSQLNullTypes(t *testing.T) {
	r := gotype.NewResolver("mysql", false, nil) // usePointers=false

	tests := []struct {
		name    string
		sqlType string
		want    gotype.GoType
	}{
		{"tinyint uses NullInt16", "tinyint", gotype.GoType{Name: "sql.NullInt16", Import: "database/sql", ZeroValue: "sql.NullInt16{}", FKConvert: gotype.FKStringSprint}},
		{"int unsigned uses NullInt64", "int unsigned", gotype.GoType{Name: "sql.NullInt64", Import: "database/sql", ZeroValue: "sql.NullInt64{}", FKConvert: gotype.FKStringSprint}},
		{"bigint unsigned uses NullInt64", "bigint unsigned", gotype.GoType{Name: "sql.NullInt64", Import: "database/sql", ZeroValue: "sql.NullInt64{}", FKConvert: gotype.FKStringSprint}},
		{"float uses NullFloat64", "float", gotype.GoType{Name: "sql.NullFloat64", Import: "database/sql", ZeroValue: "sql.NullFloat64{}", FKConvert: gotype.FKStringSprint}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.sqlType, true, "", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve(%q, nullable=true, usePointers=false) (-want +got):\n%s", tt.sqlType, diff)
			}
		})
	}
}

// --- Built-In Type Integration Tests ---

func TestGoogleUUIDIntegration(t *testing.T) {
	t.Run("non-nullable", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		want := gotype.GoType{
			Name:      "uuid.UUID",
			Import:    uuidgoogle.ImportPath,
			ZeroValue: "uuid.UUID{}",
			FKConvert: gotype.FKStringStringer,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("google/uuid non-nullable (-want +got):\n%s", diff)
		}
	})

	t.Run("nullable always uses NullUUID regardless of usePointers", func(t *testing.T) {
		for _, usePointers := range []bool{true, false} {
			r := gotype.NewResolver("postgres", usePointers, map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
			})
			got := r.Resolve("uuid", true, "id", nil, nil)
			want := gotype.GoType{
				Name:      "uuid.NullUUID",
				Import:    uuidgoogle.ImportPath,
				ZeroValue: "uuid.NullUUID{}",
				FKConvert: gotype.FKStringStringer,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("google/uuid nullable (usePointers=%v) (-want +got):\n%s", usePointers, diff)
			}
		}
	})

	t.Run("zero value is uuid.UUID{}", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.ZeroValue != "uuid.UUID{}" {
			t.Errorf("google/uuid ZeroValue = %q, want %q", got.ZeroValue, "uuid.UUID{}")
		}
	})
}

func TestGofrsUUIDIntegration(t *testing.T) {
	t.Run("non-nullable", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		want := gotype.GoType{
			Name:      "uuid.UUID",
			Import:    uuidgofrs.ImportPath,
			ZeroValue: "uuid.Nil",
			FKConvert: gotype.FKStringStringer,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("gofrs/uuid non-nullable (-want +got):\n%s", diff)
		}
	})

	t.Run("nullable always uses NullUUID regardless of usePointers", func(t *testing.T) {
		for _, usePointers := range []bool{true, false} {
			r := gotype.NewResolver("postgres", usePointers, map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
			})
			got := r.Resolve("uuid", true, "id", nil, nil)
			want := gotype.GoType{
				Name:      "uuid.NullUUID",
				Import:    uuidgofrs.ImportPath,
				ZeroValue: "uuid.NullUUID{}",
				FKConvert: gotype.FKStringStringer,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("gofrs/uuid nullable (usePointers=%v) (-want +got):\n%s", usePointers, diff)
			}
		}
	})

	t.Run("zero value is uuid.Nil", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.ZeroValue != "uuid.Nil" {
			t.Errorf("gofrs/uuid ZeroValue = %q, want %q", got.ZeroValue, "uuid.Nil")
		}
	})
}

func TestShopspringDecimalIntegration(t *testing.T) {
	t.Run("numeric non-nullable", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
		})
		got := r.Resolve("numeric", false, "price", nil, nil)
		want := gotype.GoType{
			Name:      "decimal.Decimal",
			Import:    decimal.ImportPath,
			ZeroValue: "decimal.Decimal{}",
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("decimal numeric non-nullable (-want +got):\n%s", diff)
		}
	})

	t.Run("decimal SQL type auto-detected from numeric override", func(t *testing.T) {
		// User configures only "numeric" but the integration also covers "decimal".
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
		})
		got := r.Resolve("decimal", false, "amount", nil, nil)
		want := gotype.GoType{
			Name:      "decimal.Decimal",
			Import:    decimal.ImportPath,
			ZeroValue: "decimal.Decimal{}",
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("decimal auto-detected from numeric override (-want +got):\n%s", diff)
		}
	})

	t.Run("nullable always uses NullDecimal regardless of usePointers", func(t *testing.T) {
		for _, usePointers := range []bool{true, false} {
			r := gotype.NewResolver("postgres", usePointers, map[string]config.TypeOverride{
				"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
			})
			got := r.Resolve("numeric", true, "price", nil, nil)
			want := gotype.GoType{
				Name:      "decimal.NullDecimal",
				Import:    decimal.ImportPath,
				ZeroValue: "decimal.NullDecimal{}",
				FKConvert: gotype.FKStringSprint,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("decimal nullable (usePointers=%v) (-want +got):\n%s", usePointers, diff)
			}
		}
	})

	t.Run("zero value is decimal.Decimal{}", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
		})
		got := r.Resolve("numeric", false, "price", nil, nil)
		if got.ZeroValue != "decimal.Decimal{}" {
			t.Errorf("decimal ZeroValue = %q, want %q", got.ZeroValue, "decimal.Decimal{}")
		}
	})
}

func TestIntegrationExplicitOverridePrecedence(t *testing.T) {
	t.Run("explicit nullable overrides integration default", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {
				Type:     "uuid.UUID",
				Import:   uuidgoogle.ImportPath,
				Nullable: config.NullableVariant{Type: "*uuid.UUID"},
			},
		})
		got := r.Resolve("uuid", true, "id", nil, nil)
		if got.Name != "*uuid.UUID" {
			t.Errorf("explicit nullable override: Name = %q, want %q", got.Name, "*uuid.UUID")
		}
		if got.ZeroValue != "nil" {
			t.Errorf("explicit nullable override: ZeroValue = %q, want %q", got.ZeroValue, "nil")
		}
	})

	t.Run("explicit zero_value overrides integration default", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {
				Type:      "uuid.UUID",
				Import:    uuidgofrs.ImportPath,
				ZeroValue: "customZero",
			},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.ZeroValue != "customZero" {
			t.Errorf("explicit zero_value: ZeroValue = %q, want %q", got.ZeroValue, "customZero")
		}
	})

	t.Run("table override takes precedence over integration", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, map[string]config.TypeOverride{
			"uuid": {Type: "custom.ID", Import: "example.com/custom"},
		})
		if got.Name != "custom.ID" {
			t.Errorf("table override: Name = %q, want %q", got.Name, "custom.ID")
		}
	})
}

func TestIntegrationArrayTypeWithUUID(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
	})
	got := r.Resolve("uuid[]", false, "tag_ids", nil, nil)
	want := gotype.GoType{
		Name:      "[]uuid.UUID",
		Import:    uuidgoogle.ImportPath,
		ZeroValue: "nil",
		IsSlice:   true,
		FKConvert: gotype.FKStringSprint,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("uuid[] with google/uuid (-want +got):\n%s", diff)
	}
}

func TestResolve_SchemaEnums(t *testing.T) {
	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		{
			name:    "enum resolves to PascalCase Go type",
			sqlType: "user_role",
			want: gotype.GoType{
				Name:      "UserRole",
				ZeroValue: `""`,
				FKConvert: gotype.FKStringSprint,
			},
		},
		{
			name:     "nullable enum uses pointer",
			sqlType:  "user_role",
			nullable: true,
			want: gotype.GoType{
				Name:      "*UserRole",
				ZeroValue: "nil",
				FKConvert: gotype.FKStringSprint,
			},
		},
		{
			name:    "enum is case-insensitive",
			sqlType: "USER_ROLE",
			want: gotype.GoType{
				Name:      "UserRole",
				ZeroValue: `""`,
				FKConvert: gotype.FKStringSprint,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver("postgres", true, nil)
			r.RegisterEnum("user_role", "UserRole")

			got := r.Resolve(tt.sqlType, tt.nullable, "role", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolve_SchemaEnumOverriddenByTypeMap(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)
	r.RegisterEnum("user_role", "UserRole")

	got := r.Resolve("user_role", false, "role",
		map[string]string{"role": "string"}, nil)

	if got.Name != "string" {
		t.Errorf("type_map should override enum: got %q, want %q", got.Name, "string")
	}
}

func TestResolve_SchemaDomains(t *testing.T) {
	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		{
			name:    "domain resolves through base type",
			sqlType: "positive_int",
			want: gotype.GoType{
				Name:      "int32",
				ZeroValue: "0",
				FKConvert: gotype.FKStringSprint,
			},
		},
		{
			name:     "nullable domain resolves through base type",
			sqlType:  "positive_int",
			nullable: true,
			want: gotype.GoType{
				Name:      "*int32",
				ZeroValue: "nil",
				FKConvert: gotype.FKStringSprint,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver("postgres", true, nil)
			r.RegisterDomain("positive_int", "integer")

			got := r.Resolve(tt.sqlType, tt.nullable, "quantity", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolve_SchemaSets(t *testing.T) {
	tests := []struct {
		name     string
		sqlType  string
		nullable bool
		want     gotype.GoType
	}{
		{
			name:    "set resolves to named slice type",
			sqlType: "permissions_set",
			want: gotype.GoType{
				Name:          "PermissionsSet",
				ZeroValue:     "nil",
				SliceElemType: "PermissionsSetValue",
				FKConvert:     gotype.FKStringSprint,
			},
		},
		{
			name:     "nullable set uses same type (slices are reference types)",
			sqlType:  "permissions_set",
			nullable: true,
			want: gotype.GoType{
				Name:          "PermissionsSet",
				ZeroValue:     "nil",
				SliceElemType: "PermissionsSetValue",
				FKConvert:     gotype.FKStringSprint,
			},
		},
		{
			name:    "set is case-insensitive",
			sqlType: "PERMISSIONS_SET",
			want: gotype.GoType{
				Name:          "PermissionsSet",
				ZeroValue:     "nil",
				SliceElemType: "PermissionsSetValue",
				FKConvert:     gotype.FKStringSprint,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver("mysql", true, nil)
			r.RegisterSet("permissions_set", "PermissionsSet")

			got := r.Resolve(tt.sqlType, tt.nullable, "permissions", nil, nil)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolve() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResolve_SchemaSetOverriddenByTypeMap(t *testing.T) {
	r := gotype.NewResolver("mysql", true, nil)
	r.RegisterSet("permissions_set", "PermissionsSet")

	got := r.Resolve("permissions_set", false, "permissions",
		map[string]string{"permissions": "string"}, nil)

	if got.Name != "string" {
		t.Errorf("type_map should override set: got %q, want %q", got.Name, "string")
	}
}

// TestResolveAggregateSumWidening pins the dialect-specific SUM result-type
// widening: PostgreSQL SUM(smallint|integer) → bigint and SUM(bigint) →
// numeric; MySQL SUM(<integer>) → decimal; SQLite integers are already int64
// (no widening). Non-integer bases and non-SUM aggregates are unchanged.
func TestResolveAggregateSumWidening(t *testing.T) {
	tests := []struct {
		name     string
		dialect  config.Dialect
		sqlType  string
		nullable bool
		want     string // resolved GoType.Name
	}{
		// PostgreSQL: integer sums widen to bigint (int64); bigint sums to numeric (float64).
		{"pg smallint", "postgres", "smallint", false, "int64"},
		{"pg int2", "postgres", "int2", false, "int64"},
		{"pg integer", "postgres", "integer", false, "int64"},
		{"pg int4", "postgres", "int4", false, "int64"},
		{"pg integer nullable", "postgres", "integer", true, "*int64"},
		{"pg bigint -> numeric", "postgres", "bigint", false, "float64"},
		{"pg int8 -> numeric", "postgres", "int8", false, "float64"},
		{"pg numeric unchanged", "postgres", "numeric", false, "float64"},
		{"pg numeric(10,2) unchanged", "postgres", "numeric(10,2)", false, "float64"},
		{"pg double unchanged", "postgres", "double precision", false, "float64"},

		// MySQL: every integer sum widens to decimal (float64 here).
		{"mysql tinyint", "mysql", "tinyint", false, "float64"},
		{"mysql int", "mysql", "int", false, "float64"},
		{"mysql integer", "mysql", "integer", false, "float64"},
		{"mysql bigint", "mysql", "bigint", false, "float64"},
		{"mysql int unsigned", "mysql", "int unsigned", false, "float64"},
		{"mysql bigint unsigned", "mysql", "bigint unsigned", false, "float64"},
		{"mysql decimal unchanged", "mysql", "decimal", false, "float64"},

		// SQLite: integers are already int64; no widening.
		{"sqlite integer", "sqlite", "integer", false, "int64"},
		{"sqlite int", "sqlite", "int", false, "int64"},
		{"sqlite real unchanged", "sqlite", "real", false, "float64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver(tt.dialect, true, nil)
			got := r.ResolveAggregate("SUM", tt.sqlType, tt.nullable, "total", nil, nil)
			if got.Name != tt.want {
				t.Errorf("ResolveAggregate(SUM, %q, nullable=%v) [%s] = %q, want %q",
					tt.sqlType, tt.nullable, tt.dialect, got.Name, tt.want)
			}
		})
	}
}

// TestResolveAggregateSumWidening_Domain verifies widening keys on the
// domain-resolved base, not the domain name: positive_int (AS integer) sums to
// bigint in PostgreSQL. This is the exact shape that produced a *int32 scan
// target before the fix.
func TestResolveAggregateSumWidening_Domain(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)
	r.RegisterDomain("positive_int", "integer")

	got := r.ResolveAggregate("SUM", "positive_int", true, "total_quantity", nil, nil)
	if got.Name != "*int64" {
		t.Errorf("SUM over positive_int domain = %q, want *int64 (integer widens to bigint)", got.Name)
	}
}

// TestResolveAggregate_NonSumUnchanged confirms only SUM widens: a MIN/MAX
// routed through ResolveAggregate resolves through the normal chain, and the
// column type_map still wins over any inference.
func TestResolveAggregate_NonSumUnchanged(t *testing.T) {
	r := gotype.NewResolver("postgres", true, nil)

	if got := r.ResolveAggregate("MAX", "integer", false, "peak", nil, nil); got.Name != "int32" {
		t.Errorf("MAX over integer = %q, want int32 (MAX does not widen)", got.Name)
	}

	typeMap := map[string]string{"total": "decimal.Decimal"}
	if got := r.ResolveAggregate("SUM", "integer", false, "total", typeMap, nil); got.Name != "decimal.Decimal" {
		t.Errorf("SUM with column type_map = %q, want decimal.Decimal (type_map wins)", got.Name)
	}
}

// TestStdlibUUIDIntegration covers the standard library uuid integration
// (PRD §7.4). It is the one UUID integration with no null wrapper, so its
// nullable form is the pointer and its zero value must be the composite
// literal — the package's Nil is a function, not a variable.
func TestStdlibUUIDIntegration(t *testing.T) {
	t.Run("non-nullable", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		want := gotype.GoType{
			Name:      "uuid.UUID",
			Import:    uuidstd.ImportPath,
			ZeroValue: "uuid.UUID{}",
			FKConvert: gotype.FKStringStringer,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("stdlib uuid non-nullable (-want +got):\n%s", diff)
		}
	})

	// No NullUUID exists, so uuid.UUID is nullPtrOnly (PRD §7.3): the
	// nullable form is *uuid.UUID at both use_pointers settings, where the
	// wrapper-backed integrations would hand back uuid.NullUUID at false.
	t.Run("nullable is the pointer regardless of usePointers", func(t *testing.T) {
		for _, usePointers := range []bool{true, false} {
			r := gotype.NewResolver("postgres", usePointers, map[string]config.TypeOverride{
				"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
			})
			got := r.Resolve("uuid", true, "id", nil, nil)
			want := gotype.GoType{
				Name:      "*uuid.UUID",
				Import:    uuidstd.ImportPath,
				ZeroValue: "nil",
				FKConvert: gotype.FKStringStringer,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("stdlib uuid nullable (usePointers=%v) (-want +got):\n%s", usePointers, diff)
			}
		}
	})

	t.Run("zero value is the literal, not uuid.Nil", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, nil)
		if got.ZeroValue != "uuid.UUID{}" {
			t.Errorf("stdlib uuid ZeroValue = %q, want %q (Nil is a function here)", got.ZeroValue, "uuid.UUID{}")
		}
		if got.ZeroValue == "uuid.Nil" {
			t.Error("stdlib uuid ZeroValue must not be uuid.Nil — that is the gofrs spelling")
		}
	})

	// uuid[] resolves through the array rule over the resolved base type
	// (PRD §7.2 "Array type rule"), so the element carries no pointer.
	t.Run("uuid array", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		got := r.Resolve("uuid[]", false, "ids", nil, nil)
		want := gotype.GoType{
			Name:      "[]uuid.UUID",
			Import:    uuidstd.ImportPath,
			ZeroValue: "nil",
			IsSlice:   true,
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("stdlib uuid[] (-want +got):\n%s", diff)
		}
	})
}

// TestStdlibUUIDFKExtraction pins the O2M relationship-loader path for a
// nullable stdlib UUID FK. There is no Valid field to guard on, so the
// pointer branch supplies the nil guard and the parenthesised deref
// (PRD §7.4).
func TestStdlibUUIDFKExtraction(t *testing.T) {
	r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
		"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
	})

	nullable := r.Resolve("uuid", true, "owner_id", nil, nil)
	got := r.DeriveScalarExtraction(nullable.Name)
	want := gotype.ScalarExtraction{
		GuardExpr:    "$v != nil",
		UnwrapExpr:   "(*$v)",
		StringMethod: gotype.FKStringStringer,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("DeriveScalarExtraction(%q) (-want +got):\n%s", nullable.Name, diff)
	}

	// The non-null form needs no guard and stringifies directly.
	bare := r.DeriveScalarExtraction("uuid.UUID")
	if bare.GuardExpr != "" || bare.UnwrapExpr != "$v" || bare.StringMethod != gotype.FKStringStringer {
		t.Errorf("DeriveScalarExtraction(uuid.UUID) = %+v, want no guard, $v, Stringer", bare)
	}
}

// TestIntegrationNullableNotBackfilledAcrossLibraries is the regression guard
// for cross-library enrichment. All three UUID integrations claim the SQL type
// "uuid", so a project whose overrides pull in two of them once had the second
// one's wrapper written onto the first one's import — a stdlib-bound column
// resolving to uuid.NullUUID while importing "uuid", which declares no such
// type. An integration must only enrich an override that names it.
func TestIntegrationNullableNotBackfilledAcrossLibraries(t *testing.T) {
	t.Run("stdlib uuid override is not given googles NullUUID", func(t *testing.T) {
		// The second entry pulls uuidgoogle into detection without claiming
		// the uuid SQL type for itself.
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
			"text": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", true, "ref", nil, nil)
		if got.Name != "*uuid.UUID" {
			t.Errorf("nullable stdlib uuid = %q, want *uuid.UUID (must not inherit a wrapper from another library)", got.Name)
		}
		if got.Import != uuidstd.ImportPath {
			t.Errorf("nullable stdlib uuid Import = %q, want %q", got.Import, uuidstd.ImportPath)
		}
	})

	t.Run("wrapper-backed override is not given the stdlibs empty nullable", func(t *testing.T) {
		// The mirror direction: uuidstd must not blank out google's wrapper.
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
			"text": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		got := r.Resolve("uuid", false, "ref", nil, nil)
		if got.ZeroValue != "uuid.UUID{}" {
			t.Errorf("google uuid ZeroValue = %q, want uuid.UUID{}", got.ZeroValue)
		}
		gotNull := r.Resolve("uuid", true, "ref", nil, nil)
		if gotNull.Name != "uuid.NullUUID" {
			t.Errorf("nullable google uuid = %q, want uuid.NullUUID (its own wrapper must survive)", gotNull.Name)
		}
	})

	t.Run("gofrs zero value is not overwritten by another library", func(t *testing.T) {
		// gofrs spells the zero UUID as a package variable; google and the
		// stdlib both spell it as a literal. Cross-enrichment would silently
		// swap them.
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
			"text": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		got := r.Resolve("uuid", false, "ref", nil, nil)
		if got.ZeroValue != "uuid.Nil" {
			t.Errorf("gofrs uuid ZeroValue = %q, want uuid.Nil", got.ZeroValue)
		}
	})

	// An override naming its own library still receives that library's
	// enrichment — the guard must not disable the feature it protects.
	t.Run("same-library enrichment still applies", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", true, "ref", nil, nil)
		if got.Name != "uuid.NullUUID" {
			t.Errorf("nullable google uuid = %q, want uuid.NullUUID from enrichment", got.Name)
		}
		if zero := r.Resolve("uuid", false, "ref", nil, nil).ZeroValue; zero != "uuid.UUID{}" {
			t.Errorf("google uuid ZeroValue = %q, want uuid.UUID{} from enrichment", zero)
		}
	})

	// An override that names no import is not a competing claim, so the
	// integration covering that SQL type still enriches it.
	//
	// gen rejects this config before it can generate anything —
	// validateTypeOverrideImports refuses a package-qualified `overrides.types`
	// entry with no import — so this pins resolver behavior no valid config
	// reaches. Kept as the record that enrichment fills Nullable and ZeroValue
	// and still never fills Import.
	t.Run("import-less override still enriched", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
			"decimal": {Type: "decimal.Decimal"},
		})
		got := r.Resolve("decimal", true, "amount", nil, nil)
		if got.Name != "decimal.NullDecimal" {
			t.Errorf("nullable decimal = %q, want decimal.NullDecimal (import-less override stays eligible)", got.Name)
		}
	})
}

// TestIntegrationDetectionIsDeterministic pins reproducible generation when a
// config pulls two UUID libraries into detection. All three UUID integrations
// claim the SQL type "uuid", so registration for a type the user left
// unclaimed used to be a last-writer-wins race over map iteration order: the
// same config resolved to *uuid.UUID on one run and uuid.NullUUID on the next.
// Which library wins is not what this pins — that config is ambiguous and is
// rejected. Only that the answer does not move (PRD §5.7).
func TestIntegrationDetectionIsDeterministic(t *testing.T) {
	build := func() gotype.GoType {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"text":    {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
			"varchar": {Type: "uuid.UUID", Import: uuidstd.ImportPath},
		})
		// No "uuid" override, so resolution falls to whichever integration
		// registered itself for the SQL type.
		return r.Resolve("uuid", true, "ref", nil, nil)
	}

	first := build()
	for i := range 50 {
		if got := build(); got != first {
			t.Fatalf("run %d resolved uuid to %+v, first run gave %+v — detection order must not vary", i, got, first)
		}
	}
}

// TestIntegrationDetectionIsScopeIndependent is the black-box statement of PRD
// §7.4: identical `overrides.types` text resolves to an identical mapping
// whether it is written globally or under `tables.<table>.overrides.types`.
// detectIntegrations used to walk only the global map, so the same three lines
// of YAML produced uuid.NullUUID at one nesting depth and *uuid.UUID at the
// other — a difference with no cause a consumer could see in their config.
//
// Each case resolves the same SQL type twice from the same override text, once
// with the text global and once with it table-scoped, and requires the two
// GoTypes to be equal. Neither side spells out what the answer should be: the
// point is the parity, and pinning a literal here would only duplicate the
// per-integration tests above.
func TestIntegrationDetectionIsScopeIndependent(t *testing.T) {
	tests := []struct {
		name     string
		sqlType  string
		override config.TypeOverride
	}{
		{
			name:     "google uuid, no nullable declared",
			sqlType:  "uuid",
			override: config.TypeOverride{Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		},
		{
			name:     "gofrs uuid, no nullable declared",
			sqlType:  "uuid",
			override: config.TypeOverride{Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
		},
		{
			name:     "stdlib uuid, which has no wrapper to back-fill",
			sqlType:  "uuid",
			override: config.TypeOverride{Type: "uuid.UUID", Import: uuidstd.ImportPath},
		},
		{
			name:     "shopspring decimal, no nullable declared",
			sqlType:  "numeric",
			override: config.TypeOverride{Type: "decimal.Decimal", Import: decimal.ImportPath},
		},
		{
			name:    "explicit wrapper nullable",
			sqlType: "uuid",
			override: config.TypeOverride{
				Type:     "uuid.UUID",
				Import:   uuidgoogle.ImportPath,
				Nullable: config.NullableVariant{Type: "uuid.NullUUID"},
			},
		},
		{
			name:    "explicit pointer nullable, which enrichment must not replace",
			sqlType: "uuid",
			override: config.TypeOverride{
				Type:     "uuid.UUID",
				Import:   uuidgoogle.ImportPath,
				Nullable: config.NullableVariant{Type: "*uuid.UUID"},
			},
		},
		{
			name:     "explicit zero value, which enrichment must not replace",
			sqlType:  "uuid",
			override: config.TypeOverride{Type: "uuid.UUID", Import: uuidgofrs.ImportPath, ZeroValue: "customZero"},
		},
		{
			name:     "a type override naming no known library, which activates nothing",
			sqlType:  "text",
			override: config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
		},
		{
			name:     "an array of an integration-bound base type",
			sqlType:  "uuid[]",
			override: config.TypeOverride{Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		},
	}

	for _, tt := range tests {
		for _, usePointers := range []bool{true, false} {
			for _, nullable := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/usePointers=%v/nullable=%v", tt.name, usePointers, nullable), func(t *testing.T) {
					// The override key is the base SQL type: an array column
					// resolves its element through the same chain.
					key := strings.TrimSuffix(tt.sqlType, "[]")

					globalScoped := gotype.NewResolver("postgres", usePointers, map[string]config.TypeOverride{
						key: tt.override,
					})
					wantGlobal := globalScoped.Resolve(tt.sqlType, nullable, "col", nil, nil)

					tableScoped := gotype.NewResolver("postgres", usePointers, nil)
					gotTable := tableScoped.Resolve(tt.sqlType, nullable, "col", nil, map[string]config.TypeOverride{
						key: tt.override,
					})

					if diff := cmp.Diff(wantGlobal, gotTable); diff != "" {
						t.Errorf("%s resolved differently by scope (-global +table):\n%s", tt.sqlType, diff)
					}
				})
			}
		}
	}
}

// TestTableScopedIntegrationActivatesSiblingSQLTypes pins the other half of
// "activates that integration" (PRD §7.4): an integration claims a set of SQL
// types, and declaring any one of them at table scope binds the rest of that
// table's columns too — shopspring/decimal claims both `numeric` and
// `decimal`, so naming only `numeric` still moves the table's `decimal`
// columns off float64. Globally this has always worked; this pins the table
// scope to the same behavior.
func TestTableScopedIntegrationActivatesSiblingSQLTypes(t *testing.T) {
	tableOverrides := map[string]config.TypeOverride{
		"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
	}

	t.Run("the undeclared sibling binds to the integration", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)
		got := r.Resolve("decimal", false, "amount", nil, tableOverrides)
		want := gotype.GoType{
			Name:      "decimal.Decimal",
			Import:    decimal.ImportPath,
			ZeroValue: "decimal.Decimal{}",
			FKConvert: gotype.FKStringSprint,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("table-scoped sibling activation (-want +got):\n%s", diff)
		}
	})

	t.Run("a table with no overrides block is untouched", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, nil)
		got := r.Resolve("decimal", false, "amount", nil, nil)
		if got.Name != "float64" {
			t.Errorf("unscoped decimal = %q, want float64 — activation must not leak past its table", got.Name)
		}
	})

	t.Run("an explicit global override still outranks a table-scoped activation", func(t *testing.T) {
		// Step 3 beats step 4: the activation is an integration, and every
		// explicit override at either scope sits above it (PRD §7.1).
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"decimal": {Type: "money.Amount", Import: "example.com/money"},
		})
		got := r.Resolve("decimal", false, "amount", nil, tableOverrides)
		if got.Name != "money.Amount" {
			t.Errorf("decimal = %q, want money.Amount — an explicit global override outranks an integration", got.Name)
		}
	})

	t.Run("a table-scoped activation outranks a global one for the same SQL type", func(t *testing.T) {
		// Both scopes activate a UUID integration for `uuid` without declaring
		// that key, so only step 4's internal ordering decides. A package
		// resolving two UUID libraries is rejected outright one layer up (PRD
		// §4.13); what is pinned here is that the resolver's own answer is the
		// more specific scope, not whichever map was consulted first.
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"text": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		got := r.Resolve("uuid", false, "id", nil, map[string]config.TypeOverride{
			"varchar": {Type: "uuid.UUID", Import: uuidgofrs.ImportPath},
		})
		if got.Import != uuidgofrs.ImportPath {
			t.Errorf("uuid Import = %q, want %q — the table-scoped activation is the more specific one", got.Import, uuidgofrs.ImportPath)
		}
	})
}

// TestTableScopedOverridesAreNotMutated guards the one thing enrichment at
// table scope must not borrow from the global path: r.globalOverrides is the
// resolver's own map and is enriched in place, but a table's map is handed
// straight from the parsed config and is read by every other consumer of it.
// Writing through it would make resolution order-dependent across tables.
func TestTableScopedOverridesAreNotMutated(t *testing.T) {
	tableOverrides := map[string]config.TypeOverride{
		"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
	}
	want := maps.Clone(tableOverrides)

	r := gotype.NewResolver("postgres", true, nil)
	r.Resolve("uuid", true, "id", nil, tableOverrides)

	if diff := cmp.Diff(want, tableOverrides); diff != "" {
		t.Errorf("Resolve mutated the caller's table overrides (-before +after):\n%s", diff)
	}
}

// TestDefaultUUIDBinding covers the binding a `uuid` column takes with an
// empty `overrides.types` — the standard library, as of PRD §7.2's mapping
// table and §7.4's "the standard library binding is the default".
//
// TestStdlibUUIDIntegration above reaches the same Go type by naming
// `import: uuid` explicitly. The two are not redundant: that one proves the
// integration is selectable, this one proves it is what a consumer who
// configures nothing gets. The
// override path would keep passing if the dialect mapping still said
// `string`.
func TestDefaultUUIDBinding(t *testing.T) {
	// The default must not depend on use_pointers. uuid.UUID is nullPtrOnly
	// (PRD §7.3) — the standard library ships no NullUUID — so the nullable
	// form is *uuid.UUID at both settings, where a nullSQL type would switch
	// to a sql.Null* wrapper at false.
	for _, usePointers := range []bool{true, false} {
		tests := []struct {
			name     string
			sqlType  string
			nullable bool
			want     gotype.GoType
		}{
			{
				name:    "uuid",
				sqlType: "uuid",
				want: gotype.GoType{
					Name:      "uuid.UUID",
					Import:    uuidstd.ImportPath,
					ZeroValue: "uuid.UUID{}",
					FKConvert: gotype.FKStringStringer,
				},
			},
			{
				name:     "uuid nullable",
				sqlType:  "uuid",
				nullable: true,
				want: gotype.GoType{
					Name:      "*uuid.UUID",
					Import:    uuidstd.ImportPath,
					ZeroValue: "nil",
					FKConvert: gotype.FKStringStringer,
				},
			},
			{
				// The array rule resolves the base in its non-null form, so
				// the element is the bare type (PRD §7.2 "Array type rule").
				name:    "uuid[]",
				sqlType: "uuid[]",
				want: gotype.GoType{
					Name:      "[]uuid.UUID",
					Import:    uuidstd.ImportPath,
					ZeroValue: "nil",
					IsSlice:   true,
					FKConvert: gotype.FKStringSprint,
				},
			},
			{
				// A nullable array is the same slice type — nil is already
				// its zero value.
				name:     "uuid[] nullable",
				sqlType:  "uuid[]",
				nullable: true,
				want: gotype.GoType{
					Name:      "[]uuid.UUID",
					Import:    uuidstd.ImportPath,
					ZeroValue: "nil",
					IsSlice:   true,
					FKConvert: gotype.FKStringSprint,
				},
			},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/usePointers=%v", tt.name, usePointers), func(t *testing.T) {
				r := gotype.NewResolver("postgres", usePointers, nil)
				got := r.Resolve(tt.sqlType, tt.nullable, "id", nil, nil)
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("Resolve(%q, nullable=%v) (-want +got):\n%s", tt.sqlType, tt.nullable, diff)
				}
			})
		}
	}

	// The flip is PostgreSQL-only: MySQL and SQLite declare no `uuid` SQL
	// type, so a column spelled `uuid` there still lands on builtinResolve's
	// unknown-type string fallback and must not acquire the import.
	t.Run("other dialects declare no uuid type", func(t *testing.T) {
		for _, dialect := range []config.Dialect{config.DialectMySQL, config.DialectSQLite} {
			r := gotype.NewResolver(dialect, true, nil)
			got := r.Resolve("uuid", false, "id", nil, nil)
			want := gotype.GoType{Name: "string", ZeroValue: `""`}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Resolve(uuid) on %s (-want +got):\n%s", dialect, diff)
			}
		}
	})

	// A consumer opts out by naming a wrapper library, which must win over
	// the new default at both nullabilities (PRD §7.1 step 4 over step 6).
	t.Run("a wrapper override still wins", func(t *testing.T) {
		r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{
			"uuid": {Type: "uuid.UUID", Import: uuidgoogle.ImportPath},
		})
		if got := r.Resolve("uuid", false, "id", nil, nil); got.Import != uuidgoogle.ImportPath {
			t.Errorf("Resolve(uuid).Import = %q, want %q", got.Import, uuidgoogle.ImportPath)
		}
		got := r.Resolve("uuid", true, "id", nil, nil)
		want := gotype.GoType{
			Name:      "uuid.NullUUID",
			Import:    uuidgoogle.ImportPath,
			ZeroValue: "uuid.NullUUID{}",
			FKConvert: gotype.FKStringStringer,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Resolve(uuid, nullable) with google override (-want +got):\n%s", diff)
		}
	})
}
