package gen_test

import (
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// The three supported UUID integrations, spelled the way a consumer spells
// them in `overrides.types.uuid` (PRD §7.4). Only `type` and `import` are
// given: everything else is what the integration back-fills, and pinning the
// rest here would test the fixture rather than the rule.
var (
	uuidStdlib = config.TypeOverride{Type: "uuid.UUID", Import: "uuid"}
	uuidGoogle = config.TypeOverride{Type: "uuid.UUID", Import: "github.com/google/uuid"}
	uuidGofrs  = config.TypeOverride{Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"}
)

// tableOverriding returns a TableConfig whose table-scoped `overrides.types`
// binds `uuid` to one library.
func tableOverriding(override config.TypeOverride) config.TableConfig {
	return config.TableConfig{
		Overrides: &config.OverrideConfig{
			Types: map[string]config.TypeOverride{"uuid": override},
		},
	}
}

// TestValidateUUIDLibraries covers the one-UUID-library-per-generated-package
// rule (PRD §4.13 "Two UUID libraries resolved in one generated package",
// §7.4).
//
// Every "wants error" case generated silently before this rule existed, and
// what it produced was a package that does not compile: two imports binding
// `uuid`, plus a silent mis-resolution of whichever `uuid.` qualifier lost.
//
// Every "wants no error" case is a config the rule must leave alone — each of
// the three libraries on its own, an unconfigured package, and the two shapes
// where a second library is *declared* but never resolves.
func TestValidateUUIDLibraries(t *testing.T) {
	tests := []struct {
		name       string
		global     *config.TypeOverride
		tables     []sqlparser.Table
		views      []sqlparser.View
		composites []sqlparser.CompositeType
		domains    []sqlparser.DomainType
		extras     map[string]config.ExtraType
		tableCf    map[string]config.TableConfig
		exclude    []string
		wantErr    []string
	}{
		{
			name:   "no uuid configuration at all",
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
		},
		{
			name:   "the standard library alone",
			global: &uuidStdlib,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			views:  []sqlparser.View{namedView("user_summaries", "")},
		},
		{
			name:   "google alone",
			global: &uuidGoogle,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			views:  []sqlparser.View{namedView("user_summaries", "")},
		},
		{
			name:   "gofrs alone",
			global: &uuidGofrs,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			views:  []sqlparser.View{namedView("user_summaries", "")},
		},
		{
			// The same library named twice is one library. A table-scoped
			// restatement is redundant, not a conflict.
			name:    "one library restated at table scope",
			global:  &uuidGoogle,
			tables:  []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
		},
		{
			// The headline case: a global default with one table pinned to a
			// different library. This is the config an example would trip
			// over if a table-scoped override were left behind.
			name:    "global standard library with a table-scoped google override",
			global:  &uuidStdlib,
			tables:  []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			wantErr: []string{
				"2 UUID libraries resolved in one generated package",
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by table users, column "id")`,
				"split them across separate output.dir packages",
			},
		},
		{
			// The same conflict with the standard library left IMPLICIT.
			// Were a bare `uuid` column to resolve to Go `string`, it would
			// carry no import, make no claim, and this config would
			// validate; the default `uuid.UUID` binding (PRD §7.2, §7.4)
			// makes `users.id` a standard-library claim and the package a
			// two-library one. The error must name `"uuid"` as the library
			// nothing in the config spells.
			name:    "the default binding conflicts with a table-scoped override",
			tables:  []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			wantErr: []string{
				"2 UUID libraries resolved in one generated package",
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by table users, column "id")`,
			},
		},
		{
			// `column_map.<col>.import` reaches the import block without
			// going through `overrides.types` at all, so a rule reading only
			// the override maps would miss it.
			name:   "column_map.import naming a second library",
			global: &uuidStdlib,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{
				"orders": {ColumnMap: map[string]config.ColumnOverride{
					"id": {Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
				}},
			},
			wantErr: []string{
				`"github.com/gofrs/uuid/v5" (selected by table orders, column "id")`,
				`"uuid" (selected by table users, column "id")`,
			},
		},
		{
			// A view has no `overrides` block of its own, so its columns
			// resolve through the global override — which is exactly why the
			// rule has to read view contexts and not just table ones. The
			// view is schema-qualified so the error's rendering of a
			// non-default schema is pinned too.
			name:    "a view column carrying the global override participates",
			global:  &uuidStdlib,
			tables:  []sqlparser.Table{namedTable("orders", "")},
			views:   []sqlparser.View{namedView("order_summaries", "public")},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			wantErr: []string{
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by view public.order_summaries, column "id")`,
			},
		},
		{
			name:   "all three libraries at once",
			global: &uuidStdlib,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", ""), namedTable("carts", "")},
			tableCf: map[string]config.TableConfig{
				"orders": tableOverriding(uuidGoogle),
				"carts":  tableOverriding(uuidGofrs),
			},
			wantErr: []string{
				"3 UUID libraries resolved in one generated package",
				`"github.com/gofrs/uuid/v5" (selected by table carts, column "id")`,
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by table users, column "id")`,
			},
		},
		{
			// The rule counts what resolves, not what is declared. An
			// excluded table imports nothing, so the second library it names
			// never reaches the package.
			name:    "an excluded table's library is not resolved",
			global:  &uuidStdlib,
			tables:  []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			exclude: []string{"orders"},
		},
		{
			// The three members of TypeContext render into one types_gen.go,
			// so a composite attribute is a claim on the same terms a column
			// is. Composites resolve with no table overrides in view, so this
			// one takes the global binding and disagrees with the only table.
			name:       "a composite attribute claims the global library",
			global:     &uuidStdlib,
			tables:     []sqlparser.Table{namedTable("orders", "")},
			composites: []sqlparser.CompositeType{{Name: "audit_ref", Attributes: []sqlparser.Attribute{{Name: "actor_id", Type: "uuid"}}}},
			tableCf:    map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			wantErr: []string{
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by composite type audit_ref, field "ActorID")`,
			},
		},
		{
			// A domain is a one-line alias, so the type itself is the claim
			// and the error names no member.
			name:    "a domain's base type claims the global library",
			global:  &uuidStdlib,
			tables:  []sqlparser.Table{namedTable("orders", "")},
			domains: []sqlparser.DomainType{{Name: "actor_id", BaseType: "uuid"}},
			tableCf: map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)},
			wantErr: []string{
				`"github.com/google/uuid" (selected by table orders, column "id")`,
				`"uuid" (selected by domain type actor_id)`,
			},
		},
		{
			// `extras.<T>.fields.<f>.import` is the third resident of
			// types_gen.go, and the only one the consumer names an import on
			// directly.
			name:   "an extras field naming a second library",
			global: &uuidStdlib,
			tables: []sqlparser.Table{namedTable("orders", "")},
			extras: map[string]config.ExtraType{
				"AuditRef": {Fields: map[string]config.ExtraTypeField{
					"actor_id": {Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
				}},
			},
			wantErr: []string{
				`"github.com/gofrs/uuid/v5" (selected by extra type AuditRef, field "ActorID")`,
				`"uuid" (selected by table orders, column "id")`,
			},
		},
		{
			// Same library everywhere across all three types_gen.go residents
			// and the tables: one library, no error.
			name:       "composite, domain, extras and tables all on one library",
			global:     &uuidGoogle,
			tables:     []sqlparser.Table{namedTable("orders", "")},
			composites: []sqlparser.CompositeType{{Name: "audit_ref", Attributes: []sqlparser.Attribute{{Name: "actor_id", Type: "uuid"}}}},
			domains:    []sqlparser.DomainType{{Name: "actor_id", BaseType: "uuid"}},
			extras: map[string]config.ExtraType{
				// Distinct from the composite's resolved name: two entities on
				// one Go type name is the *other* phase-3 rule, and tripping it
				// here would mask what this case is for.
				"AuditMeta": {Fields: map[string]config.ExtraTypeField{
					"actor_id": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
				}},
			},
		},
		{
			// Same principle one step further in: the override binds a SQL
			// type no column on the table has, so nothing resolves through it.
			name:   "a table-scoped override for a SQL type the table does not have",
			global: &uuidStdlib,
			tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")},
			tableCf: map[string]config.TableConfig{
				"orders": {Overrides: &config.OverrideConfig{
					Types: map[string]config.TypeOverride{
						"macaddr": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nameRuleConfig()
			if tt.global != nil {
				cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": *tt.global}
			}
			if tt.tableCf != nil {
				cfg.Tables = tt.tableCf
			}
			if tt.extras != nil {
				cfg.Extras = tt.extras
			}
			cfg.ExcludeTables = tt.exclude
			schema := &sqlparser.Schema{
				Tables:         tt.tables,
				Views:          tt.views,
				CompositeTypes: tt.composites,
				DomainTypes:    tt.domains,
			}

			_, err := gen.ValidateGeneration(schema, cfg)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("ValidateGeneration() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want an error mentioning %v", tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
				}
			}
		})
	}
}

// TestGenerate_MixedUUIDLibrariesRejectedBeforeAnyWrite drives the real
// emission path under `layout: file_per_table`, the layout the rule's
// rationale is most often doubted for: imports are resolved per package, so
// splitting the tables into separate files does not separate the libraries.
// Generation must refuse, and refuse before it has written a file.
func TestGenerate_MixedUUIDLibrariesRejectedBeforeAnyWrite(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "file_per_table")
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidStdlib}
	cfg.Tables = map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")}}

	_, err := gen.Generate(schema, cfg, "test")
	if err == nil {
		t.Fatal("Generate() error = nil, want two UUID libraries to be rejected")
	}
	for _, want := range []string{`"uuid"`, `"github.com/google/uuid"`, "table orders"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Generate() error = %v, want it to mention %q", err, want)
		}
	}

	if got := dirEntries(t, outDir); len(got) != 0 {
		t.Errorf("Generate() wrote %v before rejecting the config; want nothing on disk", got)
	}
}

// TestValidateAndGenerate_AgreeOnMixedUUIDLibraries pins the property PRD
// §4.13's phase-3 note promises: `sqlgen validate` answers for every
// resolution-level rule without writing a file, and answers with the same
// message `generate` would give. Two rules in two places would drift; one rule
// reached from both entry points cannot.
func TestValidateAndGenerate_AgreeOnMixedUUIDLibraries(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidGoogle}
	cfg.Tables = map[string]config.TableConfig{"orders": tableOverriding(uuidGofrs)}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")}}

	_, genErr := gen.Generate(schema, cfg, "test")
	if genErr == nil {
		t.Fatal("Generate() error = nil, want two UUID libraries to be rejected")
	}

	_, validateErr := gen.ValidateGeneration(schema, cfg)
	if validateErr == nil {
		t.Fatal("ValidateGeneration() error = nil, want the same rejection generate gave")
	}
	if !strings.Contains(validateErr.Error(), genErr.Error()) {
		t.Errorf("ValidateGeneration() error = %q, want it to carry generate's message %q", validateErr, genErr)
	}
	if got := dirEntries(t, outDir); len(got) != 0 {
		t.Errorf("validate wrote %v; want nothing on disk", got)
	}
}

// TestBuildEntityContextsFromSchema_RejectsMixedUUIDLibraries covers the third
// entry point. `sqlgen graphql gen` reaches the context builders without going
// through Generate or ValidateGeneration, and it emits the gqlgen handoff for
// a package whose models carry the resolved UUID type — so it has to refuse
// the same config.
func TestBuildEntityContextsFromSchema_RejectsMixedUUIDLibraries(t *testing.T) {
	cfg := nameRuleConfig()
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidStdlib}
	cfg.Tables = map[string]config.TableConfig{"orders": tableOverriding(uuidGoogle)}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{namedTable("users", ""), namedTable("orders", "")}}

	if _, err := gen.BuildTableContextsFromSchema(schema, cfg); err == nil {
		t.Fatal("BuildTableContextsFromSchema() error = nil, want two UUID libraries to be rejected")
	} else if !strings.Contains(err.Error(), "UUID libraries resolved in one generated package") {
		t.Errorf("BuildTableContextsFromSchema() error = %v, want the one-library rule", err)
	}
}

// TestGenerate_TwoPackagesEachWithOneLibrary pins the arrangement PRD §7.4
// keeps supported: a project that genuinely needs two UUID libraries gives
// each its own `sqlgen.yml` and `output.dir`. The rule is per generated
// package, not per module, so both runs must succeed — and each emitted
// package must import its own library and only its own.
func TestGenerate_TwoPackagesEachWithOneLibrary(t *testing.T) {
	packages := []struct {
		name      string
		override  config.TypeOverride
		wantOther string
	}{
		{name: "stdlib", override: uuidStdlib, wantOther: "github.com/google/uuid"},
		{name: "google", override: uuidGoogle, wantOther: "uuid"},
	}

	for _, pkg := range packages {
		t.Run(pkg.name, func(t *testing.T) {
			outDir := t.TempDir()
			cfg := loadLayoutConfig(t, outDir, "single_file")
			cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": pkg.override}
			schema := &sqlparser.Schema{Tables: []sqlparser.Table{namedTable("users", "")}}

			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error = %v, want a single-library package to generate", err)
			}

			imports := modelImports(t, filepath.Join(outDir, "models_gen.go"))
			if !imports[pkg.override.Import] {
				t.Errorf("models_gen.go imports %v, want it to import %q", sortedKeys(imports), pkg.override.Import)
			}
			if imports[pkg.wantOther] {
				t.Errorf("models_gen.go imports %q, want only %q", pkg.wantOther, pkg.override.Import)
			}
		})
	}
}

// TestTypeQualifier pins the reader the rule keys on. The qualifier is taken
// from the resolved Go type rather than from the import path, so every shape
// the resolver can produce for a UUID column — bare, pointer, wrapper, slice —
// has to report `uuid`, and nothing unqualified may.
func TestTypeQualifier(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{goType: "uuid.UUID", want: "uuid"},
		{goType: "*uuid.UUID", want: "uuid"},
		{goType: "uuid.NullUUID", want: "uuid"},
		{goType: "[]uuid.UUID", want: "uuid"},
		{goType: "[]*uuid.UUID", want: "uuid"},
		{goType: "map[string]uuid.UUID", want: "uuid"},
		{goType: "time.Time", want: "time"},
		{goType: "decimal.NullDecimal", want: "decimal"},
		{goType: "string", want: ""},
		{goType: "[]byte", want: ""},
		{goType: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.goType, func(t *testing.T) {
			if got := gen.TypeQualifierForTest(tt.goType); got != tt.want {
				t.Errorf("TypeQualifier(%q) = %q, want %q", tt.goType, got, tt.want)
			}
		})
	}
}

// modelImports parses a generated file and returns the set of import paths it
// declares. Parsing rather than grepping is what makes the negative assertion
// trustworthy: a path can appear in a comment.
func modelImports(t *testing.T, path string) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(path) //nolint:gosec // test helper, path is a file this test just generated under t.TempDir
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	out := make(map[string]bool, len(file.Imports))
	for _, imp := range file.Imports {
		unquoted, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquoting import %s in %s: %v", imp.Path.Value, path, err)
		}
		out[unquoted] = true
	}
	return out
}

// sortedKeys renders a set for a failure message.
func sortedKeys(set map[string]bool) []string {
	return slices.Sorted(maps.Keys(set))
}
