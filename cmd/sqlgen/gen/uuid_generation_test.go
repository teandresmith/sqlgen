package gen_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	sqlparser "github.com/teandresmith/sqlgen/parser"
)

// The tests in this file cover the two places sqlgen emits a call that makes a
// new UUID — the app-strategy primary key (PRD §8.6) and the event ID
// (PRD §28.8) — and the rule both now obey: the call belongs to the UUID
// integration the *package* resolved, spelled per PRD §7.4 "Generating UUID
// values".
//
// Before this, both sites were hardcoded to github.com/google/uuid. That was
// two separate defects. The primary key emitted google's spelling against
// whatever library the columns had actually bound, so an app-strategy PK could
// not compile under gofrs and could not compile under the standard library.
// The event-hooks file went further and appended google's *import*
// unconditionally, so a package that bound any other library got two packages
// named uuid — and validateUUIDLibraries could not catch it, because the
// event-hooks import comes from no config surface it reads.

// uuidLibraries is every import path a generated package could bind to the
// local name `uuid`. A test asserting "only one of these is present" has to
// name them all, including the two it expects to be absent.
var uuidLibraries = []string{"uuid", "github.com/google/uuid", "github.com/gofrs/uuid/v5"}

// appPKTable is a table whose uuid primary key has no DEFAULT, which is the
// shape detectPKStrategy resolves to "app" (PRD §8.6) — so the create and
// upsert templates emit a generating call for it.
func appPKTable(name string) sqlparser.Table {
	return sqlparser.Table{
		Name: name,
		Columns: []sqlparser.Column{
			{Name: "id", Type: "uuid", PrimaryKey: true},
			{Name: "label", Type: "text"},
		},
	}
}

// intPKTable has no uuid column at all, so a package built from it alone
// selects no integration.
func intPKTable(name string) sqlparser.Table {
	return sqlparser.Table{
		Name: name,
		Columns: []sqlparser.Column{
			{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
			{Name: "label", Type: "text"},
		},
	}
}

// TestGenerate_PKAutoGenExprPerIntegration renders the real create/upsert path
// for every integration, at both versions, with the primary key in both shapes
// PRD §7.4 distinguishes: left on the resolved uuid.UUID (the value form) and
// overridden to a Go string (the string form).
//
// Both shapes live in one generated package on purpose. The string-PK table
// binds no uuid-qualified type of its own, so the only thing that can tell the
// generator which library to spell is what the *other* table resolved — which
// is the whole point of selecting per package rather than per column.
func TestGenerate_PKAutoGenExprPerIntegration(t *testing.T) {
	integrations := []struct {
		name     string
		override config.TypeOverride
	}{
		{"stdlib", uuidStdlib},
		{"google", uuidGoogle},
		{"gofrs", uuidGofrs},
	}
	versions := []config.UUIDVersion{config.UUIDVersionV4, config.UUIDVersionV7}
	// Both layouts, because the import half of the fix is layout-sensitive in
	// principle: file_per_table splits the two tables across files, and the
	// string-PK one carries no uuid-qualified type of its own. Imports are
	// resolved per package rather than per file (see uuid_library.go), so it
	// must still land — and an assertion is cheaper than trusting that.
	layouts := []string{"single_file", "file_per_table"}

	for _, integ := range integrations {
		for _, version := range versions {
			for _, layout := range layouts {
				t.Run(integ.name+"/"+string(version)+"/"+layout, func(t *testing.T) {
					outDir := t.TempDir()
					cfg := loadLayoutConfig(t, outDir, layout)
					cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": integ.override}
					cfg.Generation.UUIDVersion = version
					// The string-PK table keeps its `uuid` SQL type — so the
					// strategy is still "app" — while resolving to a Go string.
					cfg.Tables = map[string]config.TableConfig{
						"tickets": {ColumnMap: map[string]config.ColumnOverride{"id": {Type: "string"}}},
					}
					schema := &sqlparser.Schema{Tables: []sqlparser.Table{appPKTable("orders"), appPKTable("tickets")}}

					if _, err := gen.Generate(schema, cfg, "test"); err != nil {
						t.Fatalf("Generate() error = %v", err)
					}

					binding := gotype.UUIDIntegrationFor(integ.override.Import)
					body := generatedTree(t, outDir)
					for _, want := range []string{
						"pkValue = " + binding.ValueExpr(version),
						"pkValue = " + binding.StringExpr(version),
					} {
						if !strings.Contains(body, want) {
							t.Errorf("generated package missing %q", want)
						}
					}
					assertOnlyUUIDLibrary(t, outDir, integ.override.Import)
				})
			}
		}
	}
}

// TestGenerate_StdlibEventsPackageImportsOneUUIDLibrary is the failing-first
// case for the event-hooks half. A stdlib-bound package with events enabled
// emitted two UUID imports — `uuid` from its columns and
// `github.com/google/uuid` from the hardcoded event-hooks block — which is the
// `uuid redeclared in this block` failure the one-library rule exists to
// prevent, reached from a file that rule cannot see. Several examples use
// this configuration.
func TestGenerate_StdlibEventsPackageImportsOneUUIDLibrary(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidStdlib}
	cfg.Events = &config.EventConfig{Enabled: true}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{appPKTable("orders")}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertOnlyUUIDLibrary(t, outDir, "uuid")

	hooks := readGenerated(t, filepath.Join(outDir, "event_hooks_gen.go"))
	if want := "uuid.NewV4().String()"; !strings.Contains(hooks, want) {
		t.Errorf("event_hooks_gen.go missing %q — event IDs must use the selected integration's string form", want)
	}
}

// TestGenerate_EventsOnlyPackageSelectsStdlib covers the fallback PRD §7.4 and
// §28.8 both state: a package with no uuid column selects no integration, so
// enabling events must not drag a UUID module into the consumer's go.mod. It
// did before — every events-enabled consumer was made to require
// github.com/google/uuid whether or not it used a UUID anywhere.
func TestGenerate_EventsOnlyPackageSelectsStdlib(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Events = &config.EventConfig{Enabled: true}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{intPKTable("audits")}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertOnlyUUIDLibrary(t, outDir, "uuid")

	hooks := readGenerated(t, filepath.Join(outDir, "event_hooks_gen.go"))
	if want := "uuid.NewV4().String()"; !strings.Contains(hooks, want) {
		t.Errorf("event_hooks_gen.go missing %q", want)
	}
}

// TestBuildEventHooksContext_PerIntegration pins the event-ID half of PRD
// §7.4's table directly on the context builder, across every integration and
// both versions. An event ID is a string by definition (PRD §28.3), so only
// the string column of that table is ever reachable here — which is exactly
// why google's v4 cell matters: uuid.NewString() is a different call, not
// uuid.New() with .String() appended.
func TestBuildEventHooksContext_PerIntegration(t *testing.T) {
	tests := []struct {
		name       string
		importPath string
		wantV4     string
		wantV7     string
	}{
		{"stdlib", "uuid", "uuid.NewV4().String()", "uuid.NewV7().String()"},
		{"google", "github.com/google/uuid", "uuid.NewString()", "uuid.Must(uuid.NewV7()).String()"},
		{"gofrs", "github.com/gofrs/uuid/v5", "uuid.Must(uuid.NewV4()).String()", "uuid.Must(uuid.NewV7()).String()"},
	}

	for _, tt := range tests {
		for _, version := range []config.UUIDVersion{config.UUIDVersionV4, config.UUIDVersionV7} {
			t.Run(tt.name+"/"+string(version), func(t *testing.T) {
				cfg := nameRuleConfig()
				cfg.Events = &config.EventConfig{Enabled: true}
				cfg.Generation.UUIDVersion = version

				ctx := gen.BuildEventHooksContext(nil, cfg, "db", "Client", gotype.UUIDIntegrationFor(tt.importPath))
				if ctx == nil {
					t.Fatal("BuildEventHooksContext() = nil, want a context with events enabled")
				}

				want := tt.wantV4
				if version == config.UUIDVersionV7 {
					want = tt.wantV7
				}
				if ctx.UUIDGenExpr != want {
					t.Errorf("UUIDGenExpr = %q, want %q", ctx.UUIDGenExpr, want)
				}
				if !slices.Contains(ctx.Imports, tt.importPath) {
					t.Errorf("Imports = %v, want it to declare %q", ctx.Imports, tt.importPath)
				}
				for _, other := range uuidLibraries {
					if other != tt.importPath && slices.Contains(ctx.Imports, other) {
						t.Errorf("Imports = %v, want %q alone", ctx.Imports, tt.importPath)
					}
				}
			})
		}
	}
}

// assertOnlyUUIDLibrary checks that the generated package binds `uuid` to want
// and to nothing else. Per package rather than per file is the right scope:
// sqlgen resolves imports once over the whole package, so a second library
// anywhere is a second library everywhere (see uuid_library.go).
func assertOnlyUUIDLibrary(t *testing.T, dir, want string) {
	t.Helper()

	found := make(map[string][]string)
	for _, path := range generatedGoFiles(t, dir) {
		for imp := range modelImports(t, path) {
			if slices.Contains(uuidLibraries, imp) {
				found[imp] = append(found[imp], filepath.Base(path))
			}
		}
	}

	if _, ok := found[want]; !ok {
		t.Errorf("generated package binds %v, want it to import %q", slices.Sorted(maps.Keys(found)), want)
	}
	for path, files := range found {
		if path != want {
			t.Errorf("generated package imports %q in %v, want %q alone", path, files, want)
		}
	}
}

// generatedGoFiles lists the .go files a generation run wrote, recursively —
// the API layer emits into a sub-directory.
func generatedGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return paths
}

// generatedTree returns every generated .go file concatenated, for assertions
// that do not care which file an expression landed in.
func generatedTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, path := range generatedGoFiles(t, dir) {
		b.WriteString(readGenerated(t, path))
	}
	return b.String()
}

func readGenerated(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path) //nolint:gosec // test helper, path is a file this test just generated under t.TempDir
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(src)
}

// TestGenerate_RetypedUUIDPKSelectsConfiguredIntegration pins the second step
// of selectUUIDIntegration's cascade: when nothing resolved uuid-qualified, the
// package generates with the library its *config* names rather than falling
// through to the standard library.
//
// Before this, `overrides.types.uuid` naming google plus a `type_map` retyping
// the uuid PK to a Go string produced a package that imported the standard
// library `uuid` and emitted uuid.NewV4().String(). It compiled and both
// libraries make valid v4 UUIDs, which is why it went unnoticed — it was simply
// not the library the consumer asked for.
func TestGenerate_RetypedUUIDPKSelectsConfiguredIntegration(t *testing.T) {
	tests := []struct {
		name     string
		override config.TypeOverride
		version  config.UUIDVersion
		wantLib  string
		wantExpr string
	}{
		{
			name:     "google v4",
			override: uuidGoogle,
			version:  config.UUIDVersionV4,
			wantLib:  "github.com/google/uuid",
			wantExpr: "uuid.NewString()",
		},
		{
			name:     "google v7 — the version comes from config too",
			override: uuidGoogle,
			version:  config.UUIDVersionV7,
			wantLib:  "github.com/google/uuid",
			wantExpr: "uuid.Must(uuid.NewV7()).String()",
		},
		{
			name:     "gofrs v4",
			override: config.TypeOverride{Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
			version:  config.UUIDVersionV4,
			wantLib:  "github.com/gofrs/uuid/v5",
			wantExpr: "uuid.Must(uuid.NewV4()).String()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			cfg := loadLayoutConfig(t, outDir, "single_file")
			cfg.Generation.UUIDVersion = tt.version
			cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": tt.override}
			// detectPKStrategy still resolves "app" — it reads the SQL
			// type and the absent DEFAULT — but retyping the PK means no
			// column resolves uuid-qualified, so the package carries no
			// claim for collectUUIDClaims to find.
			cfg.Tables = map[string]config.TableConfig{
				"orders": {TypeMap: map[string]string{"id": "string"}},
			}
			schema := &sqlparser.Schema{Tables: []sqlparser.Table{appPKTable("orders")}}

			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			assertOnlyUUIDLibrary(t, outDir, tt.wantLib)

			body := readGenerated(t, filepath.Join(outDir, "models_gen.go"))
			if !strings.Contains(body, tt.wantExpr) {
				t.Errorf("models_gen.go missing %q — the PK must generate with the configured integration", tt.wantExpr)
			}
			if got := "uuid.NewV4().String()"; tt.wantExpr != got && strings.Contains(body, got) {
				t.Errorf("models_gen.go contains %q — the standard library fallback, not the configured integration", got)
			}
		})
	}
}

// TestGenerate_ResolvedColumnsOutrankConfiguredIntegration pins the cascade's
// precedence. A column that actually resolved uuid-qualified is what the
// generated files already spell, so it must beat what the config names — a
// generating call in any other library would put a second package named uuid
// beside it, which is the §4.13 failure.
func TestGenerate_ResolvedColumnsOutrankConfiguredIntegration(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	// The config names google by SQL type...
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidGoogle}
	cfg.Tables = map[string]config.TableConfig{
		"orders": {
			// ...but the uuid PK is retyped away, so google claims nothing,
			// and a different column resolves gofrs instead.
			TypeMap: map[string]string{"id": "string"},
			ColumnMap: map[string]config.ColumnOverride{
				"label": {Type: "uuid.UUID", Import: "github.com/gofrs/uuid/v5"},
			},
		},
	}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{appPKTable("orders")}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertOnlyUUIDLibrary(t, outDir, "github.com/gofrs/uuid/v5")

	body := readGenerated(t, filepath.Join(outDir, "models_gen.go"))
	if want := "uuid.Must(uuid.NewV4()).String()"; !strings.Contains(body, want) {
		t.Errorf("models_gen.go missing %q — a resolved column must outrank the configured integration", want)
	}
}

// TestGenerate_NoUUIDAnywhereStillCostsNoDependency is the negative control for
// step 3, and the property PRD §7.4 and §28.8 protect: a consumer who names no
// UUID library anywhere must not acquire a UUID module by enabling events. The
// config fallback must not reach a package whose config names nothing.
func TestGenerate_NoUUIDAnywhereStillCostsNoDependency(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Events = &config.EventConfig{Enabled: true}
	schema := &sqlparser.Schema{Tables: []sqlparser.Table{intPKTable("audits")}}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertOnlyUUIDLibrary(t, outDir, "uuid")
}
