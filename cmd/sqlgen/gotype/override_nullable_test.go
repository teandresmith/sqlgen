package gotype_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// TestOverrideNullableFollowsUsePointers resolves a nullable column through an
// `overrides.types` entry that names no `nullable:` variant. Under
// use_pointers false a Go type with a database/sql null equivalent takes it;
// every other type keeps `*T` (bare when natively nilable). Under use_pointers
// true nothing changes, and an explicit `nullable:` wins at either setting.
func TestOverrideNullableFollowsUsePointers(t *testing.T) {
	type want struct {
		Name, Import, ZeroValue string
	}
	cases := []struct {
		name        string
		override    config.TypeOverride
		usePointers bool
		want        want
	}{
		{
			name:     "builtin string takes sql.NullString without pointers",
			override: config.TypeOverride{Type: "string"},
			want:     want{"sql.NullString", "database/sql", "sql.NullString{}"},
		},
		{
			name:        "builtin string takes a pointer with pointers",
			override:    config.TypeOverride{Type: "string"},
			usePointers: true,
			want:        want{"*string", "", "nil"},
		},
		{
			name:     "int8 takes sql.NullInt16 without pointers",
			override: config.TypeOverride{Type: "int8"},
			want:     want{"sql.NullInt16", "database/sql", "sql.NullInt16{}"},
		},
		{
			name:     "uint16 takes sql.NullInt32 without pointers",
			override: config.TypeOverride{Type: "uint16"},
			want:     want{"sql.NullInt32", "database/sql", "sql.NullInt32{}"},
		},
		{
			name:     "uint64 takes sql.NullInt64 without pointers",
			override: config.TypeOverride{Type: "uint64"},
			want:     want{"sql.NullInt64", "database/sql", "sql.NullInt64{}"},
		},
		{
			name:     "float32 takes sql.NullFloat64 without pointers",
			override: config.TypeOverride{Type: "float32"},
			want:     want{"sql.NullFloat64", "database/sql", "sql.NullFloat64{}"},
		},
		{
			name:     "bool takes sql.NullBool without pointers",
			override: config.TypeOverride{Type: "bool"},
			want:     want{"sql.NullBool", "database/sql", "sql.NullBool{}"},
		},
		{
			name:     "time.Time takes sql.NullTime without pointers",
			override: config.TypeOverride{Type: "time.Time", Import: "time"},
			want:     want{"sql.NullTime", "database/sql", "sql.NullTime{}"},
		},
		{
			name:     "custom type keeps a pointer without pointers",
			override: config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
			want:     want{"*ksuid.KSUID", "github.com/segmentio/ksuid", "nil"},
		},
		{
			name:        "custom type keeps a pointer with pointers",
			override:    config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
			usePointers: true,
			want:        want{"*ksuid.KSUID", "github.com/segmentio/ksuid", "nil"},
		},
		{
			name:     "natively nilable type stays bare without pointers",
			override: config.TypeOverride{Type: "json.RawMessage", Import: "encoding/json"},
			want:     want{"json.RawMessage", "encoding/json", "nil"},
		},
		{
			name:     "explicit nullable wins without pointers",
			override: config.TypeOverride{Type: "string", Nullable: config.NullableVariant{Type: "*string"}},
			want:     want{"*string", "", "nil"},
		},
		{
			name:        "explicit nullable wins with pointers",
			override:    config.TypeOverride{Type: "string", Nullable: config.NullableVariant{Type: "sql.NullString", Import: "database/sql"}},
			usePointers: true,
			want:        want{"sql.NullString", "database/sql", "sql.NullString{}"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scopes := []struct {
				name    string
				global  map[string]config.TypeOverride
				scoped  map[string]config.TypeOverride
				sqlType string
			}{
				{name: "global", global: map[string]config.TypeOverride{"citext": tc.override}, sqlType: "citext"},
				{name: "table scoped", scoped: map[string]config.TypeOverride{"citext": tc.override}, sqlType: "citext"},
			}
			for _, scope := range scopes {
				t.Run(scope.name, func(t *testing.T) {
					r := gotype.NewResolver(config.DialectPostgres, tc.usePointers, scope.global)
					gt := r.Resolve(scope.sqlType, true, "c", nil, scope.scoped)
					got := want{gt.Name, gt.Import, gt.ZeroValue}
					if diff := cmp.Diff(tc.want, got); diff != "" {
						t.Errorf("Resolve(%s, nullable) mismatch (-want +got):\n%s", scope.sqlType, diff)
					}
				})
			}
		})
	}
}

// TestOverrideNonNullableIgnoresUsePointers pins that a NOT NULL column keeps
// the override's own type at either use_pointers setting.
func TestOverrideNonNullableIgnoresUsePointers(t *testing.T) {
	cases := []struct {
		name        string
		usePointers bool
	}{
		{name: "with pointers", usePointers: true},
		{name: "without pointers", usePointers: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gotype.FromOverride(config.TypeOverride{Type: "string"}, false, tc.usePointers)
			if got.Name != "string" || got.Import != "" {
				t.Errorf("FromOverride(string, not null) = {%q, %q}, want {%q, %q}", got.Name, got.Import, "string", "")
			}
		})
	}
}

// TestTypeMapLiteralNullableIgnoresUsePointersWithOverride pins that a
// type_map literal still takes `*T` under use_pointers false even when an
// overrides.types entry exists for the column's SQL type.
func TestTypeMapLiteralNullableIgnoresUsePointersWithOverride(t *testing.T) {
	r := gotype.NewResolver(config.DialectPostgres, false, map[string]config.TypeOverride{"text": {Type: "string"}})
	got := r.Resolve("text", true, "c", map[string]string{"c": "string"}, nil)
	if got.Name != "*string" {
		t.Errorf("Resolve(text, nullable) with type_map string: Name = %q, want %q", got.Name, "*string")
	}
}
