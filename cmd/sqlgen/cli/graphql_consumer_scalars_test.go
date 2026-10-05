package cli

import (
	"maps"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// newConsumerScalarConfig returns a config whose `servers` table retypes two
// columns onto Go types the built-in registry does not cover, with the given
// scalar declarations attached.
func newConsumerScalarConfig(scalars map[string]config.ScalarBinding) *config.RootConfig {
	cfg := newAPIParityTestConfig()
	cfg.Tables["servers"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"ip":    {Type: "netip.Addr", Import: "net/netip"},
			"owner": {Type: "types.Email", Import: "example.com/types"},
		},
	}
	cfg.API.GraphQL.Scalars = scalars
	return cfg
}

// buildConsumerScalarAPICtx builds the API context for newConsumerScalarConfig's
// schema and stamps the resolver import path the way
// populateAPIContextResolverFields would.
func buildConsumerScalarAPICtx(t *testing.T, cfg *config.RootConfig) *gen.APIContext {
	t.Helper()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "servers",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "ip", Type: "inet", Nullable: false},
					{Name: "owner", Type: "text", Nullable: false},
					// The two bundled-scalar shapes. Neither needs a
					// declaration to be legal: `hits` resolves to the spec
					// `Int` and `created_at` to the registry's `DateTime`
					// unless a test retypes them, so the other tests in this
					// file are unaffected by their presence.
					{Name: "hits", Type: "bigint", Nullable: false},
					{Name: "created_at", Type: "timestamp", Nullable: false},
				},
			},
		},
	}
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error: %v", err)
	}
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"
	return apiCtx
}

var consumerScalarDecls = map[string]config.ScalarBinding{
	"IPAddr": {
		GoType:           "net/netip.Addr",
		Marshaling:       config.ScalarMarshalingExternal,
		MarshalerPackage: "example.com/app/gqlscalars",
	},
	"EmailAddress": {
		GoType:     "example.com/types.Email",
		Marshaling: config.ScalarMarshalingMethod,
	},
}

// TestBuildMergeInput_ConsumerScalarBindings pins the gqlgen `models:` shape
// per marshaling mode. An external consumer scalar anchors on the
// declared marshaler_package — not the resolver dir, which holds only the
// registry's own marshalers — and a method scalar binds straight to its Go
// type so gqlgen calls MarshalGQL / UnmarshalGQL on it.
func TestBuildMergeInput_ConsumerScalarBindings(t *testing.T) {
	cfg := newConsumerScalarConfig(consumerScalarDecls)
	merge, _ := buildMergeInput(cfg, buildConsumerScalarAPICtx(t, cfg), "example.com/foo")

	tests := []struct {
		name   string
		scalar string
		want   string
	}{
		{name: "external anchors on marshaler_package", scalar: "IPAddr", want: "example.com/app/gqlscalars.IPAddr"},
		{name: "method binds to the go type", scalar: "EmailAddress", want: "example.com/types.Email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, ok := merge.Models[tt.scalar]
			if !ok {
				t.Fatalf("merge.Models missing %q binding; have %v", tt.scalar, slices.Sorted(maps.Keys(merge.Models)))
			}
			if len(paths) != 1 || paths[0] != tt.want {
				t.Errorf("merge.Models[%q] = %v, want [%q]", tt.scalar, paths, tt.want)
			}
		})
	}
}

// TestBuildMergeInput_RegistryScalarKeepsResolverAnchor pins that redirecting
// consumer externals did not move the registry's own anchor: UUID still points
// at the resolver package, where sqlgen emits MarshalUUID / UnmarshalUUID.
func TestBuildMergeInput_RegistryScalarKeepsResolverAnchor(t *testing.T) {
	cfg := newConsumerScalarConfig(consumerScalarDecls)
	cfg.Overrides.Types["uuid"] = config.TypeOverride{Type: "uuid.UUID", Import: "github.com/google/uuid"}
	merge, _ := buildMergeInput(cfg, buildConsumerScalarAPICtx(t, cfg), "example.com/foo")

	const want = "example.com/foo/gen/graph.UUID"
	paths, ok := merge.Models["UUID"]
	if !ok {
		t.Fatalf("merge.Models missing UUID binding")
	}
	if len(paths) != 1 || paths[0] != want {
		t.Errorf("merge.Models[\"UUID\"] = %v, want [%q]", paths, want)
	}
}

// TestBuildMergeInput_UndeclaredScalarGetsNoEntry pins that the merge is driven
// by scalars the schema actually references. sqlgen used to emit a `models:`
// entry for every declared scalar whether or not any column used it; gqlgen
// ignores an entry for a type absent from the schema, so it was dead weight
// that could only ever mislead. A scalar the consumer needs for their
// own hand-written schema belongs in their own gqlgen.yml (PRD §26.5.6).
func TestBuildMergeInput_UndeclaredScalarGetsNoEntry(t *testing.T) {
	// Both fixture columns keep their declaration: a column whose Go type
	// has no binding is a generation error, so dropping EmailAddress
	// here would fail the build rather than exercise the merge. `Unused` is
	// the entry nothing resolves to, which is what this test is about.
	decls := map[string]config.ScalarBinding{
		"IPAddr":       consumerScalarDecls["IPAddr"],
		"EmailAddress": consumerScalarDecls["EmailAddress"],
		"Unused": {
			GoType:           "example.com/other.Thing",
			Marshaling:       config.ScalarMarshalingExternal,
			MarshalerPackage: "example.com/app/gqlscalars",
		},
	}
	cfg := newConsumerScalarConfig(decls)
	merge, _ := buildMergeInput(cfg, buildConsumerScalarAPICtx(t, cfg), "example.com/foo")

	if _, ok := merge.Models["IPAddr"]; !ok {
		t.Errorf("merge.Models missing IPAddr; a scalar a column resolves to must be bound")
	}
	if paths, ok := merge.Models["Unused"]; ok {
		t.Errorf("merge.Models[\"Unused\"] = %v, want no entry — no column resolves to it", paths)
	}
}

// TestBuildMergeInput_BuiltinScalarPinsTheBundledMarshaler is a regression
// pin, and it replaces an assertion that had the rule backwards —
// that `marshaling: builtin` emits NO binding, because gqlgen ships the
// marshaler and an entry would only fight its defaults.
//
// It does not fight them, it disambiguates them. gqlgen binds `Int64` to the
// two-entry list `[graphql.Int, graphql.Int64]`, and a GENERATED position
// (create/update input field, resolver argument) takes `Model[0]` — so an
// `int64` column routed onto `Int64` got an `int` input field in front of an
// `int64` model field and the input translator did not compile. Naming the
// marshaler collapses the list, because `injectBuiltins` skips any key that
// already exists.
//
// `Time` is the case that made the old rule look sound: its bundled list has
// one entry, whose Go type is the one a column carries, so it worked by luck.
// It is pinned alongside `Int64` so the two cannot diverge.
func TestBuildMergeInput_BuiltinScalarPinsTheBundledMarshaler(t *testing.T) {
	decls := map[string]config.ScalarBinding{
		"IPAddr":       consumerScalarDecls["IPAddr"],
		"EmailAddress": consumerScalarDecls["EmailAddress"],
		"Int64":        {GoType: "int64", Marshaling: config.ScalarMarshalingBuiltin},
	}
	cfg := newConsumerScalarConfig(decls)
	// `hits` is the column that routes onto Int64; `created_at` resolves to
	// time.Time, which the registry binds to the bundled `Time` scalar.
	cfg.Overrides.Types["timestamp"] = config.TypeOverride{Type: "time.Time"}
	apiCtx := buildConsumerScalarAPICtx(t, cfg)

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	tests := []struct {
		name   string
		scalar string
		want   string
	}{
		{name: "consumer-declared bundled scalar", scalar: "Int64", want: "github.com/99designs/gqlgen/graphql.Int64"},
		{name: "registry-owned bundled scalar", scalar: "Time", want: "github.com/99designs/gqlgen/graphql.Time"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, ok := merge.Models[tt.scalar]
			if !ok {
				t.Fatalf("merge.Models missing %q; a builtin-marshaled scalar must pin gqlgen's own marshaler, have %v", tt.scalar, slices.Sorted(maps.Keys(merge.Models)))
			}
			if len(paths) != 1 || paths[0] != tt.want {
				t.Errorf("merge.Models[%q] = %v, want [%q]", tt.scalar, paths, tt.want)
			}
		})
	}
}
