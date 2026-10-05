package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/wrapper"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// TestBuildMergeInput_SignatureParity_RowStructsAndScalars is the
// codegen-time signature-parity guard. The resolvers_gen.go file references
// every API table's row struct (e.g. `*<pkg>.Product`) plus every used
// scalar (UUID / JSON / etc.) via the consumer's models package. The
// gqlgen.yml `models:` block produced by buildMergeInput must alias each of
// those types — without an alias, gqlgen autobinds the GraphQL type to its
// own `graph/model/` autogen package and the generated `QueryResolver` /
// `MutationResolver` interfaces require Go types that diverge from what
// our resolvers return, breaking the consumer's build.
//
// Scope: row structs and scalars only. Generic envelopes
// (Connection[T] / PaginateResult[T]) are pinned separately by
// TestBuildMergeInput_EnvelopeBinding.
func TestBuildMergeInput_SignatureParity_RowStructsAndScalars(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
					{Name: "metadata", Type: "jsonb", Nullable: true},
					{Name: "created_at", Type: "timestamp", Nullable: false},
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "total", Type: "numeric", Nullable: false},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("expected non-nil APIContext for fixture with api.graphql.enabled")
	}
	// Simulate populateAPIContextResolverFields without depending on go.mod
	// in the test repo: production wires ResolverImportPath from
	// `<module>/<output.dir>/<resolver_dir>`; here we set it directly so the
	// merge entries point at a deterministic anchor. The fixture's `uuid`
	// columns need it — since the standard library became the default UUID
	// binding they resolve to `uuid.UUID`, which is an EXTERNAL scalar, and
	// an external scalar carrying no marshaler package of its own anchors on
	// the resolver package.
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	for _, table := range apiCtx.Tables {
		paths, ok := merge.Models[table.StructName]
		if !ok {
			t.Errorf("merge input missing alias for row struct %q (resolvers reference *<pkg>.%s as a return type)", table.StructName, table.StructName)
			continue
		}
		if len(paths) == 0 || paths[0] == "" {
			t.Errorf("merge input alias for %q is empty (got %v)", table.StructName, paths)
		}
	}

	for _, s := range apiCtx.UsedScalars {
		if s.Marshaling == config.ScalarMarshalingBuiltin {
			continue
		}
		paths, ok := merge.Models[s.Name]
		if !ok {
			t.Errorf("merge input missing alias for scalar %q (resolvers reference its Go type via the consumer models package)", s.Name)
			continue
		}
		if len(paths) == 0 || paths[0] == "" {
			t.Errorf("merge input alias for scalar %q is empty (got %v)", s.Name, paths)
		}
	}
}

// TestBuildMergeInput_EnvelopeBinding is a regression guard. With runtime /
// schema shapes aligned, the wrapper's `models:`
// merge must bind every managed table's `<T>Connection` / `<T>ListResult` to
// the per-table generic type aliases the codegen emits into the consumer's
// models package — `<pkg>.<T>Connection` resolves to `Connection[<T>]` and
// `<pkg>.<T>ListResult` resolves to `PaginateResult[<T>]` via Go 1.24+ generic
// type aliases. The alias names (not bracketed instantiation paths) are what
// gqlgen v0.17.x's `model:` parser accepts: gqlgen's `internal/code/util.go::
// PkgAndType` splits on `.` so the bracket form fails to resolve, but
// `code.Unalias()` on the alias gives gqlgen the concrete instantiated struct
// — which keeps the runtime envelopes reused directly with no
// `translate<T>Connection` / `translate<T>ListResult` helpers
// (PRD §26.5.6 — Envelope binding).
func TestBuildMergeInput_EnvelopeBinding(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "total", Type: "numeric", Nullable: false},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	if apiCtx == nil {
		t.Fatal("expected non-nil APIContext for fixture with api.graphql.enabled")
	}

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	// cfg.Output.Dir is "./gen" → tablePkg becomes "example.com/foo/gen".
	const tablePkg = "example.com/foo/gen"
	for _, table := range apiCtx.Tables {
		wantConn := tablePkg + "." + table.StructName + "Connection"
		wantEdge := tablePkg + "." + table.StructName + "Edge"
		wantList := tablePkg + "." + table.StructName + "ListResult"

		gotConn, ok := merge.Models[table.StructName+"Connection"]
		if !ok {
			t.Errorf("merge input missing %q binding", table.StructName+"Connection")
		} else if len(gotConn) != 1 || gotConn[0] != wantConn {
			t.Errorf("%sConnection binding = %v, want [%q]", table.StructName, gotConn, wantConn)
		}

		gotEdge, ok := merge.Models[table.StructName+"Edge"]
		if !ok {
			t.Errorf("merge input missing %q binding", table.StructName+"Edge")
		} else if len(gotEdge) != 1 || gotEdge[0] != wantEdge {
			t.Errorf("%sEdge binding = %v, want [%q]", table.StructName, gotEdge, wantEdge)
		}

		gotList, ok := merge.Models[table.StructName+"ListResult"]
		if !ok {
			t.Errorf("merge input missing %q binding", table.StructName+"ListResult")
		} else if len(gotList) != 1 || gotList[0] != wantList {
			t.Errorf("%sListResult binding = %v, want [%q]", table.StructName, gotList, wantList)
		}
	}

	// Shared non-generic `PageInfo` binds directly (no per-table alias).
	wantPageInfo := tablePkg + ".PageInfo"
	gotPageInfo, ok := merge.Models["PageInfo"]
	if !ok {
		t.Errorf("merge input missing PageInfo binding (required for runtime PageInfo reuse, otherwise gqlgen autogens model.PageInfo and field-resolves Connection.pageInfo)")
	} else if len(gotPageInfo) != 1 || gotPageInfo[0] != wantPageInfo {
		t.Errorf("PageInfo binding = %v, want [%q]", gotPageInfo, wantPageInfo)
	}

	// Regression guard: the bracketed instantiation form must NOT appear
	// — gqlgen v0.17.x's `model:` parser splits on `.` and fails on the
	// bracket form, so any path containing `[` here is a guaranteed gqlgen
	// failure at subprocess time.
	for key, paths := range merge.Models {
		for _, p := range paths {
			if strings.ContainsRune(p, '[') {
				t.Errorf("merge input %q binding %q must not contain bracketed generic instantiation — gqlgen v0.17.x rejects it", key, p)
			}
		}
	}
}

// TestBuildMergeInput_EnvelopeBinding_SkippedWhenModuleUnknown asserts the
// existing fail-soft path (no go.mod ⇒ tablePkg is empty) extends to the
// envelope entries — a missing module path must not produce bogus
// `Connection[.Product]` bindings that would break gqlgen's parser.
func TestBuildMergeInput_EnvelopeBinding_SkippedWhenModuleUnknown(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "name", Type: "text", Nullable: false},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}

	merge, _ := buildMergeInput(cfg, apiCtx, "")

	for _, key := range []string{"Product", "ProductConnection", "ProductEdge", "ProductListResult", "PageInfo"} {
		if _, ok := merge.Models[key]; ok {
			t.Errorf("merge input unexpectedly emitted %q with empty module path", key)
		}
	}
}

// TestBuildMergeInput_ScalarBindings is a regression guard.
// Each scalar in apiCtx.UsedScalars must bind to a `models:` entry that
// gqlgen can actually resolve marshalers for. Two binding shapes per
// PRD §26.4.1:
//
//   - Category-4 (external marshalers, generated into <resolver_dir>/scalars_gen.go):
//     bind to `<resolver_import_path>.<ScalarName>` as a discovery anchor —
//     gqlgen scans this package for `Marshal<ScalarName>` / `Unmarshal<ScalarName>`
//     and infers the Go type from the marshaler signature (Option B, no Go
//     type aliases). Applies to UUID / NullUUID / Decimal / NullDecimal.
//
//   - Category-3 (method-based, native on the runtime type): bind to
//     `<runtime>/types.<TypeName>` so gqlgen calls the type's MarshalGQL /
//     UnmarshalGQL methods directly. Applies to JSON / DateTime /
//     NullDateTime — methods live on the runtime types themselves.
//
// Regression for the old shape where scalarModelPath emitted
// `<GoImport>.<TypeName>` for ALL non-builtin scalars, which routed UUID
// to `github.com/google/uuid.UUID` (a package without MarshalUUID symbols)
// and routed both DateTime and NullDateTime to scalar `"DateTime"` (with
// the registry collision swallowing one of the two bindings).
func TestBuildMergeInput_ScalarBindings(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "user_id", Type: "uuid", Nullable: true},
					{Name: "total", Type: "numeric", Nullable: false},
					{Name: "amount", Type: "numeric", Nullable: true},
					{Name: "occurred_at", Type: "timestamptz", Nullable: false},
					{Name: "processed_at", Type: "timestamptz", Nullable: true},
					{Name: "metadata", Type: "jsonb", Nullable: true},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	cfg.Overrides.Types["uuid"] = config.TypeOverride{
		Type:     "uuid.UUID",
		Import:   "github.com/google/uuid",
		Nullable: config.NullableVariant{Type: "uuid.NullUUID", UnderlyingField: "UUID"},
	}
	cfg.Overrides.Types["numeric"] = config.TypeOverride{
		Type:     "decimal.Decimal",
		Import:   "github.com/shopspring/decimal",
		Nullable: config.NullableVariant{Type: "decimal.NullDecimal", UnderlyingField: "Decimal"},
	}
	cfg.Overrides.Types["timestamptz"] = config.TypeOverride{
		Type:     "types.DateTime",
		Import:   "github.com/teandresmith/sqlgen/types",
		Nullable: config.NullableVariant{Type: "types.NullDateTime", UnderlyingField: "Time"},
	}

	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	// Simulate populateAPIContextResolverFields without depending on go.mod
	// in the test repo: production wires ResolverImportPath from
	// `<module>/<output.dir>/<resolver_dir>`; here we set it directly so the
	// merge entries point at a deterministic anchor.
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	// Category-4 (external) scalars bind to the resolver-dir package.
	wantExternal := map[string]string{
		"UUID":        "example.com/foo/gen/graph.UUID",
		"NullUUID":    "example.com/foo/gen/graph.NullUUID",
		"Decimal":     "example.com/foo/gen/graph.Decimal",
		"NullDecimal": "example.com/foo/gen/graph.NullDecimal",
	}
	for name, want := range wantExternal {
		paths, ok := merge.Models[name]
		if !ok {
			t.Errorf("category-4 scalar %q missing from merge.Models", name)
			continue
		}
		if len(paths) != 1 || paths[0] != want {
			t.Errorf("scalar %q binding: got %v, want [%q]", name, paths, want)
		}
	}

	// Category-3 (method-based) scalars bind to the runtime types package.
	wantMethod := map[string]string{
		"JSON":         "github.com/teandresmith/sqlgen/types.JSON",
		"DateTime":     "github.com/teandresmith/sqlgen/types.DateTime",
		"NullDateTime": "github.com/teandresmith/sqlgen/types.NullDateTime",
	}
	for name, want := range wantMethod {
		paths, ok := merge.Models[name]
		if !ok {
			t.Errorf("category-3 scalar %q missing from merge.Models", name)
			continue
		}
		if len(paths) != 1 || paths[0] != want {
			t.Errorf("scalar %q binding: got %v, want [%q]", name, paths, want)
		}
	}

	// Regression: pre-fix bug — UUID must NOT bind to the third-party
	// package path (gqlgen would scan github.com/google/uuid for MarshalUUID,
	// which doesn't exist).
	for name, paths := range merge.Models {
		for _, p := range paths {
			if name == "UUID" && p == "github.com/google/uuid.UUID" {
				t.Errorf("UUID binding regressed to %q — gqlgen cannot find MarshalUUID in this package", p)
			}
			if name == "Decimal" && p == "github.com/shopspring/decimal.Decimal" {
				t.Errorf("Decimal binding regressed to %q — gqlgen cannot find MarshalDecimal in this package", p)
			}
		}
	}
}

// TestBuildMergeInput_ExternalSkippedWhenResolverPathUnresolvable
// pins the fail-soft behaviour for category-4 bindings — same shape as the
// envelope-binding fail-soft (TestBuildMergeInput_EnvelopeBinding_SkippedWhenModuleUnknown):
// missing module path ⇒ no entry, rather than emitting `.UUID` or similar
// gqlgen-rejecting paths.
func TestBuildMergeInput_ExternalSkippedWhenResolverPathUnresolvable(t *testing.T) {
	schema := &parser.Schema{
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

	cfg := newAPIParityTestConfig()
	cfg.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	cfg.Overrides.Types["numeric"] = config.TypeOverride{
		Type:   "decimal.Decimal",
		Import: "github.com/shopspring/decimal",
	}

	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	// Empty ResolverImportPath simulates the fail-soft path (go.mod
	// missing or output dir unresolvable).
	apiCtx.ResolverImportPath = ""

	merge, _ := buildMergeInput(cfg, apiCtx, "")

	for _, name := range []string{"UUID", "Decimal", "NullUUID", "NullDecimal", "JSON"} {
		if _, ok := merge.Models[name]; ok {
			t.Errorf("category-4 scalar %q must not surface in merge.Models when resolver path is unresolvable", name)
		}
	}
}

// TestBuildMergeInput_JSONRawMessageOverride is the regression guard for the
// override-only `json.RawMessage` → cat-4 `JSON` scalar binding. Both
// `types.JSON` (default, cat-3 method) and `json.RawMessage` (override, cat-4
// external) bind to the same `JSON` scalar (PRD §7.6); gqlgen requires one Go
// type per scalar so the dual case is not expressible, and
// TestBuildMergeInput_ScalarBindings carries no `json` override.
//
// This test covers the cat-4 path by exercising a schema with **only** jsonb
// columns and **only** the `json → json.RawMessage` override (no default
// `types.JSON` columns alongside), pinning:
//
//   - apiCtx.ExternalScalars contains a `JSON` entry (gates scalars_gen.go
//     emission via generateAPIScalarFile),
//   - apiCtx.UsedScalars has exactly one `JSON` entry with
//     Marshaling == external (no cat-3 cohabitation),
//   - merge.Models["JSON"] points to `<resolver_import_path>.JSON` (the
//     cat-4 discovery anchor — gqlgen scans this package for MarshalJSON /
//     UnmarshalJSON and infers the Go type from the marshaler signature, not
//     `encoding/json` where those symbols don't exist),
//   - the rendered scalars_gen.go body includes
//     `MarshalJSON(v json.RawMessage) graphql.Marshaler` /
//     `UnmarshalJSON(v any) (json.RawMessage, error)`.
func TestBuildMergeInput_JSONRawMessageOverride(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Nullable: false},
					{Name: "payload", Type: "jsonb", Nullable: false},
					{Name: "context", Type: "jsonb", Nullable: true},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	cfg.Overrides.Types["jsonb"] = config.TypeOverride{
		Type:   "json.RawMessage",
		Import: "encoding/json",
	}

	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	// Mirror populateAPIContextResolverFields without depending on go.mod;
	// merge.Models["JSON"] derives its cat-4 path from this anchor.
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	// (2) UsedScalars must hold exactly one `JSON` entry and it must be
	// external. A second entry (or a cat-3 entry) would mean the override
	// failed to fully suppress the default `types.JSON` binding.
	var jsonUses []gen.APIScalarUse
	for _, s := range apiCtx.UsedScalars {
		if s.Name == "JSON" {
			jsonUses = append(jsonUses, s)
		}
	}
	if len(jsonUses) != 1 {
		t.Fatalf("UsedScalars: want exactly one JSON entry, got %d: %+v", len(jsonUses), apiCtx.UsedScalars)
	}
	if jsonUses[0].Marshaling != config.ScalarMarshalingExternal {
		t.Errorf("UsedScalars JSON entry: Marshaling = %q, want %q", jsonUses[0].Marshaling, config.ScalarMarshalingExternal)
	}

	// (1) ExternalScalars (the filtered slice that gates scalars_gen.go
	// emission) must include JSON; an empty ExternalScalars list would skip
	// scalars_gen.go entirely and gqlgen would have no marshaler to bind.
	var hasJSONExternal bool
	for _, s := range apiCtx.ExternalScalars {
		if s.Name == "JSON" {
			hasJSONExternal = true
			break
		}
	}
	if !hasJSONExternal {
		t.Fatalf("ExternalScalars missing JSON: %+v", apiCtx.ExternalScalars)
	}

	// (3) The wrapper merge must bind GraphQL `JSON` to the resolver-dir
	// package — gqlgen uses this as the discovery anchor for MarshalJSON /
	// UnmarshalJSON. Binding to `encoding/json` (the override's import path)
	// would point gqlgen at a package without those symbols.
	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")
	const want = "example.com/foo/gen/graph.JSON"
	paths, ok := merge.Models["JSON"]
	if !ok {
		t.Fatalf("merge.Models missing JSON binding")
	}
	if len(paths) != 1 || paths[0] != want {
		t.Errorf("merge.Models[\"JSON\"] = %v, want [%q]", paths, want)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "encoding/json.") {
			t.Errorf("JSON binding regressed to %q — gqlgen cannot find MarshalJSON in encoding/json", p)
		}
	}

	// (4) The rendered scalars_gen.go body must contain the JSON marshaler
	// pair templated by templates/api/scalars.go.tmpl. Renders the template
	// directly (parallel to gen/api_schema_test.go's loadAPISchemaTemplate)
	// since buildMergeInput's caller, generateAPIScalarFile, is internal to
	// the gen package.
	body := renderAPIScalarsTemplateForTest(t, apiCtx)
	musts := []string{
		"func MarshalJSON(v json.RawMessage) graphql.Marshaler",
		"func UnmarshalJSON(v any) (json.RawMessage, error)",
	}
	for _, m := range musts {
		if !strings.Contains(body, m) {
			t.Errorf("scalars template body missing %q\nfull body:\n%s", m, body)
		}
	}
}

// renderAPIScalarsTemplateForTest parses the api/scalars template from the
// gen package's templates directory (relative to the cli test working
// directory) and executes it against the given APIContext. Mirrors
// gen.loadAPISchemaTemplate but with a path adjusted for the cli package's
// location; the cli package can't reach gen's package-private templateFS
// directly without an exported renderer.
func renderAPIScalarsTemplateForTest(t *testing.T, apiCtx *gen.APIContext) string {
	t.Helper()
	tmpl := template.New("").Funcs(gen.FuncMap(sql.NewPostgresDialect()))
	matches, err := filepath.Glob(filepath.Join("..", "gen", "templates", "api", "*.tmpl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("looking up api templates: matches=%v err=%v", matches, err)
	}
	tmpl, err = tmpl.ParseFiles(matches...)
	if err != nil {
		t.Fatalf("parsing api templates: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "api/scalars", apiCtx); err != nil {
		t.Fatalf("rendering api/scalars: %v", err)
	}
	return buf.String()
}

// newAPIParityTestConfig returns a RootConfig with api.graphql enabled +
// the minimum generation defaults BuildTableContextsFromSchema requires.
// Mirrors gen_test.testInput but lives here so the parity test stays
// self-contained inside the cli package (where buildMergeInput is private).
func newAPIParityTestConfig() *config.RootConfig {
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Output.Driver = config.DriverPgx
	cfg.Output.Package = "models"
	cfg.Output.Dir = "./gen"
	cfg.Output.Client = &config.ClientOutputConfig{Name: "Client"}
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.CursorKeys = []string{"id"}
	cfg.Generation.UUIDVersion = "v4"
	cfg.Generation.StrictUpdates = new(true)
	cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
		{Name: "deleted_at", Type: "timestamp"},
	}
	cfg.Generation.UpdateColumns = []string{"updated_at"}
	cfg.Tables = make(map[string]config.TableConfig)
	cfg.Views = make(map[string]config.ViewConfig)
	cfg.Extras = make(map[string]config.ExtraType)
	cfg.Overrides.Types = make(map[string]config.TypeOverride)
	cfg.Overrides.UsePointers = new(true)

	cfg.API = &config.APIConfig{
		Enabled: true,
		GraphQL: &config.GraphQLAPIConfig{
			Enabled:     true,
			SchemaDir:   "./graph",
			ResolverDir: "./graph",
			Package:     "graph",
			FieldCasing: config.FieldCasingCamel,
			Scalars:     map[string]config.ScalarBinding{},
		},
	}
	return cfg
}

// TestBuildMergeInput_NumericWidthAnchors pins the read-side half of the
// numeric-width anchors at the merge boundary.
//
// gqlgen's `injectBuiltins` binds `Int` to int / int32 / int64 and `Float` to
// float64 alone, and injects a builtin only when the key is ABSENT — so the
// moment sqlgen writes an `Int:` entry it owns the whole list. Three claims are
// load-bearing and each is asserted:
//
//   - gqlgen's own entries are restated, FIRST. `Model[0]` is what every
//     GENERATED position binds to, so a narrowed width in front would retype
//     every Int input field in the schema and silently truncate a bigint.
//   - Integer widths anchor on gqlgen's OWN bundled marshalers (graphql.Int16,
//     graphql.Uint32 — present in graphql/int.go and graphql/uint.go, merely
//     absent from injectBuiltins), so sqlgen emits no code for them.
//   - float32 has no bundled marshaler anywhere in gqlgen, so it alone anchors
//     on the pair sqlgen emits into scalars_gen.go.
func TestBuildMergeInput_NumericWidthAnchors(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "readings",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "small", Type: "smallint"},    // int16   — gqlgen ships MarshalInt16
					{Name: "count", Type: "integer"},     // int32   — already bound, contributes nothing
					{Name: "ratio", Type: "real"},        // float32 — sqlgen must supply the pair
					{Name: "samples", Type: "integer[]"}, // []int32 — bound via its element
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	// Anchors follow NumericWidthScalars' sort order (by Name), after the
	// three restated gqlgen entries.
	wantInt := []string{
		"github.com/99designs/gqlgen/graphql.Int",
		"github.com/99designs/gqlgen/graphql.Int32",
		"github.com/99designs/gqlgen/graphql.Int64",
		"github.com/99designs/gqlgen/graphql.Int16",
	}
	if diff := cmp.Diff(wantInt, merge.Models["Int"]); diff != "" {
		t.Errorf("Int model list mismatch (-want +got):\n%s", diff)
	}

	wantFloat := []string{
		"github.com/99designs/gqlgen/graphql.FloatContext",
		"example.com/foo/gen/graph.Float32",
	}
	if diff := cmp.Diff(wantFloat, merge.Models["Float"]); diff != "" {
		t.Errorf("Float model list mismatch (-want +got):\n%s", diff)
	}
}

// TestBuildMergeInput_UnsignedAnchors covers the unsigned family, which
// is MySQL-only. No MySQL example carries an `api:` block, so without this test
// an unsigned anchor never reaches a real `models:` list anywhere in the repo —
// it is asserted at APIFieldContext level and nowhere else.
//
// Separate from the PostgreSQL case rather than folded into it: `real` and
// array columns do not exist in MySQL's type map, so one fixture cannot carry
// both halves honestly.
func TestBuildMergeInput_UnsignedAnchors(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "readings",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "seq", Type: "int unsigned"},      // uint32  — gqlgen ships MarshalUint32
					{Name: "total", Type: "bigint unsigned"}, // uint64  — gqlgen ships MarshalUint64
					{Name: "flags", Type: "tinyint"},         // int8    — gqlgen ships MarshalInt8
					{Name: "ratio", Type: "float"},           // float32 — sqlgen must supply the pair
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	cfg.Input.Dialect = config.DialectMySQL
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	merge, warnings := buildMergeInput(cfg, apiCtx, "example.com/foo")
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none — every anchor is spellable here", warnings)
	}

	// Sorted by anchor Name after the three restated gqlgen entries.
	wantInt := []string{
		"github.com/99designs/gqlgen/graphql.Int",
		"github.com/99designs/gqlgen/graphql.Int32",
		"github.com/99designs/gqlgen/graphql.Int64",
		"github.com/99designs/gqlgen/graphql.Int8",
		"github.com/99designs/gqlgen/graphql.Uint32",
		"github.com/99designs/gqlgen/graphql.Uint64",
	}
	if diff := cmp.Diff(wantInt, merge.Models["Int"]); diff != "" {
		t.Errorf("Int model list mismatch (-want +got):\n%s", diff)
	}
	wantFloat := []string{
		"github.com/99designs/gqlgen/graphql.FloatContext",
		"example.com/foo/gen/graph.Float32",
	}
	if diff := cmp.Diff(wantFloat, merge.Models["Float"]); diff != "" {
		t.Errorf("Float model list mismatch (-want +got):\n%s", diff)
	}
}

// TestBuildMergeInput_UnspellableAnchorWarns pins that the one path that
// cannot emit an anchor says so. A sqlgen-emitted pair (float32) still lands in
// scalars_gen.go when the resolver import path is unresolvable, so silently
// skipping the anchor would leave the column with a marshaler nothing points at
// — the panic resolver the anchors exist to remove.
func TestBuildMergeInput_UnspellableAnchorWarns(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "readings",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "ratio", Type: "real"},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ResolverImportPath = "" // go.mod unreachable

	merge, warnings := buildMergeInput(cfg, apiCtx, "")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Float32") {
		t.Fatalf("warnings = %v, want one naming the Float32 anchor", warnings)
	}
	if paths, ok := merge.Models["Float"]; ok {
		t.Errorf("Models[\"Float\"] = %v, want the key absent rather than a builtin-stripping restatement", paths)
	}
}

// TestBuildMergeInput_NoAnchorsLeavesSpecScalarsAlone pins the
// no-op case, which is the common one. Writing a bare `Int:` restatement for a
// schema that needs no anchor would take ownership of gqlgen's builtin list to
// add nothing — and freeze it at whatever sqlgen last copied.
func TestBuildMergeInput_NoAnchorsLeavesSpecScalarsAlone(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "readings",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "count", Type: "integer"},
					{Name: "total", Type: "bigint"},
					{Name: "ratio", Type: "double precision"},
				},
			},
		},
	}

	cfg := newAPIParityTestConfig()
	tableCtxs, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema: %v", err)
	}
	apiCtx, err := gen.BuildAPIContext(tableCtxs, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: %v", err)
	}
	apiCtx.ResolverImportPath = "example.com/foo/gen/graph"

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")

	for _, scalar := range []string{"Int", "Float"} {
		if paths, ok := merge.Models[scalar]; ok {
			t.Errorf("Models[%q] = %v, want the key absent so gqlgen injects its own builtins", scalar, paths)
		}
	}
}

// TestManagedFieldOwners pins the wiring behind the stub-check error: every
// managed root field the wrapper checks is keyed to its entity, so a field
// gqlgen spells differently names the table or view and its struct_name key.
func TestManagedFieldOwners(t *testing.T) {
	apiCtx := &gen.APIContext{Tables: []gen.APITableContext{
		{
			StructName: "OnlyPK", StructNamePlural: "OnlyPKs", QueryName: "onlyPK",
			SQLTable:   "only_pks",
			Operations: gen.ResolvedOperations{Get: true, HardDelete: true},
		},
		{
			StructName: "GPUStat", StructNamePlural: "GPUStats", QueryName: "gpuStat", ListQueryName: "gpuStatList",
			SQLTable: "gpu_stats", IsView: true,
			Operations: gen.ResolvedOperations{Paginate: true},
		},
	}}
	want := map[string]wrapper.ManagedFieldOwner{
		"Q.OnlyPK":       {ConfigKey: "tables.only_pks", GraphQLName: "onlyPK"},
		"M.DeleteOnlyPK": {ConfigKey: "tables.only_pks", GraphQLName: "deleteOnlyPK"},
		"Q.GPUStatList":  {ConfigKey: "views.gpu_stats", GraphQLName: "gpuStatList"},
	}
	cfg := &config.RootConfig{Input: config.InputConfig{Dialect: config.DialectSQLite}}
	_, managed, err := buildSeedsAndManagedFields(apiCtx, cfg)
	if err != nil {
		t.Fatalf("buildSeedsAndManagedFields: %v", err)
	}
	if diff := cmp.Diff(want, managed.Owners); diff != "" {
		t.Errorf("managed-field owners (-want +got):\n%s", diff)
	}
}
