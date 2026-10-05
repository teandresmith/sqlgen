package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// `overrides.use_pointers: false` resolves every nullable column of a
// nullSQL type to a database/sql wrapper. Two things broke, on every dialect,
// with or without the API — and nothing caught either, because no example
// module sets the flag and `sqlgen generate` never compiles what it writes
// unless api.graphql chains gqlgen.
//
//  1. The generated file imports database/sql, which binds the file-local
//     package name `sql` that sqlgen's own runtime package
//     (github.com/teandresmith/sqlgen/sql) needs. goimports will not add a second
//     `sql`, so every sql.Dialect / sql.Table / sql.Sort reference silently
//     resolved against database/sql and the package did not compile.
//  2. The comparator was picked from the wrapper rather than from the type it
//     wraps, so a nullable numeric column asked for
//     comparator.NullableNumber[sql.NullFloat64] — which does not satisfy
//     comparator.Numeric — and a nullable boolean or timestamp fell through to
//     a string comparator.

// TestStdNullWrappers_generatedFileImportsBothSQLPackages is the pin for (1).
// The body references both packages by the same qualifier, which is exactly
// what a table file does under use_pointers: false — the row struct carries
// sql.NullString and the table client carries sql.Dialect.
func TestStdNullWrappers_generatedFileImportsBothSQLPackages(t *testing.T) {
	body := []byte(`
type row struct {
	Note sql.NullString
}

type client struct {
	dialect sql.Dialect
	table   sql.Table
	sorts   []sql.Sort
}
`)

	out, err := gen.Format(gen.WrapWithPreamble("models", []string{"database/sql"}, body), "test", "models_gen.go")
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, `stdsql "database/sql"`) {
		t.Errorf("database/sql is not aliased; generated file:\n%s", got)
	}
	if !strings.Contains(got, `"github.com/teandresmith/sqlgen/sql"`) {
		t.Errorf("the sqlgen runtime sql package is missing — every sql.Dialect reference would be undefined; generated file:\n%s", got)
	}
	if !strings.Contains(got, "Note stdsql.NullString") {
		t.Errorf("the database/sql qualifier was not rewritten to the alias; generated file:\n%s", got)
	}
	// The runtime package keeps the bare qualifier: it is the one the
	// templates spell, at 200-odd sites.
	for _, want := range []string{"dialect sql.Dialect", "table   sql.Table", "sorts   []sql.Sort"} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime qualifier %q was rewritten; generated file:\n%s", want, got)
		}
	}
}

// TestStdNullWrappers_fileWithoutDatabaseSQLIsUntouched is why every existing
// golden is byte-identical: the alias appears only in a file that imports
// database/sql, and no example resolves a column to a wrapper.
func TestStdNullWrappers_fileWithoutDatabaseSQLIsUntouched(t *testing.T) {
	body := []byte(`
type client struct {
	dialect sql.Dialect
}
`)

	out, err := gen.Format(gen.WrapWithPreamble("models", nil, body), "test", "models_gen.go")
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	got := string(out)

	if strings.Contains(got, "stdsql") {
		t.Errorf("alias leaked into a file that needs no database/sql; generated file:\n%s", got)
	}
	if !strings.Contains(got, `"github.com/teandresmith/sqlgen/sql"`) {
		t.Errorf("runtime sql import missing; generated file:\n%s", got)
	}
}

// TestStdNullWrappers_comparatorClassifiesByUnderlyingType is the pin for (2).
// The `want` column is the same comparator the identical column resolves to
// under use_pointers: true (*int32, *bool, *time.Time) — the storage shape
// changes, the comparison does not.
func TestStdNullWrappers_comparatorClassifiesByUnderlyingType(t *testing.T) {
	tests := []struct {
		name string
		col  gen.ColumnContext
		want string
	}{
		{"sql.NullInt16", gen.ColumnContext{GoType: "sql.NullInt16", SQLType: "smallint", Nullable: true}, "*comparator.NullableNumber[int16]"},
		{"sql.NullInt32", gen.ColumnContext{GoType: "sql.NullInt32", SQLType: "integer", Nullable: true}, "*comparator.NullableNumber[int32]"},
		{"sql.NullInt64", gen.ColumnContext{GoType: "sql.NullInt64", SQLType: "bigint", Nullable: true}, "*comparator.NullableNumber[int64]"},
		{"sql.NullFloat64", gen.ColumnContext{GoType: "sql.NullFloat64", SQLType: "double precision", Nullable: true}, "*comparator.NullableNumber[float64]"},
		{"sql.NullString", gen.ColumnContext{GoType: "sql.NullString", SQLType: "text", Nullable: true}, "*comparator.NullableString"},
		{"sql.NullBool", gen.ColumnContext{GoType: "sql.NullBool", SQLType: "boolean", Nullable: true}, "*comparator.NullableBool"},
		{"sql.NullTime", gen.ColumnContext{GoType: "sql.NullTime", SQLType: "timestamptz", Nullable: true}, "*comparator.NullableTime"},
	}

	comparatorType := gen.FuncMap(dialectFor("postgres"))["comparatorType"].(func(gen.ColumnContext) string)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := comparatorType(tt.col); got != tt.want {
				t.Errorf("comparatorType(%s) = %q, want %q", tt.col.GoType, got, tt.want)
			}
		})
	}
}

// TestStdNullUnderlying pins the wrapper → value-field-type table. It is NOT
// the inverse of gotype's SQL→Go mappings: `smallint` and `tinyint` both
// resolve to sql.NullInt16 and `real` resolves to sql.NullFloat64, so that
// inverse is ambiguous. What a generic constraint needs is the type the
// wrapper's own field carries.
func TestStdNullUnderlying(t *testing.T) {
	want := map[string]string{
		"sql.NullString":  "string",
		"sql.NullBool":    "bool",
		"sql.NullByte":    "byte",
		"sql.NullInt16":   "int16",
		"sql.NullInt32":   "int32",
		"sql.NullInt64":   "int64",
		"sql.NullFloat64": "float64",
		"sql.NullTime":    "time.Time",
	}
	for wrapper, underlying := range want {
		got, ok := gotype.StdNullUnderlying(wrapper)
		if !ok {
			t.Errorf("StdNullUnderlying(%q): not recognized", wrapper)
			continue
		}
		if got != underlying {
			t.Errorf("StdNullUnderlying(%q) = %q, want %q", wrapper, got, underlying)
		}
	}

	// Every wrapper gotype can produce for a nullSQL column must be in the
	// table, or a column of that type classifies by its wrapper again.
	for _, notWrapper := range []string{"string", "*int32", "uuid.NullUUID", "decimal.NullDecimal", "types.NullDateTime"} {
		if _, ok := gotype.StdNullUnderlying(notWrapper); ok {
			t.Errorf("StdNullUnderlying(%q): reported as a database/sql wrapper", notWrapper)
		}
	}
}
