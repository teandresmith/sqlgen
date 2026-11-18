package gotype_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
)

// TestFromOverrideKnownLiteralImport pins that an override naming a known
// literal with no `import` takes the literal's import, so the generated file
// does not depend on goimports finding the package in the module cache. An
// explicit import always wins.
func TestFromOverrideKnownLiteralImport(t *testing.T) {
	const typesPkg = "github.com/teandresmith/sqlgen/types"
	cases := []struct {
		name       string
		override   config.TypeOverride
		nullable   bool
		wantName   string
		wantImport string
	}{
		{
			name:       "types.JSON",
			override:   config.TypeOverride{Type: "types.JSON"},
			wantName:   "types.JSON",
			wantImport: typesPkg,
		},
		{
			name:       "json.RawMessage",
			override:   config.TypeOverride{Type: "json.RawMessage"},
			wantName:   "json.RawMessage",
			wantImport: "encoding/json",
		},
		{
			name:       "net.IP",
			override:   config.TypeOverride{Type: "net.IP"},
			wantName:   "net.IP",
			wantImport: "net",
		},
		{
			name:       "nullable types.JSON keeps the bare type",
			override:   config.TypeOverride{Type: "types.JSON"},
			nullable:   true,
			wantName:   "types.JSON",
			wantImport: typesPkg,
		},
		{
			name:       "explicit import wins",
			override:   config.TypeOverride{Type: "types.JSON", Import: "example.com/fork/types"},
			wantName:   "types.JSON",
			wantImport: "example.com/fork/types",
		},
		{
			name:       "unknown literal stays without import",
			override:   config.TypeOverride{Type: "Address"},
			wantName:   "Address",
			wantImport: "",
		},
		{
			name: "nullable variant naming a known literal",
			override: config.TypeOverride{
				Type:     "string",
				Nullable: config.NullableVariant{Type: "types.JSON"},
			},
			nullable:   true,
			wantName:   "types.JSON",
			wantImport: typesPkg,
		},
		{
			name: "nullable variant inherits the parent's explicit import",
			override: config.TypeOverride{
				Type:     "types.JSON",
				Import:   "example.com/fork/types",
				Nullable: config.NullableVariant{Type: "types.JSON"},
			},
			nullable:   true,
			wantName:   "types.JSON",
			wantImport: "example.com/fork/types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gotype.FromOverride(tc.override, tc.nullable, true)
			if got.Name != tc.wantName || got.Import != tc.wantImport {
				t.Errorf("FromOverride() = {%q, %q}, want {%q, %q}", got.Name, got.Import, tc.wantName, tc.wantImport)
			}
		})
	}
}
