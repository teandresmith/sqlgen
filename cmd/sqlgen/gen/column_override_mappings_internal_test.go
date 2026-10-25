package gen

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// columnOverrideMappings lists every spelling of every built-in mapping row,
// so each can be retyped per column. A row whose spellings are arrays is
// retyped with a slice literal.
var columnOverrideMappings = []struct {
	name      string
	dialect   config.Dialect
	spellings []string
	setType   bool
}{
	{name: "postgres text", dialect: config.DialectPostgres, spellings: []string{"text", "varchar", "char", "name"}},
	{name: "postgres int2", dialect: config.DialectPostgres, spellings: []string{"int2", "smallint"}},
	{name: "postgres int4", dialect: config.DialectPostgres, spellings: []string{"int4", "integer", "serial"}},
	{name: "postgres int8", dialect: config.DialectPostgres, spellings: []string{"int8", "bigint", "bigserial"}},
	{name: "postgres float4", dialect: config.DialectPostgres, spellings: []string{"float4", "real"}},
	{name: "postgres float8", dialect: config.DialectPostgres, spellings: []string{"float8", "double precision"}},
	{name: "postgres numeric", dialect: config.DialectPostgres, spellings: []string{"numeric", "decimal"}},
	{name: "postgres bool", dialect: config.DialectPostgres, spellings: []string{"bool", "boolean"}},
	{name: "postgres bytea", dialect: config.DialectPostgres, spellings: []string{"bytea"}},
	{name: "postgres timestamp", dialect: config.DialectPostgres, spellings: []string{"timestamp", "timestamptz"}},
	{name: "postgres date", dialect: config.DialectPostgres, spellings: []string{"date"}},
	{name: "postgres interval", dialect: config.DialectPostgres, spellings: []string{"interval"}},
	{name: "postgres uuid", dialect: config.DialectPostgres, spellings: []string{"uuid"}},
	{name: "postgres json", dialect: config.DialectPostgres, spellings: []string{"json", "jsonb"}},
	{name: "postgres inet", dialect: config.DialectPostgres, spellings: []string{"inet"}},
	{name: "postgres cidr", dialect: config.DialectPostgres, spellings: []string{"cidr"}},
	{name: "postgres macaddr", dialect: config.DialectPostgres, spellings: []string{"macaddr"}},
	{name: "postgres text array", dialect: config.DialectPostgres, spellings: []string{"text[]", "varchar[]"}},
	{name: "postgres int2 array", dialect: config.DialectPostgres, spellings: []string{"int2[]", "smallint[]"}},
	{name: "postgres int4 array", dialect: config.DialectPostgres, spellings: []string{"int4[]", "integer[]"}},
	{name: "postgres int8 array", dialect: config.DialectPostgres, spellings: []string{"int8[]", "bigint[]"}},
	{name: "postgres float4 array", dialect: config.DialectPostgres, spellings: []string{"float4[]", "real[]"}},
	{name: "postgres float8 array", dialect: config.DialectPostgres, spellings: []string{"float8[]", "double precision[]"}},
	{name: "postgres bool array", dialect: config.DialectPostgres, spellings: []string{"bool[]", "boolean[]"}},
	{name: "postgres uuid array", dialect: config.DialectPostgres, spellings: []string{"uuid[]"}},
	{name: "postgres numeric array", dialect: config.DialectPostgres, spellings: []string{"numeric[]", "decimal[]"}},
	{name: "postgres timestamp array", dialect: config.DialectPostgres, spellings: []string{"timestamp[]", "timestamptz[]"}},

	{name: "mysql tinyint", dialect: config.DialectMySQL, spellings: []string{"tinyint"}},
	{name: "mysql smallint", dialect: config.DialectMySQL, spellings: []string{"smallint"}},
	{name: "mysql int", dialect: config.DialectMySQL, spellings: []string{"int", "mediumint"}},
	{name: "mysql bigint", dialect: config.DialectMySQL, spellings: []string{"bigint"}},
	{name: "mysql tinyint unsigned", dialect: config.DialectMySQL, spellings: []string{"tinyint unsigned"}},
	{name: "mysql smallint unsigned", dialect: config.DialectMySQL, spellings: []string{"smallint unsigned"}},
	{name: "mysql int unsigned", dialect: config.DialectMySQL, spellings: []string{"int unsigned", "mediumint unsigned"}},
	{name: "mysql bigint unsigned", dialect: config.DialectMySQL, spellings: []string{"bigint unsigned"}},
	{name: "mysql float", dialect: config.DialectMySQL, spellings: []string{"float"}},
	{name: "mysql double", dialect: config.DialectMySQL, spellings: []string{"double", "decimal", "decimal(10,0)"}},
	{name: "mysql varchar", dialect: config.DialectMySQL, spellings: []string{"varchar", "text", "char", "enum"}},
	{name: "mysql set type", dialect: config.DialectMySQL, spellings: []string{"users__permissions_set"}, setType: true},
	{name: "mysql datetime", dialect: config.DialectMySQL, spellings: []string{"datetime", "timestamp"}},
	{name: "mysql date", dialect: config.DialectMySQL, spellings: []string{"date"}},
	{name: "mysql json", dialect: config.DialectMySQL, spellings: []string{"json"}},
	{name: "mysql blob", dialect: config.DialectMySQL, spellings: []string{"blob", "binary", "varbinary"}},
	{name: "mysql tinyint 1", dialect: config.DialectMySQL, spellings: []string{"tinyint(1)"}},
	{name: "mysql time", dialect: config.DialectMySQL, spellings: []string{"time"}},
	{name: "mysql year", dialect: config.DialectMySQL, spellings: []string{"year"}},
	{name: "mysql set string", dialect: config.DialectMySQL, spellings: []string{"set"}},
	{name: "mysql tinytext", dialect: config.DialectMySQL, spellings: []string{"tinytext", "mediumtext", "longtext"}},
	{name: "mysql tinyblob", dialect: config.DialectMySQL, spellings: []string{"tinyblob", "mediumblob", "longblob"}},
	{name: "mysql bit 1", dialect: config.DialectMySQL, spellings: []string{"bit(1)"}},
	{name: "mysql bit n", dialect: config.DialectMySQL, spellings: []string{"bit(2)", "bit(8)", "bit(64)"}},

	{name: "sqlite integer", dialect: config.DialectSQLite, spellings: []string{"integer", "int", "tinyint", "smallint", "bigint"}},
	{name: "sqlite text", dialect: config.DialectSQLite, spellings: []string{"text", "varchar", "char", "clob"}},
	{name: "sqlite real", dialect: config.DialectSQLite, spellings: []string{"real", "float", "double", "numeric", "decimal"}},
	{name: "sqlite blob", dialect: config.DialectSQLite, spellings: []string{"blob"}},
	{name: "sqlite boolean", dialect: config.DialectSQLite, spellings: []string{"boolean", "bool"}},
	{name: "sqlite datetime", dialect: config.DialectSQLite, spellings: []string{"datetime", "timestamp", "timestamptz", "date"}},
}

// TestColumnOverrideWinsForEverySQLType retypes a column of every spelling of
// every built-in mapping row, through `column_map.<col>.type` with its
// `.import` and through `type_map.<col>`. The literal wins over the built-in
// mapping; a nullable column takes `*T`, or bare `T` for a slice literal,
// which stays opaque rather than becoming an array column.
func TestColumnOverrideWinsForEverySQLType(t *testing.T) {
	for _, m := range columnOverrideMappings {
		t.Run(m.name, func(t *testing.T) {
			routes := []struct {
				name     string
				tableCfg func(literal string) config.TableConfig
				literal  string
				imp      string
			}{
				{
					name: "column_map",
					tableCfg: func(literal string) config.TableConfig {
						return config.TableConfig{ColumnMap: map[string]config.ColumnOverride{"c": {Type: literal, Import: "example.com/ov"}}}
					},
					literal: "ov.Custom",
					imp:     "example.com/ov",
				},
				{
					name: "type_map",
					tableCfg: func(literal string) config.TableConfig {
						return config.TableConfig{TypeMap: map[string]string{"c": literal}}
					},
					literal: "Custom",
				},
			}
			for _, route := range routes {
				t.Run(route.name, func(t *testing.T) {
					resolver := gotype.NewResolver(m.dialect, true, nil)
					if m.setType {
						resolver.RegisterSet("", m.spellings[0], "UsersPermissionsSet")
					}
					for _, sqlType := range m.spellings {
						literal := route.literal
						array := strings.HasSuffix(sqlType, "[]")
						if array {
							literal = "[]" + literal
						}
						for _, nullable := range []bool{false, true} {
							want := literal
							if nullable && !array {
								want = "*" + literal
							}
							col := &parser.Column{Name: "c", Type: sqlType, Nullable: nullable}
							got := resolveColumnGoType(resolver, col, route.tableCfg(literal), nil, nil)
							if got.Name != want || got.Import != route.imp {
								t.Errorf("%s %q (nullable=%v) = {%q, %q}, want {%q, %q}", route.name, sqlType, nullable, got.Name, got.Import, want, route.imp)
							}
							if got.IsSlice {
								t.Errorf("%s %q IsSlice = true, want an opaque literal", route.name, sqlType)
							}
						}
					}
				})
			}
		})
	}
}
