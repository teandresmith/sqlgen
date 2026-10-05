package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// scalarConfig returns a valid config carrying exactly the given
// api.graphql.scalars map, so a test case's only variable is the binding.
func scalarConfig(scalars map[string]config.ScalarBinding) *config.RootConfig {
	cfg := validConfig()
	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{
			Enabled:     true,
			SchemaDir:   "graph",
			ResolverDir: "graph",
			Package:     "graph",
			FieldCasing: config.FieldCasingCamel,
			Scalars:     scalars,
		},
	}
	return cfg
}

// TestValidatePreParse_ScalarBinding pins the §26.4.1 rules on one
// `api.graphql.scalars` entry. The map declares scalars the consumer
// owns the marshaling for, and each `marshaling:` mode carries a different
// obligation — `external` needs a marshaler_package to anchor gqlgen's
// discovery at, `method` needs go_type's full import path to bind to, and
// `builtin` needs neither.
func TestValidatePreParse_ScalarBinding(t *testing.T) {
	tests := []struct {
		name     string
		binding  config.ScalarBinding
		asName   string // scalar key; defaults to "IPAddr"
		wantErr  bool
		wantFrag string
	}{
		{
			name:    "external with marshaler_package",
			binding: config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
		},
		{
			name:    "method with fully-qualified go_type",
			binding: config.ScalarBinding{GoType: "example.com/types.Email", Marshaling: config.ScalarMarshalingMethod},
		},
		{
			// Still legal on a name gqlgen does not bundle: with no go_type
			// the entry binds nothing, so there is nothing to be wrong about
			// — it reaches no column, declares no `scalar` and emits no
			// models: entry. It is inert rather than diagnosed, since
			// unusedScalarWarnings skips an empty go_type too.
			name:    "builtin needs no go_type",
			binding: config.ScalarBinding{Marshaling: config.ScalarMarshalingBuiltin},
		},
		{
			name:     "builtin naming a go_type on a scalar gqlgen does not bundle",
			binding:  config.ScalarBinding{GoType: "int64", Marshaling: config.ScalarMarshalingBuiltin},
			wantErr:  true,
			wantFrag: `gqlgen bundles none for "IPAddr"`,
		},
		{
			name:     "builtin on a bundled scalar its marshaler cannot carry",
			binding:  config.ScalarBinding{GoType: "example.com/types.Email", Marshaling: config.ScalarMarshalingBuiltin},
			asName:   "Int64",
			wantErr:  true,
			wantFrag: `which gqlgen's bundled Int64 marshaler does not carry (it carries int / int64)`,
		},
		{
			name:    "builtin on a bundled scalar naming the narrower Go type",
			binding: config.ScalarBinding{GoType: "int", Marshaling: config.ScalarMarshalingBuiltin},
			asName:  "Int64",
		},
		{
			// The one bundled key that is package-qualified, so the only one
			// where the table lookup depends on QualifiedGoType collapsing
			// the full import path to `graphql.Upload`. The other keys are
			// unqualified (`int64`, `any`) or registry-owned (`time.Time`).
			name:    "builtin on the package-qualified Upload key",
			binding: config.ScalarBinding{GoType: "github.com/99designs/gqlgen/graphql.Upload", Marshaling: config.ScalarMarshalingBuiltin},
			asName:  "Upload",
		},
		{
			name:    "builtin accepts the interface{} spelling of any",
			binding: config.ScalarBinding{GoType: "interface{}", Marshaling: config.ScalarMarshalingBuiltin},
			asName:  "Any",
		},
		{
			name:    "builtin accepts the interface{} spelling inside a composite",
			binding: config.ScalarBinding{GoType: "map[string]interface{}", Marshaling: config.ScalarMarshalingBuiltin},
			asName:  "Map",
		},
		{
			name:     "external without marshaler_package",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal},
			wantErr:  true,
			wantFrag: `marshaler_package: required when marshaling is "external"`,
		},
		{
			name:     "marshaler_package on method",
			binding:  config.ScalarBinding{GoType: "example.com/types.Email", Marshaling: config.ScalarMarshalingMethod, MarshalerPackage: "example.com/app/gqlscalars"},
			wantErr:  true,
			wantFrag: `marshaler_package: only valid when marshaling is "external"`,
		},
		{
			name:     "marshaler_package on builtin",
			binding:  config.ScalarBinding{Marshaling: config.ScalarMarshalingBuiltin, MarshalerPackage: "example.com/app/gqlscalars"},
			wantErr:  true,
			wantFrag: `marshaler_package: only valid when marshaling is "external"`,
		},
		{
			name:     "method with unqualified go_type",
			binding:  config.ScalarBinding{GoType: "Email", Marshaling: config.ScalarMarshalingMethod},
			wantErr:  true,
			wantFrag: `go_type: "Email" has no package qualifier`,
		},
		{
			name:     "go_type owned by the built-in registry",
			binding:  config.ScalarBinding{GoType: "github.com/shopspring/decimal.Decimal", Marshaling: config.ScalarMarshalingMethod},
			wantErr:  true,
			wantFrag: `is owned by the built-in scalar "Decimal"`,
		},
		{
			name:     "go_type owned by the registry via a Null wrapper",
			binding:  config.ScalarBinding{GoType: "github.com/google/uuid.NullUUID", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
			wantErr:  true,
			wantFrag: `is owned by the built-in scalar "NullUUID"`,
		},
		{
			name:     "go_type owned by the registry as a stdlib type",
			binding:  config.ScalarBinding{GoType: "time.Time", Marshaling: config.ScalarMarshalingMethod},
			wantErr:  true,
			wantFrag: `is owned by the built-in scalar "Time"`,
		},
		{
			// The registry is keyed on the qualified short form, so reaching
			// it through a module whose path carries a major-version element
			// has to collapse `/v5` first. Before it did, this declaration
			// rendered `v5.UUID`, matched nothing, and was accepted — leaving
			// the consumer an entry that binds nothing while gen routes the
			// column to the built-in UUID scalar anyway (PRD §7.4, §26.4.1).
			name:     "go_type owned by the registry through a v2+ module path",
			binding:  config.ScalarBinding{GoType: "github.com/gofrs/uuid/v5.UUID", Marshaling: config.ScalarMarshalingMethod},
			wantErr:  true,
			wantFrag: `"github.com/gofrs/uuid/v5.UUID" resolves to "uuid.UUID", which is owned by the built-in scalar "UUID"`,
		},
		{
			name:     "scalar name owned by the built-in registry",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
			asName:   "Decimal",
			wantErr:  true,
			wantFrag: `"Decimal" is a built-in scalar and cannot be redeclared`,
		},
		{
			name:     "scalar name owned by a Null wrapper",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
			asName:   "NullUUID",
			wantErr:  true,
			wantFrag: `"NullUUID" is a built-in scalar and cannot be redeclared`,
		},
		{
			name:     "scalar name shadowing a GraphQL spec built-in",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
			asName:   "String",
			wantErr:  true,
			wantFrag: `"String" is a GraphQL spec built-in and cannot be redeclared`,
		},
		{
			name:    "gqlgen-bundled name that is not reserved",
			binding: config.ScalarBinding{GoType: "int64", Marshaling: config.ScalarMarshalingBuiltin},
			asName:  "Int64",
		},
		{
			name:     "scalar name that is not a GraphQL identifier",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"},
			asName:   "IP-Addr",
			wantErr:  true,
			wantFrag: `"IP-Addr" is not a valid GraphQL name`,
		},
		{
			name:     "missing marshaling",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr"},
			wantErr:  true,
			wantFrag: "marshaling is required",
		},
		{
			name:     "unrecognized marshaling",
			binding:  config.ScalarBinding{GoType: "net/netip.Addr", Marshaling: "bespoke"},
			wantErr:  true,
			wantFrag: `marshaling: "bespoke" is not a valid value`,
		},
		{
			name:     "method without go_type",
			binding:  config.ScalarBinding{Marshaling: config.ScalarMarshalingMethod},
			wantErr:  true,
			wantFrag: `go_type: required when marshaling is "method"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.asName
			if key == "" {
				key = "IPAddr"
			}
			cfg := scalarConfig(map[string]config.ScalarBinding{key: tt.binding})
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePreParse(%+v) = nil, want error containing %q", tt.binding, tt.wantFrag)
			}
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidatePreParse(%+v) unexpected error: %v", tt.binding, err)
				}
				return
			}
			if !strings.Contains(err.Error(), tt.wantFrag) {
				t.Errorf("ValidatePreParse(%+v) error = %v, want it to contain %q", tt.binding, err, tt.wantFrag)
			}
		})
	}
}

// TestValidatePreParse_ScalarGoTypeUniqueness pins the cross-entry rule: two
// scalars declaring the same go_type cannot both work, because the generator's
// lookup is keyed by Go type. Whichever won, the other would be silently inert.
func TestValidatePreParse_ScalarGoTypeUniqueness(t *testing.T) {
	ext := func(goType string) config.ScalarBinding {
		return config.ScalarBinding{GoType: goType, Marshaling: config.ScalarMarshalingExternal, MarshalerPackage: "example.com/app/gqlscalars"}
	}

	tests := []struct {
		name     string
		scalars  map[string]config.ScalarBinding
		wantErr  bool
		wantFrag string
	}{
		{
			name:    "distinct go types",
			scalars: map[string]config.ScalarBinding{"IPAddr": ext("net/netip.Addr"), "Email": ext("example.com/types.Email")},
		},
		{
			name: "a go_type-less builtin entry never collides",
			scalars: map[string]config.ScalarBinding{
				"IPAddr": ext("net/netip.Addr"),
				"Any":    {Marshaling: config.ScalarMarshalingBuiltin},
				"Map":    {Marshaling: config.ScalarMarshalingBuiltin},
			},
		},
		{
			name:     "two names on one go type",
			scalars:  map[string]config.ScalarBinding{"AlphaAddr": ext("net/netip.Addr"), "ZuluAddr": ext("net/netip.Addr")},
			wantErr:  true,
			wantFrag: `api.graphql.scalars.ZuluAddr.go_type: "net/netip.Addr" is already declared by api.graphql.scalars.AlphaAddr`,
		},
		{
			name:     "collision reported against the sorted-later name regardless of map order",
			scalars:  map[string]config.ScalarBinding{"ZuluAddr": ext("net/netip.Addr"), "AlphaAddr": ext("net/netip.Addr")},
			wantErr:  true,
			wantFrag: `api.graphql.scalars.ZuluAddr.go_type: "net/netip.Addr" is already declared by api.graphql.scalars.AlphaAddr`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ValidatePreParse(scalarConfig(tt.scalars))
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidatePreParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want error containing %q", tt.wantFrag)
			}
			if !strings.Contains(err.Error(), tt.wantFrag) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, tt.wantFrag)
			}
		})
	}
}

// TestValidatePreParse_ScalarBindingDisabledIsInert pins §26.5.8: with
// api.graphql.enabled false the scalars block is never validated, so a binding
// that would otherwise error passes.
func TestValidatePreParse_ScalarBindingDisabledIsInert(t *testing.T) {
	cfg := scalarConfig(map[string]config.ScalarBinding{
		"IPAddr": {GoType: "net/netip.Addr", Marshaling: config.ScalarMarshalingExternal},
	})
	cfg.API.GraphQL.Enabled = false
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Errorf("ValidatePreParse() with api.graphql disabled = %v, want nil", err)
	}
}

// TestValidatePreParse_ScalarBindingErrorsAreSorted pins that multi-entry
// failures report in a stable order. The two reads of the map used a bare
// `range`, so error order tracked Go's map-iteration seed and differed run to
// run.
func TestValidatePreParse_ScalarBindingErrorsAreSorted(t *testing.T) {
	cfg := scalarConfig(map[string]config.ScalarBinding{
		"Zulu":    {Marshaling: config.ScalarMarshalingMethod},
		"Alpha":   {Marshaling: config.ScalarMarshalingMethod},
		"Mike":    {Marshaling: config.ScalarMarshalingMethod},
		"Bravo":   {Marshaling: config.ScalarMarshalingMethod},
		"Yankee":  {Marshaling: config.ScalarMarshalingMethod},
		"Charlie": {Marshaling: config.ScalarMarshalingMethod},
	})
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() = nil, want errors for every entry")
	}
	want := err.Error()
	for i := range 20 {
		_, got := config.ValidatePreParse(cfg)
		if got == nil {
			t.Fatalf("ValidatePreParse() run %d = nil, want errors", i)
		}
		if got.Error() != want {
			t.Fatalf("ValidatePreParse() run %d error order drifted:\n first: %v\n  this: %v", i, want, got)
		}
	}
	alpha := strings.Index(want, "scalars.Alpha")
	zulu := strings.Index(want, "scalars.Zulu")
	if alpha < 0 || zulu < 0 || alpha > zulu {
		t.Errorf("ValidatePreParse() errors are not name-sorted (Alpha at %d, Zulu at %d):\n%v", alpha, zulu, err)
	}
}

// TestSplitGoType pins the last-dot split shared with gqlgen's
// internal/code.PkgAndType. A type name never contains a dot, so the rule is
// exact even for a package path that does — `gopkg.in/yaml.v3.Node` binds fine.
func TestSplitGoType(t *testing.T) {
	tests := []struct {
		name       string
		goType     string
		wantImport string
		wantType   string
		wantShort  string
	}{
		{name: "stdlib nested path", goType: "net/netip.Addr", wantImport: "net/netip", wantType: "Addr", wantShort: "netip.Addr"},
		{name: "module path", goType: "github.com/shopspring/decimal.Decimal", wantImport: "github.com/shopspring/decimal", wantType: "Decimal", wantShort: "decimal.Decimal"},
		{name: "single-element stdlib path", goType: "time.Time", wantImport: "time", wantType: "Time", wantShort: "time.Time"},
		{name: "unqualified builtin", goType: "int64", wantImport: "", wantType: "int64", wantShort: "int64"},
		{name: "package path containing dots still splits at the last one", goType: "gopkg.in/yaml.v3.Node", wantImport: "gopkg.in/yaml.v3", wantType: "Node", wantShort: "yaml.v3.Node"},
		// A module major-version element is not a package name. gofrs is the
		// UUID integration spelled this way (PRD §7.4), and rendering it
		// `v5.UUID` kept it from matching BuiltInScalarGoTypes["uuid.UUID"].
		{name: "module major version is not the package name", goType: "github.com/gofrs/uuid/v5.UUID", wantImport: "github.com/gofrs/uuid/v5", wantType: "UUID", wantShort: "uuid.UUID"},
		{name: "double-digit major version", goType: "example.com/pkg/thing/v12.T", wantImport: "example.com/pkg/thing/v12", wantType: "T", wantShort: "thing.T"},
		// v0 and v1 are never spelled in a module path, so a trailing element
		// that looks like one is a real package name and must survive.
		{name: "a package actually named v1", goType: "example.com/api/v1.Request", wantImport: "example.com/api/v1", wantType: "Request", wantShort: "v1.Request"},
		{name: "a leading zero is not a major version", goType: "example.com/api/v02.Request", wantImport: "example.com/api/v02", wantType: "Request", wantShort: "v02.Request"},
		{name: "a v-prefixed word is not a major version", goType: "example.com/pkg/vector.Vec", wantImport: "example.com/pkg/vector", wantType: "Vec", wantShort: "vector.Vec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotImport, gotType := config.SplitGoType(tt.goType)
			if gotImport != tt.wantImport || gotType != tt.wantType {
				t.Errorf("SplitGoType(%q) = (%q, %q), want (%q, %q)", tt.goType, gotImport, gotType, tt.wantImport, tt.wantType)
			}
			if got := config.QualifiedGoType(gotImport, gotType); got != tt.wantShort {
				t.Errorf("QualifiedGoType(%q, %q) = %q, want %q", gotImport, gotType, got, tt.wantShort)
			}
		})
	}
}
