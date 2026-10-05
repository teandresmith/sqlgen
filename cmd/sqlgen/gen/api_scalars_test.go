package gen_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
	"github.com/teandresmith/sqlgen/parser"
)

// scalarsTestSchemaWithUUIDDecimal returns a fixture wired so the gotype
// resolver binds uuid columns to uuid.UUID and numeric columns to
// decimal.Decimal. Both bindings are configured via overrides.types — the
// same mechanism consumers use in real configs.
func scalarsTestSchemaWithUUIDDecimal() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "price", Type: "numeric", Nullable: false},
				},
			},
		},
	}
}

func TestScalarsGen_emitsCategory4Marshalers(t *testing.T) {
	in := apiTestInput(t, scalarsTestSchemaWithUUIDDecimal())

	// Bind uuid → uuid.UUID and numeric → decimal.Decimal so the API
	// context registers both as category-4 scalars.
	in.Config.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	in.Config.Overrides.Types["numeric"] = config.TypeOverride{
		Type:   "decimal.Decimal",
		Import: "github.com/shopspring/decimal",
	}
	// Re-init resolver with the new overrides — testInput captured the resolver
	// before overrides were set.
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	names := scalarNames(apiCtx.ExternalScalars)
	wantUUID, wantDecimal := false, false
	for _, n := range names {
		if n == "UUID" {
			wantUUID = true
		}
		if n == "Decimal" {
			wantDecimal = true
		}
	}
	if !wantUUID {
		t.Fatalf("expected UUID in ExternalScalars, got %v", names)
	}
	if !wantDecimal {
		t.Fatalf("expected Decimal in ExternalScalars, got %v", names)
	}

	// JSON / DateTime should never appear in ExternalScalars (they ship
	// MarshalGQL / UnmarshalGQL on the runtime type itself).
	for _, n := range names {
		if n == "JSON" || n == "DateTime" {
			t.Fatalf("category-3 scalar %q must not appear in ExternalScalars (got %v)", n, names)
		}
	}
}

func TestScalarsGen_rejectsUnknownScalarsWithoutMarshalingMode(t *testing.T) {
	cfg := &config.RootConfig{
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled: true,
				// EmailAddress is not in the built-in registry, and the
				// consumer omits the `marshaling:` declaration.
				Scalars: map[string]config.ScalarBinding{
					"EmailAddress": {GoType: "example.com/types.Email"},
				},
			},
		},
	}

	_, err := gen.BuildAPIContext(nil, nil, nil, nil, cfg)
	if err == nil {
		t.Fatal("expected error for missing marshaling on custom scalar")
	}
	if !strings.Contains(err.Error(), "EmailAddress") {
		t.Fatalf("error must name the scalar %q, got: %v", "EmailAddress", err)
	}
	if !strings.Contains(err.Error(), "marshaling") {
		t.Fatalf("error must mention the missing field, got: %v", err)
	}
}

func TestValidateAPIConfig_rejectsBadFieldCasing(t *testing.T) {
	cfg := &config.RootConfig{
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled:     true,
				FieldCasing: "PascalCase",
			},
		},
	}
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("expected error for invalid field_casing")
	}
	if !strings.Contains(err.Error(), "field_casing") {
		t.Fatalf("error should reference field_casing, got: %v", err)
	}
}

func TestValidateAPIConfig_rejectsScalarWithoutMarshaling(t *testing.T) {
	cfg := &config.RootConfig{
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled: true,
				Scalars: map[string]config.ScalarBinding{
					"EmailAddress": {GoType: "example.com/types.Email"},
				},
			},
		},
	}
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("expected error for missing marshaling on custom scalar")
	}
	if !strings.Contains(err.Error(), "EmailAddress") {
		t.Fatalf("error should name EmailAddress, got: %v", err)
	}
}

func scalarNames(uses []gen.APIScalarUse) []string {
	out := make([]string, 0, len(uses))
	for _, u := range uses {
		out = append(out, u.Name)
	}
	return out
}

// nullVariantTestSchema seeds a single table with one column per Null-wrapper
// pair member alongside a non-null partner so both halves of every pair
// (UUID/NullUUID, Decimal/NullDecimal, DateTime/NullDateTime) end up in
// UsedScalars. Drives registry pair coverage.
func nullVariantTestSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "things",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "owner_id", Type: "uuid", Nullable: true},
					{Name: "total", Type: "numeric", Nullable: false},
					{Name: "amount", Type: "numeric", Nullable: true},
					{Name: "occurred_at", Type: "timestamptz", Nullable: false},
					{Name: "processed_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}
}

// configureNullVariantOverrides wires the four overrides the registry pair
// detection depends on: uuid + numeric (cat 4 external) and timestamptz
// (cat 3 method). Each declares its Null wrapper Go type so the resolver
// returns the wrapper for nullable columns and the bare type for non-null
// columns.
func configureNullVariantOverrides(t *testing.T, in *gen.GenerateInput) {
	t.Helper()
	in.Config.Overrides.Types["uuid"] = config.TypeOverride{
		Type:     "uuid.UUID",
		Import:   "github.com/google/uuid",
		Nullable: config.NullableVariant{Type: "uuid.NullUUID", UnderlyingField: "UUID"},
	}
	in.Config.Overrides.Types["numeric"] = config.TypeOverride{
		Type:     "decimal.Decimal",
		Import:   "github.com/shopspring/decimal",
		Nullable: config.NullableVariant{Type: "decimal.NullDecimal", UnderlyingField: "Decimal"},
	}
	in.Config.Overrides.Types["timestamptz"] = config.TypeOverride{
		Type:     "types.DateTime",
		Import:   "github.com/teandresmith/sqlgen/types",
		Nullable: config.NullableVariant{Type: "types.NullDateTime", UnderlyingField: "Time"},
	}
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)
}

// TestScalarsGen_RegistersNullPairs covers (a) registry pair registration —
// each Null-wrapper Go type matched by a column resolves to its paired
// scalar (NullUUID / NullDecimal / NullDateTime) alongside the non-null
// member (UUID / Decimal / DateTime).
func TestScalarsGen_RegistersNullPairs(t *testing.T) {
	in := apiTestInput(t, nullVariantTestSchema())
	configureNullVariantOverrides(t, in)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	gotByName := make(map[string]gen.APIScalarUse, len(apiCtx.UsedScalars))
	for _, u := range apiCtx.UsedScalars {
		gotByName[u.Name] = u
	}

	cases := []struct {
		name            string
		wantMarshaling  string
		wantNullVariant bool
	}{
		{name: "UUID", wantMarshaling: "external", wantNullVariant: false},
		{name: "NullUUID", wantMarshaling: "external", wantNullVariant: true},
		{name: "Decimal", wantMarshaling: "external", wantNullVariant: false},
		{name: "NullDecimal", wantMarshaling: "external", wantNullVariant: true},
		{name: "DateTime", wantMarshaling: "method", wantNullVariant: false},
		{name: "NullDateTime", wantMarshaling: "method", wantNullVariant: true},
	}
	for _, tc := range cases {
		got, ok := gotByName[tc.name]
		if !ok {
			t.Errorf("scalar %q missing from UsedScalars; got %v", tc.name, scalarNames(apiCtx.UsedScalars))
			continue
		}
		if got.Marshaling != tc.wantMarshaling {
			t.Errorf("scalar %q marshaling: got %q, want %q", tc.name, got.Marshaling, tc.wantMarshaling)
		}
		if got.NullVariant != tc.wantNullVariant {
			t.Errorf("scalar %q NullVariant: got %v, want %v", tc.name, got.NullVariant, tc.wantNullVariant)
		}
	}

	// Null variants must surface only their paired Null* scalar, not the
	// bare-type entry — the registry keys each wrapper Go type separately.
	if _, ok := gotByName["UUID"]; !ok {
		t.Errorf("non-null uuid column should still register UUID alongside NullUUID")
	}
}

// TestMapColumnToGraphQL_NullVariantSkipsBangSuffix covers (b) schema emission
// shape — non-null Go types resolve to `<Scalar>!` while Null wrappers
// resolve to `<NullScalar>` (no `!`). PRD §26.4.1 schema rule.
func TestMapColumnToGraphQL_NullVariantSkipsBangSuffix(t *testing.T) {
	in := apiTestInput(t, nullVariantTestSchema())
	configureNullVariantOverrides(t, in)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if len(apiCtx.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(apiCtx.Tables))
	}

	got := make(map[string]string)
	for _, f := range apiCtx.Tables[0].Fields {
		got[f.SQLName] = f.GraphQLType
	}

	wants := map[string]string{
		"id":           "UUID!", // non-null PK whose Go binding is uuid.UUID → UUID! (the registry pre-empts the spec ID/String fallback)
		"owner_id":     "NullUUID",
		"total":        "Decimal!",
		"amount":       "NullDecimal",
		"occurred_at":  "DateTime!",
		"processed_at": "NullDateTime",
	}
	for col, want := range wants {
		if got[col] != want {
			t.Errorf("column %q GraphQLType: got %q, want %q", col, got[col], want)
		}
	}
}

// TestMarshalUUID_NoNilShim covers (e) — the runtime template for MarshalUUID
// must NOT emit the gqlgen-bundled defensive shim that silently writes `null`
// when the value is uuid.Nil. PRD §26.4.1 ratifies the "no defensive shim"
// rule because masking model-layer bugs by silently nulling out non-null
// columns violates the schema contract. Asserts on the rendered template,
// not the runtime behaviour, because the marshaler is a generated file the
// consumer never imports through this package directly.
func TestMarshalUUID_NoNilShim(t *testing.T) {
	in := apiTestInput(t, scalarsTestSchemaWithUUIDDecimal())
	in.Config.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	in.Config.Overrides.Types["numeric"] = config.TypeOverride{
		Type:   "decimal.Decimal",
		Import: "github.com/shopspring/decimal",
	}
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/scalars", apiCtx); err != nil {
		t.Fatalf("rendering api/scalars: %v", err)
	}
	body := buf.String()

	if !strings.Contains(body, "func MarshalUUID(v uuid.UUID) graphql.Marshaler") {
		t.Fatalf("expected MarshalUUID emission, got:\n%s", body)
	}
	// Locate the bare-UUID marshaler body and assert it has no defensive
	// branch — gqlgen's bundled marshaler tests `if v == uuid.Nil { return
	// graphql.Null }` which would mask non-null violations on the wire.
	const marker = "func MarshalUUID(v uuid.UUID) graphql.Marshaler"
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("MarshalUUID not found in body:\n%s", body)
	}
	bareMarshalBody, _, _ := strings.Cut(rest, "\nfunc ")
	if strings.Contains(bareMarshalBody, "uuid.Nil") {
		t.Errorf("MarshalUUID must not branch on uuid.Nil (no defensive shim per PRD §26.4.1); body was:\n%s", bareMarshalBody)
	}
	if strings.Contains(bareMarshalBody, "graphql.Null") {
		t.Errorf("MarshalUUID must not emit graphql.Null (no defensive shim per PRD §26.4.1); body was:\n%s", bareMarshalBody)
	}
}

// TestMarshalNullUUID_EmitsPairedFunctions covers (d) — when a column with
// uuid.NullUUID Go type is in scope, the rendered scalars_gen.go contains
// MarshalNullUUID / UnmarshalNullUUID alongside MarshalUUID / UnmarshalUUID.
// Same expectation for NullDecimal.
func TestMarshalNullUUID_EmitsPairedFunctions(t *testing.T) {
	in := apiTestInput(t, nullVariantTestSchema())
	configureNullVariantOverrides(t, in)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/scalars", apiCtx); err != nil {
		t.Fatalf("rendering api/scalars: %v", err)
	}
	body := buf.String()

	musts := []string{
		"func MarshalUUID(v uuid.UUID) graphql.Marshaler",
		"func UnmarshalUUID(v any) (uuid.UUID, error)",
		"func MarshalNullUUID(v uuid.NullUUID) graphql.Marshaler",
		"func UnmarshalNullUUID(v any) (uuid.NullUUID, error)",
		"func MarshalDecimal(v decimal.Decimal) graphql.Marshaler",
		"func UnmarshalDecimal(v any) (decimal.Decimal, error)",
		"func MarshalNullDecimal(v decimal.NullDecimal) graphql.Marshaler",
		"func UnmarshalNullDecimal(v any) (decimal.NullDecimal, error)",
		// Null-variant marshalers must short-circuit to graphql.Null when
		// !Valid — that's the wire-format contract per §26.4.1.
		"if !v.Valid {",
		"return graphql.Null",
	}
	for _, want := range musts {
		if !strings.Contains(body, want) {
			t.Errorf("scalars template body missing %q\nfull body:\n%s", want, body)
		}
	}
}

// TestMarshalNullUUID_OmittedWhenNotInScope covers (d) — when only the
// non-null bare type is used, the Null-variant marshalers must NOT be
// emitted. Avoids dragging unused symbols into scalars_gen.go that the
// consumer would have to import deps for (and that gqlgen would never
// bind).
func TestMarshalNullUUID_OmittedWhenNotInScope(t *testing.T) {
	in := apiTestInput(t, scalarsTestSchemaWithUUIDDecimal())
	in.Config.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	in.Config.Overrides.Types["numeric"] = config.TypeOverride{
		Type:   "decimal.Decimal",
		Import: "github.com/shopspring/decimal",
	}
	in.Resolver = gotype.NewResolver("postgres", true, in.Config.Overrides.Types)

	tables, err := gen.BuildTableContexts(in, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tables, nil, nil, nil, in.Config)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	tmpl := loadAPISchemaTemplate(t)
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/scalars", apiCtx); err != nil {
		t.Fatalf("rendering api/scalars: %v", err)
	}
	body := buf.String()

	if !strings.Contains(body, "func MarshalUUID(") {
		t.Fatalf("expected bare MarshalUUID in body:\n%s", body)
	}
	for _, omit := range []string{"MarshalNullUUID", "UnmarshalNullUUID", "MarshalNullDecimal", "UnmarshalNullDecimal"} {
		if strings.Contains(body, omit) {
			t.Errorf("scalars body must not emit %q when no Null wrapper column is in scope", omit)
		}
	}
}

// TestScalarsTemplate_UUIDParseFuncAlwaysNamed pins that the emitted UUID and
// NullUUID unmarshalers always spell a function name.
//
// The parse call is per-integration because gofrs exports no Parse
// (PRD §7.4, "Parsing a UUID from a string"), so the template interpolates it.
// An APIContext built by hand carries no binding, and interpolating an empty
// string there yields `return (s)` — not Go, and invisible to every other test
// in this package, because scalars_gen.go is emitted rather than compiled here.
// APIContext.ParseUUIDFunc is what closes that, and this is the pin on it.
func TestScalarsTemplate_UUIDParseFuncAlwaysNamed(t *testing.T) {
	tests := []struct {
		name      string
		parseFunc string
		want      string
	}{
		// The shape a hand-built context takes: no binding, so the standard
		// library's spelling stands in.
		{name: "unset falls back to the standard library", parseFunc: "", want: "uuid.Parse("},
		{name: "stdlib", parseFunc: uuidstd.ParseFunc, want: "uuid.Parse("},
		{name: "google", parseFunc: uuidgoogle.ParseFunc, want: "uuid.Parse("},
		// The one that motivated the whole seam.
		{name: "gofrs", parseFunc: uuidgofrs.ParseFunc, want: "uuid.FromString("},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var uses []gen.APIScalarUse
			for _, use := range gen.ExternalScalarUsesForTest() {
				if use.Name == "UUID" || use.Name == "NullUUID" {
					use.GoImport = "github.com/google/uuid"
					uses = append(uses, use)
				}
			}
			if len(uses) != 2 {
				t.Fatalf("registry yielded %d UUID scalar uses, want the UUID/NullUUID pair", len(uses))
			}

			ctx := &gen.APIContext{ExternalScalars: uses, UUIDParseFunc: tt.parseFunc}
			body := renderGqlmodelTemplate(t, "api/scalars", ctx)

			// Both unmarshalers parse, so the call appears twice.
			if got := strings.Count(body, tt.want); got != 2 {
				t.Errorf("rendered %d %q calls, want 2:\n%s", got, tt.want, body)
			}
			// A nameless call has a non-identifier character immediately
			// before the paren; `uuid.Parse(s)` has `e`.
			if loc := namelessCall.FindString(body); loc != "" {
				t.Errorf("rendered a call with no function name (%q):\n%s", loc, body)
			}
		})
	}
}

// namelessCall matches a `(s)` call with no function name in front of it.
var namelessCall = regexp.MustCompile(`[^\w]\(s\)`)
