package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// consumerScalarSchema seeds a table whose non-PK columns are retyped, via
// column_map, onto Go types the built-in scalar registry does not cover. Both
// a non-null and a nullable column of the same type are present so the
// nullable projection is covered alongside the non-null one.
func consumerScalarSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "servers",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "ip", Type: "inet", Nullable: false},
					{Name: "alt_ip", Type: "inet", Nullable: true},
					{Name: "owner", Type: "text", Nullable: false},
				},
			},
		},
	}
}

// consumerScalarInput wires consumerScalarSchema with the column_map retypes
// plus the matching api.graphql.scalars declarations: `ip` / `alt_ip` onto an
// external consumer scalar, `owner` onto a method-marshaled one.
func consumerScalarInput(t *testing.T) *gen.GenerateInput {
	t.Helper()
	in := apiTestInput(t, consumerScalarSchema())
	in.Config.Tables = map[string]config.TableConfig{
		"servers": {
			ColumnMap: map[string]config.ColumnOverride{
				"ip":     {Type: "netip.Addr", Import: "net/netip"},
				"alt_ip": {Type: "netip.Addr", Import: "net/netip"},
				"owner":  {Type: "types.Email", Import: "example.com/types"},
			},
		},
	}
	in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
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
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)
	return in
}

func buildConsumerScalarAPI(t *testing.T, in *gen.GenerateInput) *gen.APIContext {
	t.Helper()
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext() unexpected error: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("BuildAPIContext() = nil, want a context")
	}
	return apiCtx
}

func fieldByGraphQLName(t *testing.T, apiCtx *gen.APIContext, name string) gen.APIFieldContext {
	t.Helper()
	for _, tbl := range apiCtx.Tables {
		for _, f := range tbl.Fields {
			if f.GraphQLName == name {
				return f
			}
		}
	}
	t.Fatalf("no API field named %q in the built context", name)
	return gen.APIFieldContext{}
}

// TestConsumerScalars_reachTheSchemaFieldType pins that a column retyped onto
// a Go type outside the built-in registry does not fall through to `String`,
// which would leave a non-string Go field behind a String schema field. The
// declared scalar decides the field type, and the nullable
// column gets the plain nullable form — consumer scalars carry no Null-wrapper
// pairing, so gqlgen's pointer nullability covers it.
func TestConsumerScalars_reachTheSchemaFieldType(t *testing.T) {
	apiCtx := buildConsumerScalarAPI(t, consumerScalarInput(t))

	tests := []struct {
		name      string
		field     string
		wantType  string
		wantScalr string
	}{
		{name: "external scalar on a non-null column", field: "ip", wantType: "IPAddr!", wantScalr: "IPAddr"},
		{name: "external scalar on a nullable column", field: "altIP", wantType: "IPAddr", wantScalr: "IPAddr"},
		{name: "method scalar on a non-null column", field: "owner", wantType: "EmailAddress!", wantScalr: "EmailAddress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fieldByGraphQLName(t, apiCtx, tt.field)
			if f.GraphQLType != tt.wantType {
				t.Errorf("field %q GraphQLType = %q, want %q", tt.field, f.GraphQLType, tt.wantType)
			}
			if f.GraphQLBare != tt.wantScalr {
				t.Errorf("field %q GraphQLBare = %q, want %q", tt.field, f.GraphQLBare, tt.wantScalr)
			}
		})
	}
}

// TestConsumerScalars_declaredInSchemaButNotEmitted pins the split that keeps
// scalars_gen.go honest: a consumer-declared scalar is declared in the schema
// (UsedScalars drives `scalar <N>`) but never reaches ExternalScalars, because
// sqlgen has no body to emit for it — the consumer wrote the marshalers.
func TestConsumerScalars_declaredInSchemaButNotEmitted(t *testing.T) {
	apiCtx := buildConsumerScalarAPI(t, consumerScalarInput(t))

	used := scalarNames(apiCtx.UsedScalars)
	for _, want := range []string{"IPAddr", "EmailAddress"} {
		if !slices.Contains(used, want) {
			t.Errorf("UsedScalars = %v, want it to contain %q so the schema declares it", used, want)
		}
	}

	external := scalarNames(apiCtx.ExternalScalars)
	for _, notWant := range []string{"IPAddr", "EmailAddress"} {
		if slices.Contains(external, notWant) {
			t.Errorf("ExternalScalars = %v, must not contain consumer-declared %q — scalars.go.tmpl has no body for it", external, notWant)
		}
	}
}

// TestConsumerScalars_carryTheMarshalerPackage pins that the declared
// marshaler_package survives onto the registered use, which is what redirects
// the gqlgen `models:` discovery anchor away from the resolver package.
func TestConsumerScalars_carryTheMarshalerPackage(t *testing.T) {
	apiCtx := buildConsumerScalarAPI(t, consumerScalarInput(t))

	tests := []struct {
		name       string
		scalar     string
		wantPkg    string
		wantMarshl string
	}{
		{name: "external carries the package", scalar: "IPAddr", wantPkg: "example.com/app/gqlscalars", wantMarshl: config.ScalarMarshalingExternal},
		{name: "method carries none", scalar: "EmailAddress", wantPkg: "", wantMarshl: config.ScalarMarshalingMethod},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got gen.APIScalarUse
			for _, u := range apiCtx.UsedScalars {
				if u.Name == tt.scalar {
					got = u
				}
			}
			if got.Name == "" {
				t.Fatalf("scalar %q missing from UsedScalars %v", tt.scalar, scalarNames(apiCtx.UsedScalars))
			}
			if got.MarshalerPackage != tt.wantPkg {
				t.Errorf("scalar %q MarshalerPackage = %q, want %q", tt.scalar, got.MarshalerPackage, tt.wantPkg)
			}
			if got.Marshaling != tt.wantMarshl {
				t.Errorf("scalar %q Marshaling = %q, want %q", tt.scalar, got.Marshaling, tt.wantMarshl)
			}
		})
	}
}

// TestConsumerScalars_registryStillWins pins that adding the consumer lookup
// did not displace the built-in registry: a uuid.UUID column still resolves to
// the registry's UUID scalar, still lands in ExternalScalars (sqlgen emits its
// body), and still carries no marshaler package.
func TestConsumerScalars_registryStillWins(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)

	apiCtx := buildConsumerScalarAPI(t, in)

	if f := fieldByGraphQLName(t, apiCtx, "id"); f.GraphQLBare != "UUID" {
		t.Errorf("PK field GraphQLBare = %q, want %q", f.GraphQLBare, "UUID")
	}
	external := scalarNames(apiCtx.ExternalScalars)
	if !slices.Contains(external, "UUID") {
		t.Errorf("ExternalScalars = %v, want it to contain the registry-owned %q", external, "UUID")
	}
	for _, u := range apiCtx.ExternalScalars {
		if u.MarshalerPackage != "" {
			t.Errorf("registry scalar %q MarshalerPackage = %q, want empty", u.Name, u.MarshalerPackage)
		}
	}
}

// TestConsumerScalars_undeclaredTypeIsRejected pins that a Go type with no
// GraphQL binding is a generation error, not a silent `String`.
//
// This test previously asserted the opposite. The fallthrough it pinned is
// broken downstream — gqlgen answers a `String` field in front of a non-string
// Go field with a panic("not implemented") resolver on read, and types the input field `string`, which the translator
// does not compile against. Keeping it as a "baseline" preserved a diagnostic
// that arrived as a Go type error at the far end of a multi-stage pipeline,
// naming neither the column nor the cause. The error names both, and PRD
// §26.4.1 already specified it for the sibling case.
func TestConsumerScalars_undeclaredTypeIsRejected(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.API.GraphQL.Scalars = nil

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err == nil {
		t.Fatal("BuildAPIContext() = nil error, want a rejection for the unbound netip.Addr column")
	}
	// The message has to carry all three of these or it is no better than the
	// compile error it replaces: which column, which Go type, and the way out.
	for _, want := range []string{`"ip"`, "netip.Addr", "api.graphql.scalars"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("BuildAPIContext() error = %v, want it to mention %q", err, want)
		}
	}
}

// TestConsumerScalars_undeclaredTypeAllowedWhenColumnIsHidden pins the one
// exemption from the rule above: §32.2 `hidden` / `internal` drops a column
// from every API surface, so it names no GraphQL type anywhere and needs no
// binding. Without this, `access: hidden` — the documented way to keep a
// column out of the API — would be the one setting that forces you to
// describe the column TO the API.
func TestConsumerScalars_undeclaredTypeAllowedWhenColumnIsHidden(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.API.GraphQL.Scalars = nil
	for _, col := range []string{"ip", "alt_ip"} {
		in.Config.Tables["servers"].ColumnMap[col] = config.ColumnOverride{
			Type: "netip.Addr", Import: "net/netip", Access: config.AccessHidden,
		}
	}
	in.Config.Tables["servers"].ColumnMap["owner"] = config.ColumnOverride{}

	apiCtx := buildConsumerScalarAPI(t, in)

	// The `id` PK stays exposed, and a `uuid` column resolves to `uuid.UUID`
	// with no configuration (PRD §7.2), so the registry's UUID scalar is
	// legitimately in use. Everything else in the fixture is hidden, so UUID
	// is the whole set — an exact compare keeps the claim sharp: neither
	// IPAddr nor EmailAddress may appear.
	if used := scalarNames(apiCtx.UsedScalars); !slices.Equal(used, []string{"UUID"}) {
		t.Errorf("UsedScalars = %v, want exactly [UUID] — a hidden column registers no scalar", used)
	}
}

// TestConsumerScalars_matchOnImportNotJustTypeName pins that the reverse index
// is keyed on (import path, type name). Two packages can both export a type
// named `Email`; only the declared one may claim the scalar.
func TestConsumerScalars_matchOnImportNotJustTypeName(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.Tables["servers"].ColumnMap["owner"] = config.ColumnOverride{
		Type:   "types.Email",
		Import: "example.com/other",
	}

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	// The declared EmailAddress scalar names `types.Email` from ANOTHER
	// package, so this column must not claim it. Here "does not claim
	// it" means "has no binding at all", which is a rejection rather than the
	// old silent `String` — the assertion moved but the property it pins
	// (the index keys on the import path, not just the type name) did not.
	if err == nil {
		t.Fatal("BuildAPIContext() = nil error, want a rejection — types.Email from an undeclared package claims no scalar")
	}
	if !strings.Contains(err.Error(), `"owner"`) || !strings.Contains(err.Error(), "types.Email") {
		t.Errorf("BuildAPIContext() error = %v, want it to name the owner column and its Go type", err)
	}
	if strings.Contains(err.Error(), "EmailAddress") {
		t.Errorf("BuildAPIContext() error = %v, must not suggest the scalar declared for a different package's type", err)
	}
}

// TestBuiltInScalarGoTypesMatchRegistry pins config.BuiltInScalarGoTypes — the
// mirror `sqlgen validate` reaches for the collision rule — against
// gen.builtInScalarRegistry, which holds the real bindings. The two live in
// different packages only because config must not import gen; a registry entry
// added without a mirror entry would silently make that Go type declarable.
func TestBuiltInScalarGoTypesMatchRegistry(t *testing.T) {
	got := gen.BuiltInScalarRegistryNamesForTest()
	if len(got) != len(config.BuiltInScalarGoTypes) {
		t.Errorf("registry has %d entries, config.BuiltInScalarGoTypes has %d:\n registry: %v\n   mirror: %v",
			len(got), len(config.BuiltInScalarGoTypes), got, config.BuiltInScalarGoTypes)
	}
	for goType, name := range got {
		mirrored, ok := config.BuiltInScalarGoTypes[goType]
		if !ok {
			t.Errorf("registry Go type %q is missing from config.BuiltInScalarGoTypes; a consumer could declare a scalar that silently loses to the registry", goType)
			continue
		}
		if mirrored != name {
			t.Errorf("config.BuiltInScalarGoTypes[%q] = %q, want %q (the registry's scalar name)", goType, mirrored, name)
		}
	}
	for goType := range config.BuiltInScalarGoTypes {
		if _, ok := got[goType]; !ok {
			t.Errorf("config.BuiltInScalarGoTypes has %q, which the registry does not bind; the collision error would name a scalar that does not exist", goType)
		}
	}
	// The name axis of the same rule. Guarding go_type alone leaves the
	// scalar-name route open, and that route is the more damaging of the two:
	// registration is first-write-wins by name, so a consumer entry named
	// after a registry scalar takes over its slot and the registry's own
	// columns lose their emitted marshaler.
	for _, name := range got {
		if _, ok := config.ReservedScalarNames[name]; !ok {
			t.Errorf("registry scalar %q is missing from config.ReservedScalarNames; a consumer could declare a scalar under that name and hijack the registry's registration slot", name)
		}
	}
}

// TestConsumerScalars_reportsMissingMarshalingDeterministically pins that
// BuildAPIContext's defensive check names the same offending entry on every
// run. It iterated the map with a bare `range`, so the reported name tracked
// Go's map-iteration seed.
func TestConsumerScalars_reportsMissingMarshalingDeterministically(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
		"Zulu":    {GoType: "example.com/types.Z"},
		"Alpha":   {GoType: "example.com/types.A"},
		"Mike":    {GoType: "example.com/types.M"},
		"Charlie": {GoType: "example.com/types.C"},
	}
	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}
	for i := range 20 {
		_, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
		if err == nil {
			t.Fatalf("BuildAPIContext() run %d = nil, want an error for the missing marshaling", i)
		}
		if !strings.Contains(err.Error(), "scalars.Alpha") {
			t.Fatalf("BuildAPIContext() run %d error = %v, want it to name the first entry in sorted order (Alpha)", i, err)
		}
	}
}

// TestUnusedScalarWarnings pins the diagnostic for a declaration nothing
// resolves to.
//
// This is split into two mechanisms, and the table below records the
// split. A declaration is unused for one of two reasons:
//
//   - The column that would have used it is dropped from the API by its §32.2
//     access role (or has not been added yet). The config is legitimate, so
//     this stays a WARNING — and it is a deliberate false positive, which is
//     the price of catching the next case at all.
//   - The `go_type` is a typo or names a package no column resolves to. The
//     column then has no GraphQL binding, and BuildAPIContext now REJECTS it
//     outright rather than falling through to `String`. That supersedes the
//     warning for this case: PRD §26.4.1 justified the warning by saying the
//     typo "leaves the column falling through to String with the failure
//     surfacing far downstream in gqlgen", and that is no longer what happens.
func TestUnusedScalarWarnings(t *testing.T) {
	tests := []struct {
		name        string
		goType      string
		access      string
		dropScalars bool
		wantWarn    bool
		// wantErr expects BuildAPIContext to reject the schema: the columns
		// are API-visible and no declaration binds their Go type.
		wantErr bool
	}{
		{name: "resolved scalar warns not", goType: "net/netip.Addr"},
		{name: "typoed go_type is rejected", goType: "net/netip.Add", wantErr: true},
		{name: "go_type from an undeclared package is rejected", goType: "example.com/other.Addr", wantErr: true},
		{name: "hidden columns leave a legitimate declaration unused", goType: "net/netip.Addr", access: config.AccessHidden, wantWarn: true},
		{name: "internal columns leave a legitimate declaration unused", goType: "net/netip.Addr", access: config.AccessInternal, wantWarn: true},
		{name: "no declarations at all is rejected", goType: "net/netip.Addr", dropScalars: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := consumerScalarInput(t)
			// Both netip.Addr columns take the access role: the scalar counts
			// as used while ANY exposed column resolves to it, so hiding only
			// one proves nothing.
			for _, col := range []string{"ip", "alt_ip"} {
				in.Config.Tables["servers"].ColumnMap[col] = config.ColumnOverride{
					Type: "netip.Addr", Import: "net/netip", Access: tt.access,
				}
			}
			// Keep only the IPAddr declaration so the assertion is unambiguous.
			if tt.dropScalars {
				in.Config.API.GraphQL.Scalars = nil
			} else {
				in.Config.API.GraphQL.Scalars = map[string]config.ScalarBinding{
					"IPAddr": {
						GoType:           tt.goType,
						Marshaling:       config.ScalarMarshalingExternal,
						MarshalerPackage: "example.com/app/gqlscalars",
					},
				}
			}
			// `owner` would otherwise resolve to a now-undeclared scalar; retype
			// it to a plain string so only `ip` is in play.
			in.Config.Tables["servers"].ColumnMap["owner"] = config.ColumnOverride{}

			if tt.wantErr {
				tables, err := gen.BuildTableContexts(in, nil)
				if err != nil {
					t.Fatalf("BuildTableContexts() unexpected error: %v", err)
				}
				_, err = gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
				if err == nil {
					t.Fatal("BuildAPIContext() = nil error, want a rejection for the unbound netip.Addr columns")
				}
				if !strings.Contains(err.Error(), "netip.Addr") {
					t.Errorf("BuildAPIContext() error = %v, want it to name the unbound Go type", err)
				}
				return
			}

			apiCtx := buildConsumerScalarAPI(t, in)
			got := gen.UnusedScalarWarningsForTest(in.Config, apiCtx)

			if tt.wantWarn {
				if len(got) != 1 {
					t.Fatalf("UnusedScalarWarningsForTest() = %v, want exactly one warning", got)
				}
				if !strings.Contains(got[0], "api.graphql.scalars.IPAddr") || !strings.Contains(got[0], tt.goType) {
					t.Errorf("warning = %q, want it to name the entry and its go_type %q", got[0], tt.goType)
				}
				return
			}
			if len(got) != 0 {
				t.Errorf("UnusedScalarWarningsForTest() = %v, want none", got)
			}
		})
	}
}

// TestUnusedScalarWarnings_inertWhenAPIDisabled pins that the diagnostic is
// silent when there is no API context to measure against, rather than
// reporting every declaration as unused.
func TestUnusedScalarWarnings_inertWhenAPIDisabled(t *testing.T) {
	in := consumerScalarInput(t)
	if got := gen.UnusedScalarWarningsForTest(in.Config, nil); got != nil {
		t.Errorf("UnusedScalarWarningsForTest(nil apiCtx) = %v, want nil", got)
	}
	cfg := *in.Config
	cfg.API = nil
	if got := gen.UnusedScalarWarningsForTest(&cfg, &gen.APIContext{}); got != nil {
		t.Errorf("UnusedScalarWarningsForTest(nil cfg.API) = %v, want nil", got)
	}
}

// TestUnusedScalarWarnings_skipsGoTypeLessBuiltin pins that a bare
// `marshaling: builtin` declaration is not reported: it binds no column by
// definition, so "unused" is not a meaningful claim about it.
func TestUnusedScalarWarnings_skipsGoTypeLessBuiltin(t *testing.T) {
	in := consumerScalarInput(t)
	in.Config.API.GraphQL.Scalars["Any"] = config.ScalarBinding{Marshaling: config.ScalarMarshalingBuiltin}

	apiCtx := buildConsumerScalarAPI(t, in)
	for _, w := range gen.UnusedScalarWarningsForTest(in.Config, apiCtx) {
		if strings.Contains(w, "scalars.Any") {
			t.Errorf("go_type-less builtin declaration was reported unused: %q", w)
		}
	}
}
