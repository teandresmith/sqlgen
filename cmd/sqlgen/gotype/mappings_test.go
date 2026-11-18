package gotype_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

// mysqlSetType is the SQL type the MySQL parsers write for a SET column
// whose values are in the schema, registered with the resolver as the
// generated named slice.
const mysqlSetType = "users__permissions_set"

// mysqlEnumType is the SQL type the MySQL parsers write for an inline ENUM
// column whose values are in the schema, registered as the generated enum.
const mysqlEnumType = "users__role_enum"

// builtInMapping is one row of a dialect's built-in mapping table: every
// spelling the row lists, its Go type, and its nullable Go type with and
// without pointers.
type builtInMapping struct {
	name         string
	dialect      config.Dialect
	spellings    []string
	goType       string
	nullPtr      string
	nullNoPtr    string
	array        bool
	registerSet  bool
	registerEnum bool
}

var builtInMappings = []builtInMapping{
	{name: "postgres text", dialect: config.DialectPostgres, spellings: []string{"text", "varchar", "char", "name"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "postgres int2", dialect: config.DialectPostgres, spellings: []string{"int2", "smallint"}, goType: "int16", nullPtr: "*int16", nullNoPtr: "sql.NullInt16"},
	{name: "postgres int4", dialect: config.DialectPostgres, spellings: []string{"int4", "integer", "serial"}, goType: "int32", nullPtr: "*int32", nullNoPtr: "sql.NullInt32"},
	{name: "postgres int8", dialect: config.DialectPostgres, spellings: []string{"int8", "bigint", "bigserial"}, goType: "int64", nullPtr: "*int64", nullNoPtr: "sql.NullInt64"},
	{name: "postgres float4", dialect: config.DialectPostgres, spellings: []string{"float4", "real"}, goType: "float32", nullPtr: "*float32", nullNoPtr: "sql.NullFloat64"},
	{name: "postgres float8", dialect: config.DialectPostgres, spellings: []string{"float8", "double precision"}, goType: "float64", nullPtr: "*float64", nullNoPtr: "sql.NullFloat64"},
	{name: "postgres numeric", dialect: config.DialectPostgres, spellings: []string{"numeric", "decimal"}, goType: "float64", nullPtr: "*float64", nullNoPtr: "sql.NullFloat64"},
	{name: "postgres bool", dialect: config.DialectPostgres, spellings: []string{"bool", "boolean"}, goType: "bool", nullPtr: "*bool", nullNoPtr: "sql.NullBool"},
	{name: "postgres bytea", dialect: config.DialectPostgres, spellings: []string{"bytea"}, goType: "[]byte", nullPtr: "[]byte", nullNoPtr: "[]byte"},
	{name: "postgres timestamp", dialect: config.DialectPostgres, spellings: []string{"timestamp", "timestamptz"}, goType: "time.Time", nullPtr: "*time.Time", nullNoPtr: "sql.NullTime"},
	{name: "postgres date", dialect: config.DialectPostgres, spellings: []string{"date"}, goType: "time.Time", nullPtr: "*time.Time", nullNoPtr: "sql.NullTime"},
	{name: "postgres time", dialect: config.DialectPostgres, spellings: []string{"time"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "postgres interval", dialect: config.DialectPostgres, spellings: []string{"interval"}, goType: "time.Duration", nullPtr: "*time.Duration", nullNoPtr: "*time.Duration"},
	{name: "postgres uuid", dialect: config.DialectPostgres, spellings: []string{"uuid"}, goType: "uuid.UUID", nullPtr: "*uuid.UUID", nullNoPtr: "*uuid.UUID"},
	{name: "postgres json", dialect: config.DialectPostgres, spellings: []string{"json", "jsonb"}, goType: "types.JSON", nullPtr: "types.JSON", nullNoPtr: "types.JSON"},
	{name: "postgres inet", dialect: config.DialectPostgres, spellings: []string{"inet"}, goType: "net.IP", nullPtr: "net.IP", nullNoPtr: "net.IP"},
	{name: "postgres cidr", dialect: config.DialectPostgres, spellings: []string{"cidr"}, goType: "net.IPNet", nullPtr: "*net.IPNet", nullNoPtr: "*net.IPNet"},
	{name: "postgres macaddr", dialect: config.DialectPostgres, spellings: []string{"macaddr"}, goType: "net.HardwareAddr", nullPtr: "net.HardwareAddr", nullNoPtr: "net.HardwareAddr"},
	{name: "postgres text array", dialect: config.DialectPostgres, spellings: []string{"text[]", "varchar[]"}, goType: "[]string", nullPtr: "[]string", nullNoPtr: "[]string", array: true},
	{name: "postgres int2 array", dialect: config.DialectPostgres, spellings: []string{"int2[]", "smallint[]"}, goType: "[]int16", nullPtr: "[]int16", nullNoPtr: "[]int16", array: true},
	{name: "postgres int4 array", dialect: config.DialectPostgres, spellings: []string{"int4[]", "integer[]"}, goType: "[]int32", nullPtr: "[]int32", nullNoPtr: "[]int32", array: true},
	{name: "postgres int8 array", dialect: config.DialectPostgres, spellings: []string{"int8[]", "bigint[]"}, goType: "[]int64", nullPtr: "[]int64", nullNoPtr: "[]int64", array: true},
	{name: "postgres float4 array", dialect: config.DialectPostgres, spellings: []string{"float4[]", "real[]"}, goType: "[]float32", nullPtr: "[]float32", nullNoPtr: "[]float32", array: true},
	{name: "postgres float8 array", dialect: config.DialectPostgres, spellings: []string{"float8[]", "double precision[]"}, goType: "[]float64", nullPtr: "[]float64", nullNoPtr: "[]float64", array: true},
	{name: "postgres bool array", dialect: config.DialectPostgres, spellings: []string{"bool[]", "boolean[]"}, goType: "[]bool", nullPtr: "[]bool", nullNoPtr: "[]bool", array: true},
	{name: "postgres uuid array", dialect: config.DialectPostgres, spellings: []string{"uuid[]"}, goType: "[]uuid.UUID", nullPtr: "[]uuid.UUID", nullNoPtr: "[]uuid.UUID", array: true},
	{name: "postgres numeric array", dialect: config.DialectPostgres, spellings: []string{"numeric[]", "decimal[]"}, goType: "[]float64", nullPtr: "[]float64", nullNoPtr: "[]float64", array: true},
	{name: "postgres timestamp array", dialect: config.DialectPostgres, spellings: []string{"timestamp[]", "timestamptz[]"}, goType: "[]time.Time", nullPtr: "[]time.Time", nullNoPtr: "[]time.Time", array: true},

	{name: "mysql tinyint", dialect: config.DialectMySQL, spellings: []string{"tinyint"}, goType: "int8", nullPtr: "*int8", nullNoPtr: "sql.NullInt16"},
	{name: "mysql smallint", dialect: config.DialectMySQL, spellings: []string{"smallint"}, goType: "int16", nullPtr: "*int16", nullNoPtr: "sql.NullInt16"},
	{name: "mysql int", dialect: config.DialectMySQL, spellings: []string{"int", "mediumint"}, goType: "int32", nullPtr: "*int32", nullNoPtr: "sql.NullInt32"},
	{name: "mysql bigint", dialect: config.DialectMySQL, spellings: []string{"bigint"}, goType: "int64", nullPtr: "*int64", nullNoPtr: "sql.NullInt64"},
	{name: "mysql tinyint unsigned", dialect: config.DialectMySQL, spellings: []string{"tinyint unsigned"}, goType: "uint8", nullPtr: "*uint8", nullNoPtr: "sql.NullInt16"},
	{name: "mysql smallint unsigned", dialect: config.DialectMySQL, spellings: []string{"smallint unsigned"}, goType: "uint16", nullPtr: "*uint16", nullNoPtr: "sql.NullInt32"},
	{name: "mysql int unsigned", dialect: config.DialectMySQL, spellings: []string{"int unsigned", "mediumint unsigned"}, goType: "uint32", nullPtr: "*uint32", nullNoPtr: "sql.NullInt64"},
	{name: "mysql bigint unsigned", dialect: config.DialectMySQL, spellings: []string{"bigint unsigned"}, goType: "uint64", nullPtr: "*uint64", nullNoPtr: "sql.NullInt64"},
	{name: "mysql float", dialect: config.DialectMySQL, spellings: []string{"float"}, goType: "float32", nullPtr: "*float32", nullNoPtr: "sql.NullFloat64"},
	{name: "mysql double", dialect: config.DialectMySQL, spellings: []string{"double", "decimal", "decimal(10,0)"}, goType: "float64", nullPtr: "*float64", nullNoPtr: "sql.NullFloat64"},
	{name: "mysql varchar", dialect: config.DialectMySQL, spellings: []string{"varchar", "text", "char", "enum"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "mysql enum type", dialect: config.DialectMySQL, spellings: []string{mysqlEnumType}, goType: "UsersRoleEnum", nullPtr: "*UsersRoleEnum", nullNoPtr: "*UsersRoleEnum", registerEnum: true},
	{name: "mysql set type", dialect: config.DialectMySQL, spellings: []string{mysqlSetType}, goType: "UsersPermissionsSet", nullPtr: "UsersPermissionsSet", nullNoPtr: "UsersPermissionsSet", registerSet: true},
	{name: "mysql datetime", dialect: config.DialectMySQL, spellings: []string{"datetime", "timestamp"}, goType: "time.Time", nullPtr: "*time.Time", nullNoPtr: "sql.NullTime"},
	{name: "mysql date", dialect: config.DialectMySQL, spellings: []string{"date"}, goType: "time.Time", nullPtr: "*time.Time", nullNoPtr: "sql.NullTime"},
	{name: "mysql json", dialect: config.DialectMySQL, spellings: []string{"json"}, goType: "types.JSON", nullPtr: "types.JSON", nullNoPtr: "types.JSON"},
	{name: "mysql blob", dialect: config.DialectMySQL, spellings: []string{"blob", "binary", "varbinary"}, goType: "[]byte", nullPtr: "[]byte", nullNoPtr: "[]byte"},
	{name: "mysql tinyint 1", dialect: config.DialectMySQL, spellings: []string{"tinyint(1)"}, goType: "bool", nullPtr: "*bool", nullNoPtr: "sql.NullBool"},
	{name: "mysql time", dialect: config.DialectMySQL, spellings: []string{"time"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "mysql year", dialect: config.DialectMySQL, spellings: []string{"year"}, goType: "int16", nullPtr: "*int16", nullNoPtr: "sql.NullInt16"},
	{name: "mysql set string", dialect: config.DialectMySQL, spellings: []string{"set"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "mysql tinytext", dialect: config.DialectMySQL, spellings: []string{"tinytext", "mediumtext", "longtext"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "mysql tinyblob", dialect: config.DialectMySQL, spellings: []string{"tinyblob", "mediumblob", "longblob"}, goType: "[]byte", nullPtr: "[]byte", nullNoPtr: "[]byte"},
	{name: "mysql bit 1", dialect: config.DialectMySQL, spellings: []string{"bit(1)"}, goType: "bool", nullPtr: "*bool", nullNoPtr: "sql.NullBool"},
	{name: "mysql bit n", dialect: config.DialectMySQL, spellings: []string{"bit(2)", "bit(8)", "bit(64)"}, goType: "[]byte", nullPtr: "[]byte", nullNoPtr: "[]byte"},

	{name: "sqlite integer", dialect: config.DialectSQLite, spellings: []string{"integer", "int", "tinyint", "smallint", "bigint"}, goType: "int64", nullPtr: "*int64", nullNoPtr: "sql.NullInt64"},
	{name: "sqlite text", dialect: config.DialectSQLite, spellings: []string{"text", "varchar", "char", "clob", "time"}, goType: "string", nullPtr: "*string", nullNoPtr: "sql.NullString"},
	{name: "sqlite real", dialect: config.DialectSQLite, spellings: []string{"real", "float", "double", "numeric", "decimal"}, goType: "float64", nullPtr: "*float64", nullNoPtr: "sql.NullFloat64"},
	{name: "sqlite blob", dialect: config.DialectSQLite, spellings: []string{"blob"}, goType: "[]byte", nullPtr: "[]byte", nullNoPtr: "[]byte"},
	{name: "sqlite boolean", dialect: config.DialectSQLite, spellings: []string{"boolean", "bool"}, goType: "bool", nullPtr: "*bool", nullNoPtr: "sql.NullBool"},
	{name: "sqlite datetime", dialect: config.DialectSQLite, spellings: []string{"datetime", "timestamp", "timestamptz", "date"}, goType: "types.DateTime", nullPtr: "types.NullDateTime", nullNoPtr: "types.NullDateTime"},
}

func mappingResolver(m builtInMapping, usePointers bool, global map[string]config.TypeOverride) *gotype.Resolver {
	r := gotype.NewResolver(m.dialect, usePointers, global)
	if m.registerSet {
		r.RegisterSet("", mysqlSetType, "UsersPermissionsSet")
	}
	if m.registerEnum {
		r.RegisterEnum("", mysqlEnumType, "UsersRoleEnum")
	}
	return r
}

// TestBuiltInMappings resolves every spelling of every built-in mapping row,
// NOT NULL and nullable under both `use_pointers` settings.
func TestBuiltInMappings(t *testing.T) {
	for _, m := range builtInMappings {
		t.Run(m.name, func(t *testing.T) {
			cases := []struct {
				name        string
				nullable    bool
				usePointers bool
				want        string
			}{
				{"not null", false, true, m.goType},
				{"nullable with pointers", true, true, m.nullPtr},
				{"nullable without pointers", true, false, m.nullNoPtr},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := mappingResolver(m, tc.usePointers, nil)
					for _, sqlType := range m.spellings {
						got := r.Resolve(sqlType, tc.nullable, "c", nil, nil)
						if got.Name != tc.want {
							t.Errorf("Resolve(%q, nullable=%v) Name = %q, want %q", sqlType, tc.nullable, got.Name, tc.want)
						}
						if got.IsSlice != m.array {
							t.Errorf("Resolve(%q) IsSlice = %v, want %v", sqlType, got.IsSlice, m.array)
						}
					}
				})
			}
		})
	}
}

// TestTypeOverrideWinsForEverySQLType keys an `overrides.types` entry, global
// and table-scoped, by each spelling of every built-in mapping row. A scalar
// column takes the entry's type, and its `nullable` variant when nullable. An
// array column is keyed by its base type and takes a slice of the entry's
// type in both cases.
func TestTypeOverrideWinsForEverySQLType(t *testing.T) {
	override := config.TypeOverride{
		Type:     "ov.Custom",
		Import:   "example.com/ov",
		Nullable: config.NullableVariant{Type: "ov.NullCustom"},
	}
	for _, m := range builtInMappings {
		if m.registerSet || m.registerEnum {
			continue // a generated SET or ENUM type has no overrides.types key
		}
		t.Run(m.name, func(t *testing.T) {
			wantNotNull, wantNullable := "ov.Custom", "ov.NullCustom"
			if m.array {
				wantNotNull, wantNullable = "[]ov.Custom", "[]ov.Custom"
			}
			scopes := []struct{ name string }{{"global"}, {"table"}}
			for _, scope := range scopes {
				t.Run(scope.name, func(t *testing.T) {
					for _, usePointers := range []bool{true, false} {
						for _, sqlType := range m.spellings {
							key := sqlType
							if m.array {
								key = sqlType[:len(sqlType)-len("[]")]
							}
							overrides := map[string]config.TypeOverride{key: override}
							var global, table map[string]config.TypeOverride
							if scope.name == "global" {
								global = overrides
							} else {
								table = overrides
							}
							r := mappingResolver(m, usePointers, global)
							if got := r.Resolve(sqlType, false, "c", nil, table); got.Name != wantNotNull || got.Import != "example.com/ov" {
								t.Errorf("Resolve(%q) = {%q, %q}, want {%q, %q}", sqlType, got.Name, got.Import, wantNotNull, "example.com/ov")
							}
							if got := r.Resolve(sqlType, true, "c", nil, table); got.Name != wantNullable {
								t.Errorf("Resolve(%q, nullable, use_pointers=%v) Name = %q, want %q", sqlType, usePointers, got.Name, wantNullable)
							}
						}
					}
				})
			}
		})
	}
}

// TestIntegrationsReachTheirSQLTypes selects each built-in integration and
// resolves the SQL types it matches, directly and as array elements, and the
// spellings sharing a mapping row that it does not match.
func TestIntegrationsReachTheirSQLTypes(t *testing.T) {
	uuidLibs := []struct {
		name     string
		imp      string
		nullable string
	}{
		{"standard library", uuidstd.ImportPath, "*uuid.UUID"},
		{"google", uuidgoogle.ImportPath, "uuid.NullUUID"},
		{"gofrs", uuidgofrs.ImportPath, "uuid.NullUUID"},
	}
	decimalOn := func(key string) map[string]config.TypeOverride {
		return map[string]config.TypeOverride{key: {Type: "decimal.Decimal", Import: decimal.ImportPath}}
	}
	type want struct {
		sqlType  string
		nullable bool
		name     string
	}
	check := func(t *testing.T, dialect config.Dialect, global map[string]config.TypeOverride, wants []want) {
		t.Helper()
		for _, usePointers := range []bool{true, false} {
			r := gotype.NewResolver(dialect, usePointers, global)
			for _, w := range wants {
				if got := r.Resolve(w.sqlType, w.nullable, "c", nil, nil); got.Name != w.name {
					t.Errorf("Resolve(%q, nullable=%v, use_pointers=%v) Name = %q, want %q", w.sqlType, w.nullable, usePointers, got.Name, w.name)
				}
			}
		}
	}

	t.Run("postgres uuid", func(t *testing.T) {
		for _, lib := range uuidLibs {
			t.Run(lib.name, func(t *testing.T) {
				check(t, config.DialectPostgres, map[string]config.TypeOverride{"uuid": {Type: "uuid.UUID", Import: lib.imp}}, []want{
					{"uuid", false, "uuid.UUID"},
					{"uuid", true, lib.nullable},
				})
			})
		}
	})
	t.Run("postgres uuid array", func(t *testing.T) {
		for _, lib := range uuidLibs {
			t.Run(lib.name, func(t *testing.T) {
				check(t, config.DialectPostgres, map[string]config.TypeOverride{"uuid": {Type: "uuid.UUID", Import: lib.imp}}, []want{
					{"uuid[]", false, "[]uuid.UUID"},
					{"uuid[]", true, "[]uuid.UUID"},
				})
			})
		}
	})
	t.Run("postgres numeric", func(t *testing.T) {
		check(t, config.DialectPostgres, decimalOn("numeric"), []want{
			{"numeric", false, "decimal.Decimal"},
			{"decimal", false, "decimal.Decimal"},
			{"numeric", true, "decimal.NullDecimal"},
			{"decimal", true, "decimal.NullDecimal"},
		})
	})
	t.Run("postgres numeric array", func(t *testing.T) {
		check(t, config.DialectPostgres, decimalOn("numeric"), []want{
			{"numeric[]", false, "[]decimal.Decimal"},
			{"decimal[]", false, "[]decimal.Decimal"},
			{"numeric[]", true, "[]decimal.Decimal"},
			{"decimal[]", true, "[]decimal.Decimal"},
		})
	})
	t.Run("mysql double", func(t *testing.T) {
		check(t, config.DialectMySQL, decimalOn("decimal"), []want{
			{"decimal", false, "decimal.Decimal"},
			{"decimal", true, "decimal.NullDecimal"},
			{"decimal(10,0)", false, "decimal.Decimal"},
			{"decimal(10,0)", true, "decimal.NullDecimal"},
			{"double", false, "float64"},
		})
	})
	t.Run("sqlite real", func(t *testing.T) {
		check(t, config.DialectSQLite, decimalOn("numeric"), []want{
			{"numeric", false, "decimal.Decimal"},
			{"decimal", false, "decimal.Decimal"},
			{"numeric", true, "decimal.NullDecimal"},
			{"decimal", true, "decimal.NullDecimal"},
			{"real", false, "float64"},
			{"float", false, "float64"},
			{"double", false, "float64"},
		})
	})
}

// TestFractionalSecondOverrideKey pins how a MySQL fractional-second column
// meets `overrides.types`: the key that keeps the column's precision wins,
// and the bare key still reaches the column when no precise entry exists.
func TestFractionalSecondOverrideKey(t *testing.T) {
	precise := config.TypeOverride{Type: "ov.Precise", Import: "example.com/ov"}
	bare := config.TypeOverride{Type: "ov.Bare", Import: "example.com/ov"}

	t.Run("precise key wins over the bare key", func(t *testing.T) {
		r := gotype.NewResolver(config.DialectMySQL, true, map[string]config.TypeOverride{"datetime(6)": precise, "datetime": bare})
		if got := r.Resolve("datetime(6)", false, "c", nil, nil); got.Name != "ov.Precise" {
			t.Errorf("Resolve(datetime(6)) Name = %q, want %q", got.Name, "ov.Precise")
		}
		if got := r.Resolve("datetime", false, "c", nil, nil); got.Name != "ov.Bare" {
			t.Errorf("Resolve(datetime) Name = %q, want %q", got.Name, "ov.Bare")
		}
	})

	t.Run("bare key reaches a column with precision", func(t *testing.T) {
		r := gotype.NewResolver(config.DialectMySQL, true, map[string]config.TypeOverride{"datetime": bare})
		if got := r.Resolve("datetime(6)", false, "c", nil, nil); got.Name != "ov.Bare" {
			t.Errorf("Resolve(datetime(6)) Name = %q, want %q", got.Name, "ov.Bare")
		}
	})

	t.Run("precise key misses a column without precision", func(t *testing.T) {
		r := gotype.NewResolver(config.DialectMySQL, true, map[string]config.TypeOverride{"datetime(6)": precise})
		if got := r.Resolve("datetime", false, "c", nil, nil); got.Name != "time.Time" {
			t.Errorf("Resolve(datetime) Name = %q, want %q", got.Name, "time.Time")
		}
	})
}
