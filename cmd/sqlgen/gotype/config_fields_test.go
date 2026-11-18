package gotype_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// TestResolve_NullableShorthandSharesParentImport pins that the shorthand
// `nullable: <Type>` takes the parent override's import, at global and table
// scope.
func TestResolve_NullableShorthandSharesParentImport(t *testing.T) {
	override := config.TypeOverride{
		Type:     "ksuid.KSUID",
		Import:   "github.com/segmentio/ksuid",
		Nullable: config.NullableVariant{Type: "ksuid.NullKSUID"},
	}
	tests := []struct {
		name  string
		build func() gotype.GoType
	}{
		{
			name: "global override",
			build: func() gotype.GoType {
				r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{"varchar": override})
				return r.Resolve("varchar", true, "external_id", nil, nil)
			},
		},
		{
			name: "table override",
			build: func() gotype.GoType {
				r := gotype.NewResolver("postgres", true, nil)
				return r.Resolve("varchar", true, "external_id", nil, map[string]config.TypeOverride{"varchar": override})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.build()
			if got.Name != "ksuid.NullKSUID" || got.Import != "github.com/segmentio/ksuid" {
				t.Errorf("Resolve(varchar, nullable) = (%q, %q), want (%q, %q)", got.Name, got.Import, "ksuid.NullKSUID", "github.com/segmentio/ksuid")
			}
		})
	}
}

// TestResolve_OverrideZeroValueDerived pins the zero value an override gets
// with zero_value unset: Type{} for a struct, "" for a string, 0 for a number.
func TestResolve_OverrideZeroValueDerived(t *testing.T) {
	tests := []struct {
		name     string
		override config.TypeOverride
		want     string
	}{
		{
			name:     "struct type",
			override: config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"},
			want:     "ksuid.KSUID{}",
		},
		{
			name:     "same-package struct type",
			override: config.TypeOverride{Type: "Money"},
			want:     "Money{}",
		},
		{name: "string", override: config.TypeOverride{Type: "string"}, want: `""`},
		{name: "int64", override: config.TypeOverride{Type: "int64"}, want: "0"},
		{name: "float64", override: config.TypeOverride{Type: "float64"}, want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gotype.NewResolver("postgres", true, map[string]config.TypeOverride{"numeric": tt.override})
			got := r.Resolve("numeric", false, "amount", nil, nil)
			if got.Name != tt.override.Type {
				t.Fatalf("Resolve(numeric) type = %q, want the override %q", got.Name, tt.override.Type)
			}
			if got.ZeroValue != tt.want {
				t.Errorf("Resolve(numeric) zero value = %q, want %q for %s", got.ZeroValue, tt.want, tt.override.Type)
			}
		})
	}
}
