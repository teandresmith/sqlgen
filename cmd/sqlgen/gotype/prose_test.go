package gotype_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

const proseTypesImport = "github.com/teandresmith/sqlgen/types"

// proseScopes places an overrides.types map at the global or the table scope
// and returns the resolver and the table-scoped map to resolve with.
var proseScopes = []struct {
	name  string
	build func(dialect config.Dialect, usePointers bool, overrides map[string]config.TypeOverride) (*gotype.Resolver, map[string]config.TypeOverride)
}{
	{"global", func(dialect config.Dialect, usePointers bool, overrides map[string]config.TypeOverride) (*gotype.Resolver, map[string]config.TypeOverride) {
		return gotype.NewResolver(dialect, usePointers, overrides), nil
	}},
	{"table", func(dialect config.Dialect, usePointers bool, overrides map[string]config.TypeOverride) (*gotype.Resolver, map[string]config.TypeOverride) {
		return gotype.NewResolver(dialect, usePointers, nil), overrides
	}},
}

func proseOverride(name string) config.TypeOverride {
	return config.TypeOverride{Type: name, Import: "example.com/ov"}
}

// TestNonIntegerUnsignedOverrideKey looks a non-integer `… unsigned` column up
// as written, then as `<base> unsigned`, then as the signed base.
func TestNonIntegerUnsignedOverrideKey(t *testing.T) {
	const column = "double unsigned zerofill"
	dialects := []struct {
		name    string
		dialect config.Dialect
	}{
		{"mysql", config.DialectMySQL},
		{"sqlite", config.DialectSQLite},
	}
	cases := []struct {
		name      string
		overrides map[string]config.TypeOverride
		want      string
	}{
		{
			name: "as written wins over the unsigned base",
			overrides: map[string]config.TypeOverride{
				"double unsigned zerofill": proseOverride("ov.Written"),
				"double unsigned":          proseOverride("ov.Unsigned"),
				"double":                   proseOverride("ov.Signed"),
			},
			want: "ov.Written",
		},
		{
			name: "unsigned base wins over the signed base",
			overrides: map[string]config.TypeOverride{
				"double unsigned": proseOverride("ov.Unsigned"),
				"double":          proseOverride("ov.Signed"),
			},
			want: "ov.Unsigned",
		},
		{
			name: "signed base reached when no unsigned entry exists",
			overrides: map[string]config.TypeOverride{
				"double": proseOverride("ov.Signed"),
			},
			want: "ov.Signed",
		},
	}
	for _, d := range dialects {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := gotype.NewResolver(d.dialect, true, tc.overrides)
					if got := r.Resolve(column, false, "c", nil, nil); got.Name != tc.want {
						t.Errorf("Resolve(%q) Name = %q, want %q", column, got.Name, tc.want)
					}
				})
			}
		})
	}
}

// TestEnumArrayOverrideKeys pins how `overrides.types` meets a schema enum's
// array column: the enum's own key does not reach it, the bare `<enum>[]` key
// retypes it.
func TestEnumArrayOverrideKeys(t *testing.T) {
	newResolver := func(t *testing.T, scope int, overrides map[string]config.TypeOverride) (*gotype.Resolver, map[string]config.TypeOverride) {
		t.Helper()
		r, table := proseScopes[scope].build(config.DialectPostgres, true, overrides)
		r.RegisterEnum("", "user_role", "UserRole")
		return r, table
	}

	t.Run("enum key does not reach the enum array", func(t *testing.T) {
		for i, scope := range proseScopes {
			t.Run(scope.name, func(t *testing.T) {
				r, table := newResolver(t, i, map[string]config.TypeOverride{"user_role": proseOverride("ov.Role")})
				if got := r.Resolve("user_role", false, "c", nil, table); got.Name != "ov.Role" {
					t.Fatalf("Resolve(user_role) Name = %q, want %q (override must reach the scalar column)", got.Name, "ov.Role")
				}
				for _, nullable := range []bool{false, true} {
					if got := r.Resolve("user_role[]", nullable, "c", nil, table); got.Name != "UserRoleSlice" {
						t.Errorf("Resolve(user_role[], nullable=%v) Name = %q, want %q", nullable, got.Name, "UserRoleSlice")
					}
				}
			})
		}
	})

	t.Run("bare enum array key retypes the enum array", func(t *testing.T) {
		for i, scope := range proseScopes {
			t.Run(scope.name, func(t *testing.T) {
				r, table := newResolver(t, i, map[string]config.TypeOverride{"user_role[]": proseOverride("ov.Roles")})
				got := r.Resolve("user_role[]", false, "c", nil, table)
				if got.Name != "ov.Roles" || got.Import != "example.com/ov" {
					t.Errorf("Resolve(user_role[]) = {%q, %q}, want {%q, %q}", got.Name, got.Import, "ov.Roles", "example.com/ov")
				}
			})
		}
	})
}

// TestMySQLSynonymOverrideKeysMiss keys `overrides.types` by a synonym MySQL
// never stores; the column of the stored form keeps its built-in mapping.
func TestMySQLSynonymOverrideKeysMiss(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		column string
		want   string
	}{
		{"boolean", "boolean", "tinyint(1)", "bool"},
		{"integer", "integer", "int", "int32"},
		{"numeric", "numeric", "decimal(10,0)", "float64"},
		{"real", "real", "double", "float64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, scope := range proseScopes {
				r, table := scope.build(config.DialectMySQL, true, map[string]config.TypeOverride{tc.key: proseOverride("ov.Synonym")})
				if got := r.Resolve(tc.column, false, "c", nil, table); got.Name != tc.want {
					t.Errorf("%s key %q: Resolve(%q) Name = %q, want %q", scope.name, tc.key, tc.column, got.Name, tc.want)
				}
			}
		})
	}
}

// TestUUIDChoiceLeavesOtherDialectsAlone selects each UUID integration and
// resolves every MySQL and SQLite built-in spelling.
func TestUUIDChoiceLeavesOtherDialectsAlone(t *testing.T) {
	libs := []struct {
		name string
		imp  string
	}{
		{"standard library", uuidstd.ImportPath},
		{"google", uuidgoogle.ImportPath},
		{"gofrs", uuidgofrs.ImportPath},
	}
	for _, lib := range libs {
		t.Run(lib.name, func(t *testing.T) {
			global := map[string]config.TypeOverride{"uuid": {Type: "uuid.UUID", Import: lib.imp}}
			for _, m := range builtInMappings {
				if m.dialect == config.DialectPostgres {
					continue
				}
				cases := []struct {
					nullable    bool
					usePointers bool
					want        string
				}{
					{false, true, m.goType},
					{false, false, m.goType},
					{true, true, m.nullPtr},
					{true, false, m.nullNoPtr},
				}
				for _, tc := range cases {
					r := mappingResolver(m, tc.usePointers, global)
					for _, sqlType := range m.spellings {
						if got := r.Resolve(sqlType, tc.nullable, "c", nil, nil); got.Name != tc.want {
							t.Errorf("%s: Resolve(%q, nullable=%v, use_pointers=%v) Name = %q, want %q", m.name, sqlType, tc.nullable, tc.usePointers, got.Name, tc.want)
						}
					}
				}
			}
		})
	}
}

// TestUUIDWrapperNullablePointerNamed names `*uuid.UUID` as the nullable
// variant of a library that ships uuid.NullUUID.
func TestUUIDWrapperNullablePointerNamed(t *testing.T) {
	libs := []struct {
		name string
		imp  string
	}{
		{"google", uuidgoogle.ImportPath},
		{"gofrs", uuidgofrs.ImportPath},
	}
	for _, lib := range libs {
		t.Run(lib.name, func(t *testing.T) {
			for _, usePointers := range []bool{true, false} {
				r := gotype.NewResolver(config.DialectPostgres, usePointers, map[string]config.TypeOverride{
					"uuid": {Type: "uuid.UUID", Import: lib.imp, Nullable: config.NullableVariant{Type: "*uuid.UUID"}},
				})
				if got := r.Resolve("uuid", true, "c", nil, nil); got.Name != "*uuid.UUID" {
					t.Errorf("Resolve(uuid, nullable, use_pointers=%v) Name = %q, want %q", usePointers, got.Name, "*uuid.UUID")
				}
			}
		})
	}
}

// TestDeriveScalarExtraction_BuiltinWinsOverUserForDecimalAndDateTime points
// a user override's nullable variant at decimal.NullDecimal and
// types.NullDateTime with wrong extraction fields; the canonical unwrap and
// Valid guard still win.
func TestDeriveScalarExtraction_BuiltinWinsOverUserForDecimalAndDateTime(t *testing.T) {
	cases := []struct {
		name       string
		dialect    config.Dialect
		sqlType    string
		override   config.TypeOverride
		wantUnwrap string
	}{
		{
			name:    "decimal.NullDecimal",
			dialect: config.DialectPostgres,
			sqlType: "numeric",
			override: config.TypeOverride{
				Type:   "decimal.Decimal",
				Import: decimal.ImportPath,
				Nullable: config.NullableVariant{
					Type:            "decimal.NullDecimal",
					UnderlyingField: "Bytes",
					ValidField:      "OK",
				},
			},
			wantUnwrap: "$v.Decimal",
		},
		{
			name:    "types.NullDateTime",
			dialect: config.DialectSQLite,
			sqlType: "datetime",
			override: config.TypeOverride{
				Type:   "types.DateTime",
				Import: proseTypesImport,
				Nullable: config.NullableVariant{
					Type:            "types.NullDateTime",
					UnderlyingField: "Stamp",
					ValidMethod:     "IsSet",
				},
			},
			wantUnwrap: "$v.Time",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gotype.NewResolver(tc.dialect, true, map[string]config.TypeOverride{tc.sqlType: tc.override})
			got := r.DeriveScalarExtraction(tc.override.Nullable.Type)
			if got.GuardExpr != "$v.Valid" {
				t.Errorf("GuardExpr = %q, want %q (built-in must win)", got.GuardExpr, "$v.Valid")
			}
			if got.UnwrapExpr != tc.wantUnwrap {
				t.Errorf("UnwrapExpr = %q, want %q (built-in must win)", got.UnwrapExpr, tc.wantUnwrap)
			}
		})
	}
}

// TestTableScopedOverrideFKMetadata resolves a nullable column through a
// google UUID override written at each scope and derives the FK extraction
// of the result; the table scope matches the global one.
func TestTableScopedOverrideFKMetadata(t *testing.T) {
	cases := []struct {
		name     string
		override config.TypeOverride
	}{
		{"nullable omitted", config.TypeOverride{Type: "uuid.UUID", Import: uuidgoogle.ImportPath}},
		{"nullable shorthand", config.TypeOverride{Type: "uuid.UUID", Import: uuidgoogle.ImportPath, Nullable: config.NullableVariant{Type: "uuid.NullUUID"}}},
	}
	want := gotype.ScalarExtraction{GuardExpr: "$v.Valid", UnwrapExpr: "$v.UUID", StringMethod: gotype.FKStringStringer}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var global gotype.ScalarExtraction
			for i, scope := range proseScopes {
				r, table := scope.build(config.DialectPostgres, true, map[string]config.TypeOverride{"uuid": tc.override})
				gt := r.Resolve("uuid", true, "c", nil, table)
				if gt.Name != "uuid.NullUUID" {
					t.Errorf("%s: Resolve(uuid, nullable) Name = %q, want %q", scope.name, gt.Name, "uuid.NullUUID")
				}
				got := r.DeriveScalarExtraction(gt.Name)
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("%s: DeriveScalarExtraction(%q) (-want +got):\n%s", scope.name, gt.Name, diff)
				}
				if i == 0 {
					global = got
				} else if diff := cmp.Diff(global, got); diff != "" {
					t.Errorf("table-scoped extraction differs from global (-global +table):\n%s", diff)
				}
			}
		})
	}
}

// TestTableActivationBehindTableExplicit activates the decimal integration
// at the table scope through its `numeric` key next to an explicit
// table-scoped `decimal` entry; the explicit entry wins.
func TestTableActivationBehindTableExplicit(t *testing.T) {
	r := gotype.NewResolver(config.DialectPostgres, true, nil)
	table := map[string]config.TypeOverride{
		"numeric": {Type: "decimal.Decimal", Import: decimal.ImportPath},
		"decimal": proseOverride("ov.Explicit"),
	}
	if got := r.Resolve("decimal", false, "c", nil, table); got.Name != "ov.Explicit" || got.Import != "example.com/ov" {
		t.Errorf("Resolve(decimal) = {%q, %q}, want {%q, %q}", got.Name, got.Import, "ov.Explicit", "example.com/ov")
	}
	if got := r.Resolve("numeric", false, "c", nil, table); got.Name != "decimal.Decimal" {
		t.Errorf("Resolve(numeric) Name = %q, want %q", got.Name, "decimal.Decimal")
	}
}

// TestCustomOverrideZeroValue sets a custom type override's zero value.
func TestCustomOverrideZeroValue(t *testing.T) {
	override := config.TypeOverride{Type: "ov.Custom", Import: "example.com/ov", ZeroValue: "ov.Empty"}
	for _, scope := range proseScopes {
		t.Run(scope.name, func(t *testing.T) {
			r, table := scope.build(config.DialectPostgres, true, map[string]config.TypeOverride{"text": override})
			got := r.Resolve("text", false, "c", nil, table)
			if got.Name != "ov.Custom" || got.ZeroValue != "ov.Empty" {
				t.Errorf("Resolve(text) = {Name %q, ZeroValue %q}, want {Name %q, ZeroValue %q}", got.Name, got.ZeroValue, "ov.Custom", "ov.Empty")
			}
		})
	}
}

// TestCustomWrapperValidFieldDefault declares a user wrapper with an
// underlying field and no validity shape; the guard defaults to `Valid`.
func TestCustomWrapperValidFieldDefault(t *testing.T) {
	r := gotype.NewResolver(config.DialectPostgres, true, map[string]config.TypeOverride{
		"text": {
			Type:   "ov.Str",
			Import: "example.com/ov",
			Nullable: config.NullableVariant{
				Type:            "ov.NullStr",
				UnderlyingField: "Str",
			},
		},
	})
	got := r.DeriveScalarExtraction("ov.NullStr")
	if got.GuardExpr != "$v.Valid" {
		t.Errorf("GuardExpr = %q, want %q", got.GuardExpr, "$v.Valid")
	}
	if got.UnwrapExpr != "$v.Str" {
		t.Errorf("UnwrapExpr = %q, want %q", got.UnwrapExpr, "$v.Str")
	}
}

// TestJSONBArraySlice resolves PostgreSQL's jsonb[] to a slice of types.JSON.
func TestJSONBArraySlice(t *testing.T) {
	cases := []struct {
		name        string
		nullable    bool
		usePointers bool
	}{
		{"not null with pointers", false, true},
		{"not null without pointers", false, false},
		{"nullable with pointers", true, true},
		{"nullable without pointers", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gotype.NewResolver(config.DialectPostgres, tc.usePointers, nil)
			got := r.Resolve("jsonb[]", tc.nullable, "c", nil, nil)
			if got.Name != "[]types.JSON" || !got.IsSlice {
				t.Errorf("Resolve(jsonb[], nullable=%v) = {Name %q, IsSlice %v}, want {Name %q, IsSlice true}", tc.nullable, got.Name, got.IsSlice, "[]types.JSON")
			}
		})
	}
}

// TestNamedNullTypeOverride names sql.NullString as an override's nullable
// variant in the full form.
func TestNamedNullTypeOverride(t *testing.T) {
	override := config.TypeOverride{
		Type:     "string",
		Nullable: config.NullableVariant{Type: "sql.NullString", Import: "database/sql"},
	}
	for _, scope := range proseScopes {
		t.Run(scope.name, func(t *testing.T) {
			for _, usePointers := range []bool{true, false} {
				r, table := scope.build(config.DialectPostgres, usePointers, map[string]config.TypeOverride{"text": override})
				got := r.Resolve("text", true, "c", nil, table)
				if got.Name != "sql.NullString" || got.Import != "database/sql" {
					t.Errorf("Resolve(text, nullable, use_pointers=%v) = {%q, %q}, want {%q, %q}", usePointers, got.Name, got.Import, "sql.NullString", "database/sql")
				}
			}
		})
	}
}

// TestTypeMapNullableIgnoresUsePointers resolves a nullable column through a
// type_map entry with use_pointers off: a value type takes `*T`, a natively
// nilable type stays bare.
func TestTypeMapNullableIgnoresUsePointers(t *testing.T) {
	cases := []struct {
		name    string
		literal string
		want    string
	}{
		{"value type takes a pointer", "int64", "*int64"},
		{"string takes a pointer", "string", "*string"},
		{"slice stays bare", "[]string", "[]string"},
		{"stdlib slice alias stays bare", "json.RawMessage", "json.RawMessage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gotype.NewResolver(config.DialectPostgres, false, nil)
			got := r.Resolve("text", true, "c", map[string]string{"c": tc.literal}, nil)
			if got.Name != tc.want {
				t.Errorf("Resolve(text, nullable, use_pointers=false) with type_map %q: Name = %q, want %q", tc.literal, got.Name, tc.want)
			}
		})
	}
}

// TestUUIDWrapperNullableExplicitNamed pins that a google or gofrs override
// naming `nullable: uuid.NullUUID` gives a nullable `uuid` column the wrapper
// at either `use_pointers` setting.
func TestUUIDWrapperNullableExplicitNamed(t *testing.T) {
	libs := []struct {
		name string
		imp  string
	}{
		{"google", uuidgoogle.ImportPath},
		{"gofrs", uuidgofrs.ImportPath},
	}
	for _, lib := range libs {
		t.Run(lib.name, func(t *testing.T) {
			for _, usePointers := range []bool{true, false} {
				r := gotype.NewResolver(config.DialectPostgres, usePointers, map[string]config.TypeOverride{
					"uuid": {Type: "uuid.UUID", Import: lib.imp, Nullable: config.NullableVariant{Type: "uuid.NullUUID"}},
				})
				if got := r.Resolve("uuid", true, "c", nil, nil); got.Name != "uuid.NullUUID" || got.Import != lib.imp {
					t.Errorf("use_pointers=%v: Resolve(uuid, nullable) = {%q, %q}, want {%q, %q}", usePointers, got.Name, got.Import, "uuid.NullUUID", lib.imp)
				}
			}
		})
	}
}
